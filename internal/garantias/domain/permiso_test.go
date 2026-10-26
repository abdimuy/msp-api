package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestParsePermiso(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		in     string
		want   domain.Permiso
	}{
		{"leer", "garantias:leer", domain.PermisoLeer},
		{"crear", "garantias:crear", domain.PermisoCrear},
		{"actualizar", "garantias:actualizar", domain.PermisoActualizar},
		{"autorizar", "garantias:autorizar", domain.PermisoAutorizar},
		{"cerrar", "garantias:cerrar", domain.PermisoCerrar},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			p, err := domain.ParsePermiso(c.in)
			if err != nil {
				t.Fatalf("ParsePermiso(%q): %v", c.in, err)
			}
			if p != c.want {
				t.Errorf("ParsePermiso(%q) = %q, want %q", c.in, p, c.want)
			}
			if !p.IsValid() {
				t.Errorf("%q.IsValid() = false, want true", p)
			}
			if p.String() != c.in {
				t.Errorf("String() = %q, want %q", p.String(), c.in)
			}
		})
	}
}

func TestParsePermiso_Invalido(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"",
		"garantias:borrar",
		"garantias:",
		"ventas:crear",
		"garantias:crear ",
		"GARANTIAS:CREAR",
		"garantias:leer:extra",
	} {
		p, err := domain.ParsePermiso(in)
		if !errors.Is(err, domain.ErrPermisoInvalido) {
			t.Errorf("ParsePermiso(%q) err = %v, want ErrPermisoInvalido", in, err)
		}
		if p != "" {
			t.Errorf("ParsePermiso(%q) = %q, want empty", in, p)
		}
	}
}

func TestPermiso_IsValid_Invalido(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "garantias", "garantias:leer:x", " warranties:leer"} {
		if p := domain.Permiso(in); p.IsValid() {
			t.Errorf("Permiso(%q).IsValid() = true, want false", in)
		}
	}
}

// TestPermiso_LosCincoDelSpec pins the five codes of spec §7 by their exact
// wire value. A rename here is a data migration: MSP_ROLES_PERMISOS.CODIGO is
// persisted, and the role screen would keep showing the old code forever.
func TestPermiso_LosCincoDelSpec(t *testing.T) {
	t.Parallel()
	want := []struct {
		p    domain.Permiso
		code string
	}{
		{domain.PermisoLeer, "garantias:leer"},
		{domain.PermisoCrear, "garantias:crear"},
		{domain.PermisoActualizar, "garantias:actualizar"},
		{domain.PermisoAutorizar, "garantias:autorizar"},
		{domain.PermisoCerrar, "garantias:cerrar"},
	}
	if len(want) != 5 {
		t.Fatalf("len(want) = %d, want 5", len(want))
	}
	for _, c := range want {
		if !c.p.IsValid() {
			t.Errorf("%q no es válido", c.p)
		}
		if c.p.String() != c.code {
			t.Errorf("código = %q, want %q", c.p, c.code)
		}
	}
}
