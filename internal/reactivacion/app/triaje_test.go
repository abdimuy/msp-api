//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// cleanOutput is a raw LLM output with no signals, high confidence, and a
// borrador with no debt figure — the baseline "everything is fine" case.
func cleanOutput() outbound.AnalizarOutput {
	return outbound.AnalizarOutput{
		Intencion: "pregunta por catálogo",
		Confianza: 90,
		Senales:   nil,
		Borrador:  "Claro, con gusto le comparto nuestras opciones disponibles.",
	}
}

func TestTriar_CleanCase_Responde(t *testing.T) {
	t.Parallel()
	df := triar(cleanOutput())
	assert.Equal(t, domain.AccionResponder, df.Accion)
	assert.Equal(t, domain.ResultadoPropuesto, df.Resultado)
	assert.Empty(t, df.RazonEscalamiento)
}

func TestTriar_CleanCase_EmptyBorrador_DefensiveEscala(t *testing.T) {
	t.Parallel()
	// Every other rule resolves to "respond" (no signal, high confidence, no
	// debt figure), but the LLM handed back an empty/whitespace draft — that
	// must be treated as an LLM hiccup and DEFENSIVELY escalated, never
	// silently sent as a blank message or allowed through to
	// persistirTurnoEntranteYDecision (which would fail building the saliente
	// Turno and roll back the whole inbound turn).
	cases := []string{"", "   ", "\n\t "}
	for _, borrador := range cases {
		t.Run(fmt.Sprintf("borrador=%q", borrador), func(t *testing.T) {
			t.Parallel()
			out := cleanOutput()
			out.Senales = nil
			out.Borrador = borrador
			df := triar(out)
			assert.Equal(t, domain.AccionEscalar, df.Accion)
			assert.Equal(t, domain.ResultadoEscalado, df.Resultado)
			assert.Equal(t, razonBorradorVacio, df.RazonEscalamiento)
		})
	}
}

func TestTriar_EachSenalEscalates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		senal string
		razon string
	}{
		{"deuda", string(domain.SenalDeuda), razonDeuda},
		{"senal_cierre", string(domain.SenalCierre), razonCierre},
		{"pide_humano", string(domain.SenalPideHumano), razonPideHumano},
		{"enojo_loop", string(domain.SenalEnojoLoop), razonEnojoLoop},
		{"fuera_allowlist", string(domain.SenalFueraAllowlist), razonFueraAllowlist},
		{"confianza_baja_senal", string(domain.SenalConfianzaBaja), razonConfianzaBaja},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := cleanOutput()
			out.Senales = []string{tc.senal}
			df := triar(out)
			assert.Equal(t, domain.AccionEscalar, df.Accion)
			assert.Equal(t, domain.ResultadoEscalado, df.Resultado)
			assert.Equal(t, tc.razon, df.RazonEscalamiento)
		})
	}
}

// ─── senal_compra (interés) es INFORMATIVA: no escala (taxonomía B) ──────────

func TestTriar_SenalCompra_Informational_Responde(t *testing.T) {
	t.Parallel()
	// A lone senal_compra (buying INTEREST) is the copiloto's job to sell to —
	// it must RESPOND, never escalate. This is the core of taxonomía B: only
	// senal_cierre escalates the sale, interest does not.
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalCompra)}
	df := triar(out)
	assert.Equal(t, domain.AccionResponder, df.Accion)
	assert.Equal(t, domain.ResultadoPropuesto, df.Resultado)
	assert.Empty(t, df.RazonEscalamiento)
}

func TestTriar_SenalCompra_WithEscalatingSenal_Escala(t *testing.T) {
	t.Parallel()
	// senal_compra never BLOCKS an escalation: paired with any real escalation
	// signal, the escalation still fires with the other signal's reason.
	cases := []struct {
		name  string
		otra  string
		razon string
	}{
		{"compra+deuda", string(domain.SenalDeuda), razonDeuda},
		{"compra+cierre", string(domain.SenalCierre), razonCierre},
		{"compra+pide_humano", string(domain.SenalPideHumano), razonPideHumano},
		{"compra+enojo_loop", string(domain.SenalEnojoLoop), razonEnojoLoop},
		{"compra+fuera_allowlist", string(domain.SenalFueraAllowlist), razonFueraAllowlist},
		{"compra+confianza_baja", string(domain.SenalConfianzaBaja), razonConfianzaBaja},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := cleanOutput()
			out.Senales = []string{string(domain.SenalCompra), tc.otra}
			df := triar(out)
			assert.Equal(t, domain.AccionEscalar, df.Accion)
			assert.Equal(t, tc.razon, df.RazonEscalamiento)
		})
	}
}

