package desempenohttp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
)

// Claves de cada objeto del contrato v2.1 (docs/superpowers/specs/
// 2026-10-14-desempeno-contratos.md), copiadas A MANO del documento y NO de
// los DTOs: si alguien renombra un campo del DTO, esta prueba se pone roja.
var (
	clavesActividad     = []string{"id", "tipo", "hora", "recibido_at", "cliente_id", "cliente_nombre", "venta_id", "folio", "registrado_por", "lat", "lng", "con_gps", "num_fotos", "docto_pv_id", "captura_tardia", "pago", "visita"}
	clavesRegistradoPor = []string{"usuario_uid", "cobrador_id", "nombre", "origen"}
	clavesPago          = []string{"importe", "forma_cobro", "forma_cobro_id", "aplicado", "rechazado", "docto_cc_id", "pago_recibido_id"}
	clavesVisita        = []string{"visita_id", "tipo_visita", "nota", "estado", "trato", "etiqueta"}
	clavesLinea         = []string{"dia", "puntos", "tramos", "por_hora", "pausas"}
	clavesPunto         = []string{"id", "tipo", "hora", "cliente_nombre", "importe", "resultado", "num_fotos"}
	clavesTramo         = []string{"desde", "hasta", "pagos", "visitas", "condonaciones", "clientes"}
	clavesHora          = []string{"hora", "pagos", "visitas", "condonaciones", "importe"}
	clavesPausa         = []string{"desde", "hasta", "minutos", "anterior_id", "siguiente_id", "en_curso"}
	clavesSenal         = []string{"tipo", "actividad_ids", "desde", "hasta", "detalle", "regla"}
	clavesTablero       = []string{"generado_at", "periodo", "pausa_min", "usuarios"}
	clavesUsuario       = []string{
		"uid", "nombre", "email", "zona_id", "zona_nombre", "configurado", "motivo_no_configurado", "inicio_semana",
		"ventas_universo", "estado_ahora", "estado_desde", "ultima", "primera_actividad", "ultima_actividad",
		"minutos_activos", "pausas", "pagos", "pagos_por_forma", "condonaciones", "no_aplicados", "visitas",
		"cobertura", "cobertura_por_segmento", "cobro", "lineas",
	}
	clavesJornada = []string{"uid", "fecha", "pausa_min", "generado_at", "actividades", "linea", "senales", "resumen"}
	clavesResumen = []string{"primera_actividad", "ultima_actividad", "minutos_activos", "pausas", "pagos", "visitas", "condonaciones", "clientes"}
	clavesPagina  = []string{"items", "siguiente_cursor", "totales"}
	clavesTotales = []string{"n", "importe", "por_tipo"}
	clavesCuentas = []string{"inicio_semana", "conteos", "cuentas"}
	clavesConteos = []string{"sin_visitar", "volver", "despues", "pagados", "sin_nada", "total"}
	clavesCuenta  = []string{"venta_id", "docto_pv_id", "folio", "cliente_id", "cliente_nombre", "producto", "estado", "trato", "etiqueta", "detalle", "segmento", "con_algo", "determinado_por", "ultima_actividad", "saldo", "parcialidad", "abono_semana", "num_atrasos", "nueva"}
)

// getCrudo hace GET con desempeno:ver y devuelve el cuerpo crudo (200).
func getCrudo(t *testing.T, path string) []byte {
	t.Helper()
	cu := usuarioCon(auth.PermDesempenoVer)
	rec := httptest.NewRecorder()
	router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	return rec.Body.Bytes()
}

func get(t *testing.T, path string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(getCrudo(t, path), &m))
	return m
}

