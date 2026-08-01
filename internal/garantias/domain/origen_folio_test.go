package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewOrigenFolio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input     string
		esPiso    bool
		esCliente bool
	}{
		{domain.OrigenFolioPiso, true, false},
		{domain.OrigenFolioCliente, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			o, err := domain.NewOrigenFolio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if o.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, o.Value())
			}
			if o.EsPiso() != tc.esPiso {
				t.Fatalf("EsPiso mismatch for %q", tc.input)
			}
			if o.EsCliente() != tc.esCliente {
				t.Fatalf("EsCliente mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewOrigenFolio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Piso", "CLIENTE", "piso ", " cliente", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewOrigenFolio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrOrigenFolioInvalido) {
				t.Fatalf("expected ErrOrigenFolioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestOrigenFolio_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	p, _ := domain.NewOrigenFolio(domain.OrigenFolioPiso)
	p2, _ := domain.NewOrigenFolio(domain.OrigenFolioPiso)
	c, _ := domain.NewOrigenFolio(domain.OrigenFolioCliente)

	if !p.Equals(p2) {
		t.Fatal("expected p.Equals(p2) == true")
	}
	if p.Equals(c) {
		t.Fatal("expected p.Equals(c) == false")
	}

	zero := domain.HydrateOrigenFolio("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty origen_folio")
	}
	if p.IsZero() {
		t.Fatal("expected IsZero == false for valid origen_folio")
	}
}

func TestOrigenFolio_String(t *testing.T) {
	t.Parallel()
	o, _ := domain.NewOrigenFolio(domain.OrigenFolioCliente)
	if o.String() != domain.OrigenFolioCliente {
		t.Fatalf("expected %q, got %q", domain.OrigenFolioCliente, o.String())
	}
}

func TestHydrateOrigenFolio_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	o := domain.HydrateOrigenFolio("cualquier-cosa")
	if o.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", o.Value())
	}
}
