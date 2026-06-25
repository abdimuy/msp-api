//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func TestCalcAporte(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		parcialidad  string
		plazos       string
		totalImporte string
		abonoSemana  string
		saldoHoy     string
		want         string
	}{
		// 1. Al corriente, paga 1x
		{"al_corriente_1x", "100", "10", "4000", "100", "2900", "1.00"},
		// 2. Atrasado, paga 2x
		{"atrasado_2x", "100", "10", "4000", "200", "3300", "2.00"},
		// 3. Paga la mitad — verifica división decimal
		{"paga_mitad", "200", "5", "4000", "100", "3000", "0.50"},
		// 4. Vieja pasada de plazo — verifica tope debia
		{"vieja_tope_debia", "200", "400", "4000", "600", "1400", "3.00"},
		// 5. Al corriente, paga de más
		{"al_corriente_paga_demas", "100", "10", "4000", "300", "2800", "2.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := rutasdomain.AporteInput{
				Parcialidad:  decimal.RequireFromString(tc.parcialidad),
				Plazos:       decimal.RequireFromString(tc.plazos),
				TotalImporte: decimal.RequireFromString(tc.totalImporte),
				AbonoSemana:  decimal.RequireFromString(tc.abonoSemana),
				SaldoHoy:     decimal.RequireFromString(tc.saldoHoy),
			}
			got := rutasdomain.CalcAporte(in)
			assert.True(t,
				decimal.RequireFromString(tc.want).Equal(got),
				"CalcAporte(%+v) = %s, want %s", in, got.StringFixed(4), tc.want,
			)
		})
	}
}

func TestCalcAporte_ZeroParcialidad(t *testing.T) {
	t.Parallel()
	in := rutasdomain.AporteInput{
		Parcialidad:  decimal.Zero,
		Plazos:       decimal.NewFromInt(5),
		TotalImporte: decimal.NewFromInt(4000),
		AbonoSemana:  decimal.NewFromInt(100),
		SaldoHoy:     decimal.NewFromInt(3000),
	}
	got := rutasdomain.CalcAporte(in)
	assert.True(t, decimal.Zero.Equal(got), "zero parcialidad must yield 0, got %s", got)
}

func TestCadenciaDias(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 7, rutasdomain.CadenciaDias(rutasdomain.Semanal))
	assert.Equal(t, 15, rutasdomain.CadenciaDias(rutasdomain.Quincenal))
	assert.Equal(t, 30, rutasdomain.CadenciaDias(rutasdomain.Mensual))
	assert.Equal(t, 7, rutasdomain.CadenciaDias("UNKNOWN")) // default Semanal
}

func TestFrecuencia_EsContado(t *testing.T) {
	t.Parallel()
	cases := []struct {
		frec rutasdomain.Frecuencia
		want bool
	}{
		{rutasdomain.Contado, true},
		{rutasdomain.Semanal, false},
		{rutasdomain.Quincenal, false},
		{rutasdomain.Mensual, false},
		{"", false},
		{"OTRO", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.frec.EsContado(), "EsContado(%q)", tc.frec)
	}
}
