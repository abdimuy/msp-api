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

func TestCrearTraspasoParaVenta_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso de prueba",
		Detalles: []app.CrearTraspasoDetalleInput{
			{ArticuloID: 100, Cantidad: decimal.NewFromInt(3)},
		},
		CreatedBy: uuid.New(),
	}

	tr, doctoInID, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doctoInID != 1 {
		t.Errorf("expected doctoInID=1, got %d", doctoInID)
	}
	if tr.DoctoInID() == nil || *tr.DoctoInID() != 1 {
		t.Error("traspaso should be marked applied")
	}
	if tr.TipoReverso() {
		t.Error("should not be a reverso")
	}
	if tr.VentaID() == nil || *tr.VentaID() != p.VentaID {
		t.Error("venta ID mismatch")
	}
	if repo.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", repo.SaveCalls)
	}
	// Outbox should have received TraspasoCreadoEvent.
	if len(outbox.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(outbox.entries))
	}
	if outbox.entries[0].eventType != domain.EventTypeTraspasoCreado {
		t.Errorf("expected event type %q, got %q", domain.EventTypeTraspasoCreado, outbox.entries[0].eventType)
	}
}

func TestCrearTraspasoParaVenta_SaveError_LeavesOutboxEmpty(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	repo.saveErr = errSentinel
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso de prueba",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if len(outbox.entries) != 0 {
		t.Error("outbox should be empty after save failure")
	}
}

func TestCrearTraspasoParaVenta_InvalidCantidad(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(0)}},
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if !errors.Is(err, domain.ErrCantidadInvalida) {
		t.Errorf("expected ErrCantidadInvalida, got %v", err)
	}
}

func TestCrearTraspasoParaVenta_DomainError_AlmacenesIguales(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 1, // same — domain rejects
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if !errors.Is(err, domain.ErrAlmacenesIguales) {
		t.Errorf("expected ErrAlmacenesIguales, got %v", err)
	}
}

func TestCrearTraspasoParaVenta_NoDetalles(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       nil,
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if !errors.Is(err, domain.ErrTraspasoSinDetalles) {
		t.Errorf("expected ErrTraspasoSinDetalles, got %v", err)
	}
}

func TestCrearTraspasoParaVenta_FolioMinterError(t *testing.T) {
	t.Parallel()
	minter := &errFolioMinter{err: errSentinel}
	svc := app.NewService(newFakeTraspasoRepo(), newFakeExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from folio minter, got %v", err)
	}
}

func TestCrearTraspasoParaVenta_OutboxError_DoesNotPropagateToResult(t *testing.T) {
	t.Parallel()
	// Outbox errors are best-effort; the service should still succeed.
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{err: errSentinel}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	p := app.CrearTraspasoParaVentaParams{
		VentaID:        uuid.New(),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		CreatedBy:      uuid.New(),
	}

	_, _, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Errorf("outbox error should not propagate; got %v", err)
	}
}

// errFolioMinter always returns an error.
type errFolioMinter struct{ err error }

func (m *errFolioMinter) MintFolio(_ context.Context) (domain.Folio, error) {
	return domain.Folio{}, m.err
}
