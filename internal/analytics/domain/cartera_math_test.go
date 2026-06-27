// Package domain_test — cartera_math_test.go tests pure mathematical functions
// defined in cartera.go: aging bucket lookup, PAR ratio, vintage cohort, roll-rate,
// and compliance status. Includes unit, property (rapid), and fuzz tests.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── BucketForDays ────────────────────────────────────────────────────────────

func TestBucketForDays_Boundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dias int
		want domain.AgingBucket
	}{
		// Negative → clamped to 0.
		{-1000, domain.AgingBucket0_30},
		{-1, domain.AgingBucket0_30},
		// 0-30 inclusive.
		{0, domain.AgingBucket0_30},
		{1, domain.AgingBucket0_30},
		{29, domain.AgingBucket0_30},
		{30, domain.AgingBucket0_30},
		// 31-60 inclusive.
		{31, domain.AgingBucket31_60},
		{45, domain.AgingBucket31_60},
		{60, domain.AgingBucket31_60},
		// 61-90 inclusive.
		{61, domain.AgingBucket61_90},
		{75, domain.AgingBucket61_90},
		{90, domain.AgingBucket61_90},
		// 91+ → "90+" bucket (industry convention: "90+" means >90).
		{91, domain.AgingBucket90Plus},
		{120, domain.AgingBucket90Plus},
		{365, domain.AgingBucket90Plus},
		{9999, domain.AgingBucket90Plus},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := domain.BucketForDays(tc.dias)
			assert.Equal(t, tc.want, got, "BucketForDays(%d)", tc.dias)
			// The AgingBucket string value must match the Task-1 DB constant.
			assert.Equal(t, string(tc.want), string(got))
		})
	}
}

// TestBucketForDays_StringValuesMatchTask1Constants ensures the AgingBucket typed
// constants carry the exact same string values as the BucketAgingDias* untyped
// constants used in cartera_snapshot.go and in the DB migration.
func TestBucketForDays_StringValuesMatchTask1Constants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.BucketAgingDias0_30, string(domain.AgingBucket0_30))
	assert.Equal(t, domain.BucketAgingDias31_60, string(domain.AgingBucket31_60))
	assert.Equal(t, domain.BucketAgingDias61_90, string(domain.AgingBucket61_90))
	assert.Equal(t, domain.BucketAgingDias90Plus, string(domain.AgingBucket90Plus))
}

// ─── PARRatio ─────────────────────────────────────────────────────────────────

func TestPARRatio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		moroso     string
		total      string
		wantApprox float64
		wantExact  string // empty → use wantApprox
	}{
		{
			name:      "zero total returns 0",
			moroso:    "5000",
			total:     "0",
			wantExact: "0",
		},
		{
			name:      "negative total returns 0",
			moroso:    "5000",
			total:     "-100",
			wantExact: "0",
		},
		{
			name:      "zero moroso returns 0",
			moroso:    "0",
			total:     "100000",
			wantExact: "0",
		},
		{
			name:      "half is 0.5",
			moroso:    "50000",
			total:     "100000",
			wantExact: "0.5",
		},
		{
			name:      "moroso equals total returns 1",
			moroso:    "75000",
			total:     "75000",
			wantExact: "1",
		},
		{
			name:      "moroso > total clamped to 1",
			moroso:    "200000",
			total:     "100000",
			wantExact: "1",
		},
		{
			name:      "negative moroso clamped to 0",
			moroso:    "-1000",
			total:     "50000",
			wantExact: "0",
		},
		{
			name:      "typical ratio 15%",
			moroso:    "15000",
			total:     "100000",
			wantExact: "0.15",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			moroso, _ := decimal.NewFromString(tc.moroso)
			total, _ := decimal.NewFromString(tc.total)
			got := domain.PARRatio(moroso, total)

			if tc.wantExact != "" {
				want, _ := decimal.NewFromString(tc.wantExact)
				assert.True(t, got.Equal(want), "PARRatio(%s, %s) = %s, want %s", tc.moroso, tc.total, got, tc.wantExact)
			}
			// Always verify the result is in [0, 1].
			assert.False(t, got.IsNegative(), "PAR must be >= 0")
			assert.True(t, got.LessThanOrEqual(decimal.NewFromInt(1)), "PAR must be <= 1")
		})
	}
}

