// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestMarcarListoEntrega_CaminoFeliz closes the working phase: every article is
// ready (a standby one does not block, decision 4 and regla 1: standby counts
// for both origins) and the folio waits for the client.
func TestMarcarListoEntrega_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	res, err := svc.MarcarListoEntrega(context.Background(), app.MarcarListoEntregaCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("MarcarListoEntrega: %v", err)
	}

	if res.Estado() != domain.EstadoFolioListoEntrega {
		t.Errorf("estado = %s, want listo_entrega", res.Estado())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoFolioListoEntrega {
		t.Errorf("evento = %s, want folio_listo_entrega", ev.Tipo())
	}
}

// TestMarcarListoEntrega_ArticuloNoListo refuses to declare the folio ready
// while an article is still being worked on, with the sentinel of each origin.
// On piso, listo_entrega does not count either (regla 1): the article has to
// be reinstated first.
func TestMarcarListoEntrega_ArticuloNoListo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		nombre string
		origen opcionesSemilla
		etapa  domain.Etapa
		want   error
	}{
		{"cliente_en_revision", cliente(), domain.EtapaEnRevision, domain.ErrArticulosNoListosParaEntrega},
		{"piso_en_revision", piso(), domain.EtapaEnRevision, domain.ErrArticuloPisoSinReingresar},
		{"piso_listo_entrega", piso(), domain.EtapaListoEntrega, domain.ErrArticuloPisoSinReingresar},
	} {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, _, _, _, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, tc.origen, tc.etapa)

			_, err := svc.MarcarListoEntrega(context.Background(), app.MarcarListoEntregaCmd{
				GarantiaID:        s.g.ID(),
				ClaveIdempotencia: clave(),
				DeviceCreatedAt:   clk.Now(),
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestMarcarListoEntrega_EstadoNoCorresponde pins the folio state machine: an
// abierto folio has to pass through en_proceso first.
func TestMarcarListoEntrega_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)

	_, err := svc.MarcarListoEntrega(context.Background(), app.MarcarListoEntregaCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
}
