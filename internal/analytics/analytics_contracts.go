// Package analytics is the cross-module surface of the analytics bounded context.
// Other modules import only this package — never internal/analytics/domain,
// internal/analytics/app, or internal/analytics/infra. The depguard linter
// enforces the rule.
//
// The contract exports:
//   - WinbackCandidatoContract: flat primitive view of a winback candidate,
//     intended for consumption by the future reactivacion module.
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
