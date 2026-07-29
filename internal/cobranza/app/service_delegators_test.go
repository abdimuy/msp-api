//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// This file covers the thin delegators and wiring guards on app.Service that
// the aggregate coverage sweep found uncovered: WithReconcilePorts plus the
// four digest/ids reconcile methods, ObtenerPago / ListarPagosPendientes, and
// SyncVentasPorZona / SyncPagosPorZona (including their clampSyncLimit error
// branch).

// ─── fakePagosReconcileRepo ────────────────────────────────────────────────

// fakePagosReconcileRepo is an in-memory outbound.PagosReconcileRepo for
// unit tests. It records the args of the last ListIDs call so tests can
// assert the limit clamping happened before delegation.
type fakePagosReconcileRepo struct {
	digest     outbound.DigestResult
	ids        []int
	hasMoreVal bool
	err        error

	lastZonaID int
	lastAfter  int
	lastLimit  int
	lastDesde  time.Time
}

func (f *fakePagosReconcileRepo) Digest(_ context.Context, _ int, _ time.Time) (outbound.DigestResult, error) {
	if f.err != nil {
		return outbound.DigestResult{}, f.err
	}
	return f.digest, nil
}

func (f *fakePagosReconcileRepo) ListIDs(_ context.Context, zonaID, after, limit int, desde time.Time) ([]int, bool, error) {
	f.lastZonaID = zonaID
	f.lastAfter = after
	f.lastLimit = limit
	f.lastDesde = desde
	if f.err != nil {
		return nil, false, f.err
	}
	return f.ids, f.hasMoreVal, nil
}

// ─── fakeSaldosReconcileRepo ───────────────────────────────────────────────

// fakeSaldosReconcileRepo is an in-memory outbound.SaldosReconcileRepo for
// unit tests. Same shape as fakePagosReconcileRepo.
type fakeSaldosReconcileRepo struct {
	digest     outbound.DigestResult
	ids        []int
	hasMoreVal bool
	err        error

	lastZonaID int
	lastAfter  int
	lastLimit  int
	lastDesde  time.Time
}

func (f *fakeSaldosReconcileRepo) Digest(_ context.Context, _ int, _ time.Time) (outbound.DigestResult, error) {
	if f.err != nil {
		return outbound.DigestResult{}, f.err
	}
	return f.digest, nil
}

func (f *fakeSaldosReconcileRepo) ListIDs(_ context.Context, zonaID, after, limit int, desde time.Time) ([]int, bool, error) {
	f.lastZonaID = zonaID
	f.lastAfter = after
	f.lastLimit = limit
	f.lastDesde = desde
	if f.err != nil {
		return nil, false, f.err
	}
	return f.ids, f.hasMoreVal, nil
}

// ─── fakeVentasRepo ─────────────────────────────────────────────────────────

// fakeVentasRepo is an in-memory outbound.VentasRepo for unit tests.
type fakeVentasRepo struct {
	syncPage outbound.SyncPage[domain.Venta]
	err      error
}

func (f *fakeVentasRepo) SyncPorZona(
	_ context.Context, _ int, _ time.Time, _, _ int, _ time.Time,
) (outbound.SyncPage[domain.Venta], error) {
	if f.err != nil {
		return outbound.SyncPage[domain.Venta]{}, f.err
	}
	return f.syncPage, nil
}

func (f *fakeVentasRepo) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Venta, error) {
	return nil, nil
}

// makeVenta builds a minimal domain.Venta for test use.
func makeVenta(doctoCCID, clienteID int) domain.Venta {
	return domain.HydrateVenta(domain.HydrateVentaParams{
		DoctoCCID:  doctoCCID,
		ClienteID:  clienteID,
		Folio:      "CV-0001",
		FechaCargo: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	})
}

// ─── reconcile-ports wiring helpers ──────────────────────────────────────────

// svcWithReconcilePorts builds a Service and attaches the given fake reconcile
// ports via WithReconcilePorts, exercising both that method and the happy-path
// delegation branch of the four digest/ids methods.
func svcWithReconcilePorts(pagosR *fakePagosReconcileRepo, saldosR *fakeSaldosReconcileRepo) *app.Service {
	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: time.Now()},
		nil, nil, nil, nil, nil, nil,
	)
	svc.WithReconcilePorts(pagosR, saldosR)
	return svc
}

