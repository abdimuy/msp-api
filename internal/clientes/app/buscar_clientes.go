// Package app contains the clientes module's query services.
//
//nolint:misspell // Spanish vocabulary (clientes, directorio, buscar, pulso, etc.) per project convention.
package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// searchCap is the maximum number of client IDs resolved by the search path
// (both the FTS index and the basic SQL fallback). Results beyond this cap are
// silently truncated before re-fetching and enriching.
const searchCap = 200

// BuscarClientesInput groups the directory's search query, native filters,
// pulse filters and pagination parameters.
type BuscarClientesInput struct {
	// Q is the full-text search query. Empty string activates the browse path
	// (no text search, native cursor pagination).
	Q string
	// ZonaClienteID restricts to clients in a specific sales zone. Nil = no filter.
	ZonaClienteID *int
	// CobradorID restricts to clients assigned to a specific cobrador. Nil = no filter.
	CobradorID *int
	// ConSaldo when true restricts to clients whose outstanding balance > 0.
	ConSaldo bool

	// Pulse filters — applied in-app after fetching and enriching with analytics.
	// When any pulse filter is set, items with TienePulso=false are excluded.

	// Segmento restricts to a specific analytics segment (exact match). Empty = no filter.
	Segmento string
	// EstadoPago restricts to a specific payment-solvency state (exact match). Empty = no filter.
	EstadoPago string
	// ScoreMin keeps only items whose pulse Score >= this value. Nil = no filter.
	ScoreMin *int

	Pagination outbound.ListParams
}

// DirectorioClienteItem is one directory row: native identity + saldo + analytics pulse.
type DirectorioClienteItem struct {
	Cliente    *domain.Cliente
	SaldoTotal decimal.Decimal
	Pulso      analytics.ClientePulsoContract // zero value when TienePulso is false
	TienePulso bool
}

// BuscarClientes returns a paginated directory of clients, optionally enriched
// with analytics pulse data and filtered by segment/payment-state/score.
//
// # Two execution paths
//
// Search path (in.Q != ""):
//  1. Resolve up to searchCap ranked client IDs from the FTS index (or DB fallback).
//  2. Fetch those clients from the repo, enrich with pulse, apply pulse filters.
//  3. Re-sort to the FTS relevance rank order.
//  4. Apply in-app offset-cursor pagination over the sorted result.
//
// Browse path (in.Q == ""):
//  1. Delegate pagination to the repo (native cursor, ordered by nombre then clienteID).
//  2. Enrich the page with pulse and apply pulse filters.
//
// # Phase-1 simplification
//
// Pulse filters in the browse path are applied PAGE-LOCAL: the repo returns a
// full page and then items that do not pass the pulse filter are dropped. The
// resulting page may therefore contain fewer than PageSize items when pulse
// filters are active. "Load more" via NextCursor still works correctly. A
// future phase may push pulse filters into the repo or use a pre-computed view.
func (s *Service) BuscarClientes(ctx context.Context, in BuscarClientesInput) (outbound.Page[DirectorioClienteItem], error) {
	const source = "clientes.BuscarClientes"

	if in.Q != "" {
		return s.buscarPorTexto(ctx, in, source)
	}
	return s.browsePaginado(ctx, in, source)
}

// buscarPorTexto handles the search path (in.Q != "").
func (s *Service) buscarPorTexto(ctx context.Context, in BuscarClientesInput, source string) (outbound.Page[DirectorioClienteItem], error) {
	// Step 1: resolve ranked client IDs.
	var ids []int
	var err error
	if s.search.EstaListo() {
		ids, err = s.search.Buscar(ctx, in.Q, searchCap)
	} else {
		ids, err = s.repo.BuscarClienteIDsBasico(ctx, in.Q, searchCap)
	}
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"buscar_clientes_ids_failed",
			"error al buscar clientes por texto",
		).WithSource(source).WithError(err)
	}

	// Step 2: zero results → empty page.
	if len(ids) == 0 {
		return outbound.Page[DirectorioClienteItem]{Items: []DirectorioClienteItem{}, NextCursor: ""}, nil
	}

	// Build rank map before fetching (preserves the FTS order).
	rankIndex := make(map[int]int, len(ids))
	for i, id := range ids {
		rankIndex[id] = i
	}

	// Step 3: fetch all resolved clients in one repo call.
	rawPage, err := s.repo.ListarDirectorio(ctx, outbound.ListParams{PageSize: len(ids)}, outbound.FiltroDirectorio{
		ClienteIDs:    ids,
		ZonaClienteID: in.ZonaClienteID,
		CobradorID:    in.CobradorID,
		ConSaldo:      in.ConSaldo,
	})
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"buscar_clientes_fetch_failed",
			"error al obtener clientes del directorio",
		).WithSource(source).WithError(err)
	}

	// Step 4: enrich + apply pulse filters.
	enriched, err := s.enriquecerYFiltrar(ctx, rawPage.Items, in)
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"buscar_clientes_enrich_failed",
			"error al enriquecer clientes con pulso",
		).WithSource(source).WithError(err)
	}

	// Re-sort to the FTS relevance rank order. Items not in rankIndex (filtered
	// out by native filters) keep their relative order at the end.
	sortByRank(enriched, rankIndex)

	// Apply in-app offset-cursor pagination.
	offset := decodeCursor(in.Pagination.Cursor)
	pageSize := in.Pagination.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(enriched)
	if offset >= total {
		return outbound.Page[DirectorioClienteItem]{Items: []DirectorioClienteItem{}, NextCursor: ""}, nil
	}
	end := offset + pageSize
	var nextCursor string
	if end < total {
		nextCursor = encodeCursor(end)
	}
	if end > total {
		end = total
	}

	return outbound.Page[DirectorioClienteItem]{
		Items:      enriched[offset:end],
		NextCursor: nextCursor,
	}, nil
}

