package failedintenthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// El contrato que arregla la pantalla: el listado trae el módulo y el resumen
// de cada renglón, y el filtro de los chips es un parámetro del listado.
//
// Todo lo demás de esta pantalla ya funcionaba. Lo que faltaba era el dato.

func intentoConResumen(id uuid.UUID, at time.Time, modulo, titulo, monto string) failedintent.Intent {
	i := makeIntent(id, at)
	i.Status = failedintent.StatusNew
	i.Modulo = modulo
	if titulo == "" && monto == "" {
		return i
	}
	res := &failedintent.Resumen{Modulo: modulo, Titulo: titulo, Referencia: "REF-1"}
	if monto != "" {
		d := decimal.RequireFromString(monto)
		res.Monto = &d
	}
	i.Resumen = res
	return i
}

func listar(t *testing.T, store *memoryStore, query string) failedintenthttp.ListResponse {
	t.Helper()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+query, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestListar_ExponeModuloYResumen(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.MustParse("aaaaaaaa-1111-1111-1111-111111111111")
	seedIntent(t, store, intentoConResumen(id, now, "ventas", "María Ángeles Muñoz", "12500.50"))

	resp := listar(t, store, "")
	require.Len(t, resp.Items, 1)
	got := resp.Items[0]

	assert.Equal(t, "ventas", got.Modulo)
	require.NotNil(t, got.Resumen, "el renglón necesita quién y cuánto")
	assert.Equal(t, "María Ángeles Muñoz", got.Resumen.Titulo)
	assert.Equal(t, "12500.5", got.Resumen.Monto)
	assert.Equal(t, "REF-1", got.Resumen.Referencia)
}

// El monto viaja como CADENA. Un número JSON lo pasaría por un float64 de ida
// y de vuelta, y un importe es justo el dato que no se redondea.
func TestListar_ElMontoViajaComoCadena(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aaaaaaaa-2222-2222-2222-222222222222")
	seedIntent(t, store, intentoConResumen(
		id, time.Now().UTC(), "ventas", "Ana", "0.10",
	))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var crudo struct {
		Items []struct {
			Resumen struct {
				Monto json.RawMessage `json:"monto"`
			} `json:"resumen"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &crudo))
	require.Len(t, crudo.Items, 1)
	assert.JSONEq(t, `"0.1"`, string(crudo.Items[0].Resumen.Monto))
}

// Sin resumen el campo se OMITE. Un objeto vacío obligaría al escritorio a
// distinguir "hay resumen sin nombre" de "no hay resumen", y son lo mismo.
func TestListar_SinResumenElCampoSeOmite(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aaaaaaaa-3333-3333-3333-333333333333")
	seedIntent(t, store, intentoConResumen(id, time.Now().UTC(), "ventas", "", ""))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), `"resumen"`)
	// Pero el módulo SÍ viaja: la ruta se reconoció aunque el cuerpo no.
	assert.Contains(t, rec.Body.String(), `"modulo":"ventas"`)
}

// Una fila sin módulo tampoco lo inventa. El escritorio la degrada a "otro".
func TestListar_SinModuloElCampoSeOmite(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aaaaaaaa-4444-4444-4444-444444444444")
	seedIntent(t, store, intentoConResumen(id, time.Now().UTC(), "", "", ""))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), `"modulo"`)
}

func TestListar_FiltraPorModulo(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC().Truncate(time.Second)
	venta := uuid.MustParse("bbbbbbbb-1111-1111-1111-111111111111")
	pago := uuid.MustParse("bbbbbbbb-2222-2222-2222-222222222222")
	seedIntent(t, store, intentoConResumen(venta, now.Add(-time.Minute), "ventas", "Ana", "100"))
	seedIntent(t, store, intentoConResumen(pago, now, "pagos", "Gabriel", "350"))

	resp := listar(t, store, "?modulo=pagos")
	require.Len(t, resp.Items, 1)
	assert.Equal(t, pago.String(), resp.Items[0].ID)

	resp = listar(t, store, "?modulo=ventas")
	require.Len(t, resp.Items, 1)
	assert.Equal(t, venta.String(), resp.Items[0].ID)

	// Sin filtro salen las dos: el chip "Todo".
	resp = listar(t, store, "")
	require.Len(t, resp.Items, 2)
}

// Un módulo desconocido devuelve cero filas, no un 422. El filtro no valida
// contra una lista cerrada a propósito: los módulos se registran en cmd/api y
// uno nuevo debe poder filtrarse sin tocar el transporte.
func TestListar_ModuloDesconocidoDevuelveVacio(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	seedIntent(t, store, intentoConResumen(
		uuid.MustParse("cccccccc-1111-1111-1111-111111111111"),
		time.Now().UTC(), "ventas", "Ana", "100",
	))

	resp := listar(t, store, "?modulo=inexistente")
	assert.Empty(t, resp.Items)
}

// El filtro de módulo se combina con el de estado sin pisarse.
func TestListar_ModuloYEstadoSeCombinan(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC().Truncate(time.Second)

	pendiente := intentoConResumen(
		uuid.MustParse("dddddddd-1111-1111-1111-111111111111"),
		now.Add(-time.Minute), "ventas", "Ana", "100",
	)
	cerrada := intentoConResumen(
		uuid.MustParse("dddddddd-2222-2222-2222-222222222222"),
		now, "ventas", "Beto", "200",
	)
	cerrada.Status = failedintent.StatusIgnored

	seedIntent(t, store, pendiente)
	seedIntent(t, store, cerrada)

	resp := listar(t, store, "?modulo=ventas&status=ignored")
	require.Len(t, resp.Items, 1)
	assert.Equal(t, cerrada.ID.String(), resp.Items[0].ID)
}

// El espacio de más en el parámetro no debe convertir "Ventas " en un módulo
// distinto que devuelva cero filas sin explicación.
func TestListar_RecortaElParametroDeModulo(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("eeeeeeee-1111-1111-1111-111111111111")
	seedIntent(t, store, intentoConResumen(id, time.Now().UTC(), "ventas", "Ana", "100"))

	resp := listar(t, store, "?modulo=%20ventas%20")
	require.Len(t, resp.Items, 1)
}

// El detalle expone lo mismo que el listado: abrir un renglón no debe pedir
// otra vuelta para saber de quién es.
func TestObtener_ExponeModuloYResumen(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("ffffffff-1111-1111-1111-111111111111")
	seedIntent(t, store, intentoConResumen(id, time.Now().UTC(), "ventas", "Ana Pérez", "999.99"))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "ventas", got.Modulo)
	require.NotNil(t, got.Resumen)
	assert.Equal(t, "Ana Pérez", got.Resumen.Titulo)
	assert.Equal(t, "999.99", got.Resumen.Monto)
}
