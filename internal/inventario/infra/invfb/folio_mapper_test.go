package invfb

import (
	"regexp"
	"testing"
)

// folioPattern matches the canonical traspaso folio: any 3 uppercase letters
// followed by exactly 6 digits. (The stricter MS[A-Z] prefix is validated by
// domain.NewFolio; here we only test the mapping function's output shape.)
var folioShapePattern = regexp.MustCompile(`^[A-Z]{3}\d{6}$`)

func TestNumeroAFolio_ConcreteExamples(t *testing.T) {
	t.Parallel()
	cases := []struct {
		numero int
		want   string
	}{
		{1, "MST000001"},
		{999999, "MST999999"},
		{1000000, "MSU000001"},
		{1999998, "MSU999999"},
		{1999999, "MSV000001"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := numeroAFolio(tc.numero)
			if got != tc.want {
				t.Fatalf("numeroAFolio(%d) = %q, want %q", tc.numero, got, tc.want)
			}
		})
	}
}

func TestNumeroAFolio_Shape(t *testing.T) {
	t.Parallel()
	// Probe the first block boundary and a handful of values in the middle.
	probes := []int{1, 2, 100, 999999, 1000000, 1999998, 1999999, 2000000, 5000000}
	for _, n := range probes {
		n := n
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := numeroAFolio(n)
			if !folioShapePattern.MatchString(got) {
				t.Fatalf("numeroAFolio(%d) = %q: does not match ^[A-Z]{3}\\d{6}$", n, got)
			}
		})
	}
}

func TestNumeroAFolio_Monotone(t *testing.T) {
	t.Parallel()
	// Verify that consecutive numbers produce strictly increasing folio strings
	// within a block (same prefix → lexicographic order is correct).
	for i := 1; i < 10; i++ {
		a := numeroAFolio(i)
		b := numeroAFolio(i + 1)
		if a >= b {
			t.Fatalf("expected numeroAFolio(%d)=%q < numeroAFolio(%d)=%q", i, a, i+1, b)
		}
	}
}

func TestNumeroAFolio_PrefixCycling(t *testing.T) {
	t.Parallel()
	// At the boundary between MST and MSU the prefix must change.
	lastMST := numeroAFolio(999999)
	firstMSU := numeroAFolio(1000000)
	if lastMST[:3] != "MST" {
		t.Fatalf("expected prefix MST, got %s (numero=999999)", lastMST[:3])
	}
	if firstMSU[:3] != "MSU" {
		t.Fatalf("expected prefix MSU, got %s (numero=1000000)", firstMSU[:3])
	}
}

func TestNumeroAFolio_SixDigitBody(t *testing.T) {
	t.Parallel()
	// The numeric body must always be exactly 6 digits (zero-padded).
	cases := []int{1, 42, 1000, 999999}
	for _, n := range cases {
		n := n
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := numeroAFolio(n)
			body := got[3:] // skip the 3-char prefix
			if len(body) != 6 {
				t.Fatalf("numeroAFolio(%d)=%q: body %q is not 6 chars", n, got, body)
			}
		})
	}
}
