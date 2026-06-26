// Package domain — cartera.go provides pure mathematical functions for
// credit-portfolio analytics.
//
// All functions receive `now time.Time` as an explicit parameter and never call
// time.Now() internally, ensuring determinism and testability (CLAUDE.md §1).
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// ─── AgingBucket VO ───────────────────────────────────────────────────────────

// AgingBucket is the bucket label for a credit account's days-past-due aging
// classification. String values are IDENTICAL to the canonical constants defined
// in cartera_snapshot.go and used in MSP_AN_CARTERA_SNAPSHOT — never use raw
// string literals; always use the AgingBucket* or BucketAgingDias* constants.
type AgingBucket string

// The four canonical aging buckets. String values mirror the BucketAgingDias*
// constants in cartera_snapshot.go so both packages refer to the same DB values.
const (
	AgingBucket0_30   AgingBucket = BucketAgingDias0_30   // "0-30"
	AgingBucket31_60  AgingBucket = BucketAgingDias31_60  // "31-60"
	AgingBucket61_90  AgingBucket = BucketAgingDias61_90  // "61-90"
	AgingBucket90Plus AgingBucket = BucketAgingDias90Plus // "90+"
)

// bucketOrdinals maps each AgingBucket to its severity ordinal (0 = least
// delinquent, 3 = most delinquent). Used for roll-rate arithmetic.
var bucketOrdinals = map[AgingBucket]int{
	AgingBucket0_30:   0,
	AgingBucket31_60:  1,
	AgingBucket61_90:  2,
	AgingBucket90Plus: 3,
}

// bucketMaxOrdinal is the maximum ordinal across all buckets. Used to normalize
// the roll-rate result to [-1, +1].
const bucketMaxOrdinal = 3

// BucketForDays returns the aging bucket for a credit account that is dias
// days past its payment due date.
//
// Boundary mapping (inclusive on both ends):
//
//	 dias < 0  → treated as 0 (negative values have no credit meaning; clamp to current)
//	[0,  30]  → AgingBucket0_30   ("0-30")
//	[31, 60]  → AgingBucket31_60  ("31-60")
//	[61, 90]  → AgingBucket61_90  ("61-90")
//	[91, ∞)   → AgingBucket90Plus ("90+")
//
// Boundary note: 90 lands in the "61-90" bucket; 91 is the first day in "90+".
// The name "90+" is an industry convention meaning ">90" when the "61-90" bucket
// already captures day 90.
func BucketForDays(dias int) AgingBucket {
	if dias < 0 {
		dias = 0
	}
	switch {
	case dias <= 30:
		return AgingBucket0_30
	case dias <= 60:
		return AgingBucket31_60
	case dias <= 90:
		return AgingBucket61_90
	default:
		return AgingBucket90Plus
	}
}

// ─── PAR Ratio ────────────────────────────────────────────────────────────────

// PARRatio computes the Portfolio-at-Risk ratio: the fraction of total
// outstanding balance (saldoTotal) that is delinquent (saldoMoroso).
//
// Invariants:
//   - Result is clamped to [0, 1] (decimal).
//   - When saldoTotal is zero or negative, returns 0 (safe default — division
//     by zero is avoided and an empty portfolio has no risk).
//   - Negative saldoMoroso is treated as zero (clamped via ratio < 0 check).
//   - saldoMoroso > saldoTotal is legal input but the ratio is clamped to 1.
func PARRatio(saldoMoroso, saldoTotal decimal.Decimal) decimal.Decimal {
	if !saldoTotal.IsPositive() {
		return decimal.Zero
	}
	ratio := saldoMoroso.Div(saldoTotal)
	if ratio.IsNegative() {
		return decimal.Zero
	}
	one := decimal.NewFromInt(1)
	if ratio.GreaterThan(one) {
		return one
	}
	return ratio
}

// ─── Vintage Cohort ───────────────────────────────────────────────────────────

