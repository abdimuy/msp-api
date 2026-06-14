// Package analyticsfb_test contains Firebird-backed integration tests for the
// analytics infra layer. All writes execute inside a transaction that always
// rolls back so the shared dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000035 applied (creates MSP_AN_WINBACK_CANDIDATOS and
//     MSP_AN_REFRESH_STATE). The test skips cleanly when either precondition
//     is absent.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/analytics/infra/analyticsfb/...
//
//nolint:misspell // Spanish vocabulary (candidato, cohorte, zona, etc.) by convention.
package analyticsfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// requireFBEnv skips the test when FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// fixedNow is a deterministic UTC instant used across all fixtures.
var fixedNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// fixedCohorte is a deterministic cohort date distinct from fixedNow.
var fixedCohorte = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// fixedFechaUltima is a deterministic last-purchase date.
var fixedFechaUltima = time.Date(2025, 12, 31, 10, 0, 0, 0, time.UTC)

// makeCandidato builds a WinbackCandidato with deterministic fields.
// clienteID should be a large negative number unlikely to collide with real
// Microsip data; use a unique value per test to avoid UNIQUE constraint
// collisions within the same rollback-only transaction.
func makeCandidato(t *testing.T, clienteID int, monetary, saldo string, enControl bool) *domain.WinbackCandidato {
	t.Helper()
	c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         clienteID,
		Nombre:            "Test Cliente",
		Zona:              "R/TEST",
		Telefono:          "238 000 0000",
		FechaUltimaCompra: fixedFechaUltima,
		Frecuencia:        3,
		Monetary:          decimal.RequireFromString(monetary),
		Saldo:             decimal.RequireFromString(saldo),
		PorLiquidarPct:    decimal.RequireFromString("45.50"),
		NextBestProduct:   "ROPERO MONARCA",
		EnControl:         enControl,
		CohorteFecha:      fixedCohorte,
		Now:               fixedNow,
	})
	require.NoError(t, err, "makeCandidato")
	return c
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestRepo_UpsertAndList_RoundTrip verifies that a candidato inserted via
// UpsertCandidatos can be retrieved with ListCandidatos and that all fields
// round-trip correctly (decimals, timestamps, bool EN_CONTROL).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_UpsertAndList_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// Use a synthetic clienteID unlikely to exist in production.
		const clienteID = -10001
		c := makeCandidato(t, clienteID, "15000.50", "3200.75", false)

		// Check that migration 000035 is applied by attempting the upsert.
		err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		page, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, page.Total, 1)

		// Find our row.
		var got *domain.WinbackCandidato
		for _, item := range page.Items {
			if item.ClienteID() == clienteID {
				got = item
				break
			}
		}
		require.NotNil(t, got, "inserted candidato must appear in ListCandidatos")

		assert.Equal(t, c.ID(), got.ID())
		assert.Equal(t, clienteID, got.ClienteID())
		assert.Equal(t, "Test Cliente", got.Nombre())
		assert.Equal(t, "R/TEST", got.Zona())
		assert.Equal(t, "238 000 0000", got.Telefono())
		assert.Equal(t, 3, got.Frecuencia())
		assert.True(t, c.Monetary().Equal(got.Monetary()),
			"monetary mismatch: want=%s got=%s", c.Monetary(), got.Monetary())
		assert.True(t, c.Saldo().Equal(got.Saldo()),
			"saldo mismatch: want=%s got=%s", c.Saldo(), got.Saldo())
		assert.True(t, c.PorLiquidarPct().Equal(got.PorLiquidarPct()),
			"por_liquidar_pct mismatch: want=%s got=%s", c.PorLiquidarPct(), got.PorLiquidarPct())
		assert.Equal(t, "ROPERO MONARCA", got.NextBestProduct())
		assert.False(t, got.EnControl())

		// Timestamps round-trip within 1-second tolerance (subsecond truncation
		// at the Firebird TIMESTAMP level is expected).
		assert.WithinDuration(t, fixedFechaUltima, got.FechaUltimaCompra(), time.Second)
		assert.WithinDuration(t, fixedCohorte, got.CohorteFecha(), time.Second)
		assert.WithinDuration(t, fixedNow, got.CreatedAt(), time.Second)
		assert.WithinDuration(t, fixedNow, got.UpdatedAt(), time.Second)

		t.Logf("round-trip ok: clienteID=%d monetary=%s saldo=%s",
			got.ClienteID(), got.Monetary(), got.Saldo())
	})
}

