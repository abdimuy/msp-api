//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var accionesNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

const accionesClienteID = 24037

func seedPendingDraft(deps *copilotoDeps, clienteID int, borrador string, now time.Time) *domain.Decision {
	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: clienteID,
		Accion:    domain.AccionResponder,
		Resultado: domain.ResultadoPropuesto,
		Borrador:  borrador,
		Now:       now,
	})
	if err != nil {
		panic(err)
	}
	_ = deps.decisionRepo.Insertar(context.Background(), d)
	return d
}

// ─── AprobarBorrador ────────────────────────────────────────────────────────

func TestAprobarBorrador_Success_EnqueuesAndAppendsAprobado(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "aquí tiene nuestras opciones", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.NoError(t, err)

	assert.Equal(t, 1, deps.mensajeRepo.insertadosCount())
	enviados := deps.mensajeRepo.insertadosSnapshot()
	require.Len(t, enviados, 1)
	assert.Equal(t, "aquí tiene nuestras opciones", enviados[0].Cuerpo())
	assert.Equal(t, "238 111 2222", enviados[0].Telefono())

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.Len(t, decisiones, 2)
	newest := decisiones[len(decisiones)-1]
	assert.Equal(t, domain.ResultadoAprobado, newest.Resultado())
	assert.Equal(t, "aquí tiene nuestras opciones", newest.Borrador())
}

func TestAprobarBorrador_NoPendingDraft_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.ErrorIs(t, err, app.ErrNoHayBorradorPendiente)
}

func TestAprobarBorrador_Idempotent_SecondCallIsNoOp(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador original", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	require.NoError(t, svc.AprobarBorrador(context.Background(), accionesClienteID))
	assert.Equal(t, 1, deps.mensajeRepo.insertadosCount())

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.ErrorIs(t, err, app.ErrNoHayBorradorPendiente)
	assert.Equal(t, 1, deps.mensajeRepo.insertadosCount(), "second approve must NOT re-enqueue")
}

func TestAprobarBorrador_Idempotent_TiedCreatedAt_SecondCallIsStillNoOp(t *testing.T) {
	t.Parallel()
	// Regression test for the tie-break bug: seed the pending draft at EXACTLY
	// the service clock's fixed "now", so the aprobado decision AprobarBorrador
	// appends shares an IDENTICAL CreatedAt with the propuesto it supersedes —
	// reproducing legacy Firebird's second-resolution TIMESTAMP colliding on
	// two decisions written moments apart in the same operator action. Before
	// the fix, newestDecisionPorClienteID's strict ".After()" scan returned the
	// FIRST (propuesto) of the tied pair, so a 2nd AprobarBorrador would find a
	// "pending draft" again and re-enqueue the message.
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador original", accionesNow)
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	require.NoError(t, svc.AprobarBorrador(context.Background(), accionesClienteID))
	assert.Equal(t, 1, deps.mensajeRepo.insertadosCount())

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.Len(t, decisiones, 2)
	require.True(t, decisiones[0].CreatedAt().Equal(decisiones[1].CreatedAt()), "test setup must produce a genuine CreatedAt tie")

	err = svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.ErrorIs(t, err, app.ErrNoHayBorradorPendiente)
	assert.Equal(t, 1, deps.mensajeRepo.insertadosCount(), "second approve must NOT re-enqueue, even with a tied CreatedAt")
}

func TestAprobarBorrador_DecisionListError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.decisionRepo.listarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.Error(t, err)
}

func TestAprobarBorrador_FactsReaderError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	deps.factsReader.err = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.Error(t, err)
}

func TestAprobarBorrador_InvalidSegmento_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "segmento_invalido", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.ErrorIs(t, err, domain.ErrSegmentoInvalido)
}

func TestAprobarBorrador_MensajeRepoInsertarError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	deps.mensajeRepo.insertarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.Error(t, err)
}

func TestAprobarBorrador_DecisionInsertarError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)
	deps.decisionRepo.insertarErr = assert.AnError

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.Error(t, err)
}

func TestAprobarBorrador_NoContactFacts_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	svc := newCopilotoService(deps, accionesNow)

	err := svc.AprobarBorrador(context.Background(), accionesClienteID)
	require.ErrorIs(t, err, app.ErrClienteSinDatosContacto)
	assert.Zero(t, deps.mensajeRepo.insertadosCount())
}

// ─── EditarYAprobar ─────────────────────────────────────────────────────────

func TestEditarYAprobar_UsesHumanText(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador original de la ia", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	err := svc.EditarYAprobar(context.Background(), accionesClienteID, "texto editado por el operador")
	require.NoError(t, err)

	enviados := deps.mensajeRepo.insertadosSnapshot()
	require.Len(t, enviados, 1)
	assert.Equal(t, "texto editado por el operador", enviados[0].Cuerpo())

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), accionesClienteID)
	require.NoError(t, err)
	newest := decisiones[len(decisiones)-1]
	assert.Equal(t, domain.ResultadoEditado, newest.Resultado())
	assert.Equal(t, "texto editado por el operador", newest.Borrador())
}

