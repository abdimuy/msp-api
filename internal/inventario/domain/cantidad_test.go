package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestNewCantidad_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"integer one", "1"},
		{"integer large", "9999"},
		{"two decimals", "1.25"},
		{"four decimals", "0.0001"},
		{"exact four decimal places", "12.3456"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := decimal.RequireFromString(tc.input)
			c, err := domain.NewCantidad(d)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if !c.Value().Equal(d) {
				t.Fatalf("value mismatch: want %v got %v", d, c.Value())
			}
			if c.IsZero() {
				t.Fatalf("expected IsZero=false for %q", tc.input)
			}
		})
	}
}

func TestNewCantidad_RejectsZeroOrNegative(t *testing.T) {
	t.Parallel()
	cases := []string{"0", "-1", "-0.0001", "-999"}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewCantidad(decimal.RequireFromString(tc))
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
		})
	}
}

func TestNewCantidad_RejectsScaleOver4(t *testing.T) {
	t.Parallel()
	cases := []string{"1.00001", "0.00001", "123.12345"}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewCantidad(decimal.RequireFromString(tc))
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
		})
	}
}

func TestCantidad_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewCantidad(decimal.NewFromInt(5))
	b, _ := domain.NewCantidad(decimal.NewFromInt(5))
	c, _ := domain.NewCantidad(decimal.NewFromInt(6))

	if !a.Equals(b) {
		t.Fatal("expected a.Equals(b) == true")
	}
	if a.Equals(c) {
		t.Fatal("expected a.Equals(c) == false")
	}
	if a.IsZero() {
		t.Fatal("expected IsZero == false for valid cantidad")
	}

	zero := domain.HydrateCantidad(decimal.Zero)
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for zero-value Cantidad")
	}
}

func TestCantidad_String(t *testing.T) {
	t.Parallel()
	c, _ := domain.NewCantidad(decimal.RequireFromString("3.5"))
	if c.String() != "3.5" {
		t.Fatalf("expected '3.5', got %q", c.String())
	}
}

func TestHydrateCantidad_NegativeAllowed(t *testing.T) {
	t.Parallel()
	// HydrateCantidad bypasses validation — negative stock is valid in projections.
	neg := domain.HydrateCantidad(decimal.RequireFromString("-5"))
	if neg.IsZero() {
		t.Fatal("negative hydrated cantidad should not be IsZero")
	}
}
