//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for migration 000023: MSP_RECOMPUTE_PAGO and
// MSP_RECOMPUTE_SALDO_VENTA write changelog rows and set TX_ID in the cache.
//
// These tests require FB_DATABASE to be set (requireFBEnv) and migrations
// 000022 and 000023 to be applied.  They use committed fixtures (direct pool
// INSERTs via auto-commit) so that trigger-driven writes are visible to
// independent connections — the same pattern used in
// digest_query_integration_test.go.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireMigration000023 skips the test when migration 000023 has not been
// applied to the database.
func requireMigration000023(t *testing.T) {
	t.Helper()
	pool := fbtestutil.NewTestFirebirdPool(t)
	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM MSP_MIGRATIONS WHERE ID = 23`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000023 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// ─── TestMig23_RecomputePago_NormalUpsert_WritesChangelogAndTxId ─────────────

// TestMig23_RecomputePago_NormalUpsert_WritesChangelogAndTxId verifica que una
// inserción normal de pago escriba TX_ID > 0 en MSP_PAGOS_VENTAS y una fila
// en MSP_PAGOS_CHANGELOG con el mismo TX_ID.
//
//nolint:paralleltest
func TestMig23_RecomputePago_NormalUpsert_WritesChangelogAndTxId(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	clienteID, _ := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("M3N%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("2000.00"))
	impteID := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("500.00"))
	requirePagosVentasCacheRow(t, pool, impteID)

	ctx := context.Background()

	// Verificar TX_ID > 0 en el caché.
	var cacheTxID int64
	err := pool.QueryRowContext(ctx,
		`SELECT TX_ID FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&cacheTxID)
	require.NoError(t, err, "MSP_PAGOS_VENTAS debe tener fila para impteID")
	assert.Positive(t, cacheTxID, "TX_ID en caché debe ser > 0 tras mig 23")

	// Verificar al menos 1 fila de changelog para este impteID.
	var changelogCount int
	err = pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&changelogCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, changelogCount, 1,
		"MSP_PAGOS_CHANGELOG debe tener al menos 1 fila para impteID")

	// Verificar que TX_ID del changelog coincide con el del caché
	// (mismo recompute → misma CURRENT_TRANSACTION).
	var changelogTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_PAGOS_CHANGELOG
		  WHERE IMPTE_DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, impteID,
	).Scan(&changelogTxID)
	require.NoError(t, err, "debe existir fila en MSP_PAGOS_CHANGELOG")
	assert.Equal(t, cacheTxID, changelogTxID,
		"TX_ID del changelog debe coincidir con TX_ID del caché")

	// Cleanup adicional del changelog (el cleanup del caché ya fue registrado
	// por insertCommittedPago; aquí limpiamos el changelog).
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})

	t.Logf("Mig23 pago normal: impteID=%d cacheTxID=%d changelogTxID=%d count=%d",
		impteID, cacheTxID, changelogTxID, changelogCount)
}

// ─── TestMig23_RecomputePago_Tombstone_WritesChangelog ───────────────────────

