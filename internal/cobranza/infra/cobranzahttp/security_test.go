// Security sweep for the cobranza HTTP module: asserts that every mounted
// route (read + admin routers) enforces the authn/authz preamble
// (currentUserOrError → requirePerm) and that malformed path parameters are
// rejected cleanly by Huma's schema validation before ever reaching the
// handler. No Firebird is required: the routers are built with a nil
// *cobranzaapp.Service — the handler never dereferences it because either
// authorization fails first, or Huma's binding rejects the malformed
// parameter before the handler body runs at all.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// secNoPermUser is an authenticated principal that holds none of the
// cobranza permissions. Used to prove the 403 path is authorization, not
// authentication.
func secNoPermUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sec-monica-sin-permisos",
		Email:       "monica.pineda@muebleriamsp.mx",
		Nombre:      "Monica Pineda",
		Permisos:    []string{},
	}
}

// secAllPermsUser holds every cobranza permission so path-injection subtests
// exercise Huma's schema validation instead of getting short-circuited by an
// authorization failure.
func secAllPermsUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sec-raul-admin-total",
		Email:       "raul.hernandez@muebleriamsp.mx",
		Nombre:      "Raul Hernandez",
		Permisos: []string{
			string(authdomain.PermCobranzaVerSaldos),
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermCobranzaReconciliar),
			string(authdomain.PermCobranzaBackfill),
		},
	}
}

// secReadRouter mounts the read router with a nil service. cu == nil skips
// the planter middleware entirely, simulating an unauthenticated caller.
func secReadRouter(cu *auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	cobranzahttp.MountReadRouter(r, nil, eventbus.New(), config.Cobranza{}, slog.Default(), nil, nil)
	return r
}

// secAdminRouter mounts the admin router with a nil service/reconciler.
func secAdminRouter(cu *auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	cobranzahttp.MountAdminRouter(r, nil, nil, nil)
	return r
}

// secRequest builds a request body + Content-Type header for a route.
type secRequestBuilder func(t *testing.T) (io.Reader, string)

// secNoBody is a secRequestBuilder for GET/empty-input requests.
func secNoBody(_ *testing.T) (io.Reader, string) {
	return nil, ""
}

// secRoute is one row of the security sweep table.
type secRoute struct {
	// name identifies the subtest; also documents the required permission
	// per the verified route map.
	name string
	// buildRouter selects MountReadRouter or MountAdminRouter.
	buildRouter func(cu *auth.CurrentUser) http.Handler
	method      string
	// path is a well-formed request target: valid params, satisfies Huma
	// binding, so authn/authz is what determines the response.
	path string
	body secRequestBuilder
	// injectPath, when non-empty, is a variant of path with a malformed
	// int/uuid path parameter. Empty means the route has no path param to
	// fuzz.
	injectPath string
}

