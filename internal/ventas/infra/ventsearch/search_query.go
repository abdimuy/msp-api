// Package ventsearch — search_query.go implements the Buscar method of
// MeilisearchVentaSearchIndex, translating a VentasSearchQuery into a
// platform SearchParams and mapping the SearchResult back to a
// VentasSearchResultado (ordered IDs + total).
//
//nolint:misspell // Spanish domain vocabulary (ventas, momento, etc.) by project convention.
package ventsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// situacionCancelada is the domain.SituacionCancelada wire value. Duplicated
// here (as a plain string literal) because this package must not import
// internal/ventas/domain — see the package doc in document.go.
const situacionCancelada = "cancelada"

// sortMapping maps the canonical SortBy vocabulary (from ListarVentasInput)
// to the corresponding Meilisearch sortable attribute name.
var sortMapping = map[string]string{
	"fecha_venta":    "fecha_venta_ts",
	"precio_total":   "precio_total",
	"nombre_cliente": "nombre_cliente",
}

// Buscar executes a ventas search against the Meilisearch index and returns
// the matched venta IDs in result order together with the estimated total.
//
// Filter construction: each set filter becomes an AND-joined filter clause.
// String values are quoted; numeric and boolean values are not.
//
// Sort logic:
//   - When SortBy is set: use sortMapping[SortBy]:order.
//   - When SortBy is empty AND Q is empty: default to fecha_venta_ts:desc
//     (most recent sale first).
//   - When SortBy is empty AND Q is non-empty: no sort (use Meilisearch
//     relevance).
//
// On transient Meilisearch errors (ErrMeilisearchTransient,
// ErrMeilisearchNotConfigured), returns an apperror with
// KindServiceUnavailable.
func (idx *MeilisearchVentaSearchIndex) Buscar(
	ctx context.Context,
	q outbound.VentasSearchQuery,
) (outbound.VentasSearchResultado, error) {
	params := platformmeili.SearchParams{
		Query:  q.Q,
		Offset: int64(q.Offset),
		Limit:  int64(q.Limit),
		Filter: buildFilter(q),
		Sort:   buildSort(q),
	}

	result, err := idx.client.Search(ctx, idx.indexName, params)
	if err != nil {
		if errors.Is(err, platformmeili.ErrMeilisearchNotConfigured) ||
			errors.Is(err, platformmeili.ErrMeilisearchTransient) {
			return outbound.VentasSearchResultado{}, apperror.NewServiceUnavailable(
				"ventas_search_unavailable",
				"el buscador de ventas no está disponible en este momento",
			).WithError(err)
		}
		return outbound.VentasSearchResultado{}, apperror.NewInternal(
			"ventas_search_failed",
			"error al buscar en las ventas",
		).WithError(err)
	}

	ids := make([]uuid.UUID, 0, len(result.Hits))
	for i, raw := range result.Hits {
		var hit struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &hit); err != nil {
			return outbound.VentasSearchResultado{}, apperror.NewInternal(
				"ventas_search_unmarshal_failed",
				"error al decodificar resultados de ventas",
			).WithError(fmt.Errorf("hit %d: %w", i, err))
		}
		id, err := uuid.Parse(hit.ID)
		if err != nil {
			return outbound.VentasSearchResultado{}, apperror.NewInternal(
				"ventas_search_invalid_id",
				"error al decodificar identificador de venta",
			).WithError(fmt.Errorf("hit %d: %w", i, err))
		}
		ids = append(ids, id)
	}

	total := int(result.EstimatedTotalHits)
	if total > outbound.MaxTotalHitsVentas {
		total = outbound.MaxTotalHitsVentas
	}

	return outbound.VentasSearchResultado{IDs: ids, Total: total}, nil
}

// buildFilter constructs the Meilisearch filter expression from a
// VentasSearchQuery. Returns an empty string when no filters are set
// (Meilisearch accepts ""). Multiple clauses are joined with AND.
func buildFilter(q outbound.VentasSearchQuery) string {
	var clauses []string

	if q.TipoVenta != nil {
		clauses = append(clauses, fmt.Sprintf("tipo_venta = %q", *q.TipoVenta))
	}
	if q.Situacion != nil {
		clauses = append(clauses, fmt.Sprintf("situacion = %q", *q.Situacion))
	}
	if q.Sincronizacion != nil {
		clauses = append(clauses, fmt.Sprintf("sincronizacion = %q", *q.Sincronizacion))
	}
	if q.ZonaClienteID != nil {
		clauses = append(clauses, fmt.Sprintf("zona_cliente_id = %d", *q.ZonaClienteID))
	}
	if q.VendedorEmail != nil {
		clauses = append(clauses, fmt.Sprintf("vendedor_email = %q", *q.VendedorEmail))
	}
	if q.ClienteID != nil {
		clauses = append(clauses, fmt.Sprintf("cliente_id = %d", *q.ClienteID))
	}
	if q.Estado != nil {
		clauses = append(clauses, fmt.Sprintf("estado = %q", *q.Estado))
	}
	if !q.IncluirCanceladas {
		clauses = append(clauses, fmt.Sprintf("situacion != %q", situacionCancelada))
	}
	if q.FechaDesde != nil {
		clauses = append(clauses, fmt.Sprintf("fecha_venta_ts >= %d", q.FechaDesde.UTC().Unix()))
	}
	if q.FechaHasta != nil {
		clauses = append(clauses, fmt.Sprintf("fecha_venta_ts < %d", q.FechaHasta.UTC().Unix()))
	}
	if q.PrecioMin != nil {
		clauses = append(clauses, "precio_total >= "+q.PrecioMin.String())
	}
	if q.PrecioMax != nil {
		clauses = append(clauses, "precio_total < "+q.PrecioMax.String())
	}

	return strings.Join(clauses, " AND ")
}

// buildSort returns the Meilisearch sort clause list for the given sort
// configuration. Returns nil (no sort) when relevance ranking should apply.
func buildSort(q outbound.VentasSearchQuery) []string {
	order := "asc"
	if q.SortOrder == "desc" {
		order = "desc"
	}

	if q.SortBy != "" {
		attr, ok := sortMapping[q.SortBy]
		if !ok {
			// Unknown sortBy — no sort; the app layer validates before calling.
			return nil
		}
		return []string{fmt.Sprintf("%s:%s", attr, order)}
	}

	// SortBy is empty: default to fecha_venta_ts:desc (most recent first)
	// when no text query; otherwise leave empty to use Meilisearch relevance
	// ranking.
	if q.Q == "" {
		return []string{"fecha_venta_ts:desc"}
	}
	return nil
}
