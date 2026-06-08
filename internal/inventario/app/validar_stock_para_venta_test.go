//nolint:misspell // test vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestValidarStockParaVenta_EmptyItems_ReturnsNil(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	if err := svc.ValidarStockParaVenta(context.Background(), nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := svc.ValidarStockParaVenta(context.Background(), []app.ValidarStockItem{}); err != nil {
		t.Errorf("expected nil for empty slice, got %v", err)
	}
}

func TestValidarStockParaVenta_SufficientStock_ReturnsNil(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 1, decimal.NewFromInt(10))
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 100, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(10)}, // exact
	}
	if err := svc.ValidarStockParaVenta(context.Background(), items); err != nil {
		t.Errorf("exact stock should pass; got %v", err)
	}
}

func TestValidarStockParaVenta_ExcessStock_ReturnsNil(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 1, decimal.NewFromInt(20))
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 100, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(5)},
	}
	if err := svc.ValidarStockParaVenta(context.Background(), items); err != nil {
		t.Errorf("excess stock should pass; got %v", err)
	}
}

func TestValidarStockParaVenta_InsufficientStock_ReturnsError(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 1, decimal.NewFromInt(2))
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 100, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(5)},
	}
	err := svc.ValidarStockParaVenta(context.Background(), items)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrArticuloSinExistencia) {
		t.Errorf("expected ErrArticuloSinExistencia, got %v", err)
	}
	// Verify fields are attached.
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected apperror.Error")
	}
	if appErr.Fields["articulo_id"] != 100 {
		t.Errorf("expected articulo_id=100, got %v", appErr.Fields["articulo_id"])
	}
	if appErr.Fields["almacen_id"] != 1 {
		t.Errorf("expected almacen_id=1, got %v", appErr.Fields["almacen_id"])
	}
	if appErr.Fields["cantidad_requerida"] != "5" {
		t.Errorf("expected cantidad_requerida='5', got %v", appErr.Fields["cantidad_requerida"])
	}
	if appErr.Fields["existencia_disponible"] != "2" {
		t.Errorf("expected existencia_disponible='2', got %v", appErr.Fields["existencia_disponible"])
	}
}

func TestValidarStockParaVenta_ZeroStock_Insufficient(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery() // no stock configured → returns 0
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 200, AlmacenOrigen: 3, Cantidad: decimal.NewFromInt(1)},
	}
	err := svc.ValidarStockParaVenta(context.Background(), items)
	if !errors.Is(err, domain.ErrArticuloSinExistencia) {
		t.Errorf("expected ErrArticuloSinExistencia for zero stock, got %v", err)
	}
}

func TestValidarStockParaVenta_ExistenciaQueryError(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.err = errSentinel
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 100, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(1)},
	}
	err := svc.ValidarStockParaVenta(context.Background(), items)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestValidarStockParaVenta_MultipleItems_FailsOnFirst(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 1, decimal.NewFromInt(10)) // sufficient
	eq.set(200, 1, decimal.NewFromInt(0))  // insufficient
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	items := []app.ValidarStockItem{
		{ArticuloID: 100, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(5)},
		{ArticuloID: 200, AlmacenOrigen: 1, Cantidad: decimal.NewFromInt(1)},
	}
	err := svc.ValidarStockParaVenta(context.Background(), items)
	if !errors.Is(err, domain.ErrArticuloSinExistencia) {
		t.Errorf("expected ErrArticuloSinExistencia for second item, got %v", err)
	}
	appErr, _ := apperror.As(err)
	if appErr.Fields["articulo_id"] != 200 {
		t.Errorf("expected failing articulo_id=200, got %v", appErr.Fields["articulo_id"])
	}
}
