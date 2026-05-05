package outbox_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/outbox"
	"github.com/abdimuy/msp-api/internal/platform/testutil"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

var testPool *pgxpool.Pool

// TestMain initializes the integration pool only when INTEGRATION=1 is set,
// so unit tests in this same package keep running in normal `go test` mode.
// Integration tests skip themselves via requirePool when testPool is nil.
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION") != "" || os.Getenv("TEST_DATABASE_URL") != "" {
		testPool = testutil.NewTestDatabasePool()
	}
	os.Exit(m.Run())
}

func requirePool(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("skipping integration test: set INTEGRATION=1 to run")
	}
}

// TestEnqueue_Integration validates the full path:
// transaction.Manager.RunInTx → outbox.Enqueue → row appears in outbox_events.
func TestEnqueue_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		aggID := uuid.New()
		payload := map[string]any{"foo": "bar", "n": 42}

		require.NoError(t, outbox.Enqueue(ctx, "cliente", aggID, "push_to_microsip", payload))

		// Read the row back through the same tx.
		q := transaction.GetQuerier(ctx, testPool)
		var (
			gotAgg     string
			gotAggID   uuid.UUID
			gotType    string
			rawPayload []byte
			attempts   int
		)
		err := q.QueryRow(
			ctx,
			`SELECT aggregate, aggregate_id, event_type, payload, attempts
             FROM outbox_events WHERE aggregate_id = $1`,
			aggID,
		).Scan(&gotAgg, &gotAggID, &gotType, &rawPayload, &attempts)
		require.NoError(t, err)

		assert.Equal(t, "cliente", gotAgg)
		assert.Equal(t, aggID, gotAggID)
		assert.Equal(t, "push_to_microsip", gotType)
		assert.Equal(t, 0, attempts, "new events start with 0 attempts")

		var got map[string]any
		require.NoError(t, json.Unmarshal(rawPayload, &got))
		assert.Equal(t, "bar", got["foo"])
	})
}

// TestEnqueue_RequiresActiveTx confirms that calling Enqueue without an
// active tx in context fails fast — guarding against silent dual-write bugs.
func TestEnqueue_RequiresActiveTx_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	// No WithTestTransaction wrapper: ctx has no tx.
	err := outbox.Enqueue(context.Background(), "cliente", uuid.New(), "push", map[string]any{})
	require.Error(t, err)
}

// TestEnqueue_TxRollback proves the rollback boundary: a row written in a
// rolled-back tx must NOT be visible afterward in a separate connection.
func TestEnqueue_TxRollback_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	aggID := uuid.New()

	// Write in a tx that will roll back.
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		require.NoError(
			t,
			outbox.Enqueue(ctx, "cliente", aggID, "push_to_microsip", map[string]any{"x": 1}),
		)
	})

	// The previous tx rolled back — outside, the row must be gone.
	count := countOutboxByAggregateID(t, testPool, aggID)
	assert.Equal(t, 0, count, "rolled-back row must not be visible outside the tx")
}

// countOutboxByAggregateID reads with a fresh background context — the point
// is to verify visibility OUTSIDE the test's tx, not inside it.
func countOutboxByAggregateID(t *testing.T, pool *pgxpool.Pool, aggID uuid.UUID) int {
	t.Helper()
	var count int
	require.NoError(t,
		pool.QueryRow(context.Background(),
			"SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1", aggID,
		).Scan(&count),
	)
	return count
}

// TestRunInTxCommit_Integration verifies WithTestCommit lets a Manager.RunInTx
// commit be observable across connections — needed for outbox dispatcher tests.
func TestRunInTxCommit_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	testutil.WithTestCommit(t, testPool, func(ctx context.Context) {
		aggID := uuid.New()
		mgr := transaction.NewManager(testPool)

		require.NoError(t, mgr.RunInTx(ctx, func(ctx context.Context) error {
			return outbox.Enqueue(ctx, "venta_local", aggID, "push_to_microsip", map[string]any{})
		}))

		// Cleanup so the row doesn't leak to other tests in this package.
		// Cleanup runs after the test ctx is gone — context.Background is correct.
		t.Cleanup(func() {
			_, _ = testPool.Exec(
				context.Background(),
				"DELETE FROM outbox_events WHERE aggregate_id = $1", aggID,
			)
		})

		// Visible from a different connection (the pool, not the inner tx).
		// Use a fresh background ctx — we want to read OUTSIDE the test tx.
		count := countOutboxByAggregateID(t, testPool, aggID)
		assert.Equal(t, 1, count, "committed row must be visible from a fresh query")
	})
}
