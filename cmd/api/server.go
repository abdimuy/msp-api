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
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
)

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

// provideHTTPServer wires the chi router with the standard middleware stack
// and the platform endpoints (healthz/readyz/version).
func provideHTTPServer(cfg *config.Config, health *healthcheck.Service) *httpServer {
	r := chi.NewRouter()

	// Middleware applied to every request.
	r.Use(
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

	// API surface. Module routers will mount under /v2 once they exist.
	r.Route("/v2", func(_ chi.Router) {
		// modules will register here.
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
