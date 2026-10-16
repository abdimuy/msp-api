package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// hidratarParams describes a folio rebuilt straight from persisted rows, so a
// test can start anywhere in the lifecycle without walking the machine. The
// zero value of each field is replaced by a sensible default (piso, en_proceso,
// original, almacen_revision), never validated: this stands in for the repo.
type hidratarParams struct {
	origen    domain.OrigenFolio
	estado    domain.EstadoFolio
	etapa     domain.Etapa
	ubicacion domain.Ubicacion
	rol       domain.RolArticulo
	ruta      *domain.RutaReparacion
	dictamen  *domain.Dictamen
	desenlace *domain.Desenlace
}

// articuloHidratado is one row of an article to rebuild, for the tests that
// need more than one (a delivery with an article in standby, for instance).
type articuloHidratado struct {
	rol       domain.RolArticulo
	etapa     domain.Etapa
	ubicacion domain.Ubicacion
	ruta      *domain.RutaReparacion
	dictamen  *domain.Dictamen
	desenlace *domain.Desenlace
}

// hidratarFolio rebuilds a folio with a single article from the given shape.
func hidratarFolio(t *testing.T, p hidratarParams) (*domain.Garantia, *domain.Articulo) {
	t.Helper()
	g, arts := hidratarFolioCon(t, p.origen, p.estado, articuloHidratado{
		rol: p.rol, etapa: p.etapa, ubicacion: p.ubicacion,
		ruta: p.ruta, dictamen: p.dictamen, desenlace: p.desenlace,
	})
	return g, arts[0]
}

// hidratarFolioCon rebuilds a folio in a chosen state with the listed articles.
func hidratarFolioCon(t *testing.T, origen domain.OrigenFolio, estado domain.EstadoFolio, arts ...articuloHidratado) (*domain.Garantia, []*domain.Articulo) {
	t.Helper()
	if origen == "" {
		origen = domain.OrigenFolioPiso
	}
	if estado == "" {
		estado = domain.EstadoFolioEnProceso
	}
	gID := uuid.New()
	articulos := make([]*domain.Articulo, 0, len(arts))
	for _, p := range arts {
		rol := p.rol
		if rol == "" {
			rol = domain.RolArticuloOriginal
		}
		etapa := p.etapa
		if etapa == "" {
			etapa = domain.EtapaEnRevision
		}
		ubicacion := p.ubicacion
		if ubicacion == "" {
			ubicacion = domain.UbicacionAlmacenRevision
		}
		articulos = append(articulos, domain.HydrateArticulo(domain.HydrateArticuloParams{
			ID:          uuid.New(),
			GarantiaID:  gID,
			Rol:         rol,
			Clave:       "CLAVE",
			Description: "silla",
			Ruta:        p.ruta,
			Etapa:       etapa,
			Ubicacion:   ubicacion,
			Dictamen:    p.dictamen,
			Desenlace:   p.desenlace,
			CreatedAt:   fixed,
			UpdatedAt:   fixed,
		}))
	}
	g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:          gID,
		Folio:       domain.Folio("GA-000042"),
		Origen:      origen,
		Estado:      estado,
		Description: "silla rota",
		AbiertoPor:  "juan",
		CreatedAt:   fixed,
		UpdatedAt:   fixed,
		Articulos:   articulos,
	})
	return g, articulos
}

// eventoDeArticulo returns the last pending event scoped to the article.
func eventoDeArticulo(t *testing.T, g *domain.Garantia, id uuid.UUID) *domain.Evento {
	t.Helper()
	var found *domain.Evento
	for e := range g.EventosPendientes() {
		if ref := e.ArticuloRef(); ref != nil && *ref == id {
			found = e
		}
	}
	if found == nil {
		t.Fatalf("no pending event for articulo %s", id)
	}
	return found
}

