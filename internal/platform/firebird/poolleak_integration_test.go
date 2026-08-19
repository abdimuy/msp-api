package firebird_test

// Regression test for the 28h outage: a request context expiring in the middle
// of a fetch used to make the firebirdsql driver write op_cancel to the socket
// without reading the reply, desynchronising the wire protocol. The follow-up
// round-trip (Rows.Close → closeCursor) then blocked forever on a read with no
// deadline, and the connection never returned to the pool. Ten of those and the
// whole API was dead.
//
// Verified against the local Firebird by reverting the fix: with the
// cancellation still reaching the driver, one of the goroutines below never
// returns (pool.Stats() keeps InUse=1 with every caller gone) and the storm
// times out. With the fix the same storm drains in well under a second.
//
// Needs a live Firebird (FB_DATABASE). Every statement is read-only against
// RDB$TYPES / RDB$DATABASE, system tables present in any Firebird database.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// slowReadOnlyQuery streams a large cartesian product over a small system
// table: cheap to start, slow to drain, and it writes nothing.
const slowReadOnlyQuery = `SELECT a.RDB$TYPE FROM RDB$TYPES a, RDB$TYPES b, RDB$TYPES c`

const (
	// cancelledQueryAttempts is far above the pool size, so every leaked
	// connection is one the pool never gets back.
	cancelledQueryAttempts = 40
	// stormBudget is two orders of magnitude above the ~0.5s the fixed driver
	// needs, so a failure here means "wedged", never "slow machine".
	stormBudget = 45 * time.Second
)

func TestPool_CancelledQueriesDoNotLeakConnections(t *testing.T) {
	t.Parallel()

	cfg := fbtestutil.TestFirebirdConfig(t) // skips when FB_DATABASE is unset
	// A pool of its own, deliberately tiny: exhaustion (the bug) shows up
	// immediately and can never starve the shared test pool.
	cfg.PoolSize = 4
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pool.Stop(context.Background())
	})
	require.NoError(t, pool.Start(context.Background()))

	// Read-only test, but wrapped anyway so the "never persist in the shared
	// DB" contract is enforced structurally rather than by reviewer memory.
	fbtestutil.WithTestTransaction(t, pool, func(_ context.Context) {
		require.True(t, drainStorm(t, pool),
			"quedaron consultas colgadas: el pool perdió conexiones (%+v)", pool.Stats())

		// The whole point: after the storm the pool still serves.
		for i := range 8 {
			healthCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			var one int
			err := pool.QueryRowContext(healthCtx, "SELECT 1 FROM RDB$DATABASE").Scan(&one)
			cancel()
			require.NoErrorf(t, err, "el pool quedó inutilizable (sonda %d)", i+1)
			require.Equal(t, 1, one)
		}

		// One connection is held by WithTestTransaction; nothing else may be
		// checked out once every query above has returned.
		stats := pool.Stats()
		assert.LessOrEqual(t, stats.InUse, 1,
			"conexiones ocupadas tras drenar todas las consultas: %+v", stats)
	})
}

// drainStorm fires cancelledQueryAttempts concurrent slow queries whose budgets
// expire at staggered points of the wire exchange, then reports whether every
// one of them returned inside stormBudget.
func drainStorm(t *testing.T, pool *firebird.Pool) bool {
	t.Helper()
	var wg sync.WaitGroup
	for i := range cancelledQueryAttempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Deterministic 1ms…391ms spread: the deadline lands at a different
			// point of the exchange on every goroutine (connect, execute,
			// fetch), which is what makes the race reproducible.
			runCancelledQuery(pool, time.Duration(i*10+1)*time.Millisecond)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(stormBudget):
		return false
	}
}

// runCancelledQuery starts the slow query with a budget that expires while the
// driver is still talking to the server, then walks away exactly like
// database/sql does for an HTTP handler whose middleware timeout fired.
func runCancelledQuery(pool *firebird.Pool, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	rows, err := pool.QueryContext(ctx, slowReadOnlyQuery)
	if err != nil {
		// Losing the race (deadline hit before the cursor opened) is fine:
		// what matters is that the connection is not wedged.
		return
	}
	defer func() {
		_ = rows.Close()
	}()

	var v int
	for rows.Next() {
		if err := rows.Scan(&v); err != nil {
			return
		}
	}
	_ = rows.Err()
}
