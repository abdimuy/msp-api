//nolint:misspell // Spanish vocabulary (clientes) by convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// pickExistingClienteID returns one CLIENTE_ID known to exist in Microsip's
// CLIENTES table. The dev DB is a Microsip dump, so there is always at least
// one row; if not, the test is skipped (no production data to anchor on).
func pickExistingClienteID(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var id int
	err := q.QueryRowContext(ctx, `SELECT FIRST 1 CLIENTE_ID FROM CLIENTES`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("CLIENTES table is empty in the dev DB — cannot exercise Exists hit path")
	}
	require.NoError(t, err)
	return id
}

func TestClienteRepo_Exists_HitsRealRow(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id := pickExistingClienteID(ctx, t, pool)
		ok, err := repo.Exists(ctx, id)
		require.NoError(t, err)
		assert.True(t, ok, "expected CLIENTE_ID=%d to exist", id)
	})
}

func TestClienteRepo_Exists_Miss(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// MAX(CLIENTE_ID)+1 is guaranteed to be a miss.
		q := firebird.GetQuerier(ctx, pool.DB)
		var maxID sql.NullInt32
		require.NoError(t, q.QueryRowContext(ctx, `SELECT MAX(CLIENTE_ID) FROM CLIENTES`).Scan(&maxID))
		probe := 999_999_999
		if maxID.Valid {
			probe = int(maxID.Int32) + 1
		}
		ok, err := repo.Exists(ctx, probe)
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestClienteRepo_Exists_NonPositiveIDShortCircuits(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		for _, id := range []int{0, -1, -999} {
			ok, err := repo.Exists(ctx, id)
			require.NoError(t, err, "id=%d", id)
			assert.False(t, ok, "id=%d", id)
		}
	})
}

func TestClienteRepo_Exists_PropagatesContextCancel(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.Exists(canceledCtx, 1)
		require.Error(t, err)
	})
}

func TestClienteRepo_ZonaDeCliente_HitsRealRow(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		var id int
		var zona int
		err := q.QueryRowContext(ctx, `SELECT FIRST 1 CLIENTE_ID, ZONA_CLIENTE_ID FROM CLIENTES WHERE ZONA_CLIENTE_ID IS NOT NULL`).Scan(&id, &zona)
		if errors.Is(err, sql.ErrNoRows) {
			t.Skip("no cliente with ZONA_CLIENTE_ID in dev DB")
		}
		require.NoError(t, err)
		got, err := repo.ZonaDeCliente(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, zona, *got)
	})
}

func TestClienteRepo_ZonaDeCliente_NullZona(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Pick any existing cliente and temporarily NULL out its ZONA_CLIENTE_ID.
		// The transaction rolls back at the end of the test so the dev DB is
		// left unchanged.
		id := pickExistingClienteID(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)
		_, err := q.ExecContext(ctx, `UPDATE CLIENTES SET ZONA_CLIENTE_ID = NULL WHERE CLIENTE_ID = ?`, id)
		require.NoError(t, err)

		got, err := repo.ZonaDeCliente(ctx, id)
		require.NoError(t, err, "NULL zona must not error")
		assert.Nil(t, got, "NULL zona must return nil pointer")
	})
}

func TestClienteRepo_ZonaDeCliente_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewClienteRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		got, err := repo.ZonaDeCliente(ctx, 999_999_999)
		require.ErrorIs(t, err, domain.ErrClienteNotFoundInMicrosip)
		assert.Nil(t, got)
	})
}