// TestUbicacionAutomatica_PorEtapa walks each R3 row that a stage change can
// reach, and checks both the article's new location and the from/to pair the
// stage event records.
func TestUbicacionAutomatica_PorEtapa(t *testing.T) {
	t.Parallel()
	rutaTaller := domain.RutaReparacionTaller
	casos := []struct {
		nombre         string
		origen         domain.OrigenFolio
		desde, hasta   domain.Etapa
		inicio, quiero domain.Ubicacion
		ruta           *domain.RutaReparacion
	}{
		{
			"recolectado", domain.OrigenFolioCliente,
			domain.EtapaPendienteRecoleccion, domain.EtapaRecolectado,
			domain.UbicacionDomicilioCliente, domain.UbicacionEnTransito, nil,
		},
		{
			"en_revision", domain.OrigenFolioPiso,
			domain.EtapaRegistrado, domain.EtapaEnRevision,
			domain.UbicacionEnTransito, domain.UbicacionAlmacenRevision, nil,
		},
		{
			"enviado_proveedor", domain.OrigenFolioCliente,
			domain.EtapaOrdenGenerada, domain.EtapaEnviadoProveedor,
			domain.UbicacionAlmacenRevision, domain.UbicacionProveedor, nil,
		},
		{
			"en_taller", domain.OrigenFolioPiso,
			domain.EtapaEnRevision, domain.EtapaEnTaller,
			domain.UbicacionAlmacenRevision, domain.UbicacionTaller, &rutaTaller,
		},
		{
			"reingresado_inventario", domain.OrigenFolioPiso,
			domain.EtapaEnRevision, domain.EtapaReingresadoInventario,
			domain.UbicacionTaller, domain.UbicacionAlmacenRevision, nil,
		},
	}

	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			g, a := hidratarFolio(t, hidratarParams{
				origen: tc.origen, etapa: tc.desde, ubicacion: tc.inicio, ruta: tc.ruta,
			})
			if err := g.AvanzarArticulo(a.ID(), tc.hasta, actor("juan"), fixed); err != nil {
				t.Fatalf("AvanzarArticulo(%s -> %s): %v", tc.desde, tc.hasta, err)
			}
			if a.Ubicacion() != tc.quiero {
				t.Fatalf("ubicacion = %q, want %q", a.Ubicacion(), tc.quiero)
			}
			e := lastEvent(t, g)
			if e.Tipo() != domain.TipoEventoEtapaAvanzada {
				t.Fatalf("evento = %q, want etapa_avanzada", e.Tipo())
			}
			if e.UbicacionDesde() == nil || *e.UbicacionDesde() != tc.inicio {
				t.Fatalf("UbicacionDesde = %v, want %q", e.UbicacionDesde(), tc.inicio)
			}
			if e.UbicacionHasta() == nil || *e.UbicacionHasta() != tc.quiero {
				t.Fatalf("UbicacionHasta = %v, want %q", e.UbicacionHasta(), tc.quiero)
			}
		})
	}
}

// TestUbicacionAutomatica_Entregado needs Entregar (the folio moves the
// article, per R2) and still must record the from/to pair from the article's
// event.
func TestUbicacionAutomatica_Entregado(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{
		origen:    domain.OrigenFolioCliente,
		estado:    domain.EstadoFolioListoEntrega,
		etapa:     domain.EtapaListoEntrega,
		ubicacion: domain.UbicacionTaller,
	})
	if err := g.Entregar(actorEntrega("juan"), fixed); err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if a.Ubicacion() != domain.UbicacionEntregado {
		t.Fatalf("ubicacion = %q, want entregado", a.Ubicacion())
	}
	e := eventoDeArticulo(t, g, a.ID())
	if e.UbicacionDesde() == nil || *e.UbicacionDesde() != domain.UbicacionTaller {
		t.Fatalf("UbicacionDesde = %v, want taller", e.UbicacionDesde())
	}
	if e.UbicacionHasta() == nil || *e.UbicacionHasta() != domain.UbicacionEntregado {
		t.Fatalf("UbicacionHasta = %v, want entregado", e.UbicacionHasta())
	}
}

