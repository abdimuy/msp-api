// Package app — cartera_query.go contains the cartera analytics service methods.
// All metrics are computed on-the-fly in Go from the repo aggregations —
// no business logic lives in SQL (CLAUDE.md §1).
//
// Methods on *Service (Task B4):
//   - ObtenerSaludCartera — executive KPI set (PAR, CEI, saldo, cuentas, margen)
//   - ObtenerAging        — aging-bucket distribution
//   - ObtenerCosechas     — vintage-cohort saldo by month
//   - ObtenerRankingCobradores — per-cobrador PAR, CEI, cumplimiento, cobertura
//   - ListarCuentasRiesgo — at-risk accounts with tier + RFM enrichment
//   - ObtenerCumplimiento — expected-payment compliance distribution
//   - MargenReal          — detailed proxy margin computation
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Constants ────────────────────────────────────────────────────────────────

// MargenVerificado is the verified 2025 owner gross margin (52.8%).
//
// Source: real 2025 data derived from DOCTOS_IN_DET conc 32+33.
// IVA is mostly unbilled, so it is counted as owner margin.
// See project memory: project_verified_unit_economics.
//
// Usage: MargenBruto = MargenVerificado × Ventas (period cash collected).
const MargenVerificado = 0.528

// Package-level decimals built from strings to avoid float64 representation
// noise (shopspring/decimal is exact; constructing from floats can lose the
// last ULP).
//
// lgdCarteraDecimal: Loss Given Default assumed for the portfolio (v1 constant,
// 70%). Proxy: 70% of delinquent balance is unrecoverable short-term.
// v1 formula: PerdidaEsperada = PAR × SaldoTotal × lgdCarteraDecimal.
// Replace with empirical write-off rate once historical charge-off data is
// available from the Microsip books.
var (
	margenVerificadoDecimal = decimal.RequireFromString("0.528")
	lgdCarteraDecimal       = decimal.RequireFromString("0.70")
)

// defaultCEIPeriodDays is the fallback CEI window (calendar days) when the
// caller does not supply Desde/Hasta in CarteraParams. 30 days maps to one
// typical monthly-payment cycle for furniture credit.
const defaultCEIPeriodDays = 30

// ─── Params ───────────────────────────────────────────────────────────────────

// CarteraParams groups the optional filter parameters for cartera service
// methods. All fields are optional; absent values mean "whole portfolio".
type CarteraParams struct {
	// ZonaClienteID restricts aging, CEI, and vintage queries to a specific zone
	// (matched against AgingRow.ZonaClienteID / VintageRow.ZonaClienteID).
	// Nil = all zones.
	ZonaClienteID *int

	// Zona restricts candidato-based queries (ListarCuentasRiesgo,
	// ObtenerCumplimiento) to a specific zone name string
	// (matched against WinbackCandidato.Zona()). "" = all zones.
	Zona string

	// CobradorID restricts cobrador-level queries (ObtenerRankingCobradores)
	// to a single collector. Nil = all cobradores.
	CobradorID *int

	// Desde is the inclusive start of the CEI collection period.
	// Zero defaults to (Hasta − defaultCEIPeriodDays).
	Desde time.Time

	// Hasta is the exclusive end of the CEI collection period.
	// Zero defaults to the service clock's now.
	Hasta time.Time
}

// ErrCarteraRepoNotConfigured is returned by cartera methods when WithCarteraRepo
// has not been called. Wiring error, not a user error.
var ErrCarteraRepoNotConfigured = errors.New("cartera repo not configured")

// ─── Internal helpers ─────────────────────────────────────────────────────────

// resolveCEIPeriod returns the effective [desde, hasta) window.
// Hasta defaults to now; Desde defaults to (Hasta − defaultCEIPeriodDays).
func resolveCEIPeriod(p CarteraParams, now time.Time) (time.Time, time.Time) {
	hasta := p.Hasta
	if hasta.IsZero() {
		hasta = now
	}
	desde := p.Desde
	if desde.IsZero() {
		desde = hasta.AddDate(0, 0, -defaultCEIPeriodDays)
	}
	return desde, hasta
}

// filterAgingByZona returns only the rows whose ZonaClienteID matches zonaID.
// When zonaID is nil, all rows are returned unchanged.
func filterAgingByZona(rows []outbound.AgingRow, zonaID *int) []outbound.AgingRow {
	if zonaID == nil {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.ZonaClienteID == *zonaID {
			out = append(out, r)
		}
	}
	return out
}

