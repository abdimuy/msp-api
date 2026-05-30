// Package cobranza is the cross-module surface of the cobranza bounded context.
// Other modules import only this package — never internal/cobranza/domain,
// internal/cobranza/app, or internal/cobranza/infra. The depguard linter
// enforces the rule.
//
// The contract exports:
//   - Saldo: projected view of a MSP_SALDOS_VENTAS row.
//   - ResumenZona: aggregated view of open saldos by zona.
package cobranza

import (
	"time"

	"github.com/shopspring/decimal"
)

// Saldo is the projected, cross-module view of a cobranza cache row. It is
// intentionally a flat struct of primitive values so other modules can consume
// it without importing the cobranza domain types.
type Saldo struct {
	DoctoCCID      int
	DoctoPVID      *int
	ClienteID      int
	ZonaClienteID  *int
	Folio          string
	FechaCargo     time.Time
	PrecioTotal    decimal.Decimal
	TotalImporte   decimal.Decimal
	ImpteRest      decimal.Decimal
	Saldo          decimal.Decimal
	NumPagos       int
	FechaUltPago   *time.Time
	CargoCancelado bool
	UpdatedAt      time.Time
}

// ResumenZona is the projected, cross-module view of an aggregated zona
// summary. Other modules can use this to display dashboard-style totals
// without importing the cobranza domain.
type ResumenZona struct {
	ZonaID      int
	TotalVentas int
	SaldoTotal  decimal.Decimal
}

// Pago is the projected, cross-module view of a MSP_PAGOS_VENTAS row.
type Pago struct {
	ImpteDoctoCCID int
	DoctoCCID      int
	DoctoCCAcrID   int
	ClienteID      int
	ZonaClienteID  *int
	Folio          string
	ConceptoCCID   int
	Fecha          time.Time
	Importe        decimal.Decimal
	Impuesto       decimal.Decimal
	Lat            *decimal.Decimal
	Lon            *decimal.Decimal
	Cancelado      bool
	Aplicado       bool
	UpdatedAt      time.Time
}
