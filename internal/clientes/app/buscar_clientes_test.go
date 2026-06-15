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

func TestBuscarClientes_BrowsePath_PulsoFilterPageLocal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 3 items in the page; only 1 matches the pulse filter.
	items := []outbound.DirectorioItem{
		newDirItem(1, "A", zero),
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{1: items[0].Cliente, 2: items[1].Cliente, 3: items[2].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage: outbound.Page[outbound.DirectorioItem]{
			Items:      items,
			NextCursor: "more",
		},
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
	// Only item 1 passes the filter.
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item after page-local pulse filter, got %d", len(result.Items))
	}
	if result.Items[0].Cliente.ClienteID() != 1 {
		t.Errorf("expected clienteID=1, got %d", result.Items[0].Cliente.ClienteID())
	}
	// NextCursor from repo must be carried through.
	if result.NextCursor != "more" {
		t.Errorf("expected NextCursor more, got %q", result.NextCursor)
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
