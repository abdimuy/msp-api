//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── fakePagosRecibidosRepo ───────────────────────────────────────────────────

// fakePagosRecibidosRepo is an in-memory outbound.PagosRecibidosRepo for unit
// tests. It serializes access to a simple map of UUID → *domain.PagoRecibido.
type fakePagosRecibidosRepo struct {
	rows      map[uuid.UUID]*domain.PagoRecibido
	insertErr error // if set, Insert returns this error always
	findErr   error // if set, FindByID returns this error always
	lockErr   error // if set, LockByID returns this error always
	updateErr error // if set, Update returns this error always
	updateCnt int   // counts how many times Update has been called
}

func newFakePagosRecibidosRepo() *fakePagosRecibidosRepo {
	return &fakePagosRecibidosRepo{rows: map[uuid.UUID]*domain.PagoRecibido{}}
}

func (f *fakePagosRecibidosRepo) Insert(_ context.Context, p *domain.PagoRecibido) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, exists := f.rows[p.ID()]; exists {
		return domain.ErrPagoYaExiste
	}
	// Store a snapshot by hydrating from the current state.
	f.rows[p.ID()] = p
	return nil
}

func (f *fakePagosRecibidosRepo) Update(_ context.Context, p *domain.PagoRecibido) error {
	f.updateCnt++
	if f.updateErr != nil {
		return f.updateErr
	}
	if _, exists := f.rows[p.ID()]; !exists {
		return domain.ErrPagoNoEncontrado
	}
	f.rows[p.ID()] = p
	return nil
}

func (f *fakePagosRecibidosRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	p, ok := f.rows[id]
	if !ok {
		return nil, domain.ErrPagoNoEncontrado
	}
	return p, nil
}

func (f *fakePagosRecibidosRepo) LockByID(_ context.Context, id uuid.UUID) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	if _, ok := f.rows[id]; !ok {
		return domain.ErrPagoNoEncontrado
	}
	return nil
}

