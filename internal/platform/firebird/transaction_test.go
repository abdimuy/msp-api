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
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'fbtest-sentinel', TRUE, 'FIREBASE_USER',
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

// ─── Free-function RunInReadTx / RunInSnapshotTx tests ─────────────────────────
//
// These verify the SELECT-only commit/rollback lifecycle introduced to plug
// the nakagami/firebirdsql implicit-tx leak. Tests issue no DML, so a real
// COMMIT is safe against the shared Microsip DB.
//
// Each test captures CURRENT_TRANSACTION inside fn, then queries
// MON$TRANSACTIONS on a pool connection to verify the tx terminated in the
// expected state (3 = committed, 4 = rolled back). The driver assigns a new
// TX_ID per BeginTx so the inside/outside check is unambiguous.

const (
	monStateIdle       = 0 // Firebird may briefly report idle for tx that just terminated.
	monStateActive     = 1 // The state we MUST NOT see post-RunIn* — that is the leak.
	monStateCommitted  = 3
	monStateRolledBack = 4
)

// assertTxTerminated asserts that the given TX_ID is no longer active.
// Acceptable outcomes after RunInTx returns: row purged (sql.ErrNoRows),
// state=3/4 (committed/rolled back), or briefly state=0 (idle/transitioning).
// The only failure is state=1 (active) — that is the leak we are guarding
// against. wantTerminal narrows the assertion: pass monStateCommitted to
// require the row, when present, to be in committed state (state=3 or
// purged); pass monStateRolledBack for the rollback path. State=0 is
// tolerated in either case because Firebird may report it transiently
// before terminal-state finalization.
func assertTxTerminated(t *testing.T, pool *firebird.Pool, txID int64, wantTerminal int) {
	t.Helper()
	state, qerr := monStateOf(context.Background(), pool, txID)
	if errors.Is(qerr, sql.ErrNoRows) {
		return // row purged → tx definitely terminated
	}
	require.NoError(t, qerr)
	assert.NotEqual(t, monStateActive, state,
		"tx %d still in active state — leak", txID)
	switch state {
	case monStateIdle, wantTerminal:
		// ok
	default:
		t.Errorf("tx %d ended in state=%d; expected idle(%d) or terminal(%d)",
			txID, state, monStateIdle, wantTerminal)
	}
}

