package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestCancelar_ArticuloFuera pins R5: cancellation is refused while an article
// is at the supplier or in transit, because the item is out of our hands; any
// workshop location is internal and does not block.
func TestCancelar_ArticuloFuera(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre    string
		ubicacion domain.Ubicacion
		bloquea   bool
	}{
		{"proveedor", domain.UbicacionProveedor, true},
		{"en_transito", domain.UbicacionEnTransito, true},
		{"taller", domain.UbicacionTaller, false},
		{"almacen_revision", domain.UbicacionAlmacenRevision, false},
		{"almacen_segunda_mano", domain.UbicacionAlmacenSegundaMano, false},
	}

	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			g, _ := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: tc.ubicacion,
			})
			pendientes := g.EventosPendientesCount()

			err := g.Cancelar("cliente desistió", actor("juan"), fixed)

			if tc.bloquea {
				if !errors.Is(err, domain.ErrFolioConArticuloFuera) {
					t.Fatalf("want ErrFolioConArticuloFuera, got %v", err)
				}
				if g.Estado() != domain.EstadoFolioEnProceso || g.EventosPendientesCount() != pendientes {
					t.Fatalf("mutación/evento pese al rechazo")
				}
				return
			}

			if err != nil {
				t.Fatalf("Cancelar(%s): %v", tc.ubicacion, err)
			}
			if g.Estado() != domain.EstadoFolioCancelado {
				t.Fatalf("estado = %q, want cancelado", g.Estado())
			}
			if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioCancelado {
				t.Fatalf("evento = %q, want folio_cancelado", e.Tipo())
			}
		})
	}
}
