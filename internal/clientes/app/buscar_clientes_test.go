//nolint:misspell // Spanish vocabulary (clientes, directorio, buscar, pulso, segmento, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// newDirItem builds a DirectorioItem for tests.
func newDirItem(clienteID int, nombre string, saldo decimal.Decimal) outbound.DirectorioItem {
	return outbound.DirectorioItem{
		Cliente:    newCliente(clienteID, nombre),
		SaldoTotal: saldo,
	}
}

// newPulso builds a ClientePulsoContract for tests.
func newPulso(clienteID, score int, segmento, estadoPago string) analytics.ClientePulsoContract {
	return analytics.ClientePulsoContract{
		ClienteID:  clienteID,
		Score:      score,
		Segmento:   segmento,
		EstadoPago: estadoPago,
	}
}

// ─── Search path ─────────────────────────────────────────────────────────────

func TestBuscarClientes_SearchPath_IndexListo_UsaBuscar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Index ready — returns IDs in relevance order [3, 1, 2].
	idx := &fakeSearchIndex{ready: true, ids: []int{3, 1, 2}}
	c1 := newDirItem(1, "Ana", zero)
	c2 := newDirItem(2, "Beto", zero)
	c3 := newDirItem(3, "Carlos", zero)
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: c1.Cliente, 2: c2.Cliente, 3: c3.Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: []outbound.DirectorioItem{c1, c2, c3}},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})
	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "carlos",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.lastQuery != "carlos" {
		t.Errorf("expected query carlos forwarded to index, got %q", idx.lastQuery)
	}
	// Results must be re-sorted to FTS rank: 3, 1, 2.
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 3 {
		t.Errorf("expected first item clienteID=3 (rank 0), got %d", result.Items[0].Cliente.ClienteID())
	}
	if result.Items[1].Cliente.ClienteID() != 1 {
		t.Errorf("expected second item clienteID=1 (rank 1), got %d", result.Items[1].Cliente.ClienteID())
	}
	if result.Items[2].Cliente.ClienteID() != 2 {
		t.Errorf("expected third item clienteID=2 (rank 2), got %d", result.Items[2].Cliente.ClienteID())
	}
	// The FTS-ranked ids must be forwarded to the repo as the ClienteIDs filter,
	// so the directory fetch is scoped to the search hits.
	if len(repo.lastFiltro.ClienteIDs) != 3 {
		t.Errorf("expected 3 ClienteIDs forwarded to repo, got %v", repo.lastFiltro.ClienteIDs)
	}
}

func TestBuscarClientes_SearchPath_IndexError_Propaga(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Index ready but Buscar fails (FTS transport error) → wrapped internal error.
	idx := &fakeSearchIndex{ready: true, busErr: errors.New("fts down")}
	repo := &fakeClientesRepo{}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})
	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "garcia",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error when the search index fails")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Code != "buscar_clientes_ids_failed" {
		t.Errorf("expected code buscar_clientes_ids_failed, got %q", appErr.Code)
	}
}

func TestBuscarClientes_SearchPath_IndexNoListo_UsaFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Index NOT ready — should call BuscarClienteIDsBasico.
	idx := &fakeSearchIndex{ready: false}
	c1 := newDirItem(1, "Ana", zero)
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: c1.Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: []outbound.DirectorioItem{c1}},
		basicIDs:    []int{1},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})
	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "ana",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastBasicQ != "ana" {
		t.Errorf("expected basic search query ana, got %q", repo.lastBasicQ)
	}
	if len(result.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(result.Items))
	}
}

func TestBuscarClientes_SearchPath_ZeroIDs_EmptyPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{}}
	repo := &fakeClientesRepo{clienteByID: map[int]*domain.Cliente{}}
	anl := &fakeAnalyticsClient{}

	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})
	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "xyz",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 0 {
		t.Errorf("expected empty page, got %d items", len(result.Items))
	}
	if result.NextCursor != "" {
		t.Errorf("expected empty NextCursor, got %q", result.NextCursor)
	}
}

