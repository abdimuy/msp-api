//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── applyDefaults: non-zero Interval ─────────────────────────────────────────

// TestApplyDefaults_NonZeroInterval_Preserved verifies that applyDefaults does
// NOT overwrite a positive Interval that was already set.
func TestApplyDefaults_NonZeroInterval_Preserved(t *testing.T) {
	t.Parallel()
	cfg := RefreshWorkerConfig{Interval: 5 * time.Minute}
	cfg.applyDefaults()
	if cfg.Interval != 5*time.Minute {
		t.Errorf("applyDefaults changed non-zero interval: got %v, want %v", cfg.Interval, 5*time.Minute)
	}
}

// TestApplyDefaults_ZeroInterval_SetsDefault verifies that applyDefaults sets
// Interval to 1h when the zero value is passed (the default branch).
func TestApplyDefaults_ZeroInterval_SetsDefault(t *testing.T) {
	t.Parallel()
	cfg := RefreshWorkerConfig{} // Interval is zero
	cfg.applyDefaults()
	if cfg.Interval != time.Hour {
		t.Errorf("applyDefaults did not set default interval: got %v, want %v", cfg.Interval, time.Hour)
	}
}

// TestApplyDefaults_NegativeInterval_SetsDefault verifies that applyDefaults
// also covers the <= 0 branch for negative durations.
func TestApplyDefaults_NegativeInterval_SetsDefault(t *testing.T) {
	t.Parallel()
	cfg := RefreshWorkerConfig{Interval: -time.Minute}
	cfg.applyDefaults()
	if cfg.Interval != time.Hour {
		t.Errorf("applyDefaults did not set default for negative interval: got %v, want %v", cfg.Interval, time.Hour)
	}
}

// ─── Internal fakes for worker tick tests ─────────────────────────────────────

// internalFakeRepo is a minimal WinbackRepo for worker tick tests (package app
// scope, because the worker's tick is not exported and must be exercised via
// the loop goroutine).
type internalFakeRepo struct {
	mu       sync.Mutex
	upserted int
	savedJob string
	notified chan struct{}
	once     sync.Once
}

func newInternalFakeRepo() *internalFakeRepo {
	return &internalFakeRepo{notified: make(chan struct{})}
}

func (r *internalFakeRepo) UpsertCandidatos(_ context.Context, candidatos []*domain.WinbackCandidato) error {
	r.mu.Lock()
	r.upserted += len(candidatos)
	r.mu.Unlock()
	return nil
}

func (r *internalFakeRepo) ListCandidatos(_ context.Context, _ outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	return outbound.Page[*domain.WinbackCandidato]{}, nil
}

func (r *internalFakeRepo) GetRefreshState(_ context.Context, _ string) (outbound.RefreshState, error) {
	return outbound.RefreshState{}, domain.ErrRefreshStateNotFound
}

func (r *internalFakeRepo) SaveRefreshState(_ context.Context, st outbound.RefreshState) error {
	r.mu.Lock()
	r.savedJob = st.Job
	r.mu.Unlock()
	r.once.Do(func() { close(r.notified) })
	return nil
}

func (r *internalFakeRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	return make(map[int]bool), nil
}

func (r *internalFakeRepo) GetCandidato(_ context.Context, _ int) (*domain.WinbackCandidato, error) {
	return nil, domain.ErrWinbackCandidatoNotFound
}

func (r *internalFakeRepo) ListCandidatosByClienteIDs(_ context.Context, _ []int) ([]*domain.WinbackCandidato, error) {
	return []*domain.WinbackCandidato{}, nil
}

func (r *internalFakeRepo) ListCandidatosByZona(_ context.Context, _ string) ([]*domain.WinbackCandidato, error) {
	return []*domain.WinbackCandidato{}, nil
}

func (r *internalFakeRepo) ContarPagosRecientes(_ context.Context, _ []int, _, _ time.Time) (map[int]int, error) {
	return map[int]int{}, nil
}

// internalFakeMicrosip is a minimal MicrosipReader for worker tick tests.
type internalFakeMicrosip struct {
	anclas []outbound.AnclaCliente
}

func (m *internalFakeMicrosip) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	return m.anclas, nil
}

func (m *internalFakeMicrosip) GetNotaCliente(_ context.Context, _ int) (string, error) {
	return "", nil
}

// errorInternalFakeRepo is a minimal repo that always errors on UpsertCandidatos,
// causing the tick's error-logging branch to execute. It signals via notified
// the first time the failing branch is reached and counts every call, so the
// test can assert the loop keeps ticking after an error (resilience property).
type errorInternalFakeRepo struct {
	notified chan struct{}
	once     sync.Once

	mu          sync.Mutex
	upsertCalls int
}

func newErrorInternalFakeRepo() *errorInternalFakeRepo {
	return &errorInternalFakeRepo{notified: make(chan struct{})}
}

func (r *errorInternalFakeRepo) UpsertCandidatos(_ context.Context, _ []*domain.WinbackCandidato) error {
	r.mu.Lock()
	r.upsertCalls++
	r.mu.Unlock()
	// Signal once the tick has actually reached the failing branch, so the
	// post-wait assertions verify the worker survived the error (not just that
	// it got past GetRefreshState).
	r.once.Do(func() { close(r.notified) })
	return errors.New("forced upsert error")
}

func (r *errorInternalFakeRepo) upsertCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.upsertCalls
}

func (r *errorInternalFakeRepo) ListCandidatos(_ context.Context, _ outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	return outbound.Page[*domain.WinbackCandidato]{}, nil
}

