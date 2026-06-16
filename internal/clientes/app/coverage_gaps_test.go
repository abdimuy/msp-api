//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestBuscarClientes_DefaultsPageSize exercises the pageSize<=0 default (50).
func TestBuscarClientes_DefaultsPageSize(t *testing.T) {
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
	// Meilisearch receives limit=50 (the default).
	if di.captured.Limit != 50 {
		t.Errorf("Limit: got %d, want 50 (default)", di.captured.Limit)
	}
}

// TestBuscarClientes_ServiceUnavailable_IsNotWrapped verifies that a
// KindServiceUnavailable error from the index passes through unchanged
// (the app layer does NOT re-wrap it).
func TestBuscarClientes_ServiceUnavailable_IsNotWrapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	origErr := apperror.NewServiceUnavailable(
		"directorio_search_unavailable",
		"el buscador de directorio no está disponible en este momento",
	)
	di := &fakeDirectoryIndexWithBuscar{buscarErr: origErr}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	// Must preserve the original KindServiceUnavailable and code.
	if appErr.Kind != apperror.KindServiceUnavailable {
		t.Errorf("Kind: got %v, want KindServiceUnavailable", appErr.Kind)
	}
	if appErr.Code != "directorio_search_unavailable" {
		t.Errorf("Code: got %q, want %q", appErr.Code, "directorio_search_unavailable")
	}
}

// TestBuscarClientes_NonAppError_Propagates verifies that non-apperror errors
// from the index propagate as-is (no panic, no silent swallow).
func TestBuscarClientes_NonAppError_Propagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rawErr := errors.New("unexpected transport error")
	di := &fakeDirectoryIndexWithBuscar{buscarErr: rawErr}
	svc := buildSvc(di)

	_, err := svc.BuscarClientes(ctx, app.BuscarClientesInput{
		Pagination: outbound.ListParams{PageSize: 10},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, rawErr) {
		t.Errorf("expected original error in chain, got %v", err)
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
	svc := app.NewService(repo, anl, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerFicha(ctx, 1, outbound.RangoFechas{})
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
