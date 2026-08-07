package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestEstadoCuenta_WireValues(t *testing.T) {
	t.Parallel()
	if string(domain.EstadoCuentaLiquidada) != "liquidada" {
		t.Errorf("EstadoCuentaLiquidada = %q, want \"liquidada\"", domain.EstadoCuentaLiquidada)
	}
	if string(domain.EstadoCuentaSaldoPendiente) != "saldo_pendiente" {
		t.Errorf("EstadoCuentaSaldoPendiente = %q, want \"saldo_pendiente\"", domain.EstadoCuentaSaldoPendiente)
	}
}

func TestParseEstadoCuenta_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		expected    domain.EstadoCuenta
		esLiquidada bool
	}{
		{"liquidada", domain.EstadoCuentaLiquidada, true},
		{"saldo_pendiente", domain.EstadoCuentaSaldoPendiente, false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			e, err := domain.ParseEstadoCuenta(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if e != tc.expected {
				t.Errorf("value mismatch: want %q, got %q", tc.expected, e)
			}
			if e.EsLiquidada() != tc.esLiquidada {
				t.Errorf("EsLiquidada mismatch for %q", tc.input)
			}
			if e.String() != tc.input {
				t.Errorf("String() = %q, want %q", e.String(), tc.input)
			}
		})
	}
}

func TestParseEstadoCuenta_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Liquidada", "SALDO_PENDIENTE", "liquidada ", "x"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseEstadoCuenta(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrEstadoCuentaInvalido) {
				t.Fatalf("expected ErrEstadoCuentaInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestEstadoCuenta_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.EstadoCuentaLiquidada.IsValid() {
		t.Error("EstadoCuentaLiquidada.IsValid() should be true")
	}
	if !domain.EstadoCuentaSaldoPendiente.IsValid() {
		t.Error("EstadoCuentaSaldoPendiente.IsValid() should be true")
	}
	if domain.EstadoCuenta("invalid").IsValid() {
		t.Error("invalid EstadoCuenta should not be valid")
	}
}
