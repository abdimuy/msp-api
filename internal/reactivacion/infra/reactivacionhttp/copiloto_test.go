//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm/copilotofake"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ─── fakes for the copiloto ports (kept local to this package's own test
// suite, mirroring envios_test.go's fakeMensajeRepo) ────────────────────────

// fakeConversacionRepo is a minimal in-memory outbound.ConversacionRepo.
type fakeConversacionRepo struct {
	mu               sync.Mutex
	porCliente       map[int]*domain.Conversacion
	turnosPorCliente map[int][]*domain.Turno
	lastListarParams outbound.ListarConversacionesParams
	listResult       []*domain.Conversacion // when non-nil, returned verbatim by Listar
}

func newFakeConversacionRepo() *fakeConversacionRepo {
	return &fakeConversacionRepo{
		porCliente:       map[int]*domain.Conversacion{},
		turnosPorCliente: map[int][]*domain.Turno{},
	}
}

func (r *fakeConversacionRepo) Get(_ context.Context, clienteID int) (*domain.Conversacion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.porCliente[clienteID], nil
}

func (r *fakeConversacionRepo) Upsert(_ context.Context, c *domain.Conversacion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.porCliente[c.ClienteID()] = c
	return nil
}

func (r *fakeConversacionRepo) Listar(_ context.Context, p outbound.ListarConversacionesParams) ([]*domain.Conversacion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastListarParams = p
	if r.listResult != nil {
		return r.listResult, nil
	}
	out := make([]*domain.Conversacion, 0, len(r.porCliente))
	for _, c := range r.porCliente {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeConversacionRepo) AppendTurno(_ context.Context, t *domain.Turno) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnosPorCliente[t.ClienteID()] = append(r.turnosPorCliente[t.ClienteID()], t)
	return nil
}

func (r *fakeConversacionRepo) ListarTurnos(_ context.Context, clienteID int) ([]*domain.Turno, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*domain.Turno, len(r.turnosPorCliente[clienteID]))
	copy(out, r.turnosPorCliente[clienteID])
	return out, nil
}

// fakeDecisionRepo is a minimal in-memory outbound.DecisionRepo.
type fakeDecisionRepo struct {
	mu         sync.Mutex
	porCliente map[int][]*domain.Decision
}

func newFakeDecisionRepo() *fakeDecisionRepo {
	return &fakeDecisionRepo{porCliente: map[int][]*domain.Decision{}}
}

func (r *fakeDecisionRepo) Insertar(_ context.Context, d *domain.Decision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.porCliente[d.ClienteID()] = append(r.porCliente[d.ClienteID()], d)
	return nil
}

func (r *fakeDecisionRepo) ListarPorCliente(_ context.Context, clienteID int) ([]*domain.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*domain.Decision, len(r.porCliente[clienteID]))
	copy(out, r.porCliente[clienteID])
	return out, nil
}

// fakeNotaReader is a minimal in-memory outbound.NotaReader.
type fakeNotaReader struct{ notas map[int]string }

func (r *fakeNotaReader) GetNotaCliente(_ context.Context, clienteID int) (string, error) {
	return r.notas[clienteID], nil
}

// fakeClienteFactsReader is a minimal in-memory outbound.ClienteFactsReader.
type fakeClienteFactsReader struct {
	facts map[int]*outbound.ClienteFacts
}

func (r *fakeClienteFactsReader) GetFacts(_ context.Context, clienteID int) (*outbound.ClienteFacts, error) {
	return r.facts[clienteID], nil
}

// buildServiceWithCopiloto wires a Service with both the Fase 2 canal AND the
// Fase 3a copiloto dependencies — AprobarBorrador/EditarYAprobar need the
// canal's mensajeRepo, and every copiloto method needs WithCopiloto.
func buildServiceWithCopiloto(
	convRepo outbound.ConversacionRepo,
	decisionRepo outbound.DecisionRepo,
	notaReader outbound.NotaReader,
	llm outbound.CopilotoLLM,
	factsReader outbound.ClienteFactsReader,
) *reactivacionapp.Service {
	svc, _ := buildServiceWithCopilotoAndMensajeRepo(convRepo, decisionRepo, notaReader, llm, factsReader)
	return svc
}

