package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestParseTipoComprobante_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input   domain.TipoComprobante
		esVenta bool
		esPago  bool
	}{
		{domain.TipoVenta, true, false},
		{domain.TipoPago, false, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.input), func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseTipoComprobante(string(tc.input))
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, got)
			}
			if got.EsVenta() != tc.esVenta {
				t.Fatalf("EsVenta mismatch for %q", tc.input)
			}
			if got.EsPago() != tc.esPago {
				t.Fatalf("EsPago mismatch for %q", tc.input)
			}
		})
	}
}

func TestParseTipoComprobante_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Venta", "Pago", "X", "venta ", " pago", "sale", "payment"}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q_invalid", tc), func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseTipoComprobante(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrTipoComprobanteInvalido) {
				t.Fatalf("expected ErrTipoComprobanteInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestTipoComprobante_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.TipoVenta.IsValid() {
		t.Fatal("expected TipoVenta to be valid")
	}
	if !domain.TipoPago.IsValid() {
		t.Fatal("expected TipoPago to be valid")
	}
	for _, invalid := range []domain.TipoComprobante{"", "VENTA", "venta ", "sale", "Pago "} {
		if invalid.IsValid() {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
}

func TestTipoComprobante_String(t *testing.T) {
	t.Parallel()
	tc, _ := domain.ParseTipoComprobante(string(domain.TipoVenta))
	if tc.String() != string(domain.TipoVenta) {
		t.Fatalf("expected %q, got %q", string(domain.TipoVenta), tc.String())
	}
}

func TestTipoComprobanteConstants(t *testing.T) {
	t.Parallel()
	if domain.TipoVenta != "venta" {
		t.Fatalf("expected TipoVenta='venta', got %q", domain.TipoVenta)
	}
	if domain.TipoPago != "pago" {
		t.Fatalf("expected TipoPago='pago', got %q", domain.TipoPago)
	}
}