// ─── VintageCohort ────────────────────────────────────────────────────────────

func TestVintageCohort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		fechaCargo time.Time
		want       int
	}{
		{
			name:       "zero time returns 0",
			fechaCargo: time.Time{},
			want:       0,
		},
		{
			name:       "January 2026",
			fechaCargo: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			want:       2026*12 + 1, // 24313
		},
		{
			name:       "December 2026",
			fechaCargo: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			want:       2026*12 + 12, // 24324
		},
		{
			name:       "January 2027 follows December 2026",
			fechaCargo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			want:       2027*12 + 1, // 24325
		},
		{
			name:       "year rollover is monotonic",
			fechaCargo: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
			want:       2025*12 + 12, // 24312
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.VintageCohort(tc.fechaCargo)
			assert.Equal(t, tc.want, got)
		})
	}

	// Monotonic: consecutive months always produce strictly increasing cohort IDs.
	t.Run("monotonic across year boundary", func(t *testing.T) {
		t.Parallel()
		dec2025 := domain.VintageCohort(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC))
		jan2026 := domain.VintageCohort(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Equal(t, 1, jan2026-dec2025, "year rollover must produce a gap of exactly 1")
	})
}

// ─── CohortAgeMonths ──────────────────────────────────────────────────────────

func TestCohortAgeMonths(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) // "today" in tests

	cases := []struct {
		name       string
		fechaCargo time.Time
		now        time.Time
		want       int
	}{
		{
			name:       "zero fechaCargo returns 0",
			fechaCargo: time.Time{},
			now:        ref,
			want:       0,
		},
		{
			name:       "same month returns 0",
			fechaCargo: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			now:        ref,
			want:       0,
		},
		{
			name:       "one month ago",
			fechaCargo: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC),
			now:        ref,
			want:       1,
		},
		{
			name:       "twelve months ago",
			fechaCargo: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			now:        ref,
			want:       12,
		},
		{
			name:       "year boundary: Dec 2024 → Jun 2026",
			fechaCargo: time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
			now:        ref,
			want:       18, // 6 months into 2025 + 12 months of 2025 full year? No: 2024*12+12 to 2026*12+6 = 24*12+6 - 24*12-12+12... let me compute
			// VintageCohort(2024-12) = 2024*12+12 = 24300
			// VintageCohort(2026-06) = 2026*12+6  = 24318
			// age = 24318 - 24300 = 18
		},
		{
			name:       "future fechaCargo clamped to 0",
			fechaCargo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			now:        ref,
			want:       0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.CohortAgeMonths(tc.fechaCargo, tc.now)
			assert.Equal(t, tc.want, got)
			assert.GreaterOrEqual(t, got, 0, "age must be non-negative")
		})
	}
}

// ─── RollRate ─────────────────────────────────────────────────────────────────

