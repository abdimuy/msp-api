package main

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/canal"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
)

// appOptions returns the fx.Options that build cmd/winback's application
// graph. canal.Module() supplies the whole WhatsApp channel — service,
// worker, SQLite mailbox, forwarder, sender, router, and the lifecycle
// hooks that close the database and stop the worker in order (see that
// function's doc comment). This composition root supplies exactly what
// Module() asks for — *config.Config, *slog.Logger, and a chi.Router — plus
// the HTTP server that actually listens for it.
func appOptions() []fx.Option {
	return []fx.Option{
		fx.Provide(
			config.Load,
			provideLogger,
			provideRouter,
			provideHTTPServer,
		),
		canal.Module(),
		fx.Invoke(
			registerHTTPLifecycle,
			registerStartupLogLifecycle,
		),
		fx.NopLogger, // fx's own logs are silenced; we use slog instead.
	}
}

// provideLogger builds the structured logger and installs it as the slog
// default. Mirrors cmd/api/main.go's provideLogger.
func provideLogger(cfg *config.Config) *slog.Logger {
	l := logger.New(logger.Options{
		Level:  cfg.App.LogLevel,
		Format: cfg.App.LogFormat,
	})
	slog.SetDefault(l)
	return l
}

// provideRouter returns the bare chi.Router that canal.Module()'s
// registerRoutes mounts the whole canal HTTP surface onto (Meta's raw
// webhook plus the internal Huma API).
//
// The only middleware attached ahead of that surface is RequestID, Recovery
// and AccessLog — the leading three of this codebase's standard stack (see
// internal/platform/middleware's package doc). All three are body-safe:
// none of them read-and-replace the request body, which is the one thing
// forbidden ahead of the webhook route (its HMAC signature is verified
// against the exact request bytes — see canalhttp.MountRouter's doc
// comment). CORS, auth and idempotency are deliberately omitted: no browser
// calls this edge, and per ADR-0010's constraints table, canal has no
// Firebase-authenticated user — Meta authenticates by signature, the store
// by shared token.
func provideRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recovery, middleware.AccessLog)
	return r
}
