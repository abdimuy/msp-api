//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseBandaCredito(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    domain.BandaCredito
		wantErr bool
	}{
		{"BAJO", domain.BandaCreditoBajo, false},
		{"MEDIO", domain.BandaCreditoMedio, false},
		{"ALTO", domain.BandaCreditoAlto, false},
		{"CRITICO", domain.BandaCreditoCritico, false},
		{"", "", true},
		{"bajo", "", true},
		{"UNKNOWN", "", true},
		{"bajo_riesgo", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseBandaCredito(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseBandaCredito(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBandaCredito(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseBandaCredito(%q): got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBandaCreditoIsValid(t *testing.T) {
	t.Parallel()

	validValues := []domain.BandaCredito{
		domain.BandaCreditoBajo,
		domain.BandaCreditoMedio,
		domain.BandaCreditoAlto,
		domain.BandaCreditoCritico,
	}
	for _, v := range validValues {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.IsValid() {
				t.Errorf("BandaCredito(%q).IsValid() = false, want true", v)
			}
		})
	}

	invalid := domain.BandaCredito("DESCONOCIDO")
	if invalid.IsValid() {
		t.Errorf("BandaCredito(%q).IsValid() = true, want false", invalid)
	}
}

func TestBandaCreditoString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		bc   domain.BandaCredito
		want string
	}{
		{domain.BandaCreditoBajo, "BAJO"},
		{domain.BandaCreditoMedio, "MEDIO"},
		{domain.BandaCreditoAlto, "ALTO"},
		{domain.BandaCreditoCritico, "CRITICO"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if tc.bc.String() != tc.want {
				t.Errorf("BandaCredito(%q).String() = %q, want %q", tc.bc, tc.bc.String(), tc.want)
			}
		})
	}
}

func TestBandaCreditoOrdinal(t *testing.T) {
	t.Parallel()

	// Ordinal must be monotonically increasing with risk level.
	cases := []struct {
		bc      domain.BandaCredito
		ordinal int
	}{
		{domain.BandaCreditoBajo, 0},
		{domain.BandaCreditoMedio, 1},
		{domain.BandaCreditoAlto, 2},
		{domain.BandaCreditoCritico, 3},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.bc), func(t *testing.T) {
			t.Parallel()
			got := tc.bc.Ordinal()
			if got != tc.ordinal {
				t.Errorf("BandaCredito(%q).Ordinal() = %d, want %d", tc.bc, got, tc.ordinal)
			}
		})
	}

	// Unknown value returns -1.
	unknown := domain.BandaCredito("UNKNOWN")
	if got := unknown.Ordinal(); got != -1 {
		t.Errorf("BandaCredito(%q).Ordinal() = %d, want -1", unknown, got)
	}

	// Verify ascending-risk ordering.
	if domain.BandaCreditoBajo.Ordinal() >= domain.BandaCreditoMedio.Ordinal() {
		t.Error("BAJO ordinal must be less than MEDIO")
	}
	if domain.BandaCreditoMedio.Ordinal() >= domain.BandaCreditoAlto.Ordinal() {
		t.Error("MEDIO ordinal must be less than ALTO")
	}
	if domain.BandaCreditoAlto.Ordinal() >= domain.BandaCreditoCritico.Ordinal() {
		t.Error("ALTO ordinal must be less than CRITICO")
	}
}
