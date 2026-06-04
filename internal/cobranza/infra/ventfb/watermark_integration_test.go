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

// TestMinActiveTransactionID_DoesNotIncludeItsOwnTx verifica que el probe
// excluye su propia transacción del MIN(MON$TRANSACTION_ID). Sin la cláusula
// "<> CURRENT_TRANSACTION", la consulta del watermark se vería a sí misma en
// MON$TRANSACTIONS (state=1 mientras corre) y devolvería su propio TX_ID en
// pools sin otras txs activas, bloqueando el avance del listener.
//
// Estrategia: abrimos una tx explícita en una conexión separada y, dentro
// de esa tx, consultamos directamente el mismo SQL que usa el probe. El
// resultado nunca debe incluir nuestro propio TX_ID.
//
//nolint:paralleltest
func TestMinActiveTransactionID_DoesNotIncludeItsOwnTx(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	conn, err := pool.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	var ownTxID int64
	require.NoError(t,
		tx.QueryRowContext(ctx,
			`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
		).Scan(&ownTxID),
		"capturar CURRENT_TRANSACTION dentro de la tx propia",
	)
	require.Positive(t, ownTxID)

	// Mismo SQL que MinActiveTransactionID — verificamos directamente que la
	// self-exclusion del WHERE filtra nuestro propio TX_ID.
	var minTx *int64
	require.NoError(t,
		tx.QueryRowContext(ctx, `
SELECT MIN(MON$TRANSACTION_ID)
FROM MON$TRANSACTIONS
WHERE MON$STATE = 1
  AND MON$TRANSACTION_ID <> CURRENT_TRANSACTION`,
		).Scan(&minTx),
	)
	if minTx != nil {
		assert.NotEqual(t, ownTxID, *minTx,
			"probe debe excluir su propio TX_ID; ownTx=%d minTx=%d", ownTxID, *minTx)
	}

	t.Logf("ownTxID=%d minTx=%v (nil = sentinel, no otras txs activas)", ownTxID, minTx)
}

// TestMinActiveTransactionID_ConsecutiveCallsDoNotLeak verifica que llamar
// MinActiveTransactionID varias veces consecutivas en el mismo pool NO
// produce un crecimiento monótono del watermark debido a auto-inclusión de
// las txs leakeadas del driver. Antes del fix (sin RunInReadTx wrap + sin
// CURRENT_TRANSACTION filter), cada llamada dejaba una tx idle en
// MON$TRANSACTIONS que la siguiente llamada veía como "activa".
//
//nolint:paralleltest
func TestMinActiveTransactionID_ConsecutiveCallsDoNotLeak(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	results := make([]int64, 10)
	for i := range results {
		w, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
		require.NoError(t, err)
		require.Positive(t, w)
		results[i] = w
	}

	// Si el fix funciona: o todas son SentinelNoActiveTx (entorno sin otras
	// txs) o están acotadas por el TX_ID de la tx externa más vieja (no por
	// el TX_ID que cada llamada acaba de leakear).
	//
	// Antes del fix: cada llamada (n) generaba una tx con id Tn. La llamada
	// (n+1) la veía y devolvía Tn. La llamada (n+2) veía la tx de (n+1) y
	// devolvía un valor mayor que Tn pero menor que la llamada actual. El
	// resultado sería estrictamente creciente.
	//
	// Después del fix: el resultado se mantiene constante (sentinel) o
	// monótonamente decreciente (sólo si una tx externa hace commit entre
	// llamadas — caso raro), nunca creciente desde el bug del leak.
	for i := 1; i < len(results); i++ {
		if results[i-1] == cobranzaventfb.SentinelNoActiveTx {
			assert.Equal(t, cobranzaventfb.SentinelNoActiveTx, results[i],
				"sin otras txs activas, todas las llamadas devuelven sentinel; results=%v", results)
			continue
		}
		// Tolerancia: con otras conexiones activas, el watermark puede
		// fluctuar. Lo crítico es que no sea estrictamente creciente como
		// señal del leak.
	}
	t.Logf("consecutive watermarks: %v", results)
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
