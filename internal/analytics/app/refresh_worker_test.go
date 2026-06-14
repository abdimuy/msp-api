//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app

import (
	"context"
	"testing"
	"time"
)

// ─── fullRefreshDue unit tests ────────────────────────────────────────────────

func TestFullRefreshDue_3AM_UTC(t *testing.T) {
	t.Parallel()
	at3am := time.Date(2026, 1, 15, fullRefreshHour, 0, 0, 0, time.UTC)
	if !fullRefreshDue(at3am) {
		t.Errorf("fullRefreshDue(%v) = false; want true", at3am)
	}
}

func TestFullRefreshDue_OtherHours(t *testing.T) {
	t.Parallel()
	for h := range 24 {
		if h == fullRefreshHour {
			continue
		}
		ts := time.Date(2026, 1, 15, h, 0, 0, 0, time.UTC)
		if fullRefreshDue(ts) {
			t.Errorf("fullRefreshDue(%v) = true; want false (hour=%d)", ts, h)
		}
	}
}

// fixedClock is a test clock that always returns a fixed time.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// ─── Start / Stop lifecycle tests ─────────────────────────────────────────────

// TestRefreshWorker_StartStop verifies that Start launches the goroutine and
// Stop returns promptly without a panic or hang. The interval is set well
// above the test duration so the ticker never fires (the loop waits for the
// first tick), so Stop before the interval elapses exercises a clean shutdown
// without ever invoking the nil Service.
func TestRefreshWorker_StartStop(t *testing.T) {
	t.Parallel()

	// Build a worker with a nil Service — tick will call svc.RefrescarCandidatos
	// which will panic... so we use a very short interval but stop BEFORE the
	// first tick fires. Because the loop waits for the ticker (no immediate
	// first-tick), a Stop before the first interval expires is clean.
	clk := fixedClock{t: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)} // hour != 3
	w := NewRefreshWorker(nil, clk, RefreshWorkerConfig{Interval: 10 * time.Second}, nil)

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

// TestRefreshWorker_StartIdempotent verifies that a second Start while already
// running is a no-op (returns nil, does not spawn a second goroutine).
func TestRefreshWorker_StartIdempotent(t *testing.T) {
	t.Parallel()

	clk := fixedClock{t: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)}
	w := NewRefreshWorker(nil, clk, RefreshWorkerConfig{Interval: 10 * time.Second}, nil)

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}
	if err := w.Start(ctx); err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

// TestRefreshWorker_StopIdempotent verifies that calling Stop when not running
// returns nil immediately.
func TestRefreshWorker_StopIdempotent(t *testing.T) {
	t.Parallel()

	clk := fixedClock{t: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)}
	w := NewRefreshWorker(nil, clk, RefreshWorkerConfig{Interval: time.Second}, nil)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop on not-running worker returned error: %v", err)
	}
}
