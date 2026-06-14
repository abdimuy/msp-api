//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── Error-injecting repo wrapper ─────────────────────────────────────────────

// erroringRepo wraps fakeWinbackRepo and injects errors on specific methods.
// Only the overridden methods change behaviour; the rest delegate to the embed.
type erroringRepo struct {
	*fakeWinbackRepo
	getRefreshStateErr      error
	upsertErr               error
	saveRefreshStateErr     error
	existingControlFlagsErr error
}

func (r *erroringRepo) GetRefreshState(ctx context.Context, job string) (outbound.RefreshState, error) {
	if r.getRefreshStateErr != nil {
		return outbound.RefreshState{}, r.getRefreshStateErr
	}
	return r.fakeWinbackRepo.GetRefreshState(ctx, job)
}

func (r *erroringRepo) UpsertCandidatos(ctx context.Context, candidatos []*domain.WinbackCandidato) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	return r.fakeWinbackRepo.UpsertCandidatos(ctx, candidatos)
}

func (r *erroringRepo) SaveRefreshState(ctx context.Context, st outbound.RefreshState) error {
	if r.saveRefreshStateErr != nil {
		return r.saveRefreshStateErr
	}
	return r.fakeWinbackRepo.SaveRefreshState(ctx, st)
}

func (r *erroringRepo) ExistingControlFlags(ctx context.Context) (map[int]bool, error) {
	if r.existingControlFlagsErr != nil {
		return nil, r.existingControlFlagsErr
	}
	return r.fakeWinbackRepo.ExistingControlFlags(ctx)
}

// ─── Error-injecting Microsip reader ──────────────────────────────────────────

// erroringMicrosip always returns err from LeerAnclasDesde.
type erroringMicrosip struct {
	anclas []outbound.AnclaCliente
	err    error
}

func (m *erroringMicrosip) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.anclas, nil
}

// ─── GetRefreshState non-NotFound error ───────────────────────────────────────

// TestRefrescarCandidatos_GetRefreshState_NonNotFoundError covers the branch in
// resolveJobAndSince where GetRefreshState returns an error that is NOT
// ErrRefreshStateNotFound. The error must be surfaced as an internal apperror.
func TestRefrescarCandidatos_GetRefreshState_NonNotFoundError(t *testing.T) {
	t.Parallel()

	dbDown := errors.New("db down")
	repo := &erroringRepo{
		fakeWinbackRepo:    newFakeWinbackRepo(),
		getRefreshStateErr: dbDown,
	}
	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), false) // incremental → calls GetRefreshState
	require.Error(t, err)
	// Must NOT be the "not found" sentinel — it must propagate the DB error.
	require.NotErrorIs(t, err, domain.ErrRefreshStateNotFound,
		"expected a non-NotFound error, got ErrRefreshStateNotFound")
}

// ─── LeerAnclasDesde error ────────────────────────────────────────────────────

// TestRefrescarCandidatos_LeerAnclasDesde_Error covers the branch where
// MicrosipReader.LeerAnclasDesde returns an error.
func TestRefrescarCandidatos_LeerAnclasDesde_Error(t *testing.T) {
	t.Parallel()

	repo := &erroringRepo{fakeWinbackRepo: newFakeWinbackRepo()}
	micro := &erroringMicrosip{err: errors.New("microsip unreachable")}
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	require.Error(t, err)
}

// ─── ExistingControlFlags error ───────────────────────────────────────────────

// TestRefrescarCandidatos_ExistingControlFlags_Error covers the branch where
// WinbackRepo.ExistingControlFlags returns an error.
func TestRefrescarCandidatos_ExistingControlFlags_Error(t *testing.T) {
	t.Parallel()

	repo := &erroringRepo{
		fakeWinbackRepo:         newFakeWinbackRepo(),
		existingControlFlagsErr: errors.New("flags query failed"),
	}
	micro := &fakeMicrosipReader{anclas: buildAnclas(2)}
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	require.Error(t, err)
}

// ─── UpsertCandidatos error ───────────────────────────────────────────────────

// TestRefrescarCandidatos_UpsertCandidatos_Error covers the branch where
// WinbackRepo.UpsertCandidatos returns an error. The watermark must NOT be
// saved when upsert fails.
func TestRefrescarCandidatos_UpsertCandidatos_Error(t *testing.T) {
	t.Parallel()

	repo := &erroringRepo{
		fakeWinbackRepo: newFakeWinbackRepo(),
		upsertErr:       errors.New("upsert failed"),
	}
	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	require.Error(t, err)

	// Watermark must NOT have been advanced.
	repo.mu.Lock()
	saved := repo.savedState
	repo.mu.Unlock()
	require.Nil(t, saved, "SaveRefreshState must not be called when UpsertCandidatos fails")
}

// ─── SaveRefreshState error ───────────────────────────────────────────────────

// TestRefrescarCandidatos_SaveRefreshState_Error covers the branch where
// WinbackRepo.SaveRefreshState returns an error after a successful upsert.
func TestRefrescarCandidatos_SaveRefreshState_Error(t *testing.T) {
	t.Parallel()

	repo := &erroringRepo{
		fakeWinbackRepo:     newFakeWinbackRepo(),
		saveRefreshStateErr: errors.New("save state failed"),
	}
	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	require.Error(t, err)
}

// ─── runInTx with non-nil TxRunner ───────────────────────────────────────────

// fakeTxRunner records that RunInTx was called and delegates to fn.
type fakeTxRunner struct {
	mu     sync.Mutex
	called bool
}

func (f *fakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	f.called = true
	f.mu.Unlock()
	return fn(ctx)
}

// TestRunInTx_WithTxManager_FnInvoked verifies that when a non-nil TxRunner is
// wired in, RefrescarCandidatos routes the persist step through RunInTx AND the
// upsert + save still complete successfully.
func TestRunInTx_WithTxManager_FnInvoked(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	micro := &fakeMicrosipReader{anclas: buildAnclas(2)}
	txMgr := &fakeTxRunner{}
	svc := app.NewService(repo, micro, fixedClock{testNow}, txMgr)

	result, err := svc.RefrescarCandidatos(context.Background(), true)
	require.NoError(t, err)

	txMgr.mu.Lock()
	wasCalled := txMgr.called
	txMgr.mu.Unlock()
	require.True(t, wasCalled, "RunInTx was not called")

	// Persist operations must still have completed.
	require.Equal(t, 2, result.Procesados)
	repo.mu.Lock()
	savedState := repo.savedState
	repo.mu.Unlock()
	require.NotNil(t, savedState, "SaveRefreshState must be called inside the tx")
}
