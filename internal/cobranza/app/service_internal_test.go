//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

// White-box tests for the unexported resolveCutoff function. Lives in package
// app so it can call resolveCutoff directly. The black-box service tests live
// in service_test.go.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ptr is a helper to get a pointer to an int literal.
func ptr(v int) *int { return &v }

// ptrTime is a helper to get a pointer to a time.Time.
func ptrTime(t time.Time) *time.Time { return &t }

// TestResolveCutoff_BoundariesAndArithmetic exercises all branches of
// resolveCutoff, including the sign of the AddDate offset. The
// INVERT_NEGATIVES mutation would change `.*ventanaDias` to `+*ventanaDias`,
// producing a future cutoff instead of a past one — caught by any case that
// asserts cutoff < clock.Now().
func TestResolveCutoff_BoundariesAndArithmetic(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	clock := internalFixedClock{T: fixedNow}

	tests := []struct {
		name        string
		desde       *time.Time
		ventanaDias *int
		wantCutoff  time.Time
		wantErr     error
	}{
		{
			// Both nil → default to DefaultVentanaDias (7) days ago.
			// INVERT_NEGATIVES mutant would produce fixedNow + 7d instead of - 7d.
			name:        "both_nil_uses_default_7_days_ago",
			ventanaDias: nil,
			desde:       nil,
			wantCutoff:  fixedNow.AddDate(0, 0, -DefaultVentanaDias),
		},
		{
			// ventanaDias=0 → zero time (no cutoff).
			name:        "ventana_dias_zero_returns_zero_time",
			ventanaDias: ptr(0),
			wantCutoff:  time.Time{},
		},
		{
			// ventanaDias=7 → exactly 7 days ago (same as default but explicit).
			// Kills INVERT_NEGATIVES: 7 days ago is in the past, not the future.
			name:        "ventana_dias_7_returns_7_days_ago",
			ventanaDias: ptr(7),
			wantCutoff:  fixedNow.AddDate(0, 0, -7),
		},
		{
			// ventanaDias=30 → 30 days ago.
			name:        "ventana_dias_30_returns_30_days_ago",
			ventanaDias: ptr(30),
			wantCutoff:  fixedNow.AddDate(0, 0, -30),
		},
		{
			// ventanaDias=90 → 90 days ago (at the boundary, valid).
			name:        "ventana_dias_90_boundary_valid",
			ventanaDias: ptr(MaxVentanaDias),
			wantCutoff:  fixedNow.AddDate(0, 0, -MaxVentanaDias),
		},
		{
			// ventanaDias=91 → ErrVentanaDiasInvalida (> MaxVentanaDias=90).
			name:        "ventana_dias_91_invalid",
			ventanaDias: ptr(MaxVentanaDias + 1),
			wantErr:     domain.ErrVentanaDiasInvalida,
		},
		{
			// ventanaDias=-1 → ErrVentanaDiasInvalida (< 0).
			name:        "ventana_dias_negative_invalid",
			ventanaDias: ptr(-1),
			wantErr:     domain.ErrVentanaDiasInvalida,
		},
		{
			// desde set, ventanaDias nil → returns desde as-is.
			name:       "desde_set_returns_desde",
			desde:      ptrTime(time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)),
			wantCutoff: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		},
		{
			// Both set → ErrParametrosExcluyentes.
			name:        "both_set_returns_error",
			desde:       ptrTime(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)),
			ventanaDias: ptr(7),
			wantErr:     domain.ErrParametrosExcluyentes,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveCutoff(tc.desde, tc.ventanaDias, clock)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.True(t, tc.wantCutoff.Equal(got),
				"expected cutoff %v, got %v", tc.wantCutoff, got)

			// Extra assertion: any case with a positive ventanaDias must produce a
			// cutoff strictly in the past relative to fixedNow. This directly kills
			// INVERT_NEGATIVES mutants that would produce a future cutoff.
			if tc.ventanaDias != nil && *tc.ventanaDias > 0 {
				assert.True(t, got.Before(fixedNow),
					"cutoff with ventanaDias > 0 must be in the past, got %v (fixedNow=%v)", got, fixedNow)
			}
			// The default path (both nil) also produces a past cutoff.
			if tc.desde == nil && tc.ventanaDias == nil {
				assert.True(t, got.Before(fixedNow),
					"default cutoff must be in the past, got %v (fixedNow=%v)", got, fixedNow)
			}
		})
	}
}
