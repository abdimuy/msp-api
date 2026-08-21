//nolint:misspell // Spanish vocabulary by project convention.
package firebird_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Pruebas de la dedup de capturas.
//
// El defecto que cubren: `Save` era un INSERT puro, así que cada reintento del
// teléfono escribía fila nueva Y copia nueva del cuerpo con sus fotos. Medido
// en producción sobre 7 días: **609 filas para 130 ventas distintas**, 685
// archivos y 896 MB; una sola venta dejó 13 copias de 2.8 MB.
//
// La regla que gobierna el cuerpo es **nunca sustituir uno bueno por uno
// peor**, y no es obvia: `BodyTruncated` sólo marca fallo de guardado, no una
// subida cortada a media transmisión, así que la bandera sola no basta y hace
// falta comparar tamaños.

// capturaConClave arma un intento pendiente con la clave dada.
func capturaConClave(clave, cuerpo string) failedintent.Intent {
	return failedintent.Intent{
		ID:             uuid.New(),
		ReceivedAt:     time.Now().UTC().Truncate(time.Millisecond),
		Method:         "POST",
		Path:           "/v2/ventas",
		IdempotencyKey: clave,
		RequestID:      uuid.New(),
		Body:           json.RawMessage(cuerpo),
		HTTPStatus:     422,
		ErrorCode:      "articulo_sin_existencia",
		ErrorMessage:   "sin existencia",
		Status:         failedintent.StatusNew,
	}
}

