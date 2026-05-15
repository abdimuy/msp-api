package httpdispatch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/httpdispatch"
)

// TestInternalContext_StripsChiRouteKey is the canonical regression: a
// chi.Router serving a request whose context already carries a
// chi.RouteContext (the situation when an inner handler re-dispatches
// through the router) returns 404 because chi short-circuits routing.
// InternalContext must clear the key so the router re-routes from scratch.
func TestInternalContext_StripsChiRouteKey(t *testing.T) {
	t.Parallel()

	target := chi.NewRouter()
	target.Get("/ventas/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hit"))
	})

	// Simulate the parent handler: it has its own chi.Mux with the admin
	// route mounted. Inside its handler, it re-dispatches a fresh request
	// through `target` (the application router).
	parent := chi.NewRouter()
	parent.Post("/_admin/replay", func(w http.ResponseWriter, r *http.Request) {
		req := httptest.NewRequest(http.MethodGet, "/ventas/42", nil)
		req = req.WithContext(httpdispatch.InternalContext(r.Context()))
		target.ServeHTTP(w, req)
	})

	rec := httptest.NewRecorder()
	parent.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/_admin/replay", nil))

	require.Equal(t, http.StatusOK, rec.Code, "with InternalContext, target router re-routes fresh")
	assert.Equal(t, "hit", rec.Body.String())
}

// TestInternalContext_WithoutHelper_FailsRouting documents the buggy
// behaviour the helper exists to prevent: re-dispatching without
// stripping the route key never reaches the inner router's handler.
// The exact failure mode varies (404 NotFound or 405 MethodNotAllowed)
// depending on the parent route's method, but the inner handler is
// always skipped.
func TestInternalContext_WithoutHelper_FailsRouting(t *testing.T) {
	t.Parallel()

	handlerExecuted := false
	target := chi.NewRouter()
	target.Get("/ventas/{id}", func(w http.ResponseWriter, r *http.Request) {
		handlerExecuted = true
		w.WriteHeader(http.StatusOK)
	})

	parent := chi.NewRouter()
	parent.Post("/_admin/replay", func(w http.ResponseWriter, r *http.Request) {
		// Deliberately omit InternalContext to demonstrate the bug.
		req := httptest.NewRequest(http.MethodGet, "/ventas/42", nil)
		req = req.WithContext(r.Context())
		target.ServeHTTP(w, req)
	})

	rec := httptest.NewRecorder()
	parent.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/_admin/replay", nil))

	assert.False(t, handlerExecuted, "without InternalContext, chi short-circuits routing")
	assert.NotEqual(t, http.StatusOK, rec.Code, "the inner handler must not have responded")
}

// TestInternalContext_PreservesOtherValues confirms the helper only
// neutralises the chi route key — all other context values flow through
// unchanged, so request-id / trace-span / planted CurrentUser etc.
// remain visible to the re-dispatched handler chain.
func TestInternalContext_PreservesOtherValues(t *testing.T) {
	t.Parallel()

	type contextKey string
	const k contextKey = "trace-id"

	parent := context.WithValue(context.Background(), k, "abc123")
	// Plant a chi.RouteContext too, simulating the inherited parent state.
	parent = context.WithValue(parent, chi.RouteCtxKey, &chi.Context{})

	child := httpdispatch.InternalContext(parent)

	assert.Equal(t, "abc123", child.Value(k), "non-chi values must survive")

	// chi key resolves to a typed-nil *chi.Context; chi's existence check
	// (rctx != nil after .(*Context) assertion) must therefore evaluate false.
	rctx, _ := child.Value(chi.RouteCtxKey).(*chi.Context)
	assert.Nil(t, rctx, "chi route key must be typed-nil so chi re-routes from scratch")
}