func (f *fakePagosRecibidosRepo) ListPendientes(_ context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error) {
	var out []*domain.PagoRecibido
	for _, p := range f.rows {
		if p.IsPendiente() && p.Intentos() < maxIntentos {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ─── fakeMicrosipPagoWriter ───────────────────────────────────────────────────

// fakeMicrosipPagoWriter satisfies outbound.MicrosipPagoWriter for tests.
type fakeMicrosipPagoWriter struct {
	result    outbound.MicrosipPagoResult
	err       error
	callCount int
	lastInput outbound.MicrosipPagoInput
}

func (f *fakeMicrosipPagoWriter) Aplicar(_ context.Context, in outbound.MicrosipPagoInput) (outbound.MicrosipPagoResult, error) {
	f.callCount++
	f.lastInput = in
	return f.result, f.err
}

// ─── fakeTxRunner ─────────────────────────────────────────────────────────────

// fakeTxRunner satisfies app.TxRunner for unit tests. When err is nil it
// executes fn synchronously without any real Firebird connection. When err is
// set, it returns that error immediately without calling fn.
type fakeTxRunner struct {
	err error // if set, returned without calling fn
}

func (f fakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

// newAplicarSvc wires a Service with the given fakes specifically for
// AplicarPago unit tests. saldos and pagos repos are set to no-op fakes
// (AplicarPago does not use them). The caller supplies the TxRunner, the
// pagosRecibidos repo, the writer, and the clock.
func newAplicarSvc(
	t *testing.T,
	txRunner app.TxRunner,
	pagosRecibidos *fakePagosRecibidosRepo,
	writer *fakeMicrosipPagoWriter,
	now time.Time,
) *app.Service {
	t.Helper()
	return app.NewService(
		newFakeSaldosRepo(),
		newFakePagosRepo(),
		nil, // ventas — not needed
		fixedClock{T: now},
		pagosRecibidos,
		nil, // pagosImagenes — not needed
		writer,
		nil, // storage
		nil, // imageProc
		txRunner,
	)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// baseInput returns a valid CrearPagoInput for tests. now must be
// consistent with the fixedClock set in newWriteSvc.
func baseInput(t *testing.T, now time.Time) app.CrearPagoInput {
	t.Helper()
	return app.CrearPagoInput{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  now.Add(-30 * time.Minute),
	}
}

// makeSaldoWithCancelado builds a Saldo for cargo validation tests.
func makeSaldoForCargo(doctoCCID int, saldo decimal.Decimal, cancelado bool) domain.Saldo {
	return domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:      doctoCCID,
		ClienteID:      1,
		Folio:          "CV-0001",
		FechaCargo:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal:    saldo,
		Saldo:          saldo,
		CargoCancelado: cancelado,
		UpdatedAt:      time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
	})
}

// newWriteSvc wires a Service with the given fakes for write-side tests.
// txMgr is always nil — AplicarPago fast-path will fail with errWriteDepsMissing,
// which is the expected behavior for unit tests (integration tests cover the
// full AplicarPago flow). CrearPago still succeeds: it persists the row and
// then logs the apply warning.
func newWriteSvc(
	t *testing.T,
	now time.Time,
	saldosRepo *fakeSaldosRepo,
	pagosRecibidos *fakePagosRecibidosRepo,
	writer *fakeMicrosipPagoWriter,
) *app.Service {
	t.Helper()
	return app.NewService(
		saldosRepo,
		newFakePagosRepo(),
		nil, // ventas — not needed for write tests
		fixedClock{T: now},
		pagosRecibidos,
		nil, // pagosImagenes — not needed for write tests
		writer,
		nil, // storage
		nil, // imageProc
		nil, // txMgr — nil intentionally; AplicarPago fast-path will return errWriteDepsMissing
	)
}

// ─── TestCrearPago_TimestampValidations ──────────────────────────────────────

func TestCrearPago_TimestampValidations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	tests := []struct {
		name          string
		fechaHoraPago time.Time
		wantErr       error
	}{
		{
			name:          "future_10_minutes",
			fechaHoraPago: now.Add(10 * time.Minute),
			wantErr:       domain.ErrPagoFechaFutura,
		},
		{
			name:          "too_old_31_days",
			fechaHoraPago: now.Add(-31 * 24 * time.Hour),
			wantErr:       domain.ErrPagoFechaMuyAntigua,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			saldos := newFakeSaldosRepo()
			s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
			saldos.byCargo[5000] = &s

			pagosRepo := newFakePagosRecibidosRepo()
			svc := newWriteSvc(t, now, saldos, pagosRepo, nil)

			in := baseInput(t, now)
			in.FechaHoraPago = tc.fechaHoraPago

			_, err := svc.CrearPago(context.Background(), in, by)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestCrearPago_TimestampLateUpload_Accepted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	tests := []struct {
		name          string
		fechaHoraPago time.Time
	}{
		{
			name:          "25_hours_ago_accepted",
			fechaHoraPago: now.Add(-25 * time.Hour),
		},
		{
			name:          "1_hour_ago_accepted",
			fechaHoraPago: now.Add(-1 * time.Hour),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			saldos := newFakeSaldosRepo()
			s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
			saldos.byCargo[5000] = &s

			pagosRepo := newFakePagosRecibidosRepo()
			svc := newWriteSvc(t, now, saldos, pagosRepo, nil)

			in := baseInput(t, now)
			in.FechaHoraPago = tc.fechaHoraPago

			// We expect no timestamp error; the row will be persisted as pendiente
			// (because txMgr=nil causes the fast-path apply to fail).
			pago, err := svc.CrearPago(context.Background(), in, by)
			require.NoError(t, err)
			assert.NotNil(t, pago)
		})
	}
}

// ─── TestCrearPago_CargoValidations ──────────────────────────────────────────

func TestCrearPago_CargoValidations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	t.Run("cargo_not_found", func(t *testing.T) {
		t.Parallel()
		saldos := newFakeSaldosRepo()
		// No cargo 5000 in saldos repo.
		svc := newWriteSvc(t, now, saldos, newFakePagosRecibidosRepo(), nil)

		in := baseInput(t, now)
		_, err := svc.CrearPago(context.Background(), in, by)
		assert.ErrorIs(t, err, domain.ErrPagoCargoNoEncontrado)
	})

	t.Run("cargo_cancelled", func(t *testing.T) {
		t.Parallel()
		saldos := newFakeSaldosRepo()
		s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), true /* cancelado */)
		saldos.byCargo[5000] = &s

		svc := newWriteSvc(t, now, saldos, newFakePagosRecibidosRepo(), nil)

		in := baseInput(t, now)
		_, err := svc.CrearPago(context.Background(), in, by)
		assert.ErrorIs(t, err, domain.ErrPagoCargoCancelado)
	})

	t.Run("importe_exceeds_saldo", func(t *testing.T) {
		t.Parallel()
		saldos := newFakeSaldosRepo()
		// Saldo is 100, importe is 1500.
		s := makeSaldoForCargo(5000, decimal.NewFromInt(100), false)
		saldos.byCargo[5000] = &s

		svc := newWriteSvc(t, now, saldos, newFakePagosRecibidosRepo(), nil)

		in := baseInput(t, now)
		_, err := svc.CrearPago(context.Background(), in, by)
		assert.ErrorIs(t, err, domain.ErrPagoSaldoInsuficiente)
	})
}

