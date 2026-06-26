// Package domain — CarteraSnapshot entity for portfolio aging snapshots.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Canonical aging-bucket names for MSP_AN_CARTERA_SNAPSHOT.
// Task 2 (roll-rate / trend computation) MUST use these constants — never
// raw string literals — so that bucket names in the DB stay in sync with
// domain math across tasks.
const (
	BucketAgingDias0_30   = "0-30"
	BucketAgingDias31_60  = "31-60"
	BucketAgingDias61_90  = "61-90"
	BucketAgingDias90Plus = "90+"
)

// validAgingBuckets is the accepted set of BUCKET values.
var validAgingBuckets = map[string]bool{
	BucketAgingDias0_30:   true,
	BucketAgingDias31_60:  true,
	BucketAgingDias61_90:  true,
	BucketAgingDias90Plus: true,
}

// CarteraSnapshot stores a point-in-time distribution of credit-portfolio
// aging — per zone, cobrador, and aging bucket — used for roll-rate and
// trend analysis. One row equals one bucket in one cutoff for one
// (ZONA_CLIENTE_ID, COBRADOR_ID) pair.
//
// Type classification: system-generated read-model aggregate. No domain events,
// no version field, no MicrosipSync (not pushed to Microsip).
type CarteraSnapshot struct {
	id            uuid.UUID
	fechaCorte    time.Time
	zonaClienteID int
	cobradorID    *int // nil = zona-level aggregate without cobrador breakdown
	bucket        string
	saldo         decimal.Decimal
	conteo        int
	timestamps    audit.Timestamped
}

// ─── New constructor ──────────────────────────────────────────────────────────

// NewCarteraSnapshotParams groups all inputs for NewCarteraSnapshot.
// Now must be supplied by the caller — domain code never calls time.Now() directly
// (CLAUDE.md §1, determinism / testability).
type NewCarteraSnapshotParams struct {
	// FechaCorte is the cutoff timestamp for this snapshot row. Must be non-zero.
	FechaCorte time.Time
	// ZonaClienteID is the Microsip zone ID. Must be > 0.
	ZonaClienteID int
	// CobradorID is the collector ID. Nil when the row is a zone-level
	// aggregate without a per-cobrador breakdown.
	CobradorID *int
	// Bucket is the aging range label. Must be one of the BucketAgingDias* constants.
	Bucket string
	// Saldo is the total outstanding balance for this bucket. Must be >= 0.
	Saldo decimal.Decimal
	// Conteo is the count of clientes in this bucket. Must be >= 0.
	Conteo int
	// Now is the wall-clock instant supplied by the caller for audit timestamps.
	Now time.Time
}

// NewCarteraSnapshot validates all invariants, generates a new UUID, and
// returns a fresh CarteraSnapshot ready to be persisted. Returns the first
// invariant violation encountered.
//
// Invariants:
//   - FechaCorte must not be zero
//   - ZonaClienteID must be > 0
//   - Bucket must be one of the four canonical BucketAgingDias* values (non-empty)
//   - Saldo must be >= 0
//   - Conteo must be >= 0
func NewCarteraSnapshot(p NewCarteraSnapshotParams) (*CarteraSnapshot, error) {
	if p.FechaCorte.IsZero() {
		return nil, ErrCarteraSnapshotFechaCorteInvalida
	}
	if p.ZonaClienteID <= 0 {
		return nil, ErrCarteraSnapshotZonaInvalida
	}
	if !validAgingBuckets[p.Bucket] {
		return nil, ErrCarteraSnapshotBucketInvalido
	}
	if p.Saldo.IsNegative() {
		return nil, ErrCarteraSnapshotSaldoInvalido
	}
	if p.Conteo < 0 {
		return nil, ErrCarteraSnapshotConteoInvalido
	}

	return &CarteraSnapshot{
		id:            uuid.New(),
		fechaCorte:    p.FechaCorte.UTC(),
		zonaClienteID: p.ZonaClienteID,
		cobradorID:    p.CobradorID,
		bucket:        p.Bucket,
		saldo:         p.Saldo,
		conteo:        p.Conteo,
		timestamps:    audit.NewTimestamped(p.Now),
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateCarteraSnapshotParams groups all fields for reconstructing a
// CarteraSnapshot from a persisted row. Used exclusively by the repository.
type HydrateCarteraSnapshotParams struct {
	ID            uuid.UUID
	FechaCorte    time.Time
	ZonaClienteID int
	CobradorID    *int
	Bucket        string
	Saldo         decimal.Decimal
	Conteo        int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// HydrateCarteraSnapshot reconstructs a CarteraSnapshot from a persistence
// row without re-validation. Called only from the repository layer.
func HydrateCarteraSnapshot(p HydrateCarteraSnapshotParams) *CarteraSnapshot {
	return &CarteraSnapshot{
		id:            p.ID,
		fechaCorte:    p.FechaCorte,
		zonaClienteID: p.ZonaClienteID,
		cobradorID:    p.CobradorID,
		bucket:        p.Bucket,
		saldo:         p.Saldo,
		conteo:        p.Conteo,
		timestamps:    audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's UUID.
func (c *CarteraSnapshot) ID() uuid.UUID { return c.id }

// FechaCorte returns the UTC cutoff timestamp for this snapshot row.
func (c *CarteraSnapshot) FechaCorte() time.Time { return c.fechaCorte }

// ZonaClienteID returns the Microsip zone ID. Always > 0 after NewCarteraSnapshot.
func (c *CarteraSnapshot) ZonaClienteID() int { return c.zonaClienteID }

// CobradorID returns the collector ID, or nil for zone-level aggregates.
func (c *CarteraSnapshot) CobradorID() *int { return c.cobradorID }

// Bucket returns the aging-range label (one of the BucketAgingDias* constants).
func (c *CarteraSnapshot) Bucket() string { return c.bucket }

// Saldo returns the total outstanding balance in this bucket. Always >= 0
// after NewCarteraSnapshot.
func (c *CarteraSnapshot) Saldo() decimal.Decimal { return c.saldo }

// Conteo returns the count of clientes in this bucket. Always >= 0 after
// NewCarteraSnapshot.
func (c *CarteraSnapshot) Conteo() int { return c.conteo }

// CreatedAt returns the UTC timestamp when this snapshot row was created.
func (c *CarteraSnapshot) CreatedAt() time.Time { return c.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last update to this row.
func (c *CarteraSnapshot) UpdatedAt() time.Time { return c.timestamps.UpdatedAt() }
