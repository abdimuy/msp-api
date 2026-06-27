// Package app_test — cartera_query_test.go exercises all cartera service methods
// using hand-written in-memory fakes. No database connection required.
//
// Coverage target: ≥90% of cartera_query.go lines.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
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
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── Fake CarteraRepo ─────────────────────────────────────────────────────────

type fakeCarteraRepo struct {
	agingByZona     []outbound.AgingRow
	agingByCobrador []outbound.AgingRow
	vintageRows     []outbound.VintageRow
	ceiRows         []outbound.CEIRow
	snapshots       []domain.CarteraSnapshot

	// error injection
	agingErr    error
	vintageErr  error
	ceiErr      error
	snapshotErr error
}

func (r *fakeCarteraRepo) AgingSaldosByZona(_ context.Context, _ time.Time) ([]outbound.AgingRow, error) {
	return r.agingByZona, r.agingErr
}

func (r *fakeCarteraRepo) AgingSaldosByCobrador(_ context.Context, _ time.Time) ([]outbound.AgingRow, error) {
	if r.agingErr != nil {
		return nil, r.agingErr
	}
	return r.agingByCobrador, nil
}

func (r *fakeCarteraRepo) VintageSaldos(_ context.Context) ([]outbound.VintageRow, error) {
	return r.vintageRows, r.vintageErr
}

func (r *fakeCarteraRepo) ColeccionCEI(_ context.Context, _, _ time.Time) ([]outbound.CEIRow, error) {
	return r.ceiRows, r.ceiErr
}

func (r *fakeCarteraRepo) SaveCarteraSnapshot(_ context.Context, rows []domain.CarteraSnapshot) error {
	if r.snapshotErr != nil {
		return r.snapshotErr
	}
	r.snapshots = append(r.snapshots, rows...)
	return nil
}

func (r *fakeCarteraRepo) ListRecentSnapshots(_ context.Context, _ int) ([]domain.CarteraSnapshot, error) {
	return r.snapshots, r.snapshotErr
}

// ─── Builder helpers ──────────────────────────────────────────────────────────

// newCarteraService builds a *Service wired with both a fake winback repo and
// a fake cartera repo.
func newCarteraService(wb *fakeWinbackRepo, cartera *fakeCarteraRepo) *app.Service {
	svc := app.NewService(wb, nil, fixedClock{testNow}, nil)
	svc.WithCarteraRepo(cartera)
	return svc
}

// newZonaInt is a helper to get a *int for zone IDs.
func newZonaInt(v int) *int { return &v }

// intPtr returns a *int to val.
func intPtr(v int) *int { return &v }

// ─── ObtenerSaludCartera ─────────────────────────────────────────────────────

// TestObtenerSaludCartera_PAR_Computed verifies that PAR is computed correctly
// from a known aging distribution.
//
// Portfolio:
//
//	0-30 bucket: saldo=600, 6 accounts  (current)
//	31-60 bucket: saldo=300, 3 accounts (moroso)
//	90+   bucket: saldo=100, 1 account  (moroso)
//
// saldoTotal=1000, saldoMoroso=400 → PAR=0.40.
func TestObtenerSaludCartera_PAR_Computed(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(600), Conteo: 6},
			{ZonaClienteID: 1, Bucket: "31-60", Saldo: decimal.NewFromInt(300), Conteo: 3},
			{ZonaClienteID: 1, Bucket: "90+", Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(200)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	result, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	// saldoTotal=1000, saldoMoroso=400
	assert.True(t, result.SaldoTotal.Equal(decimal.NewFromInt(1000)),
		"SaldoTotal should be 1000, got %s", result.SaldoTotal)
	assert.True(t, result.SaldoMoroso.Equal(decimal.NewFromInt(400)),
		"SaldoMoroso should be 400, got %s", result.SaldoMoroso)

	// PAR = 400/1000 = 0.40
	expected := decimal.NewFromFloat(0.40)
	assert.True(t, result.PAR.Equal(expected),
		"PAR should be 0.40, got %s", result.PAR)

	// CuentasTotal=10, CuentasEnMora=4
	assert.Equal(t, 10, result.CuentasTotal)
	assert.Equal(t, 4, result.CuentasEnMora)

	// ImporteColectado=200
	assert.True(t, result.ImporteColectado.Equal(decimal.NewFromInt(200)))

	// CEIRate = 200/1000 = 0.20
	expectedCEI := decimal.NewFromFloat(0.20)
	assert.True(t, result.CEIRate.Equal(expectedCEI),
		"CEIRate should be 0.20, got %s", result.CEIRate)
}

// TestObtenerSaludCartera_MargenReal verifies the MargenReal proxy formula.
//
// Inputs: ventas=200, PAR=0.40, saldoTotal=1000
//
// MargenBruto     = 0.528 × 200 = 105.60
// PerdidaEsperada = 0.40 × 1000 × 0.70 = 280
// MargenReal      = 105.60 − 280 = floored to 0 (negative).
func TestObtenerSaludCartera_MargenReal_Negative_Floored(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(600), Conteo: 6},
			{ZonaClienteID: 1, Bucket: "31-60", Saldo: decimal.NewFromInt(400), Conteo: 4},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(200)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	result, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	// PAR = 400/1000 = 0.40
	// MargenBruto = 0.528 * 200 = 105.60
	// PerdidaEsperada = 0.40 * 1000 * 0.70 = 280
	// MargenReal = 105.60 - 280 < 0 → floored to 0
	assert.True(t, result.MargenRealProxy.Equal(decimal.Zero),
		"MargenRealProxy should be floored to 0, got %s", result.MargenRealProxy)
}

