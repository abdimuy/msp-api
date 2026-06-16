//nolint:misspell // Spanish vocabulary (clientes, directorio, buscar, pulso, segmento, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Fake DirectoryIndex for unit tests ──────────────────────────────────────

// fakeDirectoryIndexWithBuscar records the DirectorioQuery it receives and
// returns a configurable result or error.
type fakeDirectoryIndexWithBuscar struct {
	// Buscar configuration
	resultado outbound.DirectorioResultado
	buscarErr error
	captured  outbound.DirectorioQuery

	// Reconciliar configuration
	reconcileErr error
}

func (f *fakeDirectoryIndexWithBuscar) Buscar(_ context.Context, q outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	f.captured = q
	if f.buscarErr != nil {
		return outbound.DirectorioResultado{}, f.buscarErr
	}
	return f.resultado, nil
}

func (f *fakeDirectoryIndexWithBuscar) Reconciliar(_ context.Context, _ []outbound.DirectorioDoc) error {
	return f.reconcileErr
}

// newDoc builds a minimal DirectorioDoc for tests.
func newDoc(clienteID int, nombre string, saldo decimal.Decimal) outbound.DirectorioDoc {
	return outbound.DirectorioDoc{
		ClienteID: clienteID,
		Nombre:    nombre,
		Saldo:     saldo,
	}
}

// buildSvc is a helper that wires a Service with a given DirectoryIndex stub.
func buildSvc(di outbound.DirectoryIndex) *app.Service {
	return app.NewService(
		&fakeClientesRepo{},
		&fakeAnalyticsClient{},
		di,
		fixedClock{T: fixedTime},
	)
}

// ─── BuscarClientes: input→query translation ──────────────────────────────────

func TestBuscarClientes_TranslatesFiltersToQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	zona := 3
	cobrador := 7
	scoreMin := 50
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:             "garcia",
		ZonaClienteID: &zona,
		CobradorID:    &cobrador,
		ConSaldo:      true,
		Segmento:      "ACTIVO",
		EstadoPago:    "AL_CORRIENTE",
		ScoreMin:      &scoreMin,
		SortBy:        "saldo",
		SortOrder:     "desc",
		Pagination:    outbound.ListParams{PageSize: 25},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := di.captured
	if q.Q != "garcia" {
		t.Errorf("Q: got %q, want %q", q.Q, "garcia")
	}
	if q.ZonaClienteID == nil || *q.ZonaClienteID != 3 {
		t.Errorf("ZonaClienteID: got %v, want 3", q.ZonaClienteID)
	}
	if q.CobradorID == nil || *q.CobradorID != 7 {
		t.Errorf("CobradorID: got %v, want 7", q.CobradorID)
	}
	if !q.ConSaldo {
		t.Error("ConSaldo: expected true")
	}
	if q.Segmento != "ACTIVO" {
		t.Errorf("Segmento: got %q, want %q", q.Segmento, "ACTIVO")
	}
	if q.EstadoPago != "AL_CORRIENTE" {
		t.Errorf("EstadoPago: got %q, want %q", q.EstadoPago, "AL_CORRIENTE")
	}
	if q.ScoreMin == nil || *q.ScoreMin != 50 {
		t.Errorf("ScoreMin: got %v, want 50", q.ScoreMin)
	}
	if q.SortBy != "saldo" {
		t.Errorf("SortBy: got %q, want %q", q.SortBy, "saldo")
	}
	if q.SortOrder != "desc" {
		t.Errorf("SortOrder: got %q, want %q", q.SortOrder, "desc")
	}
	if q.Limit != 25 {
		t.Errorf("Limit: got %d, want 25", q.Limit)
	}
	if q.Offset != 0 {
		t.Errorf("Offset: got %d, want 0", q.Offset)
	}
}

func TestBuscarClientes_DefaultsLimitTo50WhenZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if di.captured.Limit != 50 {
		t.Errorf("Limit: got %d, want 50 (default)", di.captured.Limit)
	}
}

func TestBuscarClientes_DecodesOffsetFromCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{Cursor: "o100", PageSize: 50},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if di.captured.Offset != 100 {
		t.Errorf("Offset: got %d, want 100", di.captured.Offset)
	}
}

func TestBuscarClientes_EmptyCursorIsOffset0(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{Cursor: "", PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if di.captured.Offset != 0 {
		t.Errorf("Offset: got %d, want 0", di.captured.Offset)
	}
}

// ─── BuscarClientes: next-cursor computation ──────────────────────────────────

func TestBuscarClientes_NextCursor_WhenMoreResultsExist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	items := []outbound.DirectorioDoc{
		newDoc(1, "A", zero),
		newDoc(2, "B", zero),
	}
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{
			Items: items,
			Total: 100, // more pages available
		},
	}
	svc := buildSvc(di)

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCursor == "" {
		t.Error("expected NextCursor when more results exist, got empty string")
	}
	// cursor must encode offset=2 (0+2).
	if result.NextCursor != "o2" {
		t.Errorf("NextCursor: got %q, want %q", result.NextCursor, "o2")
	}
}

func TestBuscarClientes_NextCursor_AbsentOnLastPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	items := []outbound.DirectorioDoc{newDoc(1, "A", zero)}
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{
			Items: items,
			Total: 1, // no more pages
		},
	}
	svc := buildSvc(di)

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 50},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCursor != "" {
		t.Errorf("NextCursor: expected empty, got %q", result.NextCursor)
	}
}

