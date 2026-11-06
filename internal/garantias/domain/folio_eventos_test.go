package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestEventosDeFolio_ConTipoPropio pins R7: IniciarProceso and
// MarcarListoEntrega no longer both emit etapa_avanzada.
func TestEventosDeFolio_ConTipoPropio(t *testing.T) {
	t.Parallel()

	t.Run("IniciarProceso emite folio_en_proceso", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("IniciarProceso: %v", err)
		}
		if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioEnProceso {
			t.Errorf("evento = %q, want folio_en_proceso", e.Tipo())
		}
	})

	t.Run("MarcarListoEntrega emite folio_listo_entrega", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("IniciarProceso: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		a = advance(t, g, a, domain.EtapaReparadoTaller)
		a = advance(t, g, a, domain.EtapaListoEntrega)
		advance(t, g, a, domain.EtapaReingresadoInventario)

		if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
			t.Fatalf("MarcarListoEntrega: %v", err)
		}
		if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioListoEntrega {
			t.Errorf("evento = %q, want folio_listo_entrega", e.Tipo())
		}
	})
}
