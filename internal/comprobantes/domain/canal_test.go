package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestParseCanal_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input  domain.Canal
		esReal bool
	}{
		{domain.CanalLocal, false},
		{domain.CanalWhatsappBusiness, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.input), func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseCanal(string(tc.input))
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
			if got != tc.input {
				t.Fatalf("value mismatch: want %q got %q", tc.input, got)
			}
			if got.EsReal() != tc.esReal {
				t.Fatalf("EsReal mismatch for %q", tc.input)
			}
		})
	}
}

func TestParseCanal_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Local", "WHATSAPP_BUSINESS", "X", "local ", " whatsapp_business", "telegram", "sms"}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q_invalid", tc), func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseCanal(tc)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc)
			}
			if !errors.Is(err, domain.ErrCanalInvalido) {
				t.Fatalf("expected ErrCanalInvalido for %q, got %v", tc, err)
			}
		})
	}
}

func TestCanal_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.CanalLocal.IsValid() {
		t.Fatal("expected CanalLocal to be valid")
	}
	if !domain.CanalWhatsappBusiness.IsValid() {
		t.Fatal("expected CanalWhatsappBusiness to be valid")
	}
	for _, invalid := range []domain.Canal{"", "local ", "LOCAL", "telegram", "whatsapp"} {
		if invalid.IsValid() {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
}

func TestCanal_String(t *testing.T) {
	t.Parallel()
	c, _ := domain.ParseCanal(string(domain.CanalLocal))
	if c.String() != string(domain.CanalLocal) {
		t.Fatalf("expected %q, got %q", string(domain.CanalLocal), c.String())
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
