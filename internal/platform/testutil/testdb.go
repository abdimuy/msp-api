package testutil

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	templateDBName = "msp_template"
	pgUser         = "test"
	pgPassword     = "test"
)

var (
	templateOnce sync.Once
	adminDSN     string // DSN that targets the template DB (admin privileges)
	errTemplate  error  // captured by templateOnce; ensureTemplate returns it on subsequent calls

	// dbCounter ensures unique per-package DB names even if Now() collides.
	dbCounter atomic.Uint64
)

// ensureTemplate boots the Postgres container and runs migrations once per
// `go test` process. Subsequent callers reuse the template DB.
func ensureTemplate() error {
	templateOnce.Do(func() {
		ctx := context.Background()

		container, err := postgres.Run(
			ctx, "postgres:17-alpine",
			postgres.WithDatabase(templateDBName),
			postgres.WithUsername(pgUser),
			postgres.WithPassword(pgPassword),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		if err != nil {
			errTemplate = fmt.Errorf("testutil: start postgres: %w", err)
			return
		}
		// The testcontainers reaper drops the container at process exit.

		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			errTemplate = fmt.Errorf("testutil: connection string: %w", err)
			return
		}
		adminDSN = dsn

		if err := runMigrations(dsn); err != nil {
			errTemplate = fmt.Errorf("testutil: %w", err)
			return
		}
	})
	return errTemplate
}

// NewTestDatabasePool boots the container (once) and returns a pool to a
// fresh database created from the template. Intended for `TestMain`:
//
//	var testPool *pgxpool.Pool
//	func TestMain(m *testing.M) {
//	    if os.Getenv("INTEGRATION") == "" { os.Exit(0) }
//	    testPool = testutil.NewTestDatabasePool()
//	    os.Exit(m.Run())
//	}
//
// The DB lives until the container is reaped at process exit.
func NewTestDatabasePool() *pgxpool.Pool {
	if err := ensureTemplate(); err != nil {
		panic(err)
	}
	name := nextDBName("pkg")
	pool, err := createDBFromTemplate(name)
	if err != nil {
		panic(err)
	}
	return pool
}

// NewTestDatabase creates an isolated DB scoped to a single test, dropped
// automatically via t.Cleanup. Use only for tests that bypass the
// transaction context (e.g. multi-step pipelines that own their own TXs).
func NewTestDatabase(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	if err := ensureTemplate(); err != nil {
		tb.Fatalf("ensure template: %v", err)
	}

	name := nextDBName(sanitize(tb.Name()))
	pool, err := createDBFromTemplate(name)
	if err != nil {
		tb.Fatalf("create test db: %v", err)
	}

	tb.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dropDatabase(ctx, name)
	})

	return pool
}

func createDBFromTemplate(name string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		return nil, fmt.Errorf("connect admin: %w", err)
	}
	defer adminPool.Close()

	// Quoted identifier — name is sanitized but quote anyway for safety.
	if _, err := adminPool.Exec(
		ctx,
		fmt.Sprintf(`CREATE DATABASE %q TEMPLATE %q`, name, templateDBName),
	); err != nil {
		return nil, fmt.Errorf("create database %s: %w", name, err)
	}

	pool, err := pgxpool.New(ctx, replaceDBName(adminDSN, name))
	if err != nil {
		// Best-effort cleanup so we don't leak DBs on partial failure.
		dropDatabase(ctx, name)
		return nil, fmt.Errorf("open database %s: %w", name, err)
	}
	return pool, nil
}

// dropDatabase removes the database; ignored when it doesn't exist.
func dropDatabase(ctx context.Context, name string) {
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		return
	}
	defer adminPool.Close()
	_, _ = adminPool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, name))
}

func nextDBName(suffix string) string {
	n := dbCounter.Add(1)
	return sanitize(fmt.Sprintf("test_%s_%d_%d", suffix, time.Now().UnixNano(), n))
}

var sanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitize(s string) string {
	out := sanitizer.ReplaceAllString(s, "_")
	out = strings.Trim(out, "_")
	if out == "" {
		out = "x"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return strings.ToLower(out)
}

// replaceDBName swaps the DB segment in a postgres DSN of the form
// postgres://user:pass@host:port/dbname?sslmode=...
func replaceDBName(dsn, name string) string {
	qIdx := strings.Index(dsn, "?")
	query := ""
	base := dsn
	if qIdx >= 0 {
		query = dsn[qIdx:]
		base = dsn[:qIdx]
	}
	slashIdx := strings.LastIndex(base, "/")
	if slashIdx < 0 {
		return dsn // malformed; let the caller fail loudly
	}
	return base[:slashIdx+1] + name + query
}

// AdminDSN exposes the admin DSN to tests that need raw SQL access (e.g.
// installing extensions or seeding outside a transaction). Empty until the
// container has been started by ensureTemplate.
func AdminDSN() string { return adminDSN }

// ErrTemplateNotReady is returned when callers ask for the admin DSN before
// the container has booted. Useful for assertions in unit tests of testutil
// itself, not for normal flow.
var ErrTemplateNotReady = errors.New("testutil: template DB not ready")