// VintageCohort returns the month ordinal for fechaCargo, computed as
//
//	year*12 + int(month)
//
// where month is 1–12. This produces a monotonically increasing integer that
// uniquely identifies a calendar month and supports cohort-age arithmetic via
// simple subtraction (CohortAgeMonths).
//
// Returns 0 for the zero time.Time ("unknown / not set").
func VintageCohort(fechaCargo time.Time) int {
	if fechaCargo.IsZero() {
		return 0
	}
	return fechaCargo.Year()*12 + int(fechaCargo.Month())
}

// CohortAgeMonths returns the age in whole calendar months of a credit account
// whose first cargo was recorded on fechaCargo, measured from now.
//
// Uses VintageCohort arithmetic so the result is always a whole-month count
// independent of DST, timezone drift, or variable month lengths.
//
// A negative result (fechaCargo in the future relative to now) is clamped to 0.
// Zero fechaCargo is treated as "unknown": CohortAgeMonths returns 0.
func CohortAgeMonths(fechaCargo, now time.Time) int {
	if fechaCargo.IsZero() {
		return 0
	}
	age := VintageCohort(now) - VintageCohort(fechaCargo)
	if age < 0 {
		return 0
	}
	return age
}

// ─── Roll Rate ────────────────────────────────────────────────────────────────

// RollRate computes the signed net delinquency migration rate between two
// consecutive portfolio aging distributions (prev → curr).
//
// Contract:
//
//  1. Assign a severity ordinal to each bucket: 0-30→0, 31-60→1, 61-90→2, 90+→3.
//  2. Compute the balance-weighted average ordinal for each distribution:
//     w = Σ(ordinal[b] × saldo[b]) / Σ(saldo[b])
//  3. RollRate = (w_curr − w_prev) / 3
//
// Result range: [−1.0, +1.0].
//   - Positive → net deterioration (balance migrated to worse/higher-ordinal buckets).
//   - Negative → net improvement  (balance migrated to better/lower-ordinal buckets).
//   - Zero     → no net migration, or either distribution is empty (zero total saldo).
//
// Monotonic property: shifting all balance from bucket i to bucket i+1 (worsening
// by exactly one step) always increases RollRate by exactly 1/3 ≈ 0.333, regardless
// of starting position. This is the core invariant tested by the property suite.
//
// Negative saldo entries are treated as zero (clamped) to prevent poisoning the
// weighted average with nonsensical values.
//
// Practical interpretation: a RollRate of +0.33 means the portfolio's average
// delinquency severity worsened by one full bucket equivalent in the window.
func RollRate(prev, curr map[AgingBucket]decimal.Decimal) float64 {
	prevTotal, prevWeighted := weightedBucketStats(prev)
	currTotal, currWeighted := weightedBucketStats(curr)

	if !prevTotal.IsPositive() || !currTotal.IsPositive() {
		return 0
	}

	prevMean := prevWeighted.Div(prevTotal).InexactFloat64()
	currMean := currWeighted.Div(currTotal).InexactFloat64()

	// Both means are in [0, bucketMaxOrdinal] because all ordinals are in that
	// range and the weights (saldo) are non-negative after clamping. The
	// division keeps the result in [-1, +1] by construction.
	return (currMean - prevMean) / float64(bucketMaxOrdinal)
}

// weightedBucketStats computes the total saldo and the sum of (ordinal × saldo)
// across all buckets in dist. Negative saldo values are clamped to zero before
// accumulation. Used exclusively by RollRate.
func weightedBucketStats(dist map[AgingBucket]decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	total := decimal.Zero
	weighted := decimal.Zero
	for b, saldo := range dist {
		if saldo.IsNegative() {
			saldo = decimal.Zero
		}
		ordinal := bucketOrdinals[b] // unknown buckets map to 0 via zero value
		total = total.Add(saldo)
		weighted = weighted.Add(decimal.NewFromInt(int64(ordinal)).Mul(saldo))
	}
	return total, weighted
}

// ─── Cumplimiento Esperado ────────────────────────────────────────────────────

// EstadoCumplimiento classifies a credit account's compliance with its next
// expected payment date. This is a FORWARD-LOOKING measure (has the next
// scheduled payment been missed?) — distinct from EstadoPago (which is
// BACKWARD-LOOKING: days since the last payment that was actually made).
//
// The compliance status is derived purely from (now, proxPago, saldo):
// no repository calls, no historical payment records, no network I/O.
type EstadoCumplimiento string

