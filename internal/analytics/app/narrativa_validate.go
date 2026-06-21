// Package app — narrativa_validate.go validates the LLM generator's raw output
// before it is cached or served. Two checks:
//
//	(a) Direction check: the narrativa must not contradict the deterministic risk
//	    and must fall within acceptable length bounds.
//	(b) Trait filtering: only catalog codes are kept, deduped, capped to 3.
//
// On a failed direction check the result is intentionally EMPTY — the ficha
// omits the AI reading entirely and the deterministic Fase-1 titulars keep
// rendering in their existing UI place. No duplication, no contradictory AI text.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// narrativaMinRunes is the minimum acceptable rune count for a valid narrativa.
const narrativaMinRunes = 40

// narrativaMaxRunes is the maximum acceptable rune count (runaway guard).
const narrativaMaxRunes = 1200

// forbiddenPhrases lists Spanish "good-payer" phrases that must NOT appear in
// a narrativa when the client carries high risk. Lowercase NFC; tunable here
// without touching the validation logic.
var forbiddenPhrases = []string{
	"buen pagador",
	"excelente pagador",
	"muy buen pagador",
	"buen cliente de crédito",
	"pagador confiable",
	"bajo riesgo",
	"riesgo bajo",
	"sin riesgo",
	"paga puntual",
	"paga a tiempo",
	"muy cumplido",
}

// ValidatedNarrativa is the post-validation result the worker persists.
type ValidatedNarrativa struct {
	Texto        string
	Rasgos       []string // validated catalog codes (≤3, deduped); nil on fallback
	UsedFallback bool     // true ⇒ direction check failed; Texto is empty
}

// ValidarNarrativa enforces that the model's output does not contradict the
// deterministic risk, and constrains traits to the curated catalog. On a failed
// direction check it returns an EMPTY narrativa with NO traits (UsedFallback=true)
// so the ficha simply omits the AI reading and keeps showing the deterministic
// Fase-1 titulars in their existing place (no regression, no duplication, no
// contradictory AI text).
func ValidarNarrativa(raw outbound.NarrativeOutput, comp analytics.PulsoComputado) ValidatedNarrativa {
	if !pasaDirectionCheck(raw.Narrativa, comp) {
		return ValidatedNarrativa{Texto: "", Rasgos: nil, UsedFallback: true}
	}

	return ValidatedNarrativa{
		Texto:        strings.TrimSpace(raw.Narrativa),
		Rasgos:       filtrarRasgos(raw.Rasgos),
		UsedFallback: false,
	}
}

// pasaDirectionCheck returns true when the narrativa passes all direction checks.
// Fails on: degenerate length (too short or too long), or contradictory positive
// phrasing under high-risk conditions.
func pasaDirectionCheck(narrativa string, comp analytics.PulsoComputado) bool {
	trimmed := strings.TrimSpace(narrativa)
	n := utf8.RuneCountInString(trimmed)
	if n < narrativaMinRunes || n > narrativaMaxRunes {
		return false
	}

	riesgoAlto := comp.TierRiesgo == domain.TierRiesgoCritico.String() ||
		comp.TierRiesgo == domain.TierRiesgoEnRiesgo.String() ||
		comp.EstadoPago == domain.EstadoPagoMoroso.String() ||
		comp.BandaCredito == domain.BandaCreditoCritico.String()

	if riesgoAlto {
		normalized := strings.ToLower(norm.NFC.String(trimmed))
		for _, phrase := range forbiddenPhrases {
			if strings.Contains(normalized, phrase) {
				return false
			}
		}
	}

	return true
}

// filtrarRasgos keeps only catalog codes, dedups preserving first-seen order,
// and caps to 3. Returns nil when no valid codes remain.
func filtrarRasgos(rasgos []string) []string {
	seen := make(map[string]struct{}, len(rasgos))
	result := make([]string, 0, 3)
	for _, code := range rasgos {
		if !EsRasgoValido(code) {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
		if len(result) == 3 {
			break
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
