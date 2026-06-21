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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestRepo_ContarPagosRecientes(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := analyticsfb.NewRepo(pool)
	ctx := context.Background()
	q := firebird.GetQuerier(ctx, pool.DB)

	// A window known to contain payments in the dev snapshot (Jan–Feb 2026).
	desde := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Pick a real client with the most payments in the window (data-agnostic).
	var clienteID, directCount int
	err := q.QueryRowContext(ctx,
		`SELECT FIRST 1 CLIENTE_ID, COUNT(*) AS N
		   FROM MSP_PAGOS_VENTAS
		  WHERE CANCELADO='N' AND APLICADO='S'
		    AND CONCEPTO_CC_ID IN (87327,155,11)
		    AND FECHA >= ? AND FECHA < ?
		  GROUP BY CLIENTE_ID
		  ORDER BY 2 DESC`,
		firebird.ToWallClock(desde), firebird.ToWallClock(hasta)).Scan(&clienteID, &directCount)
	require.NoError(t, err, "expected at least one paying client in the window")
	require.Positive(t, directCount)

	t.Run("matches a direct COUNT for the same window", func(t *testing.T) {
		got, err := repo.ContarPagosRecientes(ctx, []int{clienteID}, desde, hasta)
		require.NoError(t, err)
		assert.Equal(t, directCount, got[clienteID],
			"live count must equal the direct COUNT for client %d", clienteID)
	})

	t.Run("empty input returns an empty map", func(t *testing.T) {
		got, err := repo.ContarPagosRecientes(ctx, nil, desde, hasta)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("client with no payments in window is absent from the map", func(t *testing.T) {
		// A future window has no payments → the client is absent (not zero-keyed).
		fdesde := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		fhasta := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
		got, err := repo.ContarPagosRecientes(ctx, []int{clienteID}, fdesde, fhasta)
		require.NoError(t, err)
		_, present := got[clienteID]
		assert.False(t, present, "no payments in window → absent from map")
	})
}
