//nolint:misspell // Spanish domain vocabulary by project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestCalcularPlanPago_FormulaDecidida(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		precio     string
		periodos   int
		wantEng    string
		wantParcia string
	}{
		// enganche = round50(0.10*precio); parcialidad = round50((precio-eng)/periodos).
		{"comedor_8000_52sem", "8000", 52, "800", "150"}, // eng=800; (7200/52=138.4)→150
		{"refri_5000_52sem", "5000", 52, "500", "100"},   // eng=500; (4500/52=86.5)→100
		{"colchon_3000_48sem", "3000", 48, "300", "50"},  // eng=300; (2700/48=56.25)→50
		{"redondeo_eng_a_50", "5250", 52, "550", "100"},  // 0.10*5250=525→550
		{"mensual_12", "12000", 12, "1200", "900"},       // eng=1200; 10800/12=900
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := domain.CalcularPlanPago(decimal.RequireFromString(tc.precio), tc.periodos, "semanal")
			require.NoError(t, err)
			assert.True(t, plan.Enganche().Equal(decimal.RequireFromString(tc.wantEng)),
				"enganche: got %s want %s", plan.Enganche(), tc.wantEng)
			assert.True(t, plan.Parcialidad().Equal(decimal.RequireFromString(tc.wantParcia)),
				"parcialidad: got %s want %s", plan.Parcialidad(), tc.wantParcia)
			// Todo monto es múltiplo de 50.
			fifty := decimal.NewFromInt(50)
			assert.True(t, plan.Enganche().Mod(fifty).IsZero(), "enganche múltiplo de 50")
			assert.True(t, plan.Parcialidad().Mod(fifty).IsZero(), "parcialidad múltiplo de 50")
		})
	}
}

func TestCalcularPlanPago_Invalidos(t *testing.T) {
	t.Parallel()
	_, err := domain.CalcularPlanPago(decimal.Zero, 52, "semanal")
	require.ErrorIs(t, err, domain.ErrPrecioInvalido)
	_, err = domain.CalcularPlanPago(decimal.RequireFromString("-5"), 52, "semanal")
	require.ErrorIs(t, err, domain.ErrPrecioInvalido)
	_, err = domain.CalcularPlanPago(decimal.RequireFromString("5000"), 0, "semanal")
	require.ErrorIs(t, err, domain.ErrPeriodosInvalido)
}

func TestPlanPago_Accessors(t *testing.T) {
	t.Parallel()
	plan, err := domain.CalcularPlanPago(decimal.RequireFromString("5000"), 52, "semanal")
	require.NoError(t, err)
	assert.Equal(t, 52, plan.Periodos())
	assert.Equal(t, "semanal", plan.Cadencia())
}
