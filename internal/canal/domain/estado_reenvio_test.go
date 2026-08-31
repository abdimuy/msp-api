package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

func TestEstadoReenvio_WireValues(t *testing.T) {
	t.Parallel()
	values := map[domain.EstadoReenvio]string{
		domain.EstadoReenvioPendiente: "pendiente",
		domain.EstadoReenvioReenviado: "reenviado",
		domain.EstadoReenvioFallido:   "fallido",
	}
	for e, want := range values {
		if string(e) != want {
			t.Errorf("%s = %q, want %q", e, e, want)
		}
	}
}

func TestParseEstadoReenvio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input      string
		expected   domain.EstadoReenvio
		esTerminal bool
	}{
		{"pendiente", domain.EstadoReenvioPendiente, false},
		{"reenviado", domain.EstadoReenvioReenviado, true},
		{"fallido", domain.EstadoReenvioFallido, false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			e, err := domain.ParseEstadoReenvio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if e != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, e)
			}
			if e.IsTerminal() != tc.esTerminal {
				t.Errorf("IsTerminal mismatch for %q: want %v, got %v", tc.input, tc.esTerminal, e.IsTerminal())
			}
			if e.String() != tc.input {
				t.Errorf("String() = %q, want %q", e.String(), tc.input)
			}
		})
	}
}

func TestParseEstadoReenvio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Pendiente", "PENDIENTE", "pendiente ", "x", "en-proceso", "enviado"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseEstadoReenvio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoReenvioInvalido) {
				t.Fatalf("expected ErrEstadoReenvioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoReenvio_IsValid(t *testing.T) {
	t.Parallel()
	validStates := []domain.EstadoReenvio{
		domain.EstadoReenvioPendiente,
		domain.EstadoReenvioReenviado,
		domain.EstadoReenvioFallido,
	}
	for _, s := range validStates {
		if !s.IsValid() {
			t.Errorf("%s.IsValid() should be true", s)
		}
	}
	if domain.EstadoReenvio("invalido").IsValid() {
		t.Error("invalid EstadoReenvio should not be valid")
	}
}

func TestEstadoReenvio_CanTransitionTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from domain.EstadoReenvio
		to   domain.EstadoReenvio
		want bool
	}{
		{domain.EstadoReenvioPendiente, domain.EstadoReenvioReenviado, true},
		{domain.EstadoReenvioPendiente, domain.EstadoReenvioFallido, true},
		{domain.EstadoReenvioPendiente, domain.EstadoReenvioPendiente, false},

		{domain.EstadoReenvioFallido, domain.EstadoReenvioPendiente, true},
		{domain.EstadoReenvioFallido, domain.EstadoReenvioReenviado, false},
		{domain.EstadoReenvioFallido, domain.EstadoReenvioFallido, false},

		{domain.EstadoReenvioReenviado, domain.EstadoReenvioPendiente, false},
		{domain.EstadoReenvioReenviado, domain.EstadoReenvioFallido, false},
		{domain.EstadoReenvioReenviado, domain.EstadoReenvioReenviado, false},
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

func TestEstadoReenvio_IsTerminal(t *testing.T) {
	t.Parallel()
	if !domain.EstadoReenvioReenviado.IsTerminal() {
		t.Error("reenviado should be terminal")
	}
	if domain.EstadoReenvioPendiente.IsTerminal() {
		t.Error("pendiente should not be terminal")
	}
	if domain.EstadoReenvioFallido.IsTerminal() {
		t.Error("fallido should not be terminal — it always has a retry way out")
	}
}
