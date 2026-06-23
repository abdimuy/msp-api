//nolint:misspell // clientes vocabulary is Spanish per project convention.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/fx"

	authapp "github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"

	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
	"github.com/abdimuy/msp-api/internal/microsip/infra/microsiphttp"

	inventarioapp "github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invhttp"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticshttp"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp"

	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutashttp "github.com/abdimuy/msp-api/internal/rutas/infra/rutashttp"
)

// RootHandler is the assembled chi router exposed as an fx-typed dependency.
// Splitting the construction in two (root handler vs httpServer wrapper) lets
// the failedintent replay dispatcher receive the router via a post-construction
// fx.Invoke, breaking the dispatcher↔router↔handler cycle.
type RootHandler http.Handler

// publicV2PathPrefixes lists request-path prefixes under /v2 that bypass the
// authentication middleware. Huma auto-serves the OpenAPI spec and the docs
// UI at these paths, and industry convention (Stripe, GitHub, Twilio) is to
// keep API documentation publicly reachable — the spec describes the API
// surface, it does not contain user data, and devs need to read it before
// they can integrate.
var publicV2PathPrefixes = []string{
	"/v2/docs",
	"/v2/openapi",
	"/v2/schemas/",
}

// skipAuthForPublicDocs wraps the authn handler so requests whose path falls
// under publicV2PathPrefixes bypass authentication entirely.
func skipAuthForPublicDocs(authn func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		protected := authn(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range publicV2PathPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			protected.ServeHTTP(w, r)
		})
	}
}

// otelMiddleware wraps the chi-served handler chain in otelhttp, so every
// incoming request gets a server span. Sits BEFORE RequestID so the span is
// the outermost context and the request_id slot inside the span can later be
// correlated by the access-log middleware.
func otelMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "msp-api.http")
}

// httpServer wraps *http.Server so we can implement the lifecycle.Hooks
// interface and orchestrate graceful shutdown.
type httpServer struct {
	srv *http.Server
	cfg config.HTTP
}

// Start binds the listening socket and serves in a goroutine. Returning early
// lets fx mark startup as complete; the actual listen loop runs until Stop.
func (s *httpServer) Start(ctx context.Context) error {
	addr := s.srv.Addr
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("http: listen %s: %w", addr, err)
	}
	go func() {
		slog.InfoContext(ctx, "http: listening", "addr", addr)
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "http: serve failed", "error", err)
		}
	}()
	return nil
}

// Stop performs a graceful shutdown bounded by ShutdownTimeout.
func (s *httpServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(shutdownCtx)
}

