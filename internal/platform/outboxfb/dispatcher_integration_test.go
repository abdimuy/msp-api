//go:build !ci_skip_firebird

package outboxfb_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// uniqueType returns an event-type string that is unique to this test run by
// embedding a fresh UUID. This ensures parallel integration tests can't pick
// up each other's outbox rows, since every dispatcher scans all pending rows.
func uniqueType(base string) string {
	return fmt.Sprintf("%s.%s", base, uuid.New().String()[:8])
}

// insertPendingEvent inserts a single pending row into MSP_OUTBOX_EVENTS via a
// committed transaction, and registers a t.Cleanup that deletes the row
// unconditionally (regardless of whether the dispatcher moved it to a terminal
// state). This keeps the test table clean even when assertions fail.
func insertPendingEvent(t *testing.T, pool *firebird.Pool, aggregate, eventType, payload string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	require.NoError(t, firebird.RunInTx(ctx, pool.DB, func(ctx context.Context) error {
		return outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   aggregate,
			AggregateID: uuid.New(),
			EventType:   eventType,
			Payload:     json.RawMessage(payload),
		})
	}), "insertPendingEvent: enqueue")
	t.Cleanup(func() {
		_ = firebird.RunInTx(context.Background(), pool.DB, func(ctx context.Context) error {
			q := firebird.GetQuerier(ctx, pool.DB)
			_, _ = q.ExecContext(ctx, `DELETE FROM MSP_OUTBOX_EVENTS WHERE ID = ?`, id.String())
			return nil
		})
	})
	return id
}

// insertPendingEventAt inserts a pending row with an explicit CREATED_AT
// so ordering tests can deterministically control which row is "older".
func insertPendingEventAt(t *testing.T, pool *firebird.Pool, aggregate, eventType, payload string, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	require.NoError(t, firebird.RunInTx(ctx, pool.DB, func(ctx context.Context) error {
		return outboxfb.Enqueue(ctx, pool.DB, outboxfb.Event{
			ID:          id,
			Aggregate:   aggregate,
			AggregateID: uuid.New(),
			EventType:   eventType,
			Payload:     json.RawMessage(payload),
			CreatedAt:   createdAt,
		})
	}), "insertPendingEventAt: enqueue")
	t.Cleanup(func() {
		_ = firebird.RunInTx(context.Background(), pool.DB, func(ctx context.Context) error {
			q := firebird.GetQuerier(ctx, pool.DB)
			_, _ = q.ExecContext(ctx, `DELETE FROM MSP_OUTBOX_EVENTS WHERE ID = ?`, id.String())
			return nil
		})
	})
	return id
}

// runDispatcher starts the dispatcher, waits for delay, then stops it with a
// generous 5s timeout. Returns after Stop completes.
func runDispatcher(t *testing.T, d *outboxfb.Dispatcher, delay time.Duration) {
	t.Helper()
	require.NoError(t, d.Start(context.Background()))
	time.Sleep(delay)
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx))
}

// assertRowState reads the outbox row by ID (in a read tx) and returns it.
// The test fails if the row no longer exists.
func assertRowState(t *testing.T, pool *firebird.Pool, id uuid.UUID) outboxRow {
	t.Helper()
	var row outboxRow
	var found bool
	err := firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
		row, found = fetchOutboxRow(ctx, t, pool, id)
		return nil
	})
	require.NoError(t, err)
	require.True(t, found, "expected outbox row %s to exist", id)
	return row
}

// recordingHandler is a Handler that records every event ID it receives.
// It counts how many times Handle has been called per event ID.
type recordingHandler struct {
	mu        sync.Mutex
	eventIDs  []uuid.UUID
	callsMap  map[uuid.UUID]int
	eventType string
}

func newRecordingHandler(eventType string) *recordingHandler {
	return &recordingHandler{
		eventType: eventType,
		callsMap:  map[uuid.UUID]int{},
	}
}

func (h *recordingHandler) EventType() string { return h.eventType }
func (h *recordingHandler) Handle(_ context.Context, e outboxfb.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callsMap[e.ID]++
	h.eventIDs = append(h.eventIDs, e.ID)
	return nil
}

func (h *recordingHandler) IDs() []uuid.UUID {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]uuid.UUID, len(h.eventIDs))
	copy(out, h.eventIDs)
	return out
}

// transientHandler returns ErrTransient a fixed number of times then returns nil.
type transientHandler struct {
	mu        sync.Mutex
	eventType string
	failFor   int
	calls     int
}

func (h *transientHandler) EventType() string { return h.eventType }
func (h *transientHandler) Handle(_ context.Context, _ outboxfb.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.calls <= h.failFor {
		return outboxfb.ErrTransient
	}
	return nil
}

