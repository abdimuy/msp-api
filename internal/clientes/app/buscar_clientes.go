// Package app contains the clientes module's query services.
//
//nolint:misspell // Spanish vocabulary (clientes, directorio, buscar, pulso, etc.) per project convention.
package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

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

// Sortable column identifiers accepted in BuscarClientesInput.SortBy.
// Empty SortBy means "default order" (FTS rank on the search path, NOMBRE on the
// browse/global path).
const (
	sortByNombre     = "nombre"
	sortBySaldo      = "saldo"
	sortByZona       = "zona"
	sortByScore      = "score"
	sortBySegmento   = "segmento"
	sortByEstadoPago = "estado_pago"
	sortByRecencia   = "recencia"

	sortOrderAsc  = "asc"
	sortOrderDesc = "desc"
)

// Canonical analytics segmento / estado_pago string values, mirrored here as the
// sort-ordering vocabulary. These match analytics.ClientePulsoContract's
// Segmento/EstadoPago string contract (the analytics domain owns the enum; the
// clientes module consumes the flat string view via the contract, so it cannot
// import the analytics domain — these constants are the agreed wire vocabulary).
const (
	segLealPorLiquidar = "LEAL_POR_LIQUIDAR"
	segDormidoValioso  = "DORMIDO_VALIOSO"
	segActivo          = "ACTIVO"
	segNuevo           = "NUEVO"
	segFrio            = "FRIO"
	segPerdido         = "PERDIDO"

	epAlCorriente = "AL_CORRIENTE"
	epLiquidado   = "LIQUIDADO"
	epSinCredito  = "SIN_CREDITO"
	epAtrasado    = "ATRASADO"
	epMoroso      = "MOROSO"
)

// ErrSortByInvalido is returned when SortBy is not one of the allowed columns.
// The handler also enum-guards SortBy, so this is defence in depth.
var ErrSortByInvalido = apperror.NewValidation(
	"sort_by_invalido",
	"columna de ordenamiento no válida",
)

// pulseColumn reports whether a SortBy targets a pulse-derived field. Items
// without pulse (TienePulso=false) always sort LAST on these columns.
func pulseColumn(sortBy string) bool {
	switch sortBy {
	case sortByScore, sortBySegmento, sortByEstadoPago, sortByRecencia:
		return true
	default:
		return false
	}
}

// validSortBy reports whether sortBy is empty (default) or a known column.
func validSortBy(sortBy string) bool {
	switch sortBy {
	case "", sortByNombre, sortBySaldo, sortByZona,
		sortByScore, sortBySegmento, sortByEstadoPago, sortByRecencia:
		return true
	default:
		return false
	}
}

// segmentoOrdinal maps an analytics segmento to a sort ordinal. Lower ordinal =
// "earlier" in ascending order. The order encodes commercial priority, from the
// most actionable/valuable (lealtad por liquidar, dormido valioso) down to the
// least (perdido). Unknown segmentos sort after all known ones.
//
// Order: LEAL_POR_LIQUIDAR < DORMIDO_VALIOSO < ACTIVO < NUEVO < FRIO < PERDIDO.
func segmentoOrdinal(s string) int {
	switch s {
	case segLealPorLiquidar:
		return 0
	case segDormidoValioso:
		return 1
	case segActivo:
		return 2
	case segNuevo:
		return 3
	case segFrio:
		return 4
	case segPerdido:
		return 5
	default:
		return 6
	}
}

