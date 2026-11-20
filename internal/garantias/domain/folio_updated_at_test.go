package domain_test

import (
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// ahoraT4 is the clock of the single mutator each test exercises. Every setup
// step stamps the aggregate at fixed, so writing this distinct hour is the only
// way to prove the target mutator moved UpdatedAt itself: sharing the setup
// clock would let a dropped MarkUpdatedAt hide behind the previous stamp.
var ahoraT4 = fixed.Add(time.Hour)

// exigirUpdatedAtFolio fails when the folio did not take the target clock. The
// message is deliberately short: with one test per mutator, the test name is the
// mutant that just survived.
func exigirUpdatedAtFolio(t *testing.T, g *domain.Garantia) {
	t.Helper()
	if got := g.UpdatedAt(); !got.Equal(ahoraT4) {
		t.Fatalf("UpdatedAt() = %v, want %v (el mutador no llamó MarkUpdatedAt)", got, ahoraT4)
	}
}

// TestFolioUpdatedAt_AgregarArticulo cubre el mutante T4a.
func TestFolioUpdatedAt_AgregarArticulo(t *testing.T) {
	t.Parallel()
	g := openPiso(t)

	if err := g.AgregarArticulo(domain.AgregarArticuloParams{Description: "silla"}, actor("juan"), ahoraT4); err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_AvanzarArticulo cubre el mutante T4b.
func TestFolioUpdatedAt_AvanzarArticulo(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	iniciar(t, g)

	if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), ahoraT4); err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_RegistrarDiagnostico cubre el mutante T4c.
func TestFolioUpdatedAt_RegistrarDiagnostico(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{etapa: domain.EtapaEnRevision})

	if err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, actorDecisor("decisor"), ahoraT4); err != nil {
		t.Fatalf("RegistrarDiagnostico: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_RegistrarDictamen cubre el mutante T4d.
func TestFolioUpdatedAt_RegistrarDictamen(t *testing.T) {
	t.Parallel()
	g, a := supplierToVerdict(t)

	if err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actorDecisor("decisor"), ahoraT4); err != nil {
		t.Fatalf("RegistrarDictamen: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_AutorizarCambioFisico cubre el mutante T4e.
func TestFolioUpdatedAt_AutorizarCambioFisico(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{etapa: domain.EtapaEnTaller, ubicacion: domain.UbicacionTaller})

	if err := g.AutorizarCambioFisico(a.ID(), actorDecisor("decisor"), ahoraT4); err != nil {
		t.Fatalf("AutorizarCambioFisico: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_RegistrarDesenlace cubre el mutante T4f.
func TestFolioUpdatedAt_RegistrarDesenlace(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{etapa: domain.EtapaStandby, ubicacion: domain.UbicacionTaller})

	if err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceMerma, actorDecisor("decisor"), ahoraT4); err != nil {
		t.Fatalf("RegistrarDesenlace: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_MarcarNoDevuelto cubre el mutante T4g.
func TestFolioUpdatedAt_MarcarNoDevuelto(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{etapa: domain.EtapaEsperaRespuestaCliente})

	if err := g.MarcarNoDevuelto(a.ID(), actorDecisor("decisor"), ahoraT4); err != nil {
		t.Fatalf("MarcarNoDevuelto: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_MoverArticulo cubre el mutante T4h.
func TestFolioUpdatedAt_MoverArticulo(t *testing.T) {
	t.Parallel()
	g, a := hidratarFolio(t, hidratarParams{etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision})

	if err := g.MoverArticulo(a.ID(), domain.UbicacionTaller, actor("juan"), ahoraT4); err != nil {
		t.Fatalf("MoverArticulo: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_IniciarProceso cubre el mutante T4i.
func TestFolioUpdatedAt_IniciarProceso(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	addArticle(t, g, "silla")

	if err := g.IniciarProceso(actor("juan"), ahoraT4); err != nil {
		t.Fatalf("IniciarProceso: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_MarcarListoEntrega cubre el mutante T4j.
func TestFolioUpdatedAt_MarcarListoEntrega(t *testing.T) {
	t.Parallel()
	g, _ := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioCliente, estado: domain.EstadoFolioEnProceso,
		etapa: domain.EtapaListoEntrega, ubicacion: domain.UbicacionTaller,
	})

	if err := g.MarcarListoEntrega(actor("juan"), ahoraT4); err != nil {
		t.Fatalf("MarcarListoEntrega: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_Entregar cubre el mutante T4k.
func TestFolioUpdatedAt_Entregar(t *testing.T) {
	t.Parallel()
	g, _ := folioListoParaEntregar(t)

	if err := g.Entregar(firmaPrueba, actorEntrega("juan"), ahoraT4); err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_Cerrar cubre el mutante T4l.
func TestFolioUpdatedAt_Cerrar(t *testing.T) {
	t.Parallel()
	g := folioEntregado(t)
	entrega := puntoGPS(0)

	if err := g.Cerrar(&entrega, nil, "", actor("juan"), ahoraT4); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}

// TestFolioUpdatedAt_Cancelar cubre el mutante T4m.
func TestFolioUpdatedAt_Cancelar(t *testing.T) {
	t.Parallel()
	g, _ := hidratarFolio(t, hidratarParams{etapa: domain.EtapaEnRevision, ubicacion: domain.UbicacionAlmacenRevision})

	if err := g.Cancelar("cliente desistió", actor("juan"), ahoraT4); err != nil {
		t.Fatalf("Cancelar: %v", err)
	}
	exigirUpdatedAtFolio(t, g)
}
