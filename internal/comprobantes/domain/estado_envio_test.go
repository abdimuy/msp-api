package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestParseEstadoEnvio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []domain.EstadoEnvio{
		domain.EstadoEnvioEnEspera,
		domain.EstadoEnvioEnviando,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioDetenido,
		domain.EstadoEnvioFallido,
		domain.EstadoEnvioSinTelefono,
	}
	for _, input := range cases {
		t.Run(string(input), func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseEstadoEnvio(string(input))
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", input, err)
			}
			if got != input {
				t.Fatalf("value mismatch: want %q got %q", input, got)
			}
		})
	}
}

func TestParseEstadoEnvio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "EnEspera", "ENVIADO", "X", "en_espera ", " enviando", "cancelado", "pendiente"}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q_invalid", tc), func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseEstadoEnvio(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoEnvioInvalido) {
				t.Fatalf("expected ErrEstadoEnvioInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoEnvio_ExhaustiveHelpers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       domain.EstadoEnvio
		esDetenible bool
		isTerminal  bool
		esFalla     bool
	}{
		{domain.EstadoEnvioEnEspera, true, false, false},
		{domain.EstadoEnvioEnviando, false, false, false},
		{domain.EstadoEnvioEnviado, false, true, false},
		{domain.EstadoEnvioDetenido, false, true, false},
		{domain.EstadoEnvioFallido, false, false, true},
		{domain.EstadoEnvioSinTelefono, false, true, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.input), func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseEstadoEnvio(string(tc.input))
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got.EsDetenible() != tc.esDetenible {
				t.Fatalf("EsDetenible mismatch for %q: want %v got %v", tc.input, tc.esDetenible, got.EsDetenible())
			}
			if got.IsTerminal() != tc.isTerminal {
				t.Fatalf("IsTerminal mismatch for %q: want %v got %v", tc.input, tc.isTerminal, got.IsTerminal())
			}
			if got.EsFalla() != tc.esFalla {
				t.Fatalf("EsFalla mismatch for %q: want %v got %v", tc.input, tc.esFalla, got.EsFalla())
			}
		})
	}
}

func TestEstadoEnvio_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from   domain.EstadoEnvio
		to     domain.EstadoEnvio
		expect bool
	}{
		{domain.EstadoEnvioEnEspera, domain.EstadoEnvioEnviando, true},
		{domain.EstadoEnvioEnEspera, domain.EstadoEnvioDetenido, true},
		{domain.EstadoEnvioEnviando, domain.EstadoEnvioEnviado, true},
		{domain.EstadoEnvioEnviando, domain.EstadoEnvioFallido, true},
		{domain.EstadoEnvioFallido, domain.EstadoEnvioEnEspera, true},
		{domain.EstadoEnvioEnEspera, domain.EstadoEnvioEnviado, false},
		{domain.EstadoEnvioEnviando, domain.EstadoEnvioEnEspera, false},
		{domain.EstadoEnvioEnviado, domain.EstadoEnvioFallido, false},
		{domain.EstadoEnvioDetenido, domain.EstadoEnvioEnEspera, false},
		{domain.EstadoEnvioFallido, domain.EstadoEnvioEnviado, false},
		{domain.EstadoEnvioSinTelefono, domain.EstadoEnvioEnEspera, false},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%s_to_%s", tc.from, tc.to)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.from.CanTransitionTo(tc.to); got != tc.expect {
				t.Fatalf("CanTransitionTo(%s -> %s) = %v, want %v", tc.from, tc.to, got, tc.expect)
			}
		})
	}
}

func TestEstadoEnvio_String(t *testing.T) {
	t.Parallel()
	ee, _ := domain.ParseEstadoEnvio(string(domain.EstadoEnvioEnviado))
	if ee.String() != string(domain.EstadoEnvioEnviado) {
		t.Fatalf("expected %q, got %q", string(domain.EstadoEnvioEnviado), ee.String())
	}
}

func TestEstadoEnvioConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  domain.EstadoEnvio
		want string
	}{
		{"EnEspera", domain.EstadoEnvioEnEspera, "en_espera"},
		{"Enviando", domain.EstadoEnvioEnviando, "enviando"},
		{"Enviado", domain.EstadoEnvioEnviado, "enviado"},
		{"Detenido", domain.EstadoEnvioDetenido, "detenido"},
		{"Fallido", domain.EstadoEnvioFallido, "fallido"},
		{"SinTelefono", domain.EstadoEnvioSinTelefono, "sin_telefono"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if string(tc.got) != tc.want {
				t.Fatalf("expected constant value %q, got %q", tc.want, string(tc.got))
			}
		})
	}
}
