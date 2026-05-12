// Package fbtestutil provides TestMain helpers and test fixtures for
// Firebird-backed integration tests.
//
// Tests that import this package must be gated on the FIREBIRD env var:
//
//	func TestMain(m *testing.M) { os.Exit(fbtestutil.RunWithFirebird(m)) }
//
// The FIREBIRD var is intentionally separate from INTEGRATION so Firebird
// tests can be skipped independently on machines that only have Postgres
// available (or vice-versa).
package fbtestutil

import (
	"os"
	"testing"
)

// RunWithFirebird is a TestMain helper for Firebird-gated tests.
//
// When FIREBIRD is unset the helper returns 0 without running any tests —
// the same gating pattern Postgres integration tests use with INTEGRATION=1.
// Callers should wrap the return value with os.Exit so deferred cleanup
// still runs:
//
//	func TestMain(m *testing.M) { os.Exit(fbtestutil.RunWithFirebird(m)) }
func RunWithFirebird(m *testing.M) int {
	if os.Getenv("FIREBIRD") == "" {
		return 0
	}
	return m.Run()
}
