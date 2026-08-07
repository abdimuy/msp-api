package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestDictamen_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.DictamenAceptada) != "aceptada" {
		t.Errorf("DictamenAceptada = %q, want \"aceptada\"", domain.DictamenAceptada)
	}
	if string(domain.DictamenRechazada) != "rechazada" {
		t.Errorf("DictamenRechazada = %q, want \"rechazada\"", domain.DictamenRechazada)
	}
	if string(domain.DictamenSinFalla) != "sin_falla" {
		t.Errorf("DictamenSinFalla = %q, want \"sin_falla\"", domain.DictamenSinFalla)
	}
}

func TestParseDictamen_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		expected    domain.Dictamen
		esAceptada  bool
		esRechazada bool
		esSinFalla  bool
	}{
		{"aceptada", domain.DictamenAceptada, true, false, false},
		{"rechazada", domain.DictamenRechazada, false, true, false},
		{"sin_falla", domain.DictamenSinFalla, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			d, err := domain.ParseDictamen(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if d != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, d)
			}
			if d.EsAceptada() != tc.esAceptada {
				t.Errorf("EsAceptada mismatch for %q", tc.input)
			}
			if d.EsRechazada() != tc.esRechazada {
				t.Errorf("EsRechazada mismatch for %q", tc.input)
			}
			if d.EsSinFalla() != tc.esSinFalla {
				t.Errorf("EsSinFalla mismatch for %q", tc.input)
			}
			if d.String() != tc.input {
				t.Errorf("String() = %q, want %q", d.String(), tc.input)
			}
		})
	}
}

func TestParseDictamen_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Aceptada", "SIN_FALLA", "aceptada ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseDictamen(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrDictamenInvalido) {
				t.Fatalf("expected ErrDictamenInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestDictamen_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.DictamenAceptada.IsValid() {
		t.Error("DictamenAceptada.IsValid() should be true")
	}
	if !domain.DictamenRechazada.IsValid() {
		t.Error("DictamenRechazada.IsValid() should be true")
	}
	if !domain.DictamenSinFalla.IsValid() {
		t.Error("DictamenSinFalla.IsValid() should be true")
	}
	if domain.Dictamen("invalid").IsValid() {
		t.Error("invalid Dictamen should not be valid")
	}
}