func TestEditarYAprobar_TextoVacio_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	seedPendingDraft(deps, accionesClienteID, "borrador", accionesNow.Add(-time.Minute))
	deps.factsReader.facts = map[int]*outbound.ClienteFacts{
		accionesClienteID: factsFor("Juan Pérez", "recien_liquidado", "238 111 2222"),
	}
	svc := newCopilotoService(deps, accionesNow)

	err := svc.EditarYAprobar(context.Background(), accionesClienteID, "   ")
	require.ErrorIs(t, err, app.ErrTextoEditadoVacio)
	assert.Zero(t, deps.mensajeRepo.insertadosCount())
}

func TestEditarYAprobar_NoPendingDraft_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, accionesNow)

	err := svc.EditarYAprobar(context.Background(), accionesClienteID, "texto")
	require.ErrorIs(t, err, app.ErrNoHayBorradorPendiente)
}

func TestEditarYAprobar_DecisionListError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.decisionRepo.listarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.EditarYAprobar(context.Background(), accionesClienteID, "texto")
	require.Error(t, err)
}

// ─── Escalar ────────────────────────────────────────────────────────────────

func TestEscalar_FlipsEstadoAndLogsEscalado(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.NoError(t, err)

	conv, err := deps.convRepo.Get(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.NotNil(t, conv)
	assert.Equal(t, domain.EstadoEscalado, conv.Estado())
	assert.Equal(t, "operador.rita", conv.AsignadoA())

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.Len(t, decisiones, 1)
	assert.Equal(t, domain.AccionEscalar, decisiones[0].AccionPropuesta())
	assert.Equal(t, domain.ResultadoEscalado, decisiones[0].Resultado())
	assert.Equal(t, "escalado por el operador", decisiones[0].RazonEscalamiento())
}

func TestEscalar_ConversacionNoEncontrada_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.ErrorIs(t, err, app.ErrConversacionNoEncontrada)
}

func TestEscalar_ConvRepoGetError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.getErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.Error(t, err)
}

func TestEscalar_IllegalTransition_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	// EstadoInteresado does not allow MarcarEscalada per the domain state
	// machine (Slice A) — must surface ErrConversacionTransicionInvalida.
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoInteresado, accionesNow.Add(-time.Hour))
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestEscalar_DecisionInsertarError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.decisionRepo.insertarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.Error(t, err)
}

func TestEscalar_UpsertError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.convRepo.upsertErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	err := svc.Escalar(context.Background(), accionesClienteID, "operador.rita")
	require.Error(t, err)
}

// ─── Dictar ─────────────────────────────────────────────────────────────────

func TestDictar_ProducesPendingDraft(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.copilotoLLM.redactarOut = "aquí tiene la información que pidió"
	svc := newCopilotoService(deps, accionesNow)

	borrador, err := svc.Dictar(context.Background(), accionesClienteID, "avísale que ya llegó su pedido")
	require.NoError(t, err)
	assert.Equal(t, "aquí tiene la información que pidió", borrador)

	turnos, err := deps.convRepo.ListarTurnos(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.Len(t, turnos, 1)
	assert.Equal(t, domain.DireccionSaliente, turnos[0].Direccion())
	assert.Equal(t, domain.AutorIA, turnos[0].Autor())
	assert.Equal(t, "aquí tiene la información que pidió", turnos[0].Cuerpo())

	decisiones, err := deps.decisionRepo.ListarPorCliente(context.Background(), accionesClienteID)
	require.NoError(t, err)
	require.Len(t, decisiones, 1)
	assert.Equal(t, domain.AccionResponder, decisiones[0].AccionPropuesta())
	assert.Equal(t, domain.ResultadoPropuesto, decisiones[0].Resultado())
	assert.Equal(t, "aquí tiene la información que pidió", decisiones[0].Borrador())

	// Shadow mode: dictar never enqueues on its own.
	assert.Zero(t, deps.mensajeRepo.insertadosCount())
}

func TestDictar_IntencionVacia_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "  ")
	require.ErrorIs(t, err, app.ErrIntencionVacia)
}

func TestDictar_ConversacionNoEncontrada_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.ErrorIs(t, err, app.ErrConversacionNoEncontrada)
}

func TestDictar_ConvRepoGetError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.getErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.Error(t, err)
}

func TestDictar_ListarTurnosError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.convRepo.listarTurnosErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.Error(t, err)
}

func TestDictar_RedactarError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.copilotoLLM.redactarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.Error(t, err)
}

func TestDictar_AppendTurnoError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.convRepo.appendTurnoErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.Error(t, err)
}

func TestDictar_DecisionInsertarError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, accionesClienteID, domain.EstadoConversando, accionesNow.Add(-time.Hour))
	deps.decisionRepo.insertarErr = assert.AnError
	svc := newCopilotoService(deps, accionesNow)

	_, err := svc.Dictar(context.Background(), accionesClienteID, "algo")
	require.Error(t, err)
}
