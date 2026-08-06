package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestOrigenFolio_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.OrigenFolioPiso) != "piso" {
		t.Errorf("OrigenFolioPiso = %q, want \"piso\"", domain.OrigenFolioPiso)
	}
	if string(domain.OrigenFolioCliente) != "cliente" {
		t.Errorf("OrigenFolioCliente = %q, want \"cliente\"", domain.OrigenFolioCliente)
	}
}

func TestParseOrigenFolio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input     string
		expected  domain.OrigenFolio
		esPiso    bool
		esCliente bool
	}{
		{"piso", domain.OrigenFolioPiso, true, false},
		{"cliente", domain.OrigenFolioCliente, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			o, err := domain.ParseOrigenFolio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if o != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, o)
			}
			if o.EsPiso() != tc.esPiso {
				t.Errorf("EsPiso mismatch for %q", tc.input)
			}
			if o.EsCliente() != tc.esCliente {
				t.Errorf("EsCliente mismatch for %q", tc.input)
			}
			if o.String() != tc.input {
				t.Errorf("String() = %q, want %q", o.String(), tc.input)
			}
		})
	}
}

func TestParseOrigenFolio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Piso", "CLIENTE", "piso ", " cliente", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseOrigenFolio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrOrigenFolioInvalido) {
				t.Fatalf("expected ErrOrigenFolioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestOrigenFolio_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.OrigenFolioPiso.IsValid() {
		t.Error("OrigenFolioPiso.IsValid() should be true")
	}
	if !domain.OrigenFolioCliente.IsValid() {
		t.Error("OrigenFolioCliente.IsValid() should be true")
	}
	if domain.OrigenFolio("invalid").IsValid() {
		t.Error("invalid OrigenFolio should not be valid")
	}
}