// fixture compara el cuerpo con testdata/<nombre>.json campo por campo
// (incluidos los null). Los fixtures se escribieron a partir del contrato v2.1
// y se revisaron contra él a mano; para regenerarlos tras un cambio de
// contrato: DESEMPENO_ACTUALIZAR_FIXTURES=1 go test ./internal/desempeno/infra/desempenohttp/
// y revisar el diff línea por línea antes de commitear.
func fixture(t *testing.T, nombre string, cuerpo []byte) {
	t.Helper()
	ruta := filepath.Join("testdata", nombre+".json")
	if os.Getenv("DESEMPENO_ACTUALIZAR_FIXTURES") == "1" {
		var buf bytes.Buffer
		require.NoError(t, json.Indent(&buf, cuerpo, "", "  "))
		require.NoError(t, buf.WriteByte('\n'))
		require.NoError(t, os.WriteFile(ruta, buf.Bytes(), 0o600))
	}
	esperado, err := os.ReadFile(ruta) //nolint:gosec // ruta fija de testdata.
	require.NoError(t, err)
	assert.JSONEq(t, string(esperado), string(cuerpo), "fixture %s", ruta)
}

func claves(t *testing.T, v any, esperadas []string, donde string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "%s no es objeto: %v", donde, v)
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	want := append([]string(nil), esperadas...)
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got, "claves de %s", donde)
	return m
}

func primero(t *testing.T, v any, donde string) any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "%s no es lista", donde)
	require.NotEmpty(t, l, "%s vacía: el escenario debe llenarla", donde)
	return l[0]
}

func revisarActividad(t *testing.T, v any, donde string) {
	t.Helper()
	a := claves(t, v, clavesActividad, donde)
	claves(t, a["registrado_por"], clavesRegistradoPor, donde+".registrado_por")
	if a["pago"] != nil {
		claves(t, a["pago"], clavesPago, donde+".pago")
	}
	if a["visita"] != nil {
		claves(t, a["visita"], clavesVisita, donde+".visita")
	}
}

func revisarLinea(t *testing.T, v any, donde string) {
	t.Helper()
	l := claves(t, v, clavesLinea, donde)
	claves(t, primero(t, l["puntos"], donde+".puntos"), clavesPunto, donde+".puntos[0]")
	claves(t, primero(t, l["tramos"], donde+".tramos"), clavesTramo, donde+".tramos[0]")
	claves(t, primero(t, l["por_hora"], donde+".por_hora"), clavesHora, donde+".por_hora[0]")
	claves(t, primero(t, l["pausas"], donde+".pausas"), clavesPausa, donde+".pausas[0]")
}

func usuarioPorUID(t *testing.T, m map[string]any, uid string) map[string]any {
	t.Helper()
	for _, u := range m["usuarios"].([]any) {
		if um := u.(map[string]any); um["uid"] == uid {
			return um
		}
	}
	require.Failf(t, "usuario ausente", "uid %s", uid)
	return nil
}

