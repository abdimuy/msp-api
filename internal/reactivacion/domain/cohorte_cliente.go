//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// CohorteCliente is a snapshot entity: one row per cliente enrolled in the
// reactivación piloto, frozen at cohort-build time. It maps 1:1 to a row in
// MSP_RX_COHORTE.
//
// The control/contact assignment (EnControl, FueContactado) and the cohort date
// (CohorteFecha) are fixed at the first INSERT and never rewritten — the upsert
// preserves them across rebuilds so the A/B split and the channel's contact flag
// survive. FechaUltimaCompraBase is the baseline against which attribution later
// measures "enganche" (a new purchase strictly after the cohort date).
//
// Type classification: snapshot projection (does not fit Type A/B/C — it is a
// materialized experiment cohort, not a user- or pipeline-mutated aggregate).
type CohorteCliente struct {
	id                    uuid.UUID
	clienteID             int
	nombre                string
	telefono              string
	segmento              Segmento
	enControl             bool
	fueContactado         bool
	cohorteFecha          time.Time
	fechaUltimaCompraBase time.Time
	saldo                 decimal.Decimal
	porLiquidarPct        decimal.Decimal
	timestamps            audit.Timestamped
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearCohorteClienteParams groups all inputs for CrearCohorteCliente. Pass Now
// explicitly so the constructor is deterministic and unit-testable (domain code
// must never call time.Now() internally).
type CrearCohorteClienteParams struct {
	ClienteID             int
	Nombre                string
	Telefono              string
	Segmento              Segmento
	EnControl             bool
	FueContactado         bool
	CohorteFecha          time.Time
	FechaUltimaCompraBase time.Time // may be zero if the client has no purchase history
	Saldo                 decimal.Decimal
	PorLiquidarPct        decimal.Decimal
	Now                   time.Time
}

// CrearCohorteCliente validates all invariants, generates a new UUID, and
// returns a fresh CohorteCliente ready to be persisted. Returns the first
// invariant violation encountered.
//
// Invariants:
//   - ClienteID > 0
//   - Segmento must be one of the canonical values
//   - Saldo >= 0
//   - CohorteFecha must not be zero
func CrearCohorteCliente(p CrearCohorteClienteParams) (*CohorteCliente, error) {
	if p.ClienteID <= 0 {
		return nil, ErrCohorteClienteIDInvalido
	}
	if !p.Segmento.Valido() {
		return nil, ErrSegmentoInvalido
	}
	if p.Saldo.IsNegative() {
		return nil, ErrCohorteSaldoInvalido
	}
	if p.CohorteFecha.IsZero() {
		return nil, ErrCohorteFechaInvalida
	}

	var fechaUltimaCompraBase time.Time
	if !p.FechaUltimaCompraBase.IsZero() {
		fechaUltimaCompraBase = p.FechaUltimaCompraBase.UTC()
	}

	return &CohorteCliente{
		id:                    uuid.New(),
		clienteID:             p.ClienteID,
		nombre:                p.Nombre,
		telefono:              p.Telefono,
		segmento:              p.Segmento,
		enControl:             p.EnControl,
		fueContactado:         p.FueContactado,
		cohorteFecha:          p.CohorteFecha.UTC(),
		fechaUltimaCompraBase: fechaUltimaCompraBase,
		saldo:                 p.Saldo,
		porLiquidarPct:        p.PorLiquidarPct,
		timestamps:            audit.NewTimestamped(p.Now),
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateCohorteClienteParams groups all fields for reconstructing a
// CohorteCliente from a persisted row. Used exclusively by the repository.
type HydrateCohorteClienteParams struct {
	ID                    uuid.UUID
	ClienteID             int
	Nombre                string
	Telefono              string
	Segmento              Segmento
	EnControl             bool
	FueContactado         bool
	CohorteFecha          time.Time
	FechaUltimaCompraBase time.Time
	Saldo                 decimal.Decimal
	PorLiquidarPct        decimal.Decimal
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// HydrateCohorteCliente reconstructs a CohorteCliente from persistence with zero
// validation. Called only from the repository layer.
func HydrateCohorteCliente(p HydrateCohorteClienteParams) *CohorteCliente {
	return &CohorteCliente{
		id:                    p.ID,
		clienteID:             p.ClienteID,
		nombre:                p.Nombre,
		telefono:              p.Telefono,
		segmento:              p.Segmento,
		enControl:             p.EnControl,
		fueContactado:         p.FueContactado,
		cohorteFecha:          p.CohorteFecha,
		fechaUltimaCompraBase: p.FechaUltimaCompraBase,
		saldo:                 p.Saldo,
		porLiquidarPct:        p.PorLiquidarPct,
		timestamps:            audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's UUID.
func (c *CohorteCliente) ID() uuid.UUID { return c.id }

// ClienteID returns the Microsip cliente ID. Always > 0.
func (c *CohorteCliente) ClienteID() int { return c.clienteID }

// Nombre returns the cliente's display name (may be empty).
func (c *CohorteCliente) Nombre() string { return c.nombre }

// Telefono returns the contact phone number (may be empty).
func (c *CohorteCliente) Telefono() string { return c.telefono }

// Segmento returns the universe slice this cliente belongs to.
func (c *CohorteCliente) Segmento() Segmento { return c.segmento }

// EnControl returns true when this cliente belongs to the control group
// (never contacted; used as the A/B baseline).
func (c *CohorteCliente) EnControl() bool { return c.enControl }

// FueContactado returns true once the channel (Fase 3) has reached this cliente.
// Always false in Fase 1.
func (c *CohorteCliente) FueContactado() bool { return c.fueContactado }

// CohorteFecha returns the UTC cohort assignment date. Never zero.
func (c *CohorteCliente) CohorteFecha() time.Time { return c.cohorteFecha }

// FechaUltimaCompraBase returns the UTC last-purchase date recorded when the
// cohort was built. Zero when the client had no purchase history at that moment.
func (c *CohorteCliente) FechaUltimaCompraBase() time.Time { return c.fechaUltimaCompraBase }

// Saldo returns the outstanding balance at cohort-build time. Always >= 0.
func (c *CohorteCliente) Saldo() decimal.Decimal { return c.saldo }

// PorLiquidarPct returns the percentage of the original PRECIO_TOTAL still
// pending. Zero for recien_liquidado clients.
func (c *CohorteCliente) PorLiquidarPct() decimal.Decimal { return c.porLiquidarPct }

// CreatedAt returns the UTC timestamp when the cohort row was first created.
func (c *CohorteCliente) CreatedAt() time.Time { return c.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last cohort rebuild that touched
// this row.
func (c *CohorteCliente) UpdatedAt() time.Time { return c.timestamps.UpdatedAt() }
