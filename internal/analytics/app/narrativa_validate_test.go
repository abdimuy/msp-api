//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"strings"
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// lowRiskComp is a helper that returns a PulsoComputado for a low-risk client.
func lowRiskComp() analytics.PulsoComputado {
	return analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoAlDia.String(),
		EstadoPago:   domain.EstadoPagoAlCorriente.String(),
		BandaCredito: domain.BandaCreditoBajo.String(),
	}
}

// validNarrativa returns a >40-rune, <1200-rune Spanish paragraph without
// forbidden phrases, suitable for a low-risk client.
func validNarrativa() string {
	return "Este cliente mantiene un comportamiento de pago consistente y su historial muestra una relación comercial estable con la empresa a lo largo del tiempo."
}

// TestValidarNarrativa_HappyPath verifies that a valid narrativa with valid
// trait codes passes through intact.
func TestValidarNarrativa_HappyPath(t *testing.T) {
	t.Parallel()

	raw := outbound.NarrativeOutput{
		Narrativa: "  " + validNarrativa() + "  ",
		Rasgos:    []string{"loyal_but_stagnant", "churn_risk"},
	}
	result := app.ValidarNarrativa(raw, lowRiskComp())

	if result.UsedFallback {
		t.Fatal("expected UsedFallback=false for a valid narrativa")
	}
	if result.Texto != strings.TrimSpace(raw.Narrativa) {
		t.Errorf("Texto not trimmed: got %q", result.Texto)
	}
	if len(result.Rasgos) != 2 {
		t.Fatalf("expected 2 rasgos, got %d: %v", len(result.Rasgos), result.Rasgos)
	}
	if result.Rasgos[0] != "loyal_but_stagnant" || result.Rasgos[1] != "churn_risk" {
		t.Errorf("unexpected rasgos: %v", result.Rasgos)
	}
}

// TestValidarNarrativa_TraitFiltering verifies invalid codes are dropped, dupes
// removed, and result capped to 3, preserving first-seen order.
func TestValidarNarrativa_TraitFiltering(t *testing.T) {
	t.Parallel()

	raw := outbound.NarrativeOutput{
		Narrativa: validNarrativa(),
		Rasgos: []string{
			"loyal_but_stagnant", // valid, keep
			"NOT_A_CODE",         // invalid, drop
			"loyal_but_stagnant", // dup, drop
			"churn_risk",         // valid, keep
			"price_sensitive",    // valid, keep (cap=3 reached here)
			"cash_reliable",      // valid, but cap already reached
		},
	}
	result := app.ValidarNarrativa(raw, lowRiskComp())

	if result.UsedFallback {
		t.Fatal("expected UsedFallback=false")
	}
	want := []string{"loyal_but_stagnant", "churn_risk", "price_sensitive"}
	if len(result.Rasgos) != len(want) {
		t.Fatalf("expected %d rasgos, got %d: %v", len(want), len(result.Rasgos), result.Rasgos)
	}
	for i, code := range want {
		if result.Rasgos[i] != code {
			t.Errorf("rasgos[%d]: want %q, got %q", i, code, result.Rasgos[i])
		}
	}
}

// TestValidarNarrativa_ContradictionFallback verifies that a narrativa with a
// forbidden "good-payer" phrase under high-risk triggers the empty fallback.
func TestValidarNarrativa_ContradictionFallback(t *testing.T) {
	t.Parallel()

	raw := outbound.NarrativeOutput{
		Narrativa: "Este cliente es un buen pagador que siempre cumple con sus obligaciones y mantiene su cuenta en orden.",
		Rasgos:    []string{"steady_reliable"},
	}
	comp := analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoCritico.String(),
		EstadoPago:   domain.EstadoPagoMoroso.String(),
		BandaCredito: domain.BandaCreditoBajo.String(), // BandaCredito not critico here
	}

	result := app.ValidarNarrativa(raw, comp)

	if !result.UsedFallback {
		t.Fatal("expected UsedFallback=true for contradictory narrativa under CRITICO risk")
	}
	if result.Texto != "" {
		t.Errorf("expected empty Texto on fallback, got %q", result.Texto)
	}
	if len(result.Rasgos) != 0 {
		t.Errorf("expected no Rasgos on fallback, got %v", result.Rasgos)
	}
}

