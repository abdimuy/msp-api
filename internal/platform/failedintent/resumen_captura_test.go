package failedintent_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// Las pruebas de este archivo miden la captura, no los extractores: qué queda
// en la fila cuando el extractor contesta, cuando no reconoce el cuerpo, cuando
// no hay extractor y cuando revienta.
//
// La que más importa es la multipart. Es la ruta de una venta con fotos —o sea,
// TODAS las ventas— y es exactamente donde el dato se perdía: BODY se guarda
// vacío a propósito, así que un resumen deducido de la fila siempre daba nulo.

// registroDeVentas arma un registro con un extractor mínimo que reconoce
// `{"cliente":{"nombre":...}}`, la forma de POST /v2/ventas.
func registroDeVentas() *failedintent.RegistroExtractores {
	return failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, body []byte, _ string) *failedintent.Resumen {
				if !bytes.Contains(body, []byte(`"cliente"`)) {
					return nil
				}
				monto := decimal.RequireFromString("12500.50")
				return &failedintent.Resumen{
					Titulo:     "Ana Pérez",
					Monto:      &monto,
					Referencia: "F-1001",
				}
			},
		))
}

// multipartDeVenta arma el cuerpo real: un campo `datos` con el JSON y una
// foto. Es la forma que manda la app.
func multipartDeVenta(t *testing.T, datos string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos", datos))
	fw, err := mw.CreateFormFile("imagen", "ine.jpg")
	require.NoError(t, err)
	_, err = fw.Write(bytes.Repeat([]byte{0xFF, 0xD8}, 4096))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// handler422Drena imita al handler real: lee el cuerpo completo antes de
// contestar. Sin leerlo, el TeeReader nunca alimenta al blob y el archivo
// queda vacío — que es un comportamiento previo del middleware, no del
// resumen, pero rompe la prueba de forma confusa si no se imita.
func handler422Drena() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})
}

// El caso que la pantalla necesitaba: venta con fotos, cuerpo en disco, y el
// nombre y el monto igual llegan a la fila.
func TestCaptura_Multipart_GuardaElResumenDelCampoDatos(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.Resumen = registroDeVentas()

	req := multipartDeVenta(t, `{"cliente":{"nombre":"Ana Pérez"},"montos":{"anual":"12500.50"}}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422Drena()).ServeHTTP(rw, req)

	require.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	require.Equal(t, 1, store.count())
	got := store.first()

	// El cuerpo sigue vacío en la fila: eso NO cambia, y es justo por lo que
	// el resumen tiene que viajar aparte.
	require.Equal(t, `null`, string(got.Body))
	require.NotEmpty(t, got.BodyBlobPath)

	require.Equal(t, "ventas", got.Modulo)
	require.NotNil(t, got.Resumen, "el resumen debe llegar a la fila")
	require.Equal(t, "Ana Pérez", got.Resumen.Titulo)
	require.Equal(t, "F-1001", got.Resumen.Referencia)
	require.NotNil(t, got.Resumen.Monto)
	require.Equal(t, "12500.5", got.Resumen.Monto.String())
}

func TestCaptura_JSON_GuardaElResumenDelCuerpo(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 4096)
	cfg.Resumen = registroDeVentas()

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas",
		bytes.NewBufferString(`{"cliente":{"nombre":"Ana Pérez"}}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	require.Equal(t, "ventas", got.Modulo)
	require.NotNil(t, got.Resumen)
	require.Equal(t, "Ana Pérez", got.Resumen.Titulo)
}

// Cuerpo que el extractor no reconoce: queda el módulo (la ruta lo prueba) y
// el resumen se queda nil. Un nil aquí es lo que mantiene viva la fila para el
// relleno del janitor.
func TestCaptura_CuerpoNoReconocido_DejaModuloYResumenNil(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.Resumen = registroDeVentas()

	req := multipartDeVenta(t, `{"otra":"forma"}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422Drena()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	require.Equal(t, "ventas", got.Modulo)
	require.Nil(t, got.Resumen, "un resumen vacío no se guarda como resumen")
}

// Sin extractor configurado la captura funciona exactamente como antes. Es la
// garantía de que un despliegue a medias no rompe nada.
func TestCaptura_SinExtractor_NoCambiaNada(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs) // cfg.Resumen == nil

	req := multipartDeVenta(t, `{"cliente":{"nombre":"Ana"}}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422Drena()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	require.Empty(t, got.Modulo)
	require.Nil(t, got.Resumen)
	require.NotEmpty(t, got.BodyBlobPath, "la evidencia se guarda igual")
}

// Un extractor que revienta no puede costar la evidencia. La fila se guarda,
// con módulo y sin resumen.
func TestCaptura_ExtractorEnPanico_NoPierdeLaEvidencia(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.Resumen = failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				panic("boom")
			},
		))

	req := multipartDeVenta(t, `{"cliente":{"nombre":"Ana"}}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422Drena()).ServeHTTP(rw, req)

	require.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	require.Equal(t, 1, store.count())
	got := store.first()
	require.Equal(t, "ventas", got.Modulo)
	require.Nil(t, got.Resumen)
	require.NotEmpty(t, got.BodyBlobPath)
	require.NotEmpty(t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"la custodia se confirma igual: el resumen es un adorno, la evidencia no")
}

// Cuando el blob no se pudo guardar no hay de dónde sacar el resumen, pero la
// fila —y el módulo— siguen ahí.
func TestCaptura_BlobFallido_ConservaElModulo(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	blobs.saveErr = errors.New("disco lleno")
	cfg := newMultipartTestConfig(store, blobs)
	cfg.Resumen = registroDeVentas()

	req := multipartDeVenta(t, `{"cliente":{"nombre":"Ana"}}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422Drena()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	require.Empty(t, got.BodyBlobPath)
	require.Equal(t, "ventas", got.Modulo)
	require.Nil(t, got.Resumen)
}

// Una ruta capturada por otro middleware (visitas) no tiene extractor: ni
// módulo ni resumen, y eso es correcto — el escritorio la degrada a "otro".
func TestCaptura_RutaSinExtractor_NoInventaModulo(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 4096)
	cfg.PathPrefixes = []string{"/v2/visitas"}
	cfg.Resumen = registroDeVentas()

	req := httptest.NewRequest(http.MethodPost, "/v2/visitas",
		bytes.NewBufferString(`{"cliente":{"nombre":"Ana"}}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler422()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	require.Empty(t, got.Modulo)
	require.Nil(t, got.Resumen)
}

// El 2xx no paga nada: sin captura no hay extracción.
func TestCaptura_RespuestaBuena_NoLlamaAlExtractor(t *testing.T) {
	t.Parallel()

	llamado := false
	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.Resumen = failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				llamado = true
				return nil
			},
		))

	req := multipartDeVenta(t, `{"cliente":{"nombre":"Ana"}}`)
	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		},
	)).ServeHTTP(rw, req)

	require.Equal(t, http.StatusCreated, rw.Code)
	require.Zero(t, store.count())
	require.False(t, llamado, "la ruta feliz no debe pagar la extracción")
}