func TestRollRate(t *testing.T) {
	t.Parallel()

	d := func(s string) decimal.Decimal {
		v, _ := decimal.NewFromString(s)
		return v
	}

	cases := []struct {
		name       string
		prev       map[domain.AgingBucket]decimal.Decimal
		curr       map[domain.AgingBucket]decimal.Decimal
		wantApprox float64
		tolerance  float64
	}{
		{
			name:       "empty prev returns 0",
			prev:       map[domain.AgingBucket]decimal.Decimal{},
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")}, //nolint:exhaustive
			wantApprox: 0,
			tolerance:  1e-9,
		},
		{
			name:       "empty curr returns 0",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")}, //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{},
			wantApprox: 0,
			tolerance:  1e-9,
		},
		{
			name:       "identical distribution returns 0",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000"), domain.AgingBucket31_60: d("20000")}, //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000"), domain.AgingBucket31_60: d("20000")}, //nolint:exhaustive
			wantApprox: 0,
			tolerance:  1e-9,
		},
		{
			// All balance moves from bucket 0 to bucket 1: forward migration of 1/3.
			name:       "all balance shifts one bucket forward",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")},  //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket31_60: d("100000")}, //nolint:exhaustive
			wantApprox: 1.0 / 3.0,
			tolerance:  1e-9,
		},
		{
			// All balance moves from bucket 1 to bucket 2: also +1/3.
			name:       "shift from bucket 1 to bucket 2",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket31_60: d("100000")}, //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket61_90: d("100000")}, //nolint:exhaustive
			wantApprox: 1.0 / 3.0,
			tolerance:  1e-9,
		},
		{
			// All balance moves from bucket 2 to bucket 3: +1/3.
			name:       "shift from bucket 2 to bucket 3",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket61_90: d("100000")},  //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket90Plus: d("100000")}, //nolint:exhaustive
			wantApprox: 1.0 / 3.0,
			tolerance:  1e-9,
		},
		{
			// All balance moves from bucket 0 to bucket 3: maximum deterioration = +1.
			name:       "max deterioration: bucket 0 to bucket 3",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")},   //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket90Plus: d("100000")}, //nolint:exhaustive
			wantApprox: 1.0,
			tolerance:  1e-9,
		},
		{
			// All balance improves from bucket 3 to bucket 0: maximum improvement = -1.
			name:       "max improvement: bucket 3 to bucket 0",
			prev:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket90Plus: d("100000")}, //nolint:exhaustive
			curr:       map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")},   //nolint:exhaustive
			wantApprox: -1.0,
			tolerance:  1e-9,
		},
		{
			// Mixed: half stays in 0-30, half worsens to 31-60 → small positive rate.
			name: "half worsens by one bucket",
			prev: map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("200000")}, //nolint:exhaustive
			curr: map[domain.AgingBucket]decimal.Decimal{ //nolint:exhaustive
				domain.AgingBucket0_30:  d("100000"),
				domain.AgingBucket31_60: d("100000"),
			},
			// prev_mean = 0 ; curr_mean = (0*100000 + 1*100000) / 200000 = 0.5
			// rate = (0.5 - 0) / 3 = 1/6
			wantApprox: 1.0 / 6.0,
			tolerance:  1e-9,
		},
		{
			// Negative saldo entries should be clamped to zero.
			name: "negative saldo clamped to zero",
			prev: map[domain.AgingBucket]decimal.Decimal{domain.AgingBucket0_30: d("100000")}, //nolint:exhaustive
			curr: map[domain.AgingBucket]decimal.Decimal{ //nolint:exhaustive
				domain.AgingBucket0_30:  d("100000"),
				domain.AgingBucket31_60: d("-50000"), // poisoned entry → ignored
			},
			// curr effectively = {0-30: 100000}, same as prev → rate = 0
			wantApprox: 0,
			tolerance:  1e-9,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.RollRate(tc.prev, tc.curr)
			assert.InDelta(t, tc.wantApprox, got, tc.tolerance, "RollRate mismatch")
			// Invariant: always in [-1, +1].
			assert.GreaterOrEqual(t, got, -1.0)
			assert.LessOrEqual(t, got, 1.0)
		})
	}
}

// ─── CumplimientoEsperado ─────────────────────────────────────────────────────

