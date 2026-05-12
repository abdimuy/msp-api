package firebird_test

// Integration tests for TxManager against the REAL Microsip Firebird DB.
//
// Contract:
//   - All write paths run inside a RunInTx that returns an error → guaranteed
//     rollback. The dev database state is never mutated by these tests.
//   - No DDL (CREATE/DROP/ALTER TABLE). Tests reuse MSP_USUARIOS, which is
//     installed by migrations-firebird/000001_create_auth_tables.up.sql.
//   - Gate: FB_DATABASE must be set. fbtestutil.NewTestFirebirdPool skips
//     otherwise.
//
// The "commit on success" semantic of RunInTx is *not* tested here — we have
// no way to commit safely against the shared Microsip DB. It is exercised
// implicitly by every successful production INSERT and by the unit tests of
// the underlying *sql.Tx in stdlib.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireFirebird returns the shared pool and a TxManager. Skips the test
// when FB_DATABASE isn't configured.
func requireFirebird(t *testing.T) (*firebird.Pool, *firebird.TxManager) {
	t.Helper()
	pool := fbtestutil.NewTestFirebirdPool(t)
	return pool, firebird.NewTxManager(pool.DB)
}

// insertSentinelUser inserts a self-referencing row into MSP_USUARIOS using
// fresh UUIDs. Returns the inserted ID so callers can query it back inside
// the same transaction.
//
// The row is a tombstone: it satisfies all NOT NULL + UNIQUE + FK constraints
// (CREATED_BY/UPDATED_BY → self) while carrying obviously synthetic values.
// Because every call uses uuid.New() the FIREBASE_UID and EMAIL UNIQUE
// constraints never collide, even across parallel runs.
func insertSentinelUser(ctx context.Context, q firebird.Querier) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'fbtest-sentinel', TRUE,
		         ?, ?, ?, ?)`,
		id, "fbtest-"+id, "fbtest-"+id+"@example.invalid",
		now, now, id, id,
	)
	if err != nil {
		return "", fmt.Errorf("insert sentinel: %w", err)
	}
	return id, nil
}

// countByID returns 1 if a row with the given ID exists in MSP_USUARIOS, 0
// otherwise. The query runs through the given Querier so callers can ask
// the question both inside and outside the transaction.
func countByID(ctx context.Context, q firebird.Querier, id string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MSP_USUARIOS WHERE ID = ?", id,
	).Scan(&n)
	return n, err
}

// TestTxManager_RollsBackOnError verifies that a fn returning an error causes
// the transaction to roll back: the row inserted inside the tx is invisible
// to the pool after RunInTx returns.
func TestTxManager_RollsBackOnError(t *testing.T) {
	t.Parallel()
	pool, txm := requireFirebird(t)

	ctx := context.Background()
	want := errors.New("test failure forces rollback")

	var insertedID string
	err := txm.RunInTx(ctx, func(txCtx context.Context) error {
		id, e := insertSentinelUser(txCtx, firebird.GetQuerier(txCtx, pool.DB))
		if e != nil {
			return e
		}
		insertedID = id

		// Visible inside the tx.
		n, e := countByID(txCtx, firebird.GetQuerier(txCtx, pool.DB), id)
		if e != nil {
			return e
		}
		require.Equal(t, 1, n, "row must be visible to the tx that inserted it")

		return want
	})
	require.ErrorIs(t, err, want)
	require.NotEmpty(t, insertedID, "sentinel must have been inserted before rollback")

	// After rollback the row is gone — query through the pool directly.
	n, qerr := countByID(ctx, pool.DB, insertedID)
	require.NoError(t, qerr)
	assert.Equal(t, 0, n, "rolled-back row must not be visible on the pool")
}

// TestTxManager_Nested_ReusesOuterTx verifies that a nested RunInTx inside an
// active transaction does NOT start a new transaction — the inner closure
// runs against the outer ctx and shares its rollback fate.
func TestTxManager_Nested_ReusesOuterTx(t *testing.T) {
	t.Parallel()
	pool, txm := requireFirebird(t)

	ctx := context.Background()
	want := errors.New("outer rolls back, taking inner with it")

	var outerID, innerID string
	err := txm.RunInTx(ctx, func(outerCtx context.Context) error {
		require.True(t, firebird.HasTx(outerCtx), "outer ctx must carry a tx")

		id, e := insertSentinelUser(outerCtx, firebird.GetQuerier(outerCtx, pool.DB))
		if e != nil {
			return e
		}
		outerID = id

		// Nested call. Inner closure must see the SAME context (passthrough),
		// must carry the same tx, and its write must share rollback with outer.
		return txm.RunInTx(outerCtx, func(innerCtx context.Context) error {
			assert.Equal(t, outerCtx, innerCtx, "inner ctx must equal outer ctx (passthrough)")
			require.True(t, firebird.HasTx(innerCtx), "inner ctx must carry a tx")

			id2, e := insertSentinelUser(innerCtx, firebird.GetQuerier(innerCtx, pool.DB))
			if e != nil {
				return e
			}
			innerID = id2
			return want // outer's fn passes this through → rollback
		})
	})
	require.ErrorIs(t, err, want)
	require.NotEmpty(t, outerID)
	require.NotEmpty(t, innerID)

	// Both rows must be gone. The inner inserts even though outer fails —
	// nested RunInTx doesn't start its own tx so there's no inner commit to
	// undo, but the outer rollback covers both.
	for _, id := range []string{outerID, innerID} {
		n, qerr := countByID(ctx, pool.DB, id)
		require.NoError(t, qerr)
		assert.Equal(t, 0, n, "id %s must be rolled back", id)
	}
}

// TestTxManager_RequireTxReturnsErrNoTx_WhenNoTx is a pure context test — no
// DB needed. Kept here so it lives alongside the other TxManager tests.
func TestTxManager_RequireTxReturnsErrNoTx_WhenNoTx(t *testing.T) {
	t.Parallel()
	tx, err := firebird.RequireTx(context.Background())
	require.ErrorIs(t, err, firebird.ErrNoTx)
	assert.Nil(t, tx)
}

// TestTxManager_HasTx_OutsideTx is a pure context test.
func TestTxManager_HasTx_OutsideTx(t *testing.T) {
	t.Parallel()
	assert.False(t, firebird.HasTx(context.Background()))
}

// TestTxManager_HasTx_InsideRunInTx asserts that HasTx flips to true inside
// the fn passed to RunInTx. Uses a no-op fn so nothing touches the DB after
// BEGIN — rollback is automatic when fn returns an error.
func TestTxManager_HasTx_InsideRunInTx(t *testing.T) {
	t.Parallel()
	_, txm := requireFirebird(t)

	want := errors.New("force rollback")
	var sawTx bool
	err := txm.RunInTx(context.Background(), func(txCtx context.Context) error {
		sawTx = firebird.HasTx(txCtx)
		return want
	})
	require.ErrorIs(t, err, want)
	assert.True(t, sawTx, "context inside RunInTx must carry a tx")
}

// TestTxManager_GetQuerier_Plumbing verifies the GetQuerier helper picks the
// right executor: fallback outside a tx, *sql.Tx inside one. The "inside"
// path uses a no-op fn that returns an error, so the tx rolls back without
// touching any tables.
func TestTxManager_GetQuerier_Plumbing(t *testing.T) {
	t.Parallel()
	pool, txm := requireFirebird(t)

	fallback := pool.DB
	assert.Equal(t, firebird.Querier(fallback),
		firebird.GetQuerier(context.Background(), fallback),
		"outside RunInTx GetQuerier must return the fallback")

	want := errors.New("force rollback")
	err := txm.RunInTx(context.Background(), func(txCtx context.Context) error {
		q := firebird.GetQuerier(txCtx, fallback)
		_, isTx := q.(*sql.Tx)
		assert.True(t, isTx, "inside RunInTx GetQuerier must return *sql.Tx")
		return want
	})
	require.ErrorIs(t, err, want)
}
