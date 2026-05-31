//nolint:misspell // domain vocabulary is Spanish (concepto, cargo, importe, etc.) per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Pago is a read-only projection of a MSP_PAGOS_VENTAS row. It is a value
// object: the materialized cache is the source of truth (computed by Microsip
// triggers via MSP_RECOMPUTE_PAGO); the entity merely carries the values into
// Go so the app layer can reason about them without issuing additional DB
// queries.
//
// All fields are private; callers use the getter methods.
type Pago struct {
	impteDoctoCCID int
	doctoCCID      int
	doctoCCAcrID   int
	clienteID      int
	zonaClienteID  *int
	folio          string
	conceptoCCID   int
	fecha          time.Time
	importe        decimal.Decimal
	impuesto       decimal.Decimal
	lat            *decimal.Decimal
	lon            *decimal.Decimal
	cancelado      bool
	aplicado       bool
	updatedAt      time.Time

	// Campos enriquecidos via JOINs en el sync. Las queries simples
	// (PorVenta/PorCliente/EnRutaPorZona del endpoint admin) los dejan en
	// zero values; solo SyncPorZona los llena.
	cobrador      string
	cobradorID    *int
	nombreCliente string
	formaCobroID  *int
}

// HydratePagoParams carries the persisted shape of a Pago for repository
// reconstruction. Fields are assigned directly without recomputation;
// repositories must guarantee the values were written by the Microsip trigger.
type HydratePagoParams struct {
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

	// Campos enriquecidos (solo /sync/pagos los llena).
	Cobrador      string
	CobradorID    *int
	NombreCliente string
	FormaCobroID  *int
}

// HydratePago reconstructs an existing Pago from persistent storage. It does
// NOT recompute or validate fields against each other — the cache row is the
// source of truth. Used by repos when reading MSP_PAGOS_VENTAS rows.
func HydratePago(p HydratePagoParams) Pago {
	return Pago{
		impteDoctoCCID: p.ImpteDoctoCCID,
		doctoCCID:      p.DoctoCCID,
		doctoCCAcrID:   p.DoctoCCAcrID,
		clienteID:      p.ClienteID,
		zonaClienteID:  p.ZonaClienteID,
		folio:          p.Folio,
		conceptoCCID:   p.ConceptoCCID,
		fecha:          p.Fecha,
		importe:        p.Importe,
		impuesto:       p.Impuesto,
		lat:            p.Lat,
		lon:            p.Lon,
		cancelado:      p.Cancelado,
		aplicado:       p.Aplicado,
		updatedAt:      p.UpdatedAt,
		cobrador:       p.Cobrador,
		cobradorID:     p.CobradorID,
		nombreCliente:  p.NombreCliente,
		formaCobroID:   p.FormaCobroID,
	}
}

// Cobrador returns DOCTOS_CC.DESCRIPCION (free-text cobrador name, what the
// Node legacy used) — populated only by /sync/pagos via JOIN.
func (p Pago) Cobrador() string { return p.cobrador }

// CobradorID returns COBRADORES.COBRADOR_ID — populated only by /sync/pagos.
func (p Pago) CobradorID() *int { return p.cobradorID }

// NombreCliente returns CLIENTES.NOMBRE — populated only by /sync/pagos.
func (p Pago) NombreCliente() string { return p.nombreCliente }

// FormaCobroID returns FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID — populated only by
// /sync/pagos. Distinct from ConceptoCCID; this maps to "efectivo / cheque /
// transferencia / etc." in the mobile app.
func (p Pago) FormaCobroID() *int { return p.formaCobroID }

// ─── Getters ────────────────────────────────────────────────────────────────

// ImpteDoctoCCID returns the PK of the importe in IMPORTES_DOCTOS_CC.
func (p Pago) ImpteDoctoCCID() int { return p.impteDoctoCCID }

// DoctoCCID returns the header document ID of the abono in DOCTOS_CC.
func (p Pago) DoctoCCID() int { return p.doctoCCID }

// DoctoCCAcrID returns the cargo ID that this pago acredits (=
// MSP_SALDOS_VENTAS.DOCTO_CC_ID).
func (p Pago) DoctoCCAcrID() int { return p.doctoCCAcrID }

// ClienteID returns the Microsip cliente ID.
func (p Pago) ClienteID() int { return p.clienteID }

// ZonaClienteID returns the zona assigned to the cliente at the time of the
// last cache refresh, or nil when none.
func (p Pago) ZonaClienteID() *int { return p.zonaClienteID }

// Folio returns the document folio string of the abono.
func (p Pago) Folio() string { return p.folio }

// ConceptoCCID returns the cobranza concept that classifies the pago (87327
// for cobranza en ruta, 155 for cobro mostrador, 11 for auto-pago contado,
// etc.).
func (p Pago) ConceptoCCID() int { return p.conceptoCCID }

// Fecha returns the pago timestamp. Precision is full TIMESTAMP when the
// pago was registered via the mobile app (MSP_PAGOS_RECIBIDOS); otherwise it
// degrades to DATE precision (header DOCTOS_CC.FECHA).
func (p Pago) Fecha() time.Time { return p.fecha }

// Importe returns the pago amount.
func (p Pago) Importe() decimal.Decimal { return p.importe }

// Impuesto returns the impuesto (tax) amount for the pago.
func (p Pago) Impuesto() decimal.Decimal { return p.impuesto }

// Lat returns the latitude where the pago was registered, when available.
// Reserved for future geofencing — currently always nil.
func (p Pago) Lat() *decimal.Decimal { return p.lat }

// Lon returns the longitude where the pago was registered, when available.
// Reserved for future geofencing — currently always nil.
func (p Pago) Lon() *decimal.Decimal { return p.lon }

// Cancelado reports whether the pago row was cancelled in Microsip.
func (p Pago) Cancelado() bool { return p.cancelado }

// Aplicado reports whether the pago has been aplicado (settled) — Microsip
// flag IMPORTES_DOCTOS_CC.APLICADO.
func (p Pago) Aplicado() bool { return p.aplicado }

// UpdatedAt returns the timestamp of the last cache refresh.
func (p Pago) UpdatedAt() time.Time { return p.updatedAt }