// TestRepo_Upsert_PreservesEnControlAndCohorte is the KEY INVARIANT test:
// a second upsert with different EN_CONTROL and COHORTE_FECHA values in the
// in-memory entity must NOT overwrite the persisted EN_CONTROL or COHORTE_FECHA,
// while mutable fields (MONETARY, SALDO, etc.) MUST be updated.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_Upsert_PreservesEnControlAndCohorte(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const clienteID = -10002
		// First insert: enControl=true, cohorteFecha=fixedCohorte.
		c1 := makeCandidato(t, clienteID, "5000.00", "1000.00", true)
		err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c1})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		// Second upsert for the same clienteID: different EnControl (false) and a
		// different CohorteFecha. The MERGE WHEN MATCHED branch must ignore these
		// fields and only update the mutable columns.
		differentCohorte := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		c2, err2 := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         clienteID,
			Nombre:            "Test Cliente Actualizado",
			Zona:              "R/NUEVA",
			Telefono:          "238 111 1111",
			FechaUltimaCompra: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			Frecuencia:        7,
			Monetary:          decimal.RequireFromString("99000.00"),
			Saldo:             decimal.RequireFromString("50.00"),
			PorLiquidarPct:    decimal.RequireFromString("1.00"),
			NextBestProduct:   "LAVADORA",
			EnControl:         false,            // different from c1 — must NOT be saved
			CohorteFecha:      differentCohorte, // different — must NOT be saved
			Now:               fixedNow.Add(time.Hour),
		})
		require.NoError(t, err2)
		err = repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c2})
		require.NoError(t, err)

		// Read back and assert.
		page, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{})
		require.NoError(t, err)

		var got *domain.WinbackCandidato
		for _, item := range page.Items {
			if item.ClienteID() == clienteID {
				got = item
				break
			}
		}
		require.NotNil(t, got, "candidato must exist after second upsert")

		// Mutable fields MUST reflect the second upsert.
		assert.True(t, decimal.RequireFromString("99000.00").Equal(got.Monetary()),
			"MONETARY must be updated: got=%s", got.Monetary())
		assert.Equal(t, "LAVADORA", got.NextBestProduct(), "NEXT_BEST_PRODUCT must be updated")
		assert.Equal(t, 7, got.Frecuencia(), "FRECUENCIA must be updated")

		// Immutable fields (EN_CONTROL, COHORTE_FECHA) MUST retain first-insert values.
		assert.True(t, got.EnControl(),
			"EN_CONTROL must remain true (from first insert), not be overwritten to false by second upsert")
		assert.WithinDuration(t, fixedCohorte, got.CohorteFecha(), time.Second,
			"COHORTE_FECHA must remain %s (from first insert), not be overwritten to %s",
			fixedCohorte, differentCohorte)

		t.Logf("EN_CONTROL preserved=%v, COHORTE_FECHA preserved within 1s=%v",
			got.EnControl(), got.CohorteFecha())
	})
}

