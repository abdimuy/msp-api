//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

// White-box tests for unexported reconcile functions: ReconcilerConfig.applyDefaults()
// and Reconciler.hasDrift(). These live in package app so they can access unexported
// symbols directly. Black-box reconciler tests live in reconcile_test.go.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── ReconcilerConfig.applyDefaults() boundary tests ─────────────────────────

// TestReconcilerConfig_ApplyDefaults_Boundaries exercises every `<= 0` guard
// in ReconcilerConfig.applyDefaults(). The CONDITIONALS_BOUNDARY mutation
// `<=` → `<` would leave zero values unchanged; each "zero" sub-case catches it.
func TestReconcilerConfig_ApplyDefaults_Boundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           ReconcilerConfig
		wantInterval time.Duration
		wantPageSize int
	}{
		{
			name:         "all_zero_gets_defaults",
			in:           ReconcilerConfig{},
			wantInterval: 7 * 24 * time.Hour,
			wantPageSize: 1000,
		},
		{
			name:         "negative_interval_gets_default",
			in:           ReconcilerConfig{Interval: -1, PageSize: 1},
			wantInterval: 7 * 24 * time.Hour,
			wantPageSize: 1,
		},
		{
			name:         "negative_page_size_gets_default",
			in:           ReconcilerConfig{Interval: time.Hour, PageSize: -1},
			wantInterval: time.Hour,
			wantPageSize: 1000,
		},
		{
			// Positive values must NOT be replaced.
			// Kills the `<= 0` → `< 0` boundary mutation: zero must still be replaced.
			name:         "positive_values_unchanged",
			in:           ReconcilerConfig{Interval: time.Hour, PageSize: 500},
			wantInterval: time.Hour,
			wantPageSize: 500,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := tc.in
			cfg.applyDefaults()
			assert.Equal(t, tc.wantInterval, cfg.Interval, "Interval mismatch")
			assert.Equal(t, tc.wantPageSize, cfg.PageSize, "PageSize mismatch")
		})
	}
}

// TestReconcilerConfig_ApplyDefaults_ZeroInterval kills the specific boundary
// case: Interval == 0 must be replaced with the 7-day default.
func TestReconcilerConfig_ApplyDefaults_ZeroInterval(t *testing.T) {
	t.Parallel()
	cfg := ReconcilerConfig{Interval: 0, PageSize: 1}
	cfg.applyDefaults()
	assert.Equal(t, 7*24*time.Hour, cfg.Interval,
		"zero Interval must become 7-day default")
}

// TestReconcilerConfig_ApplyDefaults_ZeroPageSize kills the specific boundary
// case: PageSize == 0 must be replaced with 1000.
func TestReconcilerConfig_ApplyDefaults_ZeroPageSize(t *testing.T) {
	t.Parallel()
	cfg := ReconcilerConfig{Interval: time.Hour, PageSize: 0}
	cfg.applyDefaults()
	assert.Equal(t, 1000, cfg.PageSize,
		"zero PageSize must become 1000 default")
}

// ─── hasDrift() nil-safety tests ─────────────────────────────────────────────

// buildSaldo is a helper that creates a Saldo with the given saldo amount for
// hasDrift() tests.
func buildSaldo(doctoCCID int, saldoAmt decimal.Decimal) *domain.Saldo {
	s := domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:   doctoCCID,
		ClienteID:   1,
		Folio:       "TST-0001",
		FechaCargo:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal: saldoAmt,
		Saldo:       saldoAmt,
		UpdatedAt:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	return &s
}

// TestHasDrift_NilSafety verifies the `cached == nil || recomputed == nil`
// guard in hasDrift(). The INVERT_LOGICAL mutation changes `||` to `&&`, which
// would allow hasDrift(nil, non-nil) to proceed past the guard and panic.
func TestHasDrift_NilSafety(t *testing.T) {
	t.Parallel()

	r := &Reconciler{deps: ReconcilerDeps{
		Config: ReconcilerConfig{Interval: time.Hour, PageSize: 100},
	}}

	saldo := buildSaldo(1, decimal.NewFromInt(5000))

	t.Run("cached_nil_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, r.hasDrift(nil, saldo),
			"hasDrift(nil, non-nil) must return false, not panic")
	})

	t.Run("recomputed_nil_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, r.hasDrift(saldo, nil),
			"hasDrift(non-nil, nil) must return false, not panic")
	})

	t.Run("both_nil_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, r.hasDrift(nil, nil),
			"hasDrift(nil, nil) must return false, not panic")
	})

	t.Run("equal_saldos_no_drift", func(t *testing.T) {
		t.Parallel()
		saldoA := buildSaldo(1, decimal.NewFromInt(5000))
		saldoB := buildSaldo(1, decimal.NewFromInt(5000))
		assert.False(t, r.hasDrift(saldoA, saldoB),
			"hasDrift with equal saldos must return false")
	})

	t.Run("different_saldo_amount_drift_detected", func(t *testing.T) {
		t.Parallel()
		saldoA := buildSaldo(1, decimal.NewFromInt(5000))
		saldoB := buildSaldo(1, decimal.NewFromInt(4500)) // different saldo
		assert.True(t, r.hasDrift(saldoA, saldoB),
			"hasDrift with different saldo amounts must return true")
	})
}

