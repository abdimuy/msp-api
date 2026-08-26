package failedintent_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// El relleno del janitor es la mitad del arreglo que no se ve. La otra —que los
// intentos nuevos traigan resumen— sólo sirve de aquí en adelante; sin este
// paso, las filas que YA están en la tabla seguirían diciendo "Sin nombre
// capturado" para siempre, y son justo las que alguien está mirando hoy.

// esperarCiclos espera a que el janitor complete N ciclos. La señal de la purga
// llega ANTES del relleno, que es lo último del tick; por eso las pruebas que
// miden el relleno esperan un ciclo de más.
func esperarCiclos(t *testing.T, store *memStore, n int) {
	t.Helper()
	for range n {
		select {
		case <-store.purgeCh:
		case <-time.After(2 * time.Second):
			t.Fatal("el janitor no completó el ciclo a tiempo")
		}
	}
}

// intentoMultipartViejo arma una fila como las que existen hoy: capturada
// antes del extractor, con el cuerpo en disco y `null` en BODY.
func intentoMultipartViejo(t *testing.T, blobs *fakeBlobStore, datos string) failedintent.Intent {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos", datos))
	fw, err := mw.CreateFormFile("imagen", "ine.jpg")
	require.NoError(t, err)
	_, err = fw.Write(bytes.Repeat([]byte{0xAB}, 512))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	id := uuid.New()
	path := "/fake/" + id.String() + ".bin"
	blobs.blobs[path] = buf.Bytes()

	return failedintent.Intent{
		ID:              id,
		ReceivedAt:      time.Now().Add(-2 * time.Hour),
		Path:            "/v2/ventas",
		Body:            []byte(`null`),
		BodyBlobPath:    path,
		BodyContentType: mw.FormDataContentType(),
		Status:          failedintent.StatusNew,
	}
}

func janitorConRelleno(store *memStore, blobs failedintent.BlobStorage) *failedintent.Janitor {
	return failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Blob:     blobs,
		Interval: 10 * time.Millisecond,
		Resumen:  registroDeVentas(),
	})
}

// El caso completo: una fila vieja con el cuerpo en disco se ilumina sola,
// leyendo el blob UNA vez.
func TestJanitor_RellenaLaFilaViejaLeyendoElBlobUnaVez(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := newFakeBlobStore()
	vieja := intentoMultipartViejo(t, blobs, `{"cliente":{"nombre":"Ana Pérez"}}`)
	store.add(vieja)

	contador := &blobContador{BlobStorage: blobs}
	j := janitorConRelleno(store, contador)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	esperarCiclos(t, store, 1)
	require.Eventually(t, func() bool {
		return store.resumenesGuardados.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond, "la fila vieja debió rellenarse")

	got := *store.get(vieja.ID)
	require.Equal(t, "ventas", got.Modulo)
	require.NotNil(t, got.Resumen)
	require.Equal(t, "Ana Pérez", got.Resumen.Titulo)

	// Y aquí está la restricción de diseño, medida: el cuerpo se abrió una
	// sola vez en toda la vida de la fila. Varios ciclos después sigue siendo
	// una — al escribir MODULO la fila sale del filtro y no se vuelve a tocar.
	esperarCiclos(t, store, 3)
	require.Equal(t, int64(1), contador.abiertos.Load(),
		"el blob no puede reabrirse en cada ciclo")
}

// Una fila cuya ruta se reconoce pero cuyo cuerpo no, guarda MODULO y deja
// RESUMEN nulo — y con eso sale del filtro, que es lo que evita releer su blob
// cada hora para siempre.
func TestJanitor_CuerpoNoReconocido_GuardaSoloElModuloYNoReintenta(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := newFakeBlobStore()
	vieja := intentoMultipartViejo(t, blobs, `{"nada":"que ver"}`)
	store.add(vieja)

	contador := &blobContador{BlobStorage: blobs}
	j := janitorConRelleno(store, contador)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return store.get(vieja.ID).Modulo == "ventas"
	}, 2*time.Second, 10*time.Millisecond)

	got := *store.get(vieja.ID)
	require.Equal(t, "ventas", got.Modulo)
	require.Nil(t, got.Resumen, "no se inventa un resumen vacío")

	esperarCiclos(t, store, 3)
	require.Equal(t, int64(1), contador.abiertos.Load())
}

// Una fila cuyo cuerpo es JSON en la propia fila se rellena sin tocar el disco.
func TestJanitor_RellenaLaFilaConCuerpoJSONSinAbrirElDisco(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := newFakeBlobStore()
	contador := &blobContador{BlobStorage: blobs}

	id := uuid.New()
	store.add(failedintent.Intent{
		ID:         id,
		ReceivedAt: time.Now().Add(-time.Hour),
		Path:       "/v2/ventas",
		Body:       []byte(`{"cliente":{"nombre":"Beto"}}`),
		Status:     failedintent.StatusNew,
	})

	j := janitorConRelleno(store, contador)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return store.get(id).Resumen != nil
	}, 2*time.Second, 10*time.Millisecond)
	require.Zero(t, contador.abiertos.Load(), "un cuerpo en la fila no toca el disco")
}

// Una fila que YA tiene resumen no se vuelve a mirar.
func TestJanitor_NoRetocaLoQueYaTieneResumen(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := newFakeBlobStore()
	contador := &blobContador{BlobStorage: blobs}

	vieja := intentoMultipartViejo(t, blobs, `{"cliente":{"nombre":"Ana"}}`)
	vieja.Modulo = "ventas"
	vieja.Resumen = &failedintent.Resumen{Modulo: "ventas", Titulo: "Ana"}
	store.add(vieja)

	j := janitorConRelleno(store, contador)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	esperarCiclos(t, store, 2)
	require.Zero(t, store.resumenesGuardados.Load())
	require.Zero(t, contador.abiertos.Load())
}

// Sin extractor conectado el janitor purga exactamente como antes: el relleno
// es una dependencia opcional, igual que Blob y Resolution.
func TestJanitor_SinExtractorNoRellenaNada(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := newFakeBlobStore()
	vieja := intentoMultipartViejo(t, blobs, `{"cliente":{"nombre":"Ana"}}`)
	store.add(vieja)

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Blob:     blobs,
		Interval: 10 * time.Millisecond,
	})
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	esperarCiclos(t, store, 2)
	require.Zero(t, store.resumenesGuardados.Load())
	require.Empty(t, store.get(vieja.ID).Modulo)
}

// Un fallo al guardar no tumba el ciclo ni bloquea a las demás filas.
func TestJanitor_ErrorAlGuardarNoTumbaElCiclo(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.guardarResumenErr = errors.New("firebird ocupado")
	blobs := newFakeBlobStore()
	store.add(intentoMultipartViejo(t, blobs, `{"cliente":{"nombre":"Ana"}}`))

	j := janitorConRelleno(store, blobs)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	// El ciclo sigue corriendo: la purga se ejecuta varias veces.
	esperarCiclos(t, store, 3)
	require.Zero(t, store.resumenesGuardados.Load())
}

// Un error de List no tumba el ciclo tampoco.
func TestJanitor_ErrorAlListarNoTumbaElCiclo(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.listErr = errors.New("firebird caído")
	blobs := newFakeBlobStore()

	j := janitorConRelleno(store, blobs)
	require.NoError(t, j.Start(context.Background()))
	t.Cleanup(func() { _ = j.Stop(context.Background()) })

	esperarCiclos(t, store, 3)
	require.Zero(t, store.resumenesGuardados.Load())
}
