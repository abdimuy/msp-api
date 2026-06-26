//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// testPrediccionesNow is the fixed reference time for predicciones tests.
var testPrediccionesNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// makePrediccionesCandidato builds a candidato with sufficient purchase history
// to trigger Disponible=true in ObtenerPredicciones.
func makePrediccionesCandidato(clienteID int) *domain.WinbackCandidato {
	primerVenta := testPrediccionesNow.AddDate(-2, 0, 0)
	ultimaVenta := testPrediccionesNow.AddDate(0, -3, 0)
	return mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            clienteID,
		Nombre:               "Ramírez García Pedro",
		Zona:                 "NORTE",
		Telefono:             "3312345678",
		FechaUltimaCompra:    ultimaVenta,
		FechaPrimerVenta:     primerVenta,
		FechaUltimaVenta:     ultimaVenta,
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(10_000),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(50_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         primerVenta,
		Now:                  testPrediccionesNow,
	})
}

// TestObtenerPredicciones_ConHistorial verifies that a client with sufficient
// purchase history returns Disponible=true with valid credible intervals and
// Draws > 0.
func TestObtenerPredicciones_ConHistorial(t *testing.T) {
	t.Parallel()

	c := makePrediccionesCandidato(100)
	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPrediccionesNow}, nil)

	pred, err := svc.ObtenerPredicciones(context.Background(), 100)
	require.NoError(t, err)
	assert.True(t, pred.Disponible, "Disponible must be true with sufficient history")
	assert.Positive(t, pred.Draws)

	for _, tc := range []struct {
		name      string
		lo, p, hi float64
	}{
		{"PAlive", pred.PAlive.Lo, pred.PAlive.Punto, pred.PAlive.Hi},
		{"ComprasEsperadas12m", pred.ComprasEsperadas12m.Lo, pred.ComprasEsperadas12m.Punto, pred.ComprasEsperadas12m.Hi},
		{"CLV", pred.CLV.Lo, pred.CLV.Punto, pred.CLV.Hi},
		{"ProximaCompraDias", pred.ProximaCompraDias.Lo, pred.ProximaCompraDias.Punto, pred.ProximaCompraDias.Hi},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.LessOrEqual(t, tc.lo, tc.p, "%s: Lo must be ≤ Punto", tc.name)
			assert.LessOrEqual(t, tc.p, tc.hi, "%s: Punto must be ≤ Hi", tc.name)
		})
	}
}

// TestObtenerPredicciones_NotFound verifies that a missing candidato degrades
// to Disponible=false without error.
func TestObtenerPredicciones_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo() // no candidates
	svc := app.NewService(repo, nil, fixedClock{testPrediccionesNow}, nil)

	pred, err := svc.ObtenerPredicciones(context.Background(), 999)
	require.NoError(t, err)
	assert.False(t, pred.Disponible)
	assert.Equal(t, 0, pred.Draws)
}

// TestObtenerPredicciones_FechaPrimerVentaZero verifies that a candidato with
// zero FechaPrimerVenta degrades to Disponible=false without error.
func TestObtenerPredicciones_FechaPrimerVentaZero(t *testing.T) {
	t.Parallel()

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         200,
		Nombre:            "Test Zero Fecha",
		Zona:              "Z1",
		Telefono:          "000",
		FechaUltimaCompra: testPrediccionesNow.AddDate(0, -1, 0),
		// FechaPrimerVenta intentionally zero
		VentasMesesDistintos: 3,
		Monetary:             decimal.NewFromInt(10_000),
		MonetaryVProm:        decimal.NewFromInt(10_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         testPrediccionesNow.AddDate(-1, 0, 0),
		Now:                  testPrediccionesNow,
	})
	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPrediccionesNow}, nil)

	pred, err := svc.ObtenerPredicciones(context.Background(), 200)
	require.NoError(t, err)
	assert.False(t, pred.Disponible, "zero FechaPrimerVenta must degrade to Disponible=false")
}

// TestObtenerPredicciones_VentasMesesDistintosZero verifies that a candidato
// with VentasMesesDistintos < 1 degrades to Disponible=false without error.
func TestObtenerPredicciones_VentasMesesDistintosZero(t *testing.T) {
	t.Parallel()

	primerVenta := testPrediccionesNow.AddDate(-1, 0, 0)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            300,
		Nombre:               "Test Meses Cero",
		Zona:                 "Z1",
		Telefono:             "000",
		FechaUltimaCompra:    testPrediccionesNow.AddDate(0, -1, 0),
		FechaPrimerVenta:     primerVenta,
		FechaUltimaVenta:     primerVenta,
		VentasMesesDistintos: 0,
		Monetary:             decimal.NewFromInt(5_000),
		MonetaryVProm:        decimal.NewFromInt(5_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         primerVenta,
		Now:                  testPrediccionesNow,
	})
	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPrediccionesNow}, nil)

	pred, err := svc.ObtenerPredicciones(context.Background(), 300)
	require.NoError(t, err)
	assert.False(t, pred.Disponible, "VentasMesesDistintos<1 must degrade to Disponible=false")
}

// errorableRepo wraps fakeWinbackRepo and overrides GetCandidato to simulate
// a non-not-found repository failure.
type errorableRepo struct {
	*fakeWinbackRepo
	errGet error
}

func (r *errorableRepo) GetCandidato(_ context.Context, _ int) (*domain.WinbackCandidato, error) {
	return nil, r.errGet
}

// TestObtenerPredicciones_RepoError verifies that a non-not-found repo error
// is propagated as a wrapped internal error with the expected code.
func TestObtenerPredicciones_RepoError(t *testing.T) {
	t.Parallel()

	syntheticErr := errors.New("synthetic db failure")
	repo := &errorableRepo{fakeWinbackRepo: newFakeWinbackRepo(), errGet: syntheticErr}
	svc := app.NewService(repo, nil, fixedClock{testPrediccionesNow}, nil)

	_, err := svc.ObtenerPredicciones(context.Background(), 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, "predicciones_cliente_failed")
}
