// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestCancelar_CaminoFeliz voids an in-process folio whose articles are all at
// a workshop location, and keeps the reason on the event.
func TestCancelar_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	motivo := "cliente no aparece"

	res, err := svc.Cancelar(context.Background(), app.CancelarCmd{
		GarantiaID:        s.g.ID(),
		Motivo:            motivo,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("Cancelar: %v", err)
	}

	if res.Estado() != domain.EstadoFolioCancelado {
		t.Errorf("estado = %s, want cancelado", res.Estado())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	ev := eventoEscrito(t, grepo)
	if ev.Tipo() != domain.TipoEventoFolioCancelado {
		t.Errorf("evento = %s, want folio_cancelado", ev.Tipo())
	}
	if ev.Description() != motivo {
		t.Errorf("motivo = %q, want %q", ev.Description(), motivo)
	}
}

// TestCancelar_SinMotivo needs a reason: the folio_cancelado event is the only
// place it is kept.
func TestCancelar_SinMotivo(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.Cancelar(context.Background(), app.CancelarCmd{
		GarantiaID:        s.g.ID(),
		Motivo:            "   ",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrMotivoCancelacionObligatorio) {
		t.Fatalf("want ErrMotivoCancelacionObligatorio, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestCancelar_ArticuloFuera pins R5: at the supplier or in transit the item is
// out of our hands, so the folio cannot be voided yet.
func TestCancelar_ArticuloFuera(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnviadoProveedor)

	_, err := svc.Cancelar(context.Background(), app.CancelarCmd{
		GarantiaID:        s.g.ID(),
		Motivo:            "cliente no aparece",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrFolioConArticuloFuera) {
		t.Fatalf("want ErrFolioConArticuloFuera, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestCancelar_EstadoNoCorresponde only voids an open or in-process folio.
func TestCancelar_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)

	_, err := svc.Cancelar(context.Background(), app.CancelarCmd{
		GarantiaID:        s.g.ID(),
		Motivo:            "cliente no aparece",
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