// TestUbicacionAutomatica_Paralelo walks the three outcomes of the standby
// branch, which move the item to the warehouse, the second-hand store or baja.
func TestUbicacionAutomatica_Paralelo(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		d      domain.Desenlace
		quiero domain.Ubicacion
	}{
		{"segunda_mano", domain.DesenlaceSegundaMano, domain.UbicacionAlmacenSegundaMano},
		{"desarmado", domain.DesenlaceDesarmado, domain.UbicacionBaja},
		{"merma", domain.DesenlaceMerma, domain.UbicacionBaja},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaStandby, ubicacion: domain.UbicacionTaller,
			})
			if err := g.RegistrarDesenlace(a.ID(), tc.d, actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("RegistrarDesenlace(%s): %v", tc.d, err)
			}
			if a.Ubicacion() != tc.quiero {
				t.Fatalf("ubicacion = %q, want %q", a.Ubicacion(), tc.quiero)
			}
			e := lastEvent(t, g)
			if e.UbicacionDesde() == nil || *e.UbicacionDesde() != domain.UbicacionTaller {
				t.Fatalf("UbicacionDesde = %v, want taller", e.UbicacionDesde())
			}
			if e.UbicacionHasta() == nil || *e.UbicacionHasta() != tc.quiero {
				t.Fatalf("UbicacionHasta = %v, want %q", e.UbicacionHasta(), tc.quiero)
			}
		})
	}
}

// siNoCambia asserts a stage event left the article where it was and recorded
// no location pair — the deliberate "no cambia" of R3.
func siNoCambia(t *testing.T, a *domain.Articulo, e *domain.Evento, quiero domain.Ubicacion) {
	t.Helper()
	if a.Ubicacion() != quiero {
		t.Fatalf("ubicacion = %q, want %q (la etapa no debía moverla)", a.Ubicacion(), quiero)
	}
	if e.UbicacionDesde() != nil || e.UbicacionHasta() != nil {
		t.Fatalf("evento con ubicación = %v/%v, want nil/nil", e.UbicacionDesde(), e.UbicacionHasta())
	}
}

// TestUbicacionAutomatica_NoCambia pins the "no cambia" side of R3 on the three
// stages that must not move the item, each reached by its real command.
func TestUbicacionAutomatica_NoCambia(t *testing.T) {
	t.Parallel()
	t.Run("dictamen_recibido", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaEnviadoProveedor, ubicacion: domain.UbicacionProveedor,
		})
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaDictamenRecibido, actor("juan"), fixed); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		siNoCambia(t, a, lastEvent(t, g), domain.UbicacionProveedor)
	})
	t.Run("reparado_proveedor", func(t *testing.T) {
		t.Parallel()
		ruta := domain.RutaReparacionProveedor
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaDictamenRecibido,
			ubicacion: domain.UbicacionProveedor, ruta: &ruta,
		})
		if err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actor("juan"), fixed); err != nil {
			t.Fatalf("RegistrarDictamen: %v", err)
		}
		siNoCambia(t, a, lastEvent(t, g), domain.UbicacionProveedor)
	})
	t.Run("listo_entrega", func(t *testing.T) {
		t.Parallel()
		ruta := domain.RutaReparacionTaller
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, etapa: domain.EtapaReparadoTaller,
			ubicacion: domain.UbicacionTaller, ruta: &ruta,
		})
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaListoEntrega, actor("juan"), fixed); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		siNoCambia(t, a, lastEvent(t, g), domain.UbicacionTaller)
	})
}

// TestMoverArticulo_DestinosPermitidos covers each location the manual command
// may set, and that the articulo_movido event carries the from/to pair.
func TestMoverArticulo_DestinosPermitidos(t *testing.T) {
	t.Parallel()
	permitidas := []domain.Ubicacion{
		domain.UbicacionEnTransito,
		domain.UbicacionAlmacenRevision,
		domain.UbicacionTaller,
		domain.UbicacionProveedor,
		domain.UbicacionAlmacenSegundaMano,
	}
	for _, hasta := range permitidas {
		t.Run(hasta.String(), func(t *testing.T) {
			t.Parallel()
			inicio := domain.UbicacionTaller
			if hasta == domain.UbicacionTaller {
				inicio = domain.UbicacionProveedor
			}
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: inicio,
			})
			if err := g.MoverArticulo(a.ID(), hasta, actor("juan"), fixed); err != nil {
				t.Fatalf("MoverArticulo(%s): %v", hasta, err)
			}
			if a.Ubicacion() != hasta {
				t.Fatalf("ubicacion = %q, want %q", a.Ubicacion(), hasta)
			}
			if a.Etapa() != domain.EtapaEnRevision {
				t.Fatalf("la etapa no debía cambiar: %q", a.Etapa())
			}
			e := lastEvent(t, g)
			if e.Tipo() != domain.TipoEventoArticuloMovido {
				t.Fatalf("evento = %q, want articulo_movido", e.Tipo())
			}
			if e.UbicacionDesde() == nil || *e.UbicacionDesde() != inicio {
				t.Fatalf("UbicacionDesde = %v, want %q", e.UbicacionDesde(), inicio)
			}
			if e.UbicacionHasta() == nil || *e.UbicacionHasta() != hasta {
				t.Fatalf("UbicacionHasta = %v, want %q", e.UbicacionHasta(), hasta)
			}
		})
	}
}

