// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestMoverArticulo_CaminoFeliz records a hand-made move: the location changes,
// the stage does not, and the event carries both ends of the trip (R3).
func TestMoverArticulo_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	res, err := svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ubicacion:         string(domain.UbicacionTaller),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("MoverArticulo: %v", err)
	}

	art := articuloDe(t, res, s.primero())
	if art.Ubicacion() != domain.UbicacionTaller {
		t.Errorf("ubicacion = %s, want taller", art.Ubicacion())
	}
	if art.Etapa() != domain.EtapaEnRevision {
		t.Errorf("etapa = %s, want en_revision: mover no avanza", art.Etapa())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	ev := eventoEscrito(t, grepo)
	if ev.Tipo() != domain.TipoEventoArticuloMovido {
		t.Errorf("evento = %s, want articulo_movido", ev.Tipo())
	}
	if ev.UbicacionDesde() == nil || *ev.UbicacionDesde() != domain.UbicacionAlmacenRevision {
		t.Errorf("ubicacion desde = %v, want almacen_revision", ev.UbicacionDesde())
	}
	if ev.UbicacionHasta() == nil || *ev.UbicacionHasta() != domain.UbicacionTaller {
		t.Errorf("ubicacion hasta = %v, want taller", ev.UbicacionHasta())
	}
}

// TestMoverArticulo_UbicacionNoManual pins R3's limit: entregado and baja are
// set by a stage, never by hand.
func TestMoverArticulo_UbicacionNoManual(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ubicacion:         string(domain.UbicacionEntregado),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrUbicacionManualNoPermitida) {
		t.Fatalf("want ErrUbicacionManualNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestMoverArticulo_SinCambio refuses a move to where the article already is.
func TestMoverArticulo_SinCambio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ubicacion:         string(domain.UbicacionAlmacenRevision),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrUbicacionSinCambio) {
		t.Fatalf("want ErrUbicacionSinCambio, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestMoverArticulo_UbicacionInvalida is rejected while parsing the command.
func TestMoverArticulo_UbicacionInvalida(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ubicacion:         "bodega",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrUbicacionInvalida) {
		t.Fatalf("want ErrUbicacionInvalida, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestMoverArticulo_ArticuloTerminal a closed item does not move again.
func TestMoverArticulo_ArticuloTerminal(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaSegundaMano)

	_, err := svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ubicacion:         string(domain.UbicacionTaller),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrArticuloTerminalNoSeMueve) {
		t.Fatalf("want ErrArticuloTerminalNoSeMueve, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
