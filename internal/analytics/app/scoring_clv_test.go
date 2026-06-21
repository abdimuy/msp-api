//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── computeCLV gating tests ─────────────────────────────────────────────────

func TestComputeCLV_NoVHistory_AplicaFalse(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// FechaPrimerVenta zero → no V grid → aplica=false.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            1,
		Nombre:               "Sin Ventas V",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(0, -6, 0),
		Frecuencia:           3,
		Monetary:             decimal.NewFromInt(15_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-1, 0, 0),
		Now:                  now,
		VentasMesesDistintos: 0, // zero months → aplica=false
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, c.Pagos90D())

	assert.False(t, aplica, "VentasMesesDistintos=0 must yield aplica=false")
	assert.True(t, monto.Decimal().IsZero(), "monto must be zero when aplica=false")
	assert.Empty(t, banda.String(), "banda must be empty when aplica=false")
}

func TestComputeCLV_ZeroFechaPrimerVenta_AplicaFalse(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            2,
		Nombre:               "Sin Fecha Venta",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(0, -3, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(20_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-1, 0, 0),
		Now:                  now,
		VentasMesesDistintos: 3, // non-zero months but FechaPrimerVenta is zero
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, c.Pagos90D())

	assert.False(t, aplica, "zero FechaPrimerVenta must yield aplica=false")
	assert.True(t, monto.Decimal().IsZero())
	assert.Empty(t, banda.String())
}

func TestComputeCLV_BTYDNotLoaded_AplicaFalse(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            3,
		Nombre:               "Test",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(-1, 0, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(20_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-2, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-2, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(10_000),
	})

	var zeroBTYD app.BTYD // Loaded() == false
	monto, banda, aplica := app.ExportComputeCLV(c, now, zeroBTYD, sc, params, c.Pagos90D())

	assert.False(t, aplica, "zero BTYD must return aplica=false")
	assert.True(t, monto.Decimal().IsZero())
	assert.Empty(t, banda.String())
}

func TestComputeCLV_ParamsNotLoaded_AplicaFalse(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            4,
		Nombre:               "Test",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(-1, 0, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(20_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-2, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-2, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(10_000),
	})

	var zeroParams app.CLVParams // Loaded() == false
	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, zeroParams, c.Pagos90D())

	assert.False(t, aplica, "zero CLVParams must return aplica=false")
	assert.True(t, monto.Decimal().IsZero())
	assert.Empty(t, banda.String())
}

// ─── computeCLV semantic tests ────────────────────────────────────────────────

// TestComputeCLV_FrequentHighTicket_NosaldoPositiveCLV verifies that a frequent
// high-ticket buyer with no outstanding balance receives a positive CLV in a
// sensible band.
func TestComputeCLV_FrequentHighTicket_NosaldoPositiveCLV(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// Frequent buyer: 8 distinct months over 3 years, avg ticket $12,000, no saldo.
	// x=7, acquired 3 years ago, last sale 6 months ago.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            10,
		Nombre:               "Cliente Frecuente",
		Zona:                 "Z1",
		Telefono:             "555-1000",
		FechaUltimaCompra:    now.AddDate(0, -6, 0),
		Frecuencia:           8,
		Monetary:             decimal.NewFromInt(96_000),
		Saldo:                decimal.Zero, // P(paga)=1.0, perdida=0
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-3, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-3, 0, 0),
		FechaUltimaVenta:     now.AddDate(0, -6, 0),
		VentasMesesDistintos: 8,
		MonetaryVProm:        decimal.NewFromInt(12_000),
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, c.Pagos90D())

	assert.True(t, aplica, "client with V history must have aplica=true")
	assert.True(t, monto.Decimal().IsPositive(), "frequent high-ticket buyer with no saldo must have positive CLV")
	assert.True(t, banda.IsValid(), "banda must be a valid BandaCLV")
	t.Logf("CLV: monto=%s banda=%s", monto.Decimal(), banda)
}

