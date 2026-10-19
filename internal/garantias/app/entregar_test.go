// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestEntregar_CaminoFeliz delivers a client folio: every ready article leaves
// with its own event and the folio records the delivery, GPS included.
//
// Two articles on purpose. CLAVE_IDEMPOTENCIA is UNIQUE, so a call that gave
// the same key to the command's event and to each article event would violate
// the index on the second INSERT: this is the case the fake repository alone
// cannot see, because it does not enforce the UNIQUE.
func TestEntregar_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(),
		domain.EtapaListoEntrega, domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	res, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}

	if res.Estado() != domain.EstadoFolioEntregado {
		t.Errorf("estado = %s, want entregado", res.Estado())
	}
	for _, id := range s.arts {
		art := articuloDe(t, res, id)
		if art.Etapa() != domain.EtapaEntregado {
			t.Errorf("etapa del articulo %s = %s, want entregado", id, art.Etapa())
		}
		if art.Ubicacion() != domain.UbicacionEntregado {
			t.Errorf("ubicacion del articulo %s = %s, want entregado", id, art.Ubicacion())
		}
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	// One article event per article, then the folio fact.
	eventos := eventosEscritos(t, grepo)
	if len(eventos) != 3 {
		t.Fatalf("eventos = %d, want 3 (2 articulos + folio)", len(eventos))
	}
	if eventos[0].Tipo() != domain.TipoEventoEtapaAvanzada ||
		eventos[1].Tipo() != domain.TipoEventoEtapaAvanzada {
		t.Errorf("los dos primeros eventos deben ser etapa_avanzada, got %s y %s",
			eventos[0].Tipo(), eventos[1].Tipo())
	}
	if eventos[2].Tipo() != domain.TipoEventoFolioEntregado {
		t.Errorf("ultimo evento = %s, want folio_entregado", eventos[2].Tipo())
	}
	if eventos[2].GPSLat() == nil || *eventos[2].GPSLat() != *lat {
		t.Errorf("el evento del folio debe guardar el GPS de la entrega")
	}
	// Each event needs its own key: two articles delivered by the same call
	// are two rows in a column with a UNIQUE index.
	vistas := map[string]bool{}
	for _, ev := range eventos {
		if vistas[ev.ClaveIdempotencia()] {
			t.Errorf("dos eventos comparten clave de idempotencia: %q", ev.ClaveIdempotencia())
		}
		vistas[ev.ClaveIdempotencia()] = true
	}
}

// TestEntregar_SinGps refuses before the transaction: a delivery without the
// agent's coordinates is the audit hole §4.1 exists to close, so the folio is
// not even locked for it.
func TestEntregar_SinGps(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrEntregaGPSRequerido) {
		t.Fatalf("want ErrEntregaGPSRequerido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEntregar_EstadoNoCorresponde only delivers a folio that is listo_entrega.
func TestEntregar_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, cliente(), domain.EtapaEnRevision)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestEntregar_FolioDePiso pins R1: entregado is a customer stage, so a floor
// folio closes without touching its articles.
func TestEntregar_FolioDePiso(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, piso(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	res, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if res.Estado() != domain.EstadoFolioEntregado {
		t.Errorf("estado = %s, want entregado", res.Estado())
	}
	if art := articuloDe(t, res, s.primero()); art.Etapa() != domain.EtapaListoEntrega {
		t.Errorf("etapa del articulo = %s, want listo_entrega: un folio de piso no entrega articulos", art.Etapa())
	}
}