func TestSecurity_CobranzaRoutes(t *testing.T) {
	t.Parallel()

	// Valid UUID path segments for routes keyed on {id}. The nil pagosRepo
	// backing PagosPorVenta et al. is never reached because auth rejects
	// the request first in the 401/403 subtests, and Huma rejects the
	// malformed variant before the handler runs in the injection subtest.
	pagoID := uuid.New().String()

	pagoMultipart := func(t *testing.T) (io.Reader, string) {
		t.Helper()
		buf, ct := buildCrearPagoMultipart(t, "{}", nil)
		return buf, ct
	}

	routes := []secRoute{
		// ── read router: saldos ────────────────────────────────────────
		{
			name:        "GET /saldos/venta/{id} requires ver_saldos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/saldos/venta/5000",
			body:        secNoBody,
			injectPath:  "/saldos/venta/1%20OR%201=1",
		},
		{
			name:        "GET /saldos/cliente/{cliente_id} requires ver_saldos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/saldos/cliente/11486",
			body:        secNoBody,
			injectPath:  "/saldos/cliente/1%20OR%201=1",
		},
		{
			name:        "GET /saldos/zona/{zona_id} requires ver_saldos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/saldos/zona/7",
			body:        secNoBody,
			injectPath:  "/saldos/zona/..%2Fetc",
		},
		{
			name:        "GET /resumen-zonas requires ver_saldos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/resumen-zonas",
			body:        secNoBody,
		},
		// ── read router: pagos ──────────────────────────────────────────
		{
			name:        "GET /pagos/venta/{docto_cc_id} requires ver_pagos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/pagos/venta/5000",
			body:        secNoBody,
			injectPath:  "/pagos/venta/1%20OR%201=1",
		},
		{
			name:        "GET /pagos/{id} requires ver_pagos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/pagos/" + pagoID,
			body:        secNoBody,
			injectPath:  "/pagos/not-a-uuid",
		},
		{
			name:        "POST /pagos requires ver_pagos",
			buildRouter: secReadRouter,
			method:      http.MethodPost,
			path:        "/pagos",
			body:        pagoMultipart,
		},
		{
			name:        "GET /pagos/{id}/imagenes requires ver_pagos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/pagos/" + pagoID + "/imagenes",
			body:        secNoBody,
			injectPath:  "/pagos/not-a-uuid/imagenes",
		},
		// ── read router: sync ───────────────────────────────────────────
		{
			name:        "GET /sync/saldos/zona/{zona_id} requires ver_saldos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/sync/saldos/zona/7",
			body:        secNoBody,
			injectPath:  "/sync/saldos/zona/1%20OR%201=1",
		},
		{
			name:        "GET /sync/pagos/zona/{zona_id} requires ver_pagos",
			buildRouter: secReadRouter,
			method:      http.MethodGet,
			path:        "/sync/pagos/zona/7",
			body:        secNoBody,
			injectPath:  "/sync/pagos/zona/1%20OR%201=1",
		},
		// ── admin router ─────────────────────────────────────────────────
		{
			name:        "POST /reconcile requires reconciliar",
			buildRouter: secAdminRouter,
			method:      http.MethodPost,
			path:        "/reconcile",
			body:        secNoBody,
		},
		{
			name:        "POST /backfill requires backfill",
			buildRouter: secAdminRouter,
			method:      http.MethodPost,
			path:        "/backfill",
			body:        secNoBody,
		},
		{
			name:        "GET /errors requires reconciliar",
			buildRouter: secAdminRouter,
			method:      http.MethodGet,
			path:        "/errors",
			body:        secNoBody,
		},
		{
			name:        "GET /pagos/pendientes requires reconciliar",
			buildRouter: secAdminRouter,
			method:      http.MethodGet,
			path:        "/pagos/pendientes",
			body:        secNoBody,
		},
		{
			name:        "POST /pagos/{id}/aplicar requires reconciliar",
			buildRouter: secAdminRouter,
			method:      http.MethodPost,
			path:        "/pagos/" + pagoID + "/aplicar",
			body:        secNoBody,
			injectPath:  "/pagos/not-a-uuid/aplicar",
		},
	}

	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			t.Parallel()
			secRunRouteChecks(t, rt)
		})
	}
}

// secRunRouteChecks exercises the three assertions for one route: an
// unauthenticated caller gets 401, an authenticated caller lacking every
// cobranza permission gets 403, and — when the route has a path param — a
// malformed value is rejected in the 4xx range without ever hitting the nil
// service.
func secRunRouteChecks(t *testing.T, rt secRoute) {
	t.Helper()
	t.Run("no_auth_returns_401", func(t *testing.T) {
		t.Parallel()
		body, ct := rt.body(t)
		req := httptest.NewRequest(rt.method, rt.path, body)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()

		rt.buildRouter(nil).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
	})

	t.Run("no_permission_returns_403", func(t *testing.T) {
		t.Parallel()
		body, ct := rt.body(t)
		req := httptest.NewRequest(rt.method, rt.path, body)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()

		cu := secNoPermUser()
		rt.buildRouter(&cu).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	})

	if rt.injectPath == "" {
		return
	}

	t.Run("malformed_path_param_rejected_cleanly", func(t *testing.T) {
		t.Parallel()
		body, ct := rt.body(t)
		req := httptest.NewRequest(rt.method, rt.injectPath, body)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()

		cu := secAllPermsUser()
		rt.buildRouter(&cu).ServeHTTP(rec, req)

		assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
		assert.Less(t, rec.Code, 500, "body=%s", rec.Body.String())
	})
}
