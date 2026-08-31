//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm/copilotofake"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

const (
	tiendaPath      = "/reactivacion/tienda/mensaje-entrante"
	tiendaTokenHdr  = "X-Canal-Token"
	tiendaTestToken = "test-shared-token"
)

// cohorteRowTelefono builds a domain.CohorteCliente fixture for clienteID
// with an explicit telefono — unlike cohorteRow (hardcoded "238 111 2222"),
// this package's tienda tests need distinct/matching phone numbers per row.
func cohorteRowTelefono(clienteID int, telefono string) *domain.CohorteCliente {
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    uuid.New(),
		ClienteID:             clienteID,
		Nombre:                "Cliente Cohorte",
		Telefono:              telefono,
		Segmento:              domain.SegmentoRecienLiquidado,
		CohorteFecha:          fixedNow,
		FechaUltimaCompraBase: fixedNow.AddDate(0, -2, 0),
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		CreatedAt:             fixedNow,
		UpdatedAt:             fixedNow,
	})
}

// buildTiendaServiceWithCohorte wires a copiloto-capable Service (mirroring
// buildServiceWithCopiloto in copiloto_test.go) whose CohorteRepo serves
// cohorte — the fixtures ResolverClienteIDPorTelefono matches against.
func buildTiendaServiceWithCohorte(cohorte []*domain.CohorteCliente, llm outbound.CopilotoLLM) *reactivacionapp.Service {
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{list: cohorte}, &fakeMensajeRepo{}, fakeSender{}, true)
	return svc.WithCopiloto(newFakeConversacionRepo(), newFakeDecisionRepo(), &fakeNotaReader{}, llm, &fakeClienteFactsReader{})
}

// buildTiendaRouter mounts ONLY MountTiendaRouter — no chi middleware at
// all, matching cmd/api/server.go's deliberately Firebase-free Group.
func buildTiendaRouter(svc *reactivacionapp.Service, cfg reactivacionhttp.TiendaConfig) http.Handler {
	r := chi.NewRouter()
	reactivacionhttp.MountTiendaRouter(r, svc, cfg)
	return r
}

func tiendaBody(remitente, tipo, contenido string) map[string]string {
	return map[string]string{
		"wamid":           "wamid.test-1",
		"remitente":       remitente,
		"phone_number_id": "1234567890",
		"tipo":            tipo,
		"contenido":       contenido,
		"timestamp_meta":  "2026-08-31T12:00:00Z",
		"recibido_en":     "2026-08-31T12:00:01Z",
	}
}

