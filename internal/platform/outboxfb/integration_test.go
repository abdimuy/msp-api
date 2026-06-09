//go:build !ci_skip_firebird

package outboxfb_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// requireFBEnv skips the calling test when FB_DATABASE is not set — matching
// the pattern used in ventfb integration tests.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// requireOutboxTable skips the calling test when MSP_OUTBOX_EVENTS is not
// present in the Firebird database. Migration 000029 must be applied before
// this package's integration tests can run against a real DB.
func requireOutboxTable(t *testing.T, pool *firebird.Pool) {
	t.Helper()
	ctx := context.Background()
	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		row := q.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM RDB$RELATIONS WHERE RDB$RELATION_NAME = 'MSP_OUTBOX_EVENTS'`,
		)
		var count int
		if scanErr := row.Scan(&count); scanErr != nil {
			return scanErr
		}
		if count == 0 {
			t.Skip("MSP_OUTBOX_EVENTS table not found; run migration 000029 first")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("requireOutboxTable: check table existence: %v", err)
	}
}

// outboxRow is the projection of a single MSP_OUTBOX_EVENTS row selected
// back in integration tests.
type outboxRow struct {
	aggregate   string
	aggregateID string
	eventType   string
	payload     []byte
	attempts    int
	processedAt sql.NullTime
	failedAt    sql.NullTime
	lastError   sql.NullString
}

// fetchOutboxRow queries MSP_OUTBOX_EVENTS by ID through the given context's
// ambient querier. CHAR(36) columns are right-padded by Firebird so the
// returned aggregateID is already trimmed.
func fetchOutboxRow(ctx context.Context, t *testing.T, pool *firebird.Pool, id uuid.UUID) (outboxRow, bool) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	row := q.QueryRowContext(
		ctx,
		`SELECT AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD, ATTEMPTS,
		        PROCESSED_AT, FAILED_AT, LAST_ERROR
		   FROM MSP_OUTBOX_EVENTS
		  WHERE ID = ?`,
		id.String(),
	)
	var r outboxRow
	err := row.Scan(
		&r.aggregate,
		&r.aggregateID,
		&r.eventType,
		&r.payload,
		&r.attempts,
		&r.processedAt,
		&r.failedAt,
		&r.lastError,
	)
	if err == sql.ErrNoRows {
		return outboxRow{}, false
	}
	require.NoError(t, err, "fetchOutboxRow")
	r.aggregateID = strings.TrimSpace(r.aggregateID)
	return r, true
}

func TestEnqueue_InsertsRow(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id := uuid.New()
		aggregateID := uuid.New()

		err := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   "venta",
			AggregateID: aggregateID,
			EventType:   "venta.creada",
			Payload:     json.RawMessage(`{"hello":"world","emoji":"🚀"}`),
		})
		require.NoError(t, err)

		r, found := fetchOutboxRow(ctx, t, pool, id)
		require.True(t, found, "row must be visible in the same tx")
		assert.Equal(t, "venta", r.aggregate)
		assert.Equal(t, aggregateID.String(), r.aggregateID)
		assert.Equal(t, "venta.creada", r.eventType)
		assert.JSONEq(t, `{"hello":"world","emoji":"🚀"}`, string(r.payload))
		assert.Equal(t, 0, r.attempts)
		assert.False(t, r.processedAt.Valid)
		assert.False(t, r.failedAt.Valid)
		assert.False(t, r.lastError.Valid)
	})
}

func TestEnqueue_GeneratesIDWhenNil(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		aggregateID := uuid.New()

		// Pass uuid.Nil — Enqueue must generate a fresh ID.
		err := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          uuid.Nil,
			Aggregate:   "cliente",
			AggregateID: aggregateID,
			EventType:   "cliente.creado",
			Payload:     json.RawMessage(`{"gen":"id"}`),
		})
		require.NoError(t, err)

		// We don't know the generated ID, but we can confirm that exactly one
		// row landed for this aggregateID and event type.
		q := firebird.GetQuerier(ctx, pool.DB)
		row := q.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM MSP_OUTBOX_EVENTS
			  WHERE AGGREGATE_ID = ? AND EVENT_TYPE = ?`,
			aggregateID.String(), "cliente.creado",
		)
		var count int
		require.NoError(t, row.Scan(&count))
		assert.Equal(t, 1, count, "exactly one row must exist for the generated ID")
	})
}

