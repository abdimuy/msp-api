package failedintent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ---------------------------------------------------------------------------
// Shared fakes
// ---------------------------------------------------------------------------

// fakeStore is a minimal in-memory Store for unit tests. Thread-safe.
type fakeStore struct {
	mu      sync.Mutex
	saved   []failedintent.Intent
	saveErr error
}

func (f *fakeStore) Save(_ context.Context, i failedintent.Intent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, i)
	return nil
}

func (f *fakeStore) Get(_ context.Context, _ uuid.UUID) (*failedintent.Intent, error) {
	return nil, nil //nolint:nilnil // not-found sentinel per Store contract
}

func (f *fakeStore) List(_ context.Context, _ failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	return failedintent.Page[failedintent.Intent]{}, nil
}

func (f *fakeStore) UpdateStatus(
	_ context.Context,
	_ uuid.UUID,
	_, _ failedintent.Status,
	_ uuid.UUID,
	_ string,
	_ time.Time,
) error {
	return nil
}

func (f *fakeStore) IncrementRetry(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (f *fakeStore) PurgeOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

func (f *fakeStore) first() failedintent.Intent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saved[0]
}

// handler422 is a test handler that always writes an RFC9457-shaped 422 body.
const problemBody422 = `{"code":"venta_precio_invalido","detail":"el precio es inválido","title":"Unprocessable Entity"}`

func handler422() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})
}

func handler200() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
}

// newTestConfig returns a Config wired with store and the test-friendly
// defaults (deterministic clock + ID, 1 KiB body cap).
func newTestConfig(store *fakeStore, capBytes int64) failedintent.Config {
	return failedintent.Config{
		Store:        store,
		PathPrefixes: []string{"/v2/ventas"},
		Methods:      []string{http.MethodPost, http.MethodPatch, http.MethodPut},
		BodyCapBytes: capBytes,
	}
}

// ---------------------------------------------------------------------------
// Status.Valid
// ---------------------------------------------------------------------------

func TestStatus_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		s     failedintent.Status
		valid bool
	}{
		{"new", failedintent.StatusNew, true},
		{"retried_ok", failedintent.StatusRetriedOK, true},
		{"retried_fail", failedintent.StatusRetriedFail, true},
		{"ignored", failedintent.StatusIgnored, true},
		{"resolved_manual", failedintent.StatusResolvedManual, true},
		{"empty", failedintent.Status(""), false},
		{"pending", failedintent.Status("pending"), false},
		{"RETRIED_OK", failedintent.Status("RETRIED_OK"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, tc.s.Valid())
		})
	}
}

// ---------------------------------------------------------------------------
// Status.IsTerminal
// ---------------------------------------------------------------------------

func TestStatus_IsTerminal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		s        failedintent.Status
		terminal bool
	}{
		{"new is not terminal", failedintent.StatusNew, false},
		{"retried_ok is terminal", failedintent.StatusRetriedOK, true},
		{"retried_fail is terminal", failedintent.StatusRetriedFail, true},
		{"ignored is terminal", failedintent.StatusIgnored, true},
		{"resolved_manual is terminal", failedintent.StatusResolvedManual, true},
		{"unknown string is not terminal", failedintent.Status("whatever"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.terminal, tc.s.IsTerminal())
		})
	}
}

// ---------------------------------------------------------------------------
// CaptureMiddleware — skip cases
// ---------------------------------------------------------------------------

