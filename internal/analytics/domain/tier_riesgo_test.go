// internal/analytics/domain/tier_riesgo_test.go
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseTierRiesgo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    domain.TierRiesgo
		wantErr bool
	}{
		{"AL_DIA", domain.TierRiesgoAlDia, false},
		{"VIGILANCIA", domain.TierRiesgoVigilancia, false},
		{"EN_RIESGO", domain.TierRiesgoEnRiesgo, false},
		{"CRITICO", domain.TierRiesgoCritico, false},
		{"", "", true},
		{"al_dia", "", true},
		{"UNKNOWN", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseTierRiesgo(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseTierRiesgo(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTierRiesgo(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseTierRiesgo(%q): got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTierRiesgoIsValid(t *testing.T) {
	t.Parallel()

	validValues := []domain.TierRiesgo{
		domain.TierRiesgoAlDia,
		domain.TierRiesgoVigilancia,
		domain.TierRiesgoEnRiesgo,
		domain.TierRiesgoCritico,
	}
	for _, v := range validValues {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.IsValid() {
				t.Errorf("TierRiesgo(%q).IsValid() = false, want true", v)
			}
		})
	}

	invalid := domain.TierRiesgo("DESCONOCIDO")
	if invalid.IsValid() {
		t.Errorf("TierRiesgo(%q).IsValid() = true, want false", invalid)
	}
}