// TestValidarNarrativa_EmptyOrTooShortFallback verifies that blank or short
// narrativas trigger the fallback.
func TestValidarNarrativa_EmptyOrTooShortFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		narrativa string
	}{
		{"blank", ""},
		{"whitespace only", "   "},
		{"5 chars", "Hola."},
		{"39 runes", strings.Repeat("a", 39)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw := outbound.NarrativeOutput{
				Narrativa: tc.narrativa,
				Rasgos:    []string{"churn_risk"},
			}
			result := app.ValidarNarrativa(raw, lowRiskComp())
			if !result.UsedFallback {
				t.Errorf("expected fallback for narrativa %q", tc.narrativa)
			}
			if result.Texto != "" {
				t.Errorf("expected empty Texto, got %q", result.Texto)
			}
		})
	}
}

// TestValidarNarrativa_TooLongFallback verifies that a runaway narrativa >1200
// runes triggers the fallback.
func TestValidarNarrativa_TooLongFallback(t *testing.T) {
	t.Parallel()

	// 1201 runes — exceeds the cap.
	raw := outbound.NarrativeOutput{
		Narrativa: strings.Repeat("a", 1201),
		Rasgos:    []string{"churn_risk"},
	}
	result := app.ValidarNarrativa(raw, lowRiskComp())

	if !result.UsedFallback {
		t.Fatal("expected fallback for >1200-rune narrativa")
	}
	if result.Texto != "" {
		t.Errorf("expected empty Texto, got len=%d", len(result.Texto))
	}
}

// TestValidarNarrativa_LowRiskPositivePhraseOK verifies that a "good-payer"
// phrase is acceptable when the client is NOT high-risk.
func TestValidarNarrativa_LowRiskPositivePhraseOK(t *testing.T) {
	t.Parallel()

	// Narrativa explicitly contains "buen pagador" — fine for AL_DIA client.
	narrativa := "Este cliente es reconocido como buen pagador: mantiene su crédito al corriente y raramente genera alertas de cobranza en el historial registrado."
	raw := outbound.NarrativeOutput{
		Narrativa: narrativa,
		Rasgos:    []string{"steady_reliable"},
	}

	result := app.ValidarNarrativa(raw, lowRiskComp())

	if result.UsedFallback {
		t.Fatal("expected UsedFallback=false: low-risk client may receive positive phrasing")
	}
	if result.Texto != strings.TrimSpace(narrativa) {
		t.Errorf("Texto mismatch: got %q", result.Texto)
	}
	if len(result.Rasgos) != 1 || result.Rasgos[0] != "steady_reliable" {
		t.Errorf("unexpected Rasgos: %v", result.Rasgos)
	}
}

// TestValidarNarrativa_EnRiesgoContradiction verifies that EN_RIESGO tier also
// triggers the forbidden-phrase check.
func TestValidarNarrativa_EnRiesgoContradiction(t *testing.T) {
	t.Parallel()

	comp := analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoEnRiesgo.String(),
		EstadoPago:   domain.EstadoPagoAtrasado.String(),
		BandaCredito: domain.BandaCreditoAlto.String(),
	}
	raw := outbound.NarrativeOutput{
		Narrativa: "El cliente paga a tiempo y muestra un comportamiento ejemplar en sus compromisos financieros recientes con la empresa.",
		Rasgos:    []string{"steady_reliable"},
	}

	result := app.ValidarNarrativa(raw, comp)

	if !result.UsedFallback {
		t.Fatal("expected fallback: 'paga a tiempo' is forbidden under EN_RIESGO")
	}
}

