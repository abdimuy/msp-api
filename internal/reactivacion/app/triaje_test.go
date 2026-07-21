//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
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

func TestTriar_EachSenalEscalates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		senal string
		razon string
	}{
		{"deuda", string(domain.SenalDeuda), razonDeuda},
		{"senal_compra", string(domain.SenalCompra), razonSenalCompra},
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

func TestTriar_PrecedenceOrder_DeudaBeatsCompra(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalCompra), string(domain.SenalDeuda)}
	df := triar(out)
	assert.Equal(t, razonDeuda, df.RazonEscalamiento)
}

func TestTriar_PrecedenceOrder_CompraBeatsPideHumano(t *testing.T) {
	t.Parallel()
	out := cleanOutput()
	out.Senales = []string{string(domain.SenalPideHumano), string(domain.SenalCompra)}
	df := triar(out)
	assert.Equal(t, razonSenalCompra, df.RazonEscalamiento)
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

func TestBorradorMencionaCifraDeuda_CaseAndAccentInsensitive(t *testing.T) {
	t.Parallel()
	assert.True(t, borradorMencionaCifraDeuda("SU DEUDA ES DE $100"))
	assert.True(t, borradorMencionaCifraDeuda("Su Adeudo Es De 100 Pesos"))
}

func TestBorradorMencionaCifraDeuda_EmptyBorrador(t *testing.T) {
	t.Parallel()
	assert.False(t, borradorMencionaCifraDeuda(""))
}