// TestObtenerSaludCartera_MargenReal_Positive verifies MargenReal when positive.
//
// Inputs: ventas=10000, PAR=0.05, saldoTotal=1000
//
// MargenBruto     = 0.528 × 10000 = 5280
// PerdidaEsperada = 0.05 × 1000 × 0.70 = 35
// MargenReal      = 5280 − 35 = 5245.
func TestObtenerSaludCartera_MargenReal_Positive(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(950), Conteo: 9},
			{ZonaClienteID: 1, Bucket: "31-60", Saldo: decimal.NewFromInt(50), Conteo: 1},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(10_000)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	result, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	// PAR = 50/1000 = 0.05
	// MargenBruto = 0.528 * 10000 = 5280
	// PerdidaEsperada = 0.05 * 1000 * 0.70 = 35
	// MargenReal = 5280 - 35 = 5245
	expected := decimal.NewFromFloat(5245)
	diff := result.MargenRealProxy.Sub(expected).Abs()
	assert.True(t, diff.LessThan(decimal.NewFromFloat(0.01)),
		"MargenRealProxy should be ~5245, got %s", result.MargenRealProxy)
}

// TestObtenerSaludCartera_Empty verifies graceful degradation on empty data.
func TestObtenerSaludCartera_Empty(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{} // empty data
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	assert.True(t, result.SaldoTotal.Equal(decimal.Zero))
	assert.True(t, result.SaldoMoroso.Equal(decimal.Zero))
	assert.True(t, result.PAR.Equal(decimal.Zero))
	assert.True(t, result.CEIRate.Equal(decimal.Zero))
	assert.Equal(t, 0, result.CuentasTotal)
	assert.Equal(t, 0, result.CuentasEnMora)
	assert.True(t, result.MargenRealProxy.Equal(decimal.Zero))
}

// TestObtenerSaludCartera_ZonaFilter verifies that ZonaClienteID filter isolates
// the correct zone.
func TestObtenerSaludCartera_ZonaFilter(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(500), Conteo: 5},
			{ZonaClienteID: 2, Bucket: "90+", Saldo: decimal.NewFromInt(900), Conteo: 9},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(100)},
			{ZonaClienteID: 2, Importe: decimal.NewFromInt(200)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	zonaID := 1
	result, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{ZonaClienteID: &zonaID})
	require.NoError(t, err)

	// Only zona 1: saldoTotal=500, saldoMoroso=0, PAR=0
	assert.True(t, result.SaldoTotal.Equal(decimal.NewFromInt(500)))
	assert.True(t, result.SaldoMoroso.Equal(decimal.Zero))
	assert.True(t, result.PAR.Equal(decimal.Zero))
	assert.True(t, result.ImporteColectado.Equal(decimal.NewFromInt(100)))
}

// TestObtenerSaludCartera_AgingError verifies error propagation from the repo.
func TestObtenerSaludCartera_AgingError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{agingErr: errors.New("db error")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerSaludCartera_CEIError verifies error propagation when CEI fails.
func TestObtenerSaludCartera_CEIError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
		ceiErr: errors.New("cei db error"),
	}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerSaludCartera_NilRepo verifies that a nil carteraRepo returns an error.
func TestObtenerSaludCartera_NilRepo(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	// CarteraRepo NOT configured.

	_, err := svc.ObtenerSaludCartera(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// ─── ObtenerAging ─────────────────────────────────────────────────────────────

// TestObtenerAging_Distribution verifies that the four canonical buckets are
// returned with correct aggregated saldo, conteo, and pct.
func TestObtenerAging_Distribution(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			// Zone 1
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(400), Conteo: 4},
			{ZonaClienteID: 1, Bucket: "31-60", Saldo: decimal.NewFromInt(200), Conteo: 2},
			// Zone 2 — same buckets, should be merged
			{ZonaClienteID: 2, Bucket: "0-30", Saldo: decimal.NewFromInt(100), Conteo: 1},
			{ZonaClienteID: 2, Bucket: "90+", Saldo: decimal.NewFromInt(300), Conteo: 3},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	buckets, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, buckets, 4, "must always return 4 buckets")

	// Build map for easy lookup.
	byBucket := make(map[string]interface{})
	for _, b := range buckets {
		byBucket[b.Bucket] = b
	}

	// 0-30: saldo=500, conteo=5, pct=500/1000=0.5
	b030 := buckets[0]
	assert.Equal(t, "0-30", b030.Bucket)
	assert.True(t, b030.Saldo.Equal(decimal.NewFromInt(500)), "0-30 saldo")
	assert.Equal(t, 5, b030.Conteo, "0-30 conteo")
	assert.True(t, b030.PctSaldo.Equal(decimal.NewFromFloat(0.5)), "0-30 pct")

	// 31-60: saldo=200, conteo=2, pct=0.2
	b3160 := buckets[1]
	assert.Equal(t, "31-60", b3160.Bucket)
	assert.True(t, b3160.Saldo.Equal(decimal.NewFromInt(200)))

	// 61-90: saldo=0, conteo=0
	b6190 := buckets[2]
	assert.Equal(t, "61-90", b6190.Bucket)
	assert.True(t, b6190.Saldo.Equal(decimal.Zero))
	assert.Equal(t, 0, b6190.Conteo)

	// 90+: saldo=300, conteo=3
	b90p := buckets[3]
	assert.Equal(t, "90+", b90p.Bucket)
	assert.True(t, b90p.Saldo.Equal(decimal.NewFromInt(300)))
}