// alwaysTransientHandler always returns ErrTransient.
type alwaysTransientHandler struct{ eventType string }

func (h *alwaysTransientHandler) EventType() string { return h.eventType }

func (h *alwaysTransientHandler) Handle(_ context.Context, _ outboxfb.Event) error {
	return outboxfb.ErrTransient
}

// permanentErrorHandler always returns a non-transient error.
type permanentErrorHandler struct{ eventType string }

func (h *permanentErrorHandler) EventType() string { return h.eventType }
func (h *permanentErrorHandler) Handle(_ context.Context, _ outboxfb.Event) error {
	return errPermanentTest
}

var errPermanentTest = &testError{msg: "permanent test error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// delayHandler sleeps for a fixed duration before returning nil.
type delayHandler struct {
	eventType string
	delay     time.Duration
	handled   atomic.Int32
}

func (h *delayHandler) EventType() string { return h.eventType }
func (h *delayHandler) Handle(_ context.Context, _ outboxfb.Event) error {
	time.Sleep(h.delay)
	h.handled.Add(1)
	return nil
}

// TestDispatcher_PicksUpPending_HappyPath inserts 3 pending rows and asserts
// all are processed (PROCESSED_AT set, ATTEMPTS=1, FAILED_AT NULL) after a
// dispatcher run.
//
//nolint:paralleltest // dispatcher tests must run sequentially — shared Firebird pool + concurrent dispatchers cause driver-level races.
func TestDispatcher_PicksUpPending_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.happy")
	handler := newRecordingHandler(evType)
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(handler)

	id1 := insertPendingEvent(t, pool, "venta", evType, `{"n":1}`)
	id2 := insertPendingEvent(t, pool, "venta", evType, `{"n":2}`)
	id3 := insertPendingEvent(t, pool, "venta", evType, `{"n":3}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
	})
	runDispatcher(t, d, 300*time.Millisecond)

	for _, id := range []uuid.UUID{id1, id2, id3} {
		row := assertRowState(t, pool, id)
		assert.True(t, row.processedAt.Valid, "PROCESSED_AT must be set for %s", id)
		assert.Equal(t, 1, row.attempts, "ATTEMPTS must be 1 for %s", id)
		assert.False(t, row.failedAt.Valid, "FAILED_AT must be NULL for %s", id)
	}

	ids := handler.IDs()
	assert.Len(t, ids, 3, "handler must have been called 3 times")
}

// TestDispatcher_RetriesTransient_UpToMaxAttempts verifies that a handler
// returning ErrTransient is retried and, once it returns nil, the row is
// marked processed.
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_RetriesTransient_UpToMaxAttempts(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.retry")
	// Fail first 2 calls, succeed on 3rd.
	handler := &transientHandler{eventType: evType, failFor: 2}
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(handler)

	id := insertPendingEvent(t, pool, "venta", evType, `{"retry":true}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
		MaxAttempts:  5,
	})
	// Allow at least 3 polls: 3 × 50ms = 150ms, use 600ms to be safe.
	runDispatcher(t, d, 600*time.Millisecond)

	row := assertRowState(t, pool, id)
	assert.True(t, row.processedAt.Valid, "PROCESSED_AT must be set after eventual success")
	assert.Equal(t, 3, row.attempts, "ATTEMPTS must be 3 (2 retries + 1 success)")
	assert.False(t, row.failedAt.Valid, "FAILED_AT must be NULL on eventual success")
}

// TestDispatcher_MarksPermanentFailure_OnTransientExhausted verifies that a
// handler that always returns ErrTransient is eventually moved to FAILED_AT
// once MaxAttempts is exhausted.
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_MarksPermanentFailure_OnTransientExhausted(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.exhausted")
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(&alwaysTransientHandler{eventType: evType})

	id := insertPendingEvent(t, pool, "venta", evType, `{"exhaust":true}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
		MaxAttempts:  2,
	})
	// 2 ticks needed: 2 × 50ms = 100ms, use 500ms for safety.
	runDispatcher(t, d, 500*time.Millisecond)

	row := assertRowState(t, pool, id)
	assert.False(t, row.processedAt.Valid, "PROCESSED_AT must be NULL on exhausted transient")
	assert.True(t, row.failedAt.Valid, "FAILED_AT must be set when MaxAttempts exhausted")
	assert.Equal(t, 2, row.attempts, "ATTEMPTS must equal MaxAttempts")
	assert.True(t, row.lastError.Valid, "LAST_ERROR must be populated")
}

// TestDispatcher_MarksFailureOnNonTransient verifies that a handler returning
// a non-transient error immediately marks the row as permanently failed.
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_MarksFailureOnNonTransient(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.perm")
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(&permanentErrorHandler{eventType: evType})

	id := insertPendingEvent(t, pool, "venta", evType, `{"perm":true}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
		MaxAttempts:  5,
	})
	runDispatcher(t, d, 300*time.Millisecond)

	row := assertRowState(t, pool, id)
	assert.False(t, row.processedAt.Valid, "PROCESSED_AT must be NULL on permanent failure")
	assert.True(t, row.failedAt.Valid, "FAILED_AT must be set immediately")
	assert.Equal(t, 1, row.attempts, "ATTEMPTS must be 1 on first (non-transient) error")
	assert.True(t, row.lastError.Valid, "LAST_ERROR must contain the error message")
	assert.Contains(t, row.lastError.String, "permanent test error")
}

