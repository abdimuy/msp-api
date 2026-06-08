package idempotency_test

import (
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// memoryStore is an in-memory Store for unit tests.
type memoryStore struct {
	mu      sync.Mutex
	records map[string]*idempotency.Record
	getErr  error
	saveErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: map[string]*idempotency.Record{}}
}

func (m *memoryStore) Get(_ context.Context, key string) (*idempotency.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	rec, ok := m.records[key]
	if !ok {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found", matches Store contract
	}
	cp := *rec
	return &cp, nil
}

func (m *memoryStore) Save(_ context.Context, rec idempotency.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	r := rec
	m.records[rec.Key] = &r
	return nil
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestMiddleware_GET_BypassedEntirely(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store, RequireKey: true})

	calls := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)
	assert.Equal(t, 1, calls)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_POST_RequiresKeyByDefault(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store, RequireKey: true})

	calls := 0
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	h.ServeHTTP(rec, r)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, 0, calls, "downstream must not run when key is missing")
}

func TestMiddleware_POST_RequireKeyFalse_PassesThrough(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store, RequireKey: false})

	calls := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	h.ServeHTTP(rec, r)
	assert.Equal(t, 1, calls)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_FirstCall_StoresResponse(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store, TTL: time.Hour})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v2/cobros", strings.NewReader(`{"a":1}`))
	r.Header.Set(idempotency.HeaderKey, "key-1")
	mw(okHandler()).ServeHTTP(rec, r)

	assert.Equal(t, http.StatusCreated, rec.Code)

	// store should now contain the record.
	store.mu.Lock()
	saved := store.records["key-1"]
	store.mu.Unlock()
	require.NotNil(t, saved)
	assert.Equal(t, http.StatusCreated, saved.ResponseStatus)
	assert.Equal(t, http.MethodPost, saved.Method)
	assert.Equal(t, "/v2/cobros", saved.Path)
	assert.NotEmpty(t, saved.RequestHash)
	assert.True(t, saved.ExpiresAt.After(time.Now()))
}

func TestMiddleware_SecondCall_SamePayload_Replays(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	calls := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"first":true}`))
	}))

	body := `{"a":1}`
	first := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	r1.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(first, r1)

	second := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	r2.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(second, r2)

	assert.Equal(t, 1, calls, "downstream must run only once")
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, "true", second.Header().Get("Idempotent-Replay"))
	assert.Equal(t, `{"first":true}`, strings.TrimSpace(second.Body.String()))
}

func TestMiddleware_SecondCall_DifferentPayload_Returns422Mismatch(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	h := mw(okHandler())

	r1 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	r1.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(httptest.NewRecorder(), r1)

	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":2}`))
	r2.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(rec2, r2)

	// IETF Idempotency-Key draft §2.7: body-fingerprint mismatch is 422,
	// not 409. 409 is reserved for in-flight concurrent retries.
	assert.Equal(t, http.StatusUnprocessableEntity, rec2.Code)
	assert.Equal(t, "application/problem+json; charset=utf-8", rec2.Header().Get("Content-Type"))

	var p struct{ Code string }
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&p))
	assert.Equal(t, "idempotency_key_mismatch", p.Code)
}

func TestMiddleware_4xxResponse_NotCached(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store, TTL: time.Hour})

	calls := 0
	validationFailing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":"plazo_invalido","message":"el plazo en meses debe ser mayor a cero"}`))
	})
	h := mw(validationFailing)

	r1 := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"plazo":0}`))
	r1.Header.Set(idempotency.HeaderKey, "sale-abc")
	first := httptest.NewRecorder()
	h.ServeHTTP(first, r1)
	require.Equal(t, http.StatusUnprocessableEntity, first.Code)

	// Store must NOT have a record — 4xx is not cached.
	store.mu.Lock()
	_, present := store.records["sale-abc"]
	store.mu.Unlock()
	assert.False(t, present, "4xx responses must not be cached")

	// Second call with the same key + same body re-runs the handler instead
	// of replaying a stale error — the client always sees the current error.
	r2 := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"plazo":0}`))
	r2.Header.Set(idempotency.HeaderKey, "sale-abc")
	second := httptest.NewRecorder()
	h.ServeHTTP(second, r2)

	assert.Equal(t, http.StatusUnprocessableEntity, second.Code)
	assert.Equal(t, 2, calls, "handler must re-execute when prior was 4xx")
	assert.Empty(t, second.Header().Get("Idempotent-Replay"),
		"4xx replay must not be flagged as idempotent — it was a fresh execution")
}

