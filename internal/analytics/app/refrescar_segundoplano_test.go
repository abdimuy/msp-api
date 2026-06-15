//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── Test fakes ───────────────────────────────────────────────────────────────

// syncBlockingMicrosip signals when LeerAnclasDesde has been entered, then
// blocks until release is closed. This lets tests assert guard state AFTER the
// goroutine is verifiably inside the blocking call.
type syncBlockingMicrosip struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{} // closed on first entry
	release chan struct{} // close to unblock
	once    sync.Once     // ensures entered is closed only once
}

func newSyncBlockingMicrosip() *syncBlockingMicrosip {
	return &syncBlockingMicrosip{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *syncBlockingMicrosip) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil, nil
}

func (b *syncBlockingMicrosip) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestRefrescarEnSegundoPlano_GuardPreventsDuplicateRun asserts that:
//   - The first call to RefrescarEnSegundoPlano returns true (iniciado) and
//     starts exactly one background goroutine.
//   - Any further call while that goroutine is inside LeerAnclasDesde returns
//     false (ya_en_progreso) and does NOT start another goroutine.
//   - After the goroutine completes, the guard resets and a new run can start.
//
// The test is deterministic and -race clean: goroutine lifecycle is controlled
// via channels; no time.Sleep is used.
func TestRefrescarEnSegundoPlano_GuardPreventsDuplicateRun(t *testing.T) {
	t.Parallel()

	micro := newSyncBlockingMicrosip()
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{t: testNow}, nil)

	// First call: guard is free → must start a goroutine.
	assert.True(t, svc.RefrescarEnSegundoPlano(false), "first call must return true")

	// Wait until the goroutine is verifiably inside LeerAnclasDesde before
	// asserting the guard is held. Without this synchronisation the goroutine
	// might not have reached the blocking point yet.
	select {
	case <-micro.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not enter LeerAnclasDesde within 5s")
	}

	// Guard is now held (goroutine blocked inside LeerAnclasDesde).
	assert.False(t, svc.RefrescarEnSegundoPlano(false), "second call while running must return false")
	assert.False(t, svc.RefrescarEnSegundoPlano(true), "third call (full=true) while running must return false")

	// Only one call should have reached Microsip.
	assert.Equal(t, 1, micro.callCount(), "exactly one LeerAnclasDesde call must occur while guard is held")

	// Unblock the goroutine; wait for the guard to reset.
	close(micro.release)

	// Poll until a new call can start (guard reset).
	guardReset := make(chan struct{})
	go func() {
		for {
			if svc.RefrescarEnSegundoPlano(false) {
				close(guardReset)
				return
			}
		}
	}()

	select {
	case <-guardReset:
		// Guard reset confirmed.
	case <-time.After(5 * time.Second):
		t.Fatal("guard was not reset within 5s after goroutine unblocked")
	}
}

// TestRefrescarEnSegundoPlano_ConcurrentRace fires multiple concurrent callers
// and asserts that only one returns true. This validates race-safety of the
// atomic.Bool guard under -race.
func TestRefrescarEnSegundoPlano_ConcurrentRace(t *testing.T) {
	t.Parallel()

	micro := newSyncBlockingMicrosip()
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{t: testNow}, nil)

	const numCallers = 10
	results := make([]bool, numCallers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = svc.RefrescarEnSegundoPlano(false)
		}(i)
	}
	wg.Wait()

	// Wait until the one goroutine that started is blocking, so we can safely
	// assert callCount without a race.
	select {
	case <-micro.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not enter LeerAnclasDesde within 5s")
	}

	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}
	assert.Equal(t, 1, trueCount, "exactly one concurrent caller must start a refresh")
	assert.Equal(t, 1, micro.callCount(), "exactly one LeerAnclasDesde call must occur")

	// Unblock.
	close(micro.release)
}
