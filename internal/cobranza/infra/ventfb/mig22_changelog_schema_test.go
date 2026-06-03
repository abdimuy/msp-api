//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for migration 000022: MSP_PAGOS_CHANGELOG,
// MSP_SALDOS_CHANGELOG, generators, and TX_ID columns on cache tables.
//
// These tests are pure schema reads — no DDL, no writes, no transactions
// beyond the connection itself.  They use the shared Firebird pool and skip
// when FB_DATABASE is not set (requireFBEnv).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// requireMigration000022 skips the test when migration 000022 has not been
// applied to the database.
func requireMigration000022(t *testing.T) {
	t.Helper()
	pool := fbtestutil.NewTestFirebirdPool(t)
	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM MSP_MIGRATIONS WHERE ID = 22`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000022 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// TestMig22_ChangelogTablesExist verifica que ambas tablas changelog existan
// en el catálogo de Firebird.
//
//nolint:paralleltest
func TestMig22_ChangelogTablesExist(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$RELATIONS
		  WHERE RDB$RELATION_NAME IN ('MSP_PAGOS_CHANGELOG', 'MSP_SALDOS_CHANGELOG')`,
	).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "both changelog tables must exist in RDB$RELATIONS")
}

// TestMig22_ChangelogColumns verifica que cada tabla changelog tenga las
// columnas esperadas con los tipos correctos.
//
//nolint:paralleltest
func TestMig22_ChangelogColumns(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	// Firebird field types: 16 = INT64 (BIGINT), 8 = INTEGER, 35 = TIMESTAMP.
	type colCheck struct {
		name     string
		typeCode int // RDB$FIELD_TYPE from RDB$FIELDS join
	}

	tables := []struct {
		table   string
		pkCol   string
		columns []colCheck
	}{
		{
			table: "MSP_PAGOS_CHANGELOG",
			pkCol: "IMPTE_DOCTO_CC_ID",
			columns: []colCheck{
				{"SEQ_ID", 16},
				{"IMPTE_DOCTO_CC_ID", 8},
				{"TX_ID", 16},
				{"COMMIT_AT", 35},
			},
		},
		{
			table: "MSP_SALDOS_CHANGELOG",
			pkCol: "DOCTO_CC_ID",
			columns: []colCheck{
				{"SEQ_ID", 16},
				{"DOCTO_CC_ID", 8},
				{"TX_ID", 16},
				{"COMMIT_AT", 35},
			},
		},
	}

	for _, tbl := range tables {
		t.Run(tbl.table, func(t *testing.T) {
			for _, col := range tbl.columns {
				var typeCode int
				err := pool.QueryRowContext(ctx,
					`SELECT F.RDB$FIELD_TYPE
					   FROM RDB$RELATION_FIELDS RF
					   JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
					  WHERE RF.RDB$RELATION_NAME = ?
					    AND RF.RDB$FIELD_NAME    = ?`,
					tbl.table, col.name,
				).Scan(&typeCode)
				require.NoErrorf(t, err, "%s.%s must exist in RDB$RELATION_FIELDS", tbl.table, col.name)
				assert.Equalf(t, col.typeCode, typeCode,
					"%s.%s: expected RDB$FIELD_TYPE=%d (got %d)",
					tbl.table, col.name, col.typeCode, typeCode)
			}
		})
	}
}

// TestMig22_GeneratorsExist verifica que los dos generadores dedicados existan.
//
//nolint:paralleltest
func TestMig22_GeneratorsExist(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$GENERATORS
		  WHERE RDB$GENERATOR_NAME IN (
		    'GEN_MSP_PAGOS_CHANGELOG_SEQ',
		    'GEN_MSP_SALDOS_CHANGELOG_SEQ'
		  )`,
	).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "both changelog generators must exist in RDB$GENERATORS")
}

// TestMig22_TxIdColumnOnCacheTables verifica que TX_ID exista en ambas tablas
// caché con tipo BIGINT (RDB$FIELD_TYPE = 16).
//
//nolint:paralleltest
func TestMig22_TxIdColumnOnCacheTables(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	for _, tbl := range []string{"MSP_PAGOS_VENTAS", "MSP_SALDOS_VENTAS"} {
		t.Run(tbl, func(t *testing.T) {
			var typeCode int
			err := pool.QueryRowContext(ctx,
				`SELECT F.RDB$FIELD_TYPE
				   FROM RDB$RELATION_FIELDS RF
				   JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
				  WHERE RF.RDB$RELATION_NAME = ?
				    AND RF.RDB$FIELD_NAME    = 'TX_ID'`,
				tbl,
			).Scan(&typeCode)
			require.NoErrorf(t, err, "%s.TX_ID must exist", tbl)
			// RDB$FIELD_TYPE 16 = INT64 / BIGINT in Firebird.
			assert.Equalf(t, 16, typeCode, "%s.TX_ID must be BIGINT (type 16)", tbl)
		})
	}
}

// TestMig22_TxIdColumnIsNonNegative verifica que la columna TX_ID exista en
// los cachés y que todas las filas tengan TX_ID >= 0.
//
// Reemplaza la aserción original (TX_ID == 0 para todas las filas) que era
// válida inmediatamente después de aplicar mig 22, antes de que mig 23/24
// empezaran a escribir TX_IDs reales mediante RDB$GET_CONTEXT en los
// procedimientos de recompute y tombstone. Con el DB de desarrollo activo,
// los triggers ya han llenado TX_ID con valores positivos en muchas filas.
// El invariante correcto es: TX_ID >= 0 (DEFAULT 0 para filas pre-mig22;
// TX_ID positivo para filas escritas desde mig 23 en adelante).
//
//nolint:paralleltest
func TestMig22_TxIdColumnIsNonNegative(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	for _, tbl := range []string{"MSP_PAGOS_VENTAS", "MSP_SALDOS_VENTAS"} {
		t.Run(tbl, func(t *testing.T) {
			var negative int
			err := pool.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM `+tbl+` WHERE TX_ID < 0`,
			).Scan(&negative)
			require.NoErrorf(t, err, "querying %s.TX_ID for negative values", tbl)
			assert.Zerof(t, negative,
				"%s: %d rows have TX_ID < 0; TX_ID must always be >= 0 (DEFAULT 0 or committed TX_ID)",
				tbl, negative)
		})
	}
}

// TestMig25_TxIdIndicesOnCacheTables verifica que existan los índices sobre
// TX_ID en las tablas caché — agregados por la migración 000025 (commit 7
// del sprint). El test anterior (TestMig22_TxIdIndexNotPresentOnCacheTables)
// afirmaba que NO existían; ahora que mig 25 está aplicada, afirmamos
// que SÍ existen.
//
//nolint:paralleltest
func TestMig25_TxIdIndicesOnCacheTables(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	var n int
	err := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$INDICES
		  WHERE RDB$INDEX_NAME LIKE '%TX_ID%'
		    AND RDB$RELATION_NAME IN ('MSP_PAGOS_VENTAS', 'MSP_SALDOS_VENTAS')`,
	).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 2, n,
		"IDX_MSP_PAGOS_VENTAS_TX_ID and IDX_MSP_SALDOS_VENTAS_TX_ID must exist (mig 25)")
}
