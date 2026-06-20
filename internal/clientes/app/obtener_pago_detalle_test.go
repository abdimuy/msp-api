//nolint:misspell // Spanish vocabulary (pago, detalle, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestObtenerPagoDetalle_PassThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	detalle := outbound.PagoDetalle{
		DoctoCCID: 4070588,
		Importe:   decimal.NewFromFloat(200.00),
		Origen:    "microsip",
	}
	repo := &fakeClientesRepo{
		clienteByID:     map[int]*domain.Cliente{},
		pagoDetalleByID: map[int]outbound.PagoDetalle{4070588: detalle},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	result, err := svc.ObtenerPagoDetalle(ctx, 4070588)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DoctoCCID != 4070588 {
		t.Errorf("expected DoctoCCID 4070588, got %d", result.DoctoCCID)
	}
}

func TestObtenerPagoDetalle_NotFoundPropagado(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &fakeClientesRepo{
		clienteByID:     map[int]*domain.Cliente{},
		pagoDetalleByID: map[int]outbound.PagoDetalle{},
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerPagoDetalle(ctx, 9999)

	if err == nil {
		t.Fatal("expected error for missing pago")
	}
	if !errors.Is(err, domain.ErrPagoNotFound) {
		t.Errorf("expected ErrPagoNotFound, got %v", err)
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Source != "clientes.ObtenerPagoDetalle" {
		t.Errorf("expected source clientes.ObtenerPagoDetalle, got %q", appErr.Source)
	}
}

func TestObtenerPagoDetalle_InfraErrorWrapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	infraErr := errors.New("db timeout")
	repo := &fakeClientesRepo{
		clienteByID:     map[int]*domain.Cliente{},
		pagoDetalleByID: map[int]outbound.PagoDetalle{},
		pagoDetalleErr:  infraErr,
	}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerPagoDetalle(ctx, 4070588)

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
	if appErr.Source != "clientes.ObtenerPagoDetalle" {
		t.Errorf("expected source clientes.ObtenerPagoDetalle, got %q", appErr.Source)
	}
}
