package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewEstadoCuenta_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		esLiquidada bool
	}{
		{domain.EstadoCuentaLiquidada, true},
		{domain.EstadoCuentaSaldoPendiente, false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			e, err := domain.NewEstadoCuenta(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if e.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, e.Value())
			}
			if e.EsLiquidada() != tc.esLiquidada {
				t.Fatalf("EsLiquidada mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewEstadoCuenta_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Liquidada", "SALDO_PENDIENTE", "liquidada ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewEstadoCuenta(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoCuentaInvalido) {
				t.Fatalf("expected ErrEstadoCuentaInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoCuenta_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	l, _ := domain.NewEstadoCuenta(domain.EstadoCuentaLiquidada)
	l2, _ := domain.NewEstadoCuenta(domain.EstadoCuentaLiquidada)
	s, _ := domain.NewEstadoCuenta(domain.EstadoCuentaSaldoPendiente)

	if !l.Equals(l2) {
		t.Fatal("expected l.Equals(l2) == true")
	}
	if l.Equals(s) {
		t.Fatal("expected l.Equals(s) == false")
	}

	zero := domain.HydrateEstadoCuenta("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty estado_cuenta")
	}
	if l.IsZero() {
		t.Fatal("expected IsZero == false for valid estado_cuenta")
	}
}

func TestEstadoCuenta_String(t *testing.T) {
	t.Parallel()
	e, _ := domain.NewEstadoCuenta(domain.EstadoCuentaSaldoPendiente)
	if e.String() != domain.EstadoCuentaSaldoPendiente {
		t.Fatalf("expected %q, got %q", domain.EstadoCuentaSaldoPendiente, e.String())
	}
}

func TestHydrateEstadoCuenta_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	e := domain.HydrateEstadoCuenta("cualquier-cosa")
	if e.Value() != "cualquier-cosa" {
		t.Fatalf("expected hydrate to accept garbage verbatim, got %q", e.Value())
	}
}
