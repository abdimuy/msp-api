//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for migration 000024: tombstone triggers (DELETING branches
// of MSP_PAGOS_IMPORTES_AIUD and MSP_SALDOS_DOCTOS_CC_AD) now write changelog
// rows and set TX_ID in the cache.
//
// These tests require FB_DATABASE to be set (requireFBEnv) and migrations
// 000022, 000023, and 000024 to be applied.  They follow the committed-fixture
// pattern from digest_query_integration_test.go: direct pool INSERTs (auto-
// commit) so that trigger-driven writes are visible to independent connections.

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
)

// requireMigration000024 skips the test when migration 000024 has not been
// applied to the database.
func requireMigration000024(t *testing.T) {
	t.Helper()
	pool := fbtestutil.NewTestFirebirdPool(t)
	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM MSP_MIGRATIONS WHERE ID = 24`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000024 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// ─── TestMig24_PagosTombstone_DeleteFiresChangelog ───────────────────────────

// TestMig24_PagosTombstone_DeleteFiresChangelog verifica que al hacer DELETE de
// una fila de IMPORTES_DOCTOS_CC (pago), la rama DELETING del trigger
// MSP_PAGOS_IMPORTES_AIUD actualice el caché con CANCELADO='S' + TX_ID > 0 y
// escriba una fila extra en MSP_PAGOS_CHANGELOG.
//
//nolint:paralleltest
func TestMig24_PagosTombstone_DeleteFiresChangelog(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)
	requireMigration000024(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	clienteID, _ := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("M4P%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("2000.00"))
	impteID := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("500.00"))
	requirePagosVentasCacheRow(t, pool, impteID)

	ctx := context.Background()

	// Verificar que tras la inserción ya existe al menos 1 fila de changelog
	// (generada por MSP_RECOMPUTE_PAGO en mig 23).
	var countBefore int
	err := pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&countBefore)
	require.NoError(t, err)
	require.GreaterOrEqual(t, countBefore, 1,
		"debe existir al menos 1 fila de changelog antes del DELETE (mig 23 recompute)")

	// DELETE la fila del importe — dispara la rama DELETING del trigger.
	_, err = pool.ExecContext(ctx,
		`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC debe ejecutarse")

	// Verificar estado del caché: CANCELADO='S', IMPORTE=0, TX_ID > 0.
	var cancelado string
	var importe float64
	var cacheTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT CANCELADO, IMPORTE, TX_ID FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
		impteID,
	).Scan(&cancelado, &importe, &cacheTxID)
	require.NoError(t, err, "fila de caché debe existir tras tombstone por DELETE")
	assert.Equal(t, "S", strings.TrimSpace(cancelado), "CANCELADO debe ser 'S' tras DELETE")
	assert.InDelta(t, 0.0, importe, 1e-9, "IMPORTE debe ser 0 tras DELETE")
	assert.Positive(t, cacheTxID, "TX_ID en caché debe ser > 0 tras mig 24")

	// Verificar que el changelog tiene una fila más que antes.
	var countAfter int
	err = pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID,
	).Scan(&countAfter)
	require.NoError(t, err)
	assert.Equal(t, countBefore+1, countAfter,
		"DELETE debe agregar exactamente 1 fila de changelog")

	// Verificar que el TX_ID del changelog más reciente coincide con el del caché.
	var changelogTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_PAGOS_CHANGELOG
		  WHERE IMPTE_DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, impteID,
	).Scan(&changelogTxID)
	require.NoError(t, err, "debe existir fila en MSP_PAGOS_CHANGELOG")
	assert.Equal(t, cacheTxID, changelogTxID,
		"TX_ID del changelog más reciente debe coincidir con TX_ID del caché")

	// Cleanup: el DELETE ya eliminó la fila de IMPORTES_DOCTOS_CC; limpiar
	// el changelog y el caché (el cleanup de insertCommittedPago intenta borrar
	// IMPORTES_DOCTOS_CC de nuevo, lo cual fallará silenciosamente — está bien).
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})

	t.Logf("Mig24 pagos tombstone DELETE: impteID=%d cacheTxID=%d changelogTxID=%d before=%d after=%d",
		impteID, cacheTxID, changelogTxID, countBefore, countAfter)
}

// ─── TestMig24_SaldosTombstone_DeleteCargoFiresChangelog ─────────────────────