func TestCumplimientoEsperado(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	d := func(s string) decimal.Decimal {
		v, _ := decimal.NewFromString(s)
		return v
	}

	cases := []struct {
		name     string
		proxPago time.Time
		saldo    decimal.Decimal
		want     domain.EstadoCumplimiento
	}{
		{
			name:     "zero saldo → AL_CORRIENTE",
			proxPago: now.AddDate(0, 0, -60),
			saldo:    decimal.Zero,
			want:     domain.EstadoCumplimientoAlCorriente,
		},
		{
			name:     "negative saldo → AL_CORRIENTE",
			proxPago: now.AddDate(0, 0, -60),
			saldo:    d("-100"),
			want:     domain.EstadoCumplimientoAlCorriente,
		},
		{
			name:     "saldo > 0, zero proxPago → VENCIDO",
			proxPago: time.Time{},
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencido,
		},
		{
			name:     "saldo > 0, proxPago tomorrow → AL_CORRIENTE",
			proxPago: now.AddDate(0, 0, 1),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoAlCorriente,
		},
		{
			name:     "saldo > 0, proxPago = now (exact) → AL_CORRIENTE",
			proxPago: now,
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoAlCorriente,
		},
		{
			name:     "saldo > 0, proxPago 1 day ago → VENCIDO_LEVE",
			proxPago: now.AddDate(0, 0, -1),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencidoLeve,
		},
		{
			name:     "saldo > 0, proxPago 30 days ago → VENCIDO_LEVE (boundary)",
			proxPago: now.AddDate(0, 0, -30),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencidoLeve,
		},
		{
			name:     "saldo > 0, proxPago 31 days ago → VENCIDO",
			proxPago: now.AddDate(0, 0, -31),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencido,
		},
		{
			name:     "saldo > 0, proxPago 90 days ago → VENCIDO",
			proxPago: now.AddDate(0, 0, -90),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencido,
		},
		{
			name:     "saldo > 0, proxPago 365 days ago → VENCIDO",
			proxPago: now.AddDate(0, 0, -365),
			saldo:    d("5000"),
			want:     domain.EstadoCumplimientoVencido,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.CumplimientoEsperado(now, tc.proxPago, tc.saldo)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ─── EstadoCumplimiento VO ────────────────────────────────────────────────────

func TestEstadoCumplimiento_ParseAndValid(t *testing.T) {
	t.Parallel()

	valid := []string{"AL_CORRIENTE", "VENCIDO_LEVE", "VENCIDO"}
	for _, s := range valid {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			ec, err := domain.ParseEstadoCumplimiento(s)
			require.NoError(t, err)
			assert.True(t, ec.IsValid())
			assert.Equal(t, s, ec.String())
		})
	}

	t.Run("invalid returns error", func(t *testing.T) {
		t.Parallel()
		_, err := domain.ParseEstadoCumplimiento("PENDIENTE")
		require.Error(t, err)
		require.ErrorIs(t, err, domain.ErrEstadoCumplimientoInvalido)
	})

	t.Run("empty returns error", func(t *testing.T) {
		t.Parallel()
		_, err := domain.ParseEstadoCumplimiento("")
		require.Error(t, err)
	})
}

// ─── Property tests (pgregory.net/rapid) ─────────────────────────────────────

// TestProperty_PARRatio_AlwaysInRange verifies that PARRatio always returns a
// value in [0, 1] for any non-negative moroso and non-negative total inputs.
func TestProperty_PARRatio_AlwaysInRange(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Draw raw float64 amounts to cover a realistic MXN portfolio range.
		morosoF := rapid.Float64Range(0, 10_000_000).Draw(t, "moroso")
		totalF := rapid.Float64Range(0, 10_000_000).Draw(t, "total")

		moroso := decimal.NewFromFloat(morosoF)
		total := decimal.NewFromFloat(totalF)

		got := domain.PARRatio(moroso, total)

		if got.IsNegative() {
			t.Fatalf("PARRatio(%s, %s) = %s: must be >= 0", moroso, total, got)
		}
		if got.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("PARRatio(%s, %s) = %s: must be <= 1", moroso, total, got)
		}
	})
}

