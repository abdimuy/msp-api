//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb

import (
	"testing"
	"time"
)

func TestComputeProxPago(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		ultimaFecha  time.Time
		cadenciaDias int
		want         time.Time
	}{
		{
			name:         "normal cadence returns last+cadencia",
			ultimaFecha:  base,
			cadenciaDias: 30,
			want:         base.AddDate(0, 0, 30),
		},
		{
			name:         "cadencia zero returns zero",
			ultimaFecha:  base,
			cadenciaDias: 0,
			want:         time.Time{},
		},
		{
			name:         "negative cadencia returns zero",
			ultimaFecha:  base,
			cadenciaDias: -1,
			want:         time.Time{},
		},
		{
			name:         "zero ultimaFecha returns zero",
			ultimaFecha:  time.Time{},
			cadenciaDias: 30,
			want:         time.Time{},
		},
		{
			name:         "both zero returns zero",
			ultimaFecha:  time.Time{},
			cadenciaDias: 0,
			want:         time.Time{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeProxPago(tc.ultimaFecha, tc.cadenciaDias)
			if !got.Equal(tc.want) {
				t.Errorf("computeProxPago(%v, %d) = %v, want %v",
					tc.ultimaFecha, tc.cadenciaDias, got, tc.want)
			}
		})
	}
}
