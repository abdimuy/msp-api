// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestAutorizarCambioFisico_CaminoFeliz swaps the article: the original drops
// to standby and a replacement is born ready for delivery, linked back.
func TestAutorizarCambioFisico_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)
	original := s.primero()

	res, err := svc.AutorizarCambioFisico(context.Background(), app.AutorizarCambioFisicoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        original,
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AutorizarCambioFisico: %v", err)
	}

	if got := res.ArticulosCount(); got != 2 {
		t.Fatalf("articulos = %d, want 2", got)
	}
	if art := articuloDe(t, res, original); art.Etapa() != domain.EtapaStandby {
		t.Errorf("etapa del original = %s, want standby", art.Etapa())
	}
	reemplazo := reemplazoDe(t, res)
	if reemplazo.Etapa() != domain.EtapaListoEntrega {
		t.Errorf("etapa del reemplazo = %s, want listo_entrega", reemplazo.Etapa())
	}
	if reemplazo.ReemplazaA() == nil || *reemplazo.ReemplazaA() != original {
		t.Errorf("reemplazaA = %v, want %s", reemplazo.ReemplazaA(), original)
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	ev := eventoEscrito(t, grepo)
	if ev.Tipo() != domain.TipoEventoCambioAutorizado {
		t.Errorf("evento = %s, want cambio_autorizado", ev.Tipo())
	}
	if ev.EtapaDesde() == nil || *ev.EtapaDesde() != domain.EtapaEsperaRespuestaCliente {
		t.Errorf("etapa desde = %v, want espera_respuesta_cliente", ev.EtapaDesde())
	}
	if ev.EtapaHasta() == nil || *ev.EtapaHasta() != domain.EtapaStandby {
		t.Errorf("etapa hasta = %v, want standby", ev.EtapaHasta())
	}
	if ev.RolDecisor() == nil || *ev.RolDecisor() != domain.RolDecisorOficina {
		t.Errorf("rol del evento = %v, want oficina", ev.RolDecisor())
	}
}

// TestAutorizarCambioFisico_RolObligatorio keeps the swap frozen when nobody
// says in which capacity they are deciding.
func TestAutorizarCambioFisico_RolObligatorio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)

	_, err := svc.AutorizarCambioFisico(context.Background(), app.AutorizarCambioFisicoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRolDecisorObligatorio) {
		t.Fatalf("want ErrRolDecisorObligatorio, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestAutorizarCambioFisico_RolInvalido is rejected while parsing the command.
func TestAutorizarCambioFisico_RolInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)

	_, err := svc.AutorizarCambioFisico(context.Background(), app.AutorizarCambioFisicoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		RolDecisor:        "gerente",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRolDecisorInvalido) {
		t.Fatalf("want ErrRolDecisorInvalido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestAutorizarCambioFisico_EtapaNoCorresponde pins that the swap is only
// reachable from en_taller or espera_respuesta_cliente.
func TestAutorizarCambioFisico_EtapaNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.AutorizarCambioFisico(context.Background(), app.AutorizarCambioFisicoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
		t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
