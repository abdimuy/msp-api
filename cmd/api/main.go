// Package main is the API server entry point.
//
// Run subcommands:
//
//	msp-api serve      run the HTTP server (default).
//	msp-api version    print build metadata and exit.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/outbox"
	"github.com/abdimuy/msp-api/internal/platform/postgres"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// Build metadata, populated via -ldflags at compile time. See Makefile.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:   "msp-api",
		Short: "msp-api server and tooling",
	}
	root.AddCommand(serveCmd(), versionCmd(), authBootstrapCmd())

	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build metadata and exit",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Printf("msp-api %s (built %s)\n", version, buildTime)
			return err
		},
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API server",
		RunE: func(_ *cobra.Command, _ []string) error {
			fx.New(appOptions()...).Run()
			return nil
		},
	}
}

// appOptions returns the fx.Options that build the application graph.
func appOptions() []fx.Option {
	return []fx.Option{
		fx.Provide(
			config.Load,
			provideLogger,
			providePostgresPool,
			providePostgresTxManager,
			provideFirebirdPool,
			provideFirebirdTxManager,
			provideHealthService,
			provideOutboxRegistry,
			provideOutboxDispatcher,
			provideAuthUsuarioRepo,
			provideAuthRolRepo,
			provideAuthPermisoRepo,
			provideAuthClock,
			provideAuthFirebase,
			provideAuthOutboxEnqueuer,
			provideAuthService,
			provideIdempotencyStore,
			provideVentasRepo,
			provideVentasStorage,
			provideVentasClock,
			provideVentasOutboxEnqueuer,
			provideVentasImageProcessor,
			provideVentasService,
			provideHTTPServer,
		),
		fx.Invoke(
			registerPostgresLifecycle,
			registerFirebirdLifecycle,
			registerOutboxLifecycle,
			invokeAuthCatalogSync,
			registerHTTPLifecycle,
			registerProbes,
		),
		fx.NopLogger, // fx's own logs are silenced; we use slog instead.
	}
}

// provideLogger builds the structured logger and installs it as the slog default.
func provideLogger(cfg *config.Config) *slog.Logger {
	l := logger.New(logger.Options{
		Level:  cfg.App.LogLevel,
		Format: cfg.App.LogFormat,
	})
	slog.SetDefault(l)
	return l
}

// providePostgresPool builds the pgx pool from config.
func providePostgresPool(cfg *config.Config) (*postgres.Pool, error) {
	return postgres.New(cfg.Postgres)
}

// providePostgresTxManager wraps the pool with the application tx manager.
func providePostgresTxManager(p *postgres.Pool) *transaction.Manager {
	return transaction.NewManager(p.Pool)
}

// provideFirebirdPool builds the Firebird connection pool from config.
func provideFirebirdPool(cfg *config.Config) (*firebird.Pool, error) {
	return firebird.New(cfg.Firebird)
}

// provideFirebirdTxManager wraps the Firebird pool with a transaction manager.
func provideFirebirdTxManager(p *firebird.Pool) *firebird.TxManager {
	return firebird.NewTxManager(p.DB)
}

// provideHealthService returns a fresh health-check registry.
func provideHealthService() *healthcheck.Service {
	return healthcheck.New()
}

// provideOutboxRegistry returns an empty handler registry. Modules add their
// handlers via fx.Invoke when they're wired in.
func provideOutboxRegistry() *outbox.HandlerRegistry {
	return outbox.NewHandlerRegistry()
}

// provideOutboxDispatcher builds the outbox dispatcher. Defaults are applied
// inside NewDispatcher when fields are zero.
func provideOutboxDispatcher(p *postgres.Pool, reg *outbox.HandlerRegistry) *outbox.Dispatcher {
	return outbox.NewDispatcher(p.Pool, reg, outbox.DispatcherConfig{})
}

// registerPostgresLifecycle hooks the pool into the fx lifecycle.
func registerPostgresLifecycle(lc fx.Lifecycle, p *postgres.Pool) {
	lifecycle.Append(lc, "postgres", p)
}

// registerFirebirdLifecycle hooks the Firebird pool into the fx lifecycle.
func registerFirebirdLifecycle(lc fx.Lifecycle, p *firebird.Pool) {
	lifecycle.Append(lc, "firebird", p)
}

// registerOutboxLifecycle hooks the dispatcher into the fx lifecycle.
func registerOutboxLifecycle(lc fx.Lifecycle, d *outbox.Dispatcher) {
	lifecycle.Append(lc, "outbox-dispatcher", d)
}

// registerProbes registers the readiness probes for the dependencies the
// API needs to declare itself healthy.
func registerProbes(svc *healthcheck.Service, pgPool *postgres.Pool, fbPool *firebird.Pool) {
	svc.Register(healthcheck.ProbeFunc{
		N: "postgres",
		F: pgPool.HealthCheck,
	})
	svc.Register(healthcheck.ProbeFunc{
		N: "firebird",
		F: fbPool.HealthCheck,
	})
}
