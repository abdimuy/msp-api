package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// r4Permite is the hand-written R4 oracle, independent of folioAdmiteOperacion:
// abierto only takes articles; en_proceso and listo_entrega allow everything
// except that listo_entrega stops taking new ones; the three closed states only
// allow RegistrarDesenlace and MoverArticulo, and only on a standby article
// (all the closed-state cases below stage the article in standby).
func r4Permite(estado domain.EstadoFolio, op string) bool {
	switch estado {
	case domain.EstadoFolioAbierto:
		return op == "agregar"
	case domain.EstadoFolioEnProceso:
		return true
	case domain.EstadoFolioListoEntrega:
		return op != "agregar"
	case domain.EstadoFolioEntregado, domain.EstadoFolioCerrado, domain.EstadoFolioCancelado:
		return op == "desenlace" || op == "mover"
	}
	return false
}

// operacionFolio describes one article-touching operation to drive the R4
// matrix: the stage its article must sit in for the call to make sense, and the
// closure that invokes it.
type operacionFolio struct {
	nombre     string
	etapa      domain.Etapa
	ubicacion  domain.Ubicacion
	ruta       *domain.RutaReparacion
	llamar     func(g *domain.Garantia, a *domain.Articulo) error
	sobreFolio bool // AgregarArticulo does not need the existing article
}

func operacionesFolio() []operacionFolio {
	rutaProveedor := domain.RutaReparacionProveedor
	return []operacionFolio{
		{
			nombre: "agregar",
			llamar: func(g *domain.Garantia, _ *domain.Articulo) error {
				return g.AgregarArticulo(domain.AgregarArticuloParams{Description: "otra"}, actor("juan"), fixed)
			},
			sobreFolio: true,
		},
		{
			nombre: "avanzar",
			etapa:  domain.EtapaRegistrado,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), fixed)
			},
		},
		{
			nombre: "diagnostico",
			etapa:  domain.EtapaEnRevision,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, actorDecisor("juan"), fixed)
			},
		},
		{
			nombre: "dictamen",
			etapa:  domain.EtapaDictamenRecibido,
			ruta:   &rutaProveedor,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actor("juan"), fixed)
			},
		},
		{
			nombre: "cambio",
			etapa:  domain.EtapaEnTaller,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.AutorizarCambioFisico(a.ID(), actorDecisor("juan"), fixed)
			},
		},
		{
			nombre: "desenlace",
			etapa:  domain.EtapaStandby,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, actorDecisor("juan"), fixed)
			},
		},
		{
			nombre:    "mover",
			etapa:     domain.EtapaStandby,
			ubicacion: domain.UbicacionEnTransito,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.MoverArticulo(a.ID(), domain.UbicacionTaller, actor("juan"), fixed)
			},
		},
		{
			nombre: "no_devuelto",
			etapa:  domain.EtapaEsperaRespuestaCliente,
			llamar: func(g *domain.Garantia, a *domain.Articulo) error {
				return g.MarcarNoDevuelto(a.ID(), actorDecisor("juan"), fixed)
			},
		},
	}
}

