package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"

	// Drivers registered via blank imports.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// errMigrationsDirNotFound is returned when no ancestor of the cwd contains
// both a go.mod and a migrations/ directory.
var errMigrationsDirNotFound = errors.New("could not find migrations/ — no ancestor has both go.mod and migrations/")

// runMigrations applies every up migration under <repo>/migrations to the
// given Postgres DSN.
//
// The DSN is rewritten from "postgres://" to the "pgx5://" driver scheme
// expected by golang-migrate's Postgres driver registration.
func runMigrations(dsn string) error {
	dir, err := findMigrationsDir()
	if err != nil {
		return fmt.Errorf("testutil: %w", err)
	}

	m, err := migrate.New("file://"+dir, dsn)
	if err != nil {
		return fmt.Errorf("testutil: migrate.New: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("testutil: migrate.Up: %w", err)
	}
	return nil
}

// findMigrationsDir walks up from the current working directory looking for
// a `migrations/` folder next to a `go.mod` (the repo root). Returns an
// absolute path.
func findMigrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if hasFile(dir, "go.mod") && hasDir(dir, "migrations") {
			return filepath.Join(dir, "migrations"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errMigrationsDirNotFound
		}
		dir = parent
	}
}

func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

func hasDir(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}
