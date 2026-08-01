package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewRolArticulo_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		esOriginal  bool
		esReemplazo bool
	}{
		{domain.RolArticuloOriginal, true, false},
		{domain.RolArticuloReemplazo, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			r, err := domain.NewRolArticulo(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if r.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, r.Value())
			}
			if r.EsOriginal() != tc.esOriginal {
				t.Fatalf("EsOriginal mismatch for %q", tc.input)
			}
			if r.EsReemplazo() != tc.esReemplazo {
				t.Fatalf("EsReemplazo mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewRolArticulo_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Original", "REEMPLAZO", "original ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewRolArticulo(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRolArticuloInvalido) {
				t.Fatalf("expected ErrRolArticuloInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestRolArticulo_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	o, _ := domain.NewRolArticulo(domain.RolArticuloOriginal)
	o2, _ := domain.NewRolArticulo(domain.RolArticuloOriginal)
	r, _ := domain.NewRolArticulo(domain.RolArticuloReemplazo)

	if !o.Equals(o2) {
		t.Fatal("expected o.Equals(o2) == true")
	}
	if o.Equals(r) {
		t.Fatal("expected o.Equals(r) == false")
	}

	zero := domain.HydrateRolArticulo("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty rol_articulo")
	}
	if o.IsZero() {
		t.Fatal("expected IsZero == false for valid rol_articulo")
	}
}

func TestRolArticulo_String(t *testing.T) {
	t.Parallel()
	r, _ := domain.NewRolArticulo(domain.RolArticuloReemplazo)
	if r.String() != domain.RolArticuloReemplazo {
		t.Fatalf("expected %q, got %q", domain.RolArticuloReemplazo, r.String())
	}
}

func TestHydrateRolArticulo_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	r := domain.HydrateRolArticulo("cualquier-cosa")
	if r.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", r.Value())
	}
}
