// Package analytics is the cross-module surface of the analytics bounded context.
// Other modules import only this package — never internal/analytics/domain,
// internal/analytics/app, or internal/analytics/infra. The depguard linter
// enforces the rule.
//
// The contract exports:
//   - WinbackCandidatoContract: flat primitive view of a winback candidate,
//     intended for consumption by the future reactivacion module.
//   - ClientePulsoContract: flat primitive view of a client's analytics pulse
//     (score, segmento, estado_pago, recencia, RFM, next-best-product),
//     intended for consumption by the clientes hub module.
//
//nolint:misspell // Spanish domain vocabulary (clientes, segmento, etc.) by project convention.
package analytics

import (
	"time"

	"github.com/shopspring/decimal"
)

// WinbackCandidatoContract is the projected, cross-module view of a winback
// candidate. It is a flat struct of primitive values so other modules can
// consume it without importing the analytics domain types.
//
// Segmento and Score are enrichment fields computed at read time by the
// analytics app/HTTP layer. They are NOT populated by ToWinbackCandidatoContract
// (which projects the stored entity only). A caller that needs them must set
// them after mapping.
type WinbackCandidatoContract struct {
	ClienteID         int
	Nombre            string
	Zona              string
	Telefono          string
	FechaUltimaCompra time.Time
	Frecuencia        int
	Monetary          decimal.Decimal
	Saldo             decimal.Decimal
	PorLiquidarPct    decimal.Decimal
	NextBestProduct   string
	Segmento          string
	Score             int
	EnControl         bool
	// FechaUltimoPago is the most recent payment date. Zero when no history.
	FechaUltimoPago time.Time
	// EstadoPago is the payment-solvency signal. Left empty by the entity-only
	// mapper; callers that need it must set it after mapping (same pattern as
	// Segmento and Score).
	EstadoPago string
}

// ClientePulsoContract is the projected, cross-module view of a client's
// analytics pulse. It carries the scored/segmented snapshot for a single
// client as of the read time. Money fields are decimal.Decimal (exact) —
// the consuming module converts them to strings at its own HTTP boundary.
type ClientePulsoContract struct {
	ClienteID         int
	Score             int    // 0–100, computed at read time
	Segmento          string // domain.Segmento → string
	EstadoPago        string // domain.EstadoPago → string
	RecenciaDias      int    // days since last purchase; 9999 sentinel if none
	Frecuencia        int
	Monetary          decimal.Decimal
	Saldo             decimal.Decimal
	PorLiquidarPct    decimal.Decimal
	FechaUltimaCompra time.Time // zero if none
	FechaUltimoPago   time.Time // zero if none
	NextBestProduct   string

	// ─── Cobranza intelligence signals (Task B1) ─────────────────────────────────
	NumPagos        int
	CadenciaDias    int
	DiasAtrasoProm  int
	PctPagosATiempo decimal.Decimal
	FechaProxPago   time.Time // zero if no cadencia
	MontoProxPago   decimal.Decimal
	TierRiesgo      string // domain.TierRiesgo → string; computed at read time

	// ─── Credit risk score (Task R2) ─────────────────────────────────────────────
	ScoreCredito   int      // 0–100, higher = better payer; 0 when no aplica
	BandaCredito   string   // domain.BandaCredito → string; "" when no aplica (contado/sin historial)
	CreditoDrivers []string // top-3 risk reasons (Spanish labels); nil when no aplica

	// ─── Repurchase propensity score (Fase A) ────────────────────────────────────
	ScoreRecompra   int      // 0–100, higher = more likely to repurchase; 0 when no aplica
	BandaRecompra   string   // domain.BandaRecompra → string; "" when no aplica (sin historial de compras)
	RecompraDrivers []string // top-3 propensity drivers (Spanish labels); nil when no aplica

	// ─── CLV (Fase B) ────────────────────────────────────────────────────────────
	MontoCLV decimal.Decimal // risk-adjusted CLV in pesos; 0 when no aplica
	BandaCLV string          // ALTO|MEDIO|BAJO; "" when no aplica

	// ─── Quantified drivers and titulars (Fase R) ────────────────────────────────
	CLVDrivers      []string // quantified CLV drivers; nil when no aplica
	CreditoResumen  string   // titular crédito (always set, incl. "no aplica")
	RecompraResumen string   // titular recompra
	CLVResumen      string   // titular CLV

	// ─── Lectura del analista (IA) — Fase 2 ──────────────────────────────────────
	Narrativa string   // analyst paragraph (empty when LLM off / not yet generated)
	RasgosIA  []string // resolved Spanish display labels (empty when none)
	// ContextoOperativo is the operational signals the IA distilled from the
	// cobrador's note (Fase 2.1). Empty when none / LLM off.
	ContextoOperativo string
}

// MetricaBenchmark holds the benchmark stats for one scoring metric.
// Aplica indicates whether the target has a valid value for this metric.
type MetricaBenchmark struct {
	Aplica         bool    // el target tiene valor para esta métrica
	Valor          float64 // valor del target
	Percentil      float64 // 0..100 (solo si Aplica && !MuestraPequena)
	Mediana        float64
	P25            float64
	P75            float64
	N              int  // pares con valor aplicable para esta métrica
	MuestraPequena bool // N < benchmarkMuestraMinima
}

// BenchmarkContract is the cross-module view of a client's peer benchmark.
// It is produced by the analytics app layer and consumed by the clientes hub.
type BenchmarkContract struct {
	Disponible  bool   // target encontrado y con zona
	CohortBy    string // "zona" | "segmento" | "antiguedad"
	Zona        string
	N           int // tamaño de la cohorte base (tras sub-filtro, sin el target)
	Puntualidad MetricaBenchmark
	CLV         MetricaBenchmark // Valor/Mediana/P25/P75 en pesos
	Credito     MetricaBenchmark
	Recompra    MetricaBenchmark
}