// ─── loop() context.Canceled branching ───────────────────────────────────────

// TestReconciler_Loop_ContextCanceled_ExitsCleanly verifies that when Run
// returns context.Canceled, the loop returns without logging "pass failed".
// The CONDITIONALS_NEGATION mutant would make the loop log errors instead of
// returning on context.Canceled, and also return on non-cancellation errors.
//
// This test is black-box in spirit but needs the unexported loop behavior
// visible through Start/Stop, using a recording slog logger.
//
// NOTE: does NOT use t.Parallel() since slog is injected via the reconciler's
// own logger field (not process-global), so this test is safe to parallel.
// However, we leave it non-parallel for clarity with the recording handler.
func TestReconciler_Loop_ContextCanceled_ExitsCleanly(t *testing.T) {
	t.Parallel()

	// A lister that returns context.Canceled the first time Page is called.
	cancelingLister := &cancelOnFirstCallLister{}

	h := &internalRecordingHandler{}
	logger := slog.New(h)

	r := NewReconciler(ReconcilerDeps{
		SaldosLister: cancelingLister,
		SaldosRepo:   newInternalFakeSaldosRepo(),
		Recomputer:   &noopRecomputer{},
		Clock:        internalFixedClock{T: time.Now()},
		Config:       ReconcilerConfig{Interval: 10 * time.Millisecond, PageSize: 100},
		Logger:       logger,
	})

	require.NoError(t, r.Start(context.Background()))

	// Wait briefly for at least one tick to fire.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx))

	// Loop exits on context.Canceled without logging "pass failed".
	for _, rec := range h.records {
		assert.NotEqual(t, "cobranza.reconciler: pass failed", rec.Message,
			"loop must not log 'pass failed' when Run returns context.Canceled")
	}
}

// cancelOnFirstCallLister returns context.Canceled on the first Page call.
type cancelOnFirstCallLister struct {
	calls int
}

func (l *cancelOnFirstCallLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	l.calls++
	return nil, 0, context.Canceled
}

// noopRecomputer always returns an error (never called in this test path).
type noopRecomputer struct{}

func (n *noopRecomputer) Recompute(_ context.Context, _ int) (*domain.Saldo, error) {
	return nil, nil
}

// internalFakeSaldosRepo is a minimal outbound.SaldosRepo for internal tests.
type internalFakeSaldosRepo struct{}

func (r *internalFakeSaldosRepo) PorVenta(_ context.Context, _ int) (*domain.Saldo, error) {
	return nil, domain.ErrSaldoNoEncontrado
}

func (r *internalFakeSaldosRepo) PorCargo(_ context.Context, _ int) (*domain.Saldo, error) {
	return nil, domain.ErrSaldoNoEncontrado
}

func (r *internalFakeSaldosRepo) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Saldo, error) {
	return nil, nil
}

func (r *internalFakeSaldosRepo) AbiertasPorCliente(_ context.Context, _ int) ([]domain.Saldo, error) {
	return nil, nil
}

func (r *internalFakeSaldosRepo) ResumenZonas(_ context.Context) ([]domain.ResumenZona, error) {
	return nil, nil
}

func (r *internalFakeSaldosRepo) SyncPorZona(_ context.Context, _ int, _ time.Time, _, _ int) (outbound.SyncPage[domain.Saldo], error) {
	return outbound.SyncPage[domain.Saldo]{}, nil
}

func (r *internalFakeSaldosRepo) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Saldo, error) {
	return nil, nil
}

func newInternalFakeSaldosRepo() *internalFakeSaldosRepo {
	return &internalFakeSaldosRepo{}
}

// ─── Run() pagos pass conditional: both deps required ────────────────────────

