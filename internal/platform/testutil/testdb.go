package testutil

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	adminDSN     string        // DSN that targets the template DB (admin privileges).
	adminPool    *pgxpool.Pool // Pool to the `postgres` system DB; reused for CREATE/DROP.
	errTemplate  error

	// dbCounter ensures unique per-package DB names even if Now() collides.
	dbCounter atomic.Uint64

	// pkgDBs tracks DBs created via NewTestDatabasePool so DropPackageDBs can
	// drop them at TestMain exit. NewTestDatabase (per-test) cleans itself via
	// tb.Cleanup and is not tracked here.
	pkgDBs   []string
	pkgDBsMu sync.Mutex
)

// ensureTemplate prepares the template DB used to clone per-test databases.
//
// Two paths:
//
//  1. TEST_DATABASE_URL set — reuse an existing Postgres instance. The DSN
//     must point to a DB already populated with migrations and (ideally)
//     marked IS_TEMPLATE. `make db-test-up` does both.
//  2. Otherwise — boot a Postgres container via testcontainers, run all
//     up migrations against the template DB, and mark it IS_TEMPLATE.
//
// In either path adminDSN points at the template DB, while adminPool stays
// connected to the `postgres` system DB so we can clone the template without
// holding open connections to it (Postgres rejects CREATE DATABASE FROM
// TEMPLATE while any session is attached to the template).
func ensureTemplate() error {
	templateOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
			adminDSN = dsn
			pool, err := pgxpool.New(ctx, replaceDBName(dsn, "postgres"))
			if err != nil {
				errTemplate = fmt.Errorf("testutil: connect postgres system db: %w", err)
				return
			}
			adminPool = pool
			return
		}

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

		// Migrations run through a short-lived pool to the template DB itself;
		// closed before flagging IS_TEMPLATE so no sessions remain attached.
		tplPool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			errTemplate = fmt.Errorf("testutil: connect template db: %w", err)
			return
		}
		if err := runMigrations(ctx, tplPool); err != nil {
			tplPool.Close()
			errTemplate = fmt.Errorf("testutil: %w", err)
			return
		}
		tplPool.Close()

		systemPool, err := pgxpool.New(ctx, replaceDBName(dsn, "postgres"))
		if err != nil {
			errTemplate = fmt.Errorf("testutil: connect postgres system db: %w", err)
			return
		}
		if _, err := systemPool.Exec(
			ctx,
			fmt.Sprintf(`ALTER DATABASE %q IS_TEMPLATE true`, templateDBName),
		); err != nil {
			systemPool.Close()
			errTemplate = fmt.Errorf("testutil: mark template: %w", err)
			return
		}
		adminPool = systemPool
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
	pkgDBsMu.Lock()
	pkgDBs = append(pkgDBs, name)
	pkgDBsMu.Unlock()
	return pool
}

// DropPackageDBs drops every DB created by NewTestDatabasePool in this
// process. Idempotent and safe to call when no DBs were created. Intended
// for TestMain so per-package DBs don't accumulate inside a long-running
// shared Postgres (msp-postgres-test):
//
//	func TestMain(m *testing.M) {
//	    if os.Getenv("INTEGRATION") != "" || os.Getenv("TEST_DATABASE_URL") != "" {
//	        testPool = testutil.NewTestDatabasePool()
//	    }
//	    code := m.Run()
//	    testutil.DropPackageDBs()
//	    os.Exit(code)
//	}
//
// With per-process testcontainers the cleanup is unnecessary (the whole
// container is reaped at process exit), but calling it is still cheap.
func DropPackageDBs() {
	pkgDBsMu.Lock()
	names := pkgDBs
	pkgDBs = nil
	pkgDBsMu.Unlock()

	if adminPool == nil || len(names) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, n := range names {
		dropDatabase(ctx, n)
	}
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

// createDBFromTemplate clones the template DB through the cached adminPool
// (connected to the postgres system DB) and returns a pool to the new DB.
func createDBFromTemplate(name string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

// dropDatabase removes the database, terminating live sessions if needed.
func dropDatabase(ctx context.Context, name string) {
	_, _ = adminPool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
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