func TestBuscarClientes_SearchPath_OffsetCursorPagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 4 IDs in rank order [10, 20, 30, 40], page size 2.
	idx := &fakeSearchIndex{ready: true, ids: []int{10, 20, 30, 40}}
	items := []outbound.DirectorioItem{
		newDirItem(10, "A", zero),
		newDirItem(20, "B", zero),
		newDirItem(30, "C", zero),
		newDirItem(40, "D", zero),
	}
	clients := map[int]*domain.Cliente{}
	for _, it := range items {
		clients[it.Cliente.ClienteID()] = it.Cliente
	}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	// First page.
	page1, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "q",
		Pagination: outbound.ListParams{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("expected 2 items on first page, got %d", len(page1.Items))
	}
	if page1.Items[0].Cliente.ClienteID() != 10 {
		t.Errorf("expected first item=10, got %d", page1.Items[0].Cliente.ClienteID())
	}
	if page1.NextCursor == "" {
		t.Error("expected NextCursor for second page")
	}

	// Second page using the cursor.
	page2, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "q",
		Pagination: outbound.ListParams{Cursor: page1.NextCursor, PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error on second page: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("expected 2 items on second page, got %d", len(page2.Items))
	}
	if page2.Items[0].Cliente.ClienteID() != 30 {
		t.Errorf("expected first item on page2=30, got %d", page2.Items[0].Cliente.ClienteID())
	}
	if page2.NextCursor != "" {
		t.Errorf("expected empty NextCursor on last page, got %q", page2.NextCursor)
	}
}

func TestBuscarClientes_SearchPath_PulsoFiltersSegmento(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1, 2, 3}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{}
	for _, it := range items {
		clients[it.Cliente.ClienteID()] = it.Cliente
	}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			1: newPulso(1, 70, "EN_RIESGO", "AL_DIA"),
			2: newPulso(2, 50, "DORMIDO", "AL_DIA"),
			// 3 has no pulse row → excluded when Segmento filter is set
		},
	}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		Segmento:   "EN_RIESGO",
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item after segmento filter, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 1 {
		t.Errorf("expected clienteID=1, got %d", result.Items[0].Cliente.ClienteID())
	}
}

func TestBuscarClientes_SearchPath_PulsoFiltersEstadoPago(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1, 2}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
	}
	clients := map[int]*domain.Cliente{1: items[0].Cliente, 2: items[1].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			1: newPulso(1, 60, "ACTIVO", "AL_DIA"),
			2: newPulso(2, 40, "ACTIVO", "ATRASADO"),
		},
	}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		EstadoPago: "AL_DIA",
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 1 {
		t.Errorf("expected clienteID=1, got %d", result.Items[0].Cliente.ClienteID())
	}
}

func TestBuscarClientes_SearchPath_PulsoFiltersScoreMin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1, 2, 3}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{1: items[0].Cliente, 2: items[1].Cliente, 3: items[2].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			1: newPulso(1, 80, "ACTIVO", "AL_DIA"),
			2: newPulso(2, 50, "EN_RIESGO", "ATRASADO"),
			// 3 has no pulse → excluded with any pulse filter active
		},
	}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		ScoreMin:   ptr(70),
		Pagination: outbound.ListParams{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item with score>=70, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 1 {
		t.Errorf("expected clienteID=1, got %d", result.Items[0].Cliente.ClienteID())
	}
}

func TestBuscarClientes_SearchPath_NoPulsoFilter_IncludesNoPulsoItems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1, 2}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero), // no pulse row
	}
	clients := map[int]*domain.Cliente{1: items[0].Cliente, 2: items[1].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			1: newPulso(1, 70, "ACTIVO", "AL_DIA"),
			// 2 has no row
		},
	}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		Pagination: outbound.ListParams{PageSize: 20},
		// No pulse filters → include no-pulse items
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items (including no-pulse), got %d", len(result.Items))
	}
	// The no-pulse item must have TienePulso=false.
	for _, it := range result.Items {
		if it.Cliente.ClienteID() == 2 && it.TienePulso {
			t.Error("expected TienePulso=false for clienteID=2")
		}
	}
}

// ─── Browse path ─────────────────────────────────────────────────────────────