// filterAgingByCobrador returns only the rows whose CobradorID matches cobID.
// When cobID is nil, all rows are returned unchanged.
func filterAgingByCobrador(rows []outbound.AgingRow, cobID *int) []outbound.AgingRow {
	if cobID == nil {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.CobradorID != nil && *r.CobradorID == *cobID {
			out = append(out, r)
		}
	}
	return out
}

// filterVintageByZona returns only the rows whose ZonaClienteID matches zonaID.
// When zonaID is nil, all rows are returned unchanged.
func filterVintageByZona(rows []outbound.VintageRow, zonaID *int) []outbound.VintageRow {
	if zonaID == nil {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.ZonaClienteID == *zonaID {
			out = append(out, r)
		}
	}
	return out
}

// filterCEIByZona returns only the rows whose ZonaClienteID matches zonaID.
// When zonaID is nil, all rows are returned unchanged.
func filterCEIByZona(rows []outbound.CEIRow, zonaID *int) []outbound.CEIRow {
	if zonaID == nil {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.ZonaClienteID == *zonaID {
			out = append(out, r)
		}
	}
	return out
}

// filterCEIByCobrador returns only the rows whose CobradorID matches cobID.
// When cobID is nil, all rows are returned unchanged.
func filterCEIByCobrador(rows []outbound.CEIRow, cobID *int) []outbound.CEIRow {
	if cobID == nil {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.CobradorID != nil && *r.CobradorID == *cobID {
			out = append(out, r)
		}
	}
	return out
}

// agingIsMoroso returns true for buckets that represent delinquency (31+).
func agingIsMoroso(bucket string) bool {
	return bucket == domain.BucketAgingDias31_60 ||
		bucket == domain.BucketAgingDias61_90 ||
		bucket == domain.BucketAgingDias90Plus
}

// aggregateAgingTotals returns (saldoTotal, saldoMoroso, cuentasTotal, cuentasEnMora)
// from a slice of AgingRow.
func aggregateAgingTotals(rows []outbound.AgingRow) (decimal.Decimal, decimal.Decimal, int, int) {
	var saldoTotal, saldoMoroso decimal.Decimal
	var cuentasTotal, cuentasEnMora int
	for _, r := range rows {
		saldoTotal = saldoTotal.Add(r.Saldo)
		cuentasTotal += r.Conteo
		if agingIsMoroso(r.Bucket) {
			saldoMoroso = saldoMoroso.Add(r.Saldo)
			cuentasEnMora += r.Conteo
		}
	}
	return saldoTotal, saldoMoroso, cuentasTotal, cuentasEnMora
}

// sumCEIImporte returns the total importe across a slice of CEIRow.
func sumCEIImporte(rows []outbound.CEIRow) decimal.Decimal {
	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.Importe)
	}
	return total
}

// computeMargenRealDecimal computes the margin proxy using the v1 formula.
//
// MargenBruto     = MargenVerificado × ventas
// PerdidaEsperada = PAR × saldoTotal × lgdCartera
// MargenReal      = MargenBruto − PerdidaEsperada (floored at 0).
func computeMargenRealDecimal(ventas, par, saldoTotal decimal.Decimal) (decimal.Decimal, decimal.Decimal, decimal.Decimal) {
	margenBruto := margenVerificadoDecimal.Mul(ventas)
	perdida := par.Mul(saldoTotal).Mul(lgdCarteraDecimal)
	margenReal := margenBruto.Sub(perdida)
	if margenReal.IsNegative() {
		margenReal = decimal.Zero
	}
	return margenBruto, perdida, margenReal
}

// ─── ObtenerSaludCartera ─────────────────────────────────────────────────────

