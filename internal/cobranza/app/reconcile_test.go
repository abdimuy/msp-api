package app_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// fakeLister is an in-memory outbound.SaldosLister.
type fakeLister struct {
	// pages holds the cargo IDs returned per Page call. Each element is a
	// "page" of IDs; the last page returns nextCursor=0.
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
	// If there are more pages, return a non-zero cursor; otherwise 0.
	var next int
	if f.calls < len(f.pages) {
		next = f.calls * 1000 // arbitrary non-zero value
	}
	return ids, next, nil
}

// fakeRecomputer is an in-memory outbound.SaldosRecomputer. It returns the
// saldo stored in the results map for the given cargo ID, or the configured
// error.
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

func TestReconciler_Run_NoDrift(t *testing.T) {
	t.Parallel()

	s1 := makeSaldo(1, decimal.NewFromInt(5000))
	s2 := makeSaldo(2, decimal.NewFromInt(3000))

	lister := &fakeLister{pages: [][]int{{1, 2}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{1: &s1, 2: &s2}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{1: &s1, 2: &s2})
	clock := fixedClock{T: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 100,
		DriftLog: true,
		FixDrift: true,
	}, newTestLogger())

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, report.Checked)
	assert.Equal(t, 0, report.Drift)
	assert.Equal(t, 0, report.Errors)
	assert.Equal(t, 2, recomputer.calls)
}

func TestReconciler_Run_DetectsDrift(t *testing.T) {
	t.Parallel()

	// cached row has saldo=5000; recomputed row has saldo=4500 (drift)
	cached := makeSaldo(10, decimal.NewFromInt(5000))
	recomputed := makeSaldo(10, decimal.NewFromInt(4500))

	lister := &fakeLister{pages: [][]int{{10}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{10: &recomputed}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{10: &cached})
	clock := fixedClock{T: time.Now()}

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 100,
		DriftLog: true,
		FixDrift: true,
	}, newTestLogger())

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

	// Three pages: [1], [2], [3]
	lister := &fakeLister{pages: [][]int{{1}, {2}, {3}}}
	recomputer := &fakeRecomputer{results: map[int]*domain.Saldo{1: &s1, 2: &s2, 3: &s3}}
	repo := makeRepoWithCargos(map[int]*domain.Saldo{1: &s1, 2: &s2, 3: &s3})
	clock := fixedClock{T: time.Now()}

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 1,
	}, newTestLogger())

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

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 100,
	}, newTestLogger())

	report, err := r.Run(context.Background())
	require.NoError(t, err) // transient errors inside Run are counted, not propagated
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

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 100,
	}, newTestLogger())

	_, err := r.Run(context.Background())
	require.Error(t, err)
}

func TestReconciler_Run_ReportTimestamps(t *testing.T) {
	t.Parallel()

	lister := &fakeLister{pages: [][]int{{}}} // empty page — one pass with 0 rows
	recomputer := &fakeRecomputer{}
	repo := newFakeSaldosRepo()
	now := time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{T: now}

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		Interval: time.Hour,
		PageSize: 100,
	}, newTestLogger())

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

	r := app.NewReconciler(lister, recomputer, repo, clock, app.ReconcilerConfig{
		// Very short interval so the loop ticks at least once before Stop.
		Interval: 10 * time.Millisecond,
		PageSize: 100,
	}, newTestLogger())

	require.NoError(t, r.Start(context.Background()))

	// Second Start must be idempotent.
	require.NoError(t, r.Start(context.Background()))

	// Let at least one tick happen before stopping.
	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	// Second Stop must be idempotent.
	require.NoError(t, r.Stop(stopCtx))
}

func TestReconciler_Stop_WithoutStart_IsNoop(t *testing.T) {
	t.Parallel()

	r := app.NewReconciler(
		&fakeLister{},
		&fakeRecomputer{},
		newFakeSaldosRepo(),
		fixedClock{T: time.Now()},
		app.ReconcilerConfig{Interval: time.Hour, PageSize: 100},
		newTestLogger(),
	)

	ctx := context.Background()
	require.NoError(t, r.Stop(ctx))
}
