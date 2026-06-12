//nolint:misspell // Spanish vocabulary (productos, vendedores) by convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// atomicityTestEmailPattern is the marker used by every usuario seeded by
// this file. The startup scrub deletes any row whose EMAIL matches the
// pattern so a crashed previous run cannot leave permanent residue.
//
// .invalid is reserved by RFC 2606 and can never collide with a real address.
const atomicityTestEmailPattern = "e2e-atomicity-%@example.invalid"

// atomicityTestEmailPrefix is the literal prefix of the email used by a
// single seeded usuario. Combined with the row's uuid below to form a unique
// email per test, but still matching atomicityTestEmailPattern.
const atomicityTestEmailPrefix = "e2e-atomicity-"

// venatasTables is the canonical list of tables every atomicity test must
// snapshot before/after to prove the database is intact at the end.
//
//nolint:gochecknoglobals // package-private read-only constant list.
var ventasTables = []string{
	"MSP_USUARIOS",
	"MSP_VENTAS",
	"MSP_VENTAS_COMBOS",
	"MSP_VENTAS_PRODUCTOS",
	"MSP_VENTAS_VENDEDORES",
	"MSP_VENTAS_IMAGENES",
}

// snapshotCounts returns a map of table → row count for the tables listed
// in ventasTables. Used to prove the test left the database intact.
func snapshotCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	out := make(map[string]int, len(ventasTables))
	ctx := context.Background()
	for _, tbl := range ventasTables {
		var n int
		//nolint:gosec // table name is a package-private constant.
		require.NoError(t,
			db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&n),
			"snapshot count for %s", tbl,
		)
		out[tbl] = n
	}
	return out
}

// scrubAtomicityResidue deletes any usuario whose EMAIL matches the
// atomicity-test pattern. Runs at the start of every atomicity test so a
// process killed mid-cleanup on a previous run cannot pile up permanent
// rows. The operation is safe because the .invalid TLD guarantees no real
// user can ever have an address matching the pattern.
func scrubAtomicityResidue(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	res, err := db.ExecContext(ctx,
		"DELETE FROM MSP_USUARIOS WHERE EMAIL LIKE ?",
		atomicityTestEmailPattern,
	)
	require.NoError(t, err, "scrub previous atomicity residue")
	n, _ := res.RowsAffected()
	if n > 0 {
		t.Logf("scrubbed %d orphan atomicity usuario(s) from a previous run", n)
	}
}

// seedAtomicityUsuario inserts a usuario whose EMAIL matches the
// atomicity-test pattern and returns its UUID. The caller is responsible
// for deleting it (use deleteAtomicityUsuario, ideally registered via
// t.Cleanup as a safety net).
func seedAtomicityUsuario(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	now := testNow()
	email := atomicityTestEmailPrefix + id.String() + "@example.invalid"
	_, err := db.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'atomicity-test', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), "fb-atomicity-"+id.String(), email,
		now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed atomicity usuario")
	return id
}

// deleteAtomicityUsuario removes the test usuario. Idempotent: returns nil
// when the row is already gone (so it is safe to call from both the
// explicit cleanup step AND the t.Cleanup safety net).
func deleteAtomicityUsuario(db *sql.DB, id uuid.UUID) error {
	_, err := db.ExecContext(context.Background(),
		"DELETE FROM MSP_USUARIOS WHERE ID = ?", id.String())
	return err
}

// TestVentaRepo_Save_AtomicViaTxManager verifies that when Save fails
// mid-stream (FK violation on vendedor) inside firebird.TxManager.RunInTx,
// the rollback removes every prior INSERT — header, productos, etc. The
// test exercises the production atomicity path end-to-end with the real
// driver and a real Firebird tx.
//
// Why this lives outside fbtestutil.WithTestTransaction: Firebird does not
// nest transactions, so a real BEGIN/COMMIT/ROLLBACK cannot be observed
// from inside an outer test tx. To exercise the production rollback we
// need real top-level transactions; the test writes one MSP_USUARIOS row
// and deletes it on the way out, with a startup scrub + t.Cleanup safety
// net so a process kill never leaves permanent residue. The pre/post
// snapshot of every touched table proves the database is intact at the
// end of the test.
//
//nolint:paralleltest,funlen // serialized DB writes outside WithTestTransaction; long but linear.
func TestVentaRepo_Save_AtomicViaTxManager(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	txMgr := firebird.NewTxManager(pool.DB)
	repo := ventfb.NewVentaRepo(pool)
	ctx := context.Background()

	// Step 1: Idempotent scrub of any usuario residue from a previously
	// crashed atomicity test run.
	scrubAtomicityResidue(t, pool.DB)

	// Step 2: Snapshot every touched table BEFORE we do any test work.
	before := snapshotCounts(t, pool.DB)

	// Step 3: Seed one usuario as the FK target for created_by / updated_by.
	root := seedAtomicityUsuario(t, pool.DB)

	// Step 4: Register a t.Cleanup safety net. Runs even on panic. The
	// delete is idempotent so the explicit cleanup in step 7 doesn't
	// conflict.
	t.Cleanup(func() { _ = deleteAtomicityUsuario(pool.DB, root) })

	// Step 5: Build a venta whose vendedor.UsuarioID points to a uuid
	// that doesn't exist — the FK INSERT will fail mid-stream.
	ghost := uuid.New()
	v := buildVenta(t, newVentaInput{createdBy: root, vendedor: ghost})

	// Step 6: Run Save inside a REAL outer transaction via TxManager. The
	// production CrearVenta does exactly this. When Save returns an error,
	// TxManager's defer-rollback discards every INSERT it issued. This is
	// the actual lawsuit-grade atomicity check.
	err := txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return repo.Save(ctx, v)
	})
	require.Error(t, err, "Save with non-existent vendedor must propagate the error")

	// Step 7: Verify atomicity — query in a FRESH tx (post-rollback) and
	// confirm zero rows exist for our venta id across every child table.
	require.NoError(t, txMgr.RunInTx(ctx, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS", v.ID().String(), "ID", 0)
		assertCount(ctx, t, q, "MSP_VENTAS_PRODUCTOS", v.ID().String(), "VENTA_ID", 0)
		assertCount(ctx, t, q, "MSP_VENTAS_VENDEDORES", v.ID().String(), "VENTA_ID", 0)
		assertCount(ctx, t, q, "MSP_VENTAS_COMBOS", v.ID().String(), "VENTA_ID", 0)
		assertCount(ctx, t, q, "MSP_VENTAS_IMAGENES", v.ID().String(), "VENTA_ID", 0)
		return nil
	}))

	// Step 8: Explicit cleanup of the seeded usuario BEFORE the final
	// snapshot, so the snapshot reflects the post-test state of the DB.
	// The t.Cleanup safety-net delete in step 4 will be a no-op.
	require.NoError(t, deleteAtomicityUsuario(pool.DB, root),
		"explicit cleanup of test usuario before final snapshot")

	// Step 9: Snapshot AFTER and prove every touched table is identical.
	// If a stray INSERT escaped the rollback or our cleanup missed a row,
	// the diff will surface it immediately.
	after := snapshotCounts(t, pool.DB)
	for _, tbl := range ventasTables {
		assert.Equal(t, before[tbl], after[tbl],
			"row count must be identical after the test on table %s (before=%d, after=%d)",
			tbl, before[tbl], after[tbl])
	}
}
