//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// fixedClock is a controllable Clock returning a fixed instant.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// fakeUniversoReader returns a preset universe (or a preset error).
type fakeUniversoReader struct {
	universo []outbound.ClienteUniverso
	err      error
	// gate, when non-nil, blocks LeerUniversoTehuacan until it is closed. Used to
	// exercise the single-flight guard of ConstruirEnSegundoPlano.
	gate <-chan struct{}
	// calls counts how many times LeerUniversoTehuacan was invoked. Atomic so the
	// test goroutine can poll it while a background build runs.
	calls atomic.Int32
}

// Calls returns the number of LeerUniversoTehuacan invocations.
func (r *fakeUniversoReader) Calls() int { return int(r.calls.Load()) }

func (r *fakeUniversoReader) LeerUniversoTehuacan(_ context.Context) ([]outbound.ClienteUniverso, error) {
	r.calls.Add(1)
	if r.gate != nil {
		<-r.gate
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.universo, nil
}

// fakeCohorteRepo is an in-memory CohorteRepo. It captures upserts and serves
// preset flags and cohorte rows.
type fakeCohorteRepo struct {
	controlFlags    map[int]bool
	contactadoFlags map[int]bool
	listResult      []*domain.CohorteCliente

	controlErr    error
	contactadoErr error
	upsertErr     error
	listErr       error

	mu           sync.Mutex
	upserted     []*domain.CohorteCliente
	lastListParm outbound.ListarCohorteParams
}

func (r *fakeCohorteRepo) UpsertCohorte(_ context.Context, cohorte []*domain.CohorteCliente) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserted = append(r.upserted, cohorte...)
	return nil
}

// upsertedCount returns the number of rows captured so far (thread-safe).
func (r *fakeCohorteRepo) upsertedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.upserted)
}

func (r *fakeCohorteRepo) ListarCohorte(_ context.Context, p outbound.ListarCohorteParams) ([]*domain.CohorteCliente, error) {
	r.lastListParm = p
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listResult, nil
}

func (r *fakeCohorteRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	if r.controlErr != nil {
		return nil, r.controlErr
	}
	if r.controlFlags == nil {
		return map[int]bool{}, nil
	}
	return r.controlFlags, nil
}

func (r *fakeCohorteRepo) ExistingContactadoFlags(_ context.Context) (map[int]bool, error) {
	if r.contactadoErr != nil {
		return nil, r.contactadoErr
	}
	if r.contactadoFlags == nil {
		return map[int]bool{}, nil
	}
	return r.contactadoFlags, nil
}
