package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestEstadoFolio_WireValues(t *testing.T) {
	t.Parallel()
	values := map[domain.EstadoFolio]string{
		domain.EstadoFolioAbierto:      "abierto",
		domain.EstadoFolioEnProceso:    "en_proceso",
		domain.EstadoFolioListoEntrega: "listo_entrega",
		domain.EstadoFolioEntregado:    "entregado",
		domain.EstadoFolioCerrado:      "cerrado",
		domain.EstadoFolioCancelado:    "cancelado",
	}
	for e, want := range values {
		if string(e) != want {
			t.Errorf("%s = %q, want %q", e, e, want)
		}
	}
}

func TestParseEstadoFolio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		expected    domain.EstadoFolio
		esTerminal  bool
		esCancelado bool
	}{
		{"abierto", domain.EstadoFolioAbierto, false, false},
		{"en_proceso", domain.EstadoFolioEnProceso, false, false},
		{"listo_entrega", domain.EstadoFolioListoEntrega, false, false},
		{"entregado", domain.EstadoFolioEntregado, false, false},
		{"cerrado", domain.EstadoFolioCerrado, true, false},
		{"cancelado", domain.EstadoFolioCancelado, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			e, err := domain.ParseEstadoFolio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if e != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, e)
			}
			if e.IsTerminal() != tc.esTerminal {
				t.Errorf("IsTerminal mismatch for %q: want %v, got %v", tc.input, tc.esTerminal, e.IsTerminal())
			}
			if e.EsCancelado() != tc.esCancelado {
				t.Errorf("EsCancelado mismatch for %q: want %v, got %v", tc.input, tc.esCancelado, e.EsCancelado())
			}
			if e.String() != tc.input {
				t.Errorf("String() = %q, want %q", e.String(), tc.input)
			}
		})
	}
}

func TestParseEstadoFolio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Abierto", "EN_PROCESO", "abierto ", "x", "listo-entrega"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseEstadoFolio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoFolioInvalido) {
				t.Fatalf("expected ErrEstadoFolioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoFolio_IsValid(t *testing.T) {
	t.Parallel()
	validStates := []domain.EstadoFolio{
		domain.EstadoFolioAbierto,
		domain.EstadoFolioEnProceso,
		domain.EstadoFolioListoEntrega,
		domain.EstadoFolioEntregado,
		domain.EstadoFolioCerrado,
		domain.EstadoFolioCancelado,
	}
	for _, s := range validStates {
		if !s.IsValid() {
			t.Errorf("%s.IsValid() should be true", s)
		}
	}
	if domain.EstadoFolio("invalid").IsValid() {
		t.Error("invalid EstadoFolio should not be valid")
	}
}

func TestEstadoFolio_CanTransitionTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from domain.EstadoFolio
		to   domain.EstadoFolio
		want bool
	}{
		{domain.EstadoFolioAbierto, domain.EstadoFolioEnProceso, true},
		{domain.EstadoFolioAbierto, domain.EstadoFolioCancelado, true},
		{domain.EstadoFolioAbierto, domain.EstadoFolioListoEntrega, false},
		{domain.EstadoFolioAbierto, domain.EstadoFolioCerrado, false},

		{domain.EstadoFolioEnProceso, domain.EstadoFolioListoEntrega, true},
		{domain.EstadoFolioEnProceso, domain.EstadoFolioCancelado, true},
		{domain.EstadoFolioEnProceso, domain.EstadoFolioEntregado, false},

		{domain.EstadoFolioListoEntrega, domain.EstadoFolioEntregado, true},
		{domain.EstadoFolioListoEntrega, domain.EstadoFolioCancelado, false},

		{domain.EstadoFolioEntregado, domain.EstadoFolioCerrado, true},
		{domain.EstadoFolioEntregado, domain.EstadoFolioCancelado, false},

		{domain.EstadoFolioCerrado, domain.EstadoFolioAbierto, false},
		{domain.EstadoFolioCancelado, domain.EstadoFolioAbierto, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			t.Parallel()
			got := tt.from.CanTransitionTo(tt.to)
			if got != tt.want {
				t.Errorf("CanTransitionTo(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
