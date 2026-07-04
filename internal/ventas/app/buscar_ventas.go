//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// Sortable column identifiers accepted in BuscarVentasInput.SortBy. Empty
// means default order (fecha_venta:desc when Q is empty, Meilisearch
// relevance when Q is set) — see outbound.VentasSearchQuery.SortBy.
const (
	ventaSortByFechaVenta    = "fecha_venta"
	ventaSortByPrecioTotal   = "precio_total"
	ventaSortByNombreCliente = "nombre_cliente"
)

// ErrVentaSortByInvalido is returned when BuscarVentasInput.SortBy is not one
// of the allowed columns.
var ErrVentaSortByInvalido = apperror.NewValidation(
	"venta_sort_by_invalido",
	"columna de ordenamiento no válida",
)

// ErrVentaSortOrderInvalido is returned when BuscarVentasInput.SortOrder is
// not "asc" or "desc".
var ErrVentaSortOrderInvalido = apperror.NewValidation(
	"venta_sort_order_invalido",
	"orden de clasificación no válido",
)

// ErrVentaCursorInvalido is returned when BuscarVentasInput.Cursor is
// non-empty and does not decode to a valid offset. Cursors are opaque to
// callers — this fails fast instead of silently resetting to the first page.
var ErrVentaCursorInvalido = apperror.NewValidation(
	"venta_cursor_invalido",
	"el cursor de paginación no es válido",
)

// defaultVentasSearchLimit and maxVentasSearchLimit bound
// BuscarVentasInput.Limit on the Meilisearch path, mirroring the [1,500]
// range the HTTP DTO already enforces (default:"50" maximum:"500"). Direct
// app-level callers (tests, future callers) get the same defaulting/clamping
// so the query sent to the index is never zero or unbounded.
const (
	defaultVentasSearchLimit = 50
	maxVentasSearchLimit     = 500
)

// offsetCursorPrefix tags the opaque offset cursor so it is trivially
// recognizable/debuggable and leaves room for future versioning, mirroring
// clientes' BuscarClientes cursor encoding.
const offsetCursorPrefix = "o"

// validVentaSortBy reports whether sortBy is empty (default) or one of the
// three columns exposed to the ventas search.
func validVentaSortBy(sortBy string) bool {
	switch sortBy {
	case "", ventaSortByFechaVenta, ventaSortByPrecioTotal, ventaSortByNombreCliente:
		return true
	default:
		return false
	}
}

// validVentaSortOrder reports whether sortOrder is empty (default) or one of
// "asc"/"desc".
func validVentaSortOrder(sortOrder string) bool {
	switch sortOrder {
	case "", "asc", "desc":
		return true
	default:
		return false
	}
}

// clampVentasSearchLimit defaults a non-positive limit to
// defaultVentasSearchLimit and caps an oversized one to maxVentasSearchLimit.
func clampVentasSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultVentasSearchLimit
	}
	if limit > maxVentasSearchLimit {
		return maxVentasSearchLimit
	}
	return limit
}

// encodeOffsetCursor encodes an integer offset as an opaque cursor string.
func encodeOffsetCursor(offset int) string {
	return offsetCursorPrefix + strconv.Itoa(offset)
}

// decodeOffsetCursor decodes an opaque cursor string back to an integer
// offset. An empty cursor decodes to offset 0. A malformed cursor returns
// ErrVentaCursorInvalido rather than silently resetting to the first page —
// a client that mangles a cursor should see a fast, explicit failure.
func decodeOffsetCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if len(cursor) < 2 || cursor[0] != offsetCursorPrefix[0] {
		return 0, ErrVentaCursorInvalido
	}
	n, err := strconv.Atoi(cursor[1:])
	if err != nil || n < 0 {
		return 0, ErrVentaCursorInvalido
	}
	return n, nil
}

// BuscarVentasInput carries the ventas search parameters: the Meilisearch
// full-text query, every Firebird-keyset-supported filter (used verbatim on
// the fallback path), the Meili-only filters (zona, vendedor email, precio
// range), sort, and offset-cursor pagination.
type BuscarVentasInput struct {
	// Q is the full-text search query. Empty means browse (no text search);
	// ignored entirely on the Firebird fallback path.
	Q string

	// Desde/Hasta/VendedorUsuarioID/ClienteID/TipoVenta/Situacion/
	// Sincronizacion/IncluirCanceladas mirror outbound.ListVentasFilters —
	// these are the filters the Firebird fallback path understands. Only
	// TipoVenta/Situacion/Sincronizacion/ClienteID/FechaDesde-Hasta/
	// IncluirCanceladas carry over to the Meilisearch query; VendedorUsuarioID
	// has no Meili equivalent (the index filters by VendedorEmail instead —
	// see outbound.VentaSearchDoc) so it is fallback-only.
	Desde             *time.Time
	Hasta             *time.Time
	VendedorUsuarioID *uuid.UUID
	ClienteID         *int
	TipoVenta         string
	Situacion         string
	Sincronizacion    string
	IncluirCanceladas bool

	// ZonaClienteID/VendedorEmail/PrecioMin/PrecioMax are Meili-only filters
	// with no Firebird-fallback equivalent — ignored on the fallback path.
	ZonaClienteID *int
	VendedorEmail *string
	PrecioMin     *decimal.Decimal
	PrecioMax     *decimal.Decimal

	// SortBy/SortOrder are Meili-only — ignored on the fallback path (which
	// always orders by FECHA_VENTA DESC, ID).
	SortBy    string
	SortOrder string

	// Cursor/Limit drive pagination on both paths: on the fallback path they
	// are passed straight through as outbound.ListParams; on the Meili path
	// Cursor decodes to an integer offset via decodeOffsetCursor.
	Cursor string
	Limit  int
}