func TestEnqueue_GeneratesCreatedAtWhenZero(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	before := time.Now().Add(-2 * time.Second)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id := uuid.New()

		err := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   "pago",
			AggregateID: uuid.New(),
			EventType:   "pago.creado",
			Payload:     json.RawMessage(`{"zero":"time"}`),
			// CreatedAt is intentionally left zero.
		})
		require.NoError(t, err)

		// Read back the raw TIMESTAMP via ScanUTCTime.
		q := firebird.GetQuerier(ctx, pool.DB)
		row := q.QueryRowContext(ctx, `SELECT CREATED_AT FROM MSP_OUTBOX_EVENTS WHERE ID = ?`, id.String())
		var rawTS any
		require.NoError(t, row.Scan(&rawTS))
		ts, err := firebird.ScanUTCTime(rawTS)
		require.NoError(t, err)

		assert.True(t, ts.After(before), "CREATED_AT must be close to now, got %v", ts)
		assert.True(t, ts.Before(time.Now().Add(2*time.Second)), "CREATED_AT must not be in the future")
	})
}

func TestEnqueue_RejectsEmptyPayload(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		err := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          uuid.New(),
			Aggregate:   "venta",
			AggregateID: uuid.New(),
			EventType:   "venta.creada",
			Payload:     nil, // must be rejected
		})
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror, got: %T %v", err, err)
		assert.Equal(t, "outbox_empty_payload", appErr.Code)
	})
}

func TestEnqueue_NoTxInCtx_Integration(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	// context.Background() has no ambient tx.
	err := outboxfb.Enqueue(context.Background(), pool.DB, outboxfb.Event{
		ID:          uuid.New(),
		Aggregate:   "venta",
		AggregateID: uuid.New(),
		EventType:   "venta.creada",
		Payload:     json.RawMessage(`{"no":"tx"}`),
	})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror, got: %T %v", err, err)
	assert.Equal(t, "outbox_no_tx", appErr.Code)
}

func TestEnqueue_RespectsAmbientTx(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	id := uuid.New()

	// fbtestutil.WithTestTransaction always rolls back — so after the callback
	// completes, the row must not exist in the DB.
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		err := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   "venta",
			AggregateID: uuid.New(),
			EventType:   "venta.creada",
			Payload:     json.RawMessage(`{"rollback":"test"}`),
		})
		require.NoError(t, err)

		// Row is visible inside the tx.
		_, found := fetchOutboxRow(ctx, t, pool, id)
		assert.True(t, found, "row must be visible before rollback")
	})
	// Callback returned → tx was rolled back by WithTestTransaction.
	// Open a fresh read tx to confirm the row is absent.
	err := firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
		_, found := fetchOutboxRow(ctx, t, pool, id)
		assert.False(t, found, "row must be absent after rollback")
		return nil
	})
	require.NoError(t, err)
}

func TestEnqueue_LargePayload_RoundTrips(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	// Build a payload > 2 KB with special chars, embedded JSON, unicode.
	inner := map[string]any{
		"nested": map[string]any{
			"unicode": "Ñoño 🎸",
			"escaped": "line\\nbreak\ttab",
			"multi":   strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789", 60),
		},
		"array": []int{1, 2, 3, 4, 5},
	}
	payload, err := json.Marshal(inner)
	require.NoError(t, err)
	require.Greater(t, len(payload), 2048, "payload must exceed 2 KB for this test to be meaningful")

	id := uuid.New()
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		enqErr := outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   "traspaso",
			AggregateID: uuid.New(),
			EventType:   "traspaso.grande",
			Payload:     json.RawMessage(payload),
		})
		require.NoError(t, enqErr)

		r, found := fetchOutboxRow(ctx, t, pool, id)
		require.True(t, found)

		// JSON equality tolerates key ordering differences.
		assert.JSONEq(t, string(payload), string(r.payload))
	})
}
