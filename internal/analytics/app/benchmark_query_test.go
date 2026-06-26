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

// testBenchmarkNow is the fixed reference time for benchmark tests.
var testBenchmarkNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// makeBenchmarkCandidato builds a WinbackCandidato for benchmark tests.
// pctPagosATiempo controls the puntualidad metric directly (0 → no aplica).
func makeBenchmarkCandidato(id int, zona string, pctPagosATiempo float64) *domain.WinbackCandidato {
	primerVenta := testBenchmarkNow.AddDate(-2, 0, 0)
	return mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            id,
		Nombre:               "Test Candidato",
		Zona:                 zona,
		Telefono:             "3312345678",
		FechaUltimaCompra:    testBenchmarkNow.AddDate(0, -3, 0),
		FechaPrimerVenta:     primerVenta,
		FechaUltimaVenta:     testBenchmarkNow.AddDate(0, -3, 0),
		VentasMesesDistintos: 1,
		MonetaryVProm:        decimal.NewFromInt(5_000),
		Frecuencia:           1,
		Monetary:             decimal.NewFromInt(5_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		PctPagosATiempo:      decimal.NewFromFloat(pctPagosATiempo),
		NumPagos:             3,
		CadenciaDias:         30,
		CohorteFecha:         primerVenta,
		Now:                  testBenchmarkNow,
	})
}

// TestObtenerBenchmark_PuntualidadPercentilExacto verifies that the percentil
// for puntualidad is computed exactly for a controlled cohort of 36 candidates.
func TestObtenerBenchmark_PuntualidadPercentilExacto(t *testing.T) {
	t.Parallel()

	const zona = "NORTE"
	// Build 35 peers with evenly spaced puntualidad: 1%, 2%, ..., 35%.
	// Plus 1 target with puntualidad = 18%.
	// Target's rank: 18 values ≤ 18 out of 35 peers → percentil = 18/35*100 ≈ 51.43%.
	candidates := make([]*domain.WinbackCandidato, 0, 36)
	for i := 1; i <= 35; i++ {
		candidates = append(candidates, makeBenchmarkCandidato(i, zona, float64(i)))
	}
	const targetID = 100
	target := makeBenchmarkCandidato(targetID, zona, 18.0)
	candidates = append(candidates, target)

	repo := newFakeWinbackRepo()
	repo.candidates = candidates
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	result, err := svc.ObtenerBenchmark(context.Background(), targetID, "zona")
	require.NoError(t, err)
	assert.True(t, result.Disponible)
	assert.Equal(t, zona, result.Zona)
	assert.Equal(t, "zona", result.CohortBy)
	assert.Equal(t, 35, result.N)

	assert.True(t, result.Puntualidad.Aplica, "puntualidad debe aplicar para target con pct > 0")
	assert.False(t, result.Puntualidad.MuestraPequena, "35 peers >= benchmarkMuestraMinima")

	// 18 values ≤ 18 out of 35 peers → 18/35 * 100 = 51.428...
	expectedPct := 18.0 / 35.0 * 100.0
	assert.InDelta(t, expectedPct, result.Puntualidad.Percentil, 0.01, "percentil exacto puntualidad")
	assert.InDelta(t, 18.0, result.Puntualidad.Valor, 0.001)
}

// TestObtenerBenchmark_MuestraPequena verifies that a cohort < 30 returns MuestraPequena=true.
func TestObtenerBenchmark_MuestraPequena(t *testing.T) {
	t.Parallel()

	const zona = "ZONA_CHICA"
	candidates := make([]*domain.WinbackCandidato, 0, 10)
	for i := 1; i <= 9; i++ {
		candidates = append(candidates, makeBenchmarkCandidato(i, zona, float64(i*10)))
	}
	const targetID = 50
	candidates = append(candidates, makeBenchmarkCandidato(targetID, zona, 50.0))

	repo := newFakeWinbackRepo()
	repo.candidates = candidates
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	result, err := svc.ObtenerBenchmark(context.Background(), targetID, "zona")
	require.NoError(t, err)
	assert.True(t, result.Disponible)
	assert.Equal(t, 9, result.N)

	// puntualidad applies (pct=50 > 0) but N=9 < benchmarkMuestraMinima=30
	assert.True(t, result.Puntualidad.Aplica)
	assert.True(t, result.Puntualidad.MuestraPequena, "N=9 < 30 → MuestraPequena")
	assert.InDelta(t, 0.0, result.Puntualidad.Percentil, 1e-9, "percentil must be 0 when muestra pequeña")
	assert.InDelta(t, 0.0, result.Puntualidad.Mediana, 1e-9, "mediana must be 0 when muestra pequeña")
	assert.InDelta(t, 0.0, result.Puntualidad.P25, 1e-9, "p25 must be 0 when muestra pequeña")
	assert.InDelta(t, 0.0, result.Puntualidad.P75, 1e-9, "p75 must be 0 when muestra pequeña")
}

