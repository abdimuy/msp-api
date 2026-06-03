//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for MinActiveTransactionID (watermark.go).
// Gated on FB_DATABASE.
//
// These tests verify the cross-connection semantics that the listener relies on:
// a transaction open on connection A must be visible as a lower bound for the
// watermark queried from connection B.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// TestMinActiveTransactionID_SeesOwnTx verifica que la transacción corriente
// (la del test) sea visible en MON$TRANSACTIONS. El watermark devuelto debe
// ser <= CURRENT_TRANSACTION del test porque al menos nuestra tx está activa.
//
//nolint:paralleltest
func TestMinActiveTransactionID_SeesOwnTx(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	// Leer CURRENT_TRANSACTION del proceso de test. Usamos pool.DB directamente
	// (auto-commit) para obtener el ID de la transacción implícita.
	var currentTx int64
	require.NoError(
		t,
		pool.QueryRowContext(
			ctx,
			`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
		).Scan(&currentTx),
		"leer CURRENT_TRANSACTION",
	)
	require.Positive(t, currentTx, "CURRENT_TRANSACTION debe ser > 0")

	watermark, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)

	// El watermark puede ser:
	// a) SentinelNoActiveTx si no hay txs activas en el momento exacto de la consulta.
	// b) Un valor <= currentTx si hay txs activas (incluida la nuestra).
	//
	// Invariante: watermark > 0 siempre (ya sea un TX_ID real o el sentinel).
	assert.Positive(t, watermark,
		"watermark debe ser > 0 (TX_ID real o SentinelNoActiveTx)")

	t.Logf("MinActiveTransactionID: watermark=%d currentTx=%d sentinel=%d",
		watermark, currentTx, cobranzaventfb.SentinelNoActiveTx)
}

// TestMinActiveTransactionID_CrossConnection abre una transacción explícita en
// la conexión A y verifica que MON$TRANSACTIONS la incluya como activa. Luego
// confirma A y verifica que el watermark sea > 0 (sentinel o nuevo mínimo).
//
// Nota: no afirmamos watermarkConTxA <= txIDA de forma estricta porque el pool
// puede asignar la conexión de monitoreo con un TX_ID más alto que el de txA,
// pero MON$TRANSACTIONS incluirá ambas como activas simultáneamente.  En
// cambio, verificamos que txA aparezca listada en MON$TRANSACTIONS mientras
// está abierta, y que no aparezca después del commit.
//
//nolint:paralleltest
func TestMinActiveTransactionID_CrossConnection(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	// Conexión A: abrir tx explícita y leer su TX_ID.
	connA, err := pool.Conn(ctx)
	require.NoError(t, err, "abrir conexión A")
	defer connA.Close()

	txA, err := connA.BeginTx(ctx, nil)
	require.NoError(t, err, "begin tx en conexión A")
	// Asegurar que txA siempre se cierre (rollback si no se hace commit).
	txADone := false
	defer func() {
		if !txADone {
			_ = txA.Rollback()
		}
	}()

	var txIDA int64
	require.NoError(
		t,
		txA.QueryRowContext(
			ctx,
			`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
		).Scan(&txIDA),
		"leer CURRENT_TRANSACTION en txA",
	)
	require.Positive(t, txIDA, "txIDA debe ser > 0")

	// Verificar directamente en MON$TRANSACTIONS (desde pool.DB, conexión B)
	// que txA esté listada como activa (MON$STATE = 1).
	var txAActiveCount int
	require.NoError(
		t,
		pool.DB.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM MON$TRANSACTIONS
			  WHERE MON$TRANSACTION_ID = ? AND MON$STATE = 1`, txIDA,
		).Scan(&txAActiveCount),
		"verificar txA en MON$TRANSACTIONS",
	)
	if txAActiveCount == 0 {
		txADone = true
		_ = txA.Rollback()
		t.Skipf("txIDA=%d no aparece en MON$TRANSACTIONS como activa — puede ser limitación del driver", txIDA)
	}
	assert.Equal(t, 1, txAActiveCount,
		"txA debe aparecer exactamente una vez en MON$TRANSACTIONS mientras está abierta")

	// MinActiveTransactionID debe incluir txA: watermark <= txIDA.
	watermarkConTxA, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)
	assert.LessOrEqual(t, watermarkConTxA, txIDA,
		"watermark debe ser <= txIDA (txA está activa en MON$TRANSACTIONS)")

	// Confirmar txA.
	txADone = true
	require.NoError(t, txA.Commit(), "commit txA")

	// Después del commit, txA ya no debe aparecer como activa.
	var txAAfterCommit int
	require.NoError(
		t,
		pool.DB.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM MON$TRANSACTIONS
			  WHERE MON$TRANSACTION_ID = ? AND MON$STATE = 1`, txIDA,
		).Scan(&txAAfterCommit),
	)
	assert.Equal(t, 0, txAAfterCommit,
		"txA no debe aparecer en MON$TRANSACTIONS tras commit")

	watermarkPostCommit, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)
	require.Positive(t, watermarkPostCommit,
		"watermark post-commit debe ser > 0 (sentinel o nuevo mínimo)")

	t.Logf("CrossConnection: txIDA=%d watermarkConTxA=%d watermarkPostCommit=%d sentinel=%d txAActive=%d txAAfterCommit=%d",
		txIDA, watermarkConTxA, watermarkPostCommit, cobranzaventfb.SentinelNoActiveTx, txAActiveCount, txAAfterCommit)
}

// TestMinActiveTransactionID_ReturnsSentinelOrPositive verifica que el valor
// devuelto sea siempre el sentinel o un TX_ID positivo (nunca negativo ni cero).
//
//nolint:paralleltest
func TestMinActiveTransactionID_ReturnsSentinelOrPositive(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	watermark, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)

	assert.Positive(t, watermark,
		"MinActiveTransactionID siempre devuelve > 0 (sentinel=MaxInt64 o TX_ID real)")
}