// buildServiceWithCopilotoAndMensajeRepo is buildServiceWithCopiloto but also
// returns the canal's fakeMensajeRepo — tests that need to assert the
// AprobarBorrador/EditarYAprobar enqueue-on-approve side effect (the actual
// text queued for the cliente) use this variant instead of discarding it.
func buildServiceWithCopilotoAndMensajeRepo(
	convRepo outbound.ConversacionRepo,
	decisionRepo outbound.DecisionRepo,
	notaReader outbound.NotaReader,
	llm outbound.CopilotoLLM,
	factsReader outbound.ClienteFactsReader,
) (*reactivacionapp.Service, *fakeMensajeRepo) {
	mensajeRepo := &fakeMensajeRepo{}
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, mensajeRepo, fakeSender{}, true).
		WithCopiloto(convRepo, decisionRepo, notaReader, llm, factsReader)
	return svc, mensajeRepo
}

// doJSON issues method/target with body JSON-encoded (nil = no body) and
// returns the status code and raw response bytes.
func doJSON(h http.Handler, method, target string, body any) (int, []byte) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante ───────

func TestMensajeEntrante_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/conversaciones/101/mensaje-entrante", map[string]string{"mensaje": "hola"})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestMensajeEntrante_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	// reactivacion:leer is NOT enough — this is an operator action.
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/conversaciones/101/mensaje-entrante", map[string]string{"mensaje": "hola"})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestMensajeEntrante_BuySignal_Escala(t *testing.T) {
	t.Parallel()
	llm := &copilotofake.Generator{AnalizarOut: outbound.AnalizarOutput{
		Intencion: "quiere comprar otro juego de sala",
		Confianza: 90,
		Senales:   []string{"senal_compra"},
		Accion:    "responder",
		Borrador:  "aquí tienes un mensaje",
	}}
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, llm, &fakeClienteFactsReader{})

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/201/mensaje-entrante", map[string]string{"mensaje": "quiero comprar otra sala"})
	require.Equal(t, http.StatusOK, code)

	var dto reactivacionhttp.DecisionResultDTO
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.True(t, dto.Escalada)
	assert.Empty(t, dto.Borrador, "an escalated turn must not carry a saved pending draft")
	assert.Equal(t, "escalar", dto.Accion)
	assert.Equal(t, "escalado", dto.Resultado)
	assert.Contains(t, dto.Senales, "senal_compra")
}

func TestMensajeEntrante_CleanOutput_BorradorPendiente(t *testing.T) {
	t.Parallel()
	llm := &copilotofake.Generator{AnalizarOut: outbound.AnalizarOutput{
		Intencion: "saluda y pregunta por horario",
		Confianza: 95,
		Senales:   []string{},
		Accion:    "responder",
		Borrador:  "hola, con gusto te comparto nuestro horario",
	}}
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, llm, &fakeClienteFactsReader{})

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/202/mensaje-entrante", map[string]string{"mensaje": "hola, cual es su horario"})
	require.Equal(t, http.StatusOK, code)

	var dto reactivacionhttp.DecisionResultDTO
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.False(t, dto.Escalada)
	assert.Equal(t, "responder", dto.Accion)
	assert.Equal(t, "propuesto", dto.Resultado)
	assert.Equal(t, "hola, con gusto te comparto nuestro horario", dto.Borrador)
}

func TestMensajeEntrante_MensajeVacio_422(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/203/mensaje-entrante", map[string]string{"mensaje": "   "})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
}

// ─── GET /reactivacion/conversaciones ───────────────────────────────────────