// contarFilas cuenta las filas pendientes con esa clave.
func contarFilas(ctx context.Context, t *testing.T, pool *firebird.Pool, clave string) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_FAILED_INTENTS WHERE IDEMPOTENCY_KEY = ?`, clave).Scan(&n))
	return n
}

// TestSave_DedupPorClave_UnaFilaYUnConteo es el corazón del arreglo: dos
// capturas del mismo trabajo dejan UNA fila con el conteo en 2.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_DedupPorClave_UnaFilaYUnConteo(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		clave := "dedup-" + uuid.NewString()

		primera := capturaConClave(clave, `{"n":1}`)
		out, err := s.Save(ctx, primera)
		require.NoError(t, err)
		require.False(t, out.Deduped, "la primera captura no deduplica nada")

		segunda := capturaConClave(clave, `{"n":1}`)
		segunda.ReceivedAt = primera.ReceivedAt.Add(4 * time.Minute)
		out, err = s.Save(ctx, segunda)
		require.NoError(t, err)
		assert.True(t, out.Deduped, "la segunda captura debe fundirse en la primera")

		require.Equal(t, 1, contarFilas(ctx, t, pool, clave),
			"dos capturas del mismo trabajo son UNA fila; con dos, la pantalla vuelve a ser ilegible")

		got, err := s.Get(ctx, primera.ID)
		require.NoError(t, err)
		require.NotNil(t, got, "la fila que sobrevive es la PRIMERA")
		assert.Equal(t, 1, got.RetryCount, "RETRY_COUNT sube uno por reintento")

		// RECEIVED_AT no se mueve: pasa a ser "el primer intento".
		assert.WithinDuration(t, primera.ReceivedAt, got.ReceivedAt, time.Second)
		// LAST_SEEN_AT sí: es la mitad que permite decir "el último hace 4 minutos".
		require.NotNil(t, got.LastSeenAt, "LAST_SEEN_AT debe quedar escrito")
		assert.WithinDuration(t, segunda.ReceivedAt, *got.LastSeenAt, time.Second)

		// La segunda captura no dejó fila propia.
		huerfana, err := s.Get(ctx, segunda.ID)
		require.NoError(t, err)
		assert.Nil(t, huerfana)
	})
}

// TestSave_Dedup_RefrescaLaCausaVigente fija que el error que se muestra es el
// del ÚLTIMO intento, no el del primero: una venta que empezó fallando por red
// y ahora falla por inventario necesita a una persona, y con el error viejo se
// leería como que se cura sola.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_Dedup_RefrescaLaCausaVigente(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		clave := "causa-" + uuid.NewString()

		primera := capturaConClave(clave, `{"n":1}`)
		primera.HTTPStatus = 503
		primera.ErrorCode = ""
		primera.ErrorMessage = "servicio no disponible"
		require.NoError(t, saveOK(s.Save(ctx, primera)))

		segunda := capturaConClave(clave, `{"n":1}`)
		segunda.HTTPStatus = 422
		segunda.ErrorCode = "articulo_sin_existencia"
		segunda.ErrorMessage = "sin existencia"
		require.NoError(t, saveOK(s.Save(ctx, segunda)))

		got, err := s.Get(ctx, primera.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 422, got.HTTPStatus)
		assert.Equal(t, "articulo_sin_existencia", got.ErrorCode)
		assert.Equal(t, "sin existencia", got.ErrorMessage)
	})
}

// TestSave_Dedup_ElCuerpoNuncaEmpeora recorre la regla completa del cuerpo.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_Dedup_ElCuerpoNuncaEmpeora(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	bueno := `{"cliente":"Carmen López Zavaleta","montos":{"corto_plazo":"10300.00"}}`
	corto := `{"n":1}`

	casos := []struct {
		nombre        string
		segunda       func(failedintent.Intent) failedintent.Intent
		esperado      string
		porQueImporta string
	}{
		{
			nombre: "la segunda truncada NO sustituye",
			segunda: func(i failedintent.Intent) failedintent.Intent {
				i.Body = json.RawMessage(corto)
				i.BodyTruncated = true
				return i
			},
			esperado:      bueno,
			porQueImporta: "un cuerpo que no se pudo guardar entero no reemplaza a uno completo",
		},
		{
			nombre: "la segunda más chica y no truncada tampoco",
			segunda: func(i failedintent.Intent) failedintent.Intent {
				i.Body = json.RawMessage(corto)
				return i
			},
			esperado: bueno,
			porQueImporta: "BodyTruncated sólo marca fallo de guardado, no una subida cortada " +
				"a media transmisión: sin comparar tamaños, una captura cortada pasaría por buena",
		},
		{
			nombre: "la segunda buena y mayor SÍ sustituye",
			segunda: func(i failedintent.Intent) failedintent.Intent {
				i.Body = json.RawMessage(bueno + strings.Repeat(" ", 40))
				return i
			},
			esperado: bueno,
			porQueImporta: "se conserva el ÚLTIMO bueno: cuando la app tenga edición de ventas, " +
				"el cuerpo que sirve para un reenvío manual es el corregido",
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				s := failedintentfb.New(pool)
				clave := "cuerpo-" + uuid.NewString()

				primera := capturaConClave(clave, bueno)
				require.NoError(t, saveOK(s.Save(ctx, primera)))

				require.NoError(t, saveOK(s.Save(ctx, c.segunda(capturaConClave(clave, bueno)))))

				got, err := s.Get(ctx, primera.ID)
				require.NoError(t, err)
				require.NotNil(t, got)
				assert.JSONEq(t, c.esperado, string(got.Body), c.porQueImporta)
				assert.Equal(t, 1, got.RetryCount,
					"el reintento se cuenta aunque el cuerpo no se haya sustituido")
			})
		})
	}
}

// TestSave_Dedup_ElBlobDescartadoSeReporta fija el contrato que evita que el
// disco siga creciendo: Save dice qué archivo quedó sin dueño para que el
// middleware lo borre. Sin esto la tabla dejaría de crecer y los 896 MB no.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_Dedup_ElBlobDescartadoSeReporta(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	t.Run("se conserva el guardado: sobra el que acaba de llegar", func(t *testing.T) {
		fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
			s := failedintentfb.New(pool)
			clave := "blob-a-" + uuid.NewString()

			primera := capturaConClave(clave, `null`)
			primera.BodyBlobPath = "/blobs/primera.bin"
			primera.BodyContentType = "multipart/form-data; boundary=x"
			require.NoError(t, saveOK(s.Save(ctx, primera)))

			segunda := capturaConClave(clave, `null`)
			segunda.BodyBlobPath = "/blobs/segunda.bin"
			segunda.BodyContentType = "multipart/form-data; boundary=x"
			segunda.BodyTruncated = true // llegó peor: no sustituye
			out, err := s.Save(ctx, segunda)
			require.NoError(t, err)

			assert.True(t, out.Deduped)
			assert.Equal(t, "/blobs/segunda.bin", out.OrphanedBlobPath,
				"el cuerpo que llegó y no se adoptó es el que sobra en disco")

			got, err := s.Get(ctx, primera.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "/blobs/primera.bin", got.BodyBlobPath)
		})
	})

	t.Run("el nuevo desplaza al guardado: sobra el viejo", func(t *testing.T) {
		fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
			s := failedintentfb.New(pool)
			clave := "blob-b-" + uuid.NewString()

			primera := capturaConClave(clave, `null`)
			primera.BodyBlobPath = "/blobs/vieja.bin"
			primera.BodyTruncated = true // el guardado está truncado
			require.NoError(t, saveOK(s.Save(ctx, primera)))

			segunda := capturaConClave(clave, `null`)
			segunda.BodyBlobPath = "/blobs/nueva.bin"
			out, err := s.Save(ctx, segunda)
			require.NoError(t, err)

			assert.Equal(t, "/blobs/vieja.bin", out.OrphanedBlobPath)

			got, err := s.Get(ctx, primera.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "/blobs/nueva.bin", got.BodyBlobPath)
			assert.False(t, got.BodyTruncated)
		})
	})
}

// TestSave_SinClave_NoDeduplica: sin clave no hay nada que pruebe que dos
// capturas son el mismo trabajo, y juntarlas perdería evidencia.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_SinClave_NoDeduplica(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		marca := "sin-clave-" + uuid.NewString()

		for range 2 {
			i := capturaConClave("", `{"n":1}`)
			i.ErrorMessage = marca
			out, err := s.Save(ctx, i)
			require.NoError(t, err)
			assert.False(t, out.Deduped)
		}

		q := firebird.GetQuerier(ctx, pool.DB)
		var n int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_FAILED_INTENTS WHERE ERROR_MESSAGE = ?`, marca).Scan(&n))
		assert.Equal(t, 2, n, "sin clave, cada captura es su propia fila")
	})
}

