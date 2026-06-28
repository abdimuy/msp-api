//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseMoneyField_RoundsToTwoDecimals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		// El caso real: la app Android suma precios de combo en Double y manda
		// la basura de punto flotante. Debe aceptarse redondeado, no rechazarse.
		{"float garbage de combo", "300.29999999999995", "300.3"},
		{"tres decimales HALF_UP arriba", "10.125", "10.13"},
		{"tres decimales HALF_UP abajo", "10.124", "10.12"},
		{"ya con dos decimales", "3400.00", "3400"},
		{"entero", "6000", "6000"},
		{"muchos decimales", "1234.5678901", "1234.57"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMoneyField(tc.raw, "campo")
			if err != nil {
				t.Fatalf("parseMoneyField(%q) error inesperado: %v", tc.raw, err)
			}
			if got.Exponent() < -montoDecimals {
				t.Errorf("parseMoneyField(%q) = %s tiene más de %d decimales", tc.raw, got, montoDecimals)
			}
			if !got.Equal(decimal.RequireFromString(tc.want)) {
				t.Errorf("parseMoneyField(%q) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseMoneyField_InvalidReturnsError(t *testing.T) {
	t.Parallel()
	if _, err := parseMoneyField("abc", "campo"); err == nil {
		t.Fatal("se esperaba error para entrada no numérica")
	}
}

// La cantidad NO es dinero: conserva su escala completa (NUMERIC(10,4)).
func TestParseDecimalField_CantidadKeepsScale(t *testing.T) {
	t.Parallel()
	got, err := parseDecimalField("1.5", "cantidad")
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if !got.Equal(decimal.RequireFromString("1.5")) {
		t.Errorf("parseDecimalField(\"1.5\") = %s, want 1.5", got)
	}
}

// Verifica el cableado end-to-end: un combo con precio de basura flotante se
// parsea redondeado a 2 decimales (lo que antes causaba el 422).
func TestParseCombosDTO_RoundsComboPrices(t *testing.T) {
	t.Parallel()
	combos, err := parseCombosDTO([]ComboDTO{{
		ID:            "11111111-1111-1111-1111-111111111111",
		Nombre:        "Combo recámara",
		PrecioAnual:   "300.29999999999995",
		PrecioCorto:   "250.10000000000002",
		PrecioContado: "200.005",
		Cantidad:      "1",
	}})
	if err != nil {
		t.Fatalf("parseCombosDTO error inesperado: %v", err)
	}
	if len(combos) != 1 {
		t.Fatalf("se esperaba 1 combo, got %d", len(combos))
	}
	c := combos[0]
	for name, v := range map[string]decimal.Decimal{
		"PrecioAnual": c.PrecioAnual, "PrecioCorto": c.PrecioCorto, "PrecioContado": c.PrecioContado,
	} {
		if v.Exponent() < -montoDecimals {
			t.Errorf("%s = %s tiene más de %d decimales", name, v, montoDecimals)
		}
	}
}
