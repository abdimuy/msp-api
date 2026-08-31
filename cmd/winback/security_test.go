package main

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

// Security sweep for cmd/winback's VPS routes, through the same real
// fx-built router canal_e2e_test.go's composition tests use — never a
// handler called in isolation. Covers: Meta's webhook signature (invalid,
// absent, altered-after-signing) and the internal API's shared token
// (absent, wrong) — and, for every one of those rejections, that the
// response body never leaks AppSecret, VerifyToken or SharedToken.
//
// Self-review (task-11-brief.md step 5, "would this test fail if the thing
// it covers were removed"): for each control below, the corresponding
// production check was temporarily deleted/bypassed, the test re-run to
// confirm it goes red, then the code was restored — see task-11-report.md
// for exactly what was removed and the failing output observed. None of
// that survives in this file; only the confirmation that it was done.

// secretsMustNotAppearIn asserts body contains none of the module's three
// credentials — the assertion the "no leak" requirement is built on. Called
// after every forbidden response in this file.
func secretsMustNotAppearIn(t *testing.T, body string) {
	t.Helper()
	assert.NotContains(t, body, testAppSecret, "response must never echo WhatsApp.AppSecret")
	assert.NotContains(t, body, testVerifyToken, "response must never echo Canal.WebhookVerifyToken")
	assert.NotContains(t, body, testSharedToken, "response must never echo Canal.SharedToken")
}

// ── Meta's webhook: signature ────────────────────────────────────────────

func TestSecurity_WebhookChallenge_WrongVerifyToken_Forbidden_NoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	q := url.Values{
		"hub.mode":         {"subscribe"},
		"hub.verify_token": {"not-" + testVerifyToken},
		"hub.challenge":    {"should-never-be-echoed"},
	}
	req := httptest.NewRequest(http.MethodGet, webhookPath+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), "should-never-be-echoed",
		"a wrong verify_token must never get the challenge echoed back")
	secretsMustNotAppearIn(t, rec.Body.String())
}

func TestSecurity_WebhookPost_InvalidSignature_Forbidden_NotPersisted_NoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	body := metaTextPayload("wamid.sec-invalid-sig", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody("wrong-secret-entirely", body))
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	_, _, found := rowState(t, comp.dbPath, "wamid.sec-invalid-sig")
	assert.False(t, found, "an invalid signature must never reach persistence")
	secretsMustNotAppearIn(t, rec.Body.String())
}

func TestSecurity_WebhookPost_MissingSignatureHeader_Forbidden_NotPersisted_NoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	body := metaTextPayload("wamid.sec-no-sig", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	// Deliberately no X-Hub-Signature-256 header.
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	_, _, found := rowState(t, comp.dbPath, "wamid.sec-no-sig")
	assert.False(t, found, "a missing signature must never reach persistence")
	secretsMustNotAppearIn(t, rec.Body.String())
}

// TestSecurity_WebhookPost_BodyAlteredAfterSigning_Forbidden_NotPersisted is
// the test global-constraints.md #3 calls for explicitly: it signs one body
// and sends a DIFFERENT one under that signature — exactly what a
// middleware that decodes-and-re-encodes the request would produce by
// accident. Run through the real chain (RequestID, Recovery, AccessLog,
// then canalhttp.MountRouter), this is what keeps that constraint enforced
// as the middleware stack grows.
func TestSecurity_WebhookPost_BodyAlteredAfterSigning_Forbidden_NotPersisted(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	original := metaTextPayload("wamid.sec-original", "5215511112222", "1234567890", "hola", time.Now())
	sig := signBody(testAppSecret, original)

	altered := metaTextPayload("wamid.sec-altered", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(altered))
	req.Header.Set(signatureHeaderName, sig)
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	_, _, foundAltered := rowState(t, comp.dbPath, "wamid.sec-altered")
	_, _, foundOriginal := rowState(t, comp.dbPath, "wamid.sec-original")
	assert.False(t, foundAltered, "the altered body must never reach persistence")
	assert.False(t, foundOriginal, "the never-sent original body must not appear either")
	secretsMustNotAppearIn(t, rec.Body.String())
}

// ── internal API: shared token ───────────────────────────────────────────

func TestSecurity_Salientes_MissingToken_Forbidden_NoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	payload := []byte(`{"destinatario":"5215500000000","tipo":"texto","texto":"hola"}`)
	req := httptest.NewRequest(http.MethodPost, "/canal/v1/salientes", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no X-Canal-Token header.
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	secretsMustNotAppearIn(t, rec.Body.String())
}

func TestSecurity_Salientes_WrongToken_Forbidden_NoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	payload := []byte(`{"destinatario":"5215500000000","tipo":"texto","texto":"hola"}`)
	req := httptest.NewRequest(http.MethodPost, "/canal/v1/salientes", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Canal-Token", "not-"+testSharedToken)
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	secretsMustNotAppearIn(t, rec.Body.String())
}

// TestSecurity_Salud_NoTokenRequired_StillNoLeak documents the deliberate
// exception (registerSalud's doc comment): GET /canal/v1/salud carries no
// authentication at all, by design. This test proves that choice does not
// also leak a secret through the one field the handler does return
// (Pendientes) or anywhere else in the body.
func TestSecurity_Salud_NoTokenRequired_StillNoLeak(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	req := httptest.NewRequest(http.MethodGet, saludPath, nil)
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	secretsMustNotAppearIn(t, rec.Body.String())
}
