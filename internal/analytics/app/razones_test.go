//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── Money helpers ────────────────────────────────────────────────────────────

func TestPesosMiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input float64
		want  string
	}{
		{9483.21, "$9,483"},
		{19094.5, "$19,095"},
		{1000.0, "$1,000"},
		{999.0, "$999"},
		{0.0, "$0"},
		{1234567.89, "$1,234,568"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := app.ExportPesosMiles(decimal.NewFromFloat(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPesosCompact(t *testing.T) {
	t.Parallel()

	specTests := []struct {
		input float64
		want  string
	}{
		{19094, "$19.1k"},
		{950.4, "$950"},
	}

	for _, tt := range specTests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := app.ExportPesosCompact(decimal.NewFromFloat(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

// ─── Resumen crédito ──────────────────────────────────────────────────────────

func TestResumenCredito_NoAplica_SinSaldo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    1,
		Nombre:       "Sin Saldo",
		Zona:         "Z1",
		Saldo:        decimal.Zero, // no credit balance
		CohorteFecha: now.AddDate(-1, 0, 0),
		Now:          now,
	})

	got := app.ExportResumenCredito(c, now, domain.BandaCredito(""), domain.ScoreCredito{}, false)
	assert.Equal(t, "Sin saldo a crédito — no se evalúa.", got)
}

func TestResumenCredito_NoAplica_SaldoPositivo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// Saldo > 0 but aplica=false (e.g. last payment was too long ago)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    2,
		Nombre:       "Inactivo",
		Zona:         "Z1",
		Saldo:        decimal.NewFromInt(5000),
		CohorteFecha: now.AddDate(-1, 0, 0),
		Now:          now,
	})

	got := app.ExportResumenCredito(c, now, domain.BandaCredito(""), domain.ScoreCredito{}, false)
	assert.Equal(t, "Crédito inactivo — sin pagos recientes para evaluar.", got)
}

func TestResumenCredito_BuenPagador(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// Construct a client with cadencia=28, pct=92 and pass BandaCreditoBajo directly
	// so the test exercises the resumen string unconditionally without depending on
	// the embedded scorecard thresholds (which can change between versions).
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       10,
		Nombre:          "Fernández Reyes",
		Zona:            "Z1",
		Saldo:           decimal.NewFromInt(3000),
		FechaUltimoPago: now.AddDate(0, 0, -5),
		CadenciaDias:    28,
		PctPagosATiempo: decimal.NewFromFloat(92),
		CohorteFecha:    now.AddDate(-2, 0, 0),
		Now:             now,
	})

	// Pass BandaCreditoBajo directly — we are testing the resumen string builder,
	// not the scorecard thresholds.
	score, err := domain.NewScoreCredito(80)
	require.NoError(t, err)
	got := app.ExportResumenCredito(c, now, domain.BandaCreditoBajo, score, true)
	assert.True(t, strings.HasPrefix(got, "Buen pagador:"), "resumen debe empezar con 'Buen pagador:'; got: %q", got)
	// cadencia=28 > 0 → should include cadence and punctuality
	assert.Contains(t, got, "~28 días", "debe mencionar cadencia; got: %q", got)
	assert.Contains(t, got, "92%", "debe mencionar puntualidad; got: %q", got)
}

func TestResumenCredito_RiesgoCritico(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// Moroso: 131 días sin pagar, saldo ~19,000, cadencia ~6.
	// Pass BandaCreditoCritico directly — we are testing the resumen string builder,
	// not the scorecard thresholds (which can change between versions).
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:        20,
		Nombre:           "Torres Morales",
		Zona:             "Z1",
		Saldo:            decimal.NewFromInt(19000),
		FechaUltimoPago:  now.AddDate(0, 0, -131),
		FechaPrimerCargo: now.AddDate(-2, 0, 0),
		CadenciaDias:     6,
		PctPagosATiempo:  decimal.NewFromFloat(15),
		NumPagos:         10,
		Pagos90D:         0,
		DiasAtrasoProm:   120,
		CohorteFecha:     now.AddDate(-2, 0, 0),
		Now:              now,
	})

	score, err := domain.NewScoreCredito(10)
	require.NoError(t, err)
	got := app.ExportResumenCredito(c, now, domain.BandaCreditoCritico, score, true)
	assert.True(t, strings.HasPrefix(got, "Riesgo crítico:"),
		"resumen debe empezar con 'Riesgo crítico:'; got: %q", got)
	assert.Contains(t, got, "131 días", "resumen debe mencionar 131 días; got: %q", got)
	assert.Contains(t, got, "$19", "resumen debe mencionar el saldo compacto; got: %q", got)
}

