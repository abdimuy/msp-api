// Package analyticsfb_test — contar_pagos_repo_test.go is a Firebird integration
// test for ContarPagosRecientes (live trailing-window payment count). Read-only:
// it queries MSP_PAGOS_VENTAS and asserts the method agrees with a direct COUNT
// for the same window, so it is self-validating regardless of dev data drift.
//
//nolint:paralleltest // serial: shares the live dev DB.
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

// conceptoCobranzaContar es "Cobranza en ruta", uno de los conceptos que
// ContarPagosRecientes cuenta como pago.
const conceptoCobranzaContar = 87327

// TestRepo_ContarPagosRecientes verifica el conteo de pagos en una ventana.
//
// ANTES elegía "el cliente real con más pagos en la ventana" y lo contrastaba
// contra un COUNT directo. La idea era buena —se comparaba contra la base, no
// contra un número escrito— pero seguía exigiendo que la base COMPARTIDA tuviera
// pagos entre enero y marzo de 2026: contra la base reducida, donde
// MSP_PAGOS_VENTAS va en `-skip_data`, el SELECT no devolvía ni una fila y la
// prueba moría con "sql: no rows in result set".
//
// AHORA siembra sus propios pagos, y conserva la comparación contra el COUNT
// directo — que es lo que hacía valiosa a la prueba original.
func TestRepo_ContarPagosRecientes(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := analyticsfb.NewRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranzaContar)

		desde := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		hasta := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

		// Tres pagos dentro de la ventana y uno FUERA, para que el conteo tenga
		// que descartar algo y no baste con contarlo todo.
		clienteID := microsipseed.Cliente(t, q, "NORMA IBARRA GALLEGOS")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{})
		const enVentana = 3
		fechas := []time.Time{
			time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 24, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC), // fuera de [desde, hasta)
		}
		for _, f := range fechas {
			microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: conceptoCobranzaContar,
				Importe:      decimal.NewFromInt(300),
				Fecha:        f,
			})
		}

		// El COUNT directo se conserva: la prueba sigue contrastando el método
		// contra la base, no contra una constante.
		var directCount int
		err := q.QueryRowContext(ctx,
			`SELECT COUNT(*)
			   FROM MSP_PAGOS_VENTAS
			  WHERE CLIENTE_ID = ?
			    AND CANCELADO='N' AND APLICADO='S'
			    AND CONCEPTO_CC_ID IN (87327,155,11)
			    AND FECHA >= ? AND FECHA < ?`,
			clienteID, firebird.ToWallClock(desde), firebird.ToWallClock(hasta)).Scan(&directCount)
		require.NoError(t, err)
		require.Equal(t, enVentana, directCount,
			"el COUNT directo debe ver los %d pagos sembrados dentro de la ventana", enVentana)

		t.Run("matches a direct COUNT for the same window", func(t *testing.T) {
			got, err := repo.ContarPagosRecientes(ctx, []int{clienteID}, desde, hasta)
			require.NoError(t, err)
			assert.Equal(t, directCount, got[clienteID],
				"el conteo del repositorio debe igualar al COUNT directo del cliente %d", clienteID)
		})

		t.Run("empty input returns an empty map", func(t *testing.T) {
			got, err := repo.ContarPagosRecientes(ctx, nil, desde, hasta)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("client with no payments in window is absent from the map", func(t *testing.T) {
			// Una ventana sin pagos → el cliente no aparece (no queda en cero).
			fdesde := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			fhasta := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
			got, err := repo.ContarPagosRecientes(ctx, []int{clienteID}, fdesde, fhasta)
			require.NoError(t, err)
			_, present := got[clienteID]
			assert.False(t, present, "sin pagos en la ventana → ausente del mapa")
		})
	})
}
