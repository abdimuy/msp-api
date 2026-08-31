package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// httpServer wraps *http.Server so it can implement the lifecycle.Hooks
// shape and orchestrate graceful shutdown. Mirrors cmd/api/server.go's
// httpServer.
type httpServer struct {
	srv *http.Server
	cfg config.HTTP
}

// Start binds the listening socket and serves in a goroutine. Returning
// early lets fx mark startup as complete; the actual listen loop runs until
// Stop.
func (s *httpServer) Start(ctx context.Context) error {
	addr := s.srv.Addr
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("winback: http listen %s: %w", addr, err)
	}
	go func() {
		slog.InfoContext(ctx, "winback: http listening", "addr", addr)
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "winback: http serve failed", "error", err)
		}
	}()
	return nil
}

// Stop performs a graceful shutdown bounded by cfg.ShutdownTimeout. Running
// this under fx's own OnStop ctx (rather than a context derived from
// Start's, which fx has already cancelled by the time Stop fires) is what
// keeps this from racing canal.Module()'s own Stop hooks — fx runs every
// OnStop in reverse registration order, each with a fresh ctx bounded by
// StopTimeout, so the HTTP server closes its listener before
// canal-reenvio-worker drains and canal-buzon-sqlite closes the database.
func (s *httpServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(shutdownCtx)
}

// provideHTTPServer wraps the router canal.Module() mounts its whole HTTP
// surface onto in an *http.Server bound to config.Winback.Port. Every other
// timeout (read/write/idle) and the shutdown bound come from the shared
// [config.HTTP] section — see [config.Winback]'s doc comment for why only
// Port is winback-specific.
func provideHTTPServer(cfg *config.Config, router chi.Router) *httpServer {
	return &httpServer{
		srv: &http.Server{
			Addr:         net.JoinHostPort("0.0.0.0", strconv.Itoa(cfg.Winback.Port)),
			Handler:      router,
			ReadTimeout:  cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout,
			IdleTimeout:  cfg.HTTP.IdleTimeout,
		},
		cfg: cfg.HTTP,
	}
}

// registerHTTPLifecycle hooks the HTTP server into fx: OnStart binds the
// port, OnStop drains it gracefully.
func registerHTTPLifecycle(lc fx.Lifecycle, s *httpServer) {
	lc.Append(fx.Hook{
		OnStart: s.Start,
		OnStop:  s.Stop,
	})
}

// registerStartupLogLifecycle logs, at OnStart, exactly what an operator
// needs to confirm the deploy: the port winback bound and whether WhatsApp
// is configured. It never logs cfg.WhatsApp.Token, cfg.WhatsApp.AppSecret,
// cfg.Canal.WebhookVerifyToken or cfg.Canal.SharedToken — every one of
// those is a credential, not even truncated.
func registerStartupLogLifecycle(lc fx.Lifecycle, cfg *config.Config, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.InfoContext(ctx, "winback: starting",
				"port", cfg.Winback.Port,
				"whatsapp_enabled", cfg.WhatsApp.Enabled,
			)
			return nil
		},
	})
}