// TestObtenerAging_ZonaFilter verifies that ZonaClienteID filter is applied.
func TestObtenerAging_ZonaFilter(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(400), Conteo: 4},
			{ZonaClienteID: 2, Bucket: "0-30", Saldo: decimal.NewFromInt(600), Conteo: 6},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	zonaID := 1
	buckets, err := svc.ObtenerAging(context.Background(), app.CarteraParams{ZonaClienteID: &zonaID})
	require.NoError(t, err)
	require.Len(t, buckets, 4)

	// Only zona 1 in 0-30: saldo=400
	assert.True(t, buckets[0].Saldo.Equal(decimal.NewFromInt(400)), "zona filter: 0-30 saldo")
}

// TestObtenerAging_ByCobradorFilter verifies that CobradorID filter uses
// AgingSaldosByCobrador and filters the result.
func TestObtenerAging_ByCobradorFilter(t *testing.T) {
	t.Parallel()

	cobID := 10
	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(10), Bucket: "0-30", Saldo: decimal.NewFromInt(300), Conteo: 3},
			{ZonaClienteID: 1, CobradorID: intPtr(20), Bucket: "0-30", Saldo: decimal.NewFromInt(700), Conteo: 7},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	buckets, err := svc.ObtenerAging(context.Background(), app.CarteraParams{CobradorID: &cobID})
	require.NoError(t, err)
	require.Len(t, buckets, 4)

	// Only cobrador 10: 0-30 saldo=300
	assert.True(t, buckets[0].Saldo.Equal(decimal.NewFromInt(300)))
}

// TestObtenerAging_Empty verifies zero totals and zero pct on empty data.
func TestObtenerAging_Empty(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	buckets, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, buckets, 4)

	for _, b := range buckets {
		assert.True(t, b.Saldo.Equal(decimal.Zero), "empty: %s saldo should be 0", b.Bucket)
		assert.Equal(t, 0, b.Conteo)
		assert.True(t, b.PctSaldo.Equal(decimal.Zero))
	}
}