func TestListConversaciones_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouterNoAuth(svc), http.MethodGet, "/reactivacion/conversaciones")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListConversaciones_NoPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouter(svc, userWith()), http.MethodGet, "/reactivacion/conversaciones")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestListConversaciones_HappyPath_OrdenaEscaladasPrimero(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()

	alDia, err := domain.CrearConversacion(301, fixedNow)
	require.NoError(t, err)
	require.NoError(t, alDia.MarcarRespondio(fixedNow))
	require.NoError(t, alDia.MarcarConversando(fixedNow))
	require.NoError(t, convRepo.Upsert(context.Background(), alDia))

	teNecesitan, err := domain.CrearConversacion(302, fixedNow)
	require.NoError(t, err)
	require.NoError(t, teNecesitan.MarcarEscalada("ana", fixedNow))
	require.NoError(t, convRepo.Upsert(context.Background(), teNecesitan))

	convRepo.listResult = []*domain.Conversacion{alDia, teNecesitan}

	svc := buildServiceWithCopiloto(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/conversaciones")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []reactivacionhttp.ConversacionResumenDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2)
	assert.Equal(t, 302, body.Items[0].ClienteID, "escalada must sort first")
	assert.Equal(t, "escalado", body.Items[0].Estado)
	assert.Equal(t, "ana", body.Items[0].AsignadoA)
	assert.Nil(t, body.Items[0].UltimaDecision)
	assert.Equal(t, 301, body.Items[1].ClienteID)
}

func TestListConversaciones_HappyPath_IncluyeBandejaEnrichment(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()

	conv, err := domain.CrearConversacion(303, fixedNow)
	require.NoError(t, err)
	require.NoError(t, convRepo.Upsert(context.Background(), conv))

	turno, err := domain.CrearTurno(domain.CrearTurnoParams{
		ClienteID: 303, Direccion: domain.DireccionEntrante, Autor: domain.AutorCliente,
		Cuerpo: "¿qué tienen de comedores?", Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, convRepo.AppendTurno(context.Background(), turno))

	factsReader := &fakeClienteFactsReader{facts: map[int]*outbound.ClienteFacts{
		303: {Nombre: "María López", Segmento: "recien_liquidado", Telefono: "238 100 4521"},
	}}

	svc := buildServiceWithCopiloto(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, factsReader)
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/conversaciones")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []reactivacionhttp.ConversacionResumenDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "María López", body.Items[0].Nombre)
	assert.Equal(t, "recien_liquidado", body.Items[0].Segmento)
	assert.Equal(t, "¿qué tienen de comedores?", body.Items[0].UltimoMensaje)
}

func TestListConversaciones_SoloEscaladas_IgnoraEstadoConflictivo(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	svc := buildServiceWithCopiloto(convRepo, newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})

	// Sending both solo_escaladas=true AND a conflicting estado must NOT
	// silently AND them down to zero rows (the repo ANDs both filters) — the
	// handler drops estado once solo_escaladas is true.
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/conversaciones?estado=conversando&solo_escaladas=true")
	require.Equal(t, http.StatusOK, rec.Code)

	assert.True(t, convRepo.lastListarParams.SoloEscaladas)
	assert.Empty(t, convRepo.lastListarParams.Estado, "estado must be dropped when solo_escaladas=true")
}

// ─── GET /reactivacion/conversaciones/{cliente_id} ──────────────────────────

func TestObtenerConversacion_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouterNoAuth(svc), http.MethodGet, "/reactivacion/conversaciones/401")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestObtenerConversacion_NoPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouter(svc, userWith()), http.MethodGet, "/reactivacion/conversaciones/401")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestObtenerConversacion_NoEncontrada_404(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/conversaciones/999")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestObtenerConversacion_HappyPath_200(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()

	conv, err := domain.CrearConversacion(402, fixedNow)
	require.NoError(t, err)
	conv.SetContextoNota("cliente prefiere tardes", []string{"prefiere_tarde"}, "hash1", fixedNow)
	require.NoError(t, convRepo.Upsert(context.Background(), conv))

	turno, err := domain.CrearTurno(domain.CrearTurnoParams{
		ClienteID: 402, Direccion: domain.DireccionEntrante, Autor: domain.AutorCliente,
		Cuerpo: "hola", Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, convRepo.AppendTurno(context.Background(), turno))

	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: 402, TurnoRef: turno.ID(), Intencion: "saluda", Confianza: 80,
		Accion: domain.AccionResponder, Borrador: "hola de vuelta", Resultado: domain.ResultadoPropuesto, Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, decisionRepo.Insertar(context.Background(), d))

	factsReader := &fakeClienteFactsReader{facts: map[int]*outbound.ClienteFacts{
		402: {Nombre: "María López", Segmento: "recien_liquidado", Telefono: "238 100 4521"},
	}}
	svc := buildServiceWithCopiloto(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, factsReader)
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/conversaciones/402")
	require.Equal(t, http.StatusOK, rec.Code)

	var dto reactivacionhttp.ConversacionDetalleDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, 402, dto.Conversacion.ClienteID)
	assert.Equal(t, "cliente prefiere tardes", dto.Conversacion.ContextoNota)
	assert.Contains(t, dto.Conversacion.Banderas, "prefiere_tarde")
	assert.Equal(t, "María López", dto.Conversacion.Nombre)
	assert.Equal(t, "recien_liquidado", dto.Conversacion.Segmento)
	assert.Equal(t, "238 100 4521", dto.Conversacion.Telefono)
	require.Len(t, dto.Turnos, 1)
	assert.Equal(t, "entrante", dto.Turnos[0].Direccion)
	assert.Equal(t, "hola", dto.Turnos[0].Cuerpo)
	require.Len(t, dto.Decisiones, 1)
	assert.Equal(t, "saluda", dto.Decisiones[0].Intencion)
	assert.Equal(t, "propuesto", dto.Decisiones[0].Resultado)
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/aprobar ─────────────────

