// Package app — narrativa_hash_test.go
//
//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"regexp"
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/app"
)

// baseComp returns a fully-populated PulsoComputado to use as the baseline.
func baseComp() analytics.PulsoComputado {
	return analytics.PulsoComputado{
		BandaCredito:    "BAJO",
		BandaRecompra:   "MEDIO",
		BandaCLV:        "ALTO",
		CreditoResumen:  "buen pagador sin atrasos",
		RecompraResumen: "alta propensión a recompra",
		CLVResumen:      "valor de vida alto",
		// Non-input fields — must not affect the hash.
		Segmento: "VIP",
		Score:    95,
		RasgosIA: []string{"loyal_but_stagnant"},
	}
}

// hexPattern matches exactly 64 lowercase hex characters.
var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestNarrativaInputHash_Determinism verifies that identical inputs always
// produce the same 64-char lowercase-hex hash.
func TestNarrativaInputHash_Determinism(t *testing.T) {
	t.Parallel()
	comp := baseComp()

	h1 := app.NarrativaInputHash(comp, "")
	h2 := app.NarrativaInputHash(comp, "")

	if h1 != h2 {
		t.Fatalf("hash is not deterministic: %q != %q", h1, h2)
	}
	if !hexPattern.MatchString(h1) {
		t.Fatalf("hash %q is not 64 lowercase hex chars", h1)
	}
}

// TestNarrativaInputHash_SensitivityPerField verifies that changing any single
// input field changes the hash, and that changing a NON-input field does not.
func TestNarrativaInputHash_SensitivityPerField(t *testing.T) {
	t.Parallel()
	base := baseComp()
	baseHash := app.NarrativaInputHash(base, "")

	inputMutations := []struct {
		name   string
		mutate func(*analytics.PulsoComputado)
	}{
		{"BandaCredito", func(c *analytics.PulsoComputado) { c.BandaCredito = "ALTO" }},
		{"BandaRecompra", func(c *analytics.PulsoComputado) { c.BandaRecompra = "BAJO" }},
		{"BandaCLV", func(c *analytics.PulsoComputado) { c.BandaCLV = "MEDIO" }},
		{"CreditoResumen", func(c *analytics.PulsoComputado) { c.CreditoResumen = "moroso frecuente" }},
		{"RecompraResumen", func(c *analytics.PulsoComputado) { c.RecompraResumen = "baja propensión" }},
		{"CLVResumen", func(c *analytics.PulsoComputado) { c.CLVResumen = "valor de vida bajo" }},
	}
	for _, tc := range inputMutations {
		tc := tc
		t.Run("changes_hash_when_"+tc.name+"_changes", func(t *testing.T) {
			t.Parallel()
			comp := baseComp()
			tc.mutate(&comp)
			h := app.NarrativaInputHash(comp, "")
			if h == baseHash {
				t.Fatalf("expected hash to change when %s changes, but got same hash %q", tc.name, h)
			}
		})
	}

	nonInputMutations := []struct {
		name   string
		mutate func(*analytics.PulsoComputado)
	}{
		{"Segmento", func(c *analytics.PulsoComputado) { c.Segmento = "ACTIVO" }},
		{"Score", func(c *analytics.PulsoComputado) { c.Score = 42 }},
		{"RasgosIA", func(c *analytics.PulsoComputado) { c.RasgosIA = []string{"seasonal_buyer"} }},
	}
	for _, tc := range nonInputMutations {
		tc := tc
		t.Run("stable_hash_when_"+tc.name+"_changes", func(t *testing.T) {
			t.Parallel()
			comp := baseComp()
			tc.mutate(&comp)
			h := app.NarrativaInputHash(comp, "")
			if h != baseHash {
				t.Fatalf("expected hash to be stable when %s changes, but got %q vs %q", tc.name, h, baseHash)
			}
		})
	}
}

// TestNarrativaInputHash_FieldIndependence verifies that the join is
// order-sensitive: swapping two distinct field values produces a different hash.
// This guards against an accidentally order-insensitive implementation.
func TestNarrativaInputHash_FieldIndependence(t *testing.T) {
	t.Parallel()

	original := baseComp()
	// Swap CreditoResumen and RecompraResumen values.
	swapped := baseComp()
	swapped.CreditoResumen = original.RecompraResumen
	swapped.RecompraResumen = original.CreditoResumen

	hOriginal := app.NarrativaInputHash(original, "")
	hSwapped := app.NarrativaInputHash(swapped, "")

	if hOriginal == hSwapped {
		t.Fatalf(
			"hash is order-insensitive: swapping CreditoResumen (%q) and RecompraResumen (%q) yielded the same hash %q",
			original.CreditoResumen, original.RecompraResumen, hOriginal,
		)
	}
}

// TestNarrativaInputHash_NotaSensitivity verifies that the nota string affects
// the hash: same comp + different nota ⇒ different hash; same nota ⇒ equal hash.
func TestNarrativaInputHash_NotaSensitivity(t *testing.T) {
	t.Parallel()

	comp := baseComp()

	hNoNota := app.NarrativaInputHash(comp, "")
	hWithNota := app.NarrativaInputHash(comp, "acuerdo con Carmelo el viernes")

	if hNoNota == hWithNota {
		t.Fatalf("expected different hash for non-empty nota, got same: %q", hNoNota)
	}

	// Determinism: same non-empty nota ⇒ equal hash.
	hRepeat := app.NarrativaInputHash(comp, "acuerdo con Carmelo el viernes")
	if hWithNota != hRepeat {
		t.Fatalf("hash is not deterministic for same nota: %q != %q", hWithNota, hRepeat)
	}

	// Empty nota is also deterministic.
	hNoNota2 := app.NarrativaInputHash(comp, "")
	if hNoNota != hNoNota2 {
		t.Fatalf("hash is not deterministic for empty nota: %q != %q", hNoNota, hNoNota2)
	}
}