// TestObtenerAging_NilRepo verifies error on unconfigured cartera repo.
func TestObtenerAging_NilRepo(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	_, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerAging_RepoError propagates aging repo errors.
func TestObtenerAging_RepoError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{agingErr: errors.New("db fail")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// ─── ObtenerCosechas ─────────────────────────────────────────────────────────

// TestObtenerCosechas_AgeMonths verifies cohort age computation and ordering.
//
// testNow = 2026-06-13
// Cohort 2026*12+6=24318 → age=0 months
// Cohort 2025*12+6=24306 → age=12 months
// Expected order: most recent first (24318 before 24306).
func TestObtenerCosechas_AgeMonths(t *testing.T) {
	t.Parallel()

	// testNow is 2026-06-13 → nowCohort = 2026*12+6 = 24318
	cohortRecent := 2026*12 + 6 // = 24318, age=0
	cohortOld := 2025*12 + 6    // = 24306, age=12

	cartera := &fakeCarteraRepo{
		vintageRows: []outbound.VintageRow{
			{ZonaClienteID: 1, CohortMonth: cohortOld, Saldo: decimal.NewFromInt(200), Conteo: 2},
			{ZonaClienteID: 1, CohortMonth: cohortRecent, Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	cosechas, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, cosechas, 2)

	// Most recent first.
	assert.Equal(t, cohortRecent, cosechas[0].CohortMonth)
	assert.Equal(t, 0, cosechas[0].AgeMonths, "recent cohort should have 0 age months")
	assert.Equal(t, cohortOld, cosechas[1].CohortMonth)
	assert.Equal(t, 12, cosechas[1].AgeMonths, "old cohort should be 12 months old")
}

// TestObtenerCosechas_ZonaFilter verifies zone filtering on vintage rows.
func TestObtenerCosechas_ZonaFilter(t *testing.T) {
	t.Parallel()

	cohort := 2026*12 + 3
	cartera := &fakeCarteraRepo{
		vintageRows: []outbound.VintageRow{
			{ZonaClienteID: 1, CohortMonth: cohort, Saldo: decimal.NewFromInt(500), Conteo: 5},
			{ZonaClienteID: 2, CohortMonth: cohort, Saldo: decimal.NewFromInt(1000), Conteo: 10},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	zonaID := 1
	cosechas, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{ZonaClienteID: &zonaID})
	require.NoError(t, err)
	require.Len(t, cosechas, 1)
	assert.True(t, cosechas[0].Saldo.Equal(decimal.NewFromInt(500)))
}

// TestObtenerCosechas_MultiZoneMerge verifies that rows from multiple zones
// for the same cohort month are merged into a single CosechaContract.
func TestObtenerCosechas_MultiZoneMerge(t *testing.T) {
	t.Parallel()

	cohort := 2026*12 + 1
	cartera := &fakeCarteraRepo{
		vintageRows: []outbound.VintageRow{
			{ZonaClienteID: 1, CohortMonth: cohort, Saldo: decimal.NewFromInt(300), Conteo: 3},
			{ZonaClienteID: 2, CohortMonth: cohort, Saldo: decimal.NewFromInt(200), Conteo: 2},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	cosechas, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, cosechas, 1, "both zones same month should be merged")
	assert.True(t, cosechas[0].Saldo.Equal(decimal.NewFromInt(500)), "merged saldo")
	assert.Equal(t, 5, cosechas[0].Conteo, "merged conteo")
}

// TestObtenerCosechas_Empty graceful empty.
func TestObtenerCosechas_Empty(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	cosechas, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	assert.Empty(t, cosechas)
}

// TestObtenerCosechas_NilRepo error on nil repo.
func TestObtenerCosechas_NilRepo(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	_, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerCosechas_RepoError propagates vintage error.
func TestObtenerCosechas_RepoError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{vintageErr: errors.New("vintage fail")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerCosechas(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// ─── ObtenerRankingCobradores ─────────────────────────────────────────────────

// TestObtenerRankingCobradores_PerCobrador verifies per-cobrador metrics.
//
// Cobrador 10: portfolio saldo=1000, moroso=0 (all 0-30), CEI=200/1000=0.20
// Cobrador 20: portfolio saldo=500, moroso=500 (all 90+), CEI=50/500=0.10.
func TestObtenerRankingCobradores_PerCobrador(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(10), Bucket: "0-30", Saldo: decimal.NewFromInt(1000), Conteo: 10},
			{ZonaClienteID: 1, CobradorID: intPtr(20), Bucket: "90+", Saldo: decimal.NewFromInt(500), Conteo: 5},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, CobradorID: intPtr(10), Importe: decimal.NewFromInt(200)},
			{ZonaClienteID: 1, CobradorID: intPtr(20), Importe: decimal.NewFromInt(50)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, ranking, 2)

	// Sorted by CEI DESC: cobrador 10 first (CEI=0.20), cobrador 20 second (CEI=0.10).
	assert.Equal(t, 10, ranking[0].CobradorID)
	assert.True(t, ranking[0].PAR.Equal(decimal.Zero), "cob10 PAR should be 0 (no moroso)")
	assert.True(t, ranking[0].SaldoTotal.Equal(decimal.NewFromInt(1000)))

	assert.Equal(t, 20, ranking[1].CobradorID)
	// PAR for cobrador 20 = 500/500 = 1.0
	assert.True(t, ranking[1].PAR.Equal(decimal.NewFromInt(1)), "cob20 PAR should be 1.0")
}

// TestObtenerRankingCobradores_PctCorriente verifies PctCorriente (0-30 fraction).
func TestObtenerRankingCobradores_PctCorriente(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(5), Bucket: "0-30", Saldo: decimal.NewFromInt(80), Conteo: 8},
			{ZonaClienteID: 1, CobradorID: intPtr(5), Bucket: "90+", Saldo: decimal.NewFromInt(20), Conteo: 2},
		},
		ceiRows: nil,
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, ranking, 1)

	// 8 corriente / 10 total = 0.80
	expected := decimal.NewFromFloat(0.80)
	assert.True(t, ranking[0].PctCorriente.Equal(expected),
		"PctCorriente should be 0.80, got %s", ranking[0].PctCorriente)
}

// TestObtenerRankingCobradores_NilRepo error on nil repo.
func TestObtenerRankingCobradores_NilRepo(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	_, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerRankingCobradores_AgingError propagates error.
func TestObtenerRankingCobradores_AgingError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{agingErr: errors.New("aging fail")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerRankingCobradores_CEIError propagates CEI error.
func TestObtenerRankingCobradores_CEIError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(1), Bucket: "0-30", Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
		ceiErr: errors.New("cei fail"),
	}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerRankingCobradores_Empty graceful empty.
func TestObtenerRankingCobradores_Empty(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	assert.Empty(t, ranking)
}

// ─── ListarCuentasRiesgo ──────────────────────────────────────────────────────

// TestListarCuentasRiesgo_TierEnrichment verifies that at-risk accounts are
// enriched with the correct TierRiesgo and Segmento.
//
// cMoroso: saldo>0, FechaUltimoPago zero → MOROSO EstadoPago → CRITICO tier
// cSinSaldo: saldo=0 → excluded (not at risk).
func TestListarCuentasRiesgo_TierEnrichment(t *testing.T) {
	t.Parallel()

	// cMoroso: saldo>0, no payment history → MOROSO → CRITICO
	cMoroso := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    1,
		Nombre:       "Moroso Uno",
		Zona:         "Z1",
		Frecuencia:   3,
		Monetary:     decimal.NewFromInt(10_000),
		Saldo:        decimal.NewFromInt(5_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
		// FechaUltimoPago zero + saldo > 0 → MOROSO
	})

	// cSinSaldo: saldo=0 → should be excluded from cuentas riesgo
	cSinSaldo := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    2,
		Nombre:       "Sin Saldo",
		Zona:         "Z1",
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.Zero,
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{cMoroso, cSinSaldo}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	// Only cMoroso is at risk.
	require.Len(t, cuentas, 1, "zero-saldo accounts must be excluded")
	assert.Equal(t, 1, cuentas[0].ClienteID)
	assert.Equal(t, "CRITICO", cuentas[0].TierRiesgo, "no cadencia + moroso → CRITICO")
}

// TestListarCuentasRiesgo_Ordering verifies that CRITICO accounts sort before
// EN_RIESGO and VIGILANCIA.
func TestListarCuentasRiesgo_Ordering(t *testing.T) {
	t.Parallel()

	// cVigilancia: saldo>0, paid within 2×cadencia → VIGILANCIA
	// cadencia=30, días sin pagar=35 (1×cadencia < 35 ≤ 2×cadencia)
	lastPago := testNow.AddDate(0, 0, -35)
	cVigilancia := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       2,
		Nombre:          "Vigilancia",
		Zona:            "Z1",
		Frecuencia:      5,
		Monetary:        decimal.NewFromInt(20_000),
		Saldo:           decimal.NewFromInt(3_000),
		CohorteFecha:    testNow.AddDate(-2, 0, 0),
		Now:             testNow,
		FechaUltimoPago: lastPago,
		CadenciaDias:    30,
	})

	// cCritico: saldo>0, days=120 > 3×cadencia=90 → CRITICO
	lastPagoCritico := testNow.AddDate(0, 0, -120)
	cCritico := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       3,
		Nombre:          "Critico",
		Zona:            "Z1",
		Frecuencia:      5,
		Monetary:        decimal.NewFromInt(30_000),
		Saldo:           decimal.NewFromInt(8_000),
		CohorteFecha:    testNow.AddDate(-2, 0, 0),
		Now:             testNow,
		FechaUltimoPago: lastPagoCritico,
		CadenciaDias:    30,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{cVigilancia, cCritico}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, cuentas, 2)

	// CRITICO should be first.
	assert.Equal(t, "CRITICO", cuentas[0].TierRiesgo, "CRITICO should sort first")
	assert.Equal(t, 3, cuentas[0].ClienteID)
	assert.Equal(t, "VIGILANCIA", cuentas[1].TierRiesgo)
	assert.Equal(t, 2, cuentas[1].ClienteID)
}

// TestListarCuentasRiesgo_SaldoTieBreak verifies Saldo DESC tie-break within tier.
func TestListarCuentasRiesgo_SaldoTieBreak(t *testing.T) {
	t.Parallel()

	// Two CRITICO clients — different saldo, same tier.
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    1,
		Frecuencia:   3,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.NewFromInt(1_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
		// no FechaUltimoPago → MOROSO → CRITICO
	})
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    2,
		Frecuencia:   3,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.NewFromInt(9_000), // higher saldo → sorts first
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{c1, c2}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, cuentas, 2)

	// c2 (saldo=9000) should be first.
	assert.Equal(t, 2, cuentas[0].ClienteID, "higher saldo should sort first within same tier")
}

