package firebird_test

// Integration test for the Win1252 encoding boundary against the live
// Firebird database.
//
// Run with:
//
//	INTEGRATION=1 go test -count=1 -v -run TestIntegration_FirebirdEncoding ./internal/platform/firebird/
//
// The test requires the FB_* environment variables to point at the dev
// Microsip Firebird database (same vars used by fbtestutil). It inserts a
// row into MSP_USUARIOS with a fully accented Spanish name, reads it back,
// asserts the round-trip is lossless, and ROLLS BACK so no data persists.
//
// This test is skipped when the INTEGRATION environment variable is empty or
// when FB_DATABASE is not set.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestIntegration_FirebirdEncoding verifies that a Win1252-encoded string
// persisted into MSP_USUARIOS.NOMBRE survives a round-trip through the live
// Firebird database without data loss.
//
//nolint:paralleltest // uses a live transaction that must not run in parallel.
func TestIntegration_FirebirdEncoding(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("INTEGRATION not set — skipping live Firebird encoding test")
	}

	pool := fbtestutil.NewTestFirebirdPool(t) // skips if FB_DATABASE unset

	// We need a self-referential root user for CREATED_BY / UPDATED_BY FKs.
	// Everything runs inside a rolled-back transaction.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := pool.BeginTx(ctx, nil)
	require.NoError(t, err, "begin transaction")
	defer func() { _ = tx.Rollback() }() // always roll back — never commit.

	// ── Insert sentinel root usuario (self-referential) ──────────────────────
	rootID := uuid.New()
	sentinel := rootID.String()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	rootNombreEnc, err := firebird.EncodeWin1252("Root Test")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sentinel,
		"integration-root-"+sentinel,
		"enc-root-"+sentinel+"@muebleriamsp.mx",
		rootNombreEnc,
		true,
		firebird.ToWallClock(now), firebird.ToWallClock(now),
		sentinel, sentinel,
	)
	require.NoError(t, err, "insert root usuario")

	// ── Insert test usuario with fully accented Spanish name ─────────────────
	testID := uuid.New()
	testSentinel := testID.String()
	wantNombre := "Mérida áéíóúñ ¿¡ €"

	nombreEnc, err := firebird.EncodeWin1252(wantNombre)
	require.NoError(t, err, "EncodeWin1252 must succeed for Spanish orthography")

	_, err = tx.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		testSentinel,
		"integration-enc-"+testSentinel,
		"enc-test-"+testSentinel+"@muebleriamsp.mx",
		nombreEnc,
		true,
		firebird.ToWallClock(now), firebird.ToWallClock(now),
		sentinel, sentinel,
	)
	require.NoError(t, err, "insert test usuario with accented nombre")

	// ── Read it back and assert round-trip ────────────────────────────────────
	var gotNombre firebird.Win1252
	err = tx.QueryRowContext(ctx,
		`SELECT NOMBRE FROM MSP_USUARIOS WHERE ID = ?`, testSentinel,
	).Scan(&gotNombre)
	require.NoError(t, err, "select nombre back from DB")

	assert.Equal(t, wantNombre, string(gotNombre),
		"round-trip through Firebird must be lossless for fully accented Spanish string")
}