func TestTriar_SenalCompra_EmptyBorrador_DefensiveEscala(t *testing.T) {
	t.Parallel()
	// Interest but the LLM handed back no draft → the empty-borrador defensive
	// guard still fires (senal_compra does not suppress it).
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalCompra)}
	out.Borrador = "   "
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonBorradorVacio, df.RazonEscalamiento)
}

func TestTriar_SenalCompra_LowConfidence_Escala(t *testing.T) {
	t.Parallel()
	// Interest but low confidence → the confidence guard still escalates
	// (ambiguous read is never auto-sent, even on an interest turn).
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalCompra)}
	out.Confianza = umbralConfianzaBaja - 1
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonConfianzaBaja, df.RazonEscalamiento)
}

func TestTriar_ConfianzaBaja_ByThreshold_NoSenal(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Confianza = umbralConfianzaBaja - 1
	out.Senales = nil
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonConfianzaBaja, df.RazonEscalamiento)
}

func TestTriar_ConfianzaEnUmbral_NoEscala(t *testing.T) {
	t.Parallel()
	// Confianza exactly AT the threshold is NOT "below" it — the guard is a
	// strict less-than, matching the brief's "out.Confianza < umbralConfianzaBaja".
	out := cleanOutput()
	out.Confianza = umbralConfianzaBaja
	out.Senales = nil
	df := triar(out)
	assert.Equal(t, domain.AccionResponder, df.Accion)
}

func TestTriar_ConfianzaAlta_ConSenalConfianzaBaja_Escala(t *testing.T) {
	t.Parallel()
	// Even with high numeric confidence, the LLM's own confianza_baja signal
	// still forces escalation — ANY signal fires the guard.
	out := cleanOutput()
	out.Confianza = 99
	out.Senales = []string{string(domain.SenalConfianzaBaja)}
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonConfianzaBaja, df.RazonEscalamiento)
}

func TestTriar_DebtFigureGuard_KeywordAndFigure_Escala(t *testing.T) {
	t.Parallel()
	cases := []string{
		"Su saldo pendiente es de $1,200.",
		"Todavía debe 500 pesos.",
		"Le resta abonar $300 de su adeudo.",
		"Su deuda asciende a 2000.",
		"Tiene un pago vencido de $500.",
		"Está atrasado con $500 de su cuenta.",
		"El cliente aparece como moroso por $500.",
		"Le quedan $500 por cubrir.",
		"Tiene un abono pendiente de $500.",
		"Esto es lo que resta: $500.",
	}
	for _, borrador := range cases {
		t.Run(borrador, func(t *testing.T) {
			t.Parallel()
			out := cleanOutput()
			out.Senales = nil
			out.Borrador = borrador
			df := triar(out)
			assert.Equal(t, domain.AccionEscalar, df.Accion)
			assert.Equal(t, razonCifraDeuda, df.RazonEscalamiento)
		})
	}
}

func TestTriar_DebtFigureGuard_KeywordOnlyNoFigure_NoEscala(t *testing.T) {
	t.Parallel()
	// "pendiente" without any accompanying numeric/$ token must NOT trigger the
	// guard — the keyword alone is not proof of a stated debt figure.
	out := cleanOutput()
	out.Senales = nil
	out.Borrador = "Su pago quedó pendiente de confirmación, le avisamos pronto."
	df := triar(out)
	assert.Equal(t, domain.AccionResponder, df.Accion)
}

func TestTriar_DebtFigureGuard_FigureOnlyNoKeyword_NoEscala(t *testing.T) {
	t.Parallel()
	// A bare monetary figure for a NEW purchase (enganche/parcialidad) must be
	// allowed — only co-occurrence with a debt keyword triggers the guard.
	out := cleanOutput()
	out.Senales = nil
	out.Borrador = "$300 de enganche y el resto en 3 parcialidades de $150."
	df := triar(out)
	assert.Equal(t, domain.AccionResponder, df.Accion)
}

func TestTriar_DebtFigureGuard_PrestamoWordCollision_NoEscala(t *testing.T) {
	t.Parallel()
	// Whole-word matching regression: "resta" must NOT match embedded inside
	// "préstamo" (a loan OFFER, not a debt) — a naive substring scan would
	// false-positive here.
	out := cleanOutput()
	out.Senales = nil
	out.Borrador = "$5000 de préstamo"
	df := triar(out)
	assert.Equal(t, domain.AccionResponder, df.Accion)
}