// estadoPagoOrdinal maps an analytics estado_pago to a sort ordinal ordered by
// solvency, from healthiest to worst. Lower ordinal = healthier (sorts first asc).
//
// Order: AL_CORRIENTE < LIQUIDADO < SIN_CREDITO < ATRASADO < MOROSO.
// Unknown states sort after all known ones.
func estadoPagoOrdinal(s string) int {
	switch s {
	case epAlCorriente:
		return 0
	case epLiquidado:
		return 1
	case epSinCredito:
		return 2
	case epAtrasado:
		return 3
	case epMoroso:
		return 4
	default:
		return 5
	}
}

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

	// SortBy selects the global sort column. Empty = default order (FTS rank on
	// the search path, NOMBRE on the browse/global path). Allowed values:
	// nombre, saldo, zona (native) and score, segmento, estado_pago, recencia
	// (pulse). Validated against the allowed set; unknown values return
	// ErrSortByInvalido.
	SortBy string
	// SortOrder is "asc" (default) or "desc". Any value other than "desc" is
	// treated as ascending.
	SortOrder string

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
// with analytics pulse data, filtered by segment/payment-state/score, and sorted
// GLOBALLY by a chosen column.
//
// # Three execution paths
//
// Search path (in.Q != ""):
//  1. Resolve up to searchCap ranked client IDs from the FTS index (or DB fallback).
//  2. Fetch those clients (≤ searchCap) from the repo, enrich with pulse, apply
//     pulse filters GLOBALLY (over the whole bounded search set).
//  3. Sort: by SortBy (+order) when set, otherwise by FTS relevance rank.
//  4. Apply in-app offset-cursor pagination over the sorted result.
//
// Default browse path (in.Q == "" AND SortBy == "" AND no pulse filter):
//  1. Delegate pagination to the repo (native cursor, ordered by nombre then clienteID).
//  2. Enrich the page with pulse (no pulse filter is active, so nothing is dropped).
//
// This keeps the fast, native-cursor default for the common "scroll the padrón"
// case unchanged.
//
// Global path (in.Q == "" AND (SortBy != "" OR any pulse filter active)):
//  1. Fetch ALL clients matching the native filters (no pagination).
//  2. Enrich the full set with pulse and apply pulse filters GLOBALLY.
//  3. Sort by SortBy (defaulting to nombre asc when only a pulse filter is set)
//     and SortOrder.
//  4. Apply in-app offset-cursor pagination over the sorted result.
//
// Both sorting and pulse filtering are therefore GLOBAL (over the whole matching
// set), not page-local.
//
// # Performance characteristic
//
// The global path fetches and enriches the FULL matching set on every request.
// Its cost is bounded by the native filters: with a zone/cobrador filter it is
// sub-second; the unfiltered global sort over the whole padrón is expensive
// (the repo's grouped saldo aggregation scans the full credit-importes table).
// A Fase-2 optimization would materialize the pulse columns alongside saldo so
// the database can ORDER BY directly and paginate at the DB level.
func (s *Service) BuscarClientes(ctx context.Context, in BuscarClientesInput) (outbound.Page[DirectorioClienteItem], error) {
	const source = "clientes.BuscarClientes"

	if !validSortBy(in.SortBy) {
		return outbound.Page[DirectorioClienteItem]{}, ErrSortByInvalido.WithSource(source)
	}

	if in.Q != "" {
		return s.buscarPorTexto(ctx, in, source)
	}
	if in.SortBy == "" && !hasPulsoFilter(in) {
		return s.browsePaginado(ctx, in, source)
	}
	return s.browseGlobal(ctx, in, source)
}

// hasPulsoFilter reports whether any pulse filter is active.
func hasPulsoFilter(in BuscarClientesInput) bool {
	return in.Segmento != "" || in.EstadoPago != "" || in.ScoreMin != nil
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

	// Sort: an explicit SortBy (global, over the bounded search set) takes
	// precedence; otherwise re-sort to the FTS relevance rank order. Items not in
	// rankIndex (filtered out by native filters) keep their relative order at the
	// end of the FTS-rank ordering.
	if in.SortBy != "" {
		sortByColumn(enriched, in.SortBy, in.SortOrder)
	} else {
		sortByRank(enriched, rankIndex)
	}

	return paginateOffset(enriched, in.Pagination.Cursor, in.Pagination.PageSize), nil
}