func TestBuscarClientes_BrowsePath_EnriquecidoYNextCursorCarryThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	items := []outbound.DirectorioItem{
		newDirItem(10, "Ana", decimal.NewFromInt(500)),
		newDirItem(20, "Beto", zero),
	}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{10: items[0].Cliente, 20: items[1].Cliente},
		dirPage: outbound.Page[outbound.DirectorioItem]{
			Items:      items,
			NextCursor: "next-page-cursor",
		},
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			10: newPulso(10, 75, "EN_RIESGO", "AL_DIA"),
		},
	}
	idx := &fakeSearchIndex{ready: false}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		Pagination: outbound.ListParams{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.NextCursor != "next-page-cursor" {
		t.Errorf("expected NextCursor next-page-cursor, got %q", result.NextCursor)
	}
	// Item 10 has pulse, item 20 does not.
	for _, it := range result.Items {
		switch it.Cliente.ClienteID() {
		case 10:
			if !it.TienePulso {
				t.Error("expected TienePulso=true for clienteID=10")
			}
			if it.Pulso.Score != 75 {
				t.Errorf("expected Score 75, got %d", it.Pulso.Score)
			}
		case 20:
			if it.TienePulso {
				t.Error("expected TienePulso=false for clienteID=20")
			}
		}
	}
}

// When a pulse filter is active on the browse path, the service now takes the
// GLOBAL path: it fetches the FULL matching set via ListarDirectorioCompleto,
// applies the pulse filter globally, and offset-paginates. The filter is no
// longer page-local.
func TestBuscarClientes_GlobalPath_PulsoFilterGlobal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Full set of 3; only 1 matches the pulse filter.
	all := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{1: all[0].Cliente, 2: all[1].Cliente, 3: all[2].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirCompleto: all,
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{
			1: newPulso(1, 80, "EN_RIESGO", "AL_DIA"),
			2: newPulso(2, 30, "DORMIDO", "ATRASADO"),
			// 3 has no pulse → excluded (pulse filter active)
		},
	}
	svc := app.NewService(repo, anl, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		Segmento:   "EN_RIESGO",
		Pagination: outbound.ListParams{PageSize: 3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The global path must have been taken (ListarDirectorioCompleto called).
	if !repo.listarComplCalled {
		t.Error("expected ListarDirectorioCompleto to be called on the global path")
	}
	if repo.listarDirCalled {
		t.Error("expected ListarDirectorio NOT to be called on the global path")
	}
	// Only item 1 passes the filter.
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item after global pulse filter, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 1 {
		t.Errorf("expected clienteID=1, got %d", result.Items[0].Cliente.ClienteID())
	}
	// No further pages: the matching set has only 1 item.
	if result.NextCursor != "" {
		t.Errorf("expected empty NextCursor, got %q", result.NextCursor)
	}
}

func TestBuscarClientes_BrowsePath_RepoError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		dirErr:      errors.New("connection reset"),
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Kind != apperror.KindInternal {
		t.Errorf("expected KindInternal, got %v", appErr.Kind)
	}
	if appErr.Source != "clientes.BuscarClientes" {
		t.Errorf("expected source clientes.BuscarClientes, got %q", appErr.Source)
	}
}

// ─── Global path: sort + path selection ──────────────────────────────────────

// dirItemZona builds a DirectorioItem with a zona name set (for zona sort tests).
func dirItemZona(clienteID int, nombre, zona string, saldo decimal.Decimal) outbound.DirectorioItem {
	c := domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID:  clienteID,
		Nombre:     nombre,
		ZonaNombre: zona,
	})
	return outbound.DirectorioItem{Cliente: c, SaldoTotal: saldo}
}

// idsOf extracts the cliente IDs from a result page in order.
func idsOf(items []app.DirectorioClienteItem) []int {
	out := make([]int, 0, len(items))
	for _, it := range items {
		out = append(out, it.Cliente.ClienteID())
	}
	return out
}

// pulseWith builds a fully-specified pulse with score, recencia, segmento, estado.
func pulseWith(clienteID, score, recencia int, segmento, estadoPago string) analytics.ClientePulsoContract {
	return analytics.ClientePulsoContract{
		ClienteID:    clienteID,
		Score:        score,
		RecenciaDias: recencia,
		Segmento:     segmento,
		EstadoPago:   estadoPago,
	}
}

func TestBuscarClientes_GlobalPath_TakenWhenSortBySet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	all := []outbound.DirectorioItem{newDirItem(1, "A", zero)}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: all[0].Cliente},
		dirCompleto: all,
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		SortBy:     "nombre",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.listarComplCalled {
		t.Error("expected ListarDirectorioCompleto called when SortBy is set")
	}
	if repo.listarDirCalled {
		t.Error("expected ListarDirectorio NOT called when SortBy is set")
	}
}

