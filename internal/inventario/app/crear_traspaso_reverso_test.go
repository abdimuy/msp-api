//nolint:misspell // test vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// seedDirectTraspaso saves a non-reverso traspaso into the repo and returns it.
func seedDirectTraspaso(t *testing.T, svc *app.Service, repo *fakeTraspasoRepo, ventaID uuid.UUID) (*domain.Traspaso, int) {
	t.Helper()
	p := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traspaso original",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(2)}},
		CreatedBy:      uuid.New(),
	}
	tr, id, err := svc.CrearTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("seedDirectTraspaso failed: %v", err)
	}
	return tr, id
}

func TestCrearTraspasoReverso_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	minter := &fakeFolioMinter{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	by := uuid.New()

	// Seed the direct traspaso first (uses MST000001).
	original, _ := seedDirectTraspaso(t, svc, repo, ventaID)
	outbox.entries = nil // reset after seed

	// Create the reversal (uses MST000002).
	reversed, reversoID, err := svc.CrearTraspasoReverso(context.Background(), ventaID, by)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reversoID < 1 {
		t.Errorf("expected positive reversoID, got %d", reversoID)
	}
	if !reversed.TipoReverso() {
		t.Error("reversed traspaso should have TipoReverso=true")
	}
	// Almacenes should be swapped.
	if reversed.AlmacenOrigen() != original.AlmacenDestino() {
		t.Errorf("expected almacen_origen=%d, got %d", original.AlmacenDestino(), reversed.AlmacenOrigen())
	}
	if reversed.AlmacenDestino() != original.AlmacenOrigen() {
		t.Errorf("expected almacen_destino=%d, got %d", original.AlmacenOrigen(), reversed.AlmacenDestino())
	}
	// Outbox should have received TraspasoReversadoEvent.
	if len(outbox.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(outbox.entries))
	}
	if outbox.entries[0].eventType != domain.EventTypeTraspasoReversado {
		t.Errorf("expected event type %q, got %q", domain.EventTypeTraspasoReversado, outbox.entries[0].eventType)
	}
	// Payload should have tipo_reverso=true.
	payload, ok := outbox.entries[0].payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not map[string]any")
	}
	if payload["tipo_reverso"] != true {
		t.Errorf("expected tipo_reverso=true in payload, got %v", payload["tipo_reverso"])
	}
}

func TestCrearTraspasoReverso_NoDirectTraspaso(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, _, err := svc.CrearTraspasoReverso(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, domain.ErrTraspasoNoEncontrado) {
		t.Errorf("expected ErrTraspasoNoEncontrado, got %v", err)
	}
}

func TestCrearTraspasoReverso_MultipleDirectTraspasos(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	// Seed two direct traspasos for the same venta.
	seedDirectTraspaso(t, svc, repo, ventaID)
	seedDirectTraspaso(t, svc, repo, ventaID)

	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if !isAppError(err, "multiple_traspasos_directos") {
		t.Errorf("expected multiple_traspasos_directos error, got %v", err)
	}
}

func TestCrearTraspasoReverso_ListByVentaIDError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	repo.findErr = errSentinel
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, _, err := svc.CrearTraspasoReverso(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestCrearTraspasoReverso_FolioMinterError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	// First minter works (for seeding), second fails.
	minter := &countingFolioMinter{failAfter: 1, err: errSentinel}
	svc := app.NewService(repo, newFakeExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	seedDirectTraspaso(t, svc, repo, ventaID)

	// Now try to reverse — minter will fail on second call.
	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from folio minter, got %v", err)
	}
}

func TestCrearTraspasoReverso_SaveError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	minter := &fakeFolioMinter{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	seedDirectTraspaso(t, svc, repo, ventaID)
	outbox.entries = nil
	repo.saveErr = errSentinel // fail on second Save

	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if len(outbox.entries) != 0 {
		t.Error("outbox should be empty after reversal save failure")
	}
}

// countingFolioMinter returns real folios up to failAfter, then returns err.
type countingFolioMinter struct {
	count     int
	failAfter int
	err       error
}

func (m *countingFolioMinter) MintFolio(_ context.Context) (domain.Folio, error) {
	m.count++
	if m.count > m.failAfter {
		return domain.Folio{}, m.err
	}
	s := "MST" + fmt.Sprintf("%06d", m.count)
	return domain.NewFolio(s)
}