// TestDispatcher_MarksFailedWhenNoHandler verifies that a row whose EVENT_TYPE
// has no registered handler is moved to FAILED_AT with a descriptive error.
//
// The dispatcher only fetches rows whose EVENT_TYPE appears in the registry's
// KnownTypes set. To exercise the "no handler" path we use a custom Registry
// (noLookupReg) that includes the target type in KnownTypes (so the IN filter
// claims the row) but returns nil from Lookup — simulating a deployment gap
// where a row was enqueued by a newer binary version.
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_MarksFailedWhenNoHandler(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.nohandler")
	reg := &noLookupReg{claimTypes: []string{evType}}

	id := insertPendingEvent(t, pool, "venta", evType, `{"no":"handler"}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
	})
	runDispatcher(t, d, 300*time.Millisecond)

	row := assertRowState(t, pool, id)
	assert.False(t, row.processedAt.Valid, "PROCESSED_AT must be NULL when no handler")
	assert.True(t, row.failedAt.Valid, "FAILED_AT must be set")
	assert.Equal(t, 1, row.attempts, "ATTEMPTS must be 1")
	assert.True(t, row.lastError.Valid, "LAST_ERROR must describe the missing handler")
	assert.Contains(t, row.lastError.String, "no handler")
}

// noLookupReg is a Registry that claims a set of event types in the IN filter
// (KnownTypes) but always returns nil from Lookup, simulating a deployment gap
// where rows were enqueued by a newer binary the current instance can't handle.
type noLookupReg struct{ claimTypes []string }

func (r *noLookupReg) KnownTypes() []string             { return r.claimTypes }
func (r *noLookupReg) Lookup(_ string) outboxfb.Handler { return nil }

// TestDispatcher_OrderingPicksOldestFirst inserts two rows with CREATED_AT 5
// minutes apart and asserts that the older row is processed first (the
// dispatcher selects ORDER BY CREATED_AT ASC).
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_OrderingPicksOldestFirst(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.order")
	handler := newRecordingHandler(evType)
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(handler)

	now := time.Now()
	// older is 5 minutes before now; newer is now.
	olderID := insertPendingEventAt(t, pool, "venta", evType, `{"age":"old"}`, now.Add(-5*time.Minute))
	newerID := insertPendingEventAt(t, pool, "venta", evType, `{"age":"new"}`, now)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    2, // both fit in one batch
	})
	runDispatcher(t, d, 300*time.Millisecond)

	ids := handler.IDs()
	require.GreaterOrEqual(t, len(ids), 2, "both rows must have been processed")
	assert.Equal(t, olderID, ids[0], "older event must be processed first")
	assert.Equal(t, newerID, ids[1], "newer event must be processed second")
}

// TestDispatcher_StopGracefullyDrainsInflight verifies that an in-flight
// handler call completes before Stop returns, even when Stop is called while
// the handler is still executing.
//
//nolint:paralleltest // sequential; see TestDispatcher_PicksUpPending_HappyPath.
func TestDispatcher_StopGracefullyDrainsInflight(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)

	evType := uniqueType("dispatcher.inflight")
	handler := &delayHandler{eventType: evType, delay: 200 * time.Millisecond}
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(handler)

	id := insertPendingEvent(t, pool, "venta", evType, `{"delay":true}`)

	d := outboxfb.NewDispatcher(pool, reg, outboxfb.DispatcherConfig{
		PollInterval: 30 * time.Millisecond,
		BatchSize:    10,
		TickTimeout:  5 * time.Second,
	})

	require.NoError(t, d.Start(context.Background()))
	// Sleep just long enough for the first tick to start executing the handler.
	time.Sleep(80 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx), "Stop must return before deadline")

	// The handler must have completed (handler.handled >= 1).
	assert.GreaterOrEqual(t, int(handler.handled.Load()), 1, "in-flight handler must complete")

	// The row must be marked processed.
	row := assertRowState(t, pool, id)
	assert.True(t, row.processedAt.Valid, "PROCESSED_AT must be set for inflight event")
}