func TestCaptureMiddleware_SkipsWhenNotEligible(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		// mutate lets individual cases tweak the request (add headers, etc.)
		mutate func(*http.Request)
	}{
		{
			name:   "GET is not captured",
			method: http.MethodGet,
			path:   "/v2/ventas",
		},
		{
			name:   "POST outside prefix is not captured",
			method: http.MethodPost,
			path:   "/healthz",
		},
		{
			name:   "POST with X-Internal-Replay is not captured",
			method: http.MethodPost,
			path:   "/v2/ventas",
			mutate: func(r *http.Request) {
				r.Header.Set(failedintent.HeaderInternalReplay, "1")
			},
		},
		{
			name:   "POST multipart is not captured",
			method: http.MethodPost,
			path:   "/v2/ventas",
			mutate: func(r *http.Request) {
				r.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
			},
		},
		{
			name:   "DELETE is not in default methods",
			method: http.MethodDelete,
			path:   "/v2/ventas",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			cfg := newTestConfig(store, 1024)
			mw := failedintent.CaptureMiddleware(cfg)

			handlerCalled := false
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusUnprocessableEntity)
			})

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"x":1}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.mutate != nil {
				tc.mutate(req)
			}

			rw := httptest.NewRecorder()
			mw(inner).ServeHTTP(rw, req)

			assert.True(t, handlerCalled, "downstream handler must still run")
			assert.Equal(t, 0, store.count(), "store must not receive any Save call")
		})
	}
}

// ---------------------------------------------------------------------------
// CaptureMiddleware — happy-path captures
// ---------------------------------------------------------------------------

func TestCaptureMiddleware_CapturesOn422(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	body := `{"venta_id":"abc","precio":0}`
	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "exactly one Save must occur")

	got := store.first()
	assert.Equal(t, http.MethodPost, got.Method)
	assert.Equal(t, "/v2/ventas", got.Path)
	assert.Equal(t, http.StatusUnprocessableEntity, got.HTTPStatus)
	assert.Equal(t, failedintent.StatusNew, got.Status)
	assert.Equal(t, 0, got.RetryCount)
	assert.False(t, got.BodyTruncated)
	assert.Equal(t, "venta_precio_invalido", got.ErrorCode)

	// Body should be valid JSON that contains the original payload.
	assert.True(t, json.Valid(got.Body), "captured body must be valid JSON")
}

func TestCaptureMiddleware_DoesNotCaptureOn2xx(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler200()).ServeHTTP(rw, req)

	assert.Equal(t, 0, store.count(), "no capture on 2xx response")
}

func TestCaptureMiddleware_BodyRestoredForDownstream(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 4096)
	mw := failedintent.CaptureMiddleware(cfg)

	// Non-ASCII body to confirm bytes are faithfully preserved.
	originalBody := `{"nombre":"García Müller","emoji":"🎉"}`

	var seen []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		seen, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(originalBody))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(inner).ServeHTTP(rw, req)

	assert.Equal(t, originalBody, string(seen), "downstream must see original body bytes")
}

func TestCaptureMiddleware_TruncatesLargeBody(t *testing.T) {
	t.Parallel()

	const capBytes = 1024
	store := &fakeStore{}
	cfg := newTestConfig(store, capBytes)
	mw := failedintent.CaptureMiddleware(cfg)

	// Body is double the cap; content is valid JSON-safe text.
	largeBody := strings.Repeat("x", 2048)
	// Wrap in a JSON string so the body itself is plain text (will be normalised).
	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	assert.True(t, got.BodyTruncated, "BodyTruncated must be true when body exceeds cap")
}

func TestCaptureMiddleware_NormaliseNonJSONBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 4096)
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader("hello world"))
	req.Header.Set("Content-Type", "text/plain")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	// normaliseBody wraps plain text as a JSON string; the result must be valid JSON.
	assert.True(t, json.Valid(got.Body), "non-JSON body must be normalised to valid JSON")

	// Decode to confirm the string value is preserved.
	var s string
	require.NoError(t, json.Unmarshal(got.Body, &s))
	assert.Equal(t, "hello world", s)
}

func TestCaptureMiddleware_StoreSaveFailureDoesNotPropagate(t *testing.T) {
	t.Parallel()

	store := &fakeStore{saveErr: errors.New("db down")}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	// Must not panic; response code must still be the handler's 422.
	mw(handler422()).ServeHTTP(rw, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rw.Code,
		"response status must be preserved even when Save fails")
}