func TestBuscarClientes_BrowsePath_TakenWhenNoSortNoFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	items := []outbound.DirectorioItem{newDirItem(1, "A", zero)}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: items[0].Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.listarDirCalled {
		t.Error("expected ListarDirectorio (native cursor) called on the default browse path")
	}
	if repo.listarComplCalled {
		t.Error("expected ListarDirectorioCompleto NOT called on the default browse path")
	}
}

func TestBuscarClientes_InvalidSortBy_ReturnsValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc := app.NewService(&fakeClientesRepo{}, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})
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
		t.Errorf("expected KindValidation, got %v", appErr.Kind)
	}
	if appErr.Code != "sort_by_invalido" {
		t.Errorf("expected code sort_by_invalido, got %q", appErr.Code)
	}
}

// sortCase drives the native-column sort table test.
func TestBuscarClientes_GlobalPath_NativeSorts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sortBy  string
		order   string
		all     []outbound.DirectorioItem
		wantIDs []int
	}{
		{
			name:   "nombre asc case-insensitive",
			sortBy: "nombre", order: "asc",
			all: []outbound.DirectorioItem{
				newDirItem(1, "carlos", zero),
				newDirItem(2, "Ana", zero),
				newDirItem(3, "Beto", zero),
			},
			wantIDs: []int{2, 3, 1}, // Ana, Beto, carlos
		},
		{
			name:   "nombre desc",
			sortBy: "nombre", order: "desc",
			all: []outbound.DirectorioItem{
				newDirItem(1, "carlos", zero),
				newDirItem(2, "Ana", zero),
				newDirItem(3, "Beto", zero),
			},
			wantIDs: []int{1, 3, 2},
		},
		{
			name:   "saldo asc",
			sortBy: "saldo", order: "asc",
			all: []outbound.DirectorioItem{
				newDirItem(1, "A", decimal.NewFromInt(300)),
				newDirItem(2, "B", decimal.NewFromInt(100)),
				newDirItem(3, "C", decimal.NewFromInt(200)),
			},
			wantIDs: []int{2, 3, 1},
		},
		{
			name:   "saldo desc",
			sortBy: "saldo", order: "desc",
			all: []outbound.DirectorioItem{
				newDirItem(1, "A", decimal.NewFromInt(300)),
				newDirItem(2, "B", decimal.NewFromInt(100)),
				newDirItem(3, "C", decimal.NewFromInt(200)),
			},
			wantIDs: []int{1, 3, 2},
		},
		{
			name:   "zona asc",
			sortBy: "zona", order: "asc",
			all: []outbound.DirectorioItem{
				dirItemZona(1, "A", "Norte", zero),
				dirItemZona(2, "B", "Centro", zero),
				dirItemZona(3, "C", "Sur", zero),
			},
			wantIDs: []int{2, 1, 3}, // Centro, Norte, Sur
		},
		{
			name:   "zona desc",
			sortBy: "zona", order: "desc",
			all: []outbound.DirectorioItem{
				dirItemZona(1, "A", "Norte", zero),
				dirItemZona(2, "B", "Centro", zero),
				dirItemZona(3, "C", "Sur", zero),
			},
			wantIDs: []int{3, 1, 2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			clients := map[int]*domain.Cliente{}
			for _, it := range tc.all {
				clients[it.Cliente.ClienteID()] = it.Cliente
			}
			repo := &fakeClientesRepo{clienteByID: clients, dirCompleto: tc.all}
			svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

			result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
				SortBy:     tc.sortBy,
				SortOrder:  tc.order,
				Pagination: outbound.ListParams{PageSize: 10},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := idsOf(result.Items)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("expected %v, got %v", tc.wantIDs, got)
			}
			for i := range got {
				if got[i] != tc.wantIDs[i] {
					t.Fatalf("expected order %v, got %v", tc.wantIDs, got)
				}
			}
		})
	}
}

