package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewDictamen_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		esAceptada  bool
		esRechazada bool
		esSinFalla  bool
	}{
		{domain.DictamenAceptada, true, false, false},
		{domain.DictamenRechazada, false, true, false},
		{domain.DictamenSinFalla, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			d, err := domain.NewDictamen(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if d.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, d.Value())
			}
			if d.EsAceptada() != tc.esAceptada {
				t.Fatalf("EsAceptada mismatch for %q", tc.input)
			}
			if d.EsRechazada() != tc.esRechazada {
				t.Fatalf("EsRechazada mismatch for %q", tc.input)
			}
			if d.EsSinFalla() != tc.esSinFalla {
				t.Fatalf("EsSinFalla mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewDictamen_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Aceptada", "SIN_FALLA", "aceptada ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewDictamen(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrDictamenInvalido) {
				t.Fatalf("expected ErrDictamenInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestDictamen_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewDictamen(domain.DictamenAceptada)
	a2, _ := domain.NewDictamen(domain.DictamenAceptada)
	r, _ := domain.NewDictamen(domain.DictamenRechazada)

	if !a.Equals(a2) {
		t.Fatal("expected a.Equals(a2) == true")
	}
	if a.Equals(r) {
		t.Fatal("expected a.Equals(r) == false")
	}

	zero := domain.HydrateDictamen("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty dictamen")
	}
	if a.IsZero() {
		t.Fatal("expected IsZero == false for valid dictamen")
	}
}

func TestDictamen_String(t *testing.T) {
	t.Parallel()
	d, _ := domain.NewDictamen(domain.DictamenSinFalla)
	if d.String() != domain.DictamenSinFalla {
		t.Fatalf("expected %q, got %q", domain.DictamenSinFalla, d.String())
	}
}

func TestHydrateDictamen_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDictamen("cualquier-cosa")
	if d.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", d.Value())
	}
}
