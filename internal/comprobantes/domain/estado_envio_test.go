package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestNewEstadoEnvio_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
	}{
		{domain.EstadoEnvioEnEspera},
		{domain.EstadoEnvioEnviando},
		{domain.EstadoEnvioEnviado},
		{domain.EstadoEnvioDetenido},
		{domain.EstadoEnvioFallido},
		{domain.EstadoEnvioSinTelefono},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewEstadoEnvio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, got.Value())
			}
		})
	}
}

func TestNewEstadoEnvio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "EnEspera", "ENVIADO", "X", "en_espera ", " enviando", "cancelado", "pendiente"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewEstadoEnvio(tc)
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
		input       string
		esDetenible bool
		esTerminal  bool
		esFalla     bool
	}{
		{domain.EstadoEnvioEnEspera, true, false, false},
		{domain.EstadoEnvioEnviando, false, false, false},
		{domain.EstadoEnvioEnviado, false, true, false},
		{domain.EstadoEnvioDetenido, false, true, false},
		{domain.EstadoEnvioFallido, false, true, true},
		{domain.EstadoEnvioSinTelefono, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewEstadoEnvio(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got.EsDetenible() != tc.esDetenible {
				t.Fatalf("EsDetenible mismatch for %q: want %v got %v", tc.input, tc.esDetenible, got.EsDetenible())
			}
			if got.EsTerminal() != tc.esTerminal {
				t.Fatalf("EsTerminal mismatch for %q: want %v got %v", tc.input, tc.esTerminal, got.EsTerminal())
			}
			if got.EsFalla() != tc.esFalla {
				t.Fatalf("EsFalla mismatch for %q: want %v got %v", tc.input, tc.esFalla, got.EsFalla())
			}
		})
	}
}

func TestEstadoEnvio_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	ee, _ := domain.NewEstadoEnvio(domain.EstadoEnvioEnEspera)
	ee2, _ := domain.NewEstadoEnvio(domain.EstadoEnvioEnEspera)
	f, _ := domain.NewEstadoEnvio(domain.EstadoEnvioFallido)

	if !ee.Equals(ee2) {
		t.Fatal("expected ee.Equals(ee2) == true")
	}
	if ee.Equals(f) {
		t.Fatal("expected ee.Equals(f) == false")
	}

	zero := domain.HydrateEstadoEnvio("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty estado_envio")
	}
	if ee.IsZero() {
		t.Fatal("expected IsZero == false for valid estado_envio")
	}
}

func TestEstadoEnvio_String(t *testing.T) {
	t.Parallel()
	ee, _ := domain.NewEstadoEnvio(domain.EstadoEnvioEnviado)
	if ee.String() != domain.EstadoEnvioEnviado {
		t.Fatalf("expected %q, got %q", domain.EstadoEnvioEnviado, ee.String())
	}
}

func TestEstadoEnvioConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
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
			if tc.got != tc.want {
				t.Fatalf("expected constant value %q, got %q", tc.want, tc.got)
			}
		})
	}
}

func TestHydrateEstadoEnvio_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"", "garbage", "ENVIADO", "123"} {
		t.Run(tc+"_hydrate", func(t *testing.T) {
			t.Parallel()
			hydrated := domain.HydrateEstadoEnvio(tc)
			if hydrated.Value() != tc {
				t.Fatalf("expected value %q, got %q", tc, hydrated.Value())
			}
			if !hydrated.Equals(domain.HydrateEstadoEnvio(tc)) {
				t.Fatal("expected hydrated values to be equal")
			}
		})
	}
}
