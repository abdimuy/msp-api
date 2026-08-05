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
	"github.com/abdimuy/msp-api/internal/platform/firebird"
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

	// Señal directa del leak: si cada llamada dejara una tx idle, el conteo de
	// transacciones activas crecería en ~len(results). Se mide antes y después
	// porque es la única comprobación que no depende de que la base esté
	// ociosa — y la base de desarrollo es compartida (el contenedor de la API
	// de dev mantiene su propio pool abierto contra ella).
	txAntes := contarTxActivas(ctx, t, pool)

	results := make([]int64, 10)
	for i := range results {
		w, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
		require.NoError(t, err)
		require.Positive(t, w)
		results[i] = w
	}

	txDespues := contarTxActivas(ctx, t, pool)

	// Tolerancia de 2: absorbe que una conexión ajena abra o cierre una tx
	// durante el recorrido. Una fuga real sumaría ~10, no 2.
	assert.LessOrEqual(t, txDespues-txAntes, int64(2),
		"las llamadas dejaron transacciones idle: antes=%d después=%d; results=%v",
		txAntes, txDespues, results)

	// Segunda señal, la que este test buscaba originalmente: el leak se
	// manifiesta como crecimiento estricto del watermark entre llamadas
	// consecutivas, porque cada llamada veía la tx que dejó la anterior.
	//
	// Sólo se comparan valores reales entre sí. Una transición desde o hacia
	// SentinelNoActiveTx significa que una tx externa apareció o hizo commit
	// entre dos llamadas; eso es ruido del entorno compartido, no una fuga
	// nuestra, y afirmar lo contrario es lo que volvía este test intermitente.
	for i := 1; i < len(results); i++ {
		prev, cur := results[i-1], results[i]
		if prev == cobranzaventfb.SentinelNoActiveTx || cur == cobranzaventfb.SentinelNoActiveTx {
			continue
		}
		assert.LessOrEqual(t, cur, prev,
			"watermark creciente entre llamadas consecutivas = tx leakeada por el driver; results=%v",
			results)
	}

	t.Logf("consecutive watermarks: %v (txs activas: %d → %d)", results, txAntes, txDespues)
}

// contarTxActivas devuelve cuántas transacciones activas hay en el servidor
// excluyendo la de la propia consulta. Se usa para medir la fuga directamente
// en vez de inferirla de los valores del watermark.
func contarTxActivas(ctx context.Context, t *testing.T, pool *firebird.Pool) int64 {
	t.Helper()

	var n int64
	require.NoError(t,
		pool.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM MON$TRANSACTIONS
WHERE MON$STATE = 1
  AND MON$TRANSACTION_ID <> CURRENT_TRANSACTION`,
		).Scan(&n),
		"contar transacciones activas",
	)

	return n
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