// TestListarCuentasRiesgo_ZonaFilter verifies Zona string filter.
func TestListarCuentasRiesgo_ZonaFilter(t *testing.T) {
	t.Parallel()

	cNorte := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    1,
		Zona:         "NORTE",
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.NewFromInt(2_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})
	cSur := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    2,
		Zona:         "SUR",
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.NewFromInt(1_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{cNorte, cSur}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{Zona: "NORTE"})
	require.NoError(t, err)
	require.Len(t, cuentas, 1)
	assert.Equal(t, 1, cuentas[0].ClienteID)
}

// TestListarCuentasRiesgo_WinbackError propagates repo error.
func TestListarCuentasRiesgo_WinbackError(t *testing.T) {
	t.Parallel()

	wb := &erroringRepo{
		fakeWinbackRepo: newFakeWinbackRepo(),
	}
	// Inject a list error by using a custom fake — simplest: inject direct.
	// We use a noopWinbackRepo that errors.
	errRepo := &errWinbackListRepo{}
	svc := app.NewService(errRepo, nil, fixedClock{testNow}, nil)
	svc.WithCarteraRepo(&fakeCarteraRepo{})

	_, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.Error(t, err)
	_ = wb // suppress unused lint
}

// ─── ObtenerCumplimiento ──────────────────────────────────────────────────────

