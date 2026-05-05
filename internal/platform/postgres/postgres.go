// Package postgres provides the application's pgx pool wrapper.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// Pool wraps a *pgxpool.Pool with our lifecycle and a tagged logger.
type Pool struct {
	*pgxpool.Pool
	cfg config.Postgres
}

// New builds a *Pool from the given configuration. It does not yet open
// connections; call Start() to ping and verify.
func New(cfg config.Postgres) (*Pool, error) {
	pCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	pCfg.MaxConns = cfg.MaxOpenConns
	pCfg.MinConns = cfg.MaxIdleConns
	pCfg.MaxConnLifetime = time.Hour
	pCfg.MaxConnIdleTime = 30 * time.Minute
	pCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(context.Background(), pCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: build pool: %w", err)
	}
	return &Pool{Pool: pool, cfg: cfg}, nil
}

// Start verifies connectivity by pinging once with a short timeout.
func (p *Pool) Start(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Ping(pingCtx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}
	slog.InfoContext(
		ctx, "postgres: connected",
		"host", p.cfg.Host,
		"database", p.cfg.Database,
		"max_conns", p.cfg.MaxOpenConns,
	)
	return nil
}

// Stop closes the pool and waits for in-flight queries to finish.
func (p *Pool) Stop(_ context.Context) error {
	p.Close()
	return nil
}

// HealthCheck returns nil when a single round-trip query succeeds.
func (p *Pool) HealthCheck(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.Ping(pingCtx)
}