// ObtenerSaludCartera returns the executive KPI set for the credit portfolio.
//
// Metrics computed:
//   - SaldoTotal, SaldoMoroso (sum of 31-60, 61-90, 90+ buckets)
//   - PAR (PARRatio = SaldoMoroso / SaldoTotal)
//   - CEIRate (ImporteColectado / SaldoTotal — v1 proxy for collection rate)
//   - CuentasTotal, CuentasEnMora (accounts in moroso buckets)
//   - MargenRealProxy (v1: MargenVerificado × ImporteColectado − PAR × SaldoTotal × LGD)
func (s *Service) ObtenerSaludCartera(ctx context.Context, p CarteraParams) (analytics.SaludCarteraContract, error) {
	const source = "analytics.ObtenerSaludCartera"

	if s.carteraRepo == nil {
		return analytics.SaludCarteraContract{}, apperror.NewInternal("cartera_repo_nil", "el repo de cartera no está configurado").
			WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	today := s.clock.Now()

	// Aging: zone-level aggregation (no cobrador breakdown needed here).
	agingRows, err := s.carteraRepo.AgingSaldosByZona(ctx, today)
	if err != nil {
		return analytics.SaludCarteraContract{}, apperror.NewInternal("cartera_aging_failed", "error al obtener aging de cartera").
			WithSource(source).WithError(err)
	}
	agingRows = filterAgingByZona(agingRows, p.ZonaClienteID)

	saldoTotal, saldoMoroso, cuentasTotal, cuentasEnMora := aggregateAgingTotals(agingRows)
	par := domain.PARRatio(saldoMoroso, saldoTotal)

	// CEI: total collected in the period.
	desde, hasta := resolveCEIPeriod(p, today)
	ceiRows, err := s.carteraRepo.ColeccionCEI(ctx, desde, hasta)
	if err != nil {
		return analytics.SaludCarteraContract{}, apperror.NewInternal("cartera_cei_failed", "error al obtener colección CEI").
			WithSource(source).WithError(err)
	}
	ceiRows = filterCEIByZona(ceiRows, p.ZonaClienteID)
	importeColectado := sumCEIImporte(ceiRows)

	// CEIRate = ImporteColectado / SaldoTotal (v1 proxy; 0 when portfolio is empty).
	var ceiRate decimal.Decimal
	if saldoTotal.IsPositive() {
		ceiRate = importeColectado.Div(saldoTotal)
	}

	// MargenReal proxy.
	_, _, margenReal := computeMargenRealDecimal(importeColectado, par, saldoTotal)

	return analytics.SaludCarteraContract{
		SaldoTotal:       saldoTotal,
		SaldoMoroso:      saldoMoroso,
		PAR:              par,
		CEIRate:          ceiRate,
		ImporteColectado: importeColectado,
		CuentasTotal:     cuentasTotal,
		CuentasEnMora:    cuentasEnMora,
		MargenRealProxy:  margenReal,
	}, nil
}

// ─── ObtenerAging ─────────────────────────────────────────────────────────────

// ObtenerAging returns the aging-bucket distribution for the portfolio.
// Results are aggregated across all zones (or filtered to ZonaClienteID / CobradorID).
// The four canonical buckets are always returned (with zero saldo/conteo when empty).
// Buckets are ordered: 0-30, 31-60, 61-90, 90+.
func (s *Service) ObtenerAging(ctx context.Context, p CarteraParams) ([]analytics.AgingBucketContract, error) {
	const source = "analytics.ObtenerAging"

	if s.carteraRepo == nil {
		return nil, apperror.NewInternal("cartera_repo_nil", "el repo de cartera no está configurado").
			WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	today := s.clock.Now()

	var rows []outbound.AgingRow
	var err error
	if p.CobradorID != nil {
		rows, err = s.carteraRepo.AgingSaldosByCobrador(ctx, today)
	} else {
		rows, err = s.carteraRepo.AgingSaldosByZona(ctx, today)
	}
	if err != nil {
		return nil, apperror.NewInternal("cartera_aging_failed", "error al obtener aging de cartera").
			WithSource(source).WithError(err)
	}

	rows = filterAgingByZona(rows, p.ZonaClienteID)
	if p.CobradorID != nil {
		rows = filterAgingByCobrador(rows, p.CobradorID)
	}

	// Aggregate by bucket across all zones/cobradores.
	bucketOrder := []string{
		domain.BucketAgingDias0_30,
		domain.BucketAgingDias31_60,
		domain.BucketAgingDias61_90,
		domain.BucketAgingDias90Plus,
	}
	bucketSaldo := make(map[string]decimal.Decimal, 4)
	bucketConteo := make(map[string]int, 4)
	for _, b := range bucketOrder {
		bucketSaldo[b] = decimal.Zero
		bucketConteo[b] = 0
	}
	var totalSaldo decimal.Decimal
	for _, r := range rows {
		bucketSaldo[r.Bucket] = bucketSaldo[r.Bucket].Add(r.Saldo)
		bucketConteo[r.Bucket] += r.Conteo
		totalSaldo = totalSaldo.Add(r.Saldo)
	}

	result := make([]analytics.AgingBucketContract, 0, 4)
	for _, b := range bucketOrder {
		sal := bucketSaldo[b]
		var pct decimal.Decimal
		if totalSaldo.IsPositive() {
			pct = sal.Div(totalSaldo)
		}
		result = append(result, analytics.AgingBucketContract{
			Bucket:   b,
			Saldo:    sal,
			Conteo:   bucketConteo[b],
			PctSaldo: pct,
		})
	}
	return result, nil
}

// ─── ObtenerCosechas ─────────────────────────────────────────────────────────

// ObtenerCosechas returns the vintage-cohort balance distribution, aggregated
// by cohort-month (year×12+month). Results are sorted by cohort month descending
// (most recent cohort first). AgeMonths is computed relative to now.
func (s *Service) ObtenerCosechas(ctx context.Context, p CarteraParams) ([]analytics.CosechaContract, error) {
	const source = "analytics.ObtenerCosechas"

	if s.carteraRepo == nil {
		return nil, apperror.NewInternal("cartera_repo_nil", "el repo de cartera no está configurado").
			WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	rows, err := s.carteraRepo.VintageSaldos(ctx)
	if err != nil {
		return nil, apperror.NewInternal("cartera_vintage_failed", "error al obtener cosechas de cartera").
			WithSource(source).WithError(err)
	}

	rows = filterVintageByZona(rows, p.ZonaClienteID)

	today := s.clock.Now()
	nowCohort := domain.VintageCohort(today)

	// Aggregate by cohort month (rows may have multiple zones per month).
	type cosechaAgg struct {
		saldo  decimal.Decimal
		conteo int
	}
	byMonth := make(map[int]*cosechaAgg)
	for _, r := range rows {
		agg, ok := byMonth[r.CohortMonth]
		if !ok {
			byMonth[r.CohortMonth] = &cosechaAgg{}
			agg = byMonth[r.CohortMonth]
		}
		agg.saldo = agg.saldo.Add(r.Saldo)
		agg.conteo += r.Conteo
	}

	result := make([]analytics.CosechaContract, 0, len(byMonth))
	for month, agg := range byMonth {
		ageMonths := nowCohort - month
		if ageMonths < 0 {
			ageMonths = 0
		}
		result = append(result, analytics.CosechaContract{
			CohortMonth: month,
			AgeMonths:   ageMonths,
			Saldo:       agg.saldo,
			Conteo:      agg.conteo,
		})
	}

	// Sort by cohort month DESC (most recent cohort first).
	sort.Slice(result, func(i, j int) bool {
		return result[i].CohortMonth > result[j].CohortMonth
	})
	return result, nil
}

// ─── ObtenerRankingCobradores helpers ────────────────────────────────────────

// cobrKey is the composite key for the per-cobrador aggregation map.
// cobID 0 means "no cobrador assigned".
type cobrKey struct {
	cobID  int
	zonaID int
}

// cobrAgg accumulates balance, counts, and collected amounts for one cobrador.
type cobrAgg struct {
	saldoTotal       decimal.Decimal
	saldoMoroso      decimal.Decimal
	saldoCorriente   decimal.Decimal // 0-30 saldo
	cuentasTotal     int
	cuentasCorriente int // 0-30 count
	importe          decimal.Decimal
}

// mergeCEIImporte distributes CEI row importes into the byKey aggregation map.
// A row is matched on (cobID, zonaID) — both must agree so that a cobrador who
// manages clients in two zones only accumulates importe for the zone the CEI row
// belongs to (matching on cobID alone would inflate the other zone's entry).
// A row for a cobrador not present in byKey is inserted as a new entry using the
// CEI row's zona (fully-paid collector scenario).
func mergeCEIImporte(byKey map[cobrKey]*cobrAgg, rows []outbound.CEIRow) {
	for _, r := range rows {
		cobID := 0
		if r.CobradorID != nil {
			cobID = *r.CobradorID
		}
		matched := false
		for k, agg := range byKey {
			if k.cobID == cobID && k.zonaID == r.ZonaClienteID {
				agg.importe = agg.importe.Add(r.Importe)
				matched = true
			}
		}
		if !matched {
			k := cobrKey{cobID: cobID, zonaID: r.ZonaClienteID}
			byKey[k] = &cobrAgg{importe: r.Importe}
		}
	}
}

// buildCobradorPerformanceContracts converts the per-cobrador aggregation map
// into a sorted slice of CobradorPerformanceContract.
// Sort order: CEI DESC, then CobradorID ASC for determinism.
func buildCobradorPerformanceContracts(byKey map[cobrKey]*cobrAgg) []analytics.CobradorPerformanceContract {
	result := make([]analytics.CobradorPerformanceContract, 0, len(byKey))
	for k, agg := range byKey {
		par := domain.PARRatio(agg.saldoMoroso, agg.saldoTotal)

		var cei decimal.Decimal
		if agg.saldoTotal.IsPositive() {
			cei = agg.importe.Div(agg.saldoTotal)
		}

		var pctCorriente decimal.Decimal
		if agg.cuentasTotal > 0 {
			pctCorriente = decimal.NewFromInt(int64(agg.cuentasCorriente)).
				Div(decimal.NewFromInt(int64(agg.cuentasTotal)))
		}

		result = append(result, analytics.CobradorPerformanceContract{
			CobradorID:       k.cobID,
			ZonaClienteID:    k.zonaID,
			CEI:              cei,
			PAR:              par,
			PctCorriente:     pctCorriente,
			SaldoTotal:       agg.saldoTotal,
			SaldoMoroso:      agg.saldoMoroso,
			CuentasTotal:     agg.cuentasTotal,
			ImporteColectado: agg.importe,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		cmp := result[i].CEI.Cmp(result[j].CEI)
		if cmp != 0 {
			return cmp > 0
		}
		return result[i].CobradorID < result[j].CobradorID
	})
	return result
}

// ─── ObtenerRankingCobradores ─────────────────────────────────────────────────

// ObtenerRankingCobradores returns per-cobrador performance metrics for the
// credit portfolio. It is NOT a competitive leaderboard — just performance rows.
//
// Metrics per cobrador:
//   - PAR: Portfolio-at-Risk (their moroso balance / their total balance)
//   - CEI: Collection Effectiveness Index (their collected / their total saldo)
//   - PctCorriente: % of accounts in the 0-30 (current) bucket
//   - SaldoTotal, SaldoMoroso, CuentasTotal, ImporteColectado
//
// Results are sorted by CEI DESC (highest collector effectiveness first).
func (s *Service) ObtenerRankingCobradores(ctx context.Context, p CarteraParams) ([]analytics.CobradorPerformanceContract, error) {
	const source = "analytics.ObtenerRankingCobradores"

	if s.carteraRepo == nil {
		return nil, apperror.NewInternal("cartera_repo_nil", "el repo de cartera no está configurado").
			WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	today := s.clock.Now()

	agingRows, err := s.carteraRepo.AgingSaldosByCobrador(ctx, today)
	if err != nil {
		return nil, apperror.NewInternal("cartera_aging_failed", "error al obtener aging por cobrador").
			WithSource(source).WithError(err)
	}
	agingRows = filterAgingByZona(agingRows, p.ZonaClienteID)
	agingRows = filterAgingByCobrador(agingRows, p.CobradorID)

	desde, hasta := resolveCEIPeriod(p, today)
	ceiRows, err := s.carteraRepo.ColeccionCEI(ctx, desde, hasta)
	if err != nil {
		return nil, apperror.NewInternal("cartera_cei_failed", "error al obtener colección CEI").
			WithSource(source).WithError(err)
	}
	ceiRows = filterCEIByZona(ceiRows, p.ZonaClienteID)
	ceiRows = filterCEIByCobrador(ceiRows, p.CobradorID)

	// Build per-cobrador aggregations keyed by (cobID, zonaID).
	// cobID 0 = no cobrador assigned.
	byKey := make(map[cobrKey]*cobrAgg)

	for _, r := range agingRows {
		cobID := 0
		if r.CobradorID != nil {
			cobID = *r.CobradorID
		}
		k := cobrKey{cobID: cobID, zonaID: r.ZonaClienteID}
		agg, ok := byKey[k]
		if !ok {
			byKey[k] = &cobrAgg{}
			agg = byKey[k]
		}
		agg.saldoTotal = agg.saldoTotal.Add(r.Saldo)
		agg.cuentasTotal += r.Conteo
		if agingIsMoroso(r.Bucket) {
			agg.saldoMoroso = agg.saldoMoroso.Add(r.Saldo)
		}
		if r.Bucket == domain.BucketAgingDias0_30 {
			agg.saldoCorriente = agg.saldoCorriente.Add(r.Saldo)
			agg.cuentasCorriente += r.Conteo
		}
	}

	// Merge CEI collected amounts into the per-cobrador map.
	mergeCEIImporte(byKey, ceiRows)

	return buildCobradorPerformanceContracts(byKey), nil
}

// ─── ListarCuentasRiesgo ──────────────────────────────────────────────────────

// ListarCuentasRiesgo returns actionable at-risk accounts — clients with
// outstanding balance — enriched with their cobranza tier and RFM segment.
//
// Each account is enriched at read time using:
//   - computeCobranzaTier: TierRiesgo from their payment cadence (AL_DIA /
//     VIGILANCIA / EN_RIESGO / CRITICO).
//   - computeSegmentoScore: RFM segment (ACTIVO, DORMIDO_VALIOSO, etc.).
//   - estadoPagoFor: solvency signal.
//   - ajustarCobranzaRecencia: recency-adjusted atraso + puntualidad.
//
// Accounts with Saldo == 0 are excluded (not at risk for collection).
// Results are sorted by tier severity DESC (CRITICO first), then Saldo DESC.
func (s *Service) ListarCuentasRiesgo(ctx context.Context, p CarteraParams) ([]analytics.CuentaRiesgoContract, error) {
	const source = "analytics.ListarCuentasRiesgo"

	page, err := s.repo.ListCandidatos(ctx, outbound.ListWinbackParams{
		Zona:           p.Zona,
		ExcluirControl: false,
		Limit:          0,
	})
	if err != nil {
		return nil, apperror.NewInternal("cartera_cuentas_riesgo_failed", "error al listar cuentas en riesgo").
			WithSource(source).WithError(err)
	}

	now := s.clock.Now()

	result := make([]analytics.CuentaRiesgoContract, 0)
	for _, c := range page.Items {
		// Only at-risk accounts (positive outstanding balance).
		if !c.Saldo().IsPositive() {
			continue
		}

		tier := computeCobranzaTier(c, now)
		seg, _, _, ep := computeSegmentoScore(c, now)
		diasAtraso, pctATiempo := ajustarCobranzaRecencia(c, now)

		result = append(result, analytics.CuentaRiesgoContract{
			ClienteID:       c.ClienteID(),
			Nombre:          c.Nombre(),
			Zona:            c.Zona(),
			TierRiesgo:      string(tier),
			Segmento:        string(seg),
			EstadoPago:      string(ep),
			Saldo:           c.Saldo(),
			DiasAtrasoProm:  diasAtraso,
			PctPagosATiempo: pctATiempo,
			CadenciaDias:    c.CadenciaDias(),
			FechaUltimoPago: c.FechaUltimoPago(),
			FechaProxPago:   c.FechaProxPago(),
		})
	}

	// Sort: CRITICO > EN_RIESGO > VIGILANCIA > AL_DIA; tie-break by Saldo DESC.
	tierOrdinal := func(t string) int {
		switch t {
		case string(domain.TierRiesgoCritico):
			return 3
		case string(domain.TierRiesgoEnRiesgo):
			return 2
		case string(domain.TierRiesgoVigilancia):
			return 1
		default:
			return 0
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		oi, oj := tierOrdinal(result[i].TierRiesgo), tierOrdinal(result[j].TierRiesgo)
		if oi != oj {
			return oi > oj
		}
		return result[i].Saldo.GreaterThan(result[j].Saldo)
	})
	return result, nil
}

// ─── ObtenerCumplimiento ──────────────────────────────────────────────────────

// ObtenerCumplimiento returns the expected-payment compliance distribution for
// active-credit clients using domain.CumplimientoEsperado.
//
// Each client (Saldo > 0) is classified into:
//   - AL_CORRIENTE: next payment not yet due (or no balance)
//   - VENCIDO_LEVE: 1–30 days past their expected next payment
//   - VENCIDO: >30 days past due, or next payment date unknown with saldo > 0
//
// Clients with Saldo == 0 are AL_CORRIENTE by definition (CumplimientoEsperado
// returns AL_CORRIENTE for saldo <= 0) but are excluded from the count to keep
// the distribution focused on credit-bearing accounts.
func (s *Service) ObtenerCumplimiento(ctx context.Context, p CarteraParams) (analytics.CumplimientoDistContract, error) {
	const source = "analytics.ObtenerCumplimiento"

	page, err := s.repo.ListCandidatos(ctx, outbound.ListWinbackParams{
		Zona:           p.Zona,
		ExcluirControl: false,
		Limit:          0,
	})
	if err != nil {
		return analytics.CumplimientoDistContract{}, apperror.NewInternal("cartera_cumplimiento_failed", "error al obtener cumplimiento").
			WithSource(source).WithError(err)
	}

	now := s.clock.Now()

	var alCorriente, vencidoLeve, vencido int
	for _, c := range page.Items {
		if !c.Saldo().IsPositive() {
			continue // exclude zero-balance accounts
		}
		ec := domain.CumplimientoEsperado(now, c.FechaProxPago(), c.Saldo())
		switch ec {
		case domain.EstadoCumplimientoAlCorriente:
			alCorriente++
		case domain.EstadoCumplimientoVencidoLeve:
			vencidoLeve++
		case domain.EstadoCumplimientoVencido:
			vencido++
		}
	}

	return analytics.CumplimientoDistContract{
		AlCorriente: alCorriente,
		VencidoLeve: vencidoLeve,
		Vencido:     vencido,
		Total:       alCorriente + vencidoLeve + vencido,
	}, nil
}

// ─── MargenReal ───────────────────────────────────────────────────────────────

// MargenReal returns the detailed breakdown of the margin proxy computation
// for the portfolio or a filtered subset.
//
// v1 formula:
//
//	Ventas          = ImporteColectado (period cash collected via CEI)
//	MargenBruto     = MargenVerificado (0.528) × Ventas
//	PerdidaEsperada = PAR × SaldoTotal × LGD (0.70)
//	MargenReal      = MargenBruto − PerdidaEsperada (floored at 0)
//
// "Ventas" proxy = ImporteColectado, NOT total historic Monetary, because we
// need a period revenue figure. Cash collected in a period ≈ periodic revenue.
//
// LGD assumption: 70% of the moroso balance (PAR × SaldoTotal) is unrecoverable.
// Replace with an empirical write-off rate once historical data is available.
func (s *Service) MargenReal(ctx context.Context, p CarteraParams) (analytics.MargenRealContract, error) {
	const source = "analytics.MargenReal"

	if s.carteraRepo == nil {
		return analytics.MargenRealContract{}, apperror.NewInternal("cartera_repo_nil", "el repo de cartera no está configurado").
			WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	today := s.clock.Now()

	// Aging for PAR + SaldoTotal.
	agingRows, err := s.carteraRepo.AgingSaldosByZona(ctx, today)
	if err != nil {
		return analytics.MargenRealContract{}, apperror.NewInternal("cartera_aging_failed", "error al obtener aging de cartera").
			WithSource(source).WithError(err)
	}
	agingRows = filterAgingByZona(agingRows, p.ZonaClienteID)
	saldoTotal, saldoMoroso, _, _ := aggregateAgingTotals(agingRows)
	par := domain.PARRatio(saldoMoroso, saldoTotal)

	// CEI for Ventas proxy.
	desde, hasta := resolveCEIPeriod(p, today)
	ceiRows, err := s.carteraRepo.ColeccionCEI(ctx, desde, hasta)
	if err != nil {
		return analytics.MargenRealContract{}, apperror.NewInternal("cartera_cei_failed", "error al obtener colección CEI").
			WithSource(source).WithError(err)
	}
	ceiRows = filterCEIByZona(ceiRows, p.ZonaClienteID)
	ventas := sumCEIImporte(ceiRows)

	margenBruto, perdida, margenReal := computeMargenRealDecimal(ventas, par, saldoTotal)

	return analytics.MargenRealContract{
		Ventas:          ventas,
		MargenBruto:     margenBruto,
		PerdidaEsperada: perdida,
		MargenReal:      margenReal,
		PAR:             par,
		SaldoTotal:      saldoTotal,
		LGD:             lgdCarteraDecimal,
	}, nil
}
