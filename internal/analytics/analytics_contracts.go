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
}