// svcWithoutReconcilePorts builds a Service that never had WithReconcilePorts
// called, so the nil-guard branch of the four digest/ids methods fires.
func svcWithoutReconcilePorts() *app.Service {
	return app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: time.Now()},
		nil, nil, nil, nil, nil, nil,
	)
}

// assertWriteDepsMissing verifies err is the errWriteDepsMissing sentinel
// carrying the expected dep field.
func assertWriteDepsMissing(t *testing.T, err error, wantDep string) {
	t.Helper()
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok, "error must be an *apperror.Error")
	assert.Equal(t, "cobranza_write_deps_missing", ae.Code)
	assert.Equal(t, wantDep, ae.Fields["dep"])
}

// ─── DigestPagosPorZona ───────────────────────────────────────────────────

func TestDigestPagosPorZona_Delegates(t *testing.T) {
	t.Parallel()

	pagosR := &fakePagosReconcileRepo{
		digest: outbound.DigestResult{CountActivos: 3, IDsXor: 7, IDsSum: 42},
	}
	svc := svcWithReconcilePorts(pagosR, &fakeSaldosReconcileRepo{})

	got, err := svc.DigestPagosPorZona(context.Background(), 21563, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 3, got.CountActivos)
	assert.Equal(t, int64(7), got.IDsXor)
	assert.Equal(t, int64(42), got.IDsSum)
}

func TestDigestPagosPorZona_NilGuard(t *testing.T) {
	t.Parallel()

	svc := svcWithoutReconcilePorts()
	_, err := svc.DigestPagosPorZona(context.Background(), 21563, time.Time{})
	assertWriteDepsMissing(t, err, "pagos_reconcile")
}

// ─── ListIDsPagosPorZona ────────────────────────────────────────────────────

func TestListIDsPagosPorZona_DelegatesAndClampsLimit(t *testing.T) {
	t.Parallel()

	pagosR := &fakePagosReconcileRepo{ids: []int{101, 102, 103}, hasMoreVal: true}
	svc := svcWithReconcilePorts(pagosR, &fakeSaldosReconcileRepo{})

	ids, hasMore, err := svc.ListIDsPagosPorZona(context.Background(), 21563, 0, 0, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, []int{101, 102, 103}, ids)
	assert.True(t, hasMore)
	assert.Equal(t, app.DefaultReconcileLimit, pagosR.lastLimit, "limit=0 must clamp to the default before delegation")

	_, _, err = svc.ListIDsPagosPorZona(context.Background(), 21563, 0, app.MaxReconcileLimit+500, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, app.MaxReconcileLimit, pagosR.lastLimit, "oversized limit must clamp to the max before delegation")
}

func TestListIDsPagosPorZona_NilGuard(t *testing.T) {
	t.Parallel()

	svc := svcWithoutReconcilePorts()
	_, _, err := svc.ListIDsPagosPorZona(context.Background(), 21563, 0, 100, time.Time{})
	assertWriteDepsMissing(t, err, "pagos_reconcile")
}

// ─── DigestSaldosPorZona ────────────────────────────────────────────────────

func TestDigestSaldosPorZona_Delegates(t *testing.T) {
	t.Parallel()

	saldosR := &fakeSaldosReconcileRepo{
		digest: outbound.DigestResult{CountActivos: 9, IDsXor: 1, IDsSum: 55},
	}
	svc := svcWithReconcilePorts(&fakePagosReconcileRepo{}, saldosR)

	got, err := svc.DigestSaldosPorZona(context.Background(), 21563, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 9, got.CountActivos)
	assert.Equal(t, int64(1), got.IDsXor)
	assert.Equal(t, int64(55), got.IDsSum)
}

func TestDigestSaldosPorZona_NilGuard(t *testing.T) {
	t.Parallel()

	svc := svcWithoutReconcilePorts()
	_, err := svc.DigestSaldosPorZona(context.Background(), 21563, time.Time{})
	assertWriteDepsMissing(t, err, "saldos_reconcile")
}

// ─── ListIDsSaldosPorZona ───────────────────────────────────────────────────