// TestProperty_PARRatio_NegativeInputsClamped verifies that negative inputs
// are handled safely (clamped, no panic).
func TestProperty_PARRatio_NegativeInputsClamped(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		morosoF := rapid.Float64Range(-10_000_000, 10_000_000).Draw(t, "moroso")
		totalF := rapid.Float64Range(-10_000_000, 10_000_000).Draw(t, "total")

		moroso := decimal.NewFromFloat(morosoF)
		total := decimal.NewFromFloat(totalF)

		got := domain.PARRatio(moroso, total)

		if got.IsNegative() {
			t.Fatalf("PARRatio(%s, %s) = %s: must be >= 0 (clamped)", moroso, total, got)
		}
		if got.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("PARRatio(%s, %s) = %s: must be <= 1 (clamped)", moroso, total, got)
		}
	})
}

// TestProperty_BucketForDays_BoundariesExact verifies that the aging bucket
// assignment matches expected bucket for every value in the typical range.
func TestProperty_BucketForDays_BoundariesExact(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		dias := rapid.IntRange(-100, 500).Draw(t, "dias")
		got := domain.BucketForDays(dias)

		effective := dias
		if effective < 0 {
			effective = 0
		}

		var want domain.AgingBucket
		switch {
		case effective <= 30:
			want = domain.AgingBucket0_30
		case effective <= 60:
			want = domain.AgingBucket31_60
		case effective <= 90:
			want = domain.AgingBucket61_90
		default:
			want = domain.AgingBucket90Plus
		}

		if got != want {
			t.Fatalf("BucketForDays(%d): got %q, want %q", dias, got, want)
		}
	})
}

// TestProperty_CohortAgeMonths_NonNegative verifies that CohortAgeMonths is
// always >= 0 for any combination of fechaCargo and now.
func TestProperty_CohortAgeMonths_NonNegative(t *testing.T) {
	t.Parallel()
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rapid.Check(t, func(t *rapid.T) {
		// Draw offsets in days from the base to produce arbitrary dates.
		cargoOffset := rapid.IntRange(-3000, 3000).Draw(t, "cargoOffset")
		nowOffset := rapid.IntRange(-3000, 3000).Draw(t, "nowOffset")

		fechaCargo := base.AddDate(0, 0, cargoOffset)
		now := base.AddDate(0, 0, nowOffset)

		got := domain.CohortAgeMonths(fechaCargo, now)
		if got < 0 {
			t.Fatalf("CohortAgeMonths returned %d (negative) for fechaCargo=%v now=%v", got, fechaCargo, now)
		}
	})
}

// TestProperty_CohortAgeMonths_ArithmeticCorrect verifies that the age returned
// by CohortAgeMonths matches the VintageCohort difference (when non-negative).
func TestProperty_CohortAgeMonths_ArithmeticCorrect(t *testing.T) {
	t.Parallel()
	base := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	rapid.Check(t, func(t *rapid.T) {
		cargoOffset := rapid.IntRange(0, 5000).Draw(t, "cargoOffset") // cargo in past
		ageMonths := rapid.IntRange(0, 300).Draw(t, "ageMonths")

		fechaCargo := base.AddDate(0, 0, cargoOffset)
		now := fechaCargo.AddDate(0, ageMonths, 0)

		got := domain.CohortAgeMonths(fechaCargo, now)
		// The exact month count must equal the ordinal difference.
		wantOrdinal := domain.VintageCohort(now) - domain.VintageCohort(fechaCargo)
		if wantOrdinal < 0 {
			wantOrdinal = 0
		}
		if got != wantOrdinal {
			t.Fatalf("CohortAgeMonths=%d but VintageCohort diff=%d (fechaCargo=%v, now=%v)",
				got, wantOrdinal, fechaCargo, now)
		}
	})
}