// provideRootHandler assembles the chi router with the standard middleware
// stack, platform endpoints, the protected /v2 module routes, and the admin
// /v2/_admin/failed-intents endpoints. The returned http.Handler is then
// wrapped by provideHTTPServer; splitting the two lets the failedintent
// replay dispatcher receive the router via fx.Invoke.
//
//nolint:funlen // wiring function — splitting it would just hide the structural mount order from one obvious place.
func provideRootHandler(
	cfg *config.Config,
	health *healthcheck.Service,
	authSvc *authapp.Service,
	authFirebase outbound.FirebaseClient,
	authUsuarios outbound.UsuarioRepo,
	idemStore idempotency.Store,
	ventasSvc *ventasapp.Service,
	fiCaptureCfg failedintent.Config,
	fiSvc *failedintenthttp.Service,
	cobranzaSvc *cobranzaapp.Service,
	cobranzaReconciler *cobranzaapp.Reconciler,
	cobranzaErrors cobranzaoutbound.ErrorsRepo,
	cobranzaBus *eventbus.Bus,
	cobranzaPagosRepo cobranzaoutbound.PagosRepo,
	cobranzaVentasRepo cobranzaoutbound.VentasRepo,
	microsipSvc *microsipapp.Service,
	inventarioSvc *inventarioapp.Service,
	analyticsSvc *analyticsapp.Service,
	clientesSvc *clientesapp.Service,
	rutasSvc *rutasapp.Service,
	logger *slog.Logger,
) RootHandler {
	r := chi.NewRouter()

	// Middleware applied to every request. otelMiddleware sits outermost so
	// every other middleware runs inside a server span; RequestID then adds
	// the request_id attribute the access log correlates against.
	r.Use(
		otelMiddleware,
		middleware.RequestID,
		middleware.Recovery,
		middleware.AccessLog,
		middleware.SecurityHeaders,
		middleware.CORS(cfg.HTTP.CORSOrigins),
		middleware.BodyLimit(int64(cfg.HTTP.MaxBodySizeMB)*1024*1024),
		middleware.Timeout(cfg.HTTP.WriteTimeout),
	)

	// Platform endpoints — kept off the /v2 prefix so they don't trigger
	// auth/idempotency middleware once those are wired.
	r.Get("/healthz", health.Liveness)
	r.Get("/readyz", health.Readiness)
	r.Get("/version", versionHandler)

	// Shared chi middlewares for protected modules under /v2.
	authn := authhttp.NewAuthnMiddleware(authFirebase, authUsuarios, authSvc)
	idem := idempotency.Middleware(idempotency.Config{
		Store:      idemStore,
		TTL:        24 * time.Hour,
		Methods:    []string{http.MethodPost, http.MethodPatch},
		RequireKey: false,
	})
	capture := failedintent.CaptureMiddleware(fiCaptureCfg)

	// API surface. Module routers mount under /v2.
	r.Route("/v2", func(r chi.Router) {
		authhttp.MountRouter(r, authSvc, authFirebase, authUsuarios, idemStore)

		// Ventas routes share the authentication, idempotency, and
		// failed-intent capture chi middlewares; per-route authorization
		// (RequirePermission) is enforced inside each Huma handler against
		// the planted CurrentUser.
		r.Group(func(r chi.Router) {
			// capture wraps idem so it observes 409 idempotency_key_mismatch
			// and 400 idempotency_key_required — without this order the
			// "venta-zombie" pattern (app reposts with a fresh body under the
			// same key) escapes capture entirely. Capture skips its own work
			// when idem responds with the Idempotent-Replay header, which
			// signals "this 4xx/5xx was already captured on the original call".
			r.Use(skipAuthForPublicDocs(authn.Handler), capture, idem)
			venthttp.MountRouter(r, ventasSvc)
		})

		// Microsip catalog endpoints — read-only listings of almacenes,
		// articulos y zonas-cliente. Authn only: no permission gate (any
		// authenticated user reads catalogs), no idempotency (GET), no
		// failed-intent capture (no writes). The Huma API mounts a sub-
		// router so the docs-bypass skip still applies for /v2/docs etc.
		r.Group(func(r chi.Router) {
			r.Use(skipAuthForPublicDocs(authn.Handler))
			microsiphttp.MountRouter(r, microsipSvc)
		})

		// Inventario admin endpoints — GET /traspasos/{id}, GET /traspasos?venta_id=,
		// GET /inventario/stock, GET /inventario/almacenes. Authn + per-route
		// permission inside the handler (mirrors venthttp). No idempotency
		// (read-only). No failed-intent capture (no writes).
		r.Group(func(r chi.Router) {
			r.Use(skipAuthForPublicDocs(authn.Handler))
			invhttp.MountRouter(r, inventarioSvc)
		})

		// Analytics endpoints — authn only; permission enforced inside handlers.
		// The winback list and attribution are reads; the manual refresh POST is
		// a demo trigger guarded by PermAnalyticsRefresh inside the handler.
		// Authn-only, read-oriented (no idempotency middleware) like inventario;
		// mounted under a chi route prefix like cobranza (the handlers register
		// bare /winback paths, so the /analytics prefix lives here).
		r.Route("/analytics", func(r chi.Router) {
			r.Use(skipAuthForPublicDocs(authn.Handler))
			analyticshttp.MountRouter(r, analyticsSvc)
		})

		// Clientes hub endpoints — read-only Customer 360. Authn only (no
		// idempotency, no failed-intent capture — zero writes). The Huma
		// operations register bare /clientes/* paths, so we mount with Group
		// (not r.Route("/clientes")) to avoid a /v2/clientes/clientes double-prefix.
		// Final paths: /v2/clientes, /v2/clientes/{id}, /v2/clientes/{id}/ventas,
		// /v2/clientes/{id}/ventas/{doctoPvId}, /v2/clientes/_search/refresh.
		r.Group(func(r chi.Router) {
			r.Use(skipAuthForPublicDocs(authn.Handler))
			clienteshttp.MountRouter(r, clientesSvc)
		})

		// Rutas listing endpoint — read-only, authn only (no idempotency, no
		// failed-intent capture). MountRouter registers the operation at Path
		// "/rutas", so mount it on a bare group (like clientes) — NOT via
		// r.Route("/rutas"), which would double the prefix to /v2/rutas/rutas.
		// Final path: GET /v2/rutas.
		r.Group(func(r chi.Router) {
			r.Use(skipAuthForPublicDocs(authn.Handler))
			rutashttp.MountRouter(r, rutasSvc)
		})

		// Cobranza endpoints — authn only. Read (saldos, pagos, sync) plus
		// pago write (CrearPago, imágenes) share one chi router + huma.API.
		// Idempotency for POST /pagos is enforced end-to-end via body.id as
		// the canonical key (the repo INSERT trips DUPLICATE_KEY on retry and
		// the handler falls back to the idempotent fast-path).
		r.Route("/cobranza", func(r chi.Router) {
			r.Use(authn.Handler)
			cobranzahttp.MountReadRouter(r, cobranzaSvc, cobranzaBus, cfg.Cobranza, logger, cobranzaPagosRepo, cobranzaVentasRepo)
		})

		// Cobranza admin endpoints — authn only; no failed-intent capture.
		r.Route("/_admin/saldos", func(r chi.Router) {
			r.Use(authn.Handler)
			cobranzahttp.MountAdminRouter(r, cobranzaSvc, cobranzaReconciler, cobranzaErrors)
		})

		// Admin endpoints for failed-intent inspection / replay / resolution.
		// Authn is applied at the group level; per-route permission gates
		// live inside failedintenthttp.MountRouter.
		r.Route("/_admin/failed-intents", func(r chi.Router) {
			r.Use(authn.Handler)
			failedintenthttp.MountRouter(r, fiSvc)
		})

		// Self-service endpoint: lets the authenticated user query their own
		// failed intents. Authn is the only gate — no failed_intents:* perm
		// because the handler scopes results to the calling user's id.
		r.Route("/me/failed-intents", func(r chi.Router) {
			r.Use(authn.Handler)
			failedintenthttp.MountMeRouter(r, fiSvc)
		})
	})

	return RootHandler(r)
}

// provideHTTPServer wraps the assembled root handler in an *http.Server with
// the configured timeouts.
func provideHTTPServer(cfg *config.Config, root RootHandler) *httpServer {
	return &httpServer{
		srv: &http.Server{
			Addr:         net.JoinHostPort("0.0.0.0", strconv.Itoa(cfg.HTTP.Port)),
			Handler:      root,
			ReadTimeout:  cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout,
			IdleTimeout:  cfg.HTTP.IdleTimeout,
		},
		cfg: cfg.HTTP,
	}
}

// versionHandler returns build metadata as JSON.
func versionHandler(w http.ResponseWriter, _ *http.Request) {
	body := []byte(fmt.Sprintf(
		`{"version":%q,"build_time":%q,"started_at":%q}`,
		version, buildTime, startedAt.UTC().Format(time.RFC3339),
	))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// startedAt is captured at process boot for the version endpoint's uptime info.
var startedAt = time.Now()

// registerHTTPLifecycle hooks the HTTP server into fx.
func registerHTTPLifecycle(lc fx.Lifecycle, s *httpServer) {
	lc.Append(fx.Hook{
		OnStart: s.Start,
		OnStop:  s.Stop,
	})
}
