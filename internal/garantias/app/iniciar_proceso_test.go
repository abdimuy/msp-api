// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestIniciarProceso_CaminoFeliz starts the process and records the folio fact.
//
// The event type is pinned on purpose: this command used to hand the stage event
// to ejecutarFolio while the domain wrote folio_en_proceso, which made every
// resend fail as a foreign key (mutant C6).
func TestIniciarProceso_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("IniciarProceso: %v", err)
	}

	if res.Estado() != domain.EstadoFolioEnProceso {
		t.Errorf("estado = %s, want en_proceso", res.Estado())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoFolioEnProceso {
		t.Errorf("evento = %s, want folio_en_proceso: el replay compara este tipo", ev.Tipo())
	}
}

// TestIniciarProceso_SinArticulos refuses an empty folio.
func TestIniciarProceso_SinArticulos(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioAbierto, piso())

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrGarantiaSinArticulos) {
		t.Fatalf("want ErrGarantiaSinArticulos, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