// ─── Crédito drivers cuantificados ───────────────────────────────────────────

func TestRazonesCredito_Moroso131Dias(t *testing.T) {
	t.Parallel()

	// Build feature contribution for DIAS_SIN_PAGAR = 131, logit > 0, cadencia = 6
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       20,
		Nombre:          "Torres Morales",
		Zona:            "Z1",
		Saldo:           decimal.NewFromInt(19000),
		FechaUltimoPago: now.AddDate(0, 0, -131),
		CadenciaDias:    6,
		CohorteFecha:    now.AddDate(-2, 0, 0),
		Now:             now,
	})

	// Inject DIAS_SIN_PAGAR contribution directly via the exported helper.
	contribs := []app.FeatureContrib{
		{Name: "DIAS_SIN_PAGAR", Label: "días sin pagar", Valor: 131, Logit: 2.5},      // risk-increasing
		{Name: "PAGOS_90D", Label: "pagos recientes", Valor: 1, Logit: -0.3},           // risk-decreasing
		{Name: "PCT_PAGOS_A_TIEMPO_6M", Label: "puntualidad", Valor: 0.15, Logit: 0.8}, // risk-increasing
	}

	drivers := app.ExportRazonesCredito(c, contribs)
	require.NotEmpty(t, drivers, "drivers no deben estar vacíos para un moroso")

	// The DIAS_SIN_PAGAR contribution should produce "131 días sin pagar (su ritmo: ~6)"
	found := false
	for _, d := range drivers {
		if d == "131 días sin pagar (su ritmo: ~6)" {
			found = true
			break
		}
	}
	assert.True(t, found, "debe incluir '131 días sin pagar (su ritmo: ~6)'; drivers: %v", drivers)
}

// ─── Resumen recompra ─────────────────────────────────────────────────────────

func TestResumenRecompra_NoAplica(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:    30,
		Nombre:       "Sin Historial",
		Zona:         "Z1",
		CohorteFecha: now.AddDate(-1, 0, 0),
		Now:          now,
	})

	got := app.ExportResumenRecompra(c, now, domain.BandaRecompra(""), domain.ScoreRecompra{}, false)
	assert.Equal(t, "Sin historial de compras — no se evalúa.", got)
}

func TestResumenRecompra_DormidoBaja_12Meses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// Last venta was 14 months ago → recenciaMeses >= 12 → special "Poco probable — no compra hace N meses."
	fechaUltimaVenta := now.AddDate(0, -14, 0)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            40,
		Nombre:               "García Dormido",
		Zona:                 "Z1",
		FechaUltimaVenta:     fechaUltimaVenta,
		FechaPrimerVenta:     fechaUltimaVenta.AddDate(-1, 0, 0),
		VentasMesesDistintos: 3,
		MonetaryVProm:        decimal.NewFromInt(8000),
		CohorteFecha:         now.AddDate(-2, 0, 0),
		Now:                  now,
	})

	score, _ := domain.NewScoreRecompra(30)
	got := app.ExportResumenRecompra(c, now, domain.BandaRecompraBaja, score, true)

	// recenciaMeses = monthIndex(now) - monthIndex(fechaUltimaVenta) ≈ 14
	assert.True(t, strings.HasPrefix(got, "Poco probable — no compra hace "),
		"resumen dormido ≥12m debe empezar con 'Poco probable — no compra hace '; got: %q", got)
	assert.Contains(t, got, "meses.", "resumen debe terminar con 'meses.'; got: %q", got)
}

// ─── CLV con razones ─────────────────────────────────────────────────────────

func TestComputeCLVConRazones_NoAplica(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            50,
		Nombre:               "Sin Ventas V",
		Zona:                 "Z1",
		CohorteFecha:         now.AddDate(-1, 0, 0),
		Now:                  now,
		VentasMesesDistintos: 0, // gates → aplica=false
	})

	_, _, drivers, resumen, aplica := app.ExportComputeCLVConRazones(c, now, btyd, sc, params, 0)

	assert.False(t, aplica)
	assert.Nil(t, drivers)
	assert.Equal(t, "Sin historial de compras — no se evalúa.", resumen)
}