// TestMig24_SaldosTombstone_DeleteCargoFiresChangelog verifica que al hacer
// DELETE de una fila DOCTOS_CC con NATURALEZA_CONCEPTO='C', la rama DELETING
// del trigger MSP_SALDOS_DOCTOS_CC_AD actualice el caché con
// CARGO_CANCELADO='S' + TX_ID > 0 y escriba una fila en MSP_SALDOS_CHANGELOG.
//
//nolint:paralleltest
func TestMig24_SaldosTombstone_DeleteCargoFiresChangelog(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)
	requireMigration000024(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	clienteID, _ := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("M4S%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("1500.00"))
	requireSaldosCacheRow(t, pool, cargoID)

	ctx := context.Background()

	// Contar filas de changelog antes del DELETE (mig 23 ya habrá escrito 1).
	var countBefore int
	err := pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID,
	).Scan(&countBefore)
	require.NoError(t, err)
	require.GreaterOrEqual(t, countBefore, 1,
		"debe existir al menos 1 fila de changelog antes del DELETE (mig 23)")

	// DELETE IMPORTES_DOCTOS_CC del cargo primero (FK: IMPORTES_DOCTOS_CC.DOCTO_CC_ID
	// referencia DOCTOS_CC; se eliminan los importes para poder borrar el cargo).
	_, err = pool.ExecContext(ctx,
		`DELETE FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
	require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC del cargo")

	// DELETE DOCTOS_CC — dispara la rama DELETING de MSP_SALDOS_DOCTOS_CC_AD.
	_, err = pool.ExecContext(ctx,
		`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
	require.NoError(t, err, "DELETE DOCTOS_CC debe ejecutarse")

	// Verificar caché: CARGO_CANCELADO='S', TX_ID > 0.
	var cargoCancelado string
	var cacheTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT CARGO_CANCELADO, TX_ID FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`,
		cargoID,
	).Scan(&cargoCancelado, &cacheTxID)
	require.NoError(t, err, "fila de caché debe existir tras tombstone por DELETE")
	assert.Equal(t, "S", strings.TrimSpace(cargoCancelado),
		"CARGO_CANCELADO debe ser 'S' tras DELETE de DOCTOS_CC")
	assert.Positive(t, cacheTxID, "TX_ID en caché de saldos debe ser > 0 tras mig 24")

	// Verificar que el changelog tiene al menos una fila más que antes.
	// Nota: el DELETE de IMPORTES_DOCTOS_CC también dispara el trigger
	// MSP_SALDOS_IMPORTES_AIUD (mig10) que llama MSP_RECOMPUTE_SALDO_VENTA,
	// el cual a su vez escribe otra fila de changelog (mig23).  Por eso
	// puede haber más de 1 fila nueva — lo que importa es que haya al menos
	// 1 y que el TX_ID del más reciente coincida con el del caché.
	var countAfter int
	err = pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID,
	).Scan(&countAfter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, countAfter, countBefore+1,
		"DELETE debe agregar al menos 1 fila al MSP_SALDOS_CHANGELOG")

	// Verificar que el TX_ID del changelog más reciente coincide con el del caché.
	var changelogTxID int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_SALDOS_CHANGELOG
		  WHERE DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, cargoID,
	).Scan(&changelogTxID)
	require.NoError(t, err, "debe existir fila en MSP_SALDOS_CHANGELOG tras DELETE")
	assert.Equal(t, cacheTxID, changelogTxID,
		"TX_ID del changelog de saldos debe coincidir con TX_ID del caché")

	// Cleanup: DOCTOS_CC ya fue borrado; limpiar caché y changelog.
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID)
	})

	t.Logf("Mig24 saldos tombstone DELETE: cargoID=%d cacheTxID=%d changelogTxID=%d before=%d after=%d (added %d)",
		cargoID, cacheTxID, changelogTxID, countBefore, countAfter, countAfter-countBefore)
}

// ─── TestMig24_Structural_NoChangelogInWhenAnyDo ─────────────────────────────

// TestMig24_Structural_NoChangelogInWhenAnyDo verifica estructuralmente que el
// INSERT al changelog NO aparezca dentro del bloque WHEN ANY DO de ninguno de
// los dos triggers.  El changelog solo se escribe en el path de éxito —
// atomicidad rollback-safe.
//
//nolint:paralleltest
func TestMig24_Structural_NoChangelogInWhenAnyDo(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)
	requireMigration000024(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	tests := []struct {
		triggerName    string
		changelogTable string
	}{
		{"MSP_PAGOS_IMPORTES_AIUD", "MSP_PAGOS_CHANGELOG"},
		{"MSP_SALDOS_DOCTOS_CC_AD", "MSP_SALDOS_CHANGELOG"},
	}

	for _, tc := range tests {
		t.Run(tc.triggerName, func(t *testing.T) {
			var src string
			err := pool.QueryRowContext(ctx,
				`SELECT FIRST 1 COALESCE(RDB$TRIGGER_SOURCE, '')
				   FROM RDB$TRIGGERS
				  WHERE RDB$TRIGGER_NAME = ?`, tc.triggerName,
			).Scan(&src)
			require.NoErrorf(t, err,
				"debe poder leer RDB$TRIGGER_SOURCE para %s", tc.triggerName)
			require.NotEmpty(t, src,
				"RDB$TRIGGER_SOURCE no debe estar vacío para %s", tc.triggerName)

			// Normalizar para comparación case-insensitive.
			upperSrc := strings.ToUpper(src)

			// Encontrar la posición de WHEN ANY DO.
			whenIdx := strings.Index(upperSrc, "WHEN ANY DO")
			require.NotEqual(t, -1, whenIdx,
				"%s debe contener un bloque WHEN ANY DO", tc.triggerName)

			// Extraer el texto a partir de WHEN ANY DO.
			afterWhen := upperSrc[whenIdx:]

			// El INSERT al changelog NO debe aparecer después del WHEN ANY DO.
			changelogInsert := "INSERT INTO " + tc.changelogTable
			assert.NotContains(t, afterWhen, changelogInsert,
				"%s: INSERT INTO %s NO debe aparecer dentro del bloque WHEN ANY DO — "+
					"el changelog solo se escribe en paths de éxito (atomicidad rollback-safe)",
				tc.triggerName, tc.changelogTable)
		})
	}
}

// ─── TestMig24_TxIdMatchesCommittingTransaction_CrossConnection ───────────────

// TestMig24_TxIdMatchesCommittingTransaction_CrossConnection verifica que el
// TX_ID grabado en MSP_PAGOS_CHANGELOG por la rama DELETING del trigger sea el
// ID de la transacción que hizo el DELETE, no el de una transacción interna.
//
// Conexión A: BEGIN tx → captura CURRENT_TRANSACTION → INSERT cargo + pago →
// DELETE pago (trigger escribe changelog) → COMMIT.
// Conexión B: lee MSP_PAGOS_CHANGELOG y compara TX_ID grabado con el capturado.
//
//nolint:paralleltest
func TestMig24_TxIdMatchesCommittingTransaction_CrossConnection(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	requireMigration000023(t)
	requireMigration000024(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	clienteID, _ := seedZonedClienteFromPool(t, pool)

	// Reservar IDs fuera de la tx principal (GEN_ID es auto-commit).
	var cargoID, cargoImpteID, impteID int
	require.NoError(t,
		pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID))
	require.NoError(t,
		pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoImpteID))
	require.NoError(t,
		pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	folio := fmt.Sprintf("M4X%X", time.Now().UnixNano()&0xFFFFFF)
	now := time.Now()

	// Abrir conexión A explícita.
	connA, err := pool.Conn(ctx)
	require.NoError(t, err, "abrir conexión A")
	defer connA.Close()

	txA, err := connA.BeginTx(ctx, nil)
	require.NoError(t, err, "begin tx en conexión A")

	// Capturar CURRENT_TRANSACTION dentro de txA — este es el TX_ID que el
	// trigger debe grabar en el changelog al hacer el DELETE.
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
		        1, 'Mig24 CrossConn fixture',
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

	// INSERT IMPORTES_DOCTOS_CC pago dentro de txA — dispara MSP_RECOMPUTE_PAGO
	// (rama INSERT).
	_, err = txA.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, 400, 0, 'N', 'N', 'N')`,
		impteID, cargoID, now, cargoID)
	require.NoError(t, err, "INSERT IMPORTES_DOCTOS_CC pago en txA")

	// DELETE el pago dentro de txA — dispara la rama DELETING del trigger.
	_, err = txA.ExecContext(ctx,
		`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC pago en txA")

	// COMMIT txA — el INSERT al changelog y el POST_EVENT se confirman aquí.
	require.NoError(t, txA.Commit(), "commit txA")

	// Registrar cleanup tras el commit.
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, cargoImpteID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID)
	})

	// Conexión B: leer el TX_ID del changelog más reciente para este impteID.
	// El DELETE genera la última fila — debe tener TX_ID = txIDA.
	var txIDRecorded int64
	err = pool.QueryRowContext(ctx,
		`SELECT FIRST 1 TX_ID FROM MSP_PAGOS_CHANGELOG
		  WHERE IMPTE_DOCTO_CC_ID = ?
		  ORDER BY SEQ_ID DESC`, impteID,
	).Scan(&txIDRecorded)
	require.NoError(t, err,
		"debe existir fila en MSP_PAGOS_CHANGELOG tras el commit con DELETE")

	assert.Equal(t, txIDA, txIDRecorded,
		"TX_ID en el changelog del DELETE debe coincidir con CURRENT_TRANSACTION de la tx llamante")

	t.Logf("Mig24 cross-conn: impteID=%d txIDA=%d txIDRecorded=%d",
		impteID, txIDA, txIDRecorded)
}
