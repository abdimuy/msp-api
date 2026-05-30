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

func TestService_PorVenta_Found(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	pvID := 10
	s := makeSaldo(42, decimal.NewFromInt(5000))
	repo.byPV[pvID] = &s
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	got, err := svc.PorVenta(context.Background(), pvID)
	require.NoError(t, err)
	assert.Equal(t, 42, got.DoctoCCID())
}

func TestService_PorVenta_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	_, err := svc.PorVenta(context.Background(), 999)
	require.ErrorIs(t, err, domain.ErrSaldoNoEncontrado)
}

func TestService_PorCargo_Found(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	s := makeSaldo(77, decimal.NewFromInt(2000))
	repo.byCargo[77] = &s
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	got, err := svc.PorCargo(context.Background(), 77)
	require.NoError(t, err)
	assert.Equal(t, 77, got.DoctoCCID())
}

func TestService_PorCargo_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	_, err := svc.PorCargo(context.Background(), 999)
	require.Error(t, err)

	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindNotFound, ae.Kind)
}

func TestService_EnRutaPorZona_VentanaDias_Validation(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	svc := app.NewService(repo, fixedClock{T: time.Now()})
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

	repo := newFakeSaldosRepo()
	s1 := makeSaldo(1, decimal.NewFromInt(1000))
	s2 := makeSaldo(2, decimal.NewFromInt(2000))
	repo.porZona = []domain.Saldo{s1, s2}
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	vd := 7
	got, err := svc.EnRutaPorZona(context.Background(), 3, nil, &vd)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestService_EnRutaPorZona_DesdeAbsoluto(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	repo.porZona = []domain.Saldo{makeSaldo(1, decimal.NewFromInt(1000))}
	svc := app.NewService(repo, fixedClock{T: time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)})

	desde := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)
	got, err := svc.EnRutaPorZona(context.Background(), 3, &desde, nil)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_EnRutaPorZona_ParametrosExcluyentes(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	desde := time.Now()
	vd := 7
	_, err := svc.EnRutaPorZona(context.Background(), 3, &desde, &vd)
	require.ErrorIs(t, err, domain.ErrParametrosExcluyentes)
}

func TestService_EnRutaPorZona_DefaultVentana(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	repo.porZona = []domain.Saldo{}
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	// Both nil → default ventana 7
	_, err := svc.EnRutaPorZona(context.Background(), 1, nil, nil)
	require.NoError(t, err)
}

func TestService_AbiertasPorCliente_DelegatesToRepo(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	s := makeSaldo(5, decimal.NewFromInt(3000))
	repo.abiertas = []domain.Saldo{s}
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	got, err := svc.AbiertasPorCliente(context.Background(), 7)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestService_ResumenZonas_DelegatesToRepo(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	repo.resumen = []domain.ResumenZona{
		domain.HydrateResumenZona(1, 10, decimal.NewFromInt(50000)),
		domain.HydrateResumenZona(2, 5, decimal.NewFromInt(25000)),
	}
	svc := app.NewService(repo, fixedClock{T: time.Now()})

	got, err := svc.ResumenZonas(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, 10, got[0].TotalVentas())
}

func TestService_RepoError_Propagated(t *testing.T) {
	t.Parallel()

	repo := newFakeSaldosRepo()
	boom := errors.New("db unreachable")
	repo.err = boom
	svc := app.NewService(repo, fixedClock{T: time.Now()})
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
