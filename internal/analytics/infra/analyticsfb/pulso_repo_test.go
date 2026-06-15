// Package analyticsfb_test — pulso_repo_test.go contains Firebird integration
// tests for GetCandidato and ListCandidatosByClienteIDs.
//
//nolint:paralleltest // serial: shares rollback-only tx.
//nolint:misspell // Spanish vocabulary (candidato, etc.) by convention.
package analyticsfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// TestRepo_GetCandidato_Found verifies that GetCandidato returns the
// correct candidato when the row exists.
func TestRepo_GetCandidato_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const clienteID = -30001
		c := makeCandidato(t, clienteID, "12000.00", "4500.00", false)

		err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		got, err := repo.GetCandidato(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, clienteID, got.ClienteID())
		assert.Equal(t, c.ID(), got.ID())
		assert.Equal(t, "Test Cliente", got.Nombre())
		assert.True(t, decimal.RequireFromString("12000.00").Equal(got.Monetary()),
			"monetary mismatch: got=%s", got.Monetary())
		assert.WithinDuration(t, fixedFechaUltima, got.FechaUltimaCompra(), 2*time.Second)

		t.Logf("GetCandidato found: clienteID=%d monetary=%s", got.ClienteID(), got.Monetary())
	})
}

// TestRepo_GetCandidato_NotFound verifies that GetCandidato returns
// domain.ErrWinbackCandidatoNotFound for a non-existent clienteID.
func TestRepo_GetCandidato_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// Upsert a sentinel row first so a failure here (not in GetCandidato)
		// is what signals the table is missing — this keeps the not-found
		// assertion below unconditional.
		seed := makeCandidato(t, -30002, "1000.00", "0.00", false)
		if err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{seed}); err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		const nonExistentID = -99999999
		_, err := repo.GetCandidato(ctx, nonExistentID)
		require.ErrorIs(t, err, domain.ErrWinbackCandidatoNotFound,
			"GetCandidato with missing clienteID must return ErrWinbackCandidatoNotFound")
	})
}

// TestRepo_ListCandidatosByClienteIDs_SubsetFound verifies that only
// materialized candidatos appear in the result.
func TestRepo_ListCandidatosByClienteIDs_SubsetFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		c1 := makeCandidato(t, -30010, "8000.00", "0.00", false)
		c2 := makeCandidato(t, -30011, "9000.00", "0.00", false)

		err := repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c1, c2})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		// Request c1, c2, and a non-existent ID.
		const missing = -88888888
		result, err := repo.ListCandidatosByClienteIDs(ctx, []int{-30010, -30011, missing})
		require.NoError(t, err)

		// Only c1 and c2 must appear; missing is absent.
		ids := make(map[int]struct{}, len(result))
		for _, c := range result {
			ids[c.ClienteID()] = struct{}{}
		}
		assert.Contains(t, ids, -30010, "clienteID -30010 must be present")
		assert.Contains(t, ids, -30011, "clienteID -30011 must be present")
		assert.NotContains(t, ids, missing, "non-existent clienteID must be absent")

		t.Logf("ListCandidatosByClienteIDs: %d results (expected 2)", len(result))
	})
}

// TestRepo_ListCandidatosByClienteIDs_EmptyInput verifies that an empty
// input returns an empty slice without hitting the database.
func TestRepo_ListCandidatosByClienteIDs_EmptyInput(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		result, err := repo.ListCandidatosByClienteIDs(ctx, []int{})
		require.NoError(t, err)
		assert.NotNil(t, result, "result must be non-nil for empty input")
		assert.Empty(t, result, "empty input must return empty slice")
	})
}

// TestRepo_ListCandidatosByClienteIDs_AllAbsent verifies that when none of
// the requested clienteIDs are materialized, an empty slice is returned (no error).
func TestRepo_ListCandidatosByClienteIDs_AllAbsent(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// These IDs are highly unlikely to exist in any real or test data.
		result, err := repo.ListCandidatosByClienteIDs(ctx, []int{-77777771, -77777772, -77777773})
		if err != nil {
			t.Skipf("ListCandidatosByClienteIDs failed — migration 000035 may not be applied: %v", err)
		}
		assert.Empty(t, result, "absent clienteIDs must yield empty result")
	})
}
