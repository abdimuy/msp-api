package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewEstadoFolio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		esTerminal  bool
		esCancelado bool
	}{
		{domain.EstadoFolioAbierto, false, false},
		{domain.EstadoFolioEnProceso, false, false},
		{domain.EstadoFolioListoEntrega, false, false},
		{domain.EstadoFolioEntregado, false, false},
		{domain.EstadoFolioCerrado, true, false},
		{domain.EstadoFolioCancelado, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			e, err := domain.NewEstadoFolio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if e.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, e.Value())
			}
			if e.EsTerminal() != tc.esTerminal {
				t.Fatalf("EsTerminal mismatch for %q: want %v got %v", tc.input, tc.esTerminal, e.EsTerminal())
			}
			if e.EsCancelado() != tc.esCancelado {
				t.Fatalf("EsCancelado mismatch for %q: want %v got %v", tc.input, tc.esCancelado, e.EsCancelado())
			}
		})
	}
}

func TestNewEstadoFolio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Abierto", "EN_PROCESO", "abierto ", "x", "listo-entrega"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewEstadoFolio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoFolioInvalido) {
				t.Fatalf("expected ErrEstadoFolioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoFolio_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewEstadoFolio(domain.EstadoFolioAbierto)
	a2, _ := domain.NewEstadoFolio(domain.EstadoFolioAbierto)
	c, _ := domain.NewEstadoFolio(domain.EstadoFolioCerrado)

	if !a.Equals(a2) {
		t.Fatal("expected a.Equals(a2) == true")
	}
	if a.Equals(c) {
		t.Fatal("expected a.Equals(c) == false")
	}

	zero := domain.HydrateEstadoFolio("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty estado_folio")
	}
	if a.IsZero() {
		t.Fatal("expected IsZero == false for valid estado_folio")
	}
}

func TestEstadoFolio_String(t *testing.T) {
	t.Parallel()
	e, _ := domain.NewEstadoFolio(domain.EstadoFolioListoEntrega)
	if e.String() != domain.EstadoFolioListoEntrega {
		t.Fatalf("expected %q, got %q", domain.EstadoFolioListoEntrega, e.String())
	}
}

func TestHydrateEstadoFolio_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	e := domain.HydrateEstadoFolio("cualquier-cosa")
	if e.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", e.Value())
	}
}
