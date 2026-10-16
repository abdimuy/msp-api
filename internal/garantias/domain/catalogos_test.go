package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// todosLosOrigenes lists both folio origins once.
func todosLosOrigenes() []domain.OrigenFolio {
	return []domain.OrigenFolio{domain.OrigenFolioCliente, domain.OrigenFolioPiso}
}

// mismasEtapas compares two stage slices as sets, so the catalog order is not
// what this pins (the order is pinned separately).
func mismasEtapas(a, b []domain.Etapa) bool {
	if len(a) != len(b) {
		return false
	}
	cuenta := map[domain.Etapa]int{}
	for _, e := range a {
		cuenta[e]++
	}
	for _, e := range b {
		cuenta[e]--
		if cuenta[e] < 0 {
			return false
		}
	}
	return true
}

// TestR11_SiguientesPorAvanzar_CoincideConLaMaquina crosses the screen catalog
// against the R1+R2 oracle of test 1 for every stage and every origin. The
// catalog is only correct if it says exactly what AvanzarArticulo accepts:
// mutant D1 (dropping the origin filter) and mutant D2 (opening an exit from
// dictamen_recibido) make this fail from the catalog side too, not only from the
// mutator side.
func TestR11_SiguientesPorAvanzar_CoincideConLaMaquina(t *testing.T) {
	t.Parallel()
	for _, origen := range todosLosOrigenes() {
		for _, etapa := range todasLasEtapas() {
			t.Run(string(origen)+"/"+string(etapa), func(t *testing.T) {
				t.Parallel()
				quiero := avanzarEsperado[origen][etapa]
				got := domain.SiguientesPorAvanzar(etapa, origen)
				if !mismasEtapas(got, quiero) {
					t.Errorf("SiguientesPorAvanzar(%s, %s) = %v, want %v", etapa, origen, got, quiero)
				}
			})
		}
	}
}

// TestR11_SiguientesPorAvanzar_NuncaNil: a stage with no advance exit (a terminal
// one, dictamen_recibido, standby) must come back as an empty slice, not nil, so
// the JSON the screen sees is [] and not null.
func TestR11_SiguientesPorAvanzar_NuncaNil(t *testing.T) {
	t.Parallel()
	for _, origen := range todosLosOrigenes() {
		for _, etapa := range todasLasEtapas() {
			got := domain.SiguientesPorAvanzar(etapa, origen)
			if got == nil {
				t.Errorf("SiguientesPorAvanzar(%s, %s) = nil, want non-nil", etapa, origen)
			}
		}
	}
}

// TestR11_SiguientesPorAvanzar_OrdenEstable: the catalog follows the stage
// order, so the screen renders the same list on every call.
func TestR11_SiguientesPorAvanzar_OrdenEstable(t *testing.T) {
	t.Parallel()
	got := domain.SiguientesPorAvanzar(domain.EtapaEnRevision, domain.OrigenFolioPiso)
	quiero := []domain.Etapa{
		domain.EtapaOrdenGenerada, domain.EtapaEnTaller, domain.EtapaReingresadoInventario,
	}
	if len(got) != len(quiero) {
		t.Fatalf("SiguientesPorAvanzar(en_revision, piso) = %v, want %v", got, quiero)
	}
	for i := range quiero {
		if got[i] != quiero[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], quiero[i])
		}
	}
}

// TestR11_Etapas_CatalogoCompletoYEstable pins the stage catalog: every valid
// stage, once, in the lifecycle order. A stage added to etapa.go without the
// ordered list fails on the count; a reorder fails on the index.
func TestR11_Etapas_CatalogoCompletoYEstable(t *testing.T) {
	t.Parallel()
	quiero := todasLasEtapas()
	got := domain.Etapas()

	if len(got) != len(quiero) {
		t.Fatalf("Etapas() = %v (%d), want %v (%d)", got, len(got), quiero, len(quiero))
	}
	for i := range quiero {
		if got[i] != quiero[i] {
			t.Errorf("Etapas()[%d] = %q, want %q", i, got[i], quiero[i])
		}
		if !got[i].IsValid() {
			t.Errorf("Etapas()[%d] = %q is not valid", i, got[i])
		}
	}
}

// TestR11_Ubicaciones_CatalogoCompletoYEstable pins the location catalog the
// same way: every valid location, once, in the design order.
func TestR11_Ubicaciones_CatalogoCompletoYEstable(t *testing.T) {
	t.Parallel()
	quiero := []domain.Ubicacion{
		domain.UbicacionDomicilioCliente, domain.UbicacionEnTransito,
		domain.UbicacionAlmacenRevision, domain.UbicacionTaller,
		domain.UbicacionProveedor, domain.UbicacionAlmacenSegundaMano,
		domain.UbicacionEntregado, domain.UbicacionBaja,
	}
	got := domain.Ubicaciones()

	if len(got) != len(quiero) {
		t.Fatalf("Ubicaciones() = %v (%d), want %v (%d)", got, len(got), quiero, len(quiero))
	}
	for i := range quiero {
		if got[i] != quiero[i] {
			t.Errorf("Ubicaciones()[%d] = %q, want %q", i, got[i], quiero[i])
		}
		if !got[i].IsValid() {
			t.Errorf("Ubicaciones()[%d] = %q is not valid", i, got[i])
		}
	}
}

// TestR11_CatalogosSonCopias: neither catalog hands out the internal slice, so a
// caller that sorts or truncates it cannot corrupt the next reader.
func TestR11_CatalogosSonCopias(t *testing.T) {
	t.Parallel()
	etapas := domain.Etapas()
	etapas[0] = "manipulada"
	if domain.Etapas()[0] == "manipulada" {
		t.Error("Etapas() shares its backing array between calls")
	}

	ubicaciones := domain.Ubicaciones()
	ubicaciones[0] = "manipulada"
	if domain.Ubicaciones()[0] == "manipulada" {
		t.Error("Ubicaciones() shares its backing array between calls")
	}
}