// TestComputeCLV_Freq0_TicketFallback verifies that x==0 (VentasMesesDistintos=1)
// uses the observed mean ticket as E[M] fallback (no panic, aplica=true).
func TestComputeCLV_Freq0_TicketFallback(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// VentasMesesDistintos=1 → x=0 → fallback to observed mean ticket.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            20,
		Nombre:               "Primera Compra",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(-1, 0, 0),
		Frecuencia:           1,
		Monetary:             decimal.NewFromInt(8_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-2, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-1, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 1, // x = max(0, 1-1) = 0
		MonetaryVProm:        decimal.NewFromInt(8_000),
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, c.Pagos90D())

	assert.True(t, aplica, "VentasMesesDistintos=1 must have aplica=true")
	// Must not panic and must return non-negative monto.
	assert.False(t, monto.Decimal().IsNegative(), "monto must be >= 0")
	assert.True(t, banda.IsValid(), "banda must be valid")
	t.Logf("CLV (freq=0 fallback): monto=%s banda=%s", monto.Decimal(), banda)
}

// TestComputeCLV_DebtorVsSameProfileNoSaldo verifies the risk adjustment:
// a debtor with a low credit score should have a lower CLV than an identical
// client with no outstanding balance.
func TestComputeCLV_DebtorVsSameProfileNoSaldo(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	baseParams := domain.CrearWinbackCandidatoParams{
		ClienteID:            50,
		Nombre:               "Cliente Deudor",
		Zona:                 "Z1",
		Telefono:             "555-5000",
		FechaUltimaCompra:    now.AddDate(0, -3, 0),
		Frecuencia:           6,
		Monetary:             decimal.NewFromInt(60_000),
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-3, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-3, 0, 0),
		FechaUltimaVenta:     now.AddDate(0, -3, 0),
		VentasMesesDistintos: 6,
		MonetaryVProm:        decimal.NewFromInt(10_000),
		// Recent payment so credit score applies.
		FechaUltimoPago:  now.AddDate(0, 0, -15),
		FechaPrimerCargo: now.AddDate(-3, 0, 0),
		NumPagos:         12,
		CadenciaDias:     30,
		PctPagosATiempo:  decimal.NewFromFloat(60), // low on-time pct
		Pagos90D:         2,
	}

	// Profile A: with outstanding saldo → credit score applies, perdida > 0.
	paramsA := baseParams
	paramsA.ClienteID = 50
	paramsA.Saldo = decimal.NewFromInt(20_000)
	paramsA.Pagos90D = 0 // fewer recent payments → lower credit score
	cDebtor := mustCandidato(paramsA)

	// Profile B: same client, saldo cleared → P(paga)=1.0, perdida=0.
	paramsB := baseParams
	paramsB.ClienteID = 51
	paramsB.Saldo = decimal.Zero
	cNoSaldo := mustCandidato(paramsB)

	montoDebtor, bandaDebtor, aplicaDebtor := app.ExportComputeCLV(cDebtor, now, btyd, sc, params, cDebtor.Pagos90D())
	montoNoSaldo, bandaNoSaldo, aplicaNoSaldo := app.ExportComputeCLV(cNoSaldo, now, btyd, sc, params, cNoSaldo.Pagos90D())

	assert.True(t, aplicaDebtor, "debtor must have aplica=true")
	assert.True(t, aplicaNoSaldo, "no-saldo must have aplica=true")

	t.Logf("debtor CLV: monto=%s banda=%s", montoDebtor.Decimal(), bandaDebtor)
	t.Logf("no-saldo CLV: monto=%s banda=%s", montoNoSaldo.Decimal(), bandaNoSaldo)

	// Risk adjustment: debtor CLV <= no-saldo CLV.
	// (When the credit score doesn't apply to the debtor — e.g. DIAS_SIN_PAGAR>180 —
	// P(paga)=1 and perdida could come from a different branch, but the fundamental
	// assertion is that the risk-adjustment mechanism doesn't inflate CLV.)
	assert.True(t,
		montoDebtor.Decimal().LessThanOrEqual(montoNoSaldo.Decimal()),
		"debtor CLV (%s) must be <= no-saldo CLV (%s) — risk adjustment must reduce or equal CLV",
		montoDebtor.Decimal(), montoNoSaldo.Decimal(),
	)
}