func TestBuscarClientes_GlobalPath_PulseSorts_NullLast(t *testing.T) {
	t.Parallel()

	// IDs 1,2,3 have pulse; ID 4 has no pulse → must sort LAST in both orders.
	all := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
		newDirItem(4, "D", zero), // no pulse
	}
	clients := map[int]*domain.Cliente{}
	for _, it := range all {
		clients[it.Cliente.ClienteID()] = it.Cliente
	}
	pulsos := map[int]analytics.ClientePulsoContract{
		1: pulseWith(1, 90, 5, "ACTIVO", "AL_CORRIENTE"),
		2: pulseWith(2, 50, 30, "DORMIDO_VALIOSO", "ATRASADO"),
		3: pulseWith(3, 70, 10, "LEAL_POR_LIQUIDAR", "MOROSO"),
	}

	cases := []struct {
		name    string
		sortBy  string
		order   string
		wantIDs []int // 4 (no pulse) always last
	}{
		{"score asc", "score", "asc", []int{2, 3, 1, 4}},
		{"score desc", "score", "desc", []int{1, 3, 2, 4}},
		{"recencia asc", "recencia", "asc", []int{1, 3, 2, 4}},
		{"recencia desc", "recencia", "desc", []int{2, 3, 1, 4}},
		// segmento ordinal: LEAL_POR_LIQUIDAR(0) < DORMIDO_VALIOSO(1) < ACTIVO(2)
		{"segmento asc", "segmento", "asc", []int{3, 2, 1, 4}},
		{"segmento desc", "segmento", "desc", []int{1, 2, 3, 4}},
		// estado ordinal: AL_CORRIENTE(0) < ATRASADO(3) < MOROSO(4)
		{"estado_pago asc", "estado_pago", "asc", []int{1, 2, 3, 4}},
		{"estado_pago desc", "estado_pago", "desc", []int{3, 2, 1, 4}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repo := &fakeClientesRepo{clienteByID: clients, dirCompleto: all}
			anl := &fakeAnalyticsClient{pulsos: pulsos}
			svc := app.NewService(repo, anl, &fakeSearchIndex{}, fixedClock{T: fixedTime})

			result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
				SortBy:     tc.sortBy,
				SortOrder:  tc.order,
				Pagination: outbound.ListParams{PageSize: 10},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := idsOf(result.Items)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("expected %v, got %v", tc.wantIDs, got)
			}
			for i := range got {
				if got[i] != tc.wantIDs[i] {
					t.Fatalf("expected order %v, got %v", tc.wantIDs, got)
				}
			}
			// No-pulse item (4) must be last in BOTH orders.
			if got[len(got)-1] != 4 {
				t.Errorf("expected no-pulse clienteID=4 to sort last, got order %v", got)
			}
		})
	}
}

func TestBuscarClientes_GlobalPath_OffsetPagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 4 items sorted by nombre asc → A,B,C,D (ids 1..4); page size 2.
	all := []outbound.DirectorioItem{
		newDirItem(3, "C", zero),
		newDirItem(1, "A", zero),
		newDirItem(4, "D", zero),
		newDirItem(2, "B", zero),
	}
	clients := map[int]*domain.Cliente{}
	for _, it := range all {
		clients[it.Cliente.ClienteID()] = it.Cliente
	}
	repo := &fakeClientesRepo{clienteByID: clients, dirCompleto: all}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	page1, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "nombre",
		SortOrder:  "asc",
		Pagination: outbound.ListParams{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idsOf(page1.Items); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("expected page1 [1,2], got %v", idsOf(page1.Items))
	}
	if page1.NextCursor == "" {
		t.Fatal("expected NextCursor for page 2")
	}

	page2, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "nombre",
		SortOrder:  "asc",
		Pagination: outbound.ListParams{Cursor: page1.NextCursor, PageSize: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error on page2: %v", err)
	}
	if got := idsOf(page2.Items); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("expected page2 [3,4], got %v", idsOf(page2.Items))
	}
	if page2.NextCursor != "" {
		t.Errorf("expected empty NextCursor on last page, got %q", page2.NextCursor)
	}
}

func TestBuscarClientes_GlobalPath_RepoError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &fakeClientesRepo{dirCompletoErr: errors.New("boom")}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		SortBy:     "nombre",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Code != "directorio_list_completo_failed" {
		t.Errorf("expected code directorio_list_completo_failed, got %q", appErr.Code)
	}
}

// On the search path, an explicit SortBy overrides FTS rank (global sort over the
// bounded search set).
func TestBuscarClientes_SearchPath_SortByOverridesRank(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// FTS rank order [3,1,2] but we sort by nombre asc → A(1),B(2),C(3).
	idx := &fakeSearchIndex{ready: true, ids: []int{3, 1, 2}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{}
	for _, it := range items {
		clients[it.Cliente.ClienteID()] = it.Cliente
	}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		SortBy:     "nombre",
		SortOrder:  "asc",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idsOf(result.Items); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("expected nombre-sorted [1,2,3], got %v", idsOf(result.Items))
	}
}