// TestValidarNarrativa_MorosoContradiction verifies that EstadoPago=MOROSO
// alone triggers the direction check even when TierRiesgo is not CRITICO.
func TestValidarNarrativa_MorosoContradiction(t *testing.T) {
	t.Parallel()

	comp := analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoVigilancia.String(), // not the highest tier
		EstadoPago:   domain.EstadoPagoMoroso.String(),     // MOROSO alone is enough
		BandaCredito: domain.BandaCreditoMedio.String(),
	}
	raw := outbound.NarrativeOutput{
		Narrativa: "El cliente ha demostrado ser muy cumplido con sus pagos y tiene una trayectoria impecable con la empresa a lo largo de los años.",
		Rasgos:    []string{"steady_reliable"},
	}

	result := app.ValidarNarrativa(raw, comp)

	if !result.UsedFallback {
		t.Fatal("expected fallback: 'muy cumplido' is forbidden when EstadoPago=MOROSO")
	}
}

// TestValidarNarrativa_NoRasgosAfterFilter verifies that a valid narrativa with
// zero passing trait codes yields empty Rasgos (not a fallback).
func TestValidarNarrativa_NoRasgosAfterFilter(t *testing.T) {
	t.Parallel()

	raw := outbound.NarrativeOutput{
		Narrativa: validNarrativa(),
		Rasgos:    []string{"NOT_A_CODE", "ALSO_INVALID"},
	}
	result := app.ValidarNarrativa(raw, lowRiskComp())

	if result.UsedFallback {
		t.Fatal("expected UsedFallback=false: invalid rasgos do not trigger fallback")
	}
	if result.Texto == "" {
		t.Error("expected non-empty Texto")
	}
	if len(result.Rasgos) != 0 {
		t.Errorf("expected empty Rasgos, got %v", result.Rasgos)
	}
}

// TestValidarNarrativa_BandaCreditoCriticoContradiction verifies that
// BandaCredito=CRITICO alone is enough to trigger the direction check.
func TestValidarNarrativa_BandaCreditoCriticoContradiction(t *testing.T) {
	t.Parallel()

	comp := analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoAlDia.String(),
		EstadoPago:   domain.EstadoPagoAlCorriente.String(),
		BandaCredito: domain.BandaCreditoCritico.String(),
	}
	raw := outbound.NarrativeOutput{
		Narrativa: "Este cliente representa bajo riesgo para la cartera y puede recibir condiciones de crédito más favorables en la siguiente compra.",
		Rasgos:    []string{"growing_relationship"},
	}

	result := app.ValidarNarrativa(raw, comp)

	if !result.UsedFallback {
		t.Fatal("expected fallback: 'bajo riesgo' is forbidden when BandaCredito=CRITICO")
	}
}

// TestValidarNarrativa_ExactBoundaryLengths verifies the 40-rune and 1200-rune
// boundary conditions precisely.
func TestValidarNarrativa_ExactBoundaryLengths(t *testing.T) {
	t.Parallel()

	// Exactly 40 runes — should pass.
	exactly40 := strings.Repeat("a", 40)
	r40 := app.ValidarNarrativa(outbound.NarrativeOutput{Narrativa: exactly40}, lowRiskComp())
	if r40.UsedFallback {
		t.Error("expected 40-rune narrativa to pass")
	}

	// Exactly 1200 runes — should pass.
	exactly1200 := strings.Repeat("a", 1200)
	r1200 := app.ValidarNarrativa(outbound.NarrativeOutput{Narrativa: exactly1200}, lowRiskComp())
	if r1200.UsedFallback {
		t.Error("expected 1200-rune narrativa to pass")
	}

	// 39 runes — should fail.
	tooShort := strings.Repeat("a", 39)
	rShort := app.ValidarNarrativa(outbound.NarrativeOutput{Narrativa: tooShort}, lowRiskComp())
	if !rShort.UsedFallback {
		t.Error("expected 39-rune narrativa to fail")
	}

	// 1201 runes — should fail.
	tooLong := strings.Repeat("a", 1201)
	rLong := app.ValidarNarrativa(outbound.NarrativeOutput{Narrativa: tooLong}, lowRiskComp())
	if !rLong.UsedFallback {
		t.Error("expected 1201-rune narrativa to fail")
	}
}
