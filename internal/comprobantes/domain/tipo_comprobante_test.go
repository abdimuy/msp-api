package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestNewTipoComprobante_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input   string
		esVenta bool
		esPago  bool
	}{
		{domain.TipoVenta, true, false},
		{domain.TipoPago, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewTipoComprobante(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, got.Value())
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

func TestNewTipoComprobante_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Venta", "Pago", "X", "venta ", " pago", "sale", "payment"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewTipoComprobante(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrTipoComprobanteInvalido) {
				t.Fatalf("expected ErrTipoComprobanteInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestTipoComprobante_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	v, _ := domain.NewTipoComprobante(domain.TipoVenta)
	v2, _ := domain.NewTipoComprobante(domain.TipoVenta)
	p, _ := domain.NewTipoComprobante(domain.TipoPago)

	if !v.Equals(v2) {
		t.Fatal("expected v.Equals(v2) == true")
	}
	if v.Equals(p) {
		t.Fatal("expected v.Equals(p) == false")
	}

	zero := domain.HydrateTipoComprobante("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty tipo_comprobante")
	}
	if v.IsZero() {
		t.Fatal("expected IsZero == false for valid tipo_comprobante")
	}
}

func TestTipoComprobante_String(t *testing.T) {
	t.Parallel()
	tc, _ := domain.NewTipoComprobante(domain.TipoVenta)
	if tc.String() != domain.TipoVenta {
		t.Fatalf("expected %q, got %q", domain.TipoVenta, tc.String())
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

func TestHydrateTipoComprobante_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"", "garbage", "VENTA", "123"} {
		t.Run(tc+"_hydrate", func(t *testing.T) {
			t.Parallel()
			hydrated := domain.HydrateTipoComprobante(tc)
			if hydrated.Value() != tc {
				t.Fatalf("expected value %q, got %q", tc, hydrated.Value())
			}
			if !hydrated.Equals(domain.HydrateTipoComprobante(tc)) {
				t.Fatal("expected hydrated values to be equal")
			}
		})
	}
}