func TestContrato_Tablero(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/usuarios")
	fixture(t, "tablero_hoy", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	claves(t, m, clavesTablero, "tablero")
	require.Len(t, m["usuarios"], 4, "uno configurado y uno por motivo de sin configurar")
	for i, v := range m["usuarios"].([]any) {
		claves(t, v, clavesUsuario, fmt.Sprintf("usuarios[%d]", i))
	}
	u := usuarioPorUID(t, m, "abc123")
	claves(t, u["ultima"], []string{"id", "tipo", "hora", "cliente_nombre"}, "ultima")
	claves(t, u["pausas"], []string{"n", "minutos", "en_curso"}, "pausas")
	claves(t, u["pagos"], []string{"n", "importe"}, "pagos")
	claves(t, u["pagos_por_forma"], []string{"efectivo", "transferencia", "cheque"}, "pagos_por_forma")
	claves(t, u["cobertura"], []string{"con_algo", "total"}, "cobertura")
	claves(t, u["cobertura_por_segmento"], []string{"sin_visitar", "volver", "despues", "pagados", "sin_nada"}, "cobertura_por_segmento")
	claves(t, u["cobro"], []string{"cobrado", "esperado", "falta", "pct"}, "cobro")
	revisarLinea(t, primero(t, u["lineas"], "lineas"), "lineas[0]")

	assert.Equal(t, "hoy", m["periodo"])
	assert.InDelta(t, 45, m["pausa_min"], 0)
	assert.Equal(t, "2026-10-14T22:40:00Z", m["generado_at"])
	assert.NotContains(t, m, "$schema", "D-API-10: sin $schema en desempeño")
	assert.Equal(t, "150.00", u["pagos"].(map[string]any)["importe"], "dinero como string con 2 decimales")
	assert.Equal(t, "65.90", u["cobro"].(map[string]any)["pct"])
	assert.Equal(t, "2026-10-09T14:46:00Z", u["inicio_semana"])
	assert.Nil(t, u["motivo_no_configurado"], "null explícito, no omitido")
}

// Contrato v2.1 §2: una fila sin configurar trae los tres motivos, métricas en
// cero, lineas=[] y null en zona/inicio/pct cuando no hay dato.
func TestContrato_TableroSinConfigurar(t *testing.T) {
	t.Parallel()
	m := get(t, "/desempeno/usuarios")
	casos := []struct{ uid, motivo string }{
		{"sinzona", "sin_zona"},
		{"sininicio", "sin_inicio_semana"},
		{"futuro", "inicio_semana_futuro"},
	}
	for _, c := range casos {
		u := usuarioPorUID(t, m, c.uid)
		assert.Equal(t, false, u["configurado"], c.uid)
		assert.Equal(t, c.motivo, u["motivo_no_configurado"], c.uid)
		assert.Equal(t, []any{}, u["lineas"], "%s: lineas=[] y no null", c.uid)
		assert.Nil(t, u["cobro"].(map[string]any)["pct"], c.uid)
		assert.Equal(t, "0.00", u["cobro"].(map[string]any)["cobrado"], c.uid)
		assert.Nil(t, u["ultima"], c.uid)
		assert.Equal(t, "no_ha_empezado", u["estado_ahora"], c.uid)
	}
	sz := usuarioPorUID(t, m, "sinzona")
	assert.Nil(t, sz["zona_id"])
	assert.Nil(t, sz["zona_nombre"])
	assert.Nil(t, usuarioPorUID(t, m, "sininicio")["inicio_semana"])
	assert.NotNil(t, usuarioPorUID(t, m, "futuro")["inicio_semana"], "el inicio futuro se muestra para corregirlo")
}

func TestContrato_TableroSemana(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/usuarios?periodo=semana&pausa_min=60&desde_hora=08:00&hasta_hora=18:00")
	fixture(t, "tablero_semana", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	assert.Equal(t, "semana", m["periodo"])
	assert.InDelta(t, 60, m["pausa_min"], 0)
	u := usuarioPorUID(t, m, "abc123")
	// Viernes 9 → miércoles 14: un día por cada día de la semana.
	require.Len(t, u["lineas"], 6)
	assert.Equal(t, "2026-10-09", u["lineas"].([]any)[0].(map[string]any)["dia"])
	assert.Equal(t, "2026-10-14", u["lineas"].([]any)[5].(map[string]any)["dia"])
}

func TestContrato_Jornada(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/usuarios/abc123/jornada?fecha=2026-10-14")
	fixture(t, "jornada", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	claves(t, m, clavesJornada, "jornada")
	revisarActividad(t, primero(t, m["actividades"], "actividades"), "actividades[0]")
	revisarLinea(t, m["linea"], "linea")
	claves(t, primero(t, m["senales"], "senales"), clavesSenal, "senales[0]")
	claves(t, m["resumen"], clavesResumen, "resumen")
	assert.Equal(t, "2026-10-14", m["fecha"])
	assert.Equal(t, "2026-10-14T22:40:00Z", m["generado_at"])
	var visita map[string]any
	for _, a := range m["actividades"].([]any) {
		if v := a.(map[string]any)["visita"]; v != nil {
			visita = v.(map[string]any)
		}
	}
	require.NotNil(t, visita, "el escenario trae una visita")
	assert.Equal(t, "no_estaba", visita["estado"])
	assert.Equal(t, "nadie", visita["trato"])
	assert.Equal(t, "No estaba", visita["etiqueta"], "misma etiqueta que el teléfono")
	puntos := m["linea"].(map[string]any)["puntos"].([]any)
	pv, pp := puntos[0].(map[string]any), puntos[1].(map[string]any)
	assert.Nil(t, pv["importe"], "importe null en visitas")
	assert.Equal(t, "No se encontraba", pv["resultado"])
	assert.Equal(t, "150.00", pp["importe"])
	assert.Nil(t, pp["resultado"])
	assert.InDelta(t, 2, pp["num_fotos"], 0)
	assert.Len(t, m["linea"].(map[string]any)["por_hora"], 13, "horas 7..19")
}

func TestContrato_Actividad(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/usuarios/abc123/actividad?tipo=pago&tipo=visita&limit=1")
	fixture(t, "actividad_pagina1", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	claves(t, m, clavesPagina, "actividad")
	items := m["items"].([]any)
	require.Len(t, items, 1, "limit=1")
	revisarActividad(t, items[0], "items[0]")
	require.NotNil(t, m["siguiente_cursor"], "hay una segunda página")
	tot := claves(t, m["totales"], clavesTotales, "totales")
	claves(t, tot["por_tipo"], []string{"pago", "visita", "condonacion"}, "totales.por_tipo")
	assert.InDelta(t, 2, tot["n"], 0, "tipo es repetible (explode): pago Y visita")
	assert.Equal(t, "150.00", tot["importe"])

	a := items[0].(map[string]any)
	assert.Equal(t, "pago:15082369", a["id"])
	assert.Equal(t, "2026-10-14T16:31:05Z", a["recibido_at"])
	assert.Equal(t, "efectivo", a["pago"].(map[string]any)["forma_cobro"])
	assert.Equal(t, "Juan Carlos Méndez", a["registrado_por"].(map[string]any)["nombre"])
	assert.InDelta(t, 9921, a["docto_pv_id"], 0)
	assert.Equal(t, false, a["captura_tardia"], "5 s entre captura y llegada")

	// Segunda página con el cursor opaco: la visita, sin cursor siguiente.
	cuerpo2 := getCrudo(t, "/desempeno/usuarios/abc123/actividad?tipo=pago&tipo=visita&limit=1&cursor="+m["siguiente_cursor"].(string))
	fixture(t, "actividad_pagina2", cuerpo2)
	var m2 map[string]any
	require.NoError(t, json.Unmarshal(cuerpo2, &m2))
	items2 := m2["items"].([]any)
	require.Len(t, items2, 1)
	v := items2[0].(map[string]any)
	assert.Equal(t, "visita:412cc2fc-0000-4000-8000-000000000002", v["id"])
	assert.Nil(t, m2["siguiente_cursor"], "null explícito al final")
	assert.Nil(t, v["pago"])
	assert.Nil(t, v["lat"], "sin GPS → null")
	assert.Equal(t, false, v["con_gps"])
	assert.Equal(t, "abc123", v["registrado_por"].(map[string]any)["usuario_uid"], "visita sin CREATED_BY: uid resuelto por COBRADOR_ID único (D-API-4)")
}

// Filtros repetibles sin corchetes: un solo valor y varios valores.
func TestContrato_ActividadFiltrosRepetibles(t *testing.T) {
	t.Parallel()
	casos := []struct {
		query string
		n     float64
	}{
		{"tipo=visita", 1},
		{"tipo=pago&tipo=visita", 2},
		{"forma_cobro=efectivo", 1},
		{"forma_cobro=transferencia&forma_cobro=cheque", 0},
		{"tipo_visita=No+se+encontraba", 1},
		{"registrado_por=abc123", 2},
		{"registrado_por=oficina", 0},
		{"registrado_por=oficina&registrado_por=abc123", 2},
		{"con_fotos=true", 1},
		{"con_gps=false", 1},
		{"q=juana", 1},
		{"monto_min=100&monto_max=200", 1},
	}
	for _, c := range casos {
		m := get(t, "/desempeno/usuarios/abc123/actividad?"+c.query)
		assert.InDelta(t, c.n, m["totales"].(map[string]any)["n"], 0, c.query)
	}
}

func TestContrato_Catalogos(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/catalogos")
	fixture(t, "catalogos", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	claves(t, m, []string{"formas_cobro", "tipos_visita"}, "catalogos")
	claves(t, primero(t, m["formas_cobro"], "formas_cobro"), []string{"codigo", "etiqueta"}, "formas_cobro[0]")
	tv := claves(t, primero(t, m["tipos_visita"], "tipos_visita"), []string{"literal", "estado", "grupo"}, "tipos_visita[0]")
	assert.Equal(t, "No se encontraba", tv["literal"])
	assert.Equal(t, "no_estaba", tv["estado"])
	assert.Equal(t, "No estaba", tv["grupo"])
	assert.Len(t, m["formas_cobro"], 5)
	assert.Len(t, m["tipos_visita"], 14)
}

func TestContrato_Cuentas(t *testing.T) {
	t.Parallel()
	cuerpo := getCrudo(t, "/desempeno/usuarios/abc123/cuentas")
	fixture(t, "cuentas", cuerpo)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &m))
	claves(t, m, clavesCuentas, "cuentas")
	claves(t, m["conteos"], clavesConteos, "conteos")
	c := claves(t, primero(t, m["cuentas"], "cuentas"), clavesCuenta, "cuentas[0]")
	claves(t, c["ultima_actividad"], []string{"id", "tipo", "hora"}, "cuentas[0].ultima_actividad")
	assert.Equal(t, "pago", c["estado"])
	assert.Equal(t, "pagado", c["trato"])
	assert.Equal(t, "Pagó esta semana", c["etiqueta"])
	assert.Equal(t, "pagados", c["segmento"])
	assert.Equal(t, "16650.00", c["saldo"])
	assert.Equal(t, "2026-10-09T14:46:00Z", m["inicio_semana"])
}

// Regla §0 del contrato: null explícito, nunca se omite el campo.
func TestDTOs_SinOmitempty(t *testing.T) {
	t.Parallel()
	tipos := []any{
		desempenohttp.TableroDTO{},
		desempenohttp.UsuarioTableroDTO{},
		desempenohttp.JornadaDTO{},
		desempenohttp.ResumenJornadaDTO{},
		desempenohttp.ActividadDTO{},
		desempenohttp.PagoDTO{},
		desempenohttp.VisitaDTO{},
		desempenohttp.RegistradoPorDTO{},
		desempenohttp.LineaDelDiaDTO{},
		desempenohttp.TramoDTO{},
		desempenohttp.HoraDTO{},
		desempenohttp.PausaDTO{},
		desempenohttp.SenalDTO{},
		desempenohttp.PaginaActividadDTO{},
		desempenohttp.TotalesActividadDTO{},
		desempenohttp.CuentasDTO{},
		desempenohttp.CuentaDTO{},
		desempenohttp.PuntoDTO{},
		desempenohttp.CobroDTO{},
		desempenohttp.CatalogosDTO{},
	}
	for _, v := range tipos {
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			tag := rt.Field(i).Tag.Get("json")
			assert.NotContains(t, tag, "omitempty", "%s.%s", rt.Name(), rt.Field(i).Name)
			assert.NotEmpty(t, strings.Split(tag, ",")[0], "%s.%s sin nombre JSON", rt.Name(), rt.Field(i).Name)
		}
	}
}
