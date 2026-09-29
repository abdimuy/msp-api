package domain_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewFolio_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want domain.Folio
	}{
		{1, "GA-000001"},
		{7, "GA-000007"},
		{999999, "GA-999999"},
	}
	for _, tc := range cases {
		t.Run(tc.want.String(), func(t *testing.T) {
			t.Parallel()
			f, err := domain.NewFolio(tc.n)
			if err != nil {
				t.Fatalf("NewFolio(%d): unexpected error %v", tc.n, err)
			}
			if f != tc.want {
				t.Errorf("NewFolio(%d) = %q, want %q", tc.n, f, tc.want)
			}
			if f.String() != tc.want.String() {
				t.Errorf("String() = %q, want %q", f.String(), tc.want.String())
			}
			if !f.IsValid() {
				t.Errorf("%q should be valid", f)
			}
		})
	}
}

func TestNewFolio_OutOfRange(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, -1, 1000000} {
		t.Run("n_"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewFolio(n)
			if !errors.Is(err, domain.ErrFolioInvalido) {
				t.Fatalf("NewFolio(%d): want ErrFolioInvalido, got %v", n, err)
			}
		})
	}
}

func TestParseFolio_HappyPath(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"GA-000001", "GA-999999", "GA-000123"} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			f, err := domain.ParseFolio(s)
			if err != nil {
				t.Fatalf("ParseFolio(%q): unexpected error %v", s, err)
			}
			if f.String() != s {
				t.Errorf("String() = %q, want %q", f.String(), s)
			}
			if !f.IsValid() {
				t.Errorf("%q should be valid", f)
			}
		})
	}
}

func TestParseFolio_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"GA-123",
		"GA-00012",
		"GA-0001234",
		"gA-000123",
		"GB-000123",
		"GA-00012A",
		"GA-00012 ",
		"GA 000123",
		"XYZ",
	}
	for _, tc := range cases {
		t.Run("invalid_"+tc, func(t *testing.T) {
			t.Parallel()
			f, err := domain.ParseFolio(tc)
			if !errors.Is(err, domain.ErrFolioInvalido) {
				t.Fatalf("ParseFolio(%q): want ErrFolioInvalido, got %v", tc, err)
			}
			if f != "" {
				t.Errorf("ParseFolio(%q) = %q, want \"\"", tc, f)
			}
		})
	}
}

func TestFolio_IsValid(t *testing.T) {
	t.Parallel()
	if !domain.Folio("GA-000001").IsValid() {
		t.Error("GA-000001 should be valid")
	}
	for _, s := range []string{"", "GA-", "GA-0000000", "GA-00000X", "A-000001"} {
		if domain.Folio(s).IsValid() {
			t.Errorf("%q should not be valid", s)
		}
	}
}