// TestReconciler_Run_PagosPass_OnlyWhenBothDepsSet verifies that the pagos pass
// is only executed when BOTH PagosLister AND PagosRecomputer are non-nil.
// The INVERT_LOGICAL mutation changes `&&` to `||`, which would run the pass
// when only one dep is set (causing a nil-deref panic).
func TestReconciler_Run_PagosPass_OnlyWhenBothDepsSet(t *testing.T) {
	t.Parallel()

	t.Run("only_lister_set_pagos_pass_skipped", func(t *testing.T) {
		t.Parallel()
		pagosLister := &countingPagosLister{}

		r := NewReconciler(ReconcilerDeps{
			SaldosLister:    &singlePageLister{},
			SaldosRepo:      newInternalFakeSaldosRepo(),
			Recomputer:      &noopRecomputer{},
			PagosLister:     pagosLister,
			PagosRecomputer: nil, // intentionally nil
			Clock:           internalFixedClock{T: time.Now()},
			Config:          ReconcilerConfig{Interval: time.Hour, PageSize: 100},
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		})

		report, err := r.Run(context.Background())
		require.NoError(t, err)
		// Pagos pass must not have run.
		assert.Equal(t, 0, report.PagosChecked,
			"PagosChecked must be 0 when PagosRecomputer is nil")
		assert.Equal(t, 0, pagosLister.calls,
			"PagosLister.Page must not be called when PagosRecomputer is nil")
	})

	t.Run("only_recomputer_set_pagos_pass_skipped", func(t *testing.T) {
		t.Parallel()
		pagosRecomputer := &countingPagosRecomputer{}

		r := NewReconciler(ReconcilerDeps{
			SaldosLister:    &singlePageLister{},
			SaldosRepo:      newInternalFakeSaldosRepo(),
			Recomputer:      &noopRecomputer{},
			PagosLister:     nil, // intentionally nil
			PagosRecomputer: pagosRecomputer,
			Clock:           internalFixedClock{T: time.Now()},
			Config:          ReconcilerConfig{Interval: time.Hour, PageSize: 100},
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		})

		report, err := r.Run(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, report.PagosChecked,
			"PagosChecked must be 0 when PagosLister is nil")
		assert.Equal(t, 0, pagosRecomputer.calls,
			"PagosRecomputer.Recompute must not be called when PagosLister is nil")
	})

	t.Run("both_set_pagos_pass_runs", func(t *testing.T) {
		t.Parallel()
		pagosLister := &countingPagosLister{ids: []int{10, 20, 30}}
		pagosRecomputer := &countingPagosRecomputer{}

		r := NewReconciler(ReconcilerDeps{
			SaldosLister:    &singlePageLister{},
			SaldosRepo:      newInternalFakeSaldosRepo(),
			Recomputer:      &noopRecomputer{},
			PagosLister:     pagosLister,
			PagosRecomputer: pagosRecomputer,
			Clock:           internalFixedClock{T: time.Now()},
			Config:          ReconcilerConfig{Interval: time.Hour, PageSize: 100},
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		})

		report, err := r.Run(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 3, report.PagosChecked,
			"PagosChecked must be 3 when both deps are set and lister returns 3 IDs")
	})
}

// ─── Run() pagos pass error propagation ──────────────────────────────────────

// TestReconciler_Run_PagosPassError_Propagates verifies that an error from
// runPagosPass causes Run to return that error and set FinishedAt.
// The CONDITIONALS_NEGATION mutant would ignore the error and continue.
func TestReconciler_Run_PagosPassError_Propagates(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("pagos_lister_boom")
	pagosLister := &errorPagosLister{err: errBoom}

	r := NewReconciler(ReconcilerDeps{
		SaldosLister:    &singlePageLister{},
		SaldosRepo:      newInternalFakeSaldosRepo(),
		Recomputer:      &noopRecomputer{},
		PagosLister:     pagosLister,
		PagosRecomputer: &countingPagosRecomputer{},
		Clock:           internalFixedClock{T: time.Now()},
		Config:          ReconcilerConfig{Interval: time.Hour, PageSize: 100},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	report, err := r.Run(context.Background())
	require.ErrorIs(t, err, errBoom,
		"Run must propagate the pagos pass error")
	assert.False(t, report.FinishedAt.IsZero(),
		"FinishedAt must be set even when Run returns an error")
}

// ─── helper fakes for internal tests ─────────────────────────────────────────

// singlePageLister returns one empty page and then signals end-of-list.
type singlePageLister struct{}

func (l *singlePageLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	return []int{}, 0, nil
}

// countingPagosLister returns the preset ids in one page, then end-of-list.
type countingPagosLister struct {
	ids   []int
	calls int
}

func (l *countingPagosLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	l.calls++
	if l.calls == 1 {
		return l.ids, 0, nil
	}
	return nil, 0, nil
}

// countingPagosRecomputer records how many times Recompute was called.
type countingPagosRecomputer struct{ calls int }

func (r *countingPagosRecomputer) Recompute(_ context.Context, _ int) error {
	r.calls++
	return nil
}

// errorPagosLister always returns the given error from Page.
type errorPagosLister struct{ err error }

func (l *errorPagosLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	return nil, 0, l.err
}
