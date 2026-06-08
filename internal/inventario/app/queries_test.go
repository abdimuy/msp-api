//nolint:misspell // test vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ─── ConsultarExistencia ────────────────────────────────────────────────────

func TestConsultarExistencia_HappyPath(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 1, mustDecimal("5.5"))
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	got, err := svc.ConsultarExistencia(context.Background(), 100, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(mustDecimal("5.5")) {
		t.Errorf("expected 5.5, got %s", got.String())
	}
}

func TestConsultarExistencia_RepoError(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.err = errSentinel
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, err := svc.ConsultarExistencia(context.Background(), 100, 1)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// ─── ListarExistenciasAlmacen ───────────────────────────────────────────────

func TestListarExistenciasAlmacen_HappyPath(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.set(100, 2, decimal.NewFromInt(3))
	eq.set(200, 2, decimal.NewFromInt(7))
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	list, err := svc.ListarExistenciasAlmacen(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 existencias, got %d", len(list))
	}
}

func TestListarExistenciasAlmacen_RepoError(t *testing.T) {
	t.Parallel()
	eq := newFakeExistenciaQuery()
	eq.err = errSentinel
	svc := app.NewService(newFakeTraspasoRepo(), eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, err := svc.ListarExistenciasAlmacen(context.Background(), 2)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// ─── ListarAlmacenes ────────────────────────────────────────────────────────

func TestListarAlmacenes_HappyPath(t *testing.T) {
	t.Parallel()
	almacenes := newFakeAlmacenRepo(
		domain.NewAlmacen(1, "Almacén Central"),
		domain.NewAlmacen(2, "Almacén Norte"),
	)
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, almacenes, &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	list, err := svc.ListarAlmacenes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 almacenes, got %d", len(list))
	}
}

// ─── ObtenerTraspaso ────────────────────────────────────────────────────────

func TestObtenerTraspaso_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	_, doctoInID, err := svc.CrearTraspasoParaVenta(context.Background(), app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	got, err := svc.ObtenerTraspaso(context.Background(), doctoInID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil traspaso")
	}
}

func TestObtenerTraspaso_NotFound(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, err := svc.ObtenerTraspaso(context.Background(), 9999)
	if !errors.Is(err, domain.ErrTraspasoNoEncontrado) {
		t.Errorf("expected ErrTraspasoNoEncontrado, got %v", err)
	}
}

func TestObtenerTraspaso_RepoError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	repo.findErr = errSentinel
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, err := svc.ObtenerTraspaso(context.Background(), 1)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// ─── ListarTraspasosPorVenta ─────────────────────────────────────────────────

func TestListarTraspasosPorVenta_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	svc.CrearTraspasoParaVenta(context.Background(), app.CrearTraspasoParaVentaParams{ //nolint:errcheck
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	})

	list, err := svc.ListarTraspasosPorVenta(context.Background(), ventaID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 traspaso, got %d", len(list))
	}
}

func TestListarTraspasosPorVenta_EmptyForUnknownVenta(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	list, err := svc.ListarTraspasosPorVenta(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(list) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(list))
	}
}

func TestListarTraspasosPorVenta_RepoError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	repo.findErr = errSentinel
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, err := svc.ListarTraspasosPorVenta(context.Background(), uuid.New())
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}
