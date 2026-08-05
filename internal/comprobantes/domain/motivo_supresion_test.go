package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestNewMotivoSupresion_HappyPath(t *testing.T) {
	t.Parallel()
	got, err := domain.NewMotivoSupresion(domain.MotivoSupresionRebote)
	if err != nil {
		t.Fatalf("expected no error for %q, got %v", domain.MotivoSupresionRebote, err)
	}
	if got.Value() != domain.MotivoSupresionRebote {
		t.Fatalf("value mismatch: want %q got %q", domain.MotivoSupresionRebote, got.Value())
	}
	if !got.EsRebote() {
		t.Fatal("expected EsRebote == true for rebote")
	}
}

func TestNewMotivoSupresion_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Rebote", "REBOTE", "X", "rebote ", " bounce", "no_cobrar", "devolucion"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewMotivoSupresion(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrMotivoSupresionInvalido) {
				t.Fatalf("expected ErrMotivoSupresionInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestMotivoSupresion_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	r, _ := domain.NewMotivoSupresion(domain.MotivoSupresionRebote)
	r2, _ := domain.NewMotivoSupresion(domain.MotivoSupresionRebote)

	if !r.Equals(r2) {
		t.Fatal("expected r.Equals(r2) == true")
	}

	zero := domain.HydrateMotivoSupresion("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty motivo_supresion")
	}
	if r.IsZero() {
		t.Fatal("expected IsZero == false for valid motivo_supresion")
	}
}

func TestMotivoSupresion_String(t *testing.T) {
	t.Parallel()
	m, _ := domain.NewMotivoSupresion(domain.MotivoSupresionRebote)
	if m.String() != domain.MotivoSupresionRebote {
		t.Fatalf("expected %q, got %q", domain.MotivoSupresionRebote, m.String())
	}
}

func TestMotivoSupresionConstants(t *testing.T) {
	t.Parallel()
	if domain.MotivoSupresionRebote != "rebote" {
		t.Fatalf("expected MotivoSupresionRebote='rebote', got %q", domain.MotivoSupresionRebote)
	}
}

func TestHydrateMotivoSupresion_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"", "garbage", "REBOTE", "123"} {
		t.Run(tc+"_hydrate", func(t *testing.T) {
			t.Parallel()
			hydrated := domain.HydrateMotivoSupresion(tc)
			if hydrated.Value() != tc {
				t.Fatalf("expected value %q, got %q", tc, hydrated.Value())
			}
			if !hydrated.Equals(domain.HydrateMotivoSupresion(tc)) {
				t.Fatal("expected hydrated values to be equal")
			}
		})
	}
}
