package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestRolDecisor_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.RolDecisorCarpinteria) != "carpinteria" {
		t.Errorf("RolDecisorCarpinteria = %q, want \"carpinteria\"", domain.RolDecisorCarpinteria)
	}
	if string(domain.RolDecisorOficina) != "oficina" {
		t.Errorf("RolDecisorOficina = %q, want \"oficina\"", domain.RolDecisorOficina)
	}
	if string(domain.RolDecisorTecnica) != "tecnica" {
		t.Errorf("RolDecisorTecnica = %q, want \"tecnica\"", domain.RolDecisorTecnica)
	}
}

func TestParseRolDecisor_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		domain.RolDecisorCarpinteria.String(),
		domain.RolDecisorOficina.String(),
		domain.RolDecisorTecnica.String(),
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			r, err := domain.ParseRolDecisor(tc)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc, err)
			}
			if r.String() != tc {
				t.Errorf("value mismatch: want %q, got %q", tc, r.String())
			}
		})
	}
}

func TestParseRolDecisor_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Carpinteria", "OFICINA", "tecnica ", "x", "carpintero"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseRolDecisor(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRolDecisorInvalido) {
				t.Fatalf("expected ErrRolDecisorInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestRolDecisor_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.RolDecisorCarpinteria.IsValid() {
		t.Error("RolDecisorCarpinteria.IsValid() should be true")
	}
	if !domain.RolDecisorOficina.IsValid() {
		t.Error("RolDecisorOficina.IsValid() should be true")
	}
	if !domain.RolDecisorTecnica.IsValid() {
		t.Error("RolDecisorTecnica.IsValid() should be true")
	}
	if domain.RolDecisor("invalid").IsValid() {
		t.Error("invalid RolDecisor should not be valid")
	}
}
