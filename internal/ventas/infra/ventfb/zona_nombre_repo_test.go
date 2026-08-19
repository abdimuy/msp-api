//nolint:misspell // Spanish vocabulary (zonas, nombres) by convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// pickExistingZona returns one (ZONA_CLIENTE_ID, NOMBRE) pair known to exist
// in Microsip's ZONAS_CLIENTES table. The dev DB is a Microsip dump, so there
// is always at least one row; if not, the test is skipped.
func pickExistingZona(ctx context.Context, t *testing.T, pool *firebird.Pool) (int, string) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var (
		id     int
		nombre string
	)
	err := q.QueryRowContext(ctx,
		`SELECT FIRST 1 ZONA_CLIENTE_ID, NOMBRE FROM ZONAS_CLIENTES ORDER BY ZONA_CLIENTE_ID`,
	).Scan(&id, &nombre)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("ZONAS_CLIENTES table is empty in the dev DB — cannot exercise the hit path")
	}
	require.NoError(t, err)
	return id, strings.TrimSpace(nombre)
}

func TestZonaNombreRepo_NombresPorID_HitsRealRow(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewZonaNombreRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id, nombre := pickExistingZona(ctx, t, pool)
		out, err := repo.NombresPorID(ctx, []int{id})
		require.NoError(t, err)
		assert.Equal(t, nombre, out[id], "expected ZONA_CLIENTE_ID=%d to resolve to its NOMBRE", id)
	})
}

func TestZonaNombreRepo_NombresPorID_DedupsAndOmitsMisses(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewZonaNombreRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id, nombre := pickExistingZona(ctx, t, pool)

		// MAX(ZONA_CLIENTE_ID)+1 is guaranteed to be a miss.
		q := firebird.GetQuerier(ctx, pool.DB)
		var maxID sql.NullInt32
		require.NoError(t, q.QueryRowContext(ctx, `SELECT MAX(ZONA_CLIENTE_ID) FROM ZONAS_CLIENTES`).Scan(&maxID))
		miss := 999_999_999
		if maxID.Valid {
			miss = int(maxID.Int32) + 1
		}

		// Duplicate the hit id; the miss id must be absent from the result.
		out, err := repo.NombresPorID(ctx, []int{id, id, miss})
		require.NoError(t, err)
		assert.Equal(t, nombre, out[id])
		_, present := out[miss]
		assert.False(t, present, "miss id must be absent from the map")
	})
}

func TestZonaNombreRepo_NombresPorID_EmptyInput(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewZonaNombreRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		out, err := repo.NombresPorID(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, out)
	})
}
