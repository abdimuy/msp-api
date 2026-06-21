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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
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

// TestLeerCobranzaSignals_SinglePaymentIncluded verifies bug #2a: clients with a
// single qualifying payment now appear in the cobranza signals (NUM_PAGOS=1,
// ULTIMA_FECHA set, CADENCIA=0), and two-payment clients carry ULTIMA_FECHA.
func TestLeerCobranzaSignals_SinglePaymentIncluded(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := analyticsfb.NewRepo(pool)
	ctx := context.Background()

	// cutoff only affects PAGOS_90D; irrelevant to NUM_PAGOS/ULTIMA_FECHA here.
	cutoff := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	sigs, err := repo.ExportLeerCobranzaSignals(ctx, cutoff)
	require.NoError(t, err)

	t.Run("single-payment client is present with NUM_PAGOS=1 and ULTIMA_FECHA", func(t *testing.T) {
		// 3074781: exactly one payment (2026-02-17) — previously excluded.
		s, ok := sigs[3074781]
		require.True(t, ok, "single-payment client must now appear in cobranza signals")
		assert.Equal(t, 1, s.NumPagos)
		assert.Equal(t, 0, s.CadenciaDias, "no cadence with a single payment")
		assert.False(t, s.UltimaFecha.IsZero(), "ULTIMA_FECHA must be populated")
		assert.Equal(t, "2026-02-17", s.UltimaFecha.Format("2006-01-02"))
	})

	t.Run("two-payment client carries NUM_PAGOS=2 and ULTIMA_FECHA", func(t *testing.T) {
		// 114397: two payments, last 2025-12-21 — saldo cache was NULL for this one.
		s, ok := sigs[114397]
		require.True(t, ok)
		assert.Equal(t, 2, s.NumPagos)
		assert.Equal(t, "2025-12-21", s.UltimaFecha.Format("2006-01-02"))
	})
}
