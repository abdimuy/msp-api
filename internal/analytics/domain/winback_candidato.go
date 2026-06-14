//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// WinbackCandidato is a READ-MODEL projection that materializes the
// historical anclas (facts) about a cliente who is a candidate for
// re-engagement (winback). It is NOT a mutable aggregate:
//   - No mutation methods (fields are fixed at projection time).
//   - No domain events.
//   - No version field.
//   - No MicrosipSync (not pushed to Microsip).
//
// Recency, segment, and score are computed at read time in the app layer;
// they are NOT stored here. The entity maps 1:1 to the
// MSP_AN_WINBACK_CANDIDATOS table rows.
//
// Type classification: read-model (does not fit Type A/B/C — the entity is a
// recomputed projection, not created or mutated by users or pipelines).
type WinbackCandidato struct {
	id                uuid.UUID
	clienteID         int
	nombre            string
	zona              string
	telefono          string
	fechaUltimaCompra time.Time
	frecuencia        int
	monetary          decimal.Decimal
	saldo             decimal.Decimal
	porLiquidarPct    decimal.Decimal
	nextBestProduct   string
	enControl         bool
	cohorteFecha      time.Time
	timestamps        audit.Timestamped
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearWinbackCandidatoParams groups all inputs for CrearWinbackCandidato.
// Pass Now explicitly so the constructor is deterministic and unit-testable
// (domain code must never call time.Now() internally).
type CrearWinbackCandidatoParams struct {
	ClienteID         int
	Nombre            string
	Zona              string
	Telefono          string
	FechaUltimaCompra time.Time // may be zero if unknown
	Frecuencia        int
	Monetary          decimal.Decimal
	Saldo             decimal.Decimal
	PorLiquidarPct    decimal.Decimal
	NextBestProduct   string
	EnControl         bool
	CohorteFecha      time.Time
	Now               time.Time
}

// CrearWinbackCandidato validates all invariants, generates a new UUID, and
// returns a fresh WinbackCandidato ready to be persisted. Returns the first
// invariant violation encountered.
//
// Invariants:
//   - Frecuencia >= 0
//   - Monetary >= 0
//   - Saldo >= 0
//   - CohorteFecha must not be zero
func CrearWinbackCandidato(p CrearWinbackCandidatoParams) (*WinbackCandidato, error) {
	if p.Frecuencia < 0 {
		return nil, ErrWinbackCandidatoFrecuenciaInvalida
	}
	if p.Monetary.IsNegative() {
		return nil, ErrWinbackCandidatoMontoInvalido
	}
	if p.Saldo.IsNegative() {
		return nil, ErrWinbackCandidatoSaldoInvalido
	}
	if p.CohorteFecha.IsZero() {
		return nil, ErrWinbackCandidatoCohorteInvalida
	}

	var fechaUltimaCompra time.Time
	if !p.FechaUltimaCompra.IsZero() {
		fechaUltimaCompra = p.FechaUltimaCompra.UTC()
	}

	return &WinbackCandidato{
		id:                uuid.New(),
		clienteID:         p.ClienteID,
		nombre:            p.Nombre,
		zona:              p.Zona,
		telefono:          p.Telefono,
		fechaUltimaCompra: fechaUltimaCompra,
		frecuencia:        p.Frecuencia,
		monetary:          p.Monetary,
		saldo:             p.Saldo,
		porLiquidarPct:    p.PorLiquidarPct,
		nextBestProduct:   p.NextBestProduct,
		enControl:         p.EnControl,
		cohorteFecha:      p.CohorteFecha.UTC(),
		timestamps:        audit.NewTimestamped(p.Now),
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateWinbackCandidatoParams groups all fields for reconstructing a
// WinbackCandidato from a persisted row. Used exclusively by the repository.
type HydrateWinbackCandidatoParams struct {
	ID                uuid.UUID
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
	EnControl         bool
	CohorteFecha      time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// HydrateWinbackCandidato reconstructs a WinbackCandidato from persistence
// with zero validation. Called only from the repository layer.
func HydrateWinbackCandidato(p HydrateWinbackCandidatoParams) *WinbackCandidato {
	return &WinbackCandidato{
		id:                p.ID,
		clienteID:         p.ClienteID,
		nombre:            p.Nombre,
		zona:              p.Zona,
		telefono:          p.Telefono,
		fechaUltimaCompra: p.FechaUltimaCompra,
		frecuencia:        p.Frecuencia,
		monetary:          p.Monetary,
		saldo:             p.Saldo,
		porLiquidarPct:    p.PorLiquidarPct,
		nextBestProduct:   p.NextBestProduct,
		enControl:         p.EnControl,
		cohorteFecha:      p.CohorteFecha,
		timestamps:        audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's UUID.
func (w *WinbackCandidato) ID() uuid.UUID { return w.id }

// ClienteID returns the Microsip cliente ID.
func (w *WinbackCandidato) ClienteID() int { return w.clienteID }

// Nombre returns the cliente's display name (may be empty).
func (w *WinbackCandidato) Nombre() string { return w.nombre }

// Zona returns the sales zone (may be empty).
func (w *WinbackCandidato) Zona() string { return w.zona }

// Telefono returns the contact phone number (may be empty).
func (w *WinbackCandidato) Telefono() string { return w.telefono }

// FechaUltimaCompra returns the UTC timestamp of the cliente's last purchase.
// Returns the zero time.Time if no purchase history is available.
func (w *WinbackCandidato) FechaUltimaCompra() time.Time { return w.fechaUltimaCompra }

// Frecuencia returns the total count of purchases. Always >= 0.
func (w *WinbackCandidato) Frecuencia() int { return w.frecuencia }

// Monetary returns the total monetary value of all purchases. Always >= 0.
func (w *WinbackCandidato) Monetary() decimal.Decimal { return w.monetary }

// Saldo returns the outstanding balance owed. Always >= 0.
func (w *WinbackCandidato) Saldo() decimal.Decimal { return w.saldo }

// PorLiquidarPct returns the percentage of balance still pending payment.
// May be zero when fully paid. It is a trusted value carried straight from the
// Microsip projection (DOCTOS_CC), so the constructor does not validate its
// range — unlike Monetary/Saldo, no business invariant is asserted here.
func (w *WinbackCandidato) PorLiquidarPct() decimal.Decimal { return w.porLiquidarPct }

// NextBestProduct returns the recommended next product (may be empty).
func (w *WinbackCandidato) NextBestProduct() string { return w.nextBestProduct }

// EnControl returns true when this candidate belongs to the control group
// (excluded from the winback campaign for A/B measurement).
func (w *WinbackCandidato) EnControl() bool { return w.enControl }

// CohorteFecha returns the UTC cohort assignment date. Never zero.
func (w *WinbackCandidato) CohorteFecha() time.Time { return w.cohorteFecha }

// CreatedAt returns the UTC timestamp when the projection row was created.
func (w *WinbackCandidato) CreatedAt() time.Time { return w.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last projection refresh.
func (w *WinbackCandidato) UpdatedAt() time.Time { return w.timestamps.UpdatedAt() }
