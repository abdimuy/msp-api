package firebird

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultJanitorInterval is how often the janitor purges expired idempotency
// records. One hour matches the failedintent janitor convention.
const DefaultJanitorInterval = time.Hour

// purger is a minimal interface so the Janitor can be unit-tested with a fake
// without pulling in a real *Store. *Store satisfies this interface.
type purger interface {
	PurgeExpired(ctx context.Context, now time.Time) (int64, error)
}

// JanitorConfig configures the periodic purge of expired idempotency records.
type JanitorConfig struct {
	// Store is the persistence backend. *Store satisfies purger.
	Store purger
	// Interval is the period between purge ticks. Defaults to DefaultJanitorInterval.
	Interval time.Duration
	// Clock supplies the current time. Defaults to time.Now.
	Clock func() time.Time
}

func (c *JanitorConfig) defaults() {
	if c.Interval <= 0 {
		c.Interval = DefaultJanitorInterval
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
}

// Janitor periodically purges expired idempotency records from the Firebird
// table. Implements the same lifecycle as failedintent.Janitor so it can be
// wired into the application's fx graph alongside other long-running components.
type Janitor struct {
	cfg     JanitorConfig
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewJanitor builds a Janitor with cfg defaults applied.
func NewJanitor(cfg JanitorConfig) *Janitor {
	cfg.defaults()
	return &Janitor{cfg: cfg}
}

// Start launches the ticker goroutine. Idempotent — a second call while
// already running is a no-op.
func (j *Janitor) Start(_ context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return nil
	}
	// Decouple the goroutine context from the start ctx — start ctx is
	// short-lived per fx convention, but the janitor must outlive it.
	runCtx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.done = make(chan struct{})
	j.running = true
	//nolint:contextcheck // intentional: the loop must outlive the fx start ctx.
	go j.run(runCtx)
	return nil
}

// Stop signals the goroutine to exit and waits for it to drain. Bounded by
// the supplied context's deadline.
func (j *Janitor) Stop(ctx context.Context) error {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return nil
	}
	cancel := j.cancel
	done := j.done
	j.running = false
	j.mu.Unlock()

	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the ticker loop. Each tick fires purgeOnce; transient errors are
// logged and the loop continues.
func (j *Janitor) run(ctx context.Context) {
	defer close(j.done)
	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	// Purge once at boot so the first cycle isn't delayed a full interval.
	j.purgeOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.purgeOnce(ctx)
		}
	}
}

// purgeOnce performs one purge cycle. Errors are logged, not returned — the
// janitor must keep ticking even when the DB is temporarily unhappy.
func (j *Janitor) purgeOnce(ctx context.Context) {
	now := j.cfg.Clock()
	n, err := j.cfg.Store.PurgeExpired(ctx, now)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(
			ctx, "idempotency.janitor: purga fallida",
			"error", err, "now", now,
		)
		return
	}
	if n == 0 {
		return
	}
	slog.InfoContext(
		ctx, "idempotency.janitor: registros expirados eliminados",
		"count", n, "now", now,
	)
}