// TestComputeCLV_HandChecked_NumericExample verifies the CLV formula against a
// hand-calculated reference.
//
// Setup: saldo=0, VentasMesesDistintos=1 (x=0, ticket fallback), no credit score.
// Let monetary = MonetaryVProm = M.
// With saldo=0: P(paga)=1.0, perdida=0, pPaga=1.0.
// clvFinal = margin * M * DET(x=0, tx, n, H, d) * 1.0 - 0 = margin * M * DET.
//
// We use the actual DET from LoadBTYD to avoid hardcoding model parameters.
// The assertion: CLV must equal margin * MonetaryVProm * DET within 1 cent.
func TestComputeCLV_HandChecked_NumericExample(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// Acquisition: 2024-01-01, last sale: 2024-01-01 (same month), VentasMesesDistintos=1.
	// acqMonth = 2024*12+0 = 24288
	// lastMonth = 24288
	// nowMonth = 2026*12+5 = 24317
	// n = 24317 - 24288 = 29
	// tx = clamp(24288 - 24288, 0, 29) = 0
	// x = max(0, 1-1) = 0
	acqDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	const monetaryV = 8_000.0

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            99,
		Nombre:               "Hand Check",
		Zona:                 "Z1",
		FechaUltimaCompra:    acqDate,
		Frecuencia:           1,
		Monetary:             decimal.NewFromFloat(monetaryV),
		Saldo:                decimal.Zero, // pPaga=1.0, perdida=0
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         acqDate,
		Now:                  now,
		FechaPrimerVenta:     acqDate,
		FechaUltimaVenta:     acqDate,
		VentasMesesDistintos: 1, // x=0
		MonetaryVProm:        decimal.NewFromFloat(monetaryV),
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, c.Pagos90D())

	require.True(t, aplica, "hand-check client must have aplica=true")

	// Compute expected value manually:
	// n=29 (month index difference), tx=0, x=0.
	// eM = monetaryV (fallback since x==0).
	// det = btyd.DET(0, 0, 29, 24, 0.00948879).
	// clvFinal = 0.528 * eM * det * 1.0 - 0 = margin * eM * det.
	//
	// We compute det from the actual BTYD engine to avoid hardcoding model params.
	// DET is exposed via btyd.DET — we call it via the exported BTYD value.
	// Since ExportComputeCLV uses the same btyd, results must agree.
	//
	// Verify: monto == round(margin * eM * DET, 2).
	n := 29
	tx := 0
	x := 0
	det := btyd.DET(x, tx, n, params.HorizonMonths(), params.MonthlyDiscount())
	expectedCLV := params.Margin() * monetaryV * det
	expectedMonto := decimal.NewFromFloat(expectedCLV).Round(2)

	assert.True(t,
		expectedMonto.Equal(monto.Decimal()),
		"CLV mismatch: got %s, want %s (det=%.6f, margin=%.4f, eM=%.2f)",
		monto.Decimal(), expectedMonto, det, params.Margin(), monetaryV,
	)

	assert.True(t, banda.IsValid(), "banda must be valid")
	t.Logf("hand-check: n=%d tx=%d x=%d det=%.6f eM=%.2f margin=%.4f → CLV=%s banda=%s",
		n, tx, x, det, monetaryV, params.Margin(), monto.Decimal(), banda)
}

// ─── Dormancy gate tests ──────────────────────────────────────────────────────

