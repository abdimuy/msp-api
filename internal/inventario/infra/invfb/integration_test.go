//go:build !short

// Package invfb integration tests require a live Microsip Firebird database.
// Set FB_DATABASE (and optionally FB_HOST, FB_PORT, FB_USER, FB_PASSWORD) to
// point at the dev instance. All writes execute inside a transaction that is
// always rolled back — nothing escapes to the real DB.
//
// Individual tests skip automatically when FB_DATABASE is not set via
// [fbtestutil.NewTestFirebirdPool].
//
//nolint:misspell // Microsip column names are Spanish by convention.
package invfb_test

import "context"

// noopCtx is a convenience alias used by tests that need a base context.
func noopCtx() context.Context { return context.Background() }

// suppress unused warning.
var _ = noopCtx
