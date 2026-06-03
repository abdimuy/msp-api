package app_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// fakeLister is an in-memory outbound.SaldosLister.
type fakeLister struct {
	pages [][]int
	calls int
	err   error
}

func (f *fakeLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	if f.calls >= len(f.pages) {
		return nil, 0, nil
	}
	ids := f.pages[f.calls]
	f.calls++
	var next int
	if f.calls < len(f.pages) {
		next = f.calls * 1000
	}
	return ids, next, nil
}

// fakeRecomputer is an in-memory outbound.SaldosRecomputer.
type fakeRecomputer struct {
	results map[int]*domain.Saldo
	err     error
	calls   int
}

func (f *fakeRecomputer) Recompute(_ context.Context, cargoCCID int) (*domain.Saldo, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.results[cargoCCID]
	if !ok {
		return nil, domain.ErrSaldoNoEncontrado
	}
	return s, nil
}

// fakePagosLister implements outbound.PagosLister.
type fakePagosLister struct {
	pages [][]int
	calls int
}

func (f *fakePagosLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	if f.calls >= len(f.pages) {
		return nil, 0, nil
	}
	ids := f.pages[f.calls]
	f.calls++
	var next int
	if f.calls < len(f.pages) {
		next = f.calls * 1000
	}
	return ids, next, nil
}

// fakePagosRecomputer implements outbound.PagosRecomputer.
type fakePagosRecomputer struct {
	failOn map[int]bool
	calls  int
}

func (f *fakePagosRecomputer) Recompute(_ context.Context, impteID int) error {
	f.calls++
	if f.failOn[impteID] {
		return errors.New("boom")
	}
	return nil
}

// fakeSaldosTombstoneCleaner implements outbound.SaldosTombstoneCleaner.
type fakeSaldosTombstoneCleaner struct {
	deleted   int
	err       error
	lastUsed  time.Time
	callCount int
}

func (f *fakeSaldosTombstoneCleaner) DeleteTombstonesOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	f.callCount++
	f.lastUsed = cutoff
	if f.err != nil {
		return 0, f.err
	}
	return f.deleted, nil
}

// fakePagosTombstoneCleaner implements outbound.PagosTombstoneCleaner.
type fakePagosTombstoneCleaner struct {
	deleted   int
	err       error
	lastUsed  time.Time
	callCount int
}

func (f *fakePagosTombstoneCleaner) DeleteTombstonesOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	f.callCount++
	f.lastUsed = cutoff
	if f.err != nil {
		return 0, f.err
	}
	return f.deleted, nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func makeRepoWithCargos(cargos map[int]*domain.Saldo) *fakeSaldosRepo {
	repo := newFakeSaldosRepo()
	for id, s := range cargos {
		repo.byCargo[id] = s
	}
	return repo
}

func newReconciler(t *testing.T, deps app.ReconcilerDeps) *app.Reconciler {
	t.Helper()
	if deps.Logger == nil {
		deps.Logger = newTestLogger()
	}
	return app.NewReconciler(deps)
}

func TestReconciler_Run_NoDrift(t *testing.T) {
	t.Parallel()

	s1 := makeSaldo(1, decimal.NewFromInt(5000))
	s2 := makeSaldo(2, decimal.NewFromInt(3000))

	lister := &fakeLister{pages: [][]int{{1, 2}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{1: &s1, 2: &s2}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{1: &s1, 2: &s2})
	clock := fixedClock{T: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, DriftLog: true, FixDrift: true},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, report.Checked)
	assert.Equal(t, 0, report.Drift)
	assert.Equal(t, 0, report.Errors)
	assert.Equal(t, 2, recomputer.calls)
}

func TestReconciler_Run_DetectsDrift(t *testing.T) {
	t.Parallel()

	cached := makeSaldo(10, decimal.NewFromInt(5000))
	recomputed := makeSaldo(10, decimal.NewFromInt(4500))

	lister := &fakeLister{pages: [][]int{{10}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{10: &recomputed}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{10: &cached})
	clock := fixedClock{T: time.Now()}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, DriftLog: true, FixDrift: true},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, report.Checked)
	assert.Equal(t, 1, report.Drift)
	assert.Equal(t, 0, report.Errors)
}

func TestReconciler_Run_MultiPage(t *testing.T) {
	t.Parallel()

	s1 := makeSaldo(1, decimal.NewFromInt(100))
	s2 := makeSaldo(2, decimal.NewFromInt(200))
	s3 := makeSaldo(3, decimal.NewFromInt(300))

	lister := &fakeLister{pages: [][]int{{1}, {2}, {3}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{1: &s1, 2: &s2, 3: &s3}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{1: &s1, 2: &s2, 3: &s3})
	clock := fixedClock{T: time.Now()}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 1},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, report.Checked)
	assert.Equal(t, 0, report.Drift)
	assert.Equal(t, 0, report.Errors)
}