// TestComputeCLV_DormancyGate_CapsBandaAlto verifies that a client dormant beyond
// clvRecenciaMaxMeses (24 months) cannot receive BandaCLVAlto, even when the raw
// CLV formula would yield ALTO. This prevents the slow-churn BG/BB model (γ≈0.046)
// from rating 2-3yr dormant clients as high-value.
//
// Based on real audit (2026-02-20 clock):
//   - Cliente 69230: 28 months dormant, raw CLV=$2,803 ALTO → should be capped MEDIO.
//   - Cliente 1255115: 36 months dormant, raw CLV=$1,380 ALTO → should be capped MEDIO.
func TestComputeCLV_DormancyGate_CapsBandaAlto(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)

	type dormantCase struct {
		name             string
		firstVenta       time.Time
		lastVenta        time.Time
		ventasMeses      int
		monetaryVProm    float64
		recenciaMeses    int // expected months since last V purchase
		wantBandaNotAlto bool
	}

	cases := []dormantCase{
		{
			// 69230-like: 6 distinct months, last purchase 28 months ago.
			name:             "28 months dormant (like 69230) — must not be ALTO",
			firstVenta:       time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC),
			lastVenta:        time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC),
			ventasMeses:      6,
			monetaryVProm:    3712.07,
			recenciaMeses:    28,
			wantBandaNotAlto: true,
		},
		{
			// 1255115-like: 2 distinct months, last purchase 36 months ago.
			name:             "36 months dormant (like 1255115) — must not be ALTO",
			firstVenta:       time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
			lastVenta:        time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
			ventasMeses:      2,
			monetaryVProm:    5577.59,
			recenciaMeses:    36,
			wantBandaNotAlto: true,
		},
		{
			// Active client: last purchase 1 month ago — gate should not fire.
			name:             "1 month dormant (active) — ALTO allowed",
			firstVenta:       time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
			lastVenta:        time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			ventasMeses:      5,
			monetaryVProm:    8000,
			recenciaMeses:    1,
			wantBandaNotAlto: false, // gate does NOT fire for active clients
		},
		{
			// Exactly at boundary: 24 months dormant — gate fires at >24, so 24 is OK.
			name:             "24 months dormant (boundary) — ALTO still allowed",
			firstVenta:       time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
			lastVenta:        time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			ventasMeses:      4,
			monetaryVProm:    6000,
			recenciaMeses:    24,
			wantBandaNotAlto: false,
		},
		{
			// Just over boundary: 25 months dormant — gate fires.
			name:             "25 months dormant (just over boundary) — must not be ALTO",
			firstVenta:       time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
			lastVenta:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			ventasMeses:      4,
			monetaryVProm:    6000,
			recenciaMeses:    25,
			wantBandaNotAlto: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := mustCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:            500,
				Nombre:               "Dormancy Gate Test",
				Zona:                 "Z1",
				FechaUltimaCompra:    tc.lastVenta,
				Frecuencia:           tc.ventasMeses,
				Monetary:             decimal.NewFromFloat(tc.monetaryVProm * float64(tc.ventasMeses)),
				Saldo:                decimal.Zero,
				PorLiquidarPct:       decimal.Zero,
				CohorteFecha:         tc.firstVenta,
				Now:                  now,
				FechaPrimerVenta:     tc.firstVenta,
				FechaUltimaVenta:     tc.lastVenta,
				VentasMesesDistintos: tc.ventasMeses,
				MonetaryVProm:        decimal.NewFromFloat(tc.monetaryVProm),
				PctPagosATiempo:      decimal.NewFromFloat(97),
			})

			monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, 0)

			require.True(t, aplica, "client with V history must have aplica=true")
			t.Logf("%s: monto=%s banda=%s", tc.name, monto.Decimal(), banda)

			if tc.wantBandaNotAlto {
				assert.NotEqual(t, domain.BandaCLVAlto, banda,
					"dormant client (recencia=%dm) must not be rated CLV ALTO", tc.recenciaMeses)
			}
			// The monto is unchanged (gate only affects banda, not the pesos amount).
			assert.False(t, monto.Decimal().IsNegative(), "monto must be >= 0")
			assert.True(t, banda.IsValid(), "banda must be a valid BandaCLV")
		})
	}
}

