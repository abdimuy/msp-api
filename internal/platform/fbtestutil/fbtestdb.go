package fbtestutil

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Tests run against the REAL Microsip Firebird database — the same one the
// API talks to in dev. We do NOT spin up a clean container because the
// Microsip schema is proprietary and we can't recreate it from scratch.
//
// Safety contract:
//   - Tests must run inside a transaction that ALWAYS rolls back
//     (see fbtesttx.go's WithTestTransaction). Anything that escapes a
//     rollback is a bug in the test.
//   - Tests must never run DDL (CREATE/DROP/ALTER TABLE) — the dev DB
//     mirrors production schema; we only consume what's already there.
//   - Tests touch our own MSP_* tables (added by migrations-firebird/) or
//     run pure-read queries against Microsip's native tables.
//
// Required env vars: FB_DATABASE plus the standard FB_HOST/FB_PORT/FB_USER/
// FB_PASSWORD/FB_CHARSET defaults from .env. If FB_DATABASE is empty the
// helper skips the calling test instead of failing — that's how non-Firebird
// devs run the rest of the suite without a Firebird container.

var (
	fbOnce sync.Once
	fbPool *firebird.Pool
	errFB  error
)

// NewTestFirebirdPool returns a shared *firebird.Pool connected to the dev
// Microsip database described by the FB_* env vars. The pool is opened once
// per process and reused across tests.
//
// Behavior:
//   - FB_DATABASE empty → t.Skip with a clear message.
//   - Pool open or ping fails → t.Fatal.
//   - Otherwise → returns the shared pool.
func NewTestFirebirdPool(tb testing.TB) *firebird.Pool {
	tb.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		tb.Skip("FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests")
	}
	if err := ensurePool(); err != nil {
		tb.Fatalf("fbtestutil: %v", err)
	}
	return fbPool
}

// ensurePool opens the shared pool exactly once. Subsequent calls reuse the
// cached *firebird.Pool / error.
func ensurePool() error {
	fbOnce.Do(func() {
		cfg, err := loadFirebirdConfig()
		if err != nil {
			errFB = err
			return
		}

		pool, err := firebird.New(cfg)
		if err != nil {
			errFB = fmt.Errorf("fbtestutil: open pool: %w", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := pool.Start(ctx); err != nil {
			errFB = fmt.Errorf("fbtestutil: ping %s: %w", cfg.Host, err)
			return
		}
		fbPool = pool
	})
	return errFB
}

// loadFirebirdConfig reads the FB_* env vars directly so tests don't depend
// on the full config.Load pipeline (which would also require Postgres and
// Firebase env vars). Defaults mirror config.Firebird's struct tags.
func loadFirebirdConfig() (config.Firebird, error) {
	port, err := envInt("FB_PORT", 3050)
	if err != nil {
		return config.Firebird{}, fmt.Errorf("fbtestutil: FB_PORT: %w", err)
	}
	poolSize, err := envInt("FB_POOL_SIZE", 5)
	if err != nil {
		return config.Firebird{}, fmt.Errorf("fbtestutil: FB_POOL_SIZE: %w", err)
	}
	return config.Firebird{
		Host:         envOr("FB_HOST", "localhost"),
		Port:         port,
		Database:     os.Getenv("FB_DATABASE"),
		User:         envOr("FB_USER", "SYSDBA"),
		Password:     os.Getenv("FB_PASSWORD"),
		Charset:      envOr("FB_CHARSET", "UTF8"),
		PoolSize:     poolSize,
		WireCrypt:    envBool("FB_WIRE_CRYPT", true),
		WireCompress: envBool("FB_WIRE_COMPRESS", false),
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
