package garantias_test

import (
	"strings"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestPermisos_CatalogoCompleto pins spec §7: five codes, no more, no fewer.
// A sixth constant added to the domain enum without a line here would ship a
// permission the auth catalog never grants, and the role screen would show one
// less than the code implies.
func TestPermisos_CatalogoCompleto(t *testing.T) {
	t.Parallel()
	quiere := map[string]bool{
		"garantias:leer":       true,
		"garantias:crear":      true,
		"garantias:actualizar": true,
		"garantias:autorizar":  true,
		"garantias:cerrar":     true,
	}

	got := garantias.Permisos()
	if len(got) != len(quiere) {
		t.Fatalf("len(Permisos()) = %d, want %d", len(got), len(quiere))
	}
	for _, p := range got {
		if !quiere[p.Codigo] {
			t.Errorf("código inesperado %q", p.Codigo)
		}
		delete(quiere, p.Codigo)
		if strings.TrimSpace(p.Descripcion) == "" {
			t.Errorf("%s: descripción vacía", p.Codigo)
		}
	}
	for falta := range quiere {
		t.Errorf("falta el código %q", falta)
	}
}

// TestPermisos_CodigosPasanPorElDominio round-trips every code the contract
// publishes through domain.ParsePermiso, so the string the composition root
// registers in auth is provably one the domain accepts.
func TestPermisos_CodigosPasanPorElDominio(t *testing.T) {
	t.Parallel()
	for _, p := range garantias.Permisos() {
		t.Run(p.Codigo, func(t *testing.T) {
			t.Parallel()
			parsed, err := domain.ParsePermiso(p.Codigo)
			if err != nil {
				t.Fatalf("ParsePermiso(%q): %v", p.Codigo, err)
			}
			if parsed.String() != p.Codigo {
				t.Errorf("round-trip = %q, want %q", parsed.String(), p.Codigo)
			}
		})
	}
}

// TestPermisos_OrdenEstable guards the documented promise: the composition root
// renders these in order, and a map iteration would shuffle the roles screen on
// every page load.
func TestPermisos_OrdenEstable(t *testing.T) {
	t.Parallel()
	want := []string{
		"garantias:leer",
		"garantias:crear",
		"garantias:actualizar",
		"garantias:autorizar",
		"garantias:cerrar",
	}
	got := garantias.Permisos()
	for i, codigo := range want {
		if got[i].Codigo != codigo {
			t.Errorf("Permisos()[%d] = %q, want %q", i, got[i].Codigo, codigo)
		}
	}
}

// TestPermisos_NoAlias: the slice is rebuilt per call, so a caller that sorts
// or filters it in place cannot corrupt the next one.
func TestPermisos_NoAlias(t *testing.T) {
	t.Parallel()
	primero := garantias.Permisos()
	if len(primero) == 0 {
		t.Fatal("Permisos() vacío")
	}
	primero[0].Codigo = "manipulado"

	segundo := garantias.Permisos()
	if segundo[0].Codigo == "manipulado" {
		t.Error("Permisos() devuelve un slice compartido entre llamadas")
	}
	if segundo[0].Codigo != "garantias:leer" {
		t.Errorf("segundo[0] = %q, want garantias:leer", segundo[0].Codigo)
	}
}
