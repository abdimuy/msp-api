package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestRolArticulo_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.RolArticuloOriginal) != "original" {
		t.Errorf("RolArticuloOriginal = %q, want \"original\"", domain.RolArticuloOriginal)
	}
	if string(domain.RolArticuloReemplazo) != "reemplazo" {
		t.Errorf("RolArticuloReemplazo = %q, want \"reemplazo\"", domain.RolArticuloReemplazo)
	}
}

func TestParseRolArticulo_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		expected    domain.RolArticulo
		esOriginal  bool
		esReemplazo bool
	}{
		{"original", domain.RolArticuloOriginal, true, false},
		{"reemplazo", domain.RolArticuloReemplazo, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			r, err := domain.ParseRolArticulo(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if r != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, r)
			}
			if r.EsOriginal() != tc.esOriginal {
				t.Errorf("EsOriginal mismatch for %q", tc.input)
			}
			if r.EsReemplazo() != tc.esReemplazo {
				t.Errorf("EsReemplazo mismatch for %q", tc.input)
			}
			if r.String() != tc.input {
				t.Errorf("String() = %q, want %q", r.String(), tc.input)
			}
		})
	}
}

func TestParseRolArticulo_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Original", "REEMPLAZO", "original ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseRolArticulo(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRolArticuloInvalido) {
				t.Fatalf("expected ErrRolArticuloInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestRolArticulo_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.RolArticuloOriginal.IsValid() {
		t.Error("RolArticuloOriginal.IsValid() should be true")
	}
	if !domain.RolArticuloReemplazo.IsValid() {
		t.Error("RolArticuloReemplazo.IsValid() should be true")
	}
	if domain.RolArticulo("invalid").IsValid() {
		t.Error("invalid RolArticulo should not be valid")
	}
}