func TestReconciler_Run_RecomputeError_CountedAsError(t *testing.T) {
	t.Parallel()

	cached := makeSaldo(20, decimal.NewFromInt(1000))
	lister := &fakeLister{pages: [][]int{{20}}}
	recomputer := &fakeRecomputer{err: errors.New("firebird unavailable")}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{20: &cached})
	clock := fixedClock{T: time.Now()}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, report.Checked)
	assert.Equal(t, 0, report.Drift)
	assert.Equal(t, 1, report.Errors)
}

func TestReconciler_Run_ListerError_PropagatesImmediately(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{err: errors.New("lister broken")}
	recomputer := &fakeRecomputer{}
	repo := newFakeSaldosRepo()
	clock := fixedClock{T: time.Now()}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	})

	_, err := r.Run(context.Background())
	require.Error(t, err)
}

func TestReconciler_Run_ReportTimestamps(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{pages: [][]int{{}}}
	recomputer := &fakeRecomputer{}
	repo := newFakeSaldosRepo()
	now := time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{T: now}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, now, report.StartedAt)
	assert.Equal(t, now, report.FinishedAt)
}

func TestReconciler_StartStop(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{pages: [][]int{{}}}
	recomputer := &fakeRecomputer{}
	repo := newFakeSaldosRepo()
	clock := fixedClock{T: time.Now()}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: lister,
		Recomputer:   recomputer,
		SaldosRepo:   repo,
		Clock:        clock,
		Config:       app.ReconcilerConfig{Interval: 10 * time.Millisecond, PageSize: 100},
	})

	require.NoError(t, r.Start(context.Background()))

	// Second Start must be idempotent.
	require.NoError(t, r.Start(context.Background()))

	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))
	require.NoError(t, r.Stop(stopCtx))
}

func TestReconciler_Stop_WithoutStart_IsNoop(t *testing.T) {
	t.Parallel()

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister: &fakeLister{},
		Recomputer:   &fakeRecomputer{},
		SaldosRepo:   newFakeSaldosRepo(),
		Clock:        fixedClock{T: time.Now()},
		Config:       app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	})

	require.NoError(t, r.Stop(context.Background()))
}

func TestReconciler_Run_PagosPassRecomputesEachImpteID(t *testing.T) {
	t.Parallel()

	s := makeSaldo(1, decimal.NewFromInt(100))
	pagosLister := &fakePagosLister{pages: [][]int{{10, 20, 30}}}
	pagosRecomputer := &fakePagosRecomputer{failOn: map[int]bool{20: true}}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:    &fakeLister{pages: [][]int{{1}}},
		Recomputer:      &fakeRecomputer{results: map[int]*domain.Saldo{1: &s}},
		SaldosRepo:      makeRepoWithCargos(map[int]*domain.Saldo{1: &s}),
		PagosLister:     pagosLister,
		PagosRecomputer: pagosRecomputer,
		Clock:           fixedClock{T: time.Now()},
		Config:          app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, report.PagosChecked)
	assert.Equal(t, 1, report.PagosErrors)
}

func TestReconciler_Run_SaldosTombstoneCleanup(t *testing.T) {
	t.Parallel()

	cleaner := &fakeSaldosTombstoneCleaner{deleted: 42}
	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:    &fakeLister{pages: [][]int{{}}},
		Recomputer:      &fakeRecomputer{},
		SaldosRepo:      newFakeSaldosRepo(),
		SaldosTombstone: cleaner,
		Clock:           fixedClock{T: now},
		Config:          app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, TombstoneRetentionDays: 30},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 42, report.SaldosTombstonesDeleted)
	assert.Equal(t, 1, cleaner.callCount)
	expectedCutoff := now.AddDate(0, 0, -30)
	assert.Equal(t, expectedCutoff, cleaner.lastUsed)
}

func TestReconciler_Run_SaldosTombstoneCleanup_DisabledWhenRetentionZero(t *testing.T) {
	t.Parallel()

	cleaner := &fakeSaldosTombstoneCleaner{deleted: 99}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:    &fakeLister{pages: [][]int{{}}},
		Recomputer:      &fakeRecomputer{},
		SaldosRepo:      newFakeSaldosRepo(),
		SaldosTombstone: cleaner,
		Clock:           fixedClock{T: time.Now()},
		Config:          app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, TombstoneRetentionDays: 0},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, report.SaldosTombstonesDeleted)
	assert.Equal(t, 0, cleaner.callCount)
}

