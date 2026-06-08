package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestNewTipoMovimiento_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input     string
		isSalida  bool
		isEntrada bool
	}{
		{domain.TipoMovimientoSalida, true, false},
		{domain.TipoMovimientoEntrada, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			tm, err := domain.NewTipoMovimiento(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if tm.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, tm.Value())
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

func TestNewTipoMovimiento_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "s", "e", "X", "SE", "entrada", "salida", " S", "S "}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewTipoMovimiento(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
		})
	}
}

func TestTipoMovimiento_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	s, _ := domain.NewTipoMovimiento(domain.TipoMovimientoSalida)
	s2, _ := domain.NewTipoMovimiento(domain.TipoMovimientoSalida)
	e, _ := domain.NewTipoMovimiento(domain.TipoMovimientoEntrada)

	if !s.Equals(s2) {
		t.Fatal("expected s.Equals(s2) == true")
	}
	if s.Equals(e) {
		t.Fatal("expected s.Equals(e) == false")
	}

	zero := domain.HydrateTipoMovimiento("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty tipo_movimiento")
	}
	if s.IsZero() {
		t.Fatal("expected IsZero == false for valid tipo_movimiento")
	}
}

func TestTipoMovimiento_String(t *testing.T) {
	t.Parallel()
	tm, _ := domain.NewTipoMovimiento("S")
	if tm.String() != "S" {
		t.Fatalf("expected 'S', got %q", tm.String())
	}
}

func TestTipoMovimientoConstants(t *testing.T) {
	t.Parallel()
	if domain.TipoMovimientoSalida != "S" {
		t.Fatalf("expected TipoMovimientoSalida='S', got %q", domain.TipoMovimientoSalida)
	}
	if domain.TipoMovimientoEntrada != "E" {
		t.Fatalf("expected TipoMovimientoEntrada='E', got %q", domain.TipoMovimientoEntrada)
	}
}
