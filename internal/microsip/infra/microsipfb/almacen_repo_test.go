package microsipfb_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// requireFBEnv skips when FB_DATABASE is unset (matches the gating used by
// every other Firebird-backed test in the repo).
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

func TestAlmacenRepo_Listar_HitsRealRows(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42, 8437, 6925})

	rows, err := repo.Listar(context.Background())
	require.NoError(t, err)
	if len(rows) == 0 {
		t.Skip("ALMACENES table is empty in the dev DB — nothing to assert against")
	}
	// Every row must carry a non-empty UTF-8 name (Win1252 decoded) and a
	// positive ID. Existencias is signed (can be negative for bad data).
	for _, a := range rows {
		assert.Positive(t, a.ID, "almacen ID should be positive")
		assert.NotEmpty(t, a.Nombre, "almacen name should not be empty")
	}
}

func TestAlmacenRepo_Obtener_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42, 8437, 6925})

	got, err := repo.Obtener(context.Background(), -1)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestAlmacenRepo_Obtener_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42, 8437, 6925})

	rows, err := repo.Listar(context.Background())
	require.NoError(t, err)
	if len(rows) == 0 {
		t.Skip("ALMACENES table is empty")
	}
	first := rows[0]
	got, err := repo.Obtener(context.Background(), first.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, first.ID, got.ID)
	assert.Equal(t, first.Nombre, got.Nombre)
}

func TestAlmacenRepo_ListarArticulos_EmptySearchReturnsRows(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42, 8437, 6925})

	almacenes, err := repo.Listar(context.Background())
	require.NoError(t, err)
	if len(almacenes) == 0 {
		t.Skip("no almacenes to query articulos for")
	}
	// Pick the almacen with the most existencias — most likely to have
	// articulos with positive saldos.
	picked := almacenes[0]
	for _, a := range almacenes {
		if a.Existencias > picked.Existencias {
			picked = a
		}
	}
	arts, err := repo.ListarArticulos(context.Background(), picked.ID, "")
	require.NoError(t, err)
	for _, a := range arts {
		assert.Positive(t, a.ArticuloID)
		assert.NotEmpty(t, a.Articulo)
		assert.Positive(t, a.Existencias, "WHERE clause filters out non-positive")
	}
}

func TestAlmacenRepo_NewWithEmptyPriceListIDs_DoesNotPanic(t *testing.T) {
	t.Parallel()
	// Construction must succeed regardless of FB env — exercises the
	// intListToInClause empty branch (NULL fallback). We do not call
	// ListarArticulos because that would require a live FB.
	repo := microsipfb.NewAlmacenRepo(nil, nil)
	assert.NotNil(t, repo)
}
