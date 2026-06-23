//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestDesglosarAporte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id                                                 string
		parcialidad, vencidas, abono, aporte               decimal.Decimal
		antesC, antesP, pagoC, aporteC, despuesC, despuesP string
	}{
		// debía 2 cuotas, pagó 1 (parcialidad 100): después queda 1.
		{"normal", dec("100"), dec("2"), dec("100"), dec("1"), "2", "200", "1", "1", "1", "100"},
		// pagó de más (3 cuotas) sobre 2 vencidas → después 0 (no negativo).
		{"sobrepago", dec("100"), dec("2"), dec("300"), dec("2"), "2", "200", "3", "2", "0", "0"},
		// sin atraso, sin pago.
		{"cero", dec("100"), dec("0"), dec("0"), dec("0"), "0", "0", "0", "0", "0", "0"},
		// parcialidad 0 → no divide; pago en cuotas 0.
		{"parcialidad_cero", dec("0"), dec("0"), dec("0"), dec("0"), "0", "0", "0", "0", "0", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := rutasdomain.DesglosarAporte(tc.parcialidad, tc.vencidas, tc.abono, tc.aporte)
			assert.True(t, got.AtrasoAntesCuotas.Equal(dec(tc.antesC)), "antesC")
			assert.True(t, got.AtrasoAntesPesos.Equal(dec(tc.antesP)), "antesP")
			assert.True(t, got.PagoCuotas.Equal(dec(tc.pagoC)), "pagoC")
			assert.True(t, got.AporteCuotas.Equal(dec(tc.aporteC)), "aporteC")
			assert.True(t, got.AtrasoDespuesCuotas.Equal(dec(tc.despuesC)), "despuesC")
			assert.True(t, got.AtrasoDespuesPesos.Equal(dec(tc.despuesP)), "despuesP")
		})
	}
}

func TestCalcularResumenPonderado(t *testing.T) {
	t.Parallel()
	ventas := []rutasdomain.VentaCobranza{
		{Aporte: dec("1.0"), AplicaPonderado: true},
		{Aporte: dec("0.5"), AplicaPonderado: true},
		{Aporte: dec("1.0"), AplicaPonderado: false}, // no cuenta
	}
	got := rutasdomain.CalcularResumenPonderado(ventas)
	assert.Equal(t, 2, got.Denominador)
	assert.True(t, got.Numerador.Equal(dec("1.5")), "numerador")
	if assert.NotNil(t, got.Pct) {
		assert.True(t, got.Pct.Equal(dec("75")), "pct = 1.5/2*100")
	}

	// Sin ventas que aplican → Pct nil, denominador 0.
	empty := rutasdomain.CalcularResumenPonderado([]rutasdomain.VentaCobranza{{Aporte: dec("1"), AplicaPonderado: false}})
	assert.Equal(t, 0, empty.Denominador)
	assert.Nil(t, empty.Pct)
}