// browseGlobal handles the global path (in.Q == "" with SortBy or pulse filter):
// fetch ALL matching rows, enrich + pulse-filter GLOBALLY, sort, offset-paginate.
func (s *Service) browseGlobal(ctx context.Context, in BuscarClientesInput, source string) (outbound.Page[DirectorioClienteItem], error) {
	all, err := s.repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
		ZonaClienteID: in.ZonaClienteID,
		CobradorID:    in.CobradorID,
		ConSaldo:      in.ConSaldo,
	})
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"directorio_list_completo_failed",
			"error al listar el directorio completo de clientes",
		).WithSource(source).WithError(err)
	}

	enriched, err := s.enriquecerYFiltrar(ctx, all, in)
	if err != nil {
		return outbound.Page[DirectorioClienteItem]{}, apperror.NewInternal(
			"directorio_enrich_failed",
			"error al enriquecer el directorio con pulso",
		).WithSource(source).WithError(err)
	}

	// Default to nombre asc when only a pulse filter is active (SortBy empty).
	sortBy := in.SortBy
	if sortBy == "" {
		sortBy = sortByNombre
	}
	sortByColumn(enriched, sortBy, in.SortOrder)

	return paginateOffset(enriched, in.Pagination.Cursor, in.Pagination.PageSize), nil
}

// paginateOffset applies in-app offset-cursor pagination over a fully-ordered
// slice. A zero/negative pageSize falls back to 20.
func paginateOffset(items []DirectorioClienteItem, cursor string, pageSize int) outbound.Page[DirectorioClienteItem] {
	offset := decodeCursor(cursor)
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(items)
	if offset >= total {
		return outbound.Page[DirectorioClienteItem]{Items: []DirectorioClienteItem{}, NextCursor: ""}
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
		Items:      items[offset:end],
		NextCursor: nextCursor,
	}
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

// sortByColumn sorts items in place by the given column and order. The sort is
// STABLE (preserves the input order for equal keys — on the global/browse path
// the input is already nombre-ordered, giving a deterministic tiebreak).
//
// For pulse columns (score, segmento, estado_pago, recencia), items WITHOUT pulse
// (TienePulso=false) always sort LAST regardless of order. This is enforced ahead
// of the order flip so a "desc" sort still keeps no-pulse items at the very end.
func sortByColumn(items []DirectorioClienteItem, sortBy, sortOrder string) {
	desc := sortOrder == sortOrderDesc
	isPulse := pulseColumn(sortBy)

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]

		// Pulse columns: no-pulse items always last, independent of order.
		if isPulse {
			if a.TienePulso != b.TienePulso {
				return a.TienePulso // true (has pulse) sorts before false
			}
			if !a.TienePulso { // both lack pulse → preserve input order
				return false
			}
		}

		cmp := compareColumn(a, b, sortBy)
		if cmp == 0 {
			return false // stable: keep input order on ties
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// compareColumn returns -1, 0 or +1 comparing a and b on the given column in
// ascending sense. For pulse columns the caller has already guaranteed both items
// have pulse.
func compareColumn(a, b DirectorioClienteItem, sortBy string) int {
	switch sortBy {
	case sortByNombre:
		return strings.Compare(
			strings.ToLower(a.Cliente.Nombre()),
			strings.ToLower(b.Cliente.Nombre()),
		)
	case sortBySaldo:
		return a.SaldoTotal.Cmp(b.SaldoTotal)
	case sortByZona:
		return strings.Compare(a.Cliente.ZonaNombre(), b.Cliente.ZonaNombre())
	case sortByScore:
		return cmpInt(a.Pulso.Score, b.Pulso.Score)
	case sortByRecencia:
		return cmpInt(a.Pulso.RecenciaDias, b.Pulso.RecenciaDias)
	case sortBySegmento:
		return cmpInt(segmentoOrdinal(a.Pulso.Segmento), segmentoOrdinal(b.Pulso.Segmento))
	case sortByEstadoPago:
		return cmpInt(estadoPagoOrdinal(a.Pulso.EstadoPago), estadoPagoOrdinal(b.Pulso.EstadoPago))
	default:
		return 0
	}
}

// cmpInt returns -1, 0 or +1 for a vs b.
func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
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
