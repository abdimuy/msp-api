// Package clientessearch — search_query.go implements the Buscar method of
// MeilisearchDirectoryIndex, translating a DirectorioQuery into a platform
// SearchParams and mapping the SearchResult back to a DirectorioResultado.
//
//nolint:misspell // Spanish domain vocabulary (directorio, clientes, etc.) by project convention.
package clientessearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// sortMapping maps the canonical SortBy vocabulary (from BuscarClientesInput)
// to the corresponding Meilisearch sortable attribute name.
var sortMapping = map[string]string{
	"nombre":      "nombre",
	"saldo":       "saldo",
	"zona":        "zona_id",
	"score":       "score",
	"segmento":    "segmento_orden",
	"estado_pago": "estado_pago_orden",
	"recencia":    "recencia_dias",
	"puntualidad": "pct_pagos_a_tiempo",
	"prox_pago":   "fecha_prox_pago_ts",
}

// Buscar executes a directory search against the Meilisearch index and returns
// a page of matched documents together with facet counts.
//
// Filter construction: each set filter becomes an AND-joined filter clause.
// String values are quoted; numeric and boolean values are not. Server-controlled
// values (enums, ints derived from the incoming struct) are safe to interpolate
// directly, but they are still formatted with fmt.Sprintf for clarity.
//
// Sort logic:
//   - When SortBy is set: use sortMapping[SortBy]:order.
//   - When SortBy is empty AND Q is empty: default to nombre:asc.
//   - When SortBy is empty AND Q is non-empty: no sort (use Meilisearch relevance).
//
// On transient Meilisearch errors (ErrMeilisearchTransient,
// ErrMeilisearchNotConfigured), returns an apperror with KindServiceUnavailable.
func (idx *MeilisearchDirectoryIndex) Buscar(ctx context.Context, q outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	params := platformmeili.SearchParams{
		Query:  q.Q,
		Offset: int64(q.Offset),
		Limit:  int64(q.Limit),
		Facets: FacetAttributes(),
	}

	// Build filter expression.
	params.Filter = buildFilter(q)

	// Build sort clauses.
	params.Sort = buildSort(q.SortBy, q.SortOrder, q.Q)

	result, err := idx.client.Search(ctx, idx.indexName, params)
	if err != nil {
		if errors.Is(err, platformmeili.ErrMeilisearchNotConfigured) ||
			errors.Is(err, platformmeili.ErrMeilisearchTransient) {
			return outbound.DirectorioResultado{}, apperror.NewServiceUnavailable(
				"directorio_search_unavailable",
				"el buscador de directorio no está disponible en este momento",
			).WithError(err)
		}
		return outbound.DirectorioResultado{}, apperror.NewInternal(
			"directorio_search_failed",
			"error al buscar en el directorio de clientes",
		).WithError(err)
	}

	// Unmarshal hits → DirectorioDoc.
	docs := make([]outbound.DirectorioDoc, 0, len(result.Hits))
	for i, raw := range result.Hits {
		var cd ClienteDoc
		if err := json.Unmarshal(raw, &cd); err != nil {
			return outbound.DirectorioResultado{}, apperror.NewInternal(
				"directorio_search_unmarshal_failed",
				"error al decodificar resultados del directorio",
			).WithError(fmt.Errorf("hit %d: %w", i, err))
		}
		docs = append(docs, clienteDocToDirectorioDoc(cd))
	}

	// Convert FacetDistribution (map[string]map[string]int64) → map[string]map[string]int.
	facets := make(map[string]map[string]int, len(result.FacetDistribution))
	for attr, vals := range result.FacetDistribution {
		inner := make(map[string]int, len(vals))
		for k, v := range vals {
			inner[k] = int(v)
		}
		facets[attr] = inner
	}

	return outbound.DirectorioResultado{
		Items:  docs,
		Facets: facets,
		Total:  int(result.EstimatedTotalHits),
	}, nil
}

