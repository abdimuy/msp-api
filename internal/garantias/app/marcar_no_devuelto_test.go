// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestMarcarNoDevuelto_CaminoFeliz sends the article to standby without a
// replacement: the client refused a working item (R8).
func TestMarcarNoDevuelto_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)

	res, err := svc.MarcarNoDevuelto(context.Background(), app.MarcarNoDevueltoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("MarcarNoDevuelto: %v", err)
	}

	if got := res.ArticulosCount(); got != 1 {
		t.Errorf("articulos = %d, want 1: este camino no crea reemplazo", got)
	}
	art := articuloDe(t, res, s.primero())
	if art.Etapa() != domain.EtapaStandby {
		t.Errorf("etapa = %s, want standby", art.Etapa())
	}
	if art.Ubicacion() != domain.UbicacionAlmacenRevision {
		t.Errorf("ubicacion = %s, want almacen_revision: standby no mueve el articulo", art.Ubicacion())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoDevolucionFallida {
		t.Errorf("evento = %s, want devolucion_fallida", ev.Tipo())
	}
}

// TestMarcarNoDevuelto_RolObligatorio requires the decider role: standby is a
// decision, not a status the operator sets.
func TestMarcarNoDevuelto_RolObligatorio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)

	_, err := svc.MarcarNoDevuelto(context.Background(), app.MarcarNoDevueltoCmd{
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

// TestMarcarNoDevuelto_RolInvalido is rejected while parsing the command.
func TestMarcarNoDevuelto_RolInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEsperaRespuestaCliente)

	_, err := svc.MarcarNoDevuelto(context.Background(), app.MarcarNoDevueltoCmd{
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

// TestMarcarNoDevuelto_EtapaNoCorresponde only leaves espera_respuesta_cliente.
func TestMarcarNoDevuelto_EtapaNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.MarcarNoDevuelto(context.Background(), app.MarcarNoDevueltoCmd{
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