func TestAprobarBorrador_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/conversaciones/501/aprobar", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestAprobarBorrador_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/conversaciones/501/aprobar", nil)
	assert.Equal(t, http.StatusForbidden, code)
}

func TestAprobarBorrador_HappyPath_200(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()

	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: 502, Accion: domain.AccionResponder, Borrador: "hola, tenemos una promo para ti",
		Resultado: domain.ResultadoPropuesto, Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, decisionRepo.Insertar(context.Background(), d))

	factsReader := &fakeClienteFactsReader{facts: map[int]*outbound.ClienteFacts{
		502: {Nombre: "Cliente Prueba", Segmento: "recien_liquidado", Telefono: "238 111 2222"},
	}}
	svc, mensajeRepo := buildServiceWithCopilotoAndMensajeRepo(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, factsReader)

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/502/aprobar", nil)
	require.Equal(t, http.StatusOK, code)

	var dto reactivacionhttp.OkDTO
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.True(t, dto.Ok)

	// Assert the actual persisted effect, not just the ack body: a new
	// aprobado Decision was appended (append-only audit log — the original
	// propuesto stays), and the approved text was queued via the canal.
	decisiones, err := decisionRepo.ListarPorCliente(context.Background(), 502)
	require.NoError(t, err)
	require.Len(t, decisiones, 2, "the propuesto original plus the new aprobado")
	nueva := decisiones[len(decisiones)-1]
	assert.Equal(t, domain.ResultadoAprobado, nueva.Resultado())
	assert.Equal(t, "hola, tenemos una promo para ti", nueva.Borrador())

	require.Len(t, mensajeRepo.insertados, 1, "the approved draft must be queued via the canal")
	assert.Equal(t, 502, mensajeRepo.insertados[0].ClienteID())
	assert.Equal(t, "hola, tenemos una promo para ti", mensajeRepo.insertados[0].Cuerpo())
}