// browsePaginado handles the browse path (in.Q == "").
func (s *Service) browsePaginado(ctx context.Context, in BuscarClientesInput, source string) (outbound.Page[DirectorioClienteItem], error) {
	// Step 1: delegate pagination to repo (native cursor, ordered by nombre then clienteID).
	rawPage, err := s.repo.ListarDirectorio(ctx, in.Pagination, outbound.FiltroDirectorio{
		ZonaClienteID: in.ZonaClienteID,
		CobradorID:    in.CobradorID,
		ConSaldo:      in.ConSaldo,
	})
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"directorio_list_failed",
			"error al listar el directorio de clientes",
		).WithSource(source).WithError(err)
	}

	// Step 2: enrich with pulse + apply pulse filters (page-local — Phase 1).
	enriched, err := s.enriquecerYFiltrar(ctx, rawPage.Items, in)
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"directorio_enrich_failed",
			"error al enriquecer el directorio con pulso",
		).WithSource(source).WithError(err)
	}

	// Carry through the repo's NextCursor so callers can page further.
	return outbound.Page[DirectorioClienteItem]{
		Items:      enriched,
		NextCursor: rawPage.NextCursor,
	}, nil
}

// enriquecerYFiltrar fetches analytics pulses for the given directory items in
// one batched call, builds DirectorioClienteItem values, and applies any pulse
// filters from in (Segmento, EstadoPago, ScoreMin).
//
// Items with TienePulso=false are excluded when ANY pulse filter is active
// because they cannot satisfy the filter. Items with TienePulso=false are
// included when no pulse filter is set.
func (s *Service) enriquecerYFiltrar(ctx context.Context, items []outbound.DirectorioItem, in BuscarClientesInput) ([]DirectorioClienteItem, error) {
	hasPulsoFilter := in.Segmento != "" || in.EstadoPago != "" || in.ScoreMin != nil

	// Collect IDs for the batched pulse fetch.
	ids := make([]int, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Cliente.ClienteID())
	}

	var pulsos map[int]analytics.ClientePulsoContract
	if len(ids) > 0 {
		var err error
		pulsos, err = s.analytics.ObtenerPulsos(ctx, ids)
		if err != nil {
			return nil, err
		}
	} else {
		pulsos = map[int]analytics.ClientePulsoContract{}
	}

	result := make([]DirectorioClienteItem, 0, len(items))
	for _, it := range items {
		pulso, tienePulso := pulsos[it.Cliente.ClienteID()]

		// Exclude no-pulse items when any pulse filter is active.
		if hasPulsoFilter && !tienePulso {
			continue
		}

		// Apply pulse filters.
		if in.Segmento != "" && pulso.Segmento != in.Segmento {
			continue
		}
		if in.EstadoPago != "" && pulso.EstadoPago != in.EstadoPago {
			continue
		}
		if in.ScoreMin != nil && pulso.Score < *in.ScoreMin {
			continue
		}

		result = append(result, DirectorioClienteItem{
			Cliente:    it.Cliente,
			SaldoTotal: it.SaldoTotal,
			Pulso:      pulso,
			TienePulso: tienePulso,
		})
	}
	return result, nil
}

// sortByRank sorts items to match the FTS relevance rank order given in
// rankIndex (map of clienteID → rank position). Items whose clienteID is not
// present in rankIndex (filtered out by native repo filters) are placed after
// ranked items in their original relative order.
func sortByRank(items []DirectorioClienteItem, rankIndex map[int]int) {
	// Stable insertion sort: small N (≤ searchCap) so O(n²) is acceptable.
	for i := 1; i < len(items); i++ {
		key := items[i]
		keyRank, keyHasRank := rankIndex[key.Cliente.ClienteID()]
		j := i - 1
		for j >= 0 {
			jRank, jHasRank := rankIndex[items[j].Cliente.ClienteID()]
			var shouldSwap bool
			switch {
			case keyHasRank && jHasRank:
				shouldSwap = keyRank < jRank
			case keyHasRank && !jHasRank:
				shouldSwap = true // ranked items before unranked
			default:
				shouldSwap = false
			}
			if !shouldSwap {
				break
			}
			items[j+1] = items[j]
			j--
		}
		items[j+1] = key
	}
}

// encodeCursor encodes an integer offset as an opaque cursor string.
// The format is a prefixed decimal string ("o<N>") — the "o" prefix keeps the
// cursor debuggable and leaves room for lightweight format versioning; decodeCursor
// treats any value without the prefix as offset 0.
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