// TestProperty_RollRate_Monotonic is the core roll-rate invariant test.
// Shifting all balance from bucket i to bucket i+1 must always increase the
// roll-rate relative to the unshifted baseline, regardless of starting bucket.
func TestProperty_RollRate_Monotonic(t *testing.T) {
	t.Parallel()

	allBuckets := []domain.AgingBucket{
		domain.AgingBucket0_30,
		domain.AgingBucket31_60,
		domain.AgingBucket61_90,
	}

	nextBucket := map[domain.AgingBucket]domain.AgingBucket{ //nolint:exhaustive // 90+ has no next bucket by definition
		domain.AgingBucket0_30:  domain.AgingBucket31_60,
		domain.AgingBucket31_60: domain.AgingBucket61_90,
		domain.AgingBucket61_90: domain.AgingBucket90Plus,
	}

	rapid.Check(t, func(t *rapid.T) {
		// Pick a random starting bucket (not 90+ since it has no "next").
		idx := rapid.IntRange(0, len(allBuckets)-1).Draw(t, "bucketIdx")
		startBucket := allBuckets[idx]
		endBucket := nextBucket[startBucket]

		saldoF := rapid.Float64Range(1, 10_000_000).Draw(t, "saldo")
		saldo := decimal.NewFromFloat(saldoF)

		// Baseline: all balance stays in startBucket (roll-rate from startBucket
		// to startBucket = 0 by definition when prev == curr).
		baseline := map[domain.AgingBucket]decimal.Decimal{startBucket: saldo} //nolint:exhaustive
		baseRate := domain.RollRate(baseline, baseline)

		// Shifted: all balance moves to the next (worse) bucket.
		shifted := map[domain.AgingBucket]decimal.Decimal{endBucket: saldo} //nolint:exhaustive
		shiftedRate := domain.RollRate(baseline, shifted)

		// Monotonic: shifting forward must strictly increase the roll-rate.
		if shiftedRate <= baseRate {
			t.Fatalf(
				"RollRate not monotonic: bucket %q → %q with saldo=%s: baseRate=%f, shiftedRate=%f (expected shiftedRate > baseRate)",
				startBucket, endBucket, saldo, baseRate, shiftedRate,
			)
		}

		// The shift of exactly one bucket must increase the rate by 1/3.
		expectedIncrease := 1.0 / 3.0
		actualIncrease := shiftedRate - baseRate
		if math.Abs(actualIncrease-expectedIncrease) > 1e-9 {
			t.Fatalf(
				"RollRate increase for one-bucket shift: got %f, want %f (±1e-9)",
				actualIncrease, expectedIncrease,
			)
		}
	})
}

// TestProperty_RollRate_InRange verifies that RollRate always returns a value
// in [-1, +1] for any non-negative saldo distributions.
func TestProperty_RollRate_InRange(t *testing.T) {
	t.Parallel()
	allBuckets := []domain.AgingBucket{
		domain.AgingBucket0_30,
		domain.AgingBucket31_60,
		domain.AgingBucket61_90,
		domain.AgingBucket90Plus,
	}
	rapid.Check(t, func(t *rapid.T) {
		// Build two random distributions over the four buckets.
		prev := make(map[domain.AgingBucket]decimal.Decimal)
		curr := make(map[domain.AgingBucket]decimal.Decimal)
		for _, b := range allBuckets {
			pF := rapid.Float64Range(0, 1_000_000).Draw(t, "prev_"+string(b))
			cF := rapid.Float64Range(0, 1_000_000).Draw(t, "curr_"+string(b))
			prev[b] = decimal.NewFromFloat(pF)
			curr[b] = decimal.NewFromFloat(cF)
		}
		got := domain.RollRate(prev, curr)
		if got < -1.0 || got > 1.0 {
			t.Fatalf("RollRate returned %f: outside [-1, +1]", got)
		}
	})
}

// ─── Fuzz tests ───────────────────────────────────────────────────────────────