func postTienda(h http.Handler, token string, body map[string]string) *httptest.ResponseRecorder {
	buf, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, tiendaPath, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(tiendaTokenHdr, token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ─── auth ────────────────────────────────────────────────────────────────

func TestTiendaMensajeEntrante_MissingToken_Forbidden(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte(nil, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, "", tiendaBody("522381234567", "text", "hola"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), tiendaTestToken)
}

func TestTiendaRouter_DocsAndOpenAPI_Disabled(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte(nil, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	// tiendaTokenMiddleware is the only gate on this router — see
	// buildTiendaRouter's doc comment — so Docs/OpenAPI must not be
	// registered at all rather than merely be behind the token: a 404 here
	// (not a 403) proves humachi never mounted the route.
	for _, path := range []string{"/docs", "/openapi.json", "/openapi.yaml"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code, "path %q must not be registered on the tienda router", path)
	}
}

func TestTiendaMensajeEntrante_WrongToken_Forbidden(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte(nil, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, "not-the-configured-token", tiendaBody("522381234567", "text", "hola"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTiendaMensajeEntrante_WrongToken_MalformedBody_TokenCheckedFirst(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte(nil, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	req := httptest.NewRequest(http.MethodPost, tiendaPath, bytes.NewReader([]byte("{not-valid-json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTiendaMensajeEntrante_UnconfiguredToken_AlwaysForbidden(t *testing.T) {
	t.Parallel()
	// SharedToken == "" (unset in config) must fail closed — never accept
	// every request just because nothing was configured.
	svc := buildTiendaServiceWithCohorte(nil, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: ""})

	rec := postTienda(h, "", tiendaBody("522381234567", "text", "hola"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── IP allowlist (optional, off by default) ───────────────────────────────

func TestTiendaMensajeEntrante_IPAllowlist_Unconfigured_DoesNotBlock(t *testing.T) {
	t.Parallel()
	// httptest.NewRequest's default RemoteAddr is 192.0.2.1:1234 — this
	// test never lists it anywhere, proving the empty-allowlist default
	// does not filter by IP at all.
	llm := &copilotofake.Generator{AnalizarOut: outbound.AnalizarOutput{
		Confianza: 95, Accion: "responder", Borrador: "hola de vuelta",
	}}
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(301, "238 123 4567")}, llm)
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "hola"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestTiendaMensajeEntrante_IPAllowlist_Configured_RejectsOtherIPs(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(301, "238 123 4567")}, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{
		SharedToken: tiendaTestToken,
		AllowedIPs:  []string{"203.0.113.9"}, // NOT httptest.NewRequest's default 192.0.2.1
	})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "hola"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTiendaMensajeEntrante_IPAllowlist_Configured_AllowsListedIP(t *testing.T) {
	t.Parallel()
	llm := &copilotofake.Generator{AnalizarOut: outbound.AnalizarOutput{
		Confianza: 95, Accion: "responder", Borrador: "hola de vuelta",
	}}
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(301, "238 123 4567")}, llm)
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{
		SharedToken: tiendaTestToken,
		AllowedIPs:  []string{"192.0.2.1"}, // matches httptest.NewRequest's default RemoteAddr
	})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "hola"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// ─── payload mapping / phone resolution ────────────────────────────────────

func TestTiendaMensajeEntrante_ResolvedPhone_ProcesaYDevuelveDecision(t *testing.T) {
	t.Parallel()
	llm := &copilotofake.Generator{AnalizarOut: outbound.AnalizarOutput{
		Intencion: "pregunta por horario",
		Confianza: 90,
		Senales:   []string{},
		Accion:    "responder",
		Borrador:  "hola, con gusto te comparto nuestro horario",
	}}
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(401, "238 123 4567")}, llm)
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "hola, cual es su horario"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto reactivacionhttp.DecisionResultDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.False(t, dto.Escalada)
	assert.Equal(t, "responder", dto.Accion)
	assert.Equal(t, "hola, con gusto te comparto nuestro horario", dto.Borrador)
}

func TestTiendaMensajeEntrante_UnknownPhone_NotFound_NotSwallowed(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(401, "238 123 4567")}, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	// A phone that matches no cohorte row.
	rec := postTienda(h, tiendaTestToken, tiendaBody("525599998888", "text", "hola"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), tiendaTestToken)
}

func TestTiendaMensajeEntrante_AmbiguousPhone_Conflict(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{
		cohorteRowTelefono(401, "238 123 4567"),
		cohorteRowTelefono(402, "238 123 4567"), // shared household landline
	}, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "hola"))

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestTiendaMensajeEntrante_TextButBlankContenido_ProcesarMensajeEntranteError(t *testing.T) {
	t.Parallel()
	// Phone resolves fine; the failure is ProcesarMensajeEntrante's OWN
	// empty-message guard (ErrMensajeEntranteVacio) — proves the handler
	// propagates that error too, not just the resolver's.
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(401, "238 123 4567")}, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "text", "   "))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTiendaMensajeEntrante_NonTextType_UnprocessableEntity(t *testing.T) {
	t.Parallel()
	svc := buildTiendaServiceWithCohorte([]*domain.CohorteCliente{cohorteRowTelefono(401, "238 123 4567")}, &copilotofake.Generator{})
	h := buildTiendaRouter(svc, reactivacionhttp.TiendaConfig{SharedToken: tiendaTestToken})

	// image type: Contenido would carry a media id, never the cliente's words.
	rec := postTienda(h, tiendaTestToken, tiendaBody("522381234567", "image", "MEDIA-ID-123"))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