// TestObtenerCumplimiento_Distribution verifies the compliance distribution.
//
// cAlCorriente: saldo>0, prox pago in the future → AL_CORRIENTE
// cVencidoLeve: saldo>0, prox pago 15 days ago → VENCIDO_LEVE
// cVencido:     saldo>0, prox pago 60 days ago → VENCIDO
// cSinSaldo:    saldo=0 → excluded from distribution.
func TestObtenerCumplimiento_Distribution(t *testing.T) {
	t.Parallel()

	proxFuturo := testNow.AddDate(0, 0, 15)   // future
	proxLeve := testNow.AddDate(0, 0, -15)    // 15 days past
	proxVencido := testNow.AddDate(0, 0, -60) // 60 days past

	cAlCorriente := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:     1,
		Frecuencia:    2,
		Monetary:      decimal.NewFromInt(5_000),
		Saldo:         decimal.NewFromInt(1_000),
		FechaProxPago: proxFuturo,
		CohorteFecha:  testNow.AddDate(-1, 0, 0),
		Now:           testNow,
	})
	cVencidoLeve := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:     2,
		Frecuencia:    2,
		Monetary:      decimal.NewFromInt(5_000),
		Saldo:         decimal.NewFromInt(1_500),
		FechaProxPago: proxLeve,
		CohorteFecha:  testNow.AddDate(-1, 0, 0),
		Now:           testNow,
	})
	cVencido := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:     3,
		Frecuencia:    2,
		Monetary:      decimal.NewFromInt(5_000),
		Saldo:         decimal.NewFromInt(2_000),
		FechaProxPago: proxVencido,
		CohorteFecha:  testNow.AddDate(-1, 0, 0),
		Now:           testNow,
	})
	cSinSaldo := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    4,
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(1_000),
		Saldo:        decimal.Zero,
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{cAlCorriente, cVencidoLeve, cVencido, cSinSaldo}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	dist, err := svc.ObtenerCumplimiento(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	assert.Equal(t, 1, dist.AlCorriente, "1 account al corriente")
	assert.Equal(t, 1, dist.VencidoLeve, "1 account vencido leve (15 days)")
	assert.Equal(t, 1, dist.Vencido, "1 account vencido (60 days)")
	assert.Equal(t, 3, dist.Total, "zero-saldo excluded; total=3")
}

// TestObtenerCumplimiento_SaldoCeroExcluded verifies that zero-balance clients
// are not counted in the compliance distribution.
func TestObtenerCumplimiento_SaldoCeroExcluded(t *testing.T) {
	t.Parallel()

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    1,
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.Zero, // no balance → excluded
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{c}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	dist, err := svc.ObtenerCumplimiento(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	assert.Equal(t, 0, dist.Total, "zero-balance client must not count")
}

// TestObtenerCumplimiento_Empty verifies zero totals on empty repo.
func TestObtenerCumplimiento_Empty(t *testing.T) {
	t.Parallel()

	svc := newCarteraService(newFakeWinbackRepo(), &fakeCarteraRepo{})
	dist, err := svc.ObtenerCumplimiento(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	assert.Equal(t, 0, dist.Total)
}

// TestObtenerCumplimiento_WinbackError propagates winback repo error.
func TestObtenerCumplimiento_WinbackError(t *testing.T) {
	t.Parallel()

	errRepo := &errWinbackListRepo{}
	svc := app.NewService(errRepo, nil, fixedClock{testNow}, nil)
	svc.WithCarteraRepo(&fakeCarteraRepo{})

	_, err := svc.ObtenerCumplimiento(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestObtenerCumplimiento_ZonaFilter verifies that zona filter passes through.
func TestObtenerCumplimiento_ZonaFilter(t *testing.T) {
	t.Parallel()

	cNorte := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:     1,
		Zona:          "NORTE",
		Frecuencia:    2,
		Monetary:      decimal.NewFromInt(5_000),
		Saldo:         decimal.NewFromInt(1_000),
		FechaProxPago: testNow.AddDate(0, 0, -60), // vencido
		CohorteFecha:  testNow.AddDate(-1, 0, 0),
		Now:           testNow,
	})
	cSur := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    2,
		Zona:         "SUR",
		Frecuencia:   2,
		Monetary:     decimal.NewFromInt(5_000),
		Saldo:        decimal.NewFromInt(2_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0),
		Now:          testNow,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{cNorte, cSur}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	dist, err := svc.ObtenerCumplimiento(context.Background(), app.CarteraParams{Zona: "NORTE"})
	require.NoError(t, err)
	assert.Equal(t, 1, dist.Total, "only NORTE account should count")
	assert.Equal(t, 1, dist.Vencido, "NORTE account is vencido (60 days past)")
}

// ─── MargenReal ───────────────────────────────────────────────────────────────

// TestMargenReal_Formula verifies the exact MargenReal computation.
//
// Inputs: ventas=10000, PAR=0.10, saldoTotal=5000, LGD=0.70
//
// MargenBruto     = 0.528 × 10000 = 5280
// PerdidaEsperada = 0.10 × 5000 × 0.70 = 350
// MargenReal      = 5280 − 350 = 4930.
func TestMargenReal_Formula(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(4500), Conteo: 9},
			{ZonaClienteID: 1, Bucket: "90+", Saldo: decimal.NewFromInt(500), Conteo: 1},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(10_000)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	result, err := svc.MargenReal(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	// Verify formula components.
	// saldoTotal=5000, saldoMoroso=500, PAR=500/5000=0.10
	expectedPAR := decimal.NewFromFloat(0.10)
	assert.True(t, result.PAR.Equal(expectedPAR), "PAR should be 0.10, got %s", result.PAR)
	assert.True(t, result.SaldoTotal.Equal(decimal.NewFromInt(5000)))
	assert.True(t, result.LGD.Equal(decimal.NewFromFloat(0.70)))
	assert.True(t, result.Ventas.Equal(decimal.NewFromInt(10_000)))

	// MargenBruto = 0.528 × 10000 = 5280
	expectedMB := decimal.NewFromFloat(5280)
	diff := result.MargenBruto.Sub(expectedMB).Abs()
	assert.True(t, diff.LessThan(decimal.NewFromFloat(0.01)),
		"MargenBruto should be ~5280, got %s", result.MargenBruto)

	// PerdidaEsperada = 0.10 × 5000 × 0.70 = 350
	expectedPE := decimal.NewFromFloat(350)
	diffPE := result.PerdidaEsperada.Sub(expectedPE).Abs()
	assert.True(t, diffPE.LessThan(decimal.NewFromFloat(0.01)),
		"PerdidaEsperada should be ~350, got %s", result.PerdidaEsperada)

	// MargenReal = 5280 - 350 = 4930
	expectedMR := decimal.NewFromFloat(4930)
	diffMR := result.MargenReal.Sub(expectedMR).Abs()
	assert.True(t, diffMR.LessThan(decimal.NewFromFloat(0.01)),
		"MargenReal should be ~4930, got %s", result.MargenReal)
}

// TestMargenReal_Floor verifies that negative MargenReal is floored to 0.
func TestMargenReal_Floor(t *testing.T) {
	t.Parallel()

	// PAR=1.0 (100% moroso), saldoTotal=100000
	// PerdidaEsperada = 1.0 × 100000 × 0.70 = 70000
	// MargenBruto = 0.528 × 10 = 5.28
	// MargenReal = 5.28 - 70000 < 0 → floored to 0
	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "90+", Saldo: decimal.NewFromInt(100_000), Conteo: 10},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(10)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	result, err := svc.MargenReal(context.Background(), app.CarteraParams{})
	require.NoError(t, err)

	assert.True(t, result.MargenReal.Equal(decimal.Zero),
		"MargenReal must be floored at 0 when negative, got %s", result.MargenReal)
}

// TestMargenReal_ZonaFilter verifies zone filtering in MargenReal.
func TestMargenReal_ZonaFilter(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(500), Conteo: 5},
			{ZonaClienteID: 2, Bucket: "90+", Saldo: decimal.NewFromInt(2_000), Conteo: 2},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(1_000)},
			{ZonaClienteID: 2, Importe: decimal.NewFromInt(500)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	zonaID := 1
	result, err := svc.MargenReal(context.Background(), app.CarteraParams{ZonaClienteID: &zonaID})
	require.NoError(t, err)

	// Only zona 1: saldoTotal=500, no moroso → PAR=0
	assert.True(t, result.PAR.Equal(decimal.Zero))
	assert.True(t, result.Ventas.Equal(decimal.NewFromInt(1_000)))
	assert.True(t, result.SaldoTotal.Equal(decimal.NewFromInt(500)))
}

