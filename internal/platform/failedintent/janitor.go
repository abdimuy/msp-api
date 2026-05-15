package failedintent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultJanitorInterval is how often the janitor purges expired intents.
const DefaultJanitorInterval = time.Hour

// DefaultRetain is the lifetime of a captured intent.
//
// 90 days is chosen as a compromise between legal-evidence preservation
// (long enough to survive a customer dispute cycle) and PII minimisation
// (short enough to limit blast radius if the admin endpoint is ever
// compromised). See ADR-0005.
const DefaultRetain = 90 * 24 * time.Hour

// JanitorConfig configures the periodic purge.
type JanitorConfig struct {
	// Store is the persistence backend whose rows are purged.
	Store Store
	// Interval is the period between purge ticks. Defaults to one hour.
	Interval time.Duration
	// Retain is how long an intent is kept after capture. Defaults to 90 days.
	Retain time.Duration
	// Clock supplies the current time. Defaults to time.Now.
	Clock func() time.Time
}

func (c *JanitorConfig) defaults() {
	if c.Interval <= 0 {
		c.Interval = DefaultJanitorInterval
	}
	if c.Retain <= 0 {
		c.Retain = DefaultRetain
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
}

// Janitor periodically purges captured intents older than its retention
// window. Implements lifecycle.Hooks so it can be wired into the application
// fx graph alongside other long-running components.
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

// purgeOnce performs one purge cycle. Errors are logged, not returned: the
// janitor must keep ticking even when the DB is temporarily unhappy.
func (j *Janitor) purgeOnce(ctx context.Context) {
	cutoff := j.cfg.Clock().Add(-j.cfg.Retain)
	deleted, err := j.cfg.Store.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(ctx, "failedintent.janitor: purge failed",
			"error", err, "cutoff", cutoff,
		)
		return
	}
	if deleted == 0 {
		return
	}
	slog.InfoContext(ctx, "failedintent.janitor: purged",
		"count", deleted, "cutoff", cutoff,
	)
}