func TestTriar_UnknownSenal_TreatedAsFueraAllowlist(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{"algo_no_reconocido"}
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonFueraAllowlist, df.RazonEscalamiento)
}

func TestTriar_UnknownSenal_MixedWithKnownButNotConfidenceGuard(t *testing.T) {
	t.Parallel()
	// An unknown signal alongside a KNOWN, higher-priority signal must still
	// escalate with the known signal's reason (precedence order), since any
	// hit escalates and the loop returns on the first match.
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalDeuda), "algo_no_reconocido"}
	df := triar(out)
	assert.Equal(t, domain.AccionEscalar, df.Accion)
	assert.Equal(t, razonDeuda, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_DeudaBeatsCierre(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalCierre), string(domain.SenalDeuda)}
	df := triar(out)
	assert.Equal(t, razonDeuda, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_CierreBeatsPideHumano(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalPideHumano), string(domain.SenalCierre)}
	df := triar(out)
	assert.Equal(t, razonCierre, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_PideHumanoBeatsEnojoLoop(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalEnojoLoop), string(domain.SenalPideHumano)}
	df := triar(out)
	assert.Equal(t, razonPideHumano, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_EnojoLoopBeatsFueraAllowlist(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalFueraAllowlist), string(domain.SenalEnojoLoop)}
	df := triar(out)
	assert.Equal(t, razonEnojoLoop, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_FueraAllowlistBeatsConfianzaBaja(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Confianza = 10 // would also trigger the numeric confidence guard
	out.Senales = []string{string(domain.SenalFueraAllowlist)}
	df := triar(out)
	assert.Equal(t, razonFueraAllowlist, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_ConfianzaBajaBeatsDebtFigureGuard(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Confianza = umbralConfianzaBaja - 1
	out.Senales = nil
	out.Borrador = "Su saldo pendiente es de $500."
	df := triar(out)
	assert.Equal(t, razonConfianzaBaja, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_DebtFigureGuardBeatsUnknownSenal(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{"algo_no_reconocido"}
	out.Borrador = "Su saldo pendiente es de $500."
	df := triar(out)
	assert.Equal(t, razonCifraDeuda, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_UnknownSenalBeatsEmptyBorradorGuard(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{"algo_no_reconocido"}
	out.Borrador = "" // would ALSO trip the empty-borrador guard on its own
	df := triar(out)
	assert.Equal(t, razonFueraAllowlist, df.RazonEscalamiento)
}

func TestBorradorMencionaCifraDeuda_CaseAndAccentInsensitive(t *testing.T) {
	t.Parallel()
	assert.True(t, borradorMencionaCifraDeuda("SU DEUDA ES DE $100"))
	assert.True(t, borradorMencionaCifraDeuda("Su Adeudo Es De 100 Pesos"))
}

func TestBorradorMencionaCifraDeuda_EmptyBorrador(t *testing.T) {
	t.Parallel()
	assert.False(t, borradorMencionaCifraDeuda(""))
}

func TestBorradorMencionaCifraDeuda_WholeWordVsSubstringCollision(t *testing.T) {
	t.Parallel()
	// "resta" must not match embedded mid-word inside "préstamo"/"prestamo" — a
	// loan OFFER, not a debt. Whole-word (\b-bounded) matching fixes what a
	// naive strings.Contains scan would false-positive on.
	assert.False(t, borradorMencionaCifraDeuda("$5000 de préstamo"))
	assert.False(t, borradorMencionaCifraDeuda("$5000 de prestamo"))
	// But the standalone word "resta" (and "restante") must still match.
	assert.True(t, borradorMencionaCifraDeuda("esto es lo que resta: $500"))
	assert.True(t, borradorMencionaCifraDeuda("el monto restante es $500"))
}

func TestBorradorMencionaCifraDeuda_ExpandedKeywords(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"tiene un pago vencido de $500":    true,
		"está atrasado con $500":           true,
		"lleva un atraso de $500":          true,
		"aparece como moroso por $500":     true,
		"le quedan $500 por cubrir":        true,
		"tiene un abono pendiente de $500": true,
		"esto es lo que resta: $500":       true,
		"saldo pendiente de $300":          true,
		"$300 de enganche":                 false,
		"$5000 de préstamo":                false,
	}
	for borrador, want := range cases {
		t.Run(borrador, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, borradorMencionaCifraDeuda(borrador))
		})
	}
}

func TestDebtKeywordPatterns_CompiledOnePerKeyword(t *testing.T) {
	t.Parallel()
	// Guards against debtKeywords/debtKeywordPatterns drifting out of sync.
	assert.Len(t, debtKeywordPatterns, len(debtKeywords))
}
