package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewRutaReparacion_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		esProveedor bool
		esTaller    bool
	}{
		{domain.RutaReparacionProveedor, true, false},
		{domain.RutaReparacionTaller, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			r, err := domain.NewRutaReparacion(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if r.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, r.Value())
			}
			if r.EsProveedor() != tc.esProveedor {
				t.Fatalf("EsProveedor mismatch for %q", tc.input)
			}
			if r.EsTaller() != tc.esTaller {
				t.Fatalf("EsTaller mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewRutaReparacion_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Proveedor", "TALLER", "proveedor ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewRutaReparacion(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRutaReparacionInvalida) {
				t.Fatalf("expected ErrRutaReparacionInvalida for %q, got %v", tc, err)
			}
		})
	}
}

func TestRutaReparacion_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	p, _ := domain.NewRutaReparacion(domain.RutaReparacionProveedor)
	p2, _ := domain.NewRutaReparacion(domain.RutaReparacionProveedor)
	tll, _ := domain.NewRutaReparacion(domain.RutaReparacionTaller)

	if !p.Equals(p2) {
		t.Fatal("expected p.Equals(p2) == true")
	}
	if p.Equals(tll) {
		t.Fatal("expected p.Equals(tll) == false")
	}

	zero := domain.HydrateRutaReparacion("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty ruta_reparacion")
	}
	if p.IsZero() {
		t.Fatal("expected IsZero == false for valid ruta_reparacion")
	}
}

func TestRutaReparacion_String(t *testing.T) {
	t.Parallel()
	r, _ := domain.NewRutaReparacion(domain.RutaReparacionTaller)
	if r.String() != domain.RutaReparacionTaller {
		t.Fatalf("expected %q, got %q", domain.RutaReparacionTaller, r.String())
	}
}

func TestHydrateRutaReparacion_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	r := domain.HydrateRutaReparacion("cualquier-cosa")
	if r.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", r.Value())
	}
}