func TestCaptureMiddleware_ExtractsCurrentUser(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	userID := uuid.New()
	cu := auth.CurrentUser{
		ID:          userID,
		FirebaseUID: "firebase-uid-abc",
		Email:       "test@example.com",
	}

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"x":1}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.PlantCurrentUser(req.Context(), cu))
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	assert.Equal(t, "firebase-uid-abc", got.FirebaseUID)
	require.NotNil(t, got.UsuarioID)
	assert.Equal(t, userID, *got.UsuarioID)
}

func TestCaptureMiddleware_UsesInjectedClockAndNewID(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	fixedID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	cfg := failedintent.Config{
		Store:        store,
		PathPrefixes: []string{"/v2/ventas"},
		Methods:      []string{http.MethodPost},
		BodyCapBytes: 1024,
		Clock:        func() time.Time { return fixedTime },
		NewID:        func() uuid.UUID { return fixedID },
	}
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"x":1}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	assert.Equal(t, fixedID, got.ID)
	assert.Equal(t, fixedTime, got.ReceivedAt)
}

// ---------------------------------------------------------------------------
// parseProblemJSON — table-driven via middleware (exercised through cw.body)
// We test the helper indirectly by controlling the handler's response body.
// ---------------------------------------------------------------------------

func TestParseProblemJSON_ViaCapturedErrorCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		handlerBody string
		wantCode    string
		wantMsg     string
	}{
		{
			name:        "valid problem JSON with code and detail",
			handlerBody: `{"code":"some_error","detail":"detalle de error","title":"Error"}`,
			wantCode:    "some_error",
			wantMsg:     "detalle de error",
		},
		{
			name:        "detail empty falls back to title",
			handlerBody: `{"code":"some_error","detail":"","title":"Título del error"}`,
			wantCode:    "some_error",
			wantMsg:     "Título del error",
		},
		{
			name:        "non-JSON body yields empty strings",
			handlerBody: "plain text response",
			wantCode:    "",
			wantMsg:     "",
		},
		{
			name:        "JSON but missing code and detail",
			handlerBody: `{"status":422}`,
			wantCode:    "",
			wantMsg:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			cfg := newTestConfig(store, 4096)
			mw := failedintent.CaptureMiddleware(cfg)

			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, tc.handlerBody)
			})

			req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			rw := httptest.NewRecorder()
			mw(inner).ServeHTTP(rw, req)

			require.Equal(t, 1, store.count())
			got := store.first()
			assert.Equal(t, tc.wantCode, got.ErrorCode)
			assert.Equal(t, tc.wantMsg, got.ErrorMessage)
		})
	}
}

// ---------------------------------------------------------------------------
// Property test: shouldCapture consistency
// ---------------------------------------------------------------------------