// TestObtenerBenchmark_SegmentoSubFiltro verifies that cohort_by=segmento reduces N.
func TestObtenerBenchmark_SegmentoSubFiltro(t *testing.T) {
	t.Parallel()

	const zona = "CENTRO"
	now := testBenchmarkNow
	candidates := make([]*domain.WinbackCandidato, 0)

	// 20 candidates with last purchase 400 days ago (lapsed)
	// 15 candidates with last purchase 100 days ago (active)
	// target: 400 days ago (same as lapsed group)
	for i := 1; i <= 20; i++ {
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         i,
			Nombre:            "Dormido",
			Zona:              zona,
			FechaUltimaCompra: now.AddDate(0, 0, -400),
			Frecuencia:        4,
			Monetary:          decimal.NewFromInt(5_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-3, 0, 0),
			Now:               now,
		})
		candidates = append(candidates, c)
	}
	for i := 21; i <= 35; i++ {
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         i,
			Nombre:            "Activo",
			Zona:              zona,
			FechaUltimaCompra: now.AddDate(0, 0, -100),
			Frecuencia:        5,
			Monetary:          decimal.NewFromInt(10_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-3, 0, 0),
			Now:               now,
		})
		candidates = append(candidates, c)
	}
	const targetID = 200
	target := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         targetID,
		Nombre:            "Target Dormido",
		Zona:              zona,
		FechaUltimaCompra: now.AddDate(0, 0, -400),
		Frecuencia:        4,
		Monetary:          decimal.NewFromInt(5_000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      now.AddDate(-3, 0, 0),
		Now:               now,
	})
	candidates = append(candidates, target)

	repo := newFakeWinbackRepo()
	repo.candidates = candidates
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	// zona benchmark: N = 35 (all candidates minus target)
	zonaResult, err := svc.ObtenerBenchmark(context.Background(), targetID, "zona")
	require.NoError(t, err)
	assert.Equal(t, 35, zonaResult.N)

	// segmento benchmark: N should be less than 35 (only lapsed segment)
	segResult, err := svc.ObtenerBenchmark(context.Background(), targetID, "segmento")
	require.NoError(t, err)
	assert.True(t, segResult.Disponible)
	assert.Equal(t, "segmento", segResult.CohortBy)
	assert.Less(t, segResult.N, zonaResult.N, "segmento filter must reduce N vs zona")
}