const (
	// EstadoCumplimientoAlCorriente denotes a client with zero or negative saldo
	// (nothing to comply with), OR whose next payment date has not yet passed.
	EstadoCumplimientoAlCorriente EstadoCumplimiento = "AL_CORRIENTE"

	// EstadoCumplimientoVencidoLeve denotes a client who has outstanding balance
	// and whose next payment date passed between 1 and 30 days ago (mild overdue).
	// The 30-day boundary mirrors the 0-30 aging bucket so portfolio metrics stay
	// consistent across both views.
	EstadoCumplimientoVencidoLeve EstadoCumplimiento = "VENCIDO_LEVE"

	// EstadoCumplimientoVencido denotes a client who has outstanding balance and
	// whose next payment date passed more than 30 days ago, OR whose proxPago is
	// zero/unknown while saldo > 0 (conservatively classified as delinquent).
	EstadoCumplimientoVencido EstadoCumplimiento = "VENCIDO"
)

// cumplimientoLeveDias is the upper boundary (inclusive) for VENCIDO_LEVE.
// Accounts overdue by 1–cumplimientoLeveDias days are leve; beyond that → VENCIDO.
// Matches the width of the AgingBucket0_30 bucket (30 days) for consistency.
const cumplimientoLeveDias = 30

// CumplimientoEsperado returns the compliance status for a credit account based
// on the next expected payment date (proxPago), the current outstanding balance
// (saldo), and the current wall-clock instant (now).
//
// Rules (evaluated in order):
//  1. saldo <= 0                          → AL_CORRIENTE (no balance to comply with).
//  2. proxPago.IsZero()                   → VENCIDO (balance present but no scheduled
//     date; conservatively classified as delinquent).
//  3. proxPago >= now (not yet past)      → AL_CORRIENTE (on time or due today).
//  4. 1 ≤ days_past_proxPago ≤ 30        → VENCIDO_LEVE.
//  5. days_past_proxPago > 30            → VENCIDO.
//
// Note on "same day": if proxPago falls on the same calendar day as now but
// earlier in the day, !proxPago.Before(now) is false. The integer truncation
// (hours/24) yields 0 days past due → VENCIDO_LEVE (mildest non-compliant state).
// This is a conservative choice: a payment due earlier today and not yet received
// should prompt a reminder, not silence.
//
// now must be supplied by the caller — this function never calls time.Now()
// (determinism per CLAUDE.md §1).
func CumplimientoEsperado(now, proxPago time.Time, saldo decimal.Decimal) EstadoCumplimiento {
	if !saldo.IsPositive() {
		return EstadoCumplimientoAlCorriente
	}
	if proxPago.IsZero() {
		return EstadoCumplimientoVencido
	}
	// proxPago >= now: not yet due (or due at this exact nanosecond).
	if !proxPago.Before(now) {
		return EstadoCumplimientoAlCorriente
	}
	// proxPago.Before(now) is true here, so now.Sub(proxPago) > 0.
	diasVencido := int(now.Sub(proxPago).Hours() / 24)
	if diasVencido <= cumplimientoLeveDias {
		return EstadoCumplimientoVencidoLeve
	}
	return EstadoCumplimientoVencido
}

// ParseEstadoCumplimiento parses s into an EstadoCumplimiento or returns
// ErrEstadoCumplimientoInvalido. Input must match the exact UPPERCASE canonical form.
func ParseEstadoCumplimiento(s string) (EstadoCumplimiento, error) {
	ec := EstadoCumplimiento(s)
	if !ec.IsValid() {
		return "", ErrEstadoCumplimientoInvalido
	}
	return ec, nil
}

// IsValid reports whether ec is a recognized EstadoCumplimiento value.
func (ec EstadoCumplimiento) IsValid() bool {
	switch ec {
	case EstadoCumplimientoAlCorriente,
		EstadoCumplimientoVencidoLeve,
		EstadoCumplimientoVencido:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (ec EstadoCumplimiento) String() string { return string(ec) }