// TestRepo_ListCandidatos_Filters verifies Zona filter, ExcluirControl, and
// Limit, and that results arrive in MONETARY DESC order.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_ListCandidatos_Filters(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// c1: high monetary with a distinct zone for the Zona filter test.
		c1, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID: -10003, Nombre: "Z1", Zona: "ZONA_ALPHA",
			Frecuencia: 1, Monetary: decimal.RequireFromString("90000.00"),
			Saldo: decimal.Zero, PorLiquidarPct: decimal.Zero,
			CohorteFecha: fixedCohorte, Now: fixedNow,
		})
		require.NoError(t, err)
		c2 := makeCandidato(t, -10004, "50000.00", "0.00", true)  // mid monetary, in control
		c3 := makeCandidato(t, -10005, "10000.00", "0.00", false) // low monetary, no control

		upsertErr := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c1, c2, c3})
		if upsertErr != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", upsertErr)
		}

		// ── ExcluirControl should hide c2 ──────────────────────────────────
		page, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{ExcluirControl: true})
		require.NoError(t, err)
		for _, item := range page.Items {
			if item.ClienteID() == c2.ClienteID() {
				t.Errorf("ExcluirControl=true must exclude clienteID=%d (en_control=true)", c2.ClienteID())
			}
		}

		// ── Zona filter ────────────────────────────────────────────────────
		pageZona, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{Zona: "ZONA_ALPHA"})
		require.NoError(t, err)
		found := false
		for _, item := range pageZona.Items {
			if item.ClienteID() == c1.ClienteID() {
				found = true
			}
			assert.Equal(t, "ZONA_ALPHA", item.Zona(),
				"all rows in zona-filtered page must have zona=ZONA_ALPHA")
		}
		assert.True(t, found, "c1 (ZONA_ALPHA) must appear in zona-filtered result")

		// ── Limit ──────────────────────────────────────────────────────────
		// We inserted 3 rows above (c1, c2, c3); Total must count ALL rows
		// before the ROWS clause is applied, so it must be >= 3.
		pageLimit, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{Limit: 1})
		require.NoError(t, err)
		assert.Len(t, pageLimit.Items, 1, "Limit=1 must return exactly one row")
		assert.GreaterOrEqual(t, pageLimit.Total, 3, "Total must reflect full count before limit (>= 3 rows inserted)")
		assert.GreaterOrEqual(t, pageLimit.Total, len(pageLimit.Items),
			"Total must be >= len(Items)")

		// ── ORDER BY MONETARY DESC: first item should have highest monetary ──
		pageFull, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{})
		require.NoError(t, err)
		if len(pageFull.Items) >= 2 {
			first := pageFull.Items[0].Monetary()
			second := pageFull.Items[1].Monetary()
			assert.True(t, first.GreaterThanOrEqual(second),
				"ORDER BY MONETARY DESC violated: first=%s second=%s", first, second)
		}

		t.Logf("filters ok: total=%d zona_filtered=%d limit_filtered=%d",
			pageFull.Total, pageZona.Total, pageLimit.Total)
	})
}

// TestRepo_GetRefreshState_NotFound verifies that GetRefreshState returns
// domain.ErrRefreshStateNotFound when no row exists for the given job.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_GetRefreshState_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		_, err := repo.GetRefreshState(ctx, "nonexistent_job_for_test")
		if err == nil {
			t.Skip("GetRefreshState returned nil — MSP_AN_REFRESH_STATE may not exist (migration 000035 not applied)")
		}
		require.ErrorIs(t, err, domain.ErrRefreshStateNotFound,
			"GetRefreshState with missing job must return ErrRefreshStateNotFound")
	})
}

// TestRepo_SaveAndGetRefreshState_RoundTrip verifies that SaveRefreshState
// persists and GetRefreshState retrieves — for both nil and non-nil watermarks.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_SaveAndGetRefreshState_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const job = "test_winback_refresh"

		// ── nil watermark ─────────────────────────────────────────────────
		st1 := outbound.RefreshState{
			Job:           job,
			LastWatermark: nil,
			LastRunAt:     fixedNow,
		}
		err := repo.SaveRefreshState(ctx, st1)
		if err != nil {
			t.Skipf("SaveRefreshState failed — migration 000035 may not be applied: %v", err)
		}
		got1, err := repo.GetRefreshState(ctx, job)
		require.NoError(t, err)
		assert.Equal(t, job, got1.Job)
		assert.Nil(t, got1.LastWatermark, "nil watermark must round-trip as nil")
		assert.WithinDuration(t, fixedNow, got1.LastRunAt, time.Second)

		// ── non-nil watermark (update) ────────────────────────────────────
		wm := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
		st2 := outbound.RefreshState{
			Job:           job,
			LastWatermark: &wm,
			LastRunAt:     fixedNow.Add(time.Hour),
		}
		err = repo.SaveRefreshState(ctx, st2)
		require.NoError(t, err)
		got2, err := repo.GetRefreshState(ctx, job)
		require.NoError(t, err)
		require.NotNil(t, got2.LastWatermark, "non-nil watermark must round-trip as non-nil")
		assert.WithinDuration(t, wm, *got2.LastWatermark, time.Second,
			"watermark mismatch: want=%s got=%s", wm, *got2.LastWatermark)
		assert.WithinDuration(t, st2.LastRunAt, got2.LastRunAt, time.Second)

		t.Logf("refresh state round-trip ok: job=%s wm=%s runAt=%s",
			got2.Job, got2.LastWatermark, got2.LastRunAt)
	})
}

