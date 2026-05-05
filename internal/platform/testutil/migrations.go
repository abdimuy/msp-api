package testutil

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errMigrationsDirNotFound = errors.New(
		"could not find migrations/ — no ancestor of testutil/migrations.go contains both go.mod and migrations/",
	)
	errCallerUnknown = errors.New("runtime.Caller(0) failed")
)

// runMigrations applies every *.up.sql migration under <repo>/migrations to
// the given pool, in lexical order.
//
// The template DB is single-use, so we skip the schema_migrations bookkeeping
// that golang-migrate would do — it shaves ~250 ms off the boot path and
// removes the dependency from the test build. The `migrate` CLI is still the
// source of truth for dev/prod (`make migrate-*`).
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	dir, err := findMigrationsDir()
	if err != nil {
		return fmt.Errorf("testutil: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("testutil: read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	migFS := os.DirFS(dir)
	for _, f := range files {
		sql, err := fs.ReadFile(migFS, f)
		if err != nil {
			return fmt.Errorf("testutil: read migration %s: %w", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("testutil: execute migration %s: %w", f, err)
		}
	}
	return nil
}

// findMigrationsDir walks up from the directory of THIS source file looking
// for a `migrations/` folder next to a `go.mod`. Independent of the cwd from
// which `go test` was invoked, so the lookup works no matter which package
// triggers ensureTemplate.
func findMigrationsDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errCallerUnknown
	}
	dir := filepath.Dir(filename)
	for range 10 {
		if hasFile(dir, "go.mod") && hasDir(dir, "migrations") {
			return filepath.Join(dir, "migrations"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errMigrationsDirNotFound
}

func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

func hasDir(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}
