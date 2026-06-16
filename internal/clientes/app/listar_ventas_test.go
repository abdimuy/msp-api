//nolint:misspell // Spanish vocabulary (ventas, cliente, etc.) per project convention.
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

func TestListarVentas_PassThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	v1 := newVentaCliente(101, 1)
	v2 := newVentaCliente(102, 1)
	page := outbound.Page[*domain.VentaCliente]{
		Items:      []*domain.VentaCliente{v1, v2},
		NextCursor: "cursor-next",
	}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: newCliente(1, "Test")},
		ventasPage:  page,
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	result, err := svc.ListarVentas(ctx, app.ListarVentasInput{
		ClienteID:  1,
		Pagination: outbound.ListParams{Cursor: "", PageSize: 10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(result.Items))
	}
	if result.NextCursor != "cursor-next" {
		t.Errorf("expected NextCursor cursor-next, got %q", result.NextCursor)
	}
	if repo.lastClienteID != 1 {
		t.Errorf("expected clienteID 1 forwarded, got %d", repo.lastClienteID)
	}
}

func TestListarVentas_InfraErrorWrapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	infraErr := errors.New("query timeout")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		ventasErr:   infraErr,
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ListarVentas(ctx, app.ListarVentasInput{ClienteID: 1})

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
	if appErr.Code != "ventas_cliente_list_failed" {
		t.Errorf("expected code ventas_cliente_list_failed, got %q", appErr.Code)
	}
	if appErr.Source != "clientes.ListarVentas" {
		t.Errorf("expected source clientes.ListarVentas, got %q", appErr.Source)
	}
}