// buildFilter constructs the Meilisearch filter expression from a DirectorioQuery.
// Returns an empty string when no filters are set (Meilisearch accepts "").
// Multiple clauses are joined with AND.
func buildFilter(q outbound.DirectorioQuery) string {
	var clauses []string

	if q.ZonaClienteID != nil {
		clauses = append(clauses, fmt.Sprintf("zona_id = %d", *q.ZonaClienteID))
	}
	if q.CobradorID != nil {
		clauses = append(clauses, fmt.Sprintf("cobrador_id = %d", *q.CobradorID))
	}
	if q.ConSaldo {
		clauses = append(clauses, "con_saldo = true")
	}
	if q.Segmento != "" {
		clauses = append(clauses, fmt.Sprintf("segmento = %q", q.Segmento))
	}
	if q.EstadoPago != "" {
		clauses = append(clauses, fmt.Sprintf("estado_pago = %q", q.EstadoPago))
	}
	if q.ScoreMin != nil {
		clauses = append(clauses, fmt.Sprintf("score >= %d", *q.ScoreMin))
	}
	if q.TierRiesgo != "" {
		clauses = append(clauses, fmt.Sprintf("tier_riesgo = %q", q.TierRiesgo))
	}

	return strings.Join(clauses, " AND ")
}

// buildSort returns the Meilisearch sort clause list for the given sort
// configuration. Returns nil (no sort) when relevance ranking should apply.
func buildSort(sortBy, sortOrder, query string) []string {
	order := "asc"
	if sortOrder == "desc" {
		order = "desc"
	}

	if sortBy != "" {
		attr, ok := sortMapping[sortBy]
		if !ok {
			// Unknown sortBy — no sort; the app layer validates before calling.
			return nil
		}
		return []string{fmt.Sprintf("%s:%s", attr, order)}
	}

	// SortBy is empty: default to nombre:asc when no text query; otherwise
	// leave empty to use Meilisearch relevance ranking.
	if query == "" {
		return []string{"nombre:asc"}
	}
	return nil
}

// parseDecimal parses an exact decimal string from the index, returning a zero
// decimal on a malformed value rather than panicking — the index is our own
// data, so a parse failure means a bug, not user input, and EOF-safe degradation
// to zero is preferable to crashing a directory read.
func parseDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// clienteDocToDirectorioDoc maps a ClienteDoc (hit from Meilisearch) to the
// port-level DirectorioDoc. Saldo/Monetary are reconstructed from the exact
// string fields (saldo_str, monetary) to avoid float round-trip precision loss.
// PctPagosATiempo is reconstructed from pct_pagos_a_tiempo_str for the same
// reason. FechaProxPago is reconstructed from the epoch-seconds field.
func clienteDocToDirectorioDoc(cd ClienteDoc) outbound.DirectorioDoc {
	doc := outbound.DirectorioDoc{
		ClienteID:          cd.ClienteID,
		Nombre:             cd.Nombre,
		ZonaID:             cd.ZonaID,
		ZonaNombre:         cd.ZonaNombre,
		CobradorID:         cd.CobradorID,
		Estatus:            cd.Estatus,
		Telefono:           cd.Telefono,
		Direccion:          cd.Direccion,
		DireccionCalle:     cd.DireccionCalle,
		DireccionColonia:   cd.DireccionColonia,
		DireccionPoblacion: cd.DireccionPoblacion,
		DireccionCorta:     cd.DireccionCorta,
		Saldo:              parseDecimal(cd.SaldoStr),
		ConSaldo:           cd.ConSaldo,
		Score:              cd.Score,
		Segmento:           cd.Segmento,
		EstadoPago:         cd.EstadoPago,
		RecenciaDias:       cd.RecenciaDias,
		Frecuencia:         cd.Frecuencia,
		Monetary:           parseDecimal(cd.Monetary),
		NextBestProduct:    cd.NextBestProduct,
		TienePulso:         cd.TienePulso,
		TierRiesgo:         cd.TierRiesgo,
		PctPagosATiempo:    parseDecimal(cd.PctPagosATiempoStr),
	}

	// Reconstruct FechaProxPago from epoch-seconds; leave zero when absent.
	if cd.FechaProxPagoTs != 0 {
		doc.FechaProxPago = time.Unix(cd.FechaProxPagoTs, 0).UTC()
	}

	return doc
}