// TestSave_Dedup_NoTocaLasResueltas: una fila ya resuelta o ignorada es
// historia cerrada. Reabrirla con un reintento tardío borraría la decisión que
// una persona ya tomó.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_Dedup_NoTocaLasResueltas(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		clave := "resuelta-" + uuid.NewString()

		resuelta := capturaConClave(clave, `{"n":1}`)
		resuelta.Status = failedintent.StatusIgnored
		require.NoError(t, saveOK(s.Save(ctx, resuelta)))

		tardia := capturaConClave(clave, `{"n":2}`)
		out, err := s.Save(ctx, tardia)
		require.NoError(t, err)
		assert.False(t, out.Deduped, "no se funde en una fila cerrada")

		assert.Equal(t, 2, contarFilas(ctx, t, pool, clave))

		vieja, err := s.Get(ctx, resuelta.ID)
		require.NoError(t, err)
		require.NotNil(t, vieja)
		assert.Equal(t, failedintent.StatusIgnored, vieja.Status,
			"la decisión que tomó una persona no se reabre sola")
		assert.Equal(t, 0, vieja.RetryCount)
	})
}

// TestSave_Dedup_LaClaveNoCruzaRecursos: la clave la elige el cliente y nada
// garantiza que sea única entre recursos. Fundir el intento de un pago con el
// de una venta que casualmente comparten cadena sería mucho peor que dos filas.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestSave_Dedup_LaClaveNoCruzaRecursos(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		clave := "compartida-" + uuid.NewString()

		venta := capturaConClave(clave, `{"n":1}`)
		require.NoError(t, saveOK(s.Save(ctx, venta)))

		pago := capturaConClave(clave, `{"n":2}`)
		pago.Path = "/v2/cobranza/pagos"
		out, err := s.Save(ctx, pago)
		require.NoError(t, err)
		assert.False(t, out.Deduped)

		assert.Equal(t, 2, contarFilas(ctx, t, pool, clave))
	})
}

// TestSave_Dedup_Concurrente_NoDejaLaTablaInconsistente lanza varias capturas
// del mismo trabajo a la vez.
//
// El contrato que se afirma es débil a propósito: con transacciones
// concurrentes reales Firebird puede resolver la carrera dejando dos filas
// pendientes (dos INSERT que no se ven entre sí). Eso es aceptable —la
// pantalla las vuelve a agrupar por clave— y lo que NO es aceptable es perder
// una captura o dejar conteos imposibles.
//
// Esta prueba COMMITEA (necesita transacciones separadas), así que limpia lo
// suyo en t.Cleanup **sin descartar el error del DELETE**: descartarlo deja
// las filas ahí para siempre y la limpieza parece haber funcionado.
//
//nolint:paralleltest // serial: escribe y commitea en la BD compartida.
func TestSave_Dedup_Concurrente_NoDejaLaTablaInconsistente(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	ctx := context.Background()
	s := failedintentfb.New(pool)
	clave := "concurrente-" + uuid.NewString()

	t.Cleanup(func() {
		_, err := pool.ExecContext(context.Background(),
			`DELETE FROM MSP_FAILED_INTENTS WHERE IDEMPOTENCY_KEY = ?`, clave)
		require.NoError(t, err, "la limpieza tiene que borrar de verdad: si se descarta el "+
			"error, las filas se quedan y el cleanup miente")
	})

	const capturas = 5
	var wg sync.WaitGroup
	errs := make([]error, capturas)
	for n := range capturas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[n] = s.Save(ctx, capturaConClave(clave, `{"n":1}`))
		}()
	}
	wg.Wait()

	for n, err := range errs {
		require.NoError(t, err, "captura %d: ninguna se puede perder por una carrera", n)
	}

	q := firebird.GetQuerier(ctx, pool.DB)
	var filas, sumaIntentos int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(RETRY_COUNT + 1), 0)
		   FROM MSP_FAILED_INTENTS WHERE IDEMPOTENCY_KEY = ?`, clave).Scan(&filas, &sumaIntentos))

	assert.Positive(t, filas, "alguna fila tiene que quedar")
	assert.LessOrEqual(t, filas, capturas)
	assert.Equal(t, capturas, sumaIntentos,
		"las %d capturas tienen que estar contadas, repartidas o no entre filas", capturas)
}
