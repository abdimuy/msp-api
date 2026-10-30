package domain_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// idsDe returns the ids of a list of articles, for readable failure messages.
func idsDe(arts []*domain.Articulo) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(arts))
	for _, a := range arts {
		out = append(out, a.ID())
	}
	return out
}

// contieneArticulo reports whether the article id is in the list.
func contieneArticulo(arts []*domain.Articulo, id uuid.UUID) bool {
	for _, a := range arts {
		if a.ID() == id {
			return true
		}
	}
	return false
}

// TestR10_HidratadoEsLimpio: HydrateArticulo rebuilds a persisted row as clean —
// neither new nor modified — so a read-only load hands the repository nothing to
// write, the whole point of R10.
func TestR10_HidratadoEsLimpio(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{})

	if a.EsNuevo() {
		t.Error("hydrated article reported as nuevo")
	}
	if a.EstaModificado() {
		t.Error("hydrated untouched article reported as modificado")
	}
	if n := g.ArticulosNuevosForRepo(); len(n) != 0 {
		t.Errorf("ArticulosNuevosForRepo() = %v, want empty", idsDe(n))
	}
	if m := g.ArticulosModificadosForRepo(); len(m) != 0 {
		t.Errorf("ArticulosModificadosForRepo() = %v, want empty", idsDe(m))
	}
}

// TestR10_ArticuloNuevo: a fresh AgregarArticulo is nuevo and NOT modificado, so
// Guardar inserts it once and a later session does not also update it.
func TestR10_ArticuloNuevo(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")

	if !a.EsNuevo() {
		t.Error("AgregarArticulo article not reported as nuevo")
	}
	if a.EstaModificado() {
		t.Error("nuevo article also reported as modificado")
	}
	nuevos := g.ArticulosNuevosForRepo()
	if !contieneArticulo(nuevos, a.ID()) || len(nuevos) != 1 {
		t.Fatalf("ArticulosNuevosForRepo() = %v, want [%s]", idsDe(nuevos), a.ID())
	}
	if m := g.ArticulosModificadosForRepo(); len(m) != 0 {
		t.Errorf("ArticulosModificadosForRepo() = %v, want empty", idsDe(m))
	}
}

// TestR10_NuevoSigueNuevoTrasMutar: mutating an article born in this session
// keeps it nuevo — it is an INSERT, not an UPDATE — never modificado.
func TestR10_NuevoSigueNuevoTrasMutar(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
		t.Fatalf("IniciarProceso: %v", err)
	}
	if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), fixed); err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}

	got := articuloByID(t, g, a.ID())
	if !got.EsNuevo() {
		t.Error("nuevo article stopped being nuevo after a mutator")
	}
	if got.EstaModificado() {
		t.Error("nuevo article became modificado after a mutator")
	}
	if m := g.ArticulosModificadosForRepo(); len(m) != 0 {
		t.Errorf("ArticulosModificadosForRepo() = %v, want empty", idsDe(m))
	}
}

// TestR10_MutadorMarcaModificado drives every article mutator over a hydrated
// article and checks the touched article turns modificado and shows up in
// ArticulosModificadosForRepo, while anything else stays clean. This is the R10
// wiring for each mutator: a mutator that forgets tocar leaves the list short.
func TestR10_MutadorMarcaModificado(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		mutar  func(t *testing.T) (*domain.Garantia, *domain.Articulo)
	}{
		{"avanzar", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaRegistrado,
				ubicacion: domain.UbicacionAlmacenRevision,
			})
			if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), fixed); err != nil {
				t.Fatalf("AvanzarArticulo: %v", err)
			}
			return g, a
		}},
		{"diagnostico", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision,
				ubicacion: domain.UbicacionAlmacenRevision,
			})
			if err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("RegistrarDiagnostico: %v", err)
			}
			return g, a
		}},
		{"dictamen", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			ruta := domain.RutaReparacionProveedor
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaDictamenRecibido,
				ubicacion: domain.UbicacionProveedor, ruta: &ruta,
			})
			if err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("RegistrarDictamen: %v", err)
			}
			return g, a
		}},
		{"autorizar_cambio", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnTaller,
				ubicacion: domain.UbicacionTaller,
			})
			if err := g.AutorizarCambioFisico(a.ID(), actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("AutorizarCambioFisico: %v", err)
			}
			return g, a
		}},
		{"desenlace", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaStandby,
				ubicacion: domain.UbicacionTaller,
			})
			if err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("RegistrarDesenlace: %v", err)
			}
			return g, a
		}},
		{"marcar_no_devuelto", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioCliente, etapa: domain.EtapaEsperaRespuestaCliente,
				ubicacion: domain.UbicacionTaller,
			})
			if err := g.MarcarNoDevuelto(a.ID(), actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("MarcarNoDevuelto: %v", err)
			}
			return g, a
		}},
		{"entregar", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioListoEntrega,
				etapa: domain.EtapaListoEntrega, ubicacion: domain.UbicacionAlmacenRevision,
			})
			if err := g.Entregar(actorEntrega("juan"), fixed); err != nil {
				t.Fatalf("Entregar: %v", err)
			}
			return g, a
		}},
		{"mover_articulo", func(t *testing.T) (*domain.Garantia, *domain.Articulo) {
			t.Helper()
			g, a := hidratarFolio(t, hidratarParams{
				origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision,
				ubicacion: domain.UbicacionAlmacenRevision,
			})
			if err := g.MoverArticulo(a.ID(), domain.UbicacionTaller, actor("juan"), fixed); err != nil {
				t.Fatalf("MoverArticulo: %v", err)
			}
			return g, a
		}},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			g, a := c.mutar(t)

			if a.EsNuevo() {
				t.Error("mutated hydrated article reported as nuevo")
			}
			if !a.EstaModificado() {
				t.Error("mutated article not reported as modificado")
			}
			mods := g.ArticulosModificadosForRepo()
			if !contieneArticulo(mods, a.ID()) {
				t.Fatalf("ArticulosModificadosForRepo() = %v, want to contain %s", idsDe(mods), a.ID())
			}
			if contieneArticulo(g.ArticulosNuevosForRepo(), a.ID()) {
				t.Errorf("mutated hydrated article %s appeared in ArticulosNuevosForRepo()", a.ID())
			}
		})
	}
}

// TestR10_ListadosSonCopias: both accessors hand out a fresh slice, so a caller
// that reorders or truncates it cannot corrupt the aggregate.
func TestR10_ListadosSonCopias(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioPiso, etapa: domain.EtapaRegistrado,
		ubicacion: domain.UbicacionAlmacenRevision,
	})
	if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), fixed); err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}

	mods := g.ArticulosModificadosForRepo()
	if len(mods) != 1 {
		t.Fatalf("ArticulosModificadosForRepo() = %v, want exactly the mutated article", idsDe(mods))
	}
	mods[0] = nil
	segundo := g.ArticulosModificadosForRepo()
	if len(segundo) != 1 || segundo[0] == nil {
		t.Fatalf("ArticulosModificadosForRepo() shares its backing array between calls: %v", segundo)
	}
}
