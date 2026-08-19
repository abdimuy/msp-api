// Package analyticsfb_test — cobranza_signals_test.go covers the bug #2 fix:
//   - coalesceUltimoPago (FechaUltimoPago fallback) — fast unit test, no DB.
//   - leerCobranzaSignals single-payment inclusion + UltimaFecha — FB integration.
//
//nolint:paralleltest // integration subtests share the live dev DB.
//nolint:misspell // Spanish vocabulary by convention.
package analyticsfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

func TestCoalesceUltimoPago(t *testing.T) {
	t.Parallel()

	saldo := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	pagos := time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)
	zero := time.Time{}

	t.Run("saldo cache present → use it (no change for working clients)", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, saldo, analyticsfb.ExportCoalesceUltimoPago(saldo, pagos))
	})

	t.Run("saldo cache zero → fall back to MSP_PAGOS_VENTAS", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, pagos, analyticsfb.ExportCoalesceUltimoPago(zero, pagos))
	})

	t.Run("both zero → zero", func(t *testing.T) {
		t.Parallel()
		assert.True(t, analyticsfb.ExportCoalesceUltimoPago(zero, zero).IsZero())
	})
}

// conceptoCobranzaRuta es "Cobranza en ruta" (catálogo CONCEPTOS_CC). Es uno de
// los tres conceptos que la consulta de señales cuenta como pago.
const conceptoCobranzaRuta = 87327

// TestLeerCobranzaSignals_SinglePaymentIncluded verifies bug #2a: clients with a
// single qualifying payment now appear in the cobranza signals (NUM_PAGOS=1,
// ULTIMA_FECHA set, CADENCIA=0), and two-payment clients carry ULTIMA_FECHA.
//
// ANTES leía dos clientes de producción por identificador —3074781 con un solo
// pago el 2026-02-17, y 114397 con dos, el último el 2025-12-21— y afirmaba esas
// fechas. Bastaba un abono nuevo de cualquiera de los dos para que el que tenía
// "exactamente un pago" pasara a tener dos y la prueba se cayera sola; contra la
// base reducida fallaba siempre, porque MSP_PAGOS_VENTAS va en `-skip_data`.
//
// AHORA siembra los dos casos. La rama de un solo pago y la de cadencia son
// justamente lo que el bug #2a distingue, así que construirlas es más fiel que
// buscar en la base a alguien que por casualidad esté en cada una.
func TestLeerCobranzaSignals_SinglePaymentIncluded(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := analyticsfb.NewRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranzaRuta)

		unPago := time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)
		primero := time.Date(2025, 11, 21, 0, 0, 0, 0, time.UTC)
		segundo := time.Date(2025, 12, 21, 0, 0, 0, 0, time.UTC)

		// Cliente de UN solo pago → rama single-payment.
		clienteUno := microsipseed.Cliente(t, q, "LORENA GARZA VILLASEÑOR")
		ventaUno := microsipseed.VentaCredito(t, q, clienteUno, microsipseed.OpcionesVenta{})
		microsipseed.AbonoAplicado(t, q, ventaUno, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranzaRuta,
			Importe:      decimal.NewFromInt(700),
			Fecha:        unPago,
		})

		// Cliente de DOS pagos → rama de cadencia (un gap).
		clienteDos := microsipseed.Cliente(t, q, "EMILIO ZAVALA PARRA")
		ventaDos := microsipseed.VentaCredito(t, q, clienteDos, microsipseed.OpcionesVenta{})
		for _, f := range []time.Time{primero, segundo} {
			microsipseed.AbonoAplicado(t, q, ventaDos, microsipseed.OpcionesAbono{
				ConceptoCCID: conceptoCobranzaRuta,
				Importe:      decimal.NewFromInt(500),
				Fecha:        f,
			})
		}

		// cutoff only affects PAGOS_90D; irrelevant to NUM_PAGOS/ULTIMA_FECHA here.
		cutoff := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		sigs, err := repo.ExportLeerCobranzaSignals(ctx, cutoff)
		require.NoError(t, err)

		t.Run("single-payment client is present with NUM_PAGOS=1 and ULTIMA_FECHA", func(t *testing.T) {
			s, ok := sigs[clienteUno]
			require.True(t, ok, "el cliente de un solo pago debe aparecer en las señales")
			assert.Equal(t, 1, s.NumPagos)
			assert.Equal(t, 0, s.CadenciaDias, "no hay cadencia con un solo pago")
			assert.False(t, s.UltimaFecha.IsZero(), "ULTIMA_FECHA debe venir poblada")
			assert.Equal(t, unPago.Format("2006-01-02"), s.UltimaFecha.Format("2006-01-02"))
		})

		t.Run("two-payment client carries NUM_PAGOS=2 and ULTIMA_FECHA", func(t *testing.T) {
			s, ok := sigs[clienteDos]
			require.True(t, ok, "el cliente de dos pagos debe aparecer en las señales")
			assert.Equal(t, 2, s.NumPagos)
			assert.Equal(t, segundo.Format("2006-01-02"), s.UltimaFecha.Format("2006-01-02"),
				"ULTIMA_FECHA debe ser el pago más reciente")
		})
	})
}
