package canalhttp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const webhookPath = "/canal/v1/webhook/whatsapp"

// ── GET challenge ────────────────────────────────────────────────────────

func TestWebhookChallenge_ModeAndTokenMatch_EchoesChallenge(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	q := url.Values{
		"hub.mode":         {"subscribe"},
		"hub.verify_token": {testVerifyToken},
		"hub.challenge":    {"challenge-12345"},
	}
	req := httptest.NewRequest(http.MethodGet, webhookPath+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "challenge-12345", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
}

func TestWebhookChallenge_WrongVerifyToken_Forbidden(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	q := url.Values{
		"hub.mode":         {"subscribe"},
		"hub.verify_token": {"not-the-configured-token"},
		"hub.challenge":    {"challenge-12345"},
	}
	req := httptest.NewRequest(http.MethodGet, webhookPath+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// A test that only checked the status would pass even if the handler
	// echoed the challenge back on every request — assert the body never
	// leaks it, since that is the whole point of rejecting the mismatch.
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), "challenge-12345")
}

func TestWebhookChallenge_WrongMode_Forbidden(t *testing.T) {
	t.Parallel()
	r, _ := newTestRouter(t)

	q := url.Values{
		"hub.mode":         {"unsubscribe"},
		"hub.verify_token": {testVerifyToken},
		"hub.challenge":    {"challenge-12345"},
	}
	req := httptest.NewRequest(http.MethodGet, webhookPath+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

// ── POST receive ─────────────────────────────────────────────────────────

func TestWebhookReceive_ValidSignature_PersistsAndReturns200(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := metaTextPayload("wamid.abc123", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, deps.repo.count())

	m, ok := deps.repo.mensajePorWamid("wamid.abc123")
	require.True(t, ok)
	assert.Equal(t, "5215511112222", m.Remitente())
	assert.Equal(t, "1234567890", m.PhoneNumberID())
	assert.Equal(t, "hola", m.Contenido())
}

func TestWebhookReceive_InvalidSignature_Forbidden_NotPersisted(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := metaTextPayload("wamid.bad-sig", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody("wrong-secret", body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, deps.repo.count())
	assert.Equal(t, 0, deps.repo.llamadasAGuardar(), "an invalid signature must never reach the service")
}

func TestWebhookReceive_MissingSignatureHeader_Forbidden_NotPersisted(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := metaTextPayload("wamid.no-sig", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	// Deliberately no X-Hub-Signature-256 header.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, deps.repo.count())
	assert.Equal(t, 0, deps.repo.llamadasAGuardar())
}

func TestWebhookReceive_BodyAlteredAfterSigning_Forbidden_NotPersisted(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	original := metaTextPayload("wamid.original", "5215511112222", "1234567890", "hola", time.Now())
	sig := signBody(testAppSecret, original)

	// The attacker (or a re-encoding proxy) changes the body after the
	// signature was computed over `original` — the request now carries a
	// different wamid than what was signed.
	altered := metaTextPayload("wamid.altered", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(altered))
	req.Header.Set(signatureHeaderName, sig)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, deps.repo.count())
	_, ok := deps.repo.mensajePorWamid("wamid.altered")
	assert.False(t, ok, "the altered body must never reach persistence")
}

func TestWebhookReceive_SameWamidTwice_BothReturn200_NoDuplicate(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := metaTextPayload("wamid.redelivered", "5215511112222", "1234567890", "hola", time.Now())
	sig := signBody(testAppSecret, body)

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
		req.Header.Set(signatureHeaderName, sig)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "delivery %d", i+1)
	}

	assert.Equal(t, 2, deps.repo.llamadasAGuardar(), "the service must be called on the redelivery too")
	assert.Equal(t, 1, deps.repo.count(), "a redelivered wamid must not create a second row")
}

func TestWebhookReceive_UnknownMessageType_ContenidoEmpty_StillPersists(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := []byte(`{
		"entry": [{"changes": [{"value": {
			"metadata": {"phone_number_id": "1234567890"},
			"messages": [{"from": "5215511112222", "id": "wamid.location", "timestamp": "1700000000", "type": "location"}]
		}}]}]
	}`)
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	m, ok := deps.repo.mensajePorWamid("wamid.location")
	require.True(t, ok)
	assert.Empty(t, m.Contenido())
	assert.Equal(t, "location", m.Tipo())
}

func TestWebhookReceive_PersistenceFails_ReturnsNon2xx_NotSwallowed(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)
	deps.repo.guardarFalla = errRepoBoom

	body := metaTextPayload("wamid.persist-fails", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// A signature-valid body whose message content is fine must NOT be
	// answered 200 when persistence itself failed: 200 here would tell
	// Meta the delivery is done, and no mailbox row exists to prove
	// otherwise — GET /canal/v1/salud would report pendientes: 0 and look
	// healthy while the reply was silently destroyed. Redelivery is safe
	// (Guardar is ON CONFLICT (wamid) DO NOTHING), so Meta retrying is the
	// correct outcome here, not a bug to route around.
	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a storage failure must not be answered 200 — Meta must redeliver")
	_, ok := deps.repo.mensajePorWamid("wamid.persist-fails")
	assert.False(t, ok, "the message was never actually persisted")
}

func TestWebhookReceive_ValidationFails_Returns200_NotPersisted(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	// A message with no "from" fails domain.NewMensajeEntrante's Remitente
	// requirement — a genuinely permanent, validation-class failure. Meta
	// must never be told to retry this: retrying an invalid payload cannot
	// make it valid.
	body := []byte(`{
		"entry": [{"changes": [{"value": {
			"metadata": {"phone_number_id": "1234567890"},
			"messages": [{"id": "wamid.no-remitente", "timestamp": "1700000000", "type": "text", "text": {"body": "hola"}}]
		}}]}]
	}`)
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "a validation-class failure keeps the old 200: retrying it cannot help")
	_, ok := deps.repo.mensajePorWamid("wamid.no-remitente")
	assert.False(t, ok, "an invalid message must never be persisted")
}

func TestWebhookReceive_MalformedJSON_ValidSignature_Returns200_NotPersisted(t *testing.T) {
	t.Parallel()
	r, deps := newTestRouter(t)

	body := []byte(`{not-valid-json`)
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// A valid signature always gets 200, per the package doc on
	// handleReceive — even when the body inside turns out to be garbage.
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 0, deps.repo.count())
}

// signatureHeaderName is X-Hub-Signature-256, spelled out here (rather than
// importing the unexported constant) since these are black-box tests
// exercising only canalhttp's public surface.
const signatureHeaderName = "X-Hub-Signature-256"