// readCurrentTxID returns CAST(CURRENT_TRANSACTION AS BIGINT) using the
// Querier carried in ctx. Safe to call inside any RunIn* fn.
func readCurrentTxID(ctx context.Context, q firebird.Querier) (int64, error) {
	var tx int64
	err := q.QueryRowContext(ctx,
		`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
	).Scan(&tx)
	return tx, err
}

// monStateOf returns the MON$STATE of a specific TX_ID using a fresh
// auto-commit query on pool.DB. Returns sql.ErrNoRows if the tx was already
// garbage-collected from MON$TRANSACTIONS (some Firebird versions purge
// terminal states aggressively); callers should treat that as success since
// the absence of state=1 already implies termination.
func monStateOf(ctx context.Context, pool *firebird.Pool, txID int64) (int, error) {
	var state int
	err := pool.DB.QueryRowContext(ctx,
		`SELECT MON$STATE FROM MON$TRANSACTIONS WHERE MON$TRANSACTION_ID = ?`,
		txID,
	).Scan(&state)
	return state, err
}

// TestRunInReadTx_CommitsOnSuccess: fn returning nil → tx commits cleanly.
// The TX_ID we captured inside fn must transition to state=3 (committed) or
// disappear from MON$TRANSACTIONS entirely (terminal-state GC).
func TestRunInReadTx_CommitsOnSuccess(t *testing.T) {
	t.Parallel()
	pool, _ := requireFirebird(t)

	var innerTxID int64
	err := firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
		assert.True(t, firebird.HasTx(ctx), "ctx inside RunInReadTx must carry a tx")
		id, e := readCurrentTxID(ctx, firebird.GetQuerier(ctx, pool.DB))
		if e != nil {
			return e
		}
		innerTxID = id
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, innerTxID, "must have captured an active TX_ID inside fn")
	assertTxTerminated(t, pool, innerTxID, monStateCommitted)
}

// TestRunInReadTx_RollsBackOnError: fn returning a non-nil error → tx is
// rolled back. The TX_ID must transition to state=4 (rolled back) or
// disappear.
func TestRunInReadTx_RollsBackOnError(t *testing.T) {
	t.Parallel()
	pool, _ := requireFirebird(t)

	want := errors.New("force rollback in read tx")
	var innerTxID int64
	err := firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
		id, e := readCurrentTxID(ctx, firebird.GetQuerier(ctx, pool.DB))
		if e != nil {
			return e
		}
		innerTxID = id
		return want
	})
	require.ErrorIs(t, err, want)
	require.Positive(t, innerTxID)
	assertTxTerminated(t, pool, innerTxID, monStateRolledBack)
}

// TestRunInReadTx_Nested_ReusesOuterTx: outer write tx + inner RunInReadTx
// must share the same *sql.Tx — no new BEGIN issued. We verify by reading
// CURRENT_TRANSACTION at both levels: same int means same tx.
func TestRunInReadTx_Nested_ReusesOuterTx(t *testing.T) {
	t.Parallel()
	pool, txm := requireFirebird(t)

	want := errors.New("rollback outer for safety")
	var outerTxID, innerTxID int64
	err := txm.RunInTx(context.Background(), func(outerCtx context.Context) error {
		id, e := readCurrentTxID(outerCtx, firebird.GetQuerier(outerCtx, pool.DB))
		if e != nil {
			return e
		}
		outerTxID = id

		// Nested RunInReadTx must passthrough — no new tx.
		nestErr := firebird.RunInReadTx(outerCtx, pool.DB, func(innerCtx context.Context) error {
			assert.Equal(t, outerCtx, innerCtx,
				"inner ctx must equal outer ctx (passthrough, no new tx value injected)")
			id2, e2 := readCurrentTxID(innerCtx, firebird.GetQuerier(innerCtx, pool.DB))
			if e2 != nil {
				return e2
			}
			innerTxID = id2
			return nil
		})
		if nestErr != nil {
			return nestErr
		}
		return want
	})
	require.ErrorIs(t, err, want)
	require.Positive(t, outerTxID)
	require.Positive(t, innerTxID)
	assert.Equal(t, outerTxID, innerTxID,
		"nested RunInReadTx must reuse the outer tx — got distinct TX_IDs (outer=%d inner=%d)",
		outerTxID, innerTxID)
}

// TestRunInReadTx_ContextCancellation: cancelling ctx mid-fn surfaces as a
// context.Canceled error from the RunInReadTx return value.
func TestRunInReadTx_ContextCancellation(t *testing.T) {
	t.Parallel()
	pool, _ := requireFirebird(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before BeginTx even runs

	err := firebird.RunInReadTx(ctx, pool.DB, func(_ context.Context) error {
		return nil
	})
	require.Error(t, err, "RunInReadTx with cancelled ctx must surface an error")
	assert.ErrorIs(t, err, context.Canceled,
		"err must wrap context.Canceled; got %v", err)
}

// TestRunInSnapshotTx_UsesRepeatableRead: verify that the tx opened by
// RunInSnapshotTx requests REPEATABLE READ (Firebird snapshot / isc_tpb_concurrency).
//
// MON$TRANSACTIONS.MON$ISOLATION_MODE codes per Firebird docs:
//
//	0 = consistency (table reservation)
//	1 = concurrency (snapshot)
//	2 = read committed record_version
//	3 = read committed no_record_version
//
// The Go driver maps sql.LevelRepeatableRead → isc_tpb_concurrency = 1.
func TestRunInSnapshotTx_UsesRepeatableRead(t *testing.T) {
	t.Parallel()
	pool, _ := requireFirebird(t)

	var snapshotTxID int64
	var isolationMode int
	var gotIsolation bool
	err := firebird.RunInSnapshotTx(context.Background(), pool.DB, func(ctx context.Context) error {
		id, e := readCurrentTxID(ctx, firebird.GetQuerier(ctx, pool.DB))
		if e != nil {
			return e
		}
		snapshotTxID = id

		// Query MON$ISOLATION_MODE for our own tx via a separate pool
		// connection (we cannot self-query inside the same tx in some
		// Firebird builds because MON$ is materialised per-statement on
		// a fresh snapshot of monitoring state).
		row := pool.QueryRowContext(ctx,
			`SELECT MON$ISOLATION_MODE FROM MON$TRANSACTIONS WHERE MON$TRANSACTION_ID = ?`, id)
		if scanErr := row.Scan(&isolationMode); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return nil // tolerable; assertion is skipped below
			}
			return scanErr
		}
		gotIsolation = true
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, snapshotTxID)
	if !gotIsolation {
		t.Skipf("MON$TRANSACTIONS row for tx %d not found from observer conn — driver/build limitation", snapshotTxID)
	}
	assert.Equal(t, 1, isolationMode,
		"snapshot tx must use isc_tpb_concurrency (MON$ISOLATION_MODE=1); got %d", isolationMode)
}

// TestRunInTx_FreeFunction_EquivalentToTxManager: parametric test that
// verifies the free-function RunInTx and TxManager.RunInTx produce the same
// committed-on-success / rolled-back-on-error behavior. Uses SELECT-only fn
// to keep DB state untouched.
func TestRunInTx_FreeFunction_EquivalentToTxManager(t *testing.T) {
	t.Parallel()
	pool, txm := requireFirebird(t)

	type runner func(ctx context.Context, fn func(context.Context) error) error
	cases := []struct {
		name string
		run  runner
	}{
		{
			name: "TxManager.RunInTx",
			run: func(ctx context.Context, fn func(context.Context) error) error {
				return txm.RunInTx(ctx, fn)
			},
		},
		{
			name: "free-function RunInTx",
			run: func(ctx context.Context, fn func(context.Context) error) error {
				return firebird.RunInTx(ctx, pool.DB, fn)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/commits-on-nil", func(t *testing.T) {
			t.Parallel()
			var innerTxID int64
			err := tc.run(context.Background(), func(ctx context.Context) error {
				assert.True(t, firebird.HasTx(ctx))
				id, e := readCurrentTxID(ctx, firebird.GetQuerier(ctx, pool.DB))
				if e != nil {
					return e
				}
				innerTxID = id
				return nil
			})
			require.NoError(t, err)
			require.Positive(t, innerTxID)
			assertTxTerminated(t, pool, innerTxID, monStateCommitted)
		})
		t.Run(tc.name+"/rolls-back-on-error", func(t *testing.T) {
			t.Parallel()
			want := errors.New("force rollback")
			var innerTxID int64
			err := tc.run(context.Background(), func(ctx context.Context) error {
				id, e := readCurrentTxID(ctx, firebird.GetQuerier(ctx, pool.DB))
				if e != nil {
					return e
				}
				innerTxID = id
				return want
			})
			require.ErrorIs(t, err, want)
			require.Positive(t, innerTxID)
			assertTxTerminated(t, pool, innerTxID, monStateRolledBack)
		})
	}
}
