//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// DecisionFinal is the app layer's final, deterministic call on one raw LLM
// AnalizarOutput. It is what actually gets persisted as a domain.Decision —
// the LLM only PROPOSES (via AnalizarOutput.Accion/RazonEscalamiento, which
// triar never even reads); the app's policy governs.
type DecisionFinal struct {
	// Accion is the FINAL action: domain.AccionResponder or domain.AccionEscalar.
	Accion domain.Accion
	// Resultado mirrors Accion into the ResultadoDecision the Decision is
	// created with: domain.ResultadoPropuesto when responding,
	// domain.ResultadoEscalado when escalating.
	Resultado domain.ResultadoDecision
	// RazonEscalamiento explains why triar escalated. Empty when Accion is
	// domain.AccionResponder.
	RazonEscalamiento string
}

// responderFinal is the DecisionFinal for the "everything is fine, let the
// draft go out" case — no signal fired.
var responderFinal = DecisionFinal{Accion: domain.AccionResponder, Resultado: domain.ResultadoPropuesto}

// escalarFinal builds the DecisionFinal for an escalation with razon.
func escalarFinal(razon string) DecisionFinal {
	return DecisionFinal{Accion: domain.AccionEscalar, Resultado: domain.ResultadoEscalado, RazonEscalamiento: razon}
}

// triar decides the FINAL action from the raw LLM output out. Pure and
// deterministic — no I/O, no clock, no randomness — so it can be (and is)
// exhaustively unit tested. This is the safety-critical function of the
// copiloto: any ESCALATION signal, plus the confidence guard and the
// debt-figure guard, forces escalation regardless of what the LLM itself
// proposed in out.Accion/out.RazonEscalamiento (deliberately never read here).
//
// domain.SenalCompra (buying INTEREST) is deliberately NOT an escalation
// trigger: interest is the copiloto's job to sell to, so it falls through to
// responderFinal. Only domain.SenalCierre (ready-to-close) escalates the sale
// to a human — the "escalamiento invertido" that hands off at the good moment.
//
// Evaluated in this precedence — the first rule that fires wins the
// RazonEscalamiento, but every rule below escalates, so precedence only
// decides WHICH reason is recorded, never whether to escalate:
//
//  1. domain.SenalDeuda present            → razonDeuda.
//  2. domain.SenalCierre present           → razonCierre.
//  3. domain.SenalPideHumano present       → razonPideHumano.
//  4. domain.SenalEnojoLoop present        → razonEnojoLoop.
//  5. domain.SenalFueraAllowlist present   → razonFueraAllowlist.
//  6. domain.SenalConfianzaBaja present, OR out.Confianza < umbralConfianzaBaja
//     → razonConfianzaBaja.
//  7. borradorMencionaCifraDeuda(out.Borrador) → razonCifraDeuda (the
//     draft itself leaks a debt figure, independent of any Senal).
//  8. Any raw signal string that is not a recognized domain.Senal
//     → razonFueraAllowlist (unknown is treated as suspicious, never ignored).
//  9. strings.TrimSpace(out.Borrador) == "" → razonBorradorVacio (a DEFENSIVE
//     escalate: every rule above resolved to "respond", but the LLM handed
//     back an empty/whitespace draft — an LLM hiccup, not a real reply. This
//     guard is centralized HERE, not in the caller, precisely so
//     persistirTurnoEntranteYDecision never attempts to build a saliente
//     Turno with an empty Cuerpo — domain.CrearTurno would reject it
//     (ErrTurnoCuerpoRequerido) and roll back the WHOLE inbound turn,
//     including the entrante Turno that must always be recorded).
//  10. None of the above (incl. a lone domain.SenalCompra) → responderFinal.
func triar(out outbound.AnalizarOutput) DecisionFinal {
	senales, tieneSenalDesconocida := clasificarSenales(out.Senales)

	switch {
	case senales[domain.SenalDeuda]:
		return escalarFinal(razonDeuda)
	case senales[domain.SenalCierre]:
		return escalarFinal(razonCierre)
	case senales[domain.SenalPideHumano]:
		return escalarFinal(razonPideHumano)
	case senales[domain.SenalEnojoLoop]:
		return escalarFinal(razonEnojoLoop)
	case senales[domain.SenalFueraAllowlist]:
		return escalarFinal(razonFueraAllowlist)
	case senales[domain.SenalConfianzaBaja] || out.Confianza < umbralConfianzaBaja:
		return escalarFinal(razonConfianzaBaja)
	case borradorMencionaCifraDeuda(out.Borrador):
		return escalarFinal(razonCifraDeuda)
	case tieneSenalDesconocida:
		return escalarFinal(razonFueraAllowlist)
	case strings.TrimSpace(out.Borrador) == "":
		return escalarFinal(razonBorradorVacio)
	default:
		return responderFinal
	}
}

// clasificarSenales splits raw LLM signal strings into the set of recognized
// domain.Senal values present, plus whether any raw string was NOT a
// recognized value (unknown signals are suspicious by default — see triar
// rule 8).
func clasificarSenales(raw []string) (map[domain.Senal]bool, bool) {
	senales := make(map[domain.Senal]bool, len(raw))
	tieneDesconocida := false
	for _, s := range raw {
		senal := domain.Senal(s)
		if !senal.Valido() {
			tieneDesconocida = true
			continue
		}
		senales[senal] = true
	}
	return senales, tieneDesconocida
}

// debtKeywordPatterns compiles debtKeywords once into WHOLE-WORD/PHRASE
// regexes (\b-bounded) matched against normalizarTexto's output. Whole-word
// matching avoids the substring-collision false positive a plain
// strings.Contains scan would produce — e.g. the single-word keyword "resta"
// must NOT match inside "prestamo" ("préstamo", a loan OFFER, not a debt);
// \b requires a word boundary on both sides of the match, which "resta"
// embedded mid-word in "prestamo" does not have. Multi-word keywords (e.g.
// "lo que resta") match as a \b-bounded phrase, spaces included literally.
var debtKeywordPatterns = compileDebtKeywordPatterns()

// compileDebtKeywordPatterns builds debtKeywordPatterns from debtKeywords.
func compileDebtKeywordPatterns() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(debtKeywords))
	for i, kw := range debtKeywords {
		patterns[i] = regexp.MustCompile(`\b` + regexp.QuoteMeta(normalizarTexto(kw)) + `\b`)
	}
	return patterns
}

// borradorMencionaCifraDeuda reports whether borrador states a debt figure —
// a debtKeywords WHOLE-WORD hit CO-OCCURRING with a numeric or "$" token. A
// bare monetary figure alone is NOT enough: new-purchase amounts (enganche/
// parcialidad) are explicitly allowed by the allowlist, so "$300 de
// enganche" must not trip this guard, but "debe $300" must.
func borradorMencionaCifraDeuda(borrador string) bool {
	if borrador == "" {
		return false
	}
	normalizado := normalizarTexto(borrador)

	tieneKeyword := false
	for _, pattern := range debtKeywordPatterns {
		if pattern.MatchString(normalizado) {
			tieneKeyword = true
			break
		}
	}
	if !tieneKeyword {
		return false
	}

	return strings.ContainsAny(borrador, "0123456789$")
}

// normalizarTexto lowercases and NFC-normalizes s for stable keyword matching
// regardless of accent composition — calques the pattern in
// internal/analytics/app/narrativa_validate.go.
func normalizarTexto(s string) string {
	return strings.ToLower(norm.NFC.String(s))
}
