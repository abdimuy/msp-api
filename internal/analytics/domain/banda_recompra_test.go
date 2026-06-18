//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseBandaRecompra(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    domain.BandaRecompra
		wantErr bool
	}{
		{"ALTA", domain.BandaRecompraAlta, false},
		{"MEDIA", domain.BandaRecompraMedia, false},
		{"BAJA", domain.BandaRecompraBaja, false},
		{"", "", true},
		{"alta", "", true},
		{"UNKNOWN", "", true},
		{"bajo_riesgo", "", true},
		{"BAJO", "", true},
		{"CRITICO", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseBandaRecompra(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseBandaRecompra(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBandaRecompra(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseBandaRecompra(%q): got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBandaRecompraIsValid(t *testing.T) {
	t.Parallel()

	validValues := []domain.BandaRecompra{
		domain.BandaRecompraAlta,
		domain.BandaRecompraMedia,
		domain.BandaRecompraBaja,
	}
	for _, v := range validValues {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.IsValid() {
				t.Errorf("BandaRecompra(%q).IsValid() = false, want true", v)
			}
		})
	}

	invalid := domain.BandaRecompra("DESCONOCIDO")
	if invalid.IsValid() {
		t.Errorf("BandaRecompra(%q).IsValid() = true, want false", invalid)
	}
}

func TestBandaRecompraString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		br   domain.BandaRecompra
		want string
	}{
		{domain.BandaRecompraAlta, "ALTA"},
		{domain.BandaRecompraMedia, "MEDIA"},
		{domain.BandaRecompraBaja, "BAJA"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if tc.br.String() != tc.want {
				t.Errorf("BandaRecompra(%q).String() = %q, want %q", tc.br, tc.br.String(), tc.want)
			}
		})
	}
}

func TestBandaRecompraOrdinal(t *testing.T) {
	t.Parallel()

	// Ordinal must be monotonically increasing with propensity level.
	cases := []struct {
		br      domain.BandaRecompra
		ordinal int
	}{
		{domain.BandaRecompraBaja, 0},
		{domain.BandaRecompraMedia, 1},
		{domain.BandaRecompraAlta, 2},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.br), func(t *testing.T) {
			t.Parallel()
			got := tc.br.Ordinal()
			if got != tc.ordinal {
				t.Errorf("BandaRecompra(%q).Ordinal() = %d, want %d", tc.br, got, tc.ordinal)
			}
		})
	}

	// Unknown value returns -1.
	unknown := domain.BandaRecompra("UNKNOWN")
	if got := unknown.Ordinal(); got != -1 {
		t.Errorf("BandaRecompra(%q).Ordinal() = %d, want -1", unknown, got)
	}

	// Verify ascending-propensity ordering.
	if domain.BandaRecompraBaja.Ordinal() >= domain.BandaRecompraMedia.Ordinal() {
		t.Error("BAJA ordinal must be less than MEDIA")
	}
	if domain.BandaRecompraMedia.Ordinal() >= domain.BandaRecompraAlta.Ordinal() {
		t.Error("MEDIA ordinal must be less than ALTA")
	}
}
