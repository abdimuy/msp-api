// Package testutil provides small helpers reused across test files.
package testutil

import (
	"context"
	"os"
	"testing"
	"time"
)

// Context returns a context that auto-cancels with the test deadline (or
// after 30s if the test has none).
func Context(t *testing.T) context.Context {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	return ctx
}

// RequireEnv skips the test when the named env var is empty. Use it to gate
// integration tests behind INTEGRATION=1, TEST_DATABASE_URL, etc.
func RequireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping: env %q not set", key)
	}
	return v
}

// SkipShort skips the calling test when `go test -short` is in effect.
// Used by integration tests that require a real DB.
func SkipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping in -short mode")
	}
}
