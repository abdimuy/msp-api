package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestEntregar covers R9: the delivery needs the agent GPS, only listo_entrega
// articles move (one event each, plus the folio one), and a standby article is
// left alone.
func TestEntregar(t *testing.T) {
	t.Parallel()

	t.Run("cliente_entrega_solo_listo_entrega", func(t *testing.T) {
		t.Parallel()
		// One article ready to deliver and one in standby: only the first
		// moves, so the folio closes with the original still in standby (§3.2).
		g, arts := hidratarFolioCon(t, domain.OrigenFolioCliente, domain.EstadoFolioListoEntrega,
			articuloHidratado{etapa: domain.EtapaListoEntrega, ubicacion: domain.UbicacionTaller},
			articuloHidratado{etapa: domain.EtapaStandby, ubicacion: domain.UbicacionTaller},
		)
		listo, standby := arts[0], arts[1]

		pendientes := g.EventosPendientesCount()
		if err := g.Entregar(actorEntrega("juan"), fixed); err != nil {
			t.Fatalf("Entregar: %v", err)
		}

		if g.Estado() != domain.EstadoFolioEntregado {
			t.Fatalf("estado = %q, want entregado", g.Estado())
		}
		if listo.Etapa() != domain.EtapaEntregado || listo.Ubicacion() != domain.UbicacionEntregado {
			t.Fatalf("artículo listo_entrega no se entregó: %q/%q", listo.Etapa(), listo.Ubicacion())
		}
		if standby.Etapa() != domain.EtapaStandby || standby.Ubicacion() != domain.UbicacionTaller {
			t.Fatalf("el artículo en standby se tocó: %q/%q", standby.Etapa(), standby.Ubicacion())
		}
		// Exactly two events: one per delivered article plus the folio one.
		if got := g.EventosPendientesCount() - pendientes; got != 2 {
			t.Fatalf("eventos encolados = %d, want 2", got)
		}
		if e := eventoDeArticulo(t, g, listo.ID()); e.Tipo() != domain.TipoEventoEtapaAvanzada {
			t.Fatalf("evento del artículo = %q, want etapa_avanzada", e.Tipo())
		}
		if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioEntregado {
			t.Fatalf("último evento = %q, want folio_entregado", e.Tipo())
		}
	})

	t.Run("piso_rechaza_listo_entrega", func(t *testing.T) {
		t.Parallel()
		// Regla 1, defensive: a hydrated inconsistent piso folio with an
		// article still in listo_entrega is refused. Delivering it would let
		// R4 freeze the article without an outcome.
		g, a := hidratarFolio(t, hidratarParams{
			origen:    domain.OrigenFolioPiso,
			estado:    domain.EstadoFolioListoEntrega,
			etapa:     domain.EtapaListoEntrega,
			ubicacion: domain.UbicacionAlmacenRevision,
		})
		pendientes := g.EventosPendientesCount()
		err := g.Entregar(actorEntrega("juan"), fixed)
		if !errors.Is(err, domain.ErrArticuloPisoSinReingresar) {
			t.Fatalf("want ErrArticuloPisoSinReingresar, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioListoEntrega {
			t.Fatalf("estado mutado: %q", g.Estado())
		}
		if a.Etapa() != domain.EtapaListoEntrega || a.Ubicacion() != domain.UbicacionAlmacenRevision {
			t.Fatalf("artículo mutado: %q/%q", a.Etapa(), a.Ubicacion())
		}
		if g.EventosPendientesCount() != pendientes {
			t.Fatalf("evento encolado pese al rechazo")
		}
	})

	t.Run("piso_reingresado_no_mueve_articulos", func(t *testing.T) {
		t.Parallel()
		// entregado only exists for cliente items (R1): a piso folio records the
		// folio fact and moves no article.
		g, a := hidratarFolio(t, hidratarParams{
			origen:    domain.OrigenFolioPiso,
			estado:    domain.EstadoFolioListoEntrega,
			etapa:     domain.EtapaReingresadoInventario,
			ubicacion: domain.UbicacionAlmacenRevision,
		})
		pendientes := g.EventosPendientesCount()
		if err := g.Entregar(actorEntrega("juan"), fixed); err != nil {
			t.Fatalf("Entregar: %v", err)
		}
		if a.Etapa() != domain.EtapaReingresadoInventario || a.Ubicacion() != domain.UbicacionAlmacenRevision {
			t.Fatalf("el artículo de piso se movió: %q/%q", a.Etapa(), a.Ubicacion())
		}
		if got := g.EventosPendientesCount() - pendientes; got != 1 {
			t.Fatalf("eventos encolados = %d, want 1 (folio_entregado)", got)
		}
		if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioEntregado {
			t.Fatalf("último evento = %q, want folio_entregado", e.Tipo())
		}
	})

	t.Run("sin_gps", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioListoEntrega,
			etapa: domain.EtapaListoEntrega,
		})
		pendientes := g.EventosPendientesCount()
		err := g.Entregar(actor("juan"), fixed)
		if !errors.Is(err, domain.ErrEntregaGPSRequerido) {
			t.Fatalf("want ErrEntregaGPSRequerido, got %v", err)
		}
		if a.Etapa() != domain.EtapaListoEntrega || g.Estado() != domain.EstadoFolioListoEntrega || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})

	t.Run("gps_a_medias", func(t *testing.T) {
		t.Parallel()
		g, _ := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioListoEntrega,
			etapa: domain.EtapaListoEntrega,
		})
		lat := 19.4
		actor := domain.ActorParams{Usuario: "juan", ClaveIdempotencia: claveValida, DeviceCreatedAt: fixed, GPSLat: &lat}
		if err := g.Entregar(actor, fixed); !errors.Is(err, domain.ErrEntregaGPSRequerido) {
			t.Fatalf("want ErrEntregaGPSRequerido, got %v", err)
		}
	})

	t.Run("estado_incorrecto", func(t *testing.T) {
		t.Parallel()
		g, _ := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioEnProceso, etapa: domain.EtapaListoEntrega,
		})
		pendientes := g.EventosPendientesCount()
		err := g.Entregar(actorEntrega("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
			t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
		}
		if g.EventosPendientesCount() != pendientes {
			t.Fatalf("evento encolado pese al rechazo")
		}
	})

	t.Run("actor_invalido_con_listo_entrega", func(t *testing.T) {
		t.Parallel()
		// The per-article event validates the actor: a bad one fails before any
		// mutation.
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioListoEntrega,
			etapa: domain.EtapaListoEntrega,
		})
		pendientes := g.EventosPendientesCount()
		malo := actorEntrega("")
		err := g.Entregar(malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaListoEntrega || g.Estado() != domain.EstadoFolioListoEntrega || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})

	t.Run("actor_invalido_sin_listo_entrega", func(t *testing.T) {
		t.Parallel()
		// With nothing to deliver, the folio event is the one that validates the
		// actor.
		g, _ := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioPiso, estado: domain.EstadoFolioListoEntrega,
			etapa: domain.EtapaReingresadoInventario,
		})
		pendientes := g.EventosPendientesCount()
		err := g.Entregar(actorEntrega(""), fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.EventosPendientesCount() != pendientes {
			t.Fatalf("evento encolado pese al rechazo")
		}
	})
}