// IntervaloContract is a point estimate with its credible interval [Lo, Hi].
type IntervaloContract struct {
	Punto float64
	Lo    float64
	Hi    float64
}

// PrediccionesContract is the cross-module view of a client's Bayesian predictions.
// CLV values are pesos (the HTTP layer formats them as money strings).
type PrediccionesContract struct {
	Disponible          bool
	PAlive              IntervaloContract // probability in [0,1]
	ComprasEsperadas12m IntervaloContract // expected repeat purchases next 12 months
	CLV                 IntervaloContract // pesos
	ProximaCompraDias   IntervaloContract // days until next purchase
	Draws               int
}

// ─── Cartera contracts (Task B4) ──────────────────────────────────────────────

// SaludCarteraContract is the executive KPI set for credit-portfolio health.
// Money fields are decimal.Decimal; the HTTP layer converts them to strings.
type SaludCarteraContract struct {
	SaldoTotal       decimal.Decimal // total outstanding balance
	SaldoMoroso      decimal.Decimal // balance in buckets 31-60, 61-90, 90+
	PAR              decimal.Decimal // Portfolio-at-Risk = SaldoMoroso / SaldoTotal [0,1]
	CEIRate          decimal.Decimal // Collection Effectiveness Index = ImporteColectado / SaldoTotal [0,1]
	ImporteColectado decimal.Decimal // total collected in the CEI period
	CuentasTotal     int             // count of active credit accounts
	CuentasEnMora    int             // count of accounts in buckets 31+
	MargenRealProxy  decimal.Decimal // MargenVerificado × ImporteColectado − PerdidaEsperada
}

// AgingBucketContract is one row of the aging-bucket distribution.
type AgingBucketContract struct {
	Bucket   string          // one of domain.BucketAgingDias* constants
	Saldo    decimal.Decimal // total outstanding balance in this bucket
	Conteo   int             // count of active accounts in this bucket
	PctSaldo decimal.Decimal // proportion of total saldo [0,1]; 0 when total is 0
}

// CosechaContract is one vintage cohort row.
// CohortMonth = year×12 + month (matching domain.VintageCohort).
type CosechaContract struct {
	CohortMonth int             // year*12 + month ordinal (e.g. 2026*12+6 = 24318)
	AgeMonths   int             // months since cohort origin relative to now
	Saldo       decimal.Decimal // outstanding balance from this cohort
	Conteo      int             // count of active accounts from this cohort
}

// CobradorPerformanceContract holds credit-portfolio performance metrics for
// one collector (cobrador). NOT a competitive leaderboard — just performance rows.
type CobradorPerformanceContract struct {
	CobradorID       int             // 0 = accounts with no cobrador assigned
	ZonaClienteID    int             // zone of this cobrador's portfolio
	CEI              decimal.Decimal // Collection Effectiveness Index [0,1]
	PAR              decimal.Decimal // Portfolio-at-Risk for their cartera [0,1]
	PctCorriente     decimal.Decimal // % of accounts in 0-30 (current) bucket [0,1]
	SaldoTotal       decimal.Decimal // total outstanding balance they manage
	SaldoMoroso      decimal.Decimal // balance in buckets 31+
	CuentasTotal     int             // total accounts managed
	ImporteColectado decimal.Decimal // collected in the CEI period
}

// CuentaRiesgoContract is one at-risk account enriched with its cobranza tier
// and RFM segment from the riesgo×disposición matrix.
type CuentaRiesgoContract struct {
	ClienteID       int
	Nombre          string
	Zona            string
	TierRiesgo      string // domain.TierRiesgo → string
	Segmento        string // domain.Segmento → string
	EstadoPago      string // domain.EstadoPago → string
	Saldo           decimal.Decimal
	DiasAtrasoProm  int             // read-time adjusted (includes open gap)
	PctPagosATiempo decimal.Decimal // read-time adjusted [0,100]
	CadenciaDias    int
	FechaUltimoPago time.Time // zero when no payment history
	FechaProxPago   time.Time // zero when no cadencia
}

// CumplimientoDistContract is the expected-payment compliance distribution
// across the active-credit portfolio, computed via domain.CumplimientoEsperado.
type CumplimientoDistContract struct {
	AlCorriente int // count in EstadoCumplimientoAlCorriente
	VencidoLeve int // count in EstadoCumplimientoVencidoLeve
	Vencido     int // count in EstadoCumplimientoVencido
	Total       int // AlCorriente + VencidoLeve + Vencido
}

// MargenRealContract holds the detailed breakdown of the margin proxy computation.
//
// v1 formula:
//
//	MargenBruto     = MargenVerificado (0.528) × Ventas
//	PerdidaEsperada = PAR × SaldoTotal × LGD (0.70)
//	MargenReal      = MargenBruto − PerdidaEsperada  (floored at 0)
//
// "Ventas" = ImporteColectado (period cash collected) — a revenue proxy.
// LGD = 0.70 (70% of delinquent balance is assumed unrecoverable; R1 constant).
type MargenRealContract struct {
	Ventas          decimal.Decimal // period cash collected (revenue proxy)
	MargenBruto     decimal.Decimal // MargenVerificado × Ventas
	PerdidaEsperada decimal.Decimal // PAR × SaldoTotal × LGD
	MargenReal      decimal.Decimal // MargenBruto − PerdidaEsperada (≥ 0)
	PAR             decimal.Decimal // PAR used in the formula
	SaldoTotal      decimal.Decimal // SaldoTotal used in the formula
	LGD             decimal.Decimal // LGD constant used (= 0.70)
}