func TestComputeCLVConRazones_CLVPositivo(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// Good client: no saldo (low risk), recent purchase history, 3 distinct months.
	fechaPrimerVenta := now.AddDate(-1, -3, 0)
	fechaUltimaVenta := now.AddDate(0, -2, 0)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            60,
		Nombre:               "Ramírez López",
		Zona:                 "Z1",
		Saldo:                decimal.Zero, // no credit exposure
		FechaUltimaVenta:     fechaUltimaVenta,
		FechaPrimerVenta:     fechaPrimerVenta,
		VentasMesesDistintos: 3,
		MonetaryVProm:        decimal.NewFromInt(12000),
		CohorteFecha:         fechaPrimerVenta.AddDate(0, 0, -1),
		Now:                  now,
	})

	monto, _, drivers, resumen, aplica := app.ExportComputeCLVConRazones(c, now, btyd, sc, params, 0)

	require.True(t, aplica, "cliente con historial de ventas debe tener aplica=true")
	assert.NotEmpty(t, drivers, "drivers no deben estar vacíos cuando aplica")
	assert.LessOrEqual(t, len(drivers), 3, "máximo 3 drivers")

	require.True(t, monto.Decimal().IsPositive(), "fixture (saldo=0, no credit exposure → pPaga=1) must produce CLV>0; got %s", monto.Decimal())
	assert.True(t, strings.HasPrefix(resumen, "Valor estimado $"),
		"resumen positivo debe empezar con 'Valor estimado $'; got: %q", resumen)
	assert.Contains(t, resumen, "ticket de $",
		"resumen positivo debe mencionar el ticket; got: %q", resumen)

	// Drivers must contain a ticket entry.
	hasTicket := false
	for _, d := range drivers {
		if strings.HasPrefix(d, "ticket $") {
			hasTicket = true
			break
		}
	}
	assert.True(t, hasTicket, "drivers deben incluir una entrada de ticket; got: %v", drivers)
}

func TestComputeCLVConRazones_CLVCeroPorPerdida(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)
	sc, err := app.LoadScorecard()
	require.NoError(t, err)
	params, err := app.LoadCLVParams()
	require.NoError(t, err)

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	// High-risk delinquent: massive saldo (500000) + very low credit score → pPaga≈0 → perdida
	// overwhelmingly dominates gross*pPaga regardless of future model tuning.
	// Last payment within performing window (150 days) so the credit scorecard applies.
	fechaPrimerVenta := now.AddDate(-2, 0, 0)
	fechaUltimaVenta := now.AddDate(0, -3, 0)
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            70,
		Nombre:               "Mendoza Crítico",
		Zona:                 "Z1",
		Saldo:                decimal.NewFromInt(500000), // overwhelmingly large to lock loss-dominated branch
		FechaUltimoPago:      now.AddDate(0, 0, -150),    // within performing window
		FechaPrimerCargo:     now.AddDate(-2, 0, 0),
		CadenciaDias:         30,
		PctPagosATiempo:      decimal.NewFromFloat(5), // extremely low punctuality
		NumPagos:             3,                       // very few payments
		Pagos90D:             0,
		DiasAtrasoProm:       140,
		FechaUltimaVenta:     fechaUltimaVenta,
		FechaPrimerVenta:     fechaPrimerVenta,
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(8000),
		CohorteFecha:         fechaPrimerVenta.AddDate(0, 0, -1),
		Now:                  now,
	})

	monto, _, drivers, resumen, aplica := app.ExportComputeCLVConRazones(c, now, btyd, sc, params, 0)

	require.True(t, aplica, "cliente con ventas debe tener aplica=true")
	assert.NotNil(t, drivers, "drivers no deben ser nil cuando aplica")
	require.True(t, monto.Decimal().IsZero(), "fixture (saldo=500000, credit CRITICO → pPaga≈0) must produce CLV=0; got %s", monto.Decimal())

	assert.True(t, strings.HasPrefix(resumen, "Vale ~$0 ajustado por riesgo:"),
		"resumen con CLV=0 debe empezar con 'Vale ~$0 ajustado por riesgo:'; got: %q", resumen)
	assert.Contains(t, resumen, "borra el valor", "resumen debe mencionar 'borra el valor'; got: %q", resumen)

	hasImpago := false
	for _, d := range drivers {
		if strings.HasPrefix(d, "riesgo de impago (-$") {
			hasImpago = true
			break
		}
	}
	assert.True(t, hasImpago, "drivers deben incluir 'riesgo de impago (-$...'; got: %v", drivers)
}

// ─── Plural unit helpers ──────────────────────────────────────────────────────

func TestPluralDias(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1 día", app.ExportPluralDias(1))
	assert.Equal(t, "6 días", app.ExportPluralDias(6))
	assert.Equal(t, "131 días", app.ExportPluralDias(131))
}

func TestPluralMeses(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1 mes", app.ExportPluralMeses(1))
	assert.Equal(t, "5 meses", app.ExportPluralMeses(5))
	assert.Equal(t, "28 meses", app.ExportPluralMeses(28))
}

func TestPluralAnios(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1 año", app.ExportPluralAnios(1))
	assert.Equal(t, "2 años", app.ExportPluralAnios(2))
}
