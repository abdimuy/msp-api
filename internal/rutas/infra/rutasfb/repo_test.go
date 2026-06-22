// Package rutasfb_test contains Firebird integration tests for the rutas repo.
// The tests skip cleanly when FB_DATABASE is not set; they do not connect to
// the shared dev DB unless explicitly enabled.
//
// Prerequisites when running:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - MSP_CFG_ZONA_CAJA table present (migration applied).
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/rutas/infra/rutasfb/...
//
//nolint:paralleltest // serial: shares rollback-only tx.
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

// requireFBEnv skips the test when FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// TestRutasRepo_ListarRutas returns without error and produces a non-nil slice.
func TestRutasRepo_ListarRutas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := rutasfb.NewRutasRepo(pool)
		rutas, err := repo.ListarRutas(ctx)
		require.NoError(t, err)
		// Even an empty ZONAS_CLIENTES returns a non-nil slice (may be empty).
		assert.NotNil(t, rutas)
		t.Logf("ListarRutas returned %d zonas", len(rutas))
	})
}
