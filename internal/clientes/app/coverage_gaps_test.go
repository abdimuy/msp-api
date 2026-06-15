//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestBuscarClientes_SearchPath_PageSizeDefault exercises the pageSize<=0
// default branch inside buscarPorTexto.
func TestBuscarClientes_SearchPath_PageSizeDefault(t *testing.T) {
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
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	// PageSize=0 → should default to 20, return all 3 items.
	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "q",
		Pagination: outbound.ListParams{PageSize: 0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 3 {
		t.Errorf("expected 3 items with PageSize default, got %d", len(result.Items))
	}
}

// TestBuscarClientes_SearchPath_OffsetBeyondTotal exercises offset>=total branch.
func TestBuscarClientes_SearchPath_OffsetBeyondTotal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1}}
	items := []outbound.DirectorioItem{newDirItem(1, "A", zero)}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: items[0].Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	// offset=50 > total=1 → empty page.
	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "q",
		Pagination: outbound.ListParams{Cursor: "o50", PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 0 {
		t.Errorf("expected empty page for out-of-range offset, got %d", len(result.Items))
	}
}

// TestBuscarClientes_SearchPath_FetchError exercises the dirPage fetch error
// inside buscarPorTexto.
func TestBuscarClientes_SearchPath_FetchError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1}}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		dirErr:      errors.New("db fetch error"),
	}
	anl := &fakeAnalyticsClient{}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
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
}

// TestBuscarClientes_BrowsePath_EnrichError exercises the pulsosErr branch
// inside enriquecerYFiltrar (via browse path).
func TestBuscarClientes_BrowsePath_EnrichError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	items := []outbound.DirectorioItem{newDirItem(1, "A", zero)}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: items[0].Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{pulsosErr: errors.New("analytics down")}
	svc := app.NewService(repo, anl, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error from analytics failure")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Kind != apperror.KindInternal {
		t.Errorf("expected KindInternal, got %v", appErr.Kind)
	}
}

// TestBuscarClientes_SearchPath_EnrichError exercises the pulsosErr branch
// inside enriquecerYFiltrar (via search path).
func TestBuscarClientes_SearchPath_EnrichError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	idx := &fakeSearchIndex{ready: true, ids: []int{1}}
	items := []outbound.DirectorioItem{newDirItem(1, "A", zero)}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: items[0].Cliente},
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{pulsosErr: errors.New("analytics timeout")}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "x",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error from analytics failure in search path")
	}
}

// TestObtenerFicha_ClienteAppError exercises the apperror.As branch for
// ObtenerCliente returning a typed non-NotFound apperror (e.g. KindInternal
// from the repo layer).
func TestObtenerFicha_ClienteAppError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbErr := apperror.NewInternal("db_timeout", "timeout al leer cliente")
	repo := &fakeClientesRepo{clienteErr: dbErr, clienteByID: map[int]*domain.Cliente{}}
	anl := &fakeAnalyticsClient{}
	svc := app.NewService(repo, anl, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerFicha(ctx, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	// Source must be attached by the app layer.
	if appErr.Source != "clientes.ObtenerFicha" {
		t.Errorf("expected source clientes.ObtenerFicha, got %q", appErr.Source)
	}
}

// TestBuscarClientes_SearchPath_UnrankedItemsLast exercises the sortByRank
// "keyHasRank && !jHasRank" branch by having a ranked item after an unranked one.
func TestBuscarClientes_SearchPath_SortUnranked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Repo returns items 1, 2, 3 but FTS only ranked 2 and 3 (1 slipped in via native filters).
	// After re-sort: 2 (rank 0), 3 (rank 1), 1 (unranked, at end).
	idx := &fakeSearchIndex{ready: true, ids: []int{2, 3}}
	items := []outbound.DirectorioItem{
		newDirItem(1, "Extra", zero), // unranked
		newDirItem(2, "B", zero),
		newDirItem(3, "C", zero),
	}
	clients := map[int]*domain.Cliente{1: items[0].Cliente, 2: items[1].Cliente, 3: items[2].Cliente}
	repo := &fakeClientesRepo{
		clienteByID: clients,
		dirPage:     outbound.Page[outbound.DirectorioItem]{Items: items},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}
	svc := app.NewService(repo, anl, idx, fixedClock{T: fixedTime})

	result, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Q:          "q",
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	// Ranked items first: 2, 3. Unranked item 1 last.
	if result.Items[0].Cliente.ClienteID() != 2 {
		t.Errorf("expected first item clienteID=2, got %d", result.Items[0].Cliente.ClienteID())
	}
	if result.Items[1].Cliente.ClienteID() != 3 {
		t.Errorf("expected second item clienteID=3, got %d", result.Items[1].Cliente.ClienteID())
	}
	if result.Items[2].Cliente.ClienteID() != 1 {
		t.Errorf("expected third item clienteID=1 (unranked), got %d", result.Items[2].Cliente.ClienteID())
	}
}
