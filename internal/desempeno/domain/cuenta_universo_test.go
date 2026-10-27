package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Resolución del arquitecto sobre D-API-7: `nueva` es la regla del teléfono
// (msp-app-kt OrdenDeCobranza.kt:103, `sinAbonos = saldo == totalVenta - enganche`):
// nadie ha abonado todavía.
func TestCuentaUniverso_EsNueva(t *testing.T) {
	t.Parallel()
	cuenta := func(saldo, total, enganche string) domain.CuentaUniverso {
		return domain.CuentaUniverso{Saldo: dinero(saldo), PrecioTotal: dinero(total), Enganche: dinero(enganche)}
	}
	assert.True(t, cuenta("3600", "4000", "400").EsNueva(), "saldo = total − enganche: sin abonos")
	assert.True(t, cuenta("3600.00", "4000", "400.0").EsNueva(), "la escala del decimal no importa")
	assert.True(t, cuenta("4000", "4000", "0").EsNueva(), "sin enganche y sin abonos")
	assert.False(t, cuenta("3450", "4000", "400").EsNueva(), "ya abonó 150")
	assert.False(t, cuenta("3599.99", "4000", "400").EsNueva(), "un centavo abonado ya no es nueva")
	assert.False(t, cuenta("0", "4000", "400").EsNueva(), "liquidada")
}