// TestMargenReal_NilRepo verifies error on nil cartera repo.
func TestMargenReal_NilRepo(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	_, err := svc.MargenReal(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestMargenReal_AgingError propagates aging error.
func TestMargenReal_AgingError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{agingErr: errors.New("aging fail")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.MargenReal(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// TestMargenReal_CEIError propagates CEI error.
func TestMargenReal_CEIError(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "0-30", Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
		ceiErr: errors.New("cei fail"),
	}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.MargenReal(context.Background(), app.CarteraParams{})
	require.Error(t, err)
}

// ─── Additional edge cases ────────────────────────────────────────────────────

// TestObtenerRankingCobradores_CobradorFilter verifies CobradorID filter in ranking.
func TestObtenerRankingCobradores_CobradorFilter(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(1), Bucket: "0-30", Saldo: decimal.NewFromInt(500), Conteo: 5},
			{ZonaClienteID: 1, CobradorID: intPtr(2), Bucket: "0-30", Saldo: decimal.NewFromInt(300), Conteo: 3},
		},
		ceiRows: nil,
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	cobID := 1
	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{CobradorID: &cobID})
	require.NoError(t, err)
	require.Len(t, ranking, 1)
	assert.Equal(t, 1, ranking[0].CobradorID)
}

// TestObtenerAging_BucketOrder verifies that the four canonical buckets are
// always returned in the correct order: 0-30, 31-60, 61-90, 90+.
func TestObtenerAging_BucketOrder(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: "90+", Saldo: decimal.NewFromInt(100), Conteo: 1},
			{ZonaClienteID: 1, Bucket: "31-60", Saldo: decimal.NewFromInt(200), Conteo: 2},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	buckets, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, buckets, 4)

	want := []string{"0-30", "31-60", "61-90", "90+"}
	for i, b := range buckets {
		assert.Equal(t, want[i], b.Bucket, "bucket at index %d", i)
	}
}

// TestWithCarteraRepo_Chaining verifies that WithCarteraRepo returns *Service
// for method chaining (tests the fluent API).
func TestWithCarteraRepo_Chaining(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{}
	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithCarteraRepo(cartera)

	// Verify the repo was set by calling a method that uses it.
	_, err := svc.ObtenerAging(context.Background(), app.CarteraParams{})
	require.NoError(t, err, "cartera repo should be configured after chaining")
}

// ─── Error repo helper ────────────────────────────────────────────────────────

// errWinbackListRepo is a minimal WinbackRepo that always errors on ListCandidatos.
type errWinbackListRepo struct{}

func (r *errWinbackListRepo) ListCandidatos(_ context.Context, _ outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	return outbound.Page[*domain.WinbackCandidato]{}, errors.New("list failed")
}

