//nolint:misspell // Spanish vocabulary by project convention.
package failedintent_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// El caso que estas pruebas cubren: el teléfono reintenta una venta que había
// fallado, esta vez entra, y la fila de MSP_FAILED_INTENTS se queda ahí como
// pendiente para siempre. Quien abre la consola ve trabajo que ya no existe, y
// ese rezago es lo que hace que la pantalla deje de mirarse.
//
// La plataforma NO aprende que existen las ventas: no consulta ninguna tabla
// de negocio. Sólo sabe que una clave de idempotencia que había fallado acabó
// en 2xx.

func peticionConClave(clave string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{"x":1}`))
	if clave != "" {
		r.Header.Set(idempotency.HeaderKey, clave)
	}
	return r
}

func TestCierrePorExito_MismaClave_CierraElIntento(t *testing.T) {
	t.Parallel()

	store := &fakeStore{marcadas: 1}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	rec := httptest.NewRecorder()
	mw(handler200()).ServeHTTP(rec, peticionConClave("venta-77"))

	require.Equal(t, http.StatusOK, rec.Code, "la petición exitosa no se toca")
	require.Len(t, store.marcados, 1, "un 2xx con clave debe cerrar el intento pendiente")
	assert.Equal(t, "/v2/ventas", store.marcados[0].path)
	assert.Equal(t, []string{"venta-77"}, store.marcados[0].keys)
}

func TestCierrePorExito_SinClave_NoTocaNada(t *testing.T) {
	t.Parallel()

	// Sin clave no hay nada que ligue este éxito con ningún intento previo.
	// Cerrar "el más reciente" sería adivinar, y adivinar aquí cierra el
	// problema de otra persona.
	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	rec := httptest.NewRecorder()
	mw(handler200()).ServeHTTP(rec, peticionConClave(""))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, store.marcados, "sin clave no se cierra nada")
}

func TestCierrePorExito_ClaveDistinta_ElIntentoSigueAbierto(t *testing.T) {
	t.Parallel()

	// El store decide a quién alcanza el UPDATE; el middleware sólo entrega la
	// clave de ESTA petición. Lo que se fija aquí es que no invente otra.
	store := &fakeStore{marcadas: 0}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	rec := httptest.NewRecorder()
	mw(handler200()).ServeHTTP(rec, peticionConClave("otra-venta"))

	require.Len(t, store.marcados, 1)
	assert.Equal(t, []string{"otra-venta"}, store.marcados[0].keys,
		"se pasa la clave de esta petición, ninguna otra")
}

func TestCierrePorExito_Falla_NoTumbaLaPeticionQueYaSalioBien(t *testing.T) {
	t.Parallel()

	// Best-effort de principio a fin: la venta ya se creó. Devolver un error
	// al teléfono por un problema de bitácora lo haría reintentar una venta
	// que YA entró — exactamente el defecto que este módulo existe para
	// evitar.
	store := &fakeStore{marcarErr: errors.New("la base dijo que no")}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	rec := httptest.NewRecorder()
	mw(handler200()).ServeHTTP(rec, peticionConClave("venta-77"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
}

func TestCierrePorExito_ElFalloNoCierraNada(t *testing.T) {
	t.Parallel()

	// Un 4xx es lo contrario de un éxito: se captura, no se cierra.
	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	rec := httptest.NewRecorder()
	mw(handler422()).ServeHTTP(rec, peticionConClave("venta-77"))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Empty(t, store.marcados)
	assert.Equal(t, 1, store.count(), "el fallo sí se captura")
}

func TestCierrePorExito_RespuestaDeCache_NoCierra(t *testing.T) {
	t.Parallel()

	// La respuesta la sirvió el caché de idempotencia: el 2xx es de la llamada
	// original, no de ésta. Aquella ya cerró lo que tocaba, y volver a cerrar
	// re-sellaría RESOLVED_AT con una fecha que no corresponde a nada.
	store := &fakeStore{marcadas: 1}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	handlerCacheado := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(failedintent.HeaderIdempotentReplay, "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	rec := httptest.NewRecorder()
	mw(handlerCacheado).ServeHTTP(rec, peticionConClave("venta-77"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, store.marcados)
}

func TestCierrePorExito_RutaFueraDeAlcance_NiSeMira(t *testing.T) {
	t.Parallel()

	store := &fakeStore{marcadas: 1}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 1024))

	r := httptest.NewRequest(http.MethodPost, "/v2/clientes", strings.NewReader(`{"x":1}`))
	r.Header.Set(idempotency.HeaderKey, "cliente-1")

	rec := httptest.NewRecorder()
	mw(handler200()).ServeHTTP(rec, r)

	assert.Empty(t, store.marcados,
		"el middleware sólo mira las rutas que captura; fuera de ellas no tiene nada que cerrar")
}
