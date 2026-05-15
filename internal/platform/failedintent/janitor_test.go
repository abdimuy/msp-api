package failedintent_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ---------------------------------------------------------------------------
// memStore — in-memory Store for janitor tests
// ---------------------------------------------------------------------------

// memStore satisfies failedintent.Store. All methods except PurgeOlderThan
// are no-ops or return nil. PurgeOlderThan deletes intents older than before
// from an in-memory map and broadcasts on purgeCh each time it is called.
type memStore struct {
	mu       sync.Mutex
	intents  map[uuid.UUID]failedintent.Intent
	purgeErr error
	purgeCh  chan struct{} // closed/signalled after each PurgeOlderThan call
	purges   atomic.Int64  // total calls to PurgeOlderThan
}

func newMemStore() *memStore {
	return &memStore{
		intents: make(map[uuid.UUID]failedintent.Intent),
		purgeCh: make(chan struct{}, 64),
	}
}

func (m *memStore) add(i failedintent.Intent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intents[i.ID] = i
}

func (m *memStore) has(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.intents[id]
	return ok
}

func (m *memStore) Save(_ context.Context, i failedintent.Intent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intents[i.ID] = i
	return nil
}

func (m *memStore) Get(_ context.Context, _ uuid.UUID) (*failedintent.Intent, error) {
	return nil, nil //nolint:nilnil // not-found sentinel per Store contract
}

func (m *memStore) List(_ context.Context, _ failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	return failedintent.Page[failedintent.Intent]{}, nil
}

func (m *memStore) UpdateStatus(
	_ context.Context,
	_ uuid.UUID,
	_, _ failedintent.Status,
	_ uuid.UUID,
	_ string,
	_ time.Time,
) error {
	return nil
}

func (m *memStore) IncrementRetry(_ context.Context, _ uuid.UUID) error {
	return nil
}

// PurgeOlderThan removes intents whose ReceivedAt is strictly before `before`.
// It always signals purgeCh after running (even on error).
func (m *memStore) PurgeOlderThan(_ context.Context, before time.Time) (int64, error) {
	m.purges.Add(1)
	defer func() {
		m.purgeCh <- struct{}{}
	}()

	if m.purgeErr != nil {
		return 0, m.purgeErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted int64
	for id, intent := range m.intents {
		if intent.ReceivedAt.Before(before) {
			delete(m.intents, id)
			deleted++
		}
	}
	return deleted, nil
}

// waitForPurge blocks until at least one PurgeOlderThan call completes or the
// context is cancelled.
func (m *memStore) waitForPurge(ctx context.Context) bool {
	select {
	case <-m.purgeCh:
		return true
	case <-ctx.Done():
		return false
	}
}

// ---------------------------------------------------------------------------
// Janitor tests
// ---------------------------------------------------------------------------

func TestJanitor_PurgesAtBoot(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	now := time.Now()

	// Old intent: received 100 days ago (exceeds DefaultRetain of 90 days).
	oldID := uuid.New()
	store.add(failedintent.Intent{
		ID:         oldID,
		ReceivedAt: now.Add(-100 * 24 * time.Hour),
		Status:     failedintent.StatusNew,
	})

	// Fresh intent: received 1 day ago.
	freshID := uuid.New()
	store.add(failedintent.Intent{
		ID:         freshID,
		ReceivedAt: now.Add(-24 * time.Hour),
		Status:     failedintent.StatusNew,
	})

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour, // long enough that only the boot purge fires
		Retain:   90 * 24 * time.Hour,
		Clock:    func() time.Time { return now },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, j.Start(ctx))
	// Wait for the boot purge to complete.
	ok := store.waitForPurge(ctx)
	require.True(t, ok, "purge must complete within timeout")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	assert.False(t, store.has(oldID), "old intent must be purged")
	assert.True(t, store.has(freshID), "fresh intent must be kept")
}

func TestJanitor_StopReturnsPromptly(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   90 * 24 * time.Hour,
	})

	startCtx := context.Background()
	require.NoError(t, j.Start(startCtx))

	// Wait for the boot purge so the goroutine is in the select loop.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopCancel()

	err := j.Stop(stopCtx)
	assert.NoError(t, err, "Stop must return before deadline")
}

func TestJanitor_StartIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   90 * 24 * time.Hour,
	})

	ctx := context.Background()
	require.NoError(t, j.Start(ctx))
	// Second Start must be a no-op (no panic, no error).
	require.NoError(t, j.Start(ctx))

	// Wait for at most one boot purge signal.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	// Only one goroutine means exactly one boot purge (the second Start is a
	// no-op). The count should be exactly 1 (boot purge from first Start only).
	assert.Equal(t, int64(1), store.purges.Load(),
		"idempotent Start must not launch a second goroutine")
}

func TestJanitor_PurgeErrorIsLoggedButNotFatal(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.purgeErr = assert.AnError // every PurgeOlderThan call fails

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: 5 * time.Millisecond, // fast tick to get multiple cycles
		Retain:   90 * 24 * time.Hour,
	})

	startCtx := context.Background()
	require.NoError(t, j.Start(startCtx))

	// Wait for at least two purge attempts (boot + one tick).
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx) // first (boot)
	store.waitForPurge(waitCtx) // second (tick)

	assert.GreaterOrEqual(t, store.purges.Load(), int64(2),
		"janitor must keep ticking after purge errors")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	assert.NoError(t, j.Stop(stopCtx), "Stop must return nil even after repeated errors")
}

// TestJanitor_StopWithoutStart_IsNoOp verifies that calling Stop on a janitor
// that was never started is safe and returns nil immediately.
func TestJanitor_StopWithoutStart_IsNoOp(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
}

// TestJanitor_DefaultsApplied verifies that NewJanitor fills zero-valued
// JanitorConfig fields with the documented defaults.
func TestJanitor_DefaultsApplied(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{Store: store})

	// Start + Stop should succeed using the default clock (time.Now) and the
	// default interval. We don't observe the interval but Start should not
	// error, and the boot purge should fire once.
	require.NoError(t, j.Start(context.Background()))
	<-store.purgeCh

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
	assert.GreaterOrEqual(t, store.purges.Load(), int64(1))
}
