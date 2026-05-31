package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// fixedClock is an outbound.Clock that always returns T.
type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// fakeSaldosRepo is an in-memory outbound.SaldosRepo for unit tests.
type fakeSaldosRepo struct {
	byPV     map[int]*domain.Saldo
	byCargo  map[int]*domain.Saldo
	porZona  []domain.Saldo
	abiertas []domain.Saldo
	resumen  []domain.ResumenZona
	syncPage outbound.SyncPage[domain.Saldo]
	err      error
}

func newFakeSaldosRepo() *fakeSaldosRepo {
	return &fakeSaldosRepo{
		byPV:    map[int]*domain.Saldo{},
		byCargo: map[int]*domain.Saldo{},
	}
}

func (f *fakeSaldosRepo) PorVenta(_ context.Context, doctoPVID int) (*domain.Saldo, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.byPV[doctoPVID]
	if !ok {
		return nil, domain.ErrSaldoNoEncontrado
	}
	return s, nil
}

func (f *fakeSaldosRepo) PorCargo(_ context.Context, doctoCCID int) (*domain.Saldo, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.byCargo[doctoCCID]
	if !ok {
		return nil, domain.ErrSaldoNoEncontrado
	}
	return s, nil
}

func (f *fakeSaldosRepo) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Saldo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.porZona, nil
}

func (f *fakeSaldosRepo) AbiertasPorCliente(_ context.Context, _ int) ([]domain.Saldo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.abiertas, nil
}

func (f *fakeSaldosRepo) ResumenZonas(_ context.Context) ([]domain.ResumenZona, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resumen, nil
}

func (f *fakeSaldosRepo) SyncPorZona(
	_ context.Context, _ int, _ time.Time, _, _ int,
) (outbound.SyncPage[domain.Saldo], error) {
	if f.err != nil {
		return outbound.SyncPage[domain.Saldo]{}, f.err
	}
	return f.syncPage, nil
}

// fakePagosRepo is an in-memory outbound.PagosRepo for unit tests.
type fakePagosRepo struct {
	byVenta      map[int][]domain.Pago
	byCliente    map[int][]domain.Pago
	porZona      []domain.Pago
	syncPage     outbound.SyncPage[domain.Pago]
	lastSyncCall syncPagosCall
	err          error
}

func newFakePagosRepo() *fakePagosRepo {
	return &fakePagosRepo{
		byVenta:   map[int][]domain.Pago{},
		byCliente: map[int][]domain.Pago{},
	}
}

func (f *fakePagosRepo) PorVenta(_ context.Context, doctoCCID int) ([]domain.Pago, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byVenta[doctoCCID], nil
}

func (f *fakePagosRepo) PorCliente(_ context.Context, clienteID int) ([]domain.Pago, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byCliente[clienteID], nil
}

func (f *fakePagosRepo) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Pago, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.porZona, nil
}

func (f *fakePagosRepo) SyncPorZona(
	_ context.Context, _ int, cursor time.Time, afterID, limit int, desde time.Time,
) (outbound.SyncPage[domain.Pago], error) {
	f.lastSyncCall = syncPagosCall{cursor: cursor, afterID: afterID, limit: limit, desde: desde}
	if f.err != nil {
		return outbound.SyncPage[domain.Pago]{}, f.err
	}
	return f.syncPage, nil
}

// syncPagosCall records the args of the last fakePagosRepo.SyncPorZona call.
type syncPagosCall struct {
	cursor  time.Time
	afterID int
	limit   int
	desde   time.Time
}

// makeSaldo builds a minimal Saldo for test use.
func makeSaldo(doctoCCID int, saldo decimal.Decimal) domain.Saldo {
	return domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:   doctoCCID,
		ClienteID:   1,
		Folio:       "TST-0001",
		FechaCargo:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal: saldo,
		Saldo:       saldo,
		UpdatedAt:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	})
}