func TestBuscarClientes_NextCursor_PaginationWithCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Simulated page 2: offset=50, limit=50, total=150 → cursor should be o100.
	items := make([]outbound.DirectorioDoc, 50)
	for i := range items {
		items[i] = newDoc(i+50, "X", zero)
	}
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: items, Total: 150},
	}
	svc := buildSvc(di)

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{Cursor: "o50", PageSize: 50},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCursor != "o100" {
		t.Errorf("NextCursor: got %q, want %q", result.NextCursor, "o100")
	}
}

func TestBuscarClientes_NextCursor_NilWhenAtMaxTotalHits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// offset+limit = 50000 = maxTotalHits → no next cursor even if total > offset+limit.
	items := make([]outbound.DirectorioDoc, 50)
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: items, Total: 60000},
	}
	svc := buildSvc(di)

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{Cursor: "o49950", PageSize: 50},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 49950+50 = 50000 = maxTotalHits → must NOT produce cursor.
	if result.NextCursor != "" {
		t.Errorf("NextCursor: expected empty at maxTotalHits boundary, got %q", result.NextCursor)
	}
}

// ─── BuscarClientes: items + facets pass-through ─────────────────────────────

func TestBuscarClientes_ItemsAndFacetsPassThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	docs := []outbound.DirectorioDoc{
		{ClienteID: 10, Nombre: "García Ramón", Saldo: decimal.NewFromInt(5000), TienePulso: true, Score: 80},
		{ClienteID: 20, Nombre: "López María", Saldo: zero},
	}
	facets := map[string]map[string]int{
		"zona_id":  {"1": 120, "2": 80},
		"segmento": {"ACTIVO": 50, "FRIO": 30},
	}
	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{
			Items:  docs,
			Facets: facets,
			Total:  2,
		},
	}
	svc := buildSvc(di)

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 50},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Items) != 2 {
		t.Fatalf("Items: got %d, want 2", len(result.Items))
	}
	if result.Items[0].ClienteID != 10 {
		t.Errorf("Items[0].ClienteID: got %d, want 10", result.Items[0].ClienteID)
	}
	if result.Items[1].ClienteID != 20 {
		t.Errorf("Items[1].ClienteID: got %d, want 20", result.Items[1].ClienteID)
	}

	if result.Facets == nil {
		t.Fatal("Facets: expected non-nil map")
	}
	if result.Facets["zona_id"]["1"] != 120 {
		t.Errorf("Facets[zona_id][1]: got %d, want 120", result.Facets["zona_id"]["1"])
	}
	if result.Facets["segmento"]["ACTIVO"] != 50 {
		t.Errorf("Facets[segmento][ACTIVO]: got %d, want 50", result.Facets["segmento"]["ACTIVO"])
	}
}

// ─── BuscarClientes: error handling ──────────────────────────────────────────

func TestBuscarClientes_InvalidSortBy_ReturnsValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc := buildSvc(&fakeDirectoryIndexWithBuscar{})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "telefono",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected validation error for unknown sort_by")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindValidation {
		t.Errorf("Kind: got %v, want KindValidation", appErr.Kind)
	}
	if appErr.Code != "sort_by_invalido" {
		t.Errorf("Code: got %q, want %q", appErr.Code, "sort_by_invalido")
	}
}

func TestBuscarClientes_MeilisearchUnavailable_Propagates503(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// dirIndex returns a KindServiceUnavailable apperror.
	unavailErr := apperror.NewServiceUnavailable(
		"directorio_search_unavailable",
		"el buscador de directorio no está disponible en este momento",
	)
	di := &fakeDirectoryIndexWithBuscar{buscarErr: unavailErr}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err == nil {
		t.Fatal("expected error when Meilisearch is unavailable")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindServiceUnavailable {
		t.Errorf("Kind: got %v, want KindServiceUnavailable", appErr.Kind)
	}
}

func TestBuscarClientes_MeilisearchInternalError_Propagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{buscarErr: errors.New("unexpected")}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── B2: TierRiesgo filter + new sort values ───────────────────────────────────

func TestBuscarClientes_TierRiesgo_ThreadedToQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		TierRiesgo: "CRITICO",
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if di.captured.TierRiesgo != "CRITICO" {
		t.Errorf("TierRiesgo: got %q, want %q", di.captured.TierRiesgo, "CRITICO")
	}
}

func TestBuscarClientes_SortByPuntualidad_IsValid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "puntualidad",
		SortOrder:  "asc",
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error for sort_by=puntualidad: %v", err)
	}
	if di.captured.SortBy != "puntualidad" {
		t.Errorf("SortBy: got %q, want %q", di.captured.SortBy, "puntualidad")
	}
}

func TestBuscarClientes_SortByProxPago_IsValid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	di := &fakeDirectoryIndexWithBuscar{
		resultado: outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0},
	}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "prox_pago",
		SortOrder:  "asc",
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error for sort_by=prox_pago: %v", err)
	}
	if di.captured.SortBy != "prox_pago" {
		t.Errorf("SortBy: got %q, want %q", di.captured.SortBy, "prox_pago")
	}
}
