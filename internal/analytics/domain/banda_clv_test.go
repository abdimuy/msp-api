//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseBandaCLV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    domain.BandaCLV
		wantErr bool
	}{
		{"ALTO", domain.BandaCLVAlto, false},
		{"MEDIO", domain.BandaCLVMedio, false},
		{"BAJO", domain.BandaCLVBajo, false},
		{"", "", true},
		{"alto", "", true},
		{"UNKNOWN", "", true},
		{"ALTA", "", true},
		{"BAJA", "", true},
		{"MEDIA", "", true},
		{"CRITICO", "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseBandaCLV(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseBandaCLV(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBandaCLV(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseBandaCLV(%q): got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBandaCLVIsValid(t *testing.T) {
	t.Parallel()

	validValues := []domain.BandaCLV{
		domain.BandaCLVAlto,
		domain.BandaCLVMedio,
		domain.BandaCLVBajo,
	}
	for _, v := range validValues {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.IsValid() {
				t.Errorf("BandaCLV(%q).IsValid() = false, want true", v)
			}
		})
	}

	invalid := domain.BandaCLV("DESCONOCIDO")
	if invalid.IsValid() {
		t.Errorf("BandaCLV(%q).IsValid() = true, want false", invalid)
	}
}

func TestBandaCLVString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		b    domain.BandaCLV
		want string
	}{
		{domain.BandaCLVAlto, "ALTO"},
		{domain.BandaCLVMedio, "MEDIO"},
		{domain.BandaCLVBajo, "BAJO"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if tc.b.String() != tc.want {
				t.Errorf("BandaCLV(%q).String() = %q, want %q", tc.b, tc.b.String(), tc.want)
			}
		})
	}
}

func TestBandaCLV_EmptyString_Invalid(t *testing.T) {
	t.Parallel()

	var b domain.BandaCLV
	if b.IsValid() {
		t.Error("zero-value BandaCLV must be invalid")
	}
	if b.String() != "" {
		t.Errorf("zero-value BandaCLV.String() = %q, want empty", b.String())
	}
}
