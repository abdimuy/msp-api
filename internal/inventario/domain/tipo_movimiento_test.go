package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestTipoMovimiento_WireValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  domain.TipoMovimiento
		want string
	}{
		{"Salida", domain.TipoMovimientoSalida, "S"},
		{"Entrada", domain.TipoMovimientoEntrada, "E"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if string(tc.got) != tc.want {
				t.Fatalf("expected constant value %q, got %q", tc.want, tc.got)
			}
		})
	}
}

func TestParseTipoMovimiento_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input     string
		isSalida  bool
		isEntrada bool
	}{
		{"S", true, false},
		{"E", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			tm, err := domain.ParseTipoMovimiento(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if tm.String() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, tm.String())
			}
			if tm.IsSalida() != tc.isSalida {
				t.Fatalf("IsSalida mismatch for %q", tc.input)
			}
			if tm.IsEntrada() != tc.isEntrada {
				t.Fatalf("IsEntrada mismatch for %q", tc.input)
			}
		})
	}
}

func TestParseTipoMovimiento_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "s", "e", "X", "SE", "entrada", "salida", " S", "S "}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseTipoMovimiento(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrTipoMovimientoInvalido) {
				t.Fatalf("expected ErrTipoMovimientoInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestTipoMovimiento_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input domain.TipoMovimiento
		want  bool
	}{
		{domain.TipoMovimientoSalida, true},
		{domain.TipoMovimientoEntrada, true},
		{domain.TipoMovimiento(""), false},
		{domain.TipoMovimiento("X"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.input)+"_isvalid", func(t *testing.T) {
			t.Parallel()
			if tc.input.IsValid() != tc.want {
				t.Fatalf("IsValid mismatch for %q: want %v", tc.input, tc.want)
			}
		})
	}
}

func TestTipoMovimiento_String(t *testing.T) {
	t.Parallel()
	tm, err := domain.ParseTipoMovimiento("S")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tm.String() != "S" {
		t.Fatalf("expected 'S', got %q", tm.String())
	}
}
