// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// TestEscrituraDentroDeTransaccion is the assertion the brief asks for on
// purpose: the fakes record, on every write, whether a transaction was open.
// A write that lands outside RunInTx would break the aggregate rule of the
// module without failing any other test, so it gets its own.
func TestEscrituraDentroDeTransaccion(t *testing.T) {
	t.Parallel()
	base := newFakeGarantiaRepo()
	erepo := newFakeEventoRepo()
	fgen := &fakeFolioGen{nums: []int{3001}}
	tx := &fakeTxRunner{}
	rec := &recordingRepo{
		fakeGarantiaRepo: base,
		enTx:             func() bool { return tx.enTx() },
	}
	svc := app.NewService(app.Deps{
		GarantiaRepo:   rec,
		EventoRepo:     erepo,
		FolioGenerator: fgen,
		TxRunner:       tx,
		Identity:       &fakeIdentity{usuario: outbound.Usuario{ID: "u1", Nombre: "Juan"}},
		Clock:          &fakeClock{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
	})
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	if _, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla rota",
		ClaveIdempotencia: "k-tx-1",
		DeviceCreatedAt:   now,
	}); err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}

	g := seedFolio(t, base, now, 1)
	if _, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
		ClaveIdempotencia: "k-tx-2",
		DeviceCreatedAt:   now,
	}); err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}

	if len(rec.dentroDeTx) != 2 {
		t.Fatalf("writes = %d, want 2", len(rec.dentroDeTx))
	}
	for i, dentro := range rec.dentroDeTx {
		if !dentro {
			t.Errorf("escritura %d ocurrio FUERA de la transaccion", i)
		}
	}
}