// TestMoverArticulo_DestinosProhibidos pins that only a stage may set entregado
// or baja, and that domicilio_cliente is not a manual destination either.
func TestMoverArticulo_DestinosProhibidos(t *testing.T) {
	t.Parallel()
	prohibidas := []domain.Ubicacion{
		domain.UbicacionEntregado,
		domain.UbicacionBaja,
		domain.UbicacionDomicilioCliente,
		"",
	}
	for _, hasta := range prohibidas {
		t.Run(string(hasta)+"_prohibida", func(t *testing.T) {
			t.Parallel()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision,
			})
			pendientes := g.EventosPendientesCount()
			err := g.MoverArticulo(a.ID(), hasta, actor("juan"), fixed)
			if !errors.Is(err, domain.ErrUbicacionManualNoPermitida) {
				t.Fatalf("MoverArticulo(%q): want ErrUbicacionManualNoPermitida, got %v", hasta, err)
			}
			if a.Ubicacion() != domain.UbicacionAlmacenRevision || g.EventosPendientesCount() != pendientes {
				t.Fatalf("mutación/evento pese al rechazo")
			}
		})
	}
}

// TestMoverArticulo_MismaUbicacion rejects a move that moves nothing.
func TestMoverArticulo_MismaUbicacion(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision,
	})
	pendientes := g.EventosPendientesCount()
	err := g.MoverArticulo(a.ID(), domain.UbicacionAlmacenRevision, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrUbicacionSinCambio) {
		t.Fatalf("want ErrUbicacionSinCambio, got %v", err)
	}
	if g.EventosPendientesCount() != pendientes {
		t.Fatalf("evento encolado pese al rechazo")
	}
}

// TestMoverArticulo_Terminal rejects moving an article whose path is over.
func TestMoverArticulo_Terminal(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioCliente, etapa: domain.EtapaEntregado, ubicacion: domain.UbicacionEntregado,
	})
	pendientes := g.EventosPendientesCount()
	err := g.MoverArticulo(a.ID(), domain.UbicacionTaller, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrArticuloTerminalNoSeMueve) {
		t.Fatalf("want ErrArticuloTerminalNoSeMueve, got %v", err)
	}
	if a.Ubicacion() != domain.UbicacionEntregado || g.EventosPendientesCount() != pendientes {
		t.Fatalf("mutación/evento pese al rechazo")
	}
}

// TestMoverArticulo_Errores covers the two guards that run before the move
// itself: an unknown article and an actor that cannot produce an event.
func TestMoverArticulo_Errores(t *testing.T) {
	t.Parallel()

	t.Run("articulo_inexistente", func(t *testing.T) {
		t.Parallel()
		g, _ := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision,
		})
		err := g.MoverArticulo(uuid.New(), domain.UbicacionTaller, actor("juan"), fixed)
		if !errors.Is(err, domain.ErrArticuloNoEncontrado) {
			t.Fatalf("want ErrArticuloNoEncontrado, got %v", err)
		}
	})

	t.Run("actor_invalido", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision,
		})
		pendientes := g.EventosPendientesCount()
		err := g.MoverArticulo(a.ID(), domain.UbicacionTaller, actor(""), fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Ubicacion() != domain.UbicacionAlmacenRevision || g.EventosPendientesCount() != pendientes {
			t.Fatalf("mutación/evento pese al rechazo")
		}
	})
}
