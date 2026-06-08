package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestNewFolio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		"MST000001",
		"MSU999999",
		"MSV123456",
		"MSA000000",
		"MSZ999999",
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			f, err := domain.NewFolio(tc)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc, err)
			}
			if f.Value() != tc {
				t.Fatalf("value mismatch: want %q got %q", tc, f.Value())
			}
			if f.IsZero() {
				t.Fatalf("expected IsZero=false for %q", tc)
			}
		})
	}
}

func TestNewFolio_RejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"MST",
		"MST00012",   // 5 digits instead of 6
		"MSt000001",  // lowercase series letter
		"MST0000001", // 7 digits
		"MST00000A",  // letter in digits section
		"MT000001",   // missing second M
		"MSTT000001", // two series letters
		"ms000001",   // all lowercase
		" MST000001", // leading space
	}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewFolio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
		})
	}
}

func TestFolio_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewFolio("MST000001")
	b, _ := domain.NewFolio("MST000001")
	c, _ := domain.NewFolio("MST000002")

	if !a.Equals(b) {
		t.Fatal("expected a.Equals(b) == true")
	}
	if a.Equals(c) {
		t.Fatal("expected a.Equals(c) == false")
	}

	zero := domain.HydrateFolio("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty folio")
	}
}

func TestFolio_String(t *testing.T) {
	t.Parallel()
	f, _ := domain.NewFolio("MST000123")
	if f.String() != "MST000123" {
		t.Fatalf("expected 'MST000123', got %q", f.String())
	}
}

func TestHydrateFolio_BypassesValidation(t *testing.T) {
	t.Parallel()
	// HydrateFolio bypasses validation for repo reconstruction.
	f := domain.HydrateFolio("legacy-format")
	if f.Value() != "legacy-format" {
		t.Fatalf("expected 'legacy-format', got %q", f.Value())
	}
}