func TestReconciler_Run_PagosTombstoneCleanup(t *testing.T) {
	t.Parallel()

	cleaner := &fakePagosTombstoneCleaner{deleted: 17}
	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:   &fakeLister{pages: [][]int{{}}},
		Recomputer:     &fakeRecomputer{},
		SaldosRepo:     newFakeSaldosRepo(),
		PagosTombstone: cleaner,
		Clock:          fixedClock{T: now},
		Config:         app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, TombstoneRetentionDays: 30},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 17, report.PagosTombstonesDeleted)
	assert.Equal(t, 1, cleaner.callCount)
	expectedCutoff := now.AddDate(0, 0, -30)
	assert.Equal(t, expectedCutoff, cleaner.lastUsed)
}

func TestReconciler_Run_PagosTombstoneCleanup_DisabledWhenRetentionZero(t *testing.T) {
	t.Parallel()

	cleaner := &fakePagosTombstoneCleaner{deleted: 99}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:   &fakeLister{pages: [][]int{{}}},
		Recomputer:     &fakeRecomputer{},
		SaldosRepo:     newFakeSaldosRepo(),
		PagosTombstone: cleaner,
		Clock:          fixedClock{T: time.Now()},
		Config:         app.ReconcilerConfig{Interval: time.Hour, PageSize: 100, TombstoneRetentionDays: 0},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, report.PagosTombstonesDeleted)
	assert.Equal(t, 0, cleaner.callCount)
}

// ─── Changelog pruner fake repos ────────────────────────────────────────────

// fakeChangelogRepo records DeleteOlderThan calls and satisfies both
// outbound.PagosChangelogRepo and outbound.SaldosChangelogRepo.
type fakeChangelogRepo struct {
	mu         sync.Mutex
	callCount  int
	lastCutoff time.Time
	lastMaxN   int
	deleted    int
	err        error
}

func (f *fakeChangelogRepo) Since(_ context.Context, _, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
	return nil, nil
}

func (f *fakeChangelogRepo) DeleteOlderThan(_ context.Context, cutoff time.Time, maxN int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	f.lastCutoff = cutoff
	f.lastMaxN = maxN
	if f.err != nil {
		return 0, f.err
	}
	return f.deleted, nil
}

func (f *fakeChangelogRepo) MaxSeqID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (f *fakeChangelogRepo) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

// Compile-time checks that fakeChangelogRepo satisfies both changelog ports.
var (
	_ outbound.PagosChangelogRepo  = (*fakeChangelogRepo)(nil)
	_ outbound.SaldosChangelogRepo = (*fakeChangelogRepo)(nil)
)

// ─── Pruner unit tests ───────────────────────────────────────────────────────

func TestPruner_DeletesOlderThanRetention(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	pagos := &fakeChangelogRepo{deleted: 3}
	saldos := &fakeChangelogRepo{deleted: 2}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:        &fakeLister{pages: [][]int{{}}},
		Recomputer:          &fakeRecomputer{},
		SaldosRepo:          newFakeSaldosRepo(),
		PagosChangelogRepo:  pagos,
		SaldosChangelogRepo: saldos,
		Clock:               fixedClock{T: now},
		Config: app.ReconcilerConfig{
			Interval:                 7 * 24 * time.Hour,
			PageSize:                 100,
			ChangelogRetentionDays:   7,
			ChangelogPruneInterval:   30 * time.Millisecond,
			ChangelogPruneMaxPerCall: 50_000,
		},
	})

	require.NoError(t, r.Start(context.Background()))
	// Wait for at least one pruner tick.
	require.Eventually(t, func() bool {
		return pagos.calls() >= 1 && saldos.calls() >= 1
	}, 2*time.Second, 5*time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	pagos.mu.Lock()
	defer pagos.mu.Unlock()
	saldos.mu.Lock()
	defer saldos.mu.Unlock()

	wantCutoff := now.Add(-7 * 24 * time.Hour)
	assert.Equal(t, wantCutoff, pagos.lastCutoff, "pagos cutoff should be now-7d")
	assert.Equal(t, wantCutoff, saldos.lastCutoff, "saldos cutoff should be now-7d")
	assert.Equal(t, 50_000, pagos.lastMaxN, "pagos maxN should be 50_000")
	assert.Equal(t, 50_000, saldos.lastMaxN, "saldos maxN should be 50_000")
}

