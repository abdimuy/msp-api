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
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, nil)
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

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, nil)
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

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, nil)
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

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, nil)
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

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, nil)
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

// ─── El corte por etapa ───────────────────────────────────────────────────────
//
// Una venta puede estar en tres lugares: sólo aquí (llegó al API y no entró a
// la base), en MSP_VENTAS, o ya aplicada en Microsip. Esta pantalla existe para
// el primero. La discriminación es estructural: si la ruta trae un id, la fila
// ya existe y su petición nunca llevó un nombre que mostrar.

// rutasRaizDePrueba imita lo que la raíz de composición arma con los prefijos
// de captura de cada módulo.
var rutasRaizDePrueba = []string{"/v2/ventas", "/v2/cobranza/pagos", "/v2/visitas"}

func intentoEnRuta(id uuid.UUID, at time.Time, path string) failedintent.Intent {
	i := intentoConResumen(id, at, "ventas", "Ana", "100")
	i.Path = path
	return i
}

// sembrarEtapas siembra las tres etapas de un mismo módulo. La creación es la
// MÁS VIEJA a propósito.
func sembrarEtapas(
	t *testing.T, store *memoryStore, base time.Time, usuario *uuid.UUID,
) map[string]uuid.UUID {
	t.Helper()
	ids := map[string]uuid.UUID{
		"creacion": uuid.MustParse("11111111-0000-0000-0000-000000000001"),
		"detalle":  uuid.MustParse("11111111-0000-0000-0000-000000000002"),
		"aplicar":  uuid.MustParse("11111111-0000-0000-0000-000000000003"),
		"search":   uuid.MustParse("11111111-0000-0000-0000-000000000004"),
	}
	rutas := map[string]string{
		"creacion": "/v2/ventas",
		"detalle":  "/v2/ventas/922e8527-e127-47d4-a84e-28ac0b809470",
		"aplicar":  "/v2/ventas/922e8527-e127-47d4-a84e-28ac0b809470/aplicar",
		"search":   "/v2/ventas/_search/refresh",
	}
	edades := map[string]time.Duration{
		"creacion": -3 * time.Hour,
		"detalle":  -2 * time.Hour,
		"aplicar":  -time.Hour,
		"search":   0,
	}
	for clave, id := range ids {
		i := intentoEnRuta(id, base.Add(edades[clave]), rutas[clave])
		if usuario != nil {
			i.UsuarioID = usuario
		}
		seedIntent(t, store, i)
	}
	return ids
}

// listarConEtapas monta el Service como en producción: con la unión de rutas
// raíz que le pasa la raíz de composición.
func listarConEtapas(t *testing.T, store *memoryStore, query string) failedintenthttp.ListResponse {
	t.Helper()
	svc := failedintenthttp.NewService(
		store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, rutasRaizDePrueba,
	)
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

func TestListar_PorDefectoSoloLaEtapaDeCaptura(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	ids := sembrarEtapas(t, store, time.Now().UTC().Truncate(time.Second), nil)

	resp := listarConEtapas(t, store, "")

	require.Len(t, resp.Items, 1)
	assert.Equal(t, ids["creacion"].String(), resp.Items[0].ID,
		"POST /v2/ventas es la creación; lo demás ya existe en la base")
}

// El control positivo del mismo conjunto: sin el corte salen las cuatro. Sin
// esto, "sólo una fila" no se distingue de una consulta rota.
func TestListar_EtapaTodasSigueDevolviendoTodo(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	ids := sembrarEtapas(t, store, time.Now().UTC().Truncate(time.Second), nil)

	resp := listarConEtapas(t, store, "?etapa=todas")

	vistos := make(map[string]bool, len(resp.Items))
	for _, i := range resp.Items {
		vistos[i.ID] = true
	}
	for clave, id := range ids {
		assert.True(t, vistos[id.String()], "la evidencia de %q no se pierde", clave)
	}
}

// El valor por defecto explícito es el mismo que omitir el parámetro.
func TestListar_EtapaCapturaEsElValorPorDefecto(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	ids := sembrarEtapas(t, store, time.Now().UTC().Truncate(time.Second), nil)

	resp := listarConEtapas(t, store, "?etapa=captura")

	require.Len(t, resp.Items, 1)
	assert.Equal(t, ids["creacion"].String(), resp.Items[0].ID)
}

// Un valor desconocido es un 422 y no un filtro silencioso. Aquí sí hay lista
// cerrada —a diferencia de `modulo`— porque son dos valores del propio
// transporte, no nombres que otro módulo pueda registrar.
func TestListar_EtapaInvalidaEs422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(
		store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, rutasRaizDePrueba,
	)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?etapa=aplicar", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	var problem problemBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, "invalid_etapa", problem.Code)
}

// Sin rutas raíz configuradas no hay corte. Es lo que ve un despliegue que no
// las pasa, y debe comportarse como antes de este cambio.
func TestListar_SinRutasRaizConfiguradasNoAcota(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	ids := sembrarEtapas(t, store, time.Now().UTC().Truncate(time.Second), nil)

	resp := listar(t, store, "")
	assert.Len(t, resp.Items, len(ids))
}

// La pantalla del cobrador acota igual. Si no, un cobrador vería en su bandeja
// los `/aplicar` que no puede reintentar y que no dicen de quién son.
func TestMeListar_TambienAcotaLaEtapa(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	cu := defaultCU()
	base := time.Now().UTC().Truncate(time.Second)
	propio := cu.ID
	ids := sembrarEtapas(t, store, base, &propio)

	svc := failedintenthttp.NewService(
		store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil, rutasRaizDePrueba,
	)
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, ids["creacion"].String(), resp.Items[0].ID)
}
