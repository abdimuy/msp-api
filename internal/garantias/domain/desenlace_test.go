package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestDesenlace_WireValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		constant domain.Desenlace
		want     string
	}{
		{domain.DesenlaceReparado, "reparado"},
		{domain.DesenlaceReemplazado, "reemplazado"},
		{domain.DesenlaceDevuelto, "devuelto"},
		{domain.DesenlaceSegundaMano, "segunda_mano"},
		{domain.DesenlaceDesarmado, "desarmado"},
		{domain.DesenlaceMerma, "merma"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.constant.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDesenlace_HappyPath(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"reparado", "reemplazado", "devuelto",
		"segunda_mano", "desarmado", "merma",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseDesenlace(in)
			if err != nil {
				t.Fatalf("ParseDesenlace(%q) returned error: %v", in, err)
			}
			if got.String() != in {
				t.Errorf("got %q, want %q", got.String(), in)
			}
		})
	}
}

func TestParseDesenlace_RejectsInvalid(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"", "REPARADO", "reparado ",
		"devuelto_falso", "standby",
	}
	for _, in := range invalid {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseDesenlace(in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, domain.ErrDesenlaceInvalido) {
				t.Errorf("expected ErrDesenlaceInvalido, got %v", err)
			}
		})
	}
}
