//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/llm"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var procesarNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

const procesarClienteID = 24037

func TestProcesarMensajeEntrante_MensajeVacio_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "   ")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_BuySignal_Escala(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Intencion: "quiere comprar",
		Confianza: 95,
		Senales:   []string{string(domain.SenalCompra)},
		Borrador:  "",
	}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "quiero otro juego de sala")
	require.NoError(t, err)
	assert.True(t, res.Escalada)
	assert.Empty(t, res.Borrador)
	require.NotNil(t, res.Decision)
	assert.Equal(t, domain.AccionEscalar, res.Decision.AccionPropuesta())
	assert.Equal(t, domain.ResultadoEscalado, res.Decision.Resultado())

	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, domain.EstadoEscalado, conv.Estado())
}

func TestProcesarMensajeEntrante_Deuda_Escala(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Confianza: 90,
		Senales:   []string{string(domain.SenalDeuda)},
	}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "cuánto debo todavía")
	require.NoError(t, err)
	assert.True(t, res.Escalada)
	assert.Equal(t, "deuda", res.Decision.RazonEscalamiento())
}

func TestProcesarMensajeEntrante_ConfianzaBaja_Escala(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Confianza: 10,
		Borrador:  "no estoy segura de lo que pide",
	}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "mmm no sé")
	require.NoError(t, err)
	assert.True(t, res.Escalada)
}

func TestProcesarMensajeEntrante_Clean_PendingDraftCreated(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Intencion: "pregunta por horario",
		Confianza: 92,
		Borrador:  "Abrimos de 9 a 6, de lunes a sábado.",
	}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "a qué hora abren")
	require.NoError(t, err)
	assert.False(t, res.Escalada)
	assert.Equal(t, "Abrimos de 9 a 6, de lunes a sábado.", res.Borrador)
	require.NotNil(t, res.Decision)
	assert.Equal(t, domain.ResultadoPropuesto, res.Decision.Resultado())

	turnos, err := deps.convRepo.ListarTurnos(context.Background(), procesarClienteID)
	require.NoError(t, err)
	require.Len(t, turnos, 2, "one entrante turno + one PENDING saliente draft turno")
	assert.Equal(t, domain.DireccionEntrante, turnos[0].Direccion())
	assert.Equal(t, domain.AutorCliente, turnos[0].Autor())
	assert.Equal(t, domain.DireccionSaliente, turnos[1].Direccion())
	assert.Equal(t, domain.AutorIA, turnos[1].Autor())
	assert.Equal(t, "Abrimos de 9 a 6, de lunes a sábado.", turnos[1].Cuerpo())

	// Shadow mode: the draft is never enqueued via the Fase 2 channel.
	assert.Zero(t, deps.mensajeRepo.insertadosCount())
}

func TestProcesarMensajeEntrante_EstadoTransitions_ContactadoToRespondioToConversando(t *testing.T) {
	t.Parallel()
	// A single "responder" turn moves contactado straight through respondio to
	// conversando WITHIN the same call (both transitions fire unconditionally
	// per the flow), so "respondio" is never independently observable here —
	// only the end state, conversando.
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "claro que sí"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "primer mensaje")
	require.NoError(t, err)
	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoConversando, conv.Estado())

	// A second inbound "responder" turn is idempotent: conversando → conversando.
	_, err = svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "segundo mensaje")
	require.NoError(t, err)
	conv, err = deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoConversando, conv.Estado())
}

func TestProcesarMensajeEntrante_EstadoTransitions_ToEscalado(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Confianza: 90,
		Senales:   []string{string(domain.SenalPideHumano)},
	}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "quiero hablar con alguien")
	require.NoError(t, err)
	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoEscalado, conv.Estado())
}

func TestProcesarMensajeEntrante_NotaDistilledOnceAcrossTwoInbounds(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.notaReader.notas = map[int]string{procesarClienteID: "cliente prefiere hablar por la tarde"}
	deps.copilotoLLM.destilarOut = outbound.NotaOutput{Contexto: "prefiere tarde", Banderas: []string{"prefiere_tarde"}}
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "entendido"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "mensaje uno")
	require.NoError(t, err)
	_, err = svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "mensaje dos")
	require.NoError(t, err)

	assert.Equal(t, 1, deps.copilotoLLM.destilarCallCount())

	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, "prefiere tarde", conv.ContextoNota())
}

func TestProcesarMensajeEntrante_DecisionLoggedWithRightFields(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{
		Intencion: "pregunta de catálogo",
		Confianza: 88,
		Senales:   nil,
		Borrador:  "aquí tiene nuestras opciones",
		Evidencia: []string{"mencionó recámara"},
	}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "qué tienen de recámaras")
	require.NoError(t, err)

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), procesarClienteID)
	require.NoError(t, err)
	require.Len(t, decisiones, 1)
	d := decisiones[0]
	assert.Equal(t, "pregunta de catálogo", d.Intencion())
	assert.Equal(t, 88, d.Confianza())
	assert.Equal(t, []string{"mencionó recámara"}, d.Evidencia())
	assert.Equal(t, procesarClienteID, d.ClienteID())
	assert.NotEmpty(t, d.TurnoRef())
	assert.Same(t, res.Decision, d)
}

