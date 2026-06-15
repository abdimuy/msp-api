//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// TestProperty_DeterministicControl_Stable verifies that deterministicControl
// always returns the same result for the same clienteID, regardless of how many
// times it is called.
func TestProperty_DeterministicControl_Stable(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		id := rapid.IntRange(-1_000_000, 1_000_000).Draw(t, "clienteID")
		first := app.ExportDeterministicControl(id)
		second := app.ExportDeterministicControl(id)
		if first != second {
			t.Fatalf("deterministicControl(%d) is not stable: %v != %v", id, first, second)
		}
	})
}

// TestProperty_ComputeSegmentoScore_Invariants verifies three invariants that
// must hold for all valid WinbackCandidato inputs:
//  1. Determinism: two calls with the same candidato and now return identical results.
//  2. Score range: score.Int() is always in [0, 100].
//  3. Segmento validity: the returned segmento is one of the six recognised values.
func TestProperty_ComputeSegmentoScore_Invariants(t *testing.T) {
	t.Parallel()

	// now is fixed so that recency arithmetic is deterministic across the property run.
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	rapid.Check(t, func(t *rapid.T) {
		clienteID := rapid.IntRange(1, 99_999).Draw(t, "clienteID")
		frecuencia := rapid.IntRange(0, 100).Draw(t, "frecuencia")
		monetary := decimal.NewFromFloat(rapid.Float64Range(0, 100_000).Draw(t, "monetary"))
		saldo := decimal.NewFromFloat(rapid.Float64Range(0, 50_000).Draw(t, "saldo"))
		porLiquidarPct := decimal.NewFromFloat(rapid.Float64Range(0, 100).Draw(t, "porLiquidarPct"))
		telefono := rapid.OneOf(rapid.Just(""), rapid.Just("555-0001")).Draw(t, "telefono")

		// dayOffset == 0 → FechaUltimaCompra is zero (no purchase history).
		// Negative offsets go back in time from now.
		dayOffset := rapid.IntRange(-2_000, 0).Draw(t, "dayOffset")
		var fechaUltimaCompra time.Time
		if dayOffset != 0 {
			fechaUltimaCompra = now.AddDate(0, 0, dayOffset)
		}

		c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
			ID:                uuid.Nil,
			ClienteID:         clienteID,
			Nombre:            "Test",
			Zona:              "Z1",
			Telefono:          telefono,
			FechaUltimaCompra: fechaUltimaCompra,
			Frecuencia:        frecuencia,
			Monetary:          monetary,
			Saldo:             saldo,
			PorLiquidarPct:    porLiquidarPct,
			NextBestProduct:   "",
			EnControl:         false,
			CohorteFecha:      now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		// ── Invariant 1: determinism ──────────────────────────────────────────
		seg1, score1, rec1, _ := app.ExportComputeSegmentoScore(c, now)
		seg2, score2, rec2, _ := app.ExportComputeSegmentoScore(c, now)
		if seg1 != seg2 {
			t.Fatalf("non-deterministic segmento: %v != %v", seg1, seg2)
		}
		if score1 != score2 {
			t.Fatalf("non-deterministic score: %v != %v", score1, score2)
		}
		if rec1 != rec2 {
			t.Fatalf("non-deterministic recencia: %d != %d", rec1, rec2)
		}

		// ── Invariant 2: score range [0, 100] ────────────────────────────────
		if score1.Int() < 0 || score1.Int() > 100 {
			t.Fatalf("score out of [0,100]: %d", score1.Int())
		}

		// ── Invariant 3: segmento is a recognised value ───────────────────────
		if !seg1.IsValid() {
			t.Fatalf("invalid segmento: %q", seg1)
		}
	})
}