// makePago builds a minimal Pago for test use.
func makePago(impteID int, importe decimal.Decimal) domain.Pago {
	return domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: impteID,
		DoctoCCID:      impteID - 1,
		DoctoCCAcrID:   impteID - 4,
		ClienteID:      1,
		Folio:          "cv0000001",
		ConceptoCCID:   87327,
		Fecha:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Importe:        importe,
		Impuesto:       decimal.Zero,
		UpdatedAt:      time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
	})
}

// newSvc builds a Service over the fakes (and a clock fixed at T=now).
func newSvc(t *testing.T) (*app.Service, *fakeSaldosRepo, *fakePagosRepo) {
	t.Helper()
	saldos := newFakeSaldosRepo()
	pagos := newFakePagosRepo()
	svc := app.NewService(saldos, pagos, nil, fixedClock{T: time.Now()})
	return svc, saldos, pagos
}

func TestService_PorVenta_Found(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	pvID := 10
	s := makeSaldo(42, decimal.NewFromInt(5000))
	saldos.byPV[pvID] = &s

	got, err := svc.PorVenta(context.Background(), pvID)
	require.NoError(t, err)
	assert.Equal(t, 42, got.DoctoCCID())
}

func TestService_PorVenta_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)
	_, err := svc.PorVenta(context.Background(), 999)
	require.ErrorIs(t, err, domain.ErrSaldoNoEncontrado)
}

func TestService_PorCargo_Found(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	s := makeSaldo(77, decimal.NewFromInt(2000))
	saldos.byCargo[77] = &s

	got, err := svc.PorCargo(context.Background(), 77)
	require.NoError(t, err)
	assert.Equal(t, 77, got.DoctoCCID())
}

func TestService_PorCargo_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)

	_, err := svc.PorCargo(context.Background(), 999)
	require.Error(t, err)

	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindNotFound, ae.Kind)
}

func TestService_EnRutaPorZona_VentanaDias_Validation(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		ventanaDias int
		wantErr     bool
	}{
		{"zero is valid", 0, false},
		{"90 is valid", 90, false},
		{"45 is valid", 45, false},
		{"negative is invalid", -1, true},
		{"91 is invalid", 91, true},
		{"large is invalid", 365, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vd := tc.ventanaDias
			_, err := svc.EnRutaPorZona(ctx, 1, nil, &vd)
			if tc.wantErr {
				require.ErrorIs(t, err, domain.ErrVentanaDiasInvalida)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_EnRutaPorZona_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	saldos.porZona = []domain.Saldo{
		makeSaldo(1, decimal.NewFromInt(1000)),
		makeSaldo(2, decimal.NewFromInt(2000)),
	}

	vd := 7
	got, err := svc.EnRutaPorZona(context.Background(), 3, nil, &vd)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestService_EnRutaPorZona_DesdeAbsoluto(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	saldos.porZona = []domain.Saldo{makeSaldo(1, decimal.NewFromInt(1000))}

	desde := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)
	got, err := svc.EnRutaPorZona(context.Background(), 3, &desde, nil)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_EnRutaPorZona_ParametrosExcluyentes(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)

	desde := time.Now()
	vd := 7
	_, err := svc.EnRutaPorZona(context.Background(), 3, &desde, &vd)
	require.ErrorIs(t, err, domain.ErrParametrosExcluyentes)
}

func TestService_EnRutaPorZona_DefaultVentana(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)

	_, err := svc.EnRutaPorZona(context.Background(), 1, nil, nil)
	require.NoError(t, err)
}

func TestService_AbiertasPorCliente_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	saldos.abiertas = []domain.Saldo{makeSaldo(5, decimal.NewFromInt(3000))}

	got, err := svc.AbiertasPorCliente(context.Background(), 7)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_ResumenZonas_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	saldos.resumen = []domain.ResumenZona{
		domain.HydrateResumenZona(1, 10, decimal.NewFromInt(50000)),
		domain.HydrateResumenZona(2, 5, decimal.NewFromInt(25000)),
	}

	got, err := svc.ResumenZonas(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, 10, got[0].TotalVentas())
}

