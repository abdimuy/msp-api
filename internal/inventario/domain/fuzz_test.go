package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// FuzzNewCantidad exercises NewCantidad with arbitrary string-encoded decimal
// inputs. The contract is: never panic, and accepted values must be > 0 with
// at most 4 decimal places.
func FuzzNewCantidad(f *testing.F) {
	seeds := []string{
		"1", "0", "-1", "0.0001", "0.00001", "999999", "-0.0001",
		"1.2345", "1.23456", "0.1", "3.14159", "1000000000",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := decimal.NewFromString(s)
		if err != nil {
			// Not a valid decimal — skip; we only test valid decimal inputs.
			return
		}
		c, cerr := domain.NewCantidad(d)
		if cerr != nil {
			return
		}
		// Invariant: accepted value must be > 0.
		if c.Value().Sign() <= 0 {
			t.Fatalf("accepted non-positive cantidad: %v", c.Value())
		}
		// Invariant: accepted value must have at most 4 decimal places.
		if c.Value().Exponent() < -4 {
			t.Fatalf("accepted cantidad with scale > 4: exponent=%d", c.Value().Exponent())
		}
	})
}

// FuzzNewFolio exercises NewFolio with arbitrary string inputs. The contract
// is: never panic, and accepted values must satisfy the documented pattern.
func FuzzNewFolio(f *testing.F) {
	seeds := []string{
		"MST000001", "MSU999999", "MSV123456", "", "MS", "MST",
		"MST0000001", "mst000001", "MS1000001", "MSTZ123456",
		"  MST000001", "MST00001", "MST000001 ",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		fo, err := domain.NewFolio(s)
		if err != nil {
			return
		}
		// Invariant: accepted value must not be empty.
		if fo.IsZero() {
			t.Fatalf("accepted folio but IsZero=true: in=%q", s)
		}
		// Invariant: accepted value must round-trip.
		if fo.Value() != s {
			t.Fatalf("folio value mismatch: in=%q out=%q", s, fo.Value())
		}
	})
}

// FuzzNewTipoMovimiento exercises NewTipoMovimiento with arbitrary string
// inputs. The contract is: never panic, and only "S" and "E" are accepted.
func FuzzNewTipoMovimiento(f *testing.F) {
	seeds := []string{
		"S", "E", "", "s", "e", "X", "SE", "ES", " S", "S ",
		"salida", "entrada", "SALIDA", "ENTRADA",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tm, err := domain.NewTipoMovimiento(s)
		if err != nil {
			return
		}
		// Invariant: only "S" or "E" may be accepted.
		v := tm.Value()
		if v != domain.TipoMovimientoSalida && v != domain.TipoMovimientoEntrada {
			t.Fatalf("accepted invalid tipo_movimiento: %q", v)
		}
	})
}
