//nolint:misspell // rutas vocabulary is Spanish per project convention.
//nolint:paralleltest // serial: shares rollback-only tx.
package rutasfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

// TestCobranzaRepo_VentasPorZona verifica que el listado de cobranza por zona
// devuelva la venta a crédito de esa zona con sus importes.
//
// ANTES pedía la zona de producción 12271 con una ventana de "los últimos 30
// días para capturar cualquier dato real", y su único aserto fuerte era
// `assert.NotNil(ventas)`. Dos problemas: contra una base sin movimientos el
// repositorio devuelve un slice nil y la prueba FALLA (así se descubrió), y
// contra la base de desarrollo pasaba sin comprobar nada porque el ciclo podía
// recorrer cero filas.
//
// AHORA siembra su propia venta a crédito en una zona del catálogo, así que
// puede exigir que aparezca y con qué cifras.
func TestCobranzaRepo_VentasPorZona(t *testing.T) { //nolint:paralleltest // serial: shares rollback-only tx.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		repo := rutasfb.NewCobranzaRepo(pool)

		zona := microsipseed.PrimeraZona(t, q)
		clienteID := microsipseed.ClienteEnZona(t, q, "SILVIA CARRANZA MEZA", zona)
		// La venta se fecha hoy para caer dentro de la ventana consultada.
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Fecha: time.Now().UTC(),
			Total: decimal.NewFromInt(12000),
		})

		desde := time.Now().UTC().AddDate(0, 0, -30)
		hasta := time.Now().UTC().AddDate(0, 0, 1)

		ventas, err := repo.VentasPorZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		require.NotEmpty(t, ventas, "la venta sembrada en la zona %d debe aparecer", zona)

		var sembrada bool
		for _, v := range ventas {
			assert.NotZero(t, v.VentaID)
			assert.NotZero(t, v.ClienteID)
			assert.False(t, v.Saldo.IsNegative(), "el saldo no debe ser negativo")
			// Las ventas de contado nunca deben aparecer en el conjunto de
			// cobranza: no son crédito y inflarían el % ponderado.
			assert.False(t, v.Frecuencia.EsContado(),
				"venta %d de contado no debe aparecer en cobranza", v.VentaID)
			if v.ClienteID == clienteID {
				sembrada = true
				assert.Equal(t, venta.CargoCCID, v.VentaID, "VentaID = DOCTO_CC_ID del cargo")
				assert.True(t, v.Saldo.Equal(venta.Total),
					"sin abonos el saldo debe ser el total (esperado %s, obtenido %s)",
					venta.Total, v.Saldo)
			}
		}
		assert.True(t, sembrada, "el cliente %d sembrado debe estar en el resultado", clienteID)
	})
}
