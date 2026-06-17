// Package app contains the clientes module's query services.
//
//nolint:misspell // Spanish vocabulary (clientes, directorio, buscar, etc.) per project convention.
package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Sortable column identifiers accepted in BuscarClientesInput.SortBy.
// Empty SortBy means "default order": nombre:asc on the browse path, or
// Meilisearch relevance when a text query is present.
const (
	sortByNombre       = "nombre"
	sortBySaldo        = "saldo"
	sortByZona         = "zona"
	sortByScore        = "score"
	sortBySegmento     = "segmento"
	sortByEstadoPago   = "estado_pago"
	sortByRecencia     = "recencia"
	sortByPuntualidad  = "puntualidad"   // maps to pct_pagos_a_tiempo
	sortByProxPago     = "prox_pago"     // maps to fecha_prox_pago_ts
	sortByScoreCredito = "score_credito" // maps to score_credito
)

// ErrSortByInvalido is returned when SortBy is not one of the allowed columns.
// The handler also enum-guards SortBy, so this is defence in depth.
var ErrSortByInvalido = apperror.NewValidation(
	"sort_by_invalido",
	"columna de ordenamiento no válida",
)

// validSortBy reports whether sortBy is empty (default) or a known column.
func validSortBy(sortBy string) bool {
	switch sortBy {
	case "", sortByNombre, sortBySaldo, sortByZona,
		sortByScore, sortBySegmento, sortByEstadoPago, sortByRecencia,
		sortByPuntualidad, sortByProxPago, sortByScoreCredito:
		return true
	default:
		return false
	}
}

// BuscarClientesInput groups the directory's search query, native filters,
// pulse filters, sort and pagination parameters.
type BuscarClientesInput struct {
	// Q is the full-text search query. Empty string activates browse mode.
	Q string
	// ZonaClienteID restricts to clients in a specific sales zone. Nil = no filter.
	ZonaClienteID *int
	// CobradorID restricts to clients assigned to a specific cobrador. Nil = no filter.
	CobradorID *int
	// ConSaldo when true restricts to clients whose outstanding balance > 0.
	ConSaldo bool

	// Pulse / analytics filters.

	// Segmento restricts to a specific analytics segment (exact match). Empty = no filter.
	Segmento string
	// EstadoPago restricts to a specific payment-solvency state (exact match). Empty = no filter.
	EstadoPago string
	// ScoreMin keeps only items whose pulse Score >= this value. Nil = no filter.
	ScoreMin *int
	// TierRiesgo restricts to a specific cobranza risk tier (exact match). Empty = no filter.
	TierRiesgo string
	// BandaCredito restricts to a specific credit-risk band (exact match). Empty = no filter.
	// Contado clients index banda_credito="" and won't match a non-empty filter.
	BandaCredito string

	// SortBy selects the sort column. Empty = default (nombre:asc browse, relevance search).
	// Allowed values: nombre, saldo, zona, score, segmento, estado_pago, recencia,
	// puntualidad, prox_pago, score_credito.
	SortBy string
	// SortOrder is "asc" (default) or "desc".
	SortOrder string

	Pagination outbound.ListParams
}

// BuscarClientesResultado carries the directory page, facets, and next-page cursor.
type BuscarClientesResultado struct {
	// Items is the current page of directory documents.
	Items []outbound.DirectorioDoc
	// Facets holds the per-attribute value counts from Meilisearch.
	// Keys are attribute names (e.g. "zona_id", "segmento"); inner keys are the
	// facet values, inner ints are the hit counts for this query.
	Facets map[string]map[string]int
	// NextCursor is the opaque cursor to pass for the next page. Empty string
	// means this is the last page.
	NextCursor string
}

// BuscarClientes returns a paginated directory of clients from the Meilisearch
// index, together with facet counts for zone, cobrador, segment, and payment state.
//
// All filtering, sorting, and pagination is delegated to Meilisearch via a single
// dirIndex.Buscar call. There is no SQL fallback: if the index is unavailable,
// the method returns an apperror with KindServiceUnavailable (HTTP 503).
//
// Pagination uses offset-based cursors encoded as "o<N>" strings.
func (s *Service) BuscarClientes(ctx context.Context, in BuscarClientesInput) (BuscarClientesResultado, error) {
	const source = "clientes.BuscarClientes"

	if !validSortBy(in.SortBy) {
		return BuscarClientesResultado{}, ErrSortByInvalido.WithSource(source)
	}

	limit := in.Pagination.PageSize
	if limit <= 0 {
		limit = 50
	}
	offset := decodeCursor(in.Pagination.Cursor)

	q := outbound.DirectorioQuery{
		Q:             in.Q,
		ZonaClienteID: in.ZonaClienteID,
		CobradorID:    in.CobradorID,
		ConSaldo:      in.ConSaldo,
		Segmento:      in.Segmento,
		EstadoPago:    in.EstadoPago,
		ScoreMin:      in.ScoreMin,
		TierRiesgo:    in.TierRiesgo,
		BandaCredito:  in.BandaCredito,
		SortBy:        in.SortBy,
		SortOrder:     in.SortOrder,
		Offset:        offset,
		Limit:         limit,
	}

	resultado, err := s.dirIndex.Buscar(ctx, q)
	if err != nil {
		return BuscarClientesResultado{}, err
	}

	// Compute next-page cursor: advance only when more results exist and we
	// have not reached the index's pagination cap.
	var nextCursor string
	nextOffset := offset + limit
	if nextOffset < resultado.Total && nextOffset < outbound.MaxTotalHitsDirectorio {
		nextCursor = encodeCursor(nextOffset)
	}

	return BuscarClientesResultado{
		Items:      resultado.Items,
		Facets:     resultado.Facets,
		NextCursor: nextCursor,
	}, nil
}

// encodeCursor encodes an integer offset as an opaque cursor string.
// The "o" prefix makes it debuggable and leaves room for versioning.
func encodeCursor(offset int) string {
	return fmt.Sprintf("o%d", offset)
}

// decodeCursor decodes an opaque cursor string back to an integer offset.
// An empty or malformed cursor is treated as offset 0.
func decodeCursor(cursor string) int {
	if cursor == "" {
		return 0
	}
	if len(cursor) > 1 && cursor[0] == 'o' {
		n, err := strconv.Atoi(cursor[1:])
		if err == nil && n >= 0 {
			return n
		}
	}
	return 0
}
