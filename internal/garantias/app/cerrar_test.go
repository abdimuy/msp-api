// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestCerrar_CaminoFeliz seals a delivered folio and stamps when it happened.
func TestCerrar_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)

	res, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("Cerrar: %v", err)
	}

	if res.Estado() != domain.EstadoFolioCerrado {
		t.Errorf("estado = %s, want cerrado", res.Estado())
	}
	if res.CerradoEn() == nil {
		t.Error("cerradoEn debe quedar marcado")
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoFolioCerrado {
		t.Errorf("evento = %s, want folio_cerrado", ev.Tipo())
	}
}

// TestCerrar_EstadoNoCorresponde only seals what was delivered.
func TestCerrar_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