func TestProcesarMensajeEntrante_LLMDisabled_SafeEscalate_TurnAndDecisionRecorded(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarErr = llm.ErrLLMDisabled
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.NoError(t, err, "a disabled LLM must degrade to a safe escalate, never fail the whole flow")
	assert.True(t, res.Escalada)
	assert.Empty(t, res.Borrador)
	require.NotNil(t, res.Decision)
	assert.Equal(t, domain.AccionEscalar, res.Decision.AccionPropuesta())
	assert.Equal(t, "copiloto no disponible", res.Decision.RazonEscalamiento())
	assert.Equal(t, 0, res.Decision.Confianza())

	turnos, err := deps.convRepo.ListarTurnos(context.Background(), procesarClienteID)
	require.NoError(t, err)
	require.Len(t, turnos, 1, "the inbound turno is still recorded even in the safe-fallback path")

	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoEscalado, conv.Estado())
}

func TestProcesarMensajeEntrante_LLMTransientError_SafeEscalate(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarErr = &llm.TransientError{Cause: errors.New("timeout")}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.NoError(t, err)
	assert.True(t, res.Escalada)
	assert.Equal(t, "copiloto no disponible", res.Decision.RazonEscalamiento())
}

func TestProcesarMensajeEntrante_LLMOtherError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.copilotoLLM.analizarErr = errors.New("boom inesperado")
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_ExistingConversacion_Reused(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, procesarClienteID, domain.EstadoConversando, procesarNow.Add(-time.Hour))
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "claro"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "otro mensaje")
	require.NoError(t, err)

	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	// Stays conversando (MarcarRespondio only fires from contactado).
	assert.Equal(t, domain.EstadoConversando, conv.Estado())
}

func TestProcesarMensajeEntrante_FactsUsedInAnalizarInput(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		procesarClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "hola Juan"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.NoError(t, err)

	assert.Equal(t, "Juan Pérez", deps.copilotoLLM.lastAnalizarIn.Nombre)
	assert.Equal(t, "recien_liquidado", deps.copilotoLLM.lastAnalizarIn.Segmento)
}

func TestProcesarMensajeEntrante_ConvRepoGetError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.getErr = errors.New("boom")
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_ClienteIDInvalido_NoExistingConversacion_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), 0, "hola")
	require.ErrorIs(t, err, domain.ErrConversacionClienteIDInvalido)
}

func TestProcesarMensajeEntrante_FactsReaderError_DegradesWithEmptyFacts(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.factsReader.err = errors.New("facts down")
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "ok"}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.NoError(t, err, "a facts reader error must degrade, not fail the whole flow")
	assert.False(t, res.Escalada)
	assert.Empty(t, deps.copilotoLLM.lastAnalizarIn.Nombre)
}

func TestProcesarMensajeEntrante_ListarTurnosError_DegradesWithEmptyResumen(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.listarTurnosErr = errors.New("turnos down")
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "ok"}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.NoError(t, err, "a turnos-listing error must degrade, not fail the whole flow")
	assert.False(t, res.Escalada)
}

func TestProcesarMensajeEntrante_AppendTurnoError_PersistFails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.appendTurnoErr = errors.New("append down")
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "ok"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_DecisionInsertarError_PersistFails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.decisionRepo.insertarErr = errors.New("insert down")
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "ok"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_UpsertError_PersistFails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.upsertErr = errors.New("upsert down")
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 90, Borrador: "ok"}
	svc := newCopilotoService(deps, procesarNow)

	_, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "hola")
	require.Error(t, err)
}

func TestProcesarMensajeEntrante_AlreadyEscalado_InboundStillRecordedNoConversandoRegression(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, procesarClienteID, domain.EstadoEscalado, procesarNow.Add(-time.Hour))
	// Even a "clean" LLM read must not pull an escalado conversation back to
	// conversando — a human already owns it.
	deps.copilotoLLM.analizarOut = outbound.AnalizarOutput{Confianza: 95, Borrador: "todo bien"}
	svc := newCopilotoService(deps, procesarNow)

	res, err := svc.ProcesarMensajeEntrante(context.Background(), procesarClienteID, "otro mensaje")
	require.NoError(t, err)
	assert.False(t, res.Escalada, "triar itself did not escalate this turn")

	conv, err := deps.convRepo.Get(context.Background(), procesarClienteID)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoEscalado, conv.Estado(), "must stay escalado, not regress to conversando")
}
