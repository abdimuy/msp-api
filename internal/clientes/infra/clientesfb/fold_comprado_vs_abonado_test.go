//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoldCompradoVsAbonadoFilas_CategoriasCorrectas(t *testing.T) {
	t.Parallel()

	d := func(s string) decimal.Decimal {
		v, _ := decimal.NewFromString(s)
		return v
	}

	filas := []compradoVsAbonadoFila{
		{anio: 2024, mes: 7, concepto: -1, comprado: d("5000.00"), abonado: d("0")},
		{anio: 2024, mes: 7, concepto: 87327, comprado: d("0"), abonado: d("300.00")},
		{anio: 2024, mes: 7, concepto: 27969, comprado: d("0"), abonado: d("900.00")},
		{anio: 2024, mes: 7, concepto: 27968, comprado: d("0"), abonado: d("4150.00")},
		{anio: 2024, mes: 7, concepto: 99999, comprado: d("0"), abonado: d("50.00")},
	}

	pts := foldCompradoVsAbonadoFilas(filas)
	require.Len(t, pts, 1, "one month => one punto")

	pt := pts[0]
	assert.Equal(t, 2024, pt.Anio)
	assert.Equal(t, 7, pt.Mes)
	assert.True(t, pt.Comprado.Equal(d("5000.00")), "Comprado=%s", pt.Comprado)
	assert.True(t, pt.Cobranza.Equal(d("300.00")), "Cobranza=%s", pt.Cobranza)
	assert.True(t, pt.Condonacion.Equal(d("900.00")), "Condonacion=%s", pt.Condonacion)
	assert.True(t, pt.Perdida.Equal(d("4150.00")), "Perdida=%s", pt.Perdida)
	assert.True(t, pt.Otro.Equal(d("50.00")), "Otro=%s", pt.Otro)
	assert.True(t, pt.Enganche.IsZero(), "Enganche should be zero (not in fixture)")
}

func TestFoldCompradoVsAbonadoFilas_MultiMes(t *testing.T) {
	t.Parallel()

	d := func(s string) decimal.Decimal {
		v, _ := decimal.NewFromString(s)
		return v
	}

	filas := []compradoVsAbonadoFila{
		{anio: 2024, mes: 7, concepto: -1, comprado: d("1000.00"), abonado: d("0")},
		{anio: 2024, mes: 7, concepto: 87327, comprado: d("0"), abonado: d("200.00")},
		{anio: 2024, mes: 6, concepto: -1, comprado: d("2000.00"), abonado: d("0")},
		{anio: 2024, mes: 6, concepto: 24533, comprado: d("0"), abonado: d("400.00")},
	}

	pts := foldCompradoVsAbonadoFilas(filas)
	require.Len(t, pts, 2, "two months => two puntos")

	assert.Equal(t, 6, pts[0].Mes)
	assert.True(t, pts[0].Comprado.Equal(d("2000.00")))
	assert.True(t, pts[0].Enganche.Equal(d("400.00")))
	assert.True(t, pts[0].Cobranza.IsZero())

	assert.Equal(t, 7, pts[1].Mes)
	assert.True(t, pts[1].Comprado.Equal(d("1000.00")))
	assert.True(t, pts[1].Cobranza.Equal(d("200.00")))
	assert.True(t, pts[1].Enganche.IsZero())
}
