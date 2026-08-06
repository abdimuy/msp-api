package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestRutaReparacion_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.RutaReparacionProveedor) != "proveedor" {
		t.Errorf("RutaReparacionProveedor = %q, want \"proveedor\"", domain.RutaReparacionProveedor)
	}
	if string(domain.RutaReparacionTaller) != "taller" {
		t.Errorf("RutaReparacionTaller = %q, want \"taller\"", domain.RutaReparacionTaller)
	}
}

func TestParseRutaReparacion_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		expected    domain.RutaReparacion
		esProveedor bool
		esTaller    bool
	}{
		{"proveedor", domain.RutaReparacionProveedor, true, false},
		{"taller", domain.RutaReparacionTaller, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			r, err := domain.ParseRutaReparacion(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if r != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, r)
			}
			if r.EsProveedor() != tc.esProveedor {
				t.Errorf("EsProveedor mismatch for %q", tc.input)
			}
			if r.EsTaller() != tc.esTaller {
				t.Errorf("EsTaller mismatch for %q", tc.input)
			}
			if r.String() != tc.input {
				t.Errorf("String() = %q, want %q", r.String(), tc.input)
			}
		})
	}
}

func TestParseRutaReparacion_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Proveedor", "TALLER", "proveedor ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseRutaReparacion(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRutaReparacionInvalida) {
				t.Fatalf("expected ErrRutaReparacionInvalida for %q, got %v", tc, err)
			}
		})
	}
}

func TestRutaReparacion_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.RutaReparacionProveedor.IsValid() {
		t.Error("RutaReparacionProveedor.IsValid() should be true")
	}
	if !domain.RutaReparacionTaller.IsValid() {
		t.Error("RutaReparacionTaller.IsValid() should be true")
	}
	if domain.RutaReparacion("invalid").IsValid() {
		t.Error("invalid RutaReparacion should not be valid")
	}
}
