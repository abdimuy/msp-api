package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestNewCanal_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input  string
		esReal bool
	}{
		{domain.CanalLocal, false},
		{domain.CanalWhatsappBusiness, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewCanal(tc.input)
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got.Value() != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, got.Value())
			}
			if got.EsReal() != tc.esReal {
				t.Fatalf("EsReal mismatch for %q", tc.input)
			}
		})
	}
}

func TestNewCanal_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Local", "WHATSAPP_BUSINESS", "X", "local ", " whatsapp_business", "telegram", "sms"}
	for _, tc := range cases {
		t.Run(tc+"_invalid", func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewCanal(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrCanalInvalido) {
				t.Fatalf("expected ErrCanalInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestCanal_EqualsAndIsZero(t *testing.T) {
	t.Parallel()
	l, _ := domain.NewCanal(domain.CanalLocal)
	l2, _ := domain.NewCanal(domain.CanalLocal)
	w, _ := domain.NewCanal(domain.CanalWhatsappBusiness)

	if !l.Equals(l2) {
		t.Fatal("expected l.Equals(l2) == true")
	}
	if l.Equals(w) {
		t.Fatal("expected l.Equals(w) == false")
	}

	zero := domain.HydrateCanal("")
	if !zero.IsZero() {
		t.Fatal("expected IsZero == true for empty canal")
	}
	if l.IsZero() {
		t.Fatal("expected IsZero == false for valid canal")
	}
}

func TestCanal_String(t *testing.T) {
	t.Parallel()
	c, _ := domain.NewCanal(domain.CanalLocal)
	if c.String() != domain.CanalLocal {
		t.Fatalf("expected %q, got %q", domain.CanalLocal, c.String())
	}
}

func TestCanalConstants(t *testing.T) {
	t.Parallel()
	if domain.CanalLocal != "local" {
		t.Fatalf("expected CanalLocal='local', got %q", domain.CanalLocal)
	}
	if domain.CanalWhatsappBusiness != "whatsapp_business" {
		t.Fatalf("expected CanalWhatsappBusiness='whatsapp_business', got %q", domain.CanalWhatsappBusiness)
	}
}

func TestHydrateCanal_AcceptsGarbage(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"", "garbage", "LOCAL", "123"} {
		t.Run(tc+"_hydrate", func(t *testing.T) {
			t.Parallel()
			hydrated := domain.HydrateCanal(tc)
			if hydrated.Value() != tc {
				t.Fatalf("expected value %q, got %q", tc, hydrated.Value())
			}
			if !hydrated.Equals(domain.HydrateCanal(tc)) {
				t.Fatal("expected hydrated values to be equal")
			}
		})
	}
}