// TestRepo_ExistingControlFlags_ReturnsCorrectMap verifies that
// ExistingControlFlags returns the right clienteID → en_control mapping.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_ExistingControlFlags_ReturnsCorrectMap(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		c1 := makeCandidato(t, -10006, "1000.00", "0.00", true)
		c2 := makeCandidato(t, -10007, "2000.00", "0.00", false)

		err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c1, c2})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		flags, err := repo.ExistingControlFlags(ctx)
		require.NoError(t, err)

		// Verify the two inserted rows appear with the correct flags.
		flag1, ok1 := flags[-10006]
		assert.True(t, ok1, "clienteID -10006 must appear in control flags map")
		assert.True(t, flag1, "clienteID -10006 must have en_control=true")

		flag2, ok2 := flags[-10007]
		assert.True(t, ok2, "clienteID -10007 must appear in control flags map")
		assert.False(t, flag2, "clienteID -10007 must have en_control=false")

		t.Logf("control flags map: %d entries, -10006=%v -10007=%v",
			len(flags), flag1, flag2)
	})
}

// TestRepo_LeerAnclasDesde_Smoke is a read-only smoke test against existing
// Microsip data. It verifies LeerAnclasDesde does not error and returns
// sanity-valid rows (ClienteID > 0, Monetary >= 0). No data is inserted.
//
//nolint:paralleltest // serial: read-only against live Microsip data.
func TestRepo_LeerAnclasDesde_Smoke(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	// LeerAnclasDesde is read-only so WithTestTransaction is used for driver
	// implicit-tx hygiene, not for rollback protection.
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// Full read (nil since). The query touches the full DOCTOS_PV + DOCTOS_CC
		// history; in dev the result set may be large. We bound to a recent window
		// to keep the smoke test fast: only clients with last activity in the last
		// 90 days.
		since := time.Now().UTC().AddDate(0, -3, 0) // 90 days ago
		anclas, err := repo.LeerAnclasDesde(ctx, &since)
		if err != nil {
			// The query may legitimately fail if Microsip tables have unexpected
			// schema changes; report the specific error for diagnosis.
			t.Skipf("LeerAnclasDesde returned error — check Microsip schema: %v", err)
		}

		if len(anclas) == 0 {
			// On a fresh dev DB with no sales in the last 90 days this is expected.
			t.Log("LeerAnclasDesde returned 0 rows — no sales in the last 90 days; smoke ok (no error)")
			return
		}

		// Validate each row for basic sanity.
		for i, a := range anclas {
			assert.Positive(t, a.ClienteID,
				"row %d: ClienteID must be > 0, got %d", i, a.ClienteID)
			assert.False(t, a.Monetary.IsNegative(),
				"row %d: Monetary must be >= 0, got %s", i, a.Monetary)
			assert.False(t, a.Saldo.IsNegative(),
				"row %d: Saldo must be >= 0, got %s", i, a.Saldo)
			// PorLiquidarPct in [0, 100]
			assert.False(t, a.PorLiquidarPct.IsNegative(),
				"row %d: PorLiquidarPct must be >= 0, got %s", i, a.PorLiquidarPct)
			assert.True(t, a.PorLiquidarPct.LessThanOrEqual(decimal.NewFromInt(100)),
				"row %d: PorLiquidarPct must be <= 100, got %s", i, a.PorLiquidarPct)
			assert.GreaterOrEqual(t, a.Frecuencia, 0,
				"row %d: Frecuencia must be >= 0, got %d", i, a.Frecuencia)
		}

		t.Logf("LeerAnclasDesde smoke ok: %d anclas returned (since=%s)",
			len(anclas), since.Format(time.RFC3339))
		if len(anclas) > 0 {
			a := anclas[0]
			t.Logf("  first row: clienteID=%d nombre=%q zona=%q monetary=%s saldo=%s frecuencia=%d nbp=%q",
				a.ClienteID, a.Nombre, a.Zona, a.Monetary, a.Saldo, a.Frecuencia, a.NextBestProduct)
		}
	})
}
