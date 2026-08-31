package canalhttp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalhttp"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

const (
	salientesPath  = "/canal/v1/salientes"
	saludPath      = "/canal/v1/salud"
	sharedTokenHdr = "X-Canal-Token"
)

// fixedNow is the timestamp used wherever a test needs "some valid instant"
// rather than a specific one.
var fixedNow = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

// ── GET /canal/v1/salud ──────────────────────────────────────────────────

func TestSalud_NoTokenRequired_ReportsPendientes(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	// Put one entrante in the mailbox so Pendientes is provably non-zero,
	// not just a zero-value the handler never actually read.
	body := metaTextPayload("wamid.pending", "5215511112222", "1234567890", "hola", fixedNow)
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	r.ServeHTTP(httptest.NewRecorder(), req)
	require.Equal(t, 1, deps.repo.count())

	req = httptest.NewRequest(http.MethodGet, saludPath, nil)
	// Deliberately no shared-token header — salud must answer anyway.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Estado     string `json:"estado"`
		Pendientes int    `json:"pendientes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "ok", out.Estado)
	assert.Equal(t, 1, out.Pendientes)
	assert.NotContains(t, rec.Body.String(), testSharedToken)
	assert.NotContains(t, rec.Body.String(), testAppSecret)
}

// ── POST /canal/v1/salientes ─────────────────────────────────────────────

func TestSalientes_MissingToken_Forbidden(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no X-Canal-Token header.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSalientes_MissingToken_MalformedBody_TokenCheckedFirst(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	// An unauthenticated caller must never learn anything about body
	// validity — sharedTokenMiddleware must reject before Huma ever
	// attempts to parse/validate the body.
	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader([]byte("{not-valid-json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSalientes_WrongToken_Forbidden(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, "not-the-configured-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSalientes_ValidToken_SendsTextAndReturnsWamid(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{wamid: "wamid.sent-1"}}

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola cliente")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Wamid string `json:"wamid"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "wamid.sent-1", out.Wamid)
	assert.Equal(t, 1, deps.wa.sendTextCallCount())
}

func TestSalientes_InvalidTipo_UnprocessableEntity(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	payload := []byte(`{"destinatario":"5215500000000","tipo":"fax"}`)
	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSalientes_WindowClosed_MapsToConflict(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{err: whatsapp.ErrWindowClosed}}

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSalientes_RateLimited_MapsToTooManyRequests(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{err: whatsapp.ErrRateLimited}}

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestSalud_RepoError_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.repo.contarPendientesFalla = errRepoBoom

	req := httptest.NewRequest(http.MethodGet, saludPath, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestSalientes_MissingDestinatario_UnprocessableEntity(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	payload := []byte(`{"tipo":"texto","texto":"hola"}`)
	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSalientes_TipoTextoMissingTexto_UnprocessableEntity(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	payload := []byte(`{"destinatario":"5215500000000","tipo":"texto"}`)
	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSalientes_TipoPlantillaMissingPlantilla_UnprocessableEntity(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	payload := []byte(`{"destinatario":"5215500000000","tipo":"plantilla"}`)
	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSalientes_InvalidNumber_MapsToUnprocessableEntity(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{err: whatsapp.ErrInvalidNumber}}

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSalientes_Disabled_MapsToServiceUnavailable(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{err: whatsapp.ErrWhatsAppDisabled}}

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(salienteTextoBody("hola")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSalientes_TemplateType_SendsTemplate(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.wa.sendTextOutcomes = []waOutcome{{wamid: "wamid.tmpl-1"}}

	payload := map[string]any{
		"destinatario": "5215500000000",
		"tipo":         "plantilla",
		"plantilla": map[string]any{
			"nombre":     "recordatorio_pago",
			"idioma":     "es_MX",
			"parametros": []string{"Juan", "500"},
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, salientesPath, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHdr, testSharedToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// ── OpenAPI surface ───────────────────────────────────────────────────────

// TestOpenAPI_PathsRegistered follows the repo's own convention (see
// docs/module-standards/08-handlers-routes.md) of asserting every expected
// OperationID is present in the generated spec, and that the raw chi
// webhook route (never registered with Huma) does not leak into it.
func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	clock := newFixedClock(fixedNow)
	wa := newWAFake()
	svc := canalapp.NewService(repo, forwarderFake{}, clock, nil, canalapp.ReenvioConfig{}, nil)

	r := chi.NewRouter()
	api := canalhttp.MountRouter(r, canalhttp.Deps{
		Svc:        svc,
		Clock:      clock,
		Pendientes: repo,
		WA:         wa,
		Cfg: canalhttp.Config{
			AppSecret:   testAppSecret,
			VerifyToken: testVerifyToken,
			SharedToken: testSharedToken,
		},
	})

	spec := api.OpenAPI()
	registered := make(map[string]bool)
	for _, item := range spec.Paths {
		if item.Get != nil {
			registered[item.Get.OperationID] = true
		}
		if item.Post != nil {
			registered[item.Post.OperationID] = true
		}
	}
	for _, want := range []string{"canal-salud", "canal-crear-saliente"} {
		assert.True(t, registered[want], "operación no registrada: %s", want)
	}
	_, hasWebhook := spec.Paths[webhookPath]
	assert.False(t, hasWebhook, "the raw chi webhook must not appear in the Huma spec")
}

// salienteTextoBody builds a valid CrearSalienteBody JSON payload for a
// tipo=texto message.
func salienteTextoBody(texto string) []byte {
	return []byte(fmt.Sprintf(`{"destinatario":"5215500000000","tipo":"texto","texto":%q}`, texto))
}