// TestComputeCLV_NoRepeatSignalGate_CapsBandaAlto verifies that a brand-new client
// with only one V purchase month (x=0, population prior only) cannot receive ALTO CLV.
// Empirical: cliente 3074781 (single $5,500 purchase 3 days old) got CLV=$3,932 ALTO
// from population priors alone — indefensible without individual repeat signal.
func TestComputeCLV_NoRepeatSignalGate_CapsBandaAlto(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	acqDate := time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC) // 3 days ago

	// VentasMesesDistintos=1 → x=0 → no repeat signal → band must not be ALTO.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            600,
		Nombre:               "Brand New Client",
		Zona:                 "Z1",
		FechaUltimaCompra:    acqDate,
		Frecuencia:           1,
		Monetary:             decimal.NewFromInt(5_500),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         acqDate,
		Now:                  now,
		FechaPrimerVenta:     acqDate,
		FechaUltimaVenta:     acqDate,
		VentasMesesDistintos: 1, // x = max(0, 1-1) = 0
		MonetaryVProm:        decimal.NewFromInt(5_500),
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, 0)

	require.True(t, aplica, "client with V history must have aplica=true")
	assert.NotEqual(t, domain.BandaCLVAlto, banda,
		"brand-new client with x=0 must not receive CLV ALTO (raw CLV=%.2f)", monto.Decimal().InexactFloat64())
	assert.False(t, monto.Decimal().IsNegative(), "monto must be >= 0")
	assert.True(t, banda.IsValid(), "banda must be a valid BandaCLV")
	t.Logf("brand-new x=0: monto=%s banda=%s (raw would be ALTO without gate)", monto.Decimal(), banda)
}

// TestComputeCLV_ActiveFrequentClient_StillAlto verifies that the dormancy and
// no-repeat-signal gates do NOT regress genuinely active, frequent clients.
// An active client with repeat purchases must still be able to reach CLV ALTO.
func TestComputeCLV_ActiveFrequentClient_StillAlto(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)

	// Active frequent client: 8 distinct months over 4 years, last purchase 1 month ago.
	// x=7, well within the repeat signal gate; recencia=1 month, well within dormancy gate.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            700,
		Nombre:               "Cliente Activo Frecuente",
		Zona:                 "Z1",
		Telefono:             "555-7000",
		FechaUltimaCompra:    time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Frecuencia:           8,
		Monetary:             decimal.NewFromInt(96_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		Now:                  now,
		FechaPrimerVenta:     time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		FechaUltimaVenta:     time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		VentasMesesDistintos: 8,
		MonetaryVProm:        decimal.NewFromInt(12_000),
		PctPagosATiempo:      decimal.NewFromFloat(90),
	})

	monto, banda, aplica := app.ExportComputeCLV(c, now, btyd, sc, params, 0)

	require.True(t, aplica)
	assert.Equal(t, domain.BandaCLVAlto, banda,
		"active frequent client must remain CLV ALTO after gates: monto=%s", monto.Decimal())
	t.Logf("active frequent: monto=%s banda=%s", monto.Decimal(), banda)
}

// ─── Pulso wiring tests ───────────────────────────────────────────────────────

// TestObtenerPulsoCliente_CLV_WithVHistory verifies that a candidato with V
// purchase history receives non-empty BandaCLV and a non-negative MontoCLV
// in the contract. Uses the service with its embedded scorecards.
func TestObtenerPulsoCliente_CLV_WithVHistory(t *testing.T) {
	t.Parallel()

	c := makeVHistoryCandidato(300)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	pulse, err := svc.ObtenerPulsoCliente(context.Background(), 300)
	require.NoError(t, err)

	assert.False(t, pulse.MontoCLV.IsNegative(), "MontoCLV must be >= 0")
	assert.NotEmpty(t, pulse.BandaCLV, "BandaCLV must be non-empty for V history")
	t.Logf("CLV pulse: monto=%s banda=%s", pulse.MontoCLV, pulse.BandaCLV)
}

// TestObtenerPulsoCliente_CLV_NoVHistory verifies that a candidato with no V
// purchase history receives zero MontoCLV and empty BandaCLV ("no aplica").
func TestObtenerPulsoCliente_CLV_NoVHistory(t *testing.T) {
	t.Parallel()

	// makePulsoCandidato has no V-history fields (FechaPrimerVenta zero, VentasMesesDistintos=0).
	c := makePulsoCandidato(301, "20000.00", 400)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	pulse, err := svc.ObtenerPulsoCliente(context.Background(), 301)
	require.NoError(t, err)

	assert.True(t, pulse.MontoCLV.IsZero(), "MontoCLV must be 0 when no aplica")
	assert.Empty(t, pulse.BandaCLV, "BandaCLV must be empty when no aplica")
}
