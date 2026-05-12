package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/fx"

	authapp "github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

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

// provideHTTPServer wires the chi router with the standard middleware stack,
// the platform endpoints (healthz/readyz/version), and mounts the per-module
// routers under /v2.
func provideHTTPServer(
	cfg *config.Config,
	health *healthcheck.Service,
	authSvc *authapp.Service,
	authFirebase outbound.FirebaseClient,
	authUsuarios outbound.UsuarioRepo,
	idemStore idempotency.Store,
	ventasSvc *ventasapp.Service,
) *httpServer {
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
	authn := authhttp.NewAuthnMiddleware(authFirebase, authUsuarios)
	idem := idempotency.Middleware(idempotency.Config{
		Store:      idemStore,
		TTL:        24 * time.Hour,
		Methods:    []string{http.MethodPost, http.MethodPatch},
		RequireKey: false,
	})

	// API surface. Module routers mount under /v2.
	r.Route("/v2", func(r chi.Router) {
		authhttp.MountRouter(r, authSvc, authFirebase, authUsuarios, idemStore)

		// Ventas routes share the authentication and idempotency chi
		// middlewares; per-route authorization (RequirePermission) is enforced
		// inside each Huma handler against the planted CurrentUser.
		r.Group(func(r chi.Router) {
			r.Use(authn.Handler, idem)
			venthttp.MountRouter(r, ventasSvc)
		})
	})

	return &httpServer{
		srv: &http.Server{
			Addr:         net.JoinHostPort("0.0.0.0", strconv.Itoa(cfg.HTTP.Port)),
			Handler:      r,
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