// TestFolioOperacion_Matriz drives the whole estado × operación table: every
// allowed op runs and every denied op fails with its sentinel, leaving neither a
// mutation nor a queued event. All the fixtures use a piso folio (ajeno al
// camino de recolección) so the article stages are reachable.
func TestFolioOperacion_Matriz(t *testing.T) {
	t.Parallel()
	estados := []domain.EstadoFolio{
		domain.EstadoFolioAbierto,
		domain.EstadoFolioEnProceso,
		domain.EstadoFolioListoEntrega,
		domain.EstadoFolioEntregado,
		domain.EstadoFolioCerrado,
		domain.EstadoFolioCancelado,
	}

	for _, estado := range estados {
		for _, op := range operacionesFolio() {
			t.Run(estado.String()+"_"+op.nombre, func(t *testing.T) {
				t.Parallel()

				g, a := hidratarFolio(t, hidratarParams{
					origen: domain.OrigenFolioPiso, estado: estado,
					etapa: op.etapa, ubicacion: op.ubicacion, ruta: op.ruta,
				})
				etapaAntes := a.Etapa()
				ubicacionAntes := a.Ubicacion()
				articulosAntes := g.ArticulosCount()
				pendientes := g.EventosPendientesCount()

				err := op.llamar(g, a)

				if r4Permite(estado, op.nombre) {
					if err != nil {
						t.Fatalf("want ok, got %v", err)
					}
					if g.Estado() != estado {
						t.Fatalf("el estado del folio cambió: %q, want %q", g.Estado(), estado)
					}
					if g.EventosPendientesCount() == pendientes {
						t.Fatalf("no se encoló ningún evento en una operación permitida")
					}
					return
				}

				centinela := domain.ErrFolioNoAdmiteOperacion
				if op.sobreFolio {
					centinela = domain.ErrFolioNoAdmiteArticulos
				}
				if !errors.Is(err, centinela) {
					t.Fatalf("want %v, got %v", centinela, err)
				}
				if a.Etapa() != etapaAntes || a.Ubicacion() != ubicacionAntes {
					t.Fatalf("el artículo cambió pese al rechazo")
				}
				if g.ArticulosCount() != articulosAntes {
					t.Fatalf("cambió el número de artículos pese al rechazo")
				}
				if g.Estado() != estado || g.EventosPendientesCount() != pendientes {
					t.Fatalf("mutación/evento pese al rechazo")
				}
			})
		}
	}
}

// TestFolioOperacion_EstadoInvalido pins that an unknown folio state (only
// reachable through a corrupted/hydrated row) allows no article operation.
func TestFolioOperacion_EstadoInvalido(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioPiso, estado: domain.EstadoFolio("basura"), etapa: domain.EtapaEnRevision,
	})
	pendientes := g.EventosPendientesCount()
	err := g.AvanzarArticulo(a.ID(), domain.EtapaEnTaller, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrFolioNoAdmiteOperacion) {
		t.Fatalf("want ErrFolioNoAdmiteOperacion, got %v", err)
	}
	if a.Etapa() != domain.EtapaEnRevision || g.EventosPendientesCount() != pendientes {
		t.Fatalf("mutación/evento pese al rechazo")
	}
}

// TestFolioOperacion_ExcepcionStandby names the §3.2 carve-out: a cerrado folio
// still accepts RegistrarDesenlace and MoverArticulo on a standby article, and
// rejects the same two ops on a non-standby one.
func TestFolioOperacion_ExcepcionStandby(t *testing.T) {
	t.Parallel()

	t.Run("cerrado_permite_desenlace_en_standby", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioCerrado, etapa: domain.EtapaStandby,
		})
		if err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, actorDecisor("juan"), fixed); err != nil {
			t.Fatalf("RegistrarDesenlace: %v", err)
		}
		if a.Etapa() != domain.EtapaSegundaMano {
			t.Fatalf("etapa = %q, want segunda_mano", a.Etapa())
		}
	})

	t.Run("cerrado_permite_mover_en_standby", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioCerrado,
			etapa: domain.EtapaStandby, ubicacion: domain.UbicacionTaller,
		})
		if err := g.MoverArticulo(a.ID(), domain.UbicacionAlmacenSegundaMano, actor("juan"), fixed); err != nil {
			t.Fatalf("MoverArticulo: %v", err)
		}
	})

	t.Run("cerrado_rechaza_desenlace_fuera_de_standby", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioCerrado, etapa: domain.EtapaListoEntrega,
		})
		pendientes := g.EventosPendientesCount()
		err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrFolioNoAdmiteOperacion) {
			t.Fatalf("want ErrFolioNoAdmiteOperacion, got %v", err)
		}
		if g.EventosPendientesCount() != pendientes {
			t.Fatalf("evento encolado pese al rechazo")
		}
	})

	t.Run("cerrado_rechaza_mover_fuera_de_standby", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioCerrado,
			etapa: domain.EtapaListoEntrega, ubicacion: domain.UbicacionTaller,
		})
		pendientes := g.EventosPendientesCount()
		err := g.MoverArticulo(a.ID(), domain.UbicacionAlmacenRevision, actor("juan"), fixed)
		if !errors.Is(err, domain.ErrFolioNoAdmiteOperacion) {
			t.Fatalf("want ErrFolioNoAdmiteOperacion, got %v", err)
		}
		if g.EventosPendientesCount() != pendientes {
			t.Fatalf("evento encolado pese al rechazo")
		}
	})
}