func TestPruner_RespectsMaxPerCall(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	pagos := &fakeChangelogRepo{}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:       &fakeLister{pages: [][]int{{}}},
		Recomputer:         &fakeRecomputer{},
		SaldosRepo:         newFakeSaldosRepo(),
		PagosChangelogRepo: pagos,
		Clock:              fixedClock{T: now},
		Config: app.ReconcilerConfig{
			Interval:                 7 * 24 * time.Hour,
			PageSize:                 100,
			ChangelogRetentionDays:   7,
			ChangelogPruneInterval:   30 * time.Millisecond,
			ChangelogPruneMaxPerCall: 100,
		},
	})

	require.NoError(t, r.Start(context.Background()))
	require.Eventually(t, func() bool {
		return pagos.calls() >= 1
	}, 2*time.Second, 5*time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	pagos.mu.Lock()
	defer pagos.mu.Unlock()
	assert.Equal(t, 100, pagos.lastMaxN)
}

func TestPruner_DisabledWhenRetentionZero(t *testing.T) {
	t.Parallel()

	pagos := &fakeChangelogRepo{}
	saldos := &fakeChangelogRepo{}

	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:        &fakeLister{pages: [][]int{{}}},
		Recomputer:          &fakeRecomputer{},
		SaldosRepo:          newFakeSaldosRepo(),
		PagosChangelogRepo:  pagos,
		SaldosChangelogRepo: saldos,
		Clock:               fixedClock{T: time.Now()},
		Config: app.ReconcilerConfig{
			Interval:               7 * 24 * time.Hour,
			PageSize:               100,
			ChangelogRetentionDays: 0, // disabled
			ChangelogPruneInterval: 10 * time.Millisecond,
		},
	})

	require.NoError(t, r.Start(context.Background()))
	// Give a little time to ensure no spurious calls.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	assert.Equal(t, 0, pagos.calls(), "pruner must not call pagos repo when ChangelogRetentionDays=0")
	assert.Equal(t, 0, saldos.calls(), "pruner must not call saldos repo when ChangelogRetentionDays=0")
}

func TestPruner_HandlesRepoError_LogsAndContinues(t *testing.T) {
	t.Parallel()

	// Repo always returns an error. Reconciler must not panic and the next
	// tick must still fire.
	pagos := &fakeChangelogRepo{err: errors.New("firebird unavailable")}

	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	r := newReconciler(t, app.ReconcilerDeps{
		SaldosLister:       &fakeLister{pages: [][]int{{}}},
		Recomputer:         &fakeRecomputer{},
		SaldosRepo:         newFakeSaldosRepo(),
		PagosChangelogRepo: pagos,
		Clock:              fixedClock{T: now},
		Config: app.ReconcilerConfig{
			Interval:               7 * 24 * time.Hour,
			PageSize:               100,
			ChangelogRetentionDays: 7,
			ChangelogPruneInterval: 20 * time.Millisecond,
		},
	})

	require.NoError(t, r.Start(context.Background()))
	// Wait for at least 2 ticks to confirm the loop survives errors.
	require.Eventually(t, func() bool {
		return pagos.calls() >= 2
	}, 3*time.Second, 5*time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	assert.GreaterOrEqual(t, pagos.calls(), 2,
		"pruner should continue firing ticks even after repo errors")
}

func TestPruner_SkipsWhenReposNil(t *testing.T) {
	t.Parallel()

	// Build a logger that records warnings so we can assert the warning was
	// emitted.
	var warned atomic.Bool
	warningLogger := slog.New(&warnCapturingHandler{warned: &warned})

	r := app.NewReconciler(app.ReconcilerDeps{
		SaldosLister:        &fakeLister{pages: [][]int{{}}},
		Recomputer:          &fakeRecomputer{},
		SaldosRepo:          newFakeSaldosRepo(),
		PagosChangelogRepo:  nil,
		SaldosChangelogRepo: nil,
		Clock:               fixedClock{T: time.Now()},
		Config: app.ReconcilerConfig{
			Interval:               7 * 24 * time.Hour,
			PageSize:               100,
			ChangelogRetentionDays: 7,
			ChangelogPruneInterval: 10 * time.Millisecond,
		},
		Logger: warningLogger,
	})

	require.NoError(t, r.Start(context.Background()), "Start must succeed even with nil repos")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx), "Stop must not panic when pruner goroutine was not started")

	assert.True(t, warned.Load(), "reconciler must log a warning when repos are nil but retention > 0")
}

// warnCapturingHandler is a slog.Handler that sets an atomic bool whenever a
// Warn-level record is handled.
type warnCapturingHandler struct {
	warned *atomic.Bool
}

func (h *warnCapturingHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= slog.LevelWarn
}

func (h *warnCapturingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.warned.Store(true)
	}
	return nil
}

func (h *warnCapturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *warnCapturingHandler) WithGroup(_ string) slog.Handler      { return h }
