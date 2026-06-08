package fbtestutil_test

// Regression test for the rollback-only contract.
//
// fbtestutil.WithTestTransaction is the ONLY safe path to write against the
// shared dev Microsip database. Its single guarantee — "nothing fn writes
// survives after the helper returns" — is what lets us run integration tests
// against production-shaped data without ever polluting it.
//
// If a future refactor accidentally turns the deferred Rollback into a
// Commit, swallows the error, or leaks the transaction across the boundary,
// this test fails LOUDLY by leaving a sentinel row behind that a second,
// fresh connection on the same pool can see.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestWithTestTransaction_RollsBackEverythingFn_Writes is the regression test
// for the "transactional sandbox" contract. It writes a sentinel row inside
// WithTestTransaction, confirms the row IS visible to the same tx, and then —
// after the helper returns — proves the row is NOT visible to a different
// connection on the same pool. If the helper ever stops rolling back, that
// second query returns 1 and the test fails.
func TestWithTestTransaction_RollsBackEverythingFn_Writes(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t) // skips if FB_DATABASE unset

	id := uuid.NewString()
	now := time.Now().UTC()

	var visibleInsideTx int

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'rollback-regression', TRUE, 'FIREBASE_USER',
			         ?, ?, ?, ?)`,
			id, "fbtest-"+id, "fbtest-"+id+"@example.invalid",
			now, now, id, id,
		)
		require.NoError(t, err, "insert inside the test tx must succeed")

		err = q.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM MSP_USUARIOS WHERE ID = ?", id,
		).Scan(&visibleInsideTx)
		require.NoError(t, err)
	})

	// Sanity: confirm the test actually wrote something. If the INSERT
	// silently no-op'd the regression test wouldn't be proving anything.
	require.Equal(t, 1, visibleInsideTx,
		"the sentinel row must have been visible to the tx that inserted it")

	// The critical assertion — fresh connection on the same pool MUST NOT see
	// the row. If this fails, the helper is no longer rolling back and every
	// previous integration test has been silently polluting the dev DB.
	var visibleOutside int
	err := pool.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM MSP_USUARIOS WHERE ID = ?", id,
	).Scan(&visibleOutside)
	require.NoError(t, err)
	assert.Equal(t, 0, visibleOutside,
		"after WithTestTransaction returns, no inserted row may survive — "+
			"if you see this fail, the rollback contract is broken and "+
			"every integration test has been polluting the dev database")
}

// TestWithTestTransaction_DoesNotPoisonPool ensures that after a rollback the
// shared pool is still usable. If Rollback left the connection in a bad state
// the next pool query would error out — this asserts that doesn't happen, so
// downstream tests in the same process aren't undermined by an earlier one.
func TestWithTestTransaction_DoesNotPoisonPool(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		_, err := q.ExecContext(ctx,
			"UPDATE MSP_MIGRATIONS SET APPLIED_AT = APPLIED_AT WHERE ID = 1",
		)
		require.NoError(t, err, "touch a row inside the tx to dirty the conn")
	})

	// After rollback, the pool must answer a plain query without error.
	// MSP_MIGRATIONS has at least one row (the auth-tables migration); we
	// just verify the round-trip works, not the value.
	var n int
	err := pool.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM MSP_MIGRATIONS",
	).Scan(&n)
	require.NoError(t, err, "pool must be usable after a rolled-back tx")
	assert.GreaterOrEqual(t, n, 1)
}