func TestMiddleware_4xxThenFixedRetry_Succeeds(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	// Handler returns 422 if the body says "bad", 201 otherwise.
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "bad") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	// First attempt: bad payload → 422 → NOT cached.
	r1 := httptest.NewRequest(http.MethodPost, "/v2/x", strings.NewReader(`{"v":"bad"}`))
	r1.Header.Set(idempotency.HeaderKey, "k-fix")
	first := httptest.NewRecorder()
	h.ServeHTTP(first, r1)
	require.Equal(t, http.StatusUnprocessableEntity, first.Code)

	// Second attempt: same key, fixed payload → 201. No 422 mismatch because
	// the prior 4xx was never cached, so there's no record to compare against.
	r2 := httptest.NewRequest(http.MethodPost, "/v2/x", strings.NewReader(`{"v":"good"}`))
	r2.Header.Set(idempotency.HeaderKey, "k-fix")
	second := httptest.NewRecorder()
	h.ServeHTTP(second, r2)

	assert.Equal(t, http.StatusCreated, second.Code,
		"after a 4xx (not cached), the same key with a fixed body must succeed")
}

func TestMiddleware_5xxResponse_NotCached(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	calls := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))

	r1 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r1.Header.Set(idempotency.HeaderKey, "k5")
	h.ServeHTTP(httptest.NewRecorder(), r1)

	store.mu.Lock()
	_, present := store.records["k5"]
	store.mu.Unlock()
	assert.False(t, present, "5xx responses must not be cached")

	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r2.Header.Set(idempotency.HeaderKey, "k5")
	h.ServeHTTP(httptest.NewRecorder(), r2)

	assert.Equal(t, 2, calls, "handler must re-execute when prior was 5xx")
}

func TestMiddleware_2xxCached_DifferentBody_Returns422(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	// First request succeeds and is cached.
	h := mw(okHandler())
	r1 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"v":1}`))
	r1.Header.Set(idempotency.HeaderKey, "k2")
	first := httptest.NewRecorder()
	h.ServeHTTP(first, r1)
	require.Equal(t, http.StatusCreated, first.Code)

	// Second request: same key, different body → genuine mismatch against a
	// stored success. This is the only path that should ever produce 422
	// idempotency_key_mismatch in the new model.
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"v":2}`))
	r2.Header.Set(idempotency.HeaderKey, "k2")
	second := httptest.NewRecorder()
	h.ServeHTTP(second, r2)

	assert.Equal(t, http.StatusUnprocessableEntity, second.Code)
	var p struct{ Code string }
	require.NoError(t, json.NewDecoder(second.Body).Decode(&p))
	assert.Equal(t, "idempotency_key_mismatch", p.Code)
}

func TestMiddleware_StoreGetError_FailsOpen(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	store.getErr = errors.New("db down")

	mw := idempotency.Middleware(idempotency.Config{Store: store})

	calls := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(rec, r)

	assert.Equal(t, 1, calls, "fail-open: still process the request")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMiddleware_BodyIsRestoredForDownstream(t *testing.T) {
	t.Parallel()
	store := newMemoryStore()
	mw := idempotency.Middleware(idempotency.Config{Store: store})

	var seen string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	r.Header.Set(idempotency.HeaderKey, "k")
	h.ServeHTTP(rec, r)

	assert.Equal(t, `{"a":1}`, seen, "downstream must see the original body")
}
