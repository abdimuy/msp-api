// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestAvanzarArticulo_CaminoFeliz walks the first edge of the floor trunk:
// registrado becomes en_revision at the warehouse.
func TestAvanzarArticulo_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaRegistrado)

	res, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		EtapaDestino:      domain.EtapaEnRevision,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}

	art := articuloDe(t, res, s.primero())
	if art.Etapa() != domain.EtapaEnRevision {
		t.Errorf("etapa = %s, want en_revision", art.Etapa())
	}
	if art.Ubicacion() != domain.UbicacionAlmacenRevision {
		t.Errorf("ubicacion = %s, want almacen_revision", art.Ubicacion())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoEtapaAvanzada {
		t.Errorf("evento = %s, want etapa_avanzada", ev.Tipo())
	}
}

// TestAvanzarArticulo_FolioAbierto pins R4: nothing moves until the process
// starts, so the only article operation an open folio admits is adding one.
func TestAvanzarArticulo_FolioAbierto(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)

	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		EtapaDestino:      domain.EtapaEnRevision,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrFolioNoAdmiteOperacion) {
		t.Fatalf("want ErrFolioNoAdmiteOperacion, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestAvanzarArticulo_TransicionNoPermitida lets the stage machine refuse an
// edge that does not exist.
func TestAvanzarArticulo_TransicionNoPermitida(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaRegistrado)

	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		EtapaDestino:      domain.EtapaDictamenRecibido,
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
