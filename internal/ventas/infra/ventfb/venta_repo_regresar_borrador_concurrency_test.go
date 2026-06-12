//nolint:misspell // Spanish vocabulary (venta, aprobada, borrador) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestVentaRepo_RegresarABorrador_RepeatedUpdates_AreIdempotent exercises the
// idempotency contract of the regresar-borrador transition at the persistence
// layer: calling Update twice with aprobacion=nil on the same row must leave
// the persisted state identical to a single call. This is the property the row
// lock relies on when two requests race — the second one is a no-op rewrite,
// not a state-corrupting overwrite.
//
// True cross-connection concurrency (two parallel goroutines hammering the
// same row via separate tx contexts) is outside the test-transaction model
// (each test holds a single tx). The lock-mediated serialization in Firebird
// is an integration property we rely on; here we pin the only thing that can
// go wrong even with serialization — non-idempotent state writes.
func TestVentaRepo_RegresarABorrador_RepeatedUpdates_AreIdempotent(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		now := testNow()
		require.NoError(t, v.EnviarARevision(root, now))
		require.NoError(t, repo.Update(ctx, v))
		require.NoError(t, v.Aprobar(root, now))
		require.NoError(t, repo.Update(ctx, v))
		require.NoError(t, v.RegresarABorrador(root, now))
		require.NoError(t, repo.Update(ctx, v))

		// First snapshot after regress.
		first, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		require.Equal(t, domain.SituacionBorrador, first.Situacion())
		require.Nil(t, first.Aprobacion())

		// A second Update with the same (already-regressed) aggregate must
		// be a clean no-op rewrite: the row remains in borrador with
		// aprobacion null, audit updated_by stable.
		require.NoError(t, repo.Update(ctx, v))

		second, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, domain.SituacionBorrador, second.Situacion(),
			"second redundant Update must not flip situación")
		assert.Equal(t, domain.SincronizacionPendiente, second.Sincronizacion())
		assert.Nil(t, second.Aprobacion(), "aprobacion must stay null across repeated Updates")

		firstAudit := first.Audit()
		secondAudit := second.Audit()
		assert.Equal(t, firstAudit.UpdatedBy(), secondAudit.UpdatedBy(),
			"updated_by must be stable when nothing actually changed in the aggregate")
	})
}