func TestAprobarBorrador_SinBorradorPendiente_422(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/503/aprobar", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, code)
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/editar ──────────────────

func TestEditarBorrador_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/conversaciones/601/editar", map[string]string{"texto": "hola editado"})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestEditarBorrador_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	// reactivacion:leer is NOT enough — this is an operator action.
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/conversaciones/601/editar", map[string]string{"texto": "hola editado"})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestEditarBorrador_HappyPath_200(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()

	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: 602, Accion: domain.AccionResponder, Borrador: "borrador original",
		Resultado: domain.ResultadoPropuesto, Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, decisionRepo.Insertar(context.Background(), d))

	factsReader := &fakeClienteFactsReader{facts: map[int]*outbound.ClienteFacts{
		602: {Nombre: "Cliente Prueba", Segmento: "recien_liquidado", Telefono: "238 111 2222"},
	}}
	svc, mensajeRepo := buildServiceWithCopilotoAndMensajeRepo(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, factsReader)

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/602/editar", map[string]string{"texto": "borrador editado por el operador"})
	require.Equal(t, http.StatusOK, code)

	var dto reactivacionhttp.OkDTO
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.True(t, dto.Ok)

	// Assert the actual persisted effect, not just the ack body: a new
	// editado Decision was appended carrying the OPERATOR'S edited text (not
	// the original draft), and that edited text — not the original — was
	// queued via the canal.
	decisiones, err := decisionRepo.ListarPorCliente(context.Background(), 602)
	require.NoError(t, err)
	require.Len(t, decisiones, 2, "the propuesto original plus the new editado")
	nueva := decisiones[len(decisiones)-1]
	assert.Equal(t, domain.ResultadoEditado, nueva.Resultado())
	assert.Equal(t, "borrador editado por el operador", nueva.Borrador())

	require.Len(t, mensajeRepo.insertados, 1, "the edited draft must be queued via the canal")
	assert.Equal(t, 602, mensajeRepo.insertados[0].ClienteID())
	assert.Equal(t, "borrador editado por el operador", mensajeRepo.insertados[0].Cuerpo())
}

func TestEditarBorrador_TextoVacio_422(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	decisionRepo := newFakeDecisionRepo()
	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: 603, Accion: domain.AccionResponder, Borrador: "borrador original",
		Resultado: domain.ResultadoPropuesto, Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, decisionRepo.Insertar(context.Background(), d))

	svc := buildServiceWithCopiloto(convRepo, decisionRepo, &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/603/editar", map[string]string{"texto": "   "})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/dictar ──────────────────

func TestDictar_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/conversaciones/701/dictar", map[string]string{"intencion": "avisar promo"})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestDictar_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	// reactivacion:leer is NOT enough — this is an operator action.
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/conversaciones/701/dictar", map[string]string{"intencion": "avisar promo"})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestDictar_HappyPath_200(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	conv, err := domain.CrearConversacion(702, fixedNow)
	require.NoError(t, err)
	require.NoError(t, convRepo.Upsert(context.Background(), conv))

	llm := &copilotofake.Generator{RedactarOut: "hola, te comento sobre nuestra promo"}
	svc := buildServiceWithCopiloto(convRepo, newFakeDecisionRepo(), &fakeNotaReader{}, llm, &fakeClienteFactsReader{})

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/702/dictar", map[string]string{"intencion": "avisar promo"})
	require.Equal(t, http.StatusOK, code)

	var dto struct {
		Borrador string `json:"borrador"`
	}
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.Equal(t, "hola, te comento sobre nuestra promo", dto.Borrador)
}

func TestDictar_ConversacionNoEncontrada_404(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/703/dictar", map[string]string{"intencion": "avisar promo"})
	assert.Equal(t, http.StatusNotFound, code)
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/escalar ─────────────────

func TestEscalar_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/conversaciones/801/escalar", map[string]string{"asignado_a": "ana"})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestEscalar_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	// reactivacion:leer is NOT enough — this is an operator action.
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/conversaciones/801/escalar", map[string]string{"asignado_a": "ana"})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestEscalar_HappyPath_200(t *testing.T) {
	t.Parallel()
	convRepo := newFakeConversacionRepo()
	conv, err := domain.CrearConversacion(802, fixedNow)
	require.NoError(t, err)
	require.NoError(t, convRepo.Upsert(context.Background(), conv))

	svc := buildServiceWithCopiloto(convRepo, newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})

	code, body := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/802/escalar", map[string]string{"asignado_a": "ana"})
	require.Equal(t, http.StatusOK, code)

	var dto reactivacionhttp.OkDTO
	require.NoError(t, json.Unmarshal(body, &dto))
	assert.True(t, dto.Ok)

	updated, err := convRepo.Get(context.Background(), 802)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, domain.EstadoEscalado, updated.Estado())
	assert.Equal(t, "ana", updated.AsignadoA())
}

func TestEscalar_ConversacionNoEncontrada_404(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, &copilotofake.Generator{}, &fakeClienteFactsReader{})
	code, _ := doJSON(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/conversaciones/803/escalar", map[string]string{})
	assert.Equal(t, http.StatusNotFound, code)
}
