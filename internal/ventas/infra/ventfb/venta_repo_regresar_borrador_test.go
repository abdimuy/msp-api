//nolint:misspell // Spanish vocabulary (venta, aprobada, borrador) by convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestVentaRepo_Update_FromAprobada_NullsAprobacionFields drives a venta from
// borrador → revisada → aprobada (persisted) and then back to borrador via
// RegresarABorrador, and asserts the persisted APPROVED_AT / APPROVED_BY
// columns are NULL on Firebird. This pins the contract between the domain
// (Aprobacion() returning nil) and the repository helper aprobacionFields()
// + updateVentaHeader statement that materializes that nil into SQL NULL.
func TestVentaRepo_Update_FromAprobada_NullsAprobacionFields(t *testing.T) {
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

		// Sanity check: aprobacion fields are populated after Aprobar.
		approvedAt, approvedBy := readAprobacionRow(ctx, t, pool, v.ID().String())
		require.True(t, approvedAt.Valid, "APPROVED_AT must be set after Aprobar")
		require.True(t, approvedBy.Valid, "APPROVED_BY must be set after Aprobar")

		// Now regress and re-Update — the persisted columns must go back to NULL.
		require.NoError(t, v.RegresarABorrador(root, now))
		require.NoError(t, repo.Update(ctx, v))

		approvedAt, approvedBy = readAprobacionRow(ctx, t, pool, v.ID().String())
		assert.False(t, approvedAt.Valid, "APPROVED_AT must be NULL after RegresarABorrador, got %v", approvedAt)
		assert.False(t, approvedBy.Valid, "APPROVED_BY must be NULL after RegresarABorrador, got %v", approvedBy)
	})
}

// readAprobacionRow fetches APPROVED_AT and APPROVED_BY for the given venta id
// using the active test transaction's querier.
func readAprobacionRow(ctx context.Context, t *testing.T, pool *firebird.Pool, ventaID string) (sql.NullTime, sql.NullString) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var at sql.NullTime
	var by sql.NullString
	err := q.QueryRowContext(
		ctx,
		`SELECT APPROVED_AT, APPROVED_BY FROM MSP_VENTAS WHERE ID = ?`,
		ventaID,
	).Scan(&at, &by)
	require.NoError(t, err)
	return at, by
}
