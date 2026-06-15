//nolint:misspell // Spanish vocabulary (venta, detalle, etc.) per project convention.
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

func TestObtenerVentaDetalle_PassThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	venta := newVentaCliente(55, 1)
	detalle := outbound.VentaDetalle{Venta: venta}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		detalleByID: map[int]outbound.VentaDetalle{55: detalle},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	result, err := svc.ObtenerVentaDetalle(ctx, 55)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Venta.DoctoPVID() != 55 {
		t.Errorf("expected DoctoPVID 55, got %d", result.Venta.DoctoPVID())
	}
}

func TestObtenerVentaDetalle_VentaNotFoundPropagada(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		detalleByID: map[int]outbound.VentaDetalle{},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerVentaDetalle(ctx, 9999)

	if err == nil {
		t.Fatal("expected error for missing venta")
	}
	if !errors.Is(err, domain.ErrVentaNotFound) {
		t.Errorf("expected ErrVentaNotFound, got %v", err)
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Source != "clientes.ObtenerVentaDetalle" {
		t.Errorf("expected source clientes.ObtenerVentaDetalle, got %q", appErr.Source)
	}
}

func TestObtenerVentaDetalle_InfraErrorWrapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	infraErr := errors.New("db timeout")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
		detalleByID: map[int]outbound.VentaDetalle{},
		detalleErr:  infraErr,
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeSearchIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerVentaDetalle(ctx, 55)

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
	if appErr.Source != "clientes.ObtenerVentaDetalle" {
		t.Errorf("expected source clientes.ObtenerVentaDetalle, got %q", appErr.Source)
	}
}
