package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestCalcularCobro(t *testing.T) {
	t.Parallel()
	pct := decimal.RequireFromString("65.90")
	c := domain.CalcularCobro(domain.CobroSemana{
		Ventas: []domain.CobroVenta{
			{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("150"), AplicaPonderado: true},
			{VentaID: 2, Parcialidad: dinero("200"), AbonoSemana: dinero("0"), AplicaPonderado: true},
			{VentaID: 3, Parcialidad: dinero("300"), AbonoSemana: dinero("100"), AplicaPonderado: false},
		},
		PctPonderado: &pct,
	})
	assert.Equal(t, "250.00", c.Cobrado.StringFixed(2))
	assert.Equal(t, "350.00", c.Esperado.StringFixed(2))
	assert.Equal(t, "100.00", c.Falta.StringFixed(2), "esperado − cobrado")
	require.NotNil(t, c.Pct)
	assert.Equal(t, "65.90", c.Pct.StringFixed(2))

	vacio := domain.CalcularCobro(domain.CobroSemana{})
	assert.Equal(t, "0.00", vacio.Cobrado.StringFixed(2))
	assert.Equal(t, "0.00", vacio.Falta.StringFixed(2))

	sobrado := domain.CalcularCobro(domain.CobroSemana{Ventas: []domain.CobroVenta{
		{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("400"), AplicaPonderado: true},
	}})
	assert.Equal(t, "0.00", sobrado.Falta.StringFixed(2), "cobrar de más no da falta negativa")
	assert.Nil(t, vacio.Pct)
}