// TestObtenerBenchmark_AntiguedadSubFiltro verifies that cohort_by=antiguedad reduces N.
func TestObtenerBenchmark_AntiguedadSubFiltro(t *testing.T) {
	t.Parallel()

	const zona = "OESTE"
	now := testBenchmarkNow
	candidates := make([]*domain.WinbackCandidato, 0)

	// 20 candidates with FechaPrimerVenta ~2 years ago (within ±6 months of target)
	// 15 candidates with FechaPrimerVenta ~5 years ago (outside ±6 months)
	for i := 1; i <= 20; i++ {
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         i,
			Nombre:            "Mismo Rango",
			Zona:              zona,
			FechaPrimerVenta:  now.AddDate(-2, 0, 0).AddDate(0, i%6, 0),
			FechaUltimaCompra: now.AddDate(0, -3, 0),
			Frecuencia:        2,
			Monetary:          decimal.NewFromInt(5_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-3, 0, 0),
			Now:               now,
		})
		candidates = append(candidates, c)
	}
	for i := 21; i <= 35; i++ {
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         i,
			Nombre:            "Muy Antiguo",
			Zona:              zona,
			FechaPrimerVenta:  now.AddDate(-5, 0, 0),
			FechaUltimaCompra: now.AddDate(0, -3, 0),
			Frecuencia:        2,
			Monetary:          decimal.NewFromInt(5_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-6, 0, 0),
			Now:               now,
		})
		candidates = append(candidates, c)
	}
	const targetID = 300
	target := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         targetID,
		Nombre:            "Target",
		Zona:              zona,
		FechaPrimerVenta:  now.AddDate(-2, 0, 0),
		FechaUltimaCompra: now.AddDate(0, -3, 0),
		Frecuencia:        2,
		Monetary:          decimal.NewFromInt(5_000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      now.AddDate(-3, 0, 0),
		Now:               now,
	})
	candidates = append(candidates, target)

	repo := newFakeWinbackRepo()
	repo.candidates = candidates
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	zonaResult, err := svc.ObtenerBenchmark(context.Background(), targetID, "zona")
	require.NoError(t, err)
	assert.Equal(t, 35, zonaResult.N)

	antResult, err := svc.ObtenerBenchmark(context.Background(), targetID, "antiguedad")
	require.NoError(t, err)
	assert.True(t, antResult.Disponible)
	assert.Equal(t, "antiguedad", antResult.CohortBy)
	// Should be less than 35 — only peers within ±6 months
	assert.Less(t, antResult.N, zonaResult.N, "antiguedad filter must reduce N vs zona")
}

// TestObtenerBenchmark_TargetNotFound verifies graceful degrade for missing candidato.
func TestObtenerBenchmark_TargetNotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	result, err := svc.ObtenerBenchmark(context.Background(), 9999, "zona")
	require.NoError(t, err)
	assert.False(t, result.Disponible)
	assert.Equal(t, "zona", result.CohortBy)
}

// TestObtenerBenchmark_InvalidCohortBy verifies that invalid cohort_by defaults to "zona".
func TestObtenerBenchmark_InvalidCohortBy(t *testing.T) {
	t.Parallel()

	const zona = "NORTE"
	target := makeBenchmarkCandidato(1, zona, 50.0)
	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{target}
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	for _, invalid := range []string{"", "foo", "ZONA", "Zona"} {
		result, err := svc.ObtenerBenchmark(context.Background(), 1, invalid)
		require.NoError(t, err, "cohortBy=%q", invalid)
		assert.Equal(t, "zona", result.CohortBy, "invalid cohortBy=%q must default to 'zona'", invalid)
	}
}

// TestObtenerBenchmark_RepoError verifies that non-not-found errors are propagated.
func TestObtenerBenchmark_RepoError(t *testing.T) {
	t.Parallel()

	syntheticErr := errors.New("db failure")
	repo := &errorableRepo{fakeWinbackRepo: newFakeWinbackRepo(), errGet: syntheticErr}
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	_, err := svc.ObtenerBenchmark(context.Background(), 1, "zona")
	require.Error(t, err)
	assert.ErrorContains(t, err, "benchmark_cliente_failed")
}

// TestObtenerBenchmark_NoZona verifies that a candidato with empty zona returns Disponible=false.
func TestObtenerBenchmark_NoZona(t *testing.T) {
	t.Parallel()

	noZona := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:      1,
		Nombre:         "Sin Zona",
		Zona:           "",
		Frecuencia:     1,
		Monetary:       decimal.NewFromInt(1_000),
		Saldo:          decimal.Zero,
		PorLiquidarPct: decimal.Zero,
		CohorteFecha:   testBenchmarkNow.AddDate(-1, 0, 0),
		Now:            testBenchmarkNow,
	})
	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{noZona}
	svc := app.NewService(repo, nil, fixedClock{testBenchmarkNow}, nil)

	result, err := svc.ObtenerBenchmark(context.Background(), 1, "zona")
	require.NoError(t, err)
	assert.False(t, result.Disponible, "empty zona must return Disponible=false")
}