func (r *errWinbackListRepo) UpsertCandidatos(_ context.Context, _ []*domain.WinbackCandidato) error {
	return nil
}

func (r *errWinbackListRepo) GetRefreshState(_ context.Context, _ string) (outbound.RefreshState, error) {
	return outbound.RefreshState{}, nil
}

func (r *errWinbackListRepo) SaveRefreshState(_ context.Context, _ outbound.RefreshState) error {
	return nil
}

func (r *errWinbackListRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	return map[int]bool{}, nil
}

func (r *errWinbackListRepo) GetCandidato(_ context.Context, _ int) (*domain.WinbackCandidato, error) {
	return nil, domain.ErrWinbackCandidatoNotFound
}

func (r *errWinbackListRepo) ListCandidatosByClienteIDs(_ context.Context, _ []int) ([]*domain.WinbackCandidato, error) {
	return nil, nil
}

func (r *errWinbackListRepo) ListCandidatosByZona(_ context.Context, _ string) ([]*domain.WinbackCandidato, error) {
	return nil, nil
}

func (r *errWinbackListRepo) ContarPagosRecientes(_ context.Context, _ []int, _, _ time.Time) (map[int]int, error) {
	return map[int]int{}, nil
}

// Verify *errWinbackListRepo satisfies outbound.WinbackRepo.
var _ outbound.WinbackRepo = (*errWinbackListRepo)(nil)

// TestObtenerRankingCobradores_CEIOnlyEntry verifies that a cobrador who appears
// only in CEI rows (collected but portfolio fully paid) is included in ranking.
func TestObtenerRankingCobradores_CEIOnlyEntry(t *testing.T) {
	t.Parallel()

	// Cobrador 99 has no aging rows (fully paid portfolio) but did collect.
	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(10), Bucket: "0-30", Saldo: decimal.NewFromInt(500), Conteo: 5},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, CobradorID: intPtr(10), Importe: decimal.NewFromInt(100)},
			// cobrador 99: collected but no active aging rows
			{ZonaClienteID: 1, CobradorID: intPtr(99), Importe: decimal.NewFromInt(50)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	// Both cobrador 10 and cobrador 99 should appear.
	require.Len(t, ranking, 2)
}

// TestObtenerRankingCobradores_CEICobradorFilter verifies that filterCEIByCobrador
// correctly retains only matching CEI rows when CobradorID is set.
func TestObtenerRankingCobradores_CEICobradorFilter(t *testing.T) {
	t.Parallel()

	cobID := 7
	cartera := &fakeCarteraRepo{
		agingByCobrador: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: intPtr(7), Bucket: "0-30", Saldo: decimal.NewFromInt(300), Conteo: 3},
		},
		ceiRows: []outbound.CEIRow{
			// cobrador 7: should be included
			{ZonaClienteID: 1, CobradorID: intPtr(7), Importe: decimal.NewFromInt(100)},
			// cobrador 8: should be filtered out
			{ZonaClienteID: 1, CobradorID: intPtr(8), Importe: decimal.NewFromInt(999)},
		},
	}

	svc := newCarteraService(newFakeWinbackRepo(), cartera)
	ranking, err := svc.ObtenerRankingCobradores(context.Background(), app.CarteraParams{CobradorID: &cobID})
	require.NoError(t, err)
	require.Len(t, ranking, 1)
	assert.Equal(t, 7, ranking[0].CobradorID)
	// ImporteColectado should be 100 (cobrador 8's 999 filtered out)
	assert.True(t, ranking[0].ImporteColectado.Equal(decimal.NewFromInt(100)))
}

// TestListarCuentasRiesgo_Empty verifies graceful empty on empty repo.
func TestListarCuentasRiesgo_Empty(t *testing.T) {
	t.Parallel()

	svc := newCarteraService(newFakeWinbackRepo(), &fakeCarteraRepo{})
	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	assert.Empty(t, cuentas)
}

// TestListarCuentasRiesgo_EnRiesgo verifies EN_RIESGO tier classification.
// cadencia=30, días sin pagar=75 → 2×30 < 75 ≤ 3×30 → EN_RIESGO.
func TestListarCuentasRiesgo_EnRiesgo(t *testing.T) {
	t.Parallel()

	lastPago := testNow.AddDate(0, 0, -75)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       1,
		Frecuencia:      4,
		Monetary:        decimal.NewFromInt(10_000),
		Saldo:           decimal.NewFromInt(3_000),
		CohorteFecha:    testNow.AddDate(-2, 0, 0),
		Now:             testNow,
		FechaUltimoPago: lastPago,
		CadenciaDias:    30,
	})

	wb := newFakeWinbackRepo()
	wb.candidates = []*domain.WinbackCandidato{c}
	svc := newCarteraService(wb, &fakeCarteraRepo{})

	cuentas, err := svc.ListarCuentasRiesgo(context.Background(), app.CarteraParams{})
	require.NoError(t, err)
	require.Len(t, cuentas, 1)
	assert.Equal(t, "EN_RIESGO", cuentas[0].TierRiesgo)
}

// Verify *fakeCarteraRepo satisfies outbound.CarteraRepo.
var _ outbound.CarteraRepo = (*fakeCarteraRepo)(nil)

// Verify *newZonaInt works (compile check only).
var _ = newZonaInt
