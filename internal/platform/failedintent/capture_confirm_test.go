package failedintent_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ---------------------------------------------------------------------------
// X-Intent-Captured — custody confirmation header
//
// The phone releases a capture on a 4xx/5xx only when the server proves it
// holds the intent for later correction. That proof is this header, and it is
// emitted ONLY when Store.Save returned nil. See
// docs/superpowers/specs/2026-08-15-entrega-garantizada-capturas-design.md §4.1.
// ---------------------------------------------------------------------------

func TestCaptureMiddleware_EmitsIntentCapturedHeaderOn422(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"precio":0}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "the intent must have been saved")

	got := rw.Header().Get(failedintent.HeaderIntentCaptured)
	require.NotEmpty(t, got, "a saved intent must be confirmed with X-Intent-Captured")

	parsed, err := uuid.Parse(got)
	require.NoError(t, err, "the header must carry the intent UUID")
	assert.Equal(t, store.first().ID, parsed, "header must name the intent that was saved")

	// Deferring the flush must not corrupt what the client receives.
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code, "status must survive the deferred flush")
	assert.JSONEq(t, problemBody422, rw.Body.String(), "body must survive the deferred flush")
	assert.Equal(
		t, "application/problem+json; charset=utf-8", rw.Header().Get("Content-Type"),
		"handler-set headers must survive the deferred flush",
	)
}

func TestCaptureMiddleware_OmitsIntentCapturedHeaderWhenStoreFails(t *testing.T) {
	t.Parallel()

	// The pool-wedged case: the request fails AND the capture fails. Claiming
	// custody here is exactly the bug that loses payments.
	store := &fakeStore{saveErr: errors.New("firebird pool wedged")}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"precio":0}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	assert.Empty(
		t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"a failed Save must NOT claim custody",
	)
	// The response itself is unaffected — capture is best-effort.
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	assert.JSONEq(t, problemBody422, rw.Body.String())
}

func TestCaptureMiddleware_NoIntentCapturedHeaderOn2xx(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler200()).ServeHTTP(rw, req)

	assert.Empty(t, rw.Header().Get(failedintent.HeaderIntentCaptured), "2xx captures nothing")
	assert.Equal(t, http.StatusOK, rw.Code)
	assert.JSONEq(t, `{"ok":true}`, rw.Body.String())
}

func TestCaptureMiddleware_NoIntentCapturedHeaderOnIdempotentReplay(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	replay := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(failedintent.HeaderIdempotentReplay, "true")
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"precio":0}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(replay).ServeHTTP(rw, req)

	assert.Equal(t, 0, store.count(), "a replay must not re-capture")
	assert.Empty(
		t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"a replay saved nothing now, so it must not claim custody now",
	)
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	assert.JSONEq(t, problemBody422, rw.Body.String())
}

// A 500 with no body at all still has to confirm custody: the phone's decision
// table reads only the header.
func TestCaptureMiddleware_EmitsIntentCapturedHeaderOn500WithoutBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	bare500 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"precio":0}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(bare500).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	assert.NotEmpty(t, rw.Header().Get(failedintent.HeaderIntentCaptured))
	assert.Equal(t, http.StatusInternalServerError, rw.Code)
}

// The multipart path shares saveIntent but has its own flush point, so it gets
// its own assertion rather than trusting the JSON path to cover it.
func TestCaptureMiddleware_EmitsIntentCapturedHeaderOnMultipart(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blob := newFakeBlobStore()
	cfg := newTestConfig(store, 1024)
	cfg.Blob = blob
	mw := failedintent.CaptureMiddleware(cfg)

	req := buildMultipartRequest(t, []byte("fake-image-bytes"))
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "multipart 422 must be captured")

	got := rw.Header().Get(failedintent.HeaderIntentCaptured)
	require.NotEmpty(t, got, "multipart capture must confirm custody too")
	parsed, err := uuid.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, store.first().ID, parsed)
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code)
}

func TestCaptureMiddleware_OmitsIntentCapturedHeaderOnMultipartStoreFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{saveErr: errors.New("firebird pool wedged")}
	blob := newFakeBlobStore()
	cfg := newTestConfig(store, 1024)
	cfg.Blob = blob
	mw := failedintent.CaptureMiddleware(cfg)

	req := buildMultipartRequest(t, []byte("fake-image-bytes"))
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	assert.Empty(
		t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"a failed multipart Save must NOT claim custody",
	)
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code)
}

// Requests the middleware does not capture must pass through untouched — no
// header, and no buffering side effects.
func TestCaptureMiddleware_NoIntentCapturedHeaderWhenNotEligible(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	req := httptest.NewRequest(http.MethodPost, "/healthz", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	assert.Equal(t, 0, store.count())
	assert.Empty(t, rw.Header().Get(failedintent.HeaderIntentCaptured))
	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	assert.JSONEq(t, problemBody422, rw.Body.String())
}
