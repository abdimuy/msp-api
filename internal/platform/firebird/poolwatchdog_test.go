package firebird

// White-box package: evaluate() takes a synthetic sql.DBStats so the
// saturation logic is testable without a live database.

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// safeBuffer is a bytes.Buffer usable from the watchdog goroutine.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // io.Writer adapter.
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newTestWatchdog builds a watchdog with a logger writing into buf.
func newTestWatchdog(t *testing.T) (*PoolWatchdog, *safeBuffer) {
	t.Helper()
	buf := &safeBuffer{}
	log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return NewPoolWatchdog(nil, 0, log), buf
}

func TestPoolWatchdog_WarnsWhenPoolIsSaturated(t *testing.T) {
	t.Parallel()
	w, buf := newTestWatchdog(t)

	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 10, InUse: 10})

	out := buf.String()
	assert.Contains(t, out, "pool saturado")
	assert.Contains(t, out, "in_use=10")
	assert.Contains(t, out, "max_open=10")
}

func TestPoolWatchdog_WarnsWhenWaitCountGrows(t *testing.T) {
	t.Parallel()
	w, buf := newTestWatchdog(t)

	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 10, InUse: 1, WaitCount: 4})
	require.Contains(t, buf.String(), "wait_count_delta=4")

	// Same total on the next sample: nothing new to report.
	before := len(buf.String())
	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 10, InUse: 1, WaitCount: 4})
	assert.Len(t, buf.String(), before, "a flat WaitCount must not re-warn")

	// It grew again.
	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 10, InUse: 1, WaitCount: 9})
	assert.Contains(t, buf.String(), "wait_count_delta=5")
}

func TestPoolWatchdog_SilentWhenHealthy(t *testing.T) {
	t.Parallel()
	w, buf := newTestWatchdog(t)

	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 10, InUse: 3, Idle: 7})

	assert.Empty(t, buf.String(), "a healthy pool must not produce noise")
}

func TestPoolWatchdog_IgnoresUnboundedPool(t *testing.T) {
	t.Parallel()
	w, buf := newTestWatchdog(t)

	// MaxOpenConnections == 0 means "unlimited": InUse can never saturate it.
	w.evaluate(context.Background(), sql.DBStats{MaxOpenConnections: 0, InUse: 50})

	assert.Empty(t, buf.String())
}

func TestPoolWatchdog_StartStopIsIdempotentAndSurvivesFxCancel(t *testing.T) {
	t.Parallel()
	buf := &safeBuffer{}
	log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	pool := newDeadTestPool(t)
	w := NewPoolWatchdog(pool, 10*time.Millisecond, log)

	// fx cancels the OnStart context once the start phase completes; the loop
	// must not die with it.
	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, w.Start(startCtx))
	require.NoError(t, w.Start(startCtx), "Start must be idempotent")
	cancelStart()

	time.Sleep(50 * time.Millisecond)
	w.mu.Lock()
	stillRunning := w.running
	w.mu.Unlock()
	assert.True(t, stillRunning, "the loop must outlive the fx OnStart context")

	require.NoError(t, w.Stop(context.Background()))
	require.NoError(t, w.Stop(context.Background()), "Stop must be idempotent")
}

func TestPoolWatchdog_SamplesRealPoolStats(t *testing.T) {
	t.Parallel()
	buf := &safeBuffer{}
	log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	pool := newDeadTestPool(t)
	w := NewPoolWatchdog(pool, time.Hour, log)

	// A closed pool reports zero in-use connections: healthy-looking, silent.
	w.sample(context.Background())
	assert.NotContains(t, buf.String(), "pool saturado")
}

// newDeadTestPool returns a Pool whose *sql.DB is already closed. Enough for
// Stats() (which never dials) without touching the shared dev database.
func newDeadTestPool(t *testing.T) *Pool {
	t.Helper()
	db, err := sql.Open(baseDriverName, "sysdba:x@127.0.0.1:65000//nonexistent.fdb")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return &Pool{DB: db}
}
