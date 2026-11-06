package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestMarcarNoDevuelto covers R8: the "no se pudo devolver" branch moves an
// article from espera_respuesta_cliente to standby without creating a
// replacement, and requires a decider role.
func TestMarcarNoDevuelto(t *testing.T) {
	t.Parallel()

	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente,
			etapa:  domain.EtapaEsperaRespuestaCliente, ubicacion: domain.UbicacionProveedor,
		})
		antes := g.ArticulosCount()

		if err := g.MarcarNoDevuelto(a.ID(), actorDecisor("juan"), fixed); err != nil {
			t.Fatalf("MarcarNoDevuelto: %v", err)
		}
		if a.Etapa() != domain.EtapaStandby {
			t.Fatalf("etapa = %q, want standby", a.Etapa())
		}
		if g.ArticulosCount() != antes {
			t.Fatalf("se creó un reemplazo: %d -> %d", antes, g.ArticulosCount())
		}
		if a.Ubicacion() != domain.UbicacionProveedor {
			t.Fatalf("la ubicación no debía cambiar: %q", a.Ubicacion())
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoDevolucionFallida {
			t.Fatalf("evento = %q, want devolucion_fallida", e.Tipo())
		}
		if e.EtapaDesde() == nil || *e.EtapaDesde() != domain.EtapaEsperaRespuestaCliente {
			t.Fatalf("EtapaDesde = %v, want espera_respuesta_cliente", e.EtapaDesde())
		}
		if e.EtapaHasta() == nil || *e.EtapaHasta() != domain.EtapaStandby {
			t.Fatalf("EtapaHasta = %v, want standby", e.EtapaHasta())
		}
	})

	t.Run("sin_rol", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaEsperaRespuestaCliente,
		})
		pendientes := g.EventosPendientesCount()
		err := g.MarcarNoDevuelto(a.ID(), actor("juan"), fixed)
		if !errors.Is(err, domain.ErrRolDecisorObligatorio) {
			t.Fatalf("want ErrRolDecisorObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaEsperaRespuestaCliente || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})

	t.Run("etapa_incorrecta", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaListoEntrega,
		})
		pendientes := g.EventosPendientesCount()
		err := g.MarcarNoDevuelto(a.ID(), actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
			t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
		}
		if a.Etapa() != domain.EtapaListoEntrega || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})

	t.Run("folio_cerrado", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioCerrado,
			etapa: domain.EtapaEsperaRespuestaCliente,
		})
		pendientes := g.EventosPendientesCount()
		err := g.MarcarNoDevuelto(a.ID(), actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrFolioNoAdmiteOperacion) {
			t.Fatalf("want ErrFolioNoAdmiteOperacion, got %v", err)
		}
		if a.Etapa() != domain.EtapaEsperaRespuestaCliente || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})

	t.Run("articulo_inexistente", func(t *testing.T) {
		t.Parallel()
		g, _ := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaEsperaRespuestaCliente,
		})
		err := g.MarcarNoDevuelto(uuid.New(), actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrArticuloNoEncontrado) {
			t.Fatalf("want ErrArticuloNoEncontrado, got %v", err)
		}
	})

	t.Run("actor_invalido", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaEsperaRespuestaCliente,
		})
		pendientes := g.EventosPendientesCount()
		malo := actorDecisor("")
		err := g.MarcarNoDevuelto(a.ID(), malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaEsperaRespuestaCliente || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})
}