// TestMig23_RecomputePago_Tombstone_WritesChangelog verifica que al cancelar
// un pago (CANCELADO='S') el caché tenga CANCELADO='S' + TX_ID > 0, y el
// changelog acumule 2 filas (inserción original + cancelación).
//
//nolint:paralleltest
func TestMig23_RecomputePago_Tombstone_WritesChangelog(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	clienteID, _ := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("M3T%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("1500.00"))
	impteID := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("300.00"))
	requirePagosVentasCacheRow(t, pool, impteID)

	ctx := context.Background()

	// Cancelar el importe — dispara el trigger → MSP_RECOMPUTE_PAGO path tombstone.
	_, err := pool.ExecContext(ctx,
		`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'S' WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	require.NoError(t, err, "UPDATE CANCELADO='S' debe ejecutarse")

	// Verificar tombstone en el caché.
	var cancelado string
	var txID int64
	err = pool.QueryRowContext(ctx,
		`SELECT CANCELADO, TX_ID FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&cancelado, &txID)
	require.NoError(t, err, "fila de caché debe existir tras tombstone")
	assert.Equal(t, "S", strings.TrimSpace(cancelado), "CANCELADO debe ser 'S'")
	assert.Positive(t, txID, "TX_ID debe ser > 0 en la fila tombstone")

	// Verificar que hay exactamente 2 filas de changelog para este pk
	// (1 por inserción original, 1 por cancelación).
	var changelogCount int
	err = pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&changelogCount)
	require.NoError(t, err)
	assert.Equal(t, 2, changelogCount,
		"debe haber exactamente 2 filas de changelog: inserción + cancelación")

	// Cleanup del changelog.
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx,
			`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'N' WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})

	t.Logf("Mig23 pago tombstone: impteID=%d txID=%d changelogCount=%d",
		impteID, txID, changelogCount)
}

// ─── TestMig23_RecomputeSaldoVenta_NormalUpsert_WritesChangelogAndTxId ───────

// TestMig23_RecomputeSaldoVenta_NormalUpsert_WritesChangelogAndTxId verifica
// que al insertar un cargo la tabla MSP_SALDOS_VENTAS tenga TX_ID > 0 y
// MSP_SALDOS_CHANGELOG tenga al menos 1 fila con el mismo TX_ID.
//
//nolint:paralleltest
func TestMig23_RecomputeSaldoVenta_NormalUpsert_WritesChangelogAndTxId(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	clienteID, _ := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("M3S%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("3000.00"))
	requireSaldosCacheRow(t, pool, cargoID)

	ctx := context.Background()

	// Verificar TX_ID > 0 en el caché de saldos.
	var cacheTxID int64
	err := pool.QueryRowContext(ctx,
		`SELECT TX_ID FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID,
	).Scan(&cacheTxID)
	require.NoError(t, err, "MSP_SALDOS_VENTAS debe tener fila para cargoID")
	assert.Positive(t, cacheTxID, "TX_ID en caché de saldos debe ser > 0 tras mig 23")

	// Verificar al menos 1 fila de changelog para este cargoID.
	var changelogCount int
	err = pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID,
	).Scan(&changelogCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, changelogCount, 1,
		"MSP_SALDOS_CHANGELOG debe tener al menos 1 fila para cargoID")

	// Verificar que TX_ID del changelog coincide con el del caché.
	var changelogTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_SALDOS_CHANGELOG
		  WHERE DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, cargoID,
	).Scan(&changelogTxID)
	require.NoError(t, err, "debe existir fila en MSP_SALDOS_CHANGELOG")
	assert.Equal(t, cacheTxID, changelogTxID,
		"TX_ID del changelog de saldos debe coincidir con TX_ID del caché")

	// Cleanup del changelog.
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID)
	})

	t.Logf("Mig23 saldo normal: cargoID=%d cacheTxID=%d changelogTxID=%d count=%d",
		cargoID, cacheTxID, changelogTxID, changelogCount)
}

// ─── TestMig23_NoChangelogInWhenAnyDo_Structural ─────────────────────────────

// TestMig23_NoChangelogInWhenAnyDo_Structural verifica estructuralmente que el
// INSERT al changelog NO aparezca dentro del bloque WHEN ANY DO de ninguno de
// los dos procedures.  Es la guardia de atomicidad: un changelog INSERT dentro
// del error handler significaría que se registraría un cambio incluso cuando el
// upsert falló.
//
//nolint:paralleltest
func TestMig23_NoChangelogInWhenAnyDo_Structural(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	tests := []struct {
		procName       string
		changelogTable string
	}{
		{"MSP_RECOMPUTE_PAGO", "MSP_PAGOS_CHANGELOG"},
		{"MSP_RECOMPUTE_SALDO_VENTA", "MSP_SALDOS_CHANGELOG"},
	}

	for _, tc := range tests {
		t.Run(tc.procName, func(t *testing.T) {
			var src string
			err := pool.QueryRowContext(ctx,
				`SELECT FIRST 1 COALESCE(RDB$PROCEDURE_SOURCE, '')
				   FROM RDB$PROCEDURES
				  WHERE RDB$PROCEDURE_NAME = ?`, tc.procName,
			).Scan(&src)
			require.NoErrorf(t, err, "debe poder leer RDB$PROCEDURE_SOURCE para %s", tc.procName)
			require.NotEmpty(t, src, "RDB$PROCEDURE_SOURCE no debe estar vacío para %s", tc.procName)

			// Normalizar espacios para comparación.
			upperSrc := strings.ToUpper(src)

			// Encontrar la posición de WHEN ANY DO.
			whenIdx := strings.Index(upperSrc, "WHEN ANY DO")
			require.NotEqual(t, -1, whenIdx,
				"%s debe contener un bloque WHEN ANY DO", tc.procName)

			// Extraer el texto a partir de WHEN ANY DO.
			afterWhen := upperSrc[whenIdx:]

			// Verificar que el INSERT al changelog no aparezca en ese bloque.
			changelogInsert := "INSERT INTO " + tc.changelogTable
			assert.NotContains(t, afterWhen, changelogInsert,
				"%s: INSERT INTO %s NO debe aparecer dentro del bloque WHEN ANY DO — "+
					"el changelog solo se escribe en paths de éxito (atomicidad rollback-safe)",
				tc.procName, tc.changelogTable)
		})
	}
}

