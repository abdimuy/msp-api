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
	"database/sql"
	"errors"
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

// ─────────────────────────────────────────────────────────────────────────────
// Ventanas de SÓLO LECTURA (defecto C1)
//
// Microsip es un conjunto de aplicaciones de escritorio Delphi: al abrir una
// pantalla abren una transacción y la sostienen mientras el usuario la tenga
// abierta — se midieron transacciones de 4 a 5 horas en operación normal. Como
// el watermark tomaba el MIN de TODAS las transacciones activas, cualquier
// empleado con una ventana abierta congelaba el sync de la flota completa: el
// 2026-08-17 dejaron de bajar 889 pagos.
//
// Esas ventanas son de sólo lectura, y una transacción que no puede escribir no
// aporta nada a la garantía que da el watermark. Estas pruebas fijan eso.
// ─────────────────────────────────────────────────────────────────────────────

// abrirVentanaSoloLectura reproduce en la base de desarrollo lo que hace una
// ventana de Microsip: una transacción de SÓLO LECTURA que queda abierta con un
// cursor vivo. Devuelve su TX_ID; el cierre queda registrado en t.Cleanup.
//
// El cursor no es decorativo. Firebird reporta MON$STATE = 1 (activa) sólo
// mientras la transacción sostiene un statement; una transacción abierta pero
// ociosa aparece con MON$STATE = 0 y no entra al MIN del watermark. Con un
// cursor a medio consumir el statement queda en estado 2 (STALLED) y la
// transacción en MON$STATE = 1 — exactamente la firma que se midió en
// producción.
//
// La serie recursiva evita depender de datos: no lee ninguna tabla de negocio,
// así que la prueba se comporta igual contra la base anonimizada de pruebas.
func abrirVentanaSoloLectura(ctx context.Context, t *testing.T, pool *firebird.Pool) int64 {
	t.Helper()

	conn, err := pool.Conn(ctx)
	require.NoError(t, err, "abrir conexión para la ventana de sólo lectura")
	t.Cleanup(func() { _ = conn.Close() })

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err, "abrir transacción de sólo lectura")
	t.Cleanup(func() { _ = tx.Rollback() })

	var txID int64
	require.NoError(t,
		tx.QueryRowContext(ctx,
			`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
		).Scan(&txID),
		"leer CURRENT_TRANSACTION de la ventana de sólo lectura",
	)
	require.Positive(t, txID)

	//nolint:sqlclosecheck // El cursor debe quedar ABIERTO: es lo que mantiene
	// la transacción en MON$STATE = 1. Se cierra en el t.Cleanup de abajo.
	rows, err := tx.QueryContext(ctx, `
WITH RECURSIVE serie(n) AS (
  SELECT 1 FROM RDB$DATABASE
  UNION ALL
  SELECT n + 1 FROM serie WHERE n < 100000)
SELECT n FROM serie`)
	require.NoError(t, err, "abrir el cursor que sostiene la ventana")
	t.Cleanup(func() { _ = rows.Close() })

	require.True(t, rows.Next(), "la serie recursiva debe devolver filas: %v", rows.Err())

	return txID
}

// leerMonTransaccion devuelve (MON$STATE, MON$READ_ONLY, encontrada) de una
// transacción. Se consulta dentro de RunInReadTx porque el snapshot de las
// tablas MON$ se congela al inicio de la transacción que las lee: reusar una
// conexión con transacción implícita viva daría una foto vieja.
func leerMonTransaccion(
	ctx context.Context, t *testing.T, pool *firebird.Pool, txID int64,
) (int, int, bool) {
	t.Helper()

	var estado, soloLectura int
	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		return q.QueryRowContext(ctx, `
SELECT MON$STATE, MON$READ_ONLY
FROM MON$TRANSACTIONS
WHERE MON$TRANSACTION_ID = ?`, txID,
		).Scan(&estado, &soloLectura)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false
	}
	require.NoError(t, err, "leer MON$TRANSACTIONS para tx=%d", txID)

	return estado, soloLectura, true
}

// requireVentanaVisible valida el montaje de la prueba antes de medir nada: si
// el driver no honró sql.TxOptions{ReadOnly: true}, o si la transacción no
// quedó activa, la prueba no está observando lo que cree y saltarla es más
// honesto que dar un verde falso.
func requireVentanaVisible(ctx context.Context, t *testing.T, pool *firebird.Pool, txID int64) {
	t.Helper()

	estado, soloLectura, encontrada := leerMonTransaccion(ctx, t, pool, txID)
	if !encontrada {
		t.Skipf("la ventana tx=%d no aparece en MON$TRANSACTIONS; el montaje no reproduce el escenario", txID)
	}
	if estado != 1 {
		t.Skipf("la ventana tx=%d no quedó activa (MON$STATE=%d); sin cursor vivo no entra al watermark", txID, estado)
	}
	if soloLectura != 1 {
		t.Skipf("el driver no honró ReadOnly: tx=%d tiene MON$READ_ONLY=%d", txID, soloLectura)
	}
}

// minTxEscrituraActiva devuelve el MIN(MON$TRANSACTION_ID) de las transacciones
// activas de ESCRITURA ajenas a la consulta, o nil si no hay ninguna. Sirve
// para detectar ruido: la base de desarrollo es compartida (el contenedor
// msp-api-dev mantiene su propio pool contra ella).
func minTxEscrituraActiva(ctx context.Context, t *testing.T, pool *firebird.Pool) *int64 {
	t.Helper()

	var minTx *int64
	require.NoError(t,
		firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
			q := firebird.GetQuerier(ctx, pool.DB)
			return q.QueryRowContext(ctx, `
SELECT MIN(MON$TRANSACTION_ID)
FROM MON$TRANSACTIONS
WHERE MON$STATE = 1
  AND MON$TRANSACTION_ID <> CURRENT_TRANSACTION
  AND MON$READ_ONLY = 0`,
			).Scan(&minTx)
		}),
		"medir la transacción de escritura activa más vieja",
	)

	return minTx
}

// TestMinActiveTransactionID_NuncaDevuelveUnaTxDeSoloLectura fija el invariante
// universal del watermark: el valor devuelto jamás puede ser una transacción de
// sólo lectura. La garantía que da el watermark es "ninguna fila entregada fue
// escrita por una transacción todavía en vuelo", y una transacción que no puede
// escribir no participa de esa garantía.
//
//nolint:paralleltest
func TestMinActiveTransactionID_NuncaDevuelveUnaTxDeSoloLectura(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	txIDRO := abrirVentanaSoloLectura(ctx, t, pool)
	requireVentanaVisible(ctx, t, pool, txIDRO)

	watermark, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)

	assert.NotEqual(t, txIDRO, watermark,
		"el watermark se quedó en la ventana de sólo lectura tx=%d", txIDRO)

	// Invariante universal: sea cual sea el mínimo devuelto, si es una
	// transacción real debe ser de escritura. Que la fila ya no exista es ruido
	// del entorno compartido (hizo commit entre ambas consultas), no un fallo.
	if watermark == cobranzaventfb.SentinelNoActiveTx {
		t.Logf("watermark = sentinel; ventana de sólo lectura tx=%d correctamente ignorada", txIDRO)

		return
	}

	_, soloLectura, encontrada := leerMonTransaccion(ctx, t, pool, watermark)
	if !encontrada {
		t.Logf("la tx del watermark (%d) ya no está activa; ruido del entorno compartido", watermark)

		return
	}
	assert.Equal(t, 0, soloLectura,
		"el watermark devolvió una transacción de sólo lectura: tx=%d", watermark)
}

// TestMinActiveTransactionID_VentanaDeSoloLecturaNoCongelaElSync reproduce el
// síntoma de producción del 2026-08-17: con una ventana de Microsip abierta,
// todo lo confirmado después de ella quedaba por encima del watermark y el sync
// dejaba de entregarlo (889 pagos que no bajaron a los teléfonos).
//
// La prueba demuestra lo contrario de forma determinista: una transacción
// confirmada DESPUÉS de abrir la ventana pasa el filtro "TX_ID < watermark".
//
//nolint:paralleltest
func TestMinActiveTransactionID_VentanaDeSoloLecturaNoCongelaElSync(t *testing.T) {
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	txIDRO := abrirVentanaSoloLectura(ctx, t, pool)
	requireVentanaVisible(ctx, t, pool, txIDRO)

	// Guarda anti-ruido: si ya hay una transacción de ESCRITURA ajena más vieja
	// que la ventana, ella marcaría el watermark y la medición no diría nada
	// sobre el defecto que perseguimos.
	if minEscritura := minTxEscrituraActiva(ctx, t, pool); minEscritura != nil && *minEscritura <= txIDRO {
		t.Skipf(
			"hay una transacción de escritura ajena más vieja que la ventana (tx=%d <= %d) que contaminaría la medición; pruebe con `docker stop msp-api-dev`",
			*minEscritura, txIDRO,
		)
	}

	// Transacción de escritura posterior a la ventana, confirmada. No escribe
	// filas a propósito: basta su TX_ID, y el repo prohíbe dejar datos de
	// prueba en la base compartida.
	connEscritura, err := pool.Conn(ctx)
	require.NoError(t, err, "abrir conexión de escritura")
	defer connEscritura.Close()

	txEscritura, err := connEscritura.BeginTx(ctx, nil)
	require.NoError(t, err, "abrir transacción de escritura")

	var txWrite int64
	require.NoError(t,
		txEscritura.QueryRowContext(ctx,
			`SELECT CAST(CURRENT_TRANSACTION AS BIGINT) FROM RDB$DATABASE`,
		).Scan(&txWrite),
		"leer CURRENT_TRANSACTION de la transacción de escritura",
	)
	require.NoError(t, txEscritura.Commit(), "confirmar la transacción de escritura")
	require.Greater(t, txWrite, txIDRO,
		"los TX_ID de Firebird son monótonos: la escritura debe ser posterior a la ventana")

	watermark, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)

	// Si el mínimo lo marca una transacción de escritura AJENA que se activó
	// durante la medición, eso es ruido del entorno compartido y no el defecto.
	// Cuando el mínimo es la ventana, en cambio, es exactamente el defecto.
	if watermark <= txWrite && watermark != txIDRO {
		t.Skipf(
			"una transacción de escritura ajena (tx=%d) se activó durante la medición; pruebe con `docker stop msp-api-dev`",
			watermark,
		)
	}

	assert.Less(t, txWrite, watermark,
		"una fila escrita por la tx confirmada %d no pasaría el filtro TX_ID < watermark (%d): "+
			"la ventana de sólo lectura tx=%d congeló el sync",
		txWrite, watermark, txIDRO)

	t.Logf("ventana RO=%d escritura confirmada=%d watermark=%d (sentinel=%d)",
		txIDRO, txWrite, watermark, cobranzaventfb.SentinelNoActiveTx)
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
