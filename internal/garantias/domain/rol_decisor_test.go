package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewRolDecisor_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		domain.RolDecisorCarpinteria,
		domain.RolDecisorOficina,
		domain.RolDecisorTecnica,
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			r, err := domain.NewRolDecisor(tc)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc, err)
			}
			if r.Value() != tc {
				t.Fatalf("value mismatch: want %q got %q", tc, r.Value())
			}
		})
	}
}

func TestNewRolDecisor_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Carpinteria", "OFICINA", "tecnica ", "x", "carpintero"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewRolDecisor(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrRolDecisorInvalido) {
				t.Fatalf("expected ErrRolDecisorInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestRolDecisor_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	c, _ := domain.NewRolDecisor(domain.RolDecisorCarpinteria)
	c2, _ := domain.NewRolDecisor(domain.RolDecisorCarpinteria)
	o, _ := domain.NewRolDecisor(domain.RolDecisorOficina)

	if !c.Equals(c2) {
		t.Fatal("expected c.Equals(c2) == true")
	}
	if c.Equals(o) {
		t.Fatal("expected c.Equals(o) == false")
	}

	zero := domain.HydrateRolDecisor("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty rol_decisor")
	}
	if c.IsZero() {
		t.Fatal("expected IsZero == false for valid rol_decisor")
	}
}

func TestRolDecisor_String(t *testing.T) {
	t.Parallel()
	r, _ := domain.NewRolDecisor(domain.RolDecisorTecnica)
	if r.String() != domain.RolDecisorTecnica {
		t.Fatalf("expected %q, got %q", domain.RolDecisorTecnica, r.String())
	}
}

func TestHydrateRolDecisor_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	r := domain.HydrateRolDecisor("cualquier-cosa")
	if r.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", r.Value())
	}
}