// ─── TestMig23_TxIdMatchesCurrentTransaction_CrossConnection ─────────────────

// TestMig23_TxIdMatchesCurrentTransaction_CrossConnection verifica que el
// TX_ID grabado en MSP_PAGOS_CHANGELOG sea el ID de la transacción llamante
// (no el de una transacción interna del procedure).  Para ello, abre una
// conexión directa a Firebird, obtiene CAST(CURRENT_TRANSACTION AS BIGINT)
// dentro de la misma tx antes del INSERT, confirma, y compara con el TX_ID
// grabado por el procedure.  Usa CURRENT_TRANSACTION keyword (Firebird 2.1+)
// en lugar de RDB$GET_CONTEXT que requiere Firebird 3.0+.
//
//nolint:paralleltest
func TestMig23_TxIdMatchesCurrentTransaction_CrossConnection(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	clienteID, _ := seedZonedClienteFromPool(t, pool)

	// Obtener IDs para el fixture fuera de la tx principal.
	var cargoID, cargoImpteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID))
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoImpteID))
	var impteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	folio := fmt.Sprintf("M3X%X", time.Now().UnixNano()&0xFFFFFF)
	now := time.Now()

	// Abrir conexión A explícita (conn directa al pool).
	connA, err := pool.Conn(ctx)
	require.NoError(t, err, "abrir conexión A")
	defer connA.Close()

	// Iniciar tx en conexión A.
	txA, err := connA.BeginTx(ctx, nil)
	require.NoError(t, err, "begin tx en conexión A")

	// Leer CURRENT_TRANSACTION dentro de txA — este es el TX_ID que el procedure
	// debe grabar.  CURRENT_TRANSACTION es un pseudo-registro PSQL disponible
	// también en SQL DML (Firebird 2.1+); diferente de
	// RDB$GET_CONTEXT('SYSTEM','CURRENT_TRANSACTION') que es Firebird 3.0+.
	var txIDA int64
	err = txA.QueryRowContext(ctx,
		`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
	).Scan(&txIDA)
	require.NoError(t, err, "leer CURRENT_TRANSACTION en txA")
	require.Positive(t, txIDA, "txIDA debe ser > 0")

	// INSERT DOCTOS_CC dentro de txA.
	_, err = txA.ExecContext(ctx,
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, 87327, ?, 'C',
		        225490, ?, ?, '0001',
		        1, 'Mig23 CrossConn fixture',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		cargoID, folio, now, clienteID)
	require.NoError(t, err, "INSERT DOCTOS_CC en txA")

	// INSERT IMPORTES_DOCTOS_CC cargo dentro de txA.
	_, err = txA.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'C', NULL, 2000, 0, 'N', 'N', 'N')`,
		cargoImpteID, cargoID, now)
	require.NoError(t, err, "INSERT IMPORTES_DOCTOS_CC cargo en txA")

	// INSERT IMPORTES_DOCTOS_CC pago dentro de txA — dispara MSP_RECOMPUTE_PAGO.
	_, err = txA.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, 400, 0, 'N', 'N', 'N')`,
		impteID, cargoID, now, cargoID)
	require.NoError(t, err, "INSERT IMPORTES_DOCTOS_CC pago en txA")

	// COMMIT txA — el changelog INSERT y el POST_EVENT se confirman aquí.
	require.NoError(t, txA.Commit(), "commit txA")

	// Registrar cleanup tras el commit.
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, cargoImpteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
	})

	// Conexión B: leer el TX_ID grabado en el changelog.
	var txIDRecorded int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_PAGOS_CHANGELOG
		  WHERE IMPTE_DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, impteID,
	).Scan(&txIDRecorded)
	require.NoError(t, err, "debe existir fila en MSP_PAGOS_CHANGELOG tras el commit")

	assert.Equal(t, txIDA, txIDRecorded,
		"TX_ID en el changelog debe coincidir con CURRENT_TRANSACTION de la tx llamante")

	t.Logf("Mig23 cross-conn: impteID=%d txIDA=%d txIDRecorded=%d",
		impteID, txIDA, txIDRecorded)
}

// ─── helpers locales ─────────────────────────────────────────────────────────

// insertCommittedCargoForSaldos es alias de insertCommittedCargo que también
// registra cleanup de MSP_SALDOS_VENTAS y MSP_SALDOS_CHANGELOG.
func insertCommittedCargoForSaldos(t *testing.T, pool *firebird.Pool, clienteID int, folio string, importe decimal.Decimal) int {
	t.Helper()
	ctx := context.Background()
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, importe)
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID)
	})
	return cargoID
}
