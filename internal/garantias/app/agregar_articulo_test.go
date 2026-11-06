// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestAgregarArticulo_CaminoFeliz adds a second item to an open folio and leaves
// it registered, with its own event pointing at it.
func TestAgregarArticulo_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)

	res, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        s.g.ID(),
		Clave:             "B",
		Description:       "cajon roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}

	if got := res.ArticulosCount(); got != 2 {
		t.Fatalf("articulos = %d, want 2", got)
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	ev := eventoEscrito(t, grepo)
	if ev.Tipo() != domain.TipoEventoArticuloAgregado {
		t.Fatalf("evento = %s, want articulo_agregado", ev.Tipo())
	}
	if ev.ArticuloRef() == nil {
		t.Fatal("el evento debe apuntar al articulo agregado")
	}
	nuevo := articuloDe(t, res, *ev.ArticuloRef())
	if nuevo.Etapa() != domain.EtapaRegistrado {
		t.Errorf("etapa del articulo = %s, want registrado", nuevo.Etapa())
	}
	if nuevo.Clave() != "B" {
		t.Errorf("clave = %q, want B", nuevo.Clave())
	}
}

// TestAgregarArticulo_FolioListoEntrega pins R4's other half: once the folio is
// ready to deliver it takes no new articles, or MarcarListoEntrega could be
// undone after the fact.
func TestAgregarArticulo_FolioListoEntrega(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, piso(), domain.EtapaListoEntrega)

	_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        s.g.ID(),
		Clave:             "B",
		Description:       "cajon roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrFolioNoAdmiteArticulos) {
		t.Fatalf("want ErrFolioNoAdmiteArticulos, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
