//nolint:misspell // domain vocabulary is Spanish (venta, cobranza, parcialidad, enganche, vendedor) per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Venta is a read-only projection that combines an MSP_SALDOS_VENTAS row with
// the static cliente/dirección/cobrador/contrato fields the mobile cobranza
// app needs to render a route. The sync cursor is MSP_SALDOS_VENTAS.UPDATED_AT
// — so a cliente edit (name/address) only propagates when the cliente has
// saldo activity. That trade-off is documented in the module README.
//
// All fields are private; callers use the getter methods.
type Venta struct {
	doctoCCID      int
	doctoPVID      *int
	clienteID      int
	zonaClienteID  *int
	folio          string
	fechaCargo     time.Time
	precioTotal    decimal.Decimal
	totalImporte   decimal.Decimal
	impteRest      decimal.Decimal
	saldo          decimal.Decimal
	numPagos       int
	fechaUltPago   *time.Time
	cargoCancelado bool
	updatedAt      time.Time

	fechaVenta *time.Time

	clienteNombre  string
	limiteCredito  *decimal.Decimal
	clienteNotas   string
	cobradorID     *int
	nombreCobrador string

	zonaNombre string

	calle    string
	ciudad   string
	estado   string
	telefono string

	parcialidad           *int
	enganche              *decimal.Decimal
	tiempoCortoPlazoMeses *int
	montoCortoPlazo       *decimal.Decimal
	precioDeContado       *decimal.Decimal
	avalOResponsable      string
	vendedor1             string
	vendedor2             string
	vendedor3             string
	frecPago              string
}

// HydrateVentaParams carries the persisted shape of a Venta for repository
// reconstruction. Fields are assigned directly without recomputation; repos
// must guarantee the values came from the canonical JOIN query.
type HydrateVentaParams struct {
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

	FechaVenta *time.Time

	ClienteNombre  string
	LimiteCredito  *decimal.Decimal
	ClienteNotas   string
	CobradorID     *int
	NombreCobrador string

	ZonaNombre string

	Calle    string
	Ciudad   string
	Estado   string
	Telefono string

	Parcialidad           *int
	Enganche              *decimal.Decimal
	TiempoCortoPlazoMeses *int
	MontoCortoPlazo       *decimal.Decimal
	PrecioDeContado       *decimal.Decimal
	AvalOResponsable      string
	Vendedor1             string
	Vendedor2             string
	Vendedor3             string
	FrecPago              string
}

// HydrateVenta reconstructs an existing Venta from the JOIN query result.
func HydrateVenta(p HydrateVentaParams) Venta {
	return Venta{
		doctoCCID:             p.DoctoCCID,
		doctoPVID:             p.DoctoPVID,
		clienteID:             p.ClienteID,
		zonaClienteID:         p.ZonaClienteID,
		folio:                 p.Folio,
		fechaCargo:            p.FechaCargo,
		precioTotal:           p.PrecioTotal,
		totalImporte:          p.TotalImporte,
		impteRest:             p.ImpteRest,
		saldo:                 p.Saldo,
		numPagos:              p.NumPagos,
		fechaUltPago:          p.FechaUltPago,
		cargoCancelado:        p.CargoCancelado,
		updatedAt:             p.UpdatedAt,
		fechaVenta:            p.FechaVenta,
		clienteNombre:         p.ClienteNombre,
		limiteCredito:         p.LimiteCredito,
		clienteNotas:          p.ClienteNotas,
		cobradorID:            p.CobradorID,
		nombreCobrador:        p.NombreCobrador,
		zonaNombre:            p.ZonaNombre,
		calle:                 p.Calle,
		ciudad:                p.Ciudad,
		estado:                p.Estado,
		telefono:              p.Telefono,
		parcialidad:           p.Parcialidad,
		enganche:              p.Enganche,
		tiempoCortoPlazoMeses: p.TiempoCortoPlazoMeses,
		montoCortoPlazo:       p.MontoCortoPlazo,
		precioDeContado:       p.PrecioDeContado,
		avalOResponsable:      p.AvalOResponsable,
		vendedor1:             p.Vendedor1,
		vendedor2:             p.Vendedor2,
		vendedor3:             p.Vendedor3,
		frecPago:              p.FrecPago,
	}
}

// ─── Getters: saldo fields ──────────────────────────────────────────────────

// DoctoCCID returns the cargo's primary key in DOCTOS_CC.
func (v Venta) DoctoCCID() int { return v.doctoCCID }

// DoctoPVID returns the originating PV document ID, or nil when the cargo was
// created independently of a PV document.
func (v Venta) DoctoPVID() *int { return v.doctoPVID }

