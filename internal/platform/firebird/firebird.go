package firebird

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/nakagami/firebirdsql" // registers the "firebirdsql" sql/driver.
	"go.opentelemetry.io/otel/attribute"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// otelDriverName holds the wrapped driver name registered exactly once.
// otelsql.Register returns a unique generated name (e.g. "firebirdsql-otelsql-0")
// we then reuse for every Pool. Registration failures are stored so they
// bubble up cleanly on the first New() call rather than crashing init().
var (
	otelDriverOnce sync.Once
	otelDriverName string
	errOtelDriver  error
)

func registerOtelDriver() (string, error) {
	otelDriverOnce.Do(func() {
		// db.system follows the OTel semantic conventions: a free-form string
		// identifying the engine, so dashboards can group spans by backend.
		// We pin v1.7 of the convention (still supported by every collector
		// we care about) rather than the newer db.system.name from v1.39.
		name, err := otelsql.Register(
			"firebirdsql",
			otelsql.WithAttributes(attribute.String("db.system", "firebird")),
		)
		if err != nil {
			errOtelDriver = fmt.Errorf("firebird: register otelsql driver: %w", err)
			return
		}
		otelDriverName = name
	})
	return otelDriverName, errOtelDriver
}

// Pool wraps a *sql.DB connected to Firebird with our lifecycle hooks.
type Pool struct {
	*sql.DB
	cfg config.Firebird
}

// New opens a Pool against the configured Firebird instance. It does not yet
// verify connectivity; call Start() to ping.
func New(cfg config.Firebird) (*Pool, error) {
	name, err := registerOtelDriver()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(name, cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("firebird: open: %w", err)
	}
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}
	db.SetMaxOpenConns(poolSize)
	idle := poolSize / 2
	if idle > 5 {
		idle = 5
	}
	if idle < 1 {
		idle = 1
	}
	db.SetMaxIdleConns(idle)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(30 * time.Minute)

	// Wire sql.DBStats metrics into OTel. When no MeterProvider is configured
	// the noop meter's RegisterCallback returns a valid no-op registration
	// without error, so we treat any error here as fatal.
	if _, err := otelsql.RegisterDBStatsMetrics(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("firebird: register stats: %w", err)
	}

	return &Pool{DB: db, cfg: cfg}, nil
}

// Start verifies connectivity with a 5s ping. Logs a structured success line.
func (p *Pool) Start(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.PingContext(pingCtx); err != nil {
		return fmt.Errorf("firebird: ping: %w", err)
	}
	slog.InfoContext(ctx, "firebird: connected",
		"host", p.cfg.Host,
		"database", p.cfg.Database,
		"charset", p.cfg.Charset,
		"pool_size", p.cfg.PoolSize,
	)
	return nil
}

// Stop closes the underlying *sql.DB. Idempotent: subsequent calls return nil.
func (p *Pool) Stop(_ context.Context) error {
	if err := p.Close(); err != nil {
		return fmt.Errorf("firebird: close: %w", err)
	}
	return nil
}

// HealthCheck pings with a 2s budget for /readyz.
func (p *Pool) HealthCheck(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.PingContext(pingCtx)
}