// BuscarVentas returns a page of ventas matching in. When no VentaSearchIndex
// is wired (s.searchIndex == nil) it falls back to the existing Firebird
// keyset listing (ListarVentas), mapping only the filters the fallback
// understands and ignoring the Meili-only fields — this preserves today's
// GET /v2/ventas behavior exactly. When an index is wired, it queries
// Meilisearch for the matching ids, hydrates them from Firebird via
// FindByIDs, and reorders the result to match the search engine's ranking —
// Firebird's IN-query does not preserve id order.
func (s *Service) BuscarVentas(ctx context.Context, in BuscarVentasInput) (outbound.Page[*domain.Venta], error) {
	if !validVentaSortBy(in.SortBy) {
		return outbound.Page[*domain.Venta]{}, ErrVentaSortByInvalido
	}
	if !validVentaSortOrder(in.SortOrder) {
		return outbound.Page[*domain.Venta]{}, ErrVentaSortOrderInvalido
	}

	if s.searchIndex == nil {
		return s.ListarVentas(ctx, ListarVentasInput{
			Pagination: outbound.ListParams{Cursor: in.Cursor, PageSize: in.Limit},
			Filters: outbound.ListVentasFilters{
				Desde:             in.Desde,
				Hasta:             in.Hasta,
				VendedorUsuarioID: in.VendedorUsuarioID,
				ClienteID:         in.ClienteID,
				TipoVenta:         in.TipoVenta,
				Situacion:         in.Situacion,
				Sincronizacion:    in.Sincronizacion,
				IncluirCanceladas: in.IncluirCanceladas,
			},
		})
	}

	offset, err := decodeOffsetCursor(in.Cursor)
	if err != nil {
		return outbound.Page[*domain.Venta]{}, err
	}
	limit := clampVentasSearchLimit(in.Limit)

	q := outbound.VentasSearchQuery{
		Q:                 in.Q,
		ZonaClienteID:     in.ZonaClienteID,
		VendedorEmail:     in.VendedorEmail,
		ClienteID:         in.ClienteID,
		IncluirCanceladas: in.IncluirCanceladas,
		FechaDesde:        in.Desde,
		FechaHasta:        in.Hasta,
		PrecioMin:         in.PrecioMin,
		PrecioMax:         in.PrecioMax,
		SortBy:            in.SortBy,
		SortOrder:         in.SortOrder,
		Offset:            offset,
		Limit:             limit,
	}
	if in.TipoVenta != "" {
		tipoVenta := in.TipoVenta
		q.TipoVenta = &tipoVenta
	}
	if in.Situacion != "" {
		situacion := in.Situacion
		q.Situacion = &situacion
	}
	if in.Sincronizacion != "" {
		sincronizacion := in.Sincronizacion
		q.Sincronizacion = &sincronizacion
	}

	res, err := s.searchIndex.Buscar(ctx, q)
	if err != nil {
		return outbound.Page[*domain.Venta]{}, err
	}

	ventas, err := s.ventas.FindByIDs(ctx, res.IDs)
	if err != nil {
		return outbound.Page[*domain.Venta]{}, err
	}

	ordered := reorderVentasByIDs(ventas, res.IDs)

	var nextCursor string
	nextOffset := offset + limit
	if offset+len(res.IDs) < res.Total && nextOffset < outbound.MaxTotalHitsVentas {
		nextCursor = encodeOffsetCursor(nextOffset)
	}

	return outbound.Page[*domain.Venta]{Items: ordered, NextCursor: nextCursor}, nil
}

// reorderVentasByIDs re-emits the hydrated ventas in the exact order given by
// ids (the Meilisearch result order). An id with no hydrated venta — indexed
// but since deleted from Firebird — is silently dropped without disordering
// the rest.
func reorderVentasByIDs(ventas []*domain.Venta, ids []uuid.UUID) []*domain.Venta {
	byID := make(map[uuid.UUID]*domain.Venta, len(ventas))
	for _, v := range ventas {
		byID[v.ID()] = v
	}
	ordered := make([]*domain.Venta, 0, len(ids))
	for _, id := range ids {
		if v, ok := byID[id]; ok {
			ordered = append(ordered, v)
		}
	}
	return ordered
}