// ClienteID returns the cliente's primary key in CLIENTES.
func (v Venta) ClienteID() int { return v.clienteID }

// ZonaClienteID returns the cliente's zona, or nil when none.
func (v Venta) ZonaClienteID() *int { return v.zonaClienteID }

// Folio returns the cargo folio string.
func (v Venta) Folio() string { return v.folio }

// FechaCargo returns the cargo creation date in Microsip.
func (v Venta) FechaCargo() time.Time { return v.fechaCargo }

// PrecioTotal returns the original total price of the cargo.
func (v Venta) PrecioTotal() decimal.Decimal { return v.precioTotal }

// TotalImporte returns the sum of cobros applied to the cargo.
func (v Venta) TotalImporte() decimal.Decimal { return v.totalImporte }

// ImpteRest returns the sum of other deductions (enganches viejos, condonaciones, fugas).
func (v Venta) ImpteRest() decimal.Decimal { return v.impteRest }

// Saldo returns the outstanding balance.
func (v Venta) Saldo() decimal.Decimal { return v.saldo }

// NumPagos returns how many payment transactions have been applied.
func (v Venta) NumPagos() int { return v.numPagos }

// FechaUltPago returns the date of the most recent payment, or nil when none.
func (v Venta) FechaUltPago() *time.Time { return v.fechaUltPago }

// CargoCancelado reports whether the cargo has been cancelled in Microsip.
func (v Venta) CargoCancelado() bool { return v.cargoCancelado }

// UpdatedAt returns the timestamp of the last cache refresh.
func (v Venta) UpdatedAt() time.Time { return v.updatedAt }

// ─── Getters: PV column ─────────────────────────────────────────────────────

// FechaVenta returns the PV document's date when the cargo originated from
// one, or nil otherwise.
func (v Venta) FechaVenta() *time.Time { return v.fechaVenta }

// ─── Getters: cliente ───────────────────────────────────────────────────────

// ClienteNombre returns the cliente's name.
func (v Venta) ClienteNombre() string { return v.clienteNombre }

// LimiteCredito returns the cliente's authorised credit limit, or nil when
// the column was SQL NULL.
func (v Venta) LimiteCredito() *decimal.Decimal { return v.limiteCredito }

// ClienteNotas returns the cliente's free-form notes.
func (v Venta) ClienteNotas() string { return v.clienteNotas }

// CobradorID returns the ID of the cobrador assigned to the cliente.
func (v Venta) CobradorID() *int { return v.cobradorID }

// NombreCobrador returns the cobrador's display name.
func (v Venta) NombreCobrador() string { return v.nombreCobrador }

// ─── Getters: zona ──────────────────────────────────────────────────────────

// ZonaNombre returns the zona's display name.
func (v Venta) ZonaNombre() string { return v.zonaNombre }

// ─── Getters: dirección ─────────────────────────────────────────────────────

// Calle returns the street + number portion of the primary address.
func (v Venta) Calle() string { return v.calle }

// Ciudad returns the city/población of the primary address.
func (v Venta) Ciudad() string { return v.ciudad }

// Estado returns the state portion of the primary address.
func (v Venta) Estado() string { return v.estado }

// Telefono returns the cliente's primary phone number.
func (v Venta) Telefono() string { return v.telefono }

// ─── Getters: contrato (LIBRES_CARGOS_CC) ────────────────────────────────────

// Parcialidad returns the agreed payment amount per installment.
func (v Venta) Parcialidad() *int { return v.parcialidad }

// Enganche returns the agreed down payment.
func (v Venta) Enganche() *decimal.Decimal { return v.enganche }

// TiempoCortoPlazoMeses returns the short-term plan duration in months.
func (v Venta) TiempoCortoPlazoMeses() *int { return v.tiempoCortoPlazoMeses }

// MontoCortoPlazo returns the amount for the short-term plan.
func (v Venta) MontoCortoPlazo() *decimal.Decimal { return v.montoCortoPlazo }

// PrecioDeContado returns the equivalent cash price.
func (v Venta) PrecioDeContado() *decimal.Decimal { return v.precioDeContado }

// AvalOResponsable returns the guarantor or responsible party.
func (v Venta) AvalOResponsable() string { return v.avalOResponsable }

// Vendedor1 returns the display name of the first vendedor.
func (v Venta) Vendedor1() string { return v.vendedor1 }

// Vendedor2 returns the display name of the second vendedor.
func (v Venta) Vendedor2() string { return v.vendedor2 }

// Vendedor3 returns the display name of the third vendedor.
func (v Venta) Vendedor3() string { return v.vendedor3 }

// FrecPago returns the payment frequency (SEMANAL/QUINCENAL/MENSUAL).
func (v Venta) FrecPago() string { return v.frecPago }