// ─── TestCrearPago_PersistsAndReturns ────────────────────────────────────────

// TestCrearPago_PersistsAndReturns verifies that CrearPago inserts a row and
// returns a valid pago. Because txMgr is nil the fast-path AplicarPago will
// fail, so the returned pago will be pendiente — but the row must be present in
// the repo.
func TestCrearPago_PersistsAndReturns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	saldos := newFakeSaldosRepo()
	s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	svc := newWriteSvc(t, now, saldos, pagosRepo, nil)

	in := baseInput(t, now)
	pago, err := svc.CrearPago(context.Background(), in, by)

	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())
	assert.Equal(t, in.CargoDoctoCCID, pago.CargoDoctoCCID())
	assert.Equal(t, in.ClienteID, pago.ClienteID())
	assert.True(t, in.Importe.Equal(pago.Importe()))

	// Row must exist in the repo.
	stored, findErr := pagosRepo.FindByID(context.Background(), in.ID)
	require.NoError(t, findErr)
	assert.Equal(t, in.ID, stored.ID())
}

// ─── TestCrearPago_Idempotency ────────────────────────────────────────────────

func TestCrearPago_Idempotency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	saldos := newFakeSaldosRepo()
	s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	svc := newWriteSvc(t, now, saldos, pagosRepo, nil)

	in := baseInput(t, now)

	// First call: inserts row.
	pago1, err := svc.CrearPago(context.Background(), in, by)
	require.NoError(t, err)
	require.NotNil(t, pago1)

	// Second call with same UUID: idempotency fast-path, returns existing row.
	pago2, err := svc.CrearPago(context.Background(), in, by)
	require.NoError(t, err)
	require.NotNil(t, pago2)

	assert.Equal(t, pago1.ID(), pago2.ID())

	// Exactly one row in the repo.
	assert.Len(t, pagosRepo.rows, 1)
}

// ─── TestCrearPago_ConceptoDerivation ────────────────────────────────────────

func TestCrearPago_ConceptoDerivation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	saldos := newFakeSaldosRepo()
	s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	svc := newWriteSvc(t, now, saldos, pagosRepo, nil)

	in := baseInput(t, now)
	in.FormaCobroID = 137026 // abono mostrador

	pago, err := svc.CrearPago(context.Background(), in, by)
	require.NoError(t, err)
	require.NotNil(t, pago)

	assert.Equal(t, 27969, pago.ConceptoCCID(),
		"formaCobroID 137026 debe derivar conceptoCCID 27969 (abono mostrador)")
}

// ─── TestCrearPago_NilRepoDependency ─────────────────────────────────────────

func TestCrearPago_NilRepoDependency(t *testing.T) {
	t.Parallel()

	// Service with no pagosRecibidos repo wired — should surface wiring error.
	svc := app.NewService(
		newFakeSaldosRepo(),
		newFakePagosRepo(),
		nil,
		fixedClock{T: time.Now()},
		nil, // pagosRecibidos intentionally nil
		nil, nil, nil, nil, nil,
	)

	now := time.Now().UTC()
	in := baseInput(t, now)
	_, err := svc.CrearPago(context.Background(), in, uuid.New())
	require.Error(t, err)
	// Not a domain validation error — it's an internal wiring error.
	assert.NotErrorIs(t, err, domain.ErrPagoCargoNoEncontrado)
}
