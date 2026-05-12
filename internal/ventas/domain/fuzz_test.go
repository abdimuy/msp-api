package domain

import (
	"math"
	"testing"
)

// FuzzNewGPSCoords exercises NewGPSCoords with arbitrary float pairs. The
// contract is: it must never panic, and accepted values must satisfy the
// documented bounds.
func FuzzNewGPSCoords(f *testing.F) {
	seeds := []struct{ lat, lng float64 }{
		{0, 0},
		{90, 180},
		{-90, -180},
		{91, 0},
		{-91, 0},
		{0, 181},
		{0, -181},
		{math.NaN(), 0},
		{0, math.NaN()},
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, s := range seeds {
		f.Add(s.lat, s.lng)
	}
	f.Fuzz(func(t *testing.T, lat, lng float64) {
		g, err := NewGPSCoords(lat, lng)
		if err != nil {
			return
		}
		if g.Latitud() < latMin || g.Latitud() > latMax {
			t.Fatalf("accepted lat %v outside bounds", g.Latitud())
		}
		if g.Longitud() < lngMin || g.Longitud() > lngMax {
			t.Fatalf("accepted lng %v outside bounds", g.Longitud())
		}
	})
}

// FuzzNewDiaCobranzaMes exercises NewDiaCobranzaMes. Accepted values must
// be in [1,31].
func FuzzNewDiaCobranzaMes(f *testing.F) {
	seeds := []int{0, 1, 15, 31, 32, -1, 100, -100}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, day int) {
		d, err := NewDiaCobranzaMes(day)
		if err != nil {
			return
		}
		if d.Mes() == nil {
			t.Fatalf("accepted but mes pointer nil: in=%d", day)
		}
		got := *d.Mes()
		if got < diaMesMin || got > diaMesMax {
			t.Fatalf("accepted day %d outside [%d,%d]", got, diaMesMin, diaMesMax)
		}
	})
}

// FuzzNewNombreCliente exercises NewNombreCliente. The contract is: never
// panic, and accepted values are trimmed and length-bounded.
func FuzzNewNombreCliente(f *testing.F) {
	seeds := []string{
		"", "  ", "Juan", "  Juan  ", "José", "中文",
		"a", "x\x00y", "<script>",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := NewNombreCliente(s)
		if err != nil {
			return
		}
		if n.IsZero() {
			t.Fatalf("accepted but IsZero true: in=%q", s)
		}
		if len(n.Value()) > maxNombreClienteLength {
			t.Fatalf("accepted but value too long: %d", len(n.Value()))
		}
	})
}

// FuzzNewImagenStorage exercises NewImagenStorage's key validation. Accepted
// keys must satisfy the documented safety properties.
func FuzzNewImagenStorage(f *testing.F) {
	seeds := []string{
		"ok.jpg",
		"",
		"   ",
		"/leading",
		"a/../b",
		"a\x00b",
		"images/2026/01/x.png",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		s, err := NewImagenStorage(StorageKindFilesystem, key)
		if err != nil {
			return
		}
		if s.Key() == "" {
			t.Fatalf("accepted but key empty: in=%q", key)
		}
		if !isSafeStorageKey(s.Key()) {
			t.Fatalf("accepted unsafe key: %q", s.Key())
		}
	})
}