// FuzzBucketForDays verifies that BucketForDays never panics and always returns
// a valid AgingBucket value for any integer input.
func FuzzBucketForDays(f *testing.F) {
	// Seed corpus with boundary values.
	for _, v := range []int{-1, 0, 30, 31, 60, 61, 90, 91, 9999} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, dias int) {
		got := domain.BucketForDays(dias)
		switch got {
		case domain.AgingBucket0_30, domain.AgingBucket31_60,
			domain.AgingBucket61_90, domain.AgingBucket90Plus:
			// valid
		default:
			t.Fatalf("BucketForDays(%d) returned invalid bucket %q", dias, got)
		}
	})
}

// FuzzPARRatio verifies that PARRatio never panics and always returns a value
// in [0, 1] for any float64 pair.
func FuzzPARRatio(f *testing.F) {
	f.Add(0.0, 0.0)
	f.Add(100.0, 0.0)
	f.Add(50000.0, 100000.0)
	f.Add(-1.0, 100000.0)
	f.Add(200000.0, 100000.0)
	f.Fuzz(func(t *testing.T, morosoF, totalF float64) {
		// Skip NaN/Inf which have undefined arithmetic semantics.
		if math.IsNaN(morosoF) || math.IsInf(morosoF, 0) ||
			math.IsNaN(totalF) || math.IsInf(totalF, 0) {
			return
		}
		moroso := decimal.NewFromFloat(morosoF)
		total := decimal.NewFromFloat(totalF)
		got := domain.PARRatio(moroso, total)
		if got.IsNegative() {
			t.Fatalf("PARRatio(%f, %f) = %s: must be >= 0", morosoF, totalF, got)
		}
		if got.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("PARRatio(%f, %f) = %s: must be <= 1", morosoF, totalF, got)
		}
	})
}

// FuzzCohortAgeMonths verifies that CohortAgeMonths never panics and always
// returns a non-negative value for any pair of Unix timestamps (as int64 seconds).
func FuzzCohortAgeMonths(f *testing.F) {
	base := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	f.Add(base.Unix(), base.AddDate(0, 6, 0).Unix())
	f.Add(base.AddDate(-5, 0, 0).Unix(), base.Unix())
	f.Add(base.Unix(), base.AddDate(0, 0, -1).Unix()) // future cargo → age 0
	f.Fuzz(func(t *testing.T, cargoSec, nowSec int64) {
		fechaCargo := time.Unix(cargoSec, 0).UTC()
		now := time.Unix(nowSec, 0).UTC()
		got := domain.CohortAgeMonths(fechaCargo, now)
		if got < 0 {
			t.Fatalf("CohortAgeMonths(cargo=%v, now=%v) = %d: must be >= 0", fechaCargo, now, got)
		}
	})
}

// FuzzCumplimientoEsperado verifies that CumplimientoEsperado never panics and
// always returns a valid EstadoCumplimiento for any (now, proxPago, saldo) triple.
func FuzzCumplimientoEsperado(f *testing.F) {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	f.Add(base.Unix(), base.AddDate(0, 0, 1).Unix(), 5000.0)
	f.Add(base.Unix(), base.AddDate(0, 0, -31).Unix(), 5000.0)
	f.Add(base.Unix(), int64(0), 5000.0) // zero proxPago
	f.Add(base.Unix(), base.Unix(), 0.0) // zero saldo
	f.Fuzz(func(t *testing.T, nowSec, proxPagoSec int64, saldoF float64) {
		if math.IsNaN(saldoF) || math.IsInf(saldoF, 0) {
			return
		}
		now := time.Unix(nowSec, 0).UTC()
		var proxPago time.Time
		if proxPagoSec != 0 {
			proxPago = time.Unix(proxPagoSec, 0).UTC()
		}
		saldo := decimal.NewFromFloat(saldoF)
		got := domain.CumplimientoEsperado(now, proxPago, saldo)
		if !got.IsValid() {
			t.Fatalf("CumplimientoEsperado returned invalid value %q", got)
		}
	})
}