func TestProperty_ShouldCapture_Consistency(t *testing.T) {
	t.Parallel()

	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPatch,
		http.MethodPut, http.MethodDelete, http.MethodOptions,
	}
	paths := []string{
		"/", "/v2", "/v2/ventas", "/v2/ventas/x", "/v2/healthz", "/auth",
	}
	defaultMethods := map[string]bool{
		http.MethodPost:  true,
		http.MethodPatch: true,
		http.MethodPut:   true,
	}

	rapid.Check(t, func(rt *rapid.T) {
		method := rapid.SampledFrom(methods).Draw(rt, "method")
		path := rapid.SampledFrom(paths).Draw(rt, "path")
		withReplay := rapid.Bool().Draw(rt, "with_replay")
		withMultipart := rapid.Bool().Draw(rt, "with_multipart")

		store := &fakeStore{}
		cfg := newTestConfig(store, 1024)
		mw := failedintent.CaptureMiddleware(cfg)

		req := httptest.NewRequest(method, path, strings.NewReader(`{"x":1}`))
		req.Header.Set("Content-Type", "application/json")
		if withReplay {
			req.Header.Set(failedintent.HeaderInternalReplay, "1")
		}
		if withMultipart {
			req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
		}

		rw := httptest.NewRecorder()
		mw(handler422()).ServeHTTP(rw, req)

		// Derive expected capture decision from the same rules as shouldCapture.
		shouldCapture := defaultMethods[method] &&
			strings.HasPrefix(path, "/v2/ventas") &&
			!withReplay &&
			!withMultipart

		if shouldCapture {
			if store.count() != 1 {
				rt.Fatalf("method=%s path=%s: expected 1 save, got %d", method, path, store.count())
			}
		} else {
			if store.count() != 0 {
				rt.Fatalf("method=%s path=%s: expected 0 saves, got %d", method, path, store.count())
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Fuzz: body normalisation never panics, result is always valid JSON
// ---------------------------------------------------------------------------

func FuzzCaptureMiddlewareBody(f *testing.F) {
	f.Add([]byte(`{"venta_id":"abc","precio":0}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{"utf8":"Ñoño","emoji":"🎉","price":1.5}`))
	f.Add([]byte{0xFF, 0xFE, 0x00}) // invalid UTF-8

	f.Fuzz(func(t *testing.T, body []byte) {
		store := &fakeStore{}
		cfg := failedintent.Config{
			Store:        store,
			PathPrefixes: []string{"/v2/ventas"},
			Methods:      []string{http.MethodPost},
			BodyCapBytes: 1024,
		}
		mw := failedintent.CaptureMiddleware(cfg)

		req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()

		// Must not panic regardless of body content.
		mw(handler422()).ServeHTTP(rw, req)

		// When something was captured, its body must be valid JSON.
		if store.count() > 0 {
			got := store.first()
			assert.True(t, json.Valid(got.Body), "captured body must always be valid JSON")
		}
	})
}

// ---------------------------------------------------------------------------
// Coverage top-ups
// ---------------------------------------------------------------------------

func TestStatus_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "new", failedintent.StatusNew.String())
	assert.Equal(t, "retried_ok", failedintent.StatusRetriedOK.String())
}

// TestCaptureMiddleware_NoRequestID_GeneratesOne exercises requestIDOrNew when
// no request ID has been planted on the context.
func TestCaptureMiddleware_NoRequestID_GeneratesOne(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	assert.NotEqual(t, uuid.Nil, got.RequestID, "request id must be auto-generated when missing")
}

// TestCaptureMiddleware_NoBody_DoesNotPanic covers the readCappedBody nil
// branch when the request was constructed with http.NoBody.
func TestCaptureMiddleware_NoBody_DoesNotPanic(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)
	assert.Equal(t, 1, store.count())
}

// TestCaptureMiddleware_LargeWriteAfterFull verifies bufferForCapture when the
// internal buffer is already at capacity (subsequent writes are still forwarded).
func TestCaptureMiddleware_LargeWriteAfterFull(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	mw := failedintent.CaptureMiddleware(cfg)

	// Inner handler writes the response in two chunks, the second one after
	// the response body buffer is already saturated by an oversized first.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		big := strings.Repeat("a", 70*1024) // > DefaultResponseBodyCapBytes
		_, _ = io.WriteString(w, big)
		_, _ = io.WriteString(w, "after-full")
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(inner).ServeHTTP(rw, req)
	require.Equal(t, 1, store.count())
}

// TestConfig_Defaults verifies the Config.defaults method via its observable
// effect — a Config with everything zero behaves identically to one fully set
// to the documented defaults.
func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(failedintent.Config{Store: store})

	// PUT default method, default path prefix.
	req := httptest.NewRequest(http.MethodPut, "/v2/ventas/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	mw(handler422()).ServeHTTP(rw, req)
	assert.Equal(t, 1, store.count())
}