func TestService_RepoError_Propagated(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	boom := errors.New("db unreachable")
	saldos.err = boom
	ctx := context.Background()

	_, err := svc.PorVenta(ctx, 1)
	require.ErrorIs(t, err, boom)

	_, err = svc.PorCargo(ctx, 1)
	require.ErrorIs(t, err, boom)

	_, err = svc.AbiertasPorCliente(ctx, 1)
	require.ErrorIs(t, err, boom)

	_, err = svc.ResumenZonas(ctx)
	require.ErrorIs(t, err, boom)
}

// ─── Sync tests ───────────────────────────────────────────────────────────────

func TestService_SyncSaldosPorZona_DefaultLimit(t *testing.T) {
	t.Parallel()
	svc, saldos, _ := newSvc(t)
	saldos.syncPage = outbound.SyncPage[domain.Saldo]{
		Items:        []domain.Saldo{makeSaldo(1, decimal.NewFromInt(100))},
		MaxUpdatedAt: time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
		ServerNow:    time.Date(2026, 5, 30, 0, 0, 5, 0, time.UTC),
		HasMore:      false,
	}

	page, err := svc.SyncSaldosPorZona(context.Background(), 21563, time.Time{}, 0, 0)
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	assert.False(t, page.HasMore)
}

func TestService_SyncSaldosPorZona_LimitClamping(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvc(t)

	_, err := svc.SyncSaldosPorZona(context.Background(), 1, time.Time{}, 0, -1)
	require.ErrorIs(t, err, domain.ErrParametrosExcluyentes)

	// Large limit clamps silently — no error.
	_, err = svc.SyncSaldosPorZona(context.Background(), 1, time.Time{}, 0, 99999)
	require.NoError(t, err)
}

func TestService_PagosPorVenta_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, _, pagos := newSvc(t)
	pagos.byVenta[100] = []domain.Pago{makePago(200, decimal.NewFromInt(500))}

	got, err := svc.PagosPorVenta(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 200, got[0].ImpteDoctoCCID())
}

func TestService_PagosPorCliente_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, _, pagos := newSvc(t)
	pagos.byCliente[7] = []domain.Pago{makePago(300, decimal.NewFromInt(750))}

	got, err := svc.PagosPorCliente(context.Background(), 7)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_PagosEnRutaPorZona_ResolvesCutoff(t *testing.T) {
	t.Parallel()
	svc, _, pagos := newSvc(t)
	pagos.porZona = []domain.Pago{makePago(400, decimal.NewFromInt(1000))}

	desde := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	got, err := svc.PagosEnRutaPorZona(context.Background(), 3, &desde, nil)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_SyncPagosPorZona_DelegatesToRepo(t *testing.T) {
	t.Parallel()
	svc, _, pagos := newSvc(t)
	pagos.syncPage = outbound.SyncPage[domain.Pago]{
		Items:     []domain.Pago{makePago(500, decimal.NewFromInt(2000))},
		ServerNow: time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
	}

	page, err := svc.SyncPagosPorZona(context.Background(), 21563, time.Time{}, 0, 1000, nil)
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	assert.True(t, pagos.lastSyncCall.desde.IsZero(), "desde nil debe propagarse como zero time al repo")
}

func TestService_SyncPagosPorZona_PropagatesDesde(t *testing.T) {
	t.Parallel()
	svc, _, pagos := newSvc(t)
	desde := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	_, err := svc.SyncPagosPorZona(context.Background(), 21563, time.Time{}, 0, 1000, &desde)
	require.NoError(t, err)
	assert.True(t, desde.Equal(pagos.lastSyncCall.desde), "desde no nil debe propagarse al repo")
}