func TestListIDsSaldosPorZona_DelegatesAndClampsLimit(t *testing.T) {
	t.Parallel()

	saldosR := &fakeSaldosReconcileRepo{ids: []int{201, 202}, hasMoreVal: false}
	svc := svcWithReconcilePorts(&fakePagosReconcileRepo{}, saldosR)

	ids, hasMore, err := svc.ListIDsSaldosPorZona(context.Background(), 21563, 0, -5, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, []int{201, 202}, ids)
	assert.False(t, hasMore)
	assert.Equal(t, app.DefaultReconcileLimit, saldosR.lastLimit, "negative limit must clamp to the default before delegation")
}

func TestListIDsSaldosPorZona_NilGuard(t *testing.T) {
	t.Parallel()

	svc := svcWithoutReconcilePorts()
	_, _, err := svc.ListIDsSaldosPorZona(context.Background(), 21563, 0, 100, time.Time{})
	assertWriteDepsMissing(t, err, "saldos_reconcile")
}

// ─── ObtenerPago ────────────────────────────────────────────────────────────

func TestObtenerPago_Delegates(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		repo, nil, nil, nil, nil, nil,
	)

	got, err := svc.ObtenerPago(context.Background(), pago.ID())
	require.NoError(t, err)
	assert.Equal(t, pago.ID(), got.ID())
}

func TestObtenerPago_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		repo, nil, nil, nil, nil, nil,
	)

	_, err := svc.ObtenerPago(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrPagoNoEncontrado)
}

func TestObtenerPago_NilGuard(t *testing.T) {
	t.Parallel()

	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		nil, nil, nil, nil, nil, nil,
	)

	_, err := svc.ObtenerPago(context.Background(), uuid.New())
	assertWriteDepsMissing(t, err, "pagos_recibidos_repo")
}

// ─── ListarPagosPendientes ──────────────────────────────────────────────────

func TestListarPagosPendientes_Delegates(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pendingPagoInRepo(t, repo) // pendiente, intentos=0 by construction

	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		repo, nil, nil, nil, nil, nil,
	)

	got, err := svc.ListarPagosPendientes(context.Background(), 10, 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListarPagosPendientes_DefaultsWhenNonPositive(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pendingPagoInRepo(t, repo)

	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		repo, nil, nil, nil, nil, nil,
	)

	// limit<=0 and maxIntentos<=0 both fall back to the method's defaults
	// (100 / 10) instead of being passed through as-is.
	got, err := svc.ListarPagosPendientes(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListarPagosPendientes_NilGuard(t *testing.T) {
	t.Parallel()

	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), nil,
		fixedClock{T: fixedNow},
		nil, nil, nil, nil, nil, nil,
	)

	_, err := svc.ListarPagosPendientes(context.Background(), 10, 100)
	assertWriteDepsMissing(t, err, "pagos_recibidos_repo")
}

// ─── SyncVentasPorZona ──────────────────────────────────────────────────────

func TestSyncVentasPorZona_Delegates(t *testing.T) {
	t.Parallel()

	ventas := &fakeVentasRepo{
		syncPage: outbound.SyncPage[domain.Venta]{
			Items:     []domain.Venta{makeVenta(9001, 100)},
			ServerNow: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), ventas,
		fixedClock{T: time.Now()},
		nil, nil, nil, nil, nil, nil,
	)

	page, err := svc.SyncVentasPorZona(context.Background(), 21563, time.Time{}, 0, 1000, nil)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, 9001, page.Items[0].DoctoCCID())
}

func TestSyncVentasPorZona_NegativeLimit_Error(t *testing.T) {
	t.Parallel()

	ventas := &fakeVentasRepo{}
	svc := app.NewService(
		newFakeSaldosRepo(), newFakePagosRepo(), ventas,
		fixedClock{T: time.Now()},
		nil, nil, nil, nil, nil, nil,
	)

	_, err := svc.SyncVentasPorZona(context.Background(), 21563, time.Time{}, 0, -1, nil)
	require.ErrorIs(t, err, domain.ErrParametrosExcluyentes)
}

// ─── SyncPagosPorZona (clampSyncLimit error branch) ─────────────────────────

func TestSyncPagosPorZona_NegativeLimit_Error(t *testing.T) {
	t.Parallel()

	svc, _, _ := newSvc(t)

	_, err := svc.SyncPagosPorZona(context.Background(), 21563, time.Time{}, 0, -1, nil)
	require.ErrorIs(t, err, domain.ErrParametrosExcluyentes)
}