func (r *errorInternalFakeRepo) GetRefreshState(_ context.Context, _ string) (outbound.RefreshState, error) {
	return outbound.RefreshState{}, domain.ErrRefreshStateNotFound
}

func (r *errorInternalFakeRepo) SaveRefreshState(_ context.Context, _ outbound.RefreshState) error {
	return nil
}

func (r *errorInternalFakeRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	return make(map[int]bool), nil
}

func (r *errorInternalFakeRepo) GetCandidato(_ context.Context, _ int) (*domain.WinbackCandidato, error) {
	return nil, domain.ErrWinbackCandidatoNotFound
}

func (r *errorInternalFakeRepo) ListCandidatosByClienteIDs(_ context.Context, _ []int) ([]*domain.WinbackCandidato, error) {
	return []*domain.WinbackCandidato{}, nil
}

func (r *errorInternalFakeRepo) ListCandidatosByZona(_ context.Context, _ string) ([]*domain.WinbackCandidato, error) {
	return []*domain.WinbackCandidato{}, nil
}

func (r *errorInternalFakeRepo) ContarPagosRecientes(_ context.Context, _ []int, _, _ time.Time) (map[int]int, error) {
	return map[int]int{}, nil
}

// buildInternalAnclas creates n valid AnclaCliente values for tick tests.
func buildInternalAnclas(n int) []outbound.AnclaCliente {
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	out := make([]outbound.AnclaCliente, n)
	for i := range out {
		out[i] = outbound.AnclaCliente{
			ClienteID:         i + 5000,
			Nombre:            "Cliente",
			Zona:              "Z1",
			Telefono:          "555-0001",
			FechaUltimaCompra: now.AddDate(0, 0, -400),
			Frecuencia:        3,
			Monetary:          decimal.NewFromInt(10_000),
			Saldo:             decimal.NewFromInt(500),
			PorLiquidarPct:    decimal.NewFromFloat(20.0),
		}
	}
	return out
}

// ─── tick: incremental path (hour != 3) ───────────────────────────────────────

// TestRefreshWorker_Tick_Incremental starts a worker with a very short interval
// at a non-3AM clock, lets it tick, and verifies that an incremental
// ("winback_incr") job was saved and UpsertCandidatos was called.
func TestRefreshWorker_Tick_Incremental(t *testing.T) {
	t.Parallel()

	clk := fixedClock{t: time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)} // hour=10, not 3
	repo := newInternalFakeRepo()
	micro := &internalFakeMicrosip{anclas: buildInternalAnclas(2)}
	svc := NewService(repo, micro, clk, nil)

	w := NewRefreshWorker(svc, clk, RefreshWorkerConfig{Interval: 20 * time.Millisecond}, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.Stop(stopCtx)
	})

	// Wait for SaveRefreshState to be called (signalled via notified channel).
	select {
	case <-repo.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker tick to complete")
	}

	repo.mu.Lock()
	job := repo.savedJob
	upserted := repo.upserted
	repo.mu.Unlock()

	require.Equal(t, "winback_incr", job, "incremental tick should save winback_incr job")
	require.Positive(t, upserted, "expected at least one candidato upserted")
}

// ─── tick: full path (hour == 3) ──────────────────────────────────────────────

// TestRefreshWorker_Tick_Full starts a worker whose clock reports 3 AM UTC,
// verifying that the tick drives a full ("winback_full") refresh job.
func TestRefreshWorker_Tick_Full(t *testing.T) {
	t.Parallel()

	clk := fixedClock{t: time.Date(2026, 6, 13, fullRefreshHour, 0, 0, 0, time.UTC)} // hour == 3
	repo := newInternalFakeRepo()
	micro := &internalFakeMicrosip{anclas: buildInternalAnclas(1)}
	svc := NewService(repo, micro, clk, nil)

	w := NewRefreshWorker(svc, clk, RefreshWorkerConfig{Interval: 20 * time.Millisecond}, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.Stop(stopCtx)
	})

	select {
	case <-repo.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker tick to complete")
	}

	repo.mu.Lock()
	job := repo.savedJob
	repo.mu.Unlock()

	require.Equal(t, "winback_full", job, "full tick should save winback_full job")
}

// ─── tick: error path (service returns error) ─────────────────────────────────

// TestRefreshWorker_Tick_Error exercises the error-logging branch of tick by
// wiring a repo that always errors on UpsertCandidatos. The worker must NOT
// crash — it logs the error and keeps running (resilience property).
func TestRefreshWorker_Tick_Error(t *testing.T) {
	t.Parallel()

	clk := fixedClock{t: time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)} // hour=10, incremental
	repo := newErrorInternalFakeRepo()
	micro := &internalFakeMicrosip{anclas: buildInternalAnclas(1)}
	svc := NewService(repo, micro, clk, nil)

	w := NewRefreshWorker(svc, clk, RefreshWorkerConfig{Interval: 20 * time.Millisecond}, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.Stop(stopCtx)
	})

	// Wait until the first tick reaches the UpsertCandidatos error branch.
	select {
	case <-repo.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker tick error path")
	}

	// Resilience: after logging the first error, the loop must keep ticking.
	// A second UpsertCandidatos call proves the worker did not crash or exit.
	require.Eventually(t, func() bool {
		return repo.upsertCallCount() >= 2
	}, 2*time.Second, 5*time.Millisecond, "worker must keep ticking after a tick error")
}
