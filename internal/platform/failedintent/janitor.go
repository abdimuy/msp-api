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
	// Blob, when non-nil, is invoked to delete every on-disk blob whose
	// owning row was just purged. Leaving it nil is valid for tests and
	// deployments that never opt into multipart capture.
	Blob BlobStorage
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
// When the deletion freed any on-disk blobs and a Blob storage is wired,
// the blobs are removed best-effort — a Delete error never aborts the cycle
// (the boot-time orphan sweep is the safety net).
func (j *Janitor) purgeOnce(ctx context.Context) {
	cutoff := j.cfg.Clock().Add(-j.cfg.Retain)
	result, err := j.cfg.Store.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(
			ctx, "failedintent.janitor: purge failed",
			"error", err, "cutoff", cutoff,
		)
		return
	}
	if result.RowsDeleted == 0 {
		return
	}
	if j.cfg.Blob != nil {
		for _, path := range result.BlobPaths {
			if delErr := j.cfg.Blob.Delete(ctx, path); delErr != nil {
				slog.WarnContext(
					ctx, "failedintent.janitor: blob delete failed",
					"error", delErr, "path", path,
				)
			}
		}
	}
	slog.InfoContext(
		ctx, "failedintent.janitor: purged",
		"count", result.RowsDeleted,
		"blobs_deleted", len(result.BlobPaths),
		"cutoff", cutoff,
	)
}
