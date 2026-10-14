package domain_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Juego de casos compartido con msp-app-kt (contrato §7). La fuente es este
// repo; msp-app-kt guarda una copia byte a byte en
// core/common/src/test/resources/estado-cuenta/ y comprueba el MISMO VERSION.
const dirCasos = "../../../testdata/estado-cuenta"

var (
	nombreCaso = regexp.MustCompile(`^[0-9]{3}-[a-z0-9-]+\.json$`)
	hexVersion = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// listarCasos devuelve los .json en orden de bytes (= LC_ALL=C sort) y falla
// si hay cualquier archivo que no sea un caso bien nombrado o VERSION.
func listarCasos(dir string) ([]string, error) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var casos, intrusos []string
	for _, e := range entradas {
		n := e.Name()
		switch {
		case strings.HasPrefix(n, "."), n == "VERSION":
			continue
		case !e.IsDir() && nombreCaso.MatchString(n):
			casos = append(casos, n)
		default:
			intrusos = append(intrusos, n)
		}
	}
	if len(intrusos) > 0 {
		return nil, fmt.Errorf("archivos fuera de formato en %s: %v", dir, intrusos)
	}
	sort.Strings(casos)
	return casos, nil
}

func archivosDeCasos(t *testing.T, dir string) []string {
	t.Helper()
	casos, err := listarCasos(dir)
	require.NoError(t, err)
	return casos
}

func hashDeCasos(t *testing.T, dir string, casos []string) string {
	t.Helper()
	h := sha256.New()
	for _, n := range casos {
		b, err := os.ReadFile(filepath.Join(dir, n))
		require.NoError(t, err)
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func versionDeclarada(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	require.NoError(t, err)
	v := strings.TrimSuffix(string(b), "\n")
	require.Regexp(t, hexVersion, v, "VERSION debe ser 64 hex minúsculas con \\n final opcional")
	return v
}

func TestCasosEstadoCuenta_VersionCorrespondeAlHash(t *testing.T) {
	t.Parallel()
	casos := archivosDeCasos(t, dirCasos)
	require.GreaterOrEqual(t, len(casos), 25, "el juego exige al menos 25 casos")
	assert.Equal(t, versionDeclarada(t, dirCasos), hashDeCasos(t, dirCasos, casos),
		"cambió un caso sin actualizar VERSION: "+
			"cd testdata/estado-cuenta && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1 > VERSION")
}

// Control positivo del verificador: un byte de más en una copia cambia el hash.
func TestCasosEstadoCuenta_ControlPositivoDelHash(t *testing.T) {
	t.Parallel()
	casos := archivosDeCasos(t, dirCasos)
	tmp := t.TempDir()
	for i, n := range casos {
		b, err := os.ReadFile(filepath.Join(dirCasos, n))
		require.NoError(t, err)
		if i == 0 {
			b = append(b, ' ')
		}
		require.NoError(t, os.WriteFile(filepath.Join(tmp, n), b, 0o600))
	}
	assert.NotEqual(t, versionDeclarada(t, dirCasos), hashDeCasos(t, tmp, casos))
}

func TestCasosEstadoCuenta_ArchivoIntrusoSeRechaza(t *testing.T) {
	t.Parallel()
	for _, intruso := range []string{"1-caso.json", "README.md", "002-Con-Mayusculas.json", "003-con espacio.json"} {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "001-bueno.json"), []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(tmp, intruso), []byte("{}"), 0o600))
		_, err := listarCasos(tmp)
		require.Error(t, err, intruso)
		assert.Contains(t, err.Error(), intruso)
	}
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "001-bueno.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "VERSION"), []byte("x"), 0o600))
	casos, err := listarCasos(tmp)
	require.NoError(t, err, "control positivo: un caso bien nombrado y VERSION pasan")
	assert.Equal(t, []string{"001-bueno.json"}, casos)
}

// ─── Parseo estricto ─────────────────────────────────────────────────────────

type casoJSON struct {
	Nombre   string
	Ventana  domain.Ventana
	Cuentas  []domain.CuentaPeriodo
	Pagos    []domain.PagoPeriodo
	Visitas  []domain.VisitaPeriodo
	Esperado map[int]esperadoJSON
}

type esperadoJSON struct {
	Estado  domain.EstadoCuenta
	ConAlgo bool
}

// objeto decodifica un objeto JSON exigiendo EXACTAMENTE las claves dadas:
// una clave desconocida o una ausente es error (un campo mal escrito no puede
// leerse como null y dar verde por casualidad).
func objeto(raw json.RawMessage, donde string, claves ...string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: no es un objeto: %w", donde, err)
	}
	permitidas := map[string]bool{}
	for _, c := range claves {
		permitidas[c] = true
		if _, ok := m[c]; !ok {
			return nil, fmt.Errorf("%s: falta la clave %q", donde, c)
		}
	}
	for c := range m {
		if !permitidas[c] {
			return nil, fmt.Errorf("%s: clave desconocida %q", donde, c)
		}
	}
	return m, nil
}

func lista(raw json.RawMessage, donde string) ([]json.RawMessage, error) {
	var l []json.RawMessage
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s: no es una lista: %w", donde, err)
	}
	return l, nil
}

func cadena(raw json.RawMessage, donde string) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%s: debe ser string: %w", donde, err)
	}
	return s, nil
}

func cadenaONula(raw json.RawMessage, donde string) (*string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil //nolint:nilnil // null explícito es un valor válido del contrato.
	}
	s, err := cadena(raw, donde)
	return &s, err
}

func entero(raw json.RawMessage, donde string) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("%s: debe ser entero: %w", donde, err)
	}
	return n, nil
}

func booleano(raw json.RawMessage, donde string) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("%s: debe ser booleano: %w", donde, err)
	}
	return b, nil
}

// dineroJSON: SOLO desde string; un número JSON es error.
func dineroJSON(raw json.RawMessage, donde string) (decimal.Decimal, error) {
	s, err := cadena(raw, donde+" (dinero como string decimal, no como número JSON)")
	if err != nil {
		return decimal.Decimal{}, err
	}
	return decimal.NewFromString(s)
}

func instante(raw json.RawMessage, donde string) (time.Time, error) {
	s, err := cadena(raw, donde)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, s)
}

//nolint:gocognit,cyclop,funlen // parseo lineal de un formato fijo; partirlo esconde el contrato.
func parsearCaso(archivo string, texto []byte) (casoJSON, error) {
	var c casoJSON
	raiz, err := objeto(texto, archivo, "nombre", "ventana", "cuentas", "pagos", "visitas", "esperado")
	if err != nil {
		return c, err
	}
	if c.Nombre, err = cadena(raiz["nombre"], archivo+".nombre"); err != nil {
		return c, err
	}
	vent, err := objeto(raiz["ventana"], archivo+".ventana", "inicio", "fin")
	if err != nil {
		return c, err
	}
	ini, err := instante(vent["inicio"], archivo+".ventana.inicio")
	if err != nil {
		return c, err
	}
	fin, err := instante(vent["fin"], archivo+".ventana.fin")
	if err != nil {
		return c, err
	}
	if c.Ventana, err = domain.NuevaVentana(ini, fin); err != nil {
		return c, err
	}

	cuentas, err := lista(raiz["cuentas"], archivo+".cuentas")
	if err != nil {
		return c, err
	}
	vistas := map[int]bool{}
	for i, raw := range cuentas {
		donde := fmt.Sprintf("%s.cuentas[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "cliente_id", "parcialidad")
		if err != nil {
			return c, err
		}
		var cp domain.CuentaPeriodo
		if cp.VentaID, err = entero(m["venta_id"], donde+".venta_id"); err != nil {
			return c, err
		}
		if cp.ClienteID, err = entero(m["cliente_id"], donde+".cliente_id"); err != nil {
			return c, err
		}
		if cp.Parcialidad, err = dineroJSON(m["parcialidad"], donde+".parcialidad"); err != nil {
			return c, err
		}
		if vistas[cp.VentaID] {
			return c, fmt.Errorf("%s: venta_id %d repetido en cuentas", archivo, cp.VentaID)
		}
		vistas[cp.VentaID] = true
		c.Cuentas = append(c.Cuentas, cp)
	}

	pagos, err := lista(raiz["pagos"], archivo+".pagos")
	if err != nil {
		return c, err
	}
	for i, raw := range pagos {
		donde := fmt.Sprintf("%s.pagos[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "importe", "forma_cobro_id", "fecha_hora", "rechazado")
		if err != nil {
			return c, err
		}
		var p domain.PagoPeriodo
		if p.VentaID, err = entero(m["venta_id"], donde+".venta_id"); err != nil {
			return c, err
		}
		if p.Importe, err = dineroJSON(m["importe"], donde+".importe"); err != nil {
			return c, err
		}
		if p.FormaCobroID, err = entero(m["forma_cobro_id"], donde+".forma_cobro_id"); err != nil {
			return c, err
		}
		if p.FechaHora, err = instante(m["fecha_hora"], donde+".fecha_hora"); err != nil {
			return c, err
		}
		if p.Rechazado, err = booleano(m["rechazado"], donde+".rechazado"); err != nil {
			return c, err
		}
		p.ActividadID = fmt.Sprintf("pago:%d", i)
		c.Pagos = append(c.Pagos, p)
	}

	visitas, err := lista(raiz["visitas"], archivo+".visitas")
	if err != nil {
		return c, err
	}
	for i, raw := range visitas {
		donde := fmt.Sprintf("%s.visitas[%d]", archivo, i)
		m, err := objeto(raw, donde, "cliente_id", "venta_id", "tipo_visita", "fecha_hora",
			"fecha_cita", "hora_cita", "fecha_promesa")
		if err != nil {
			return c, err
		}
		var v domain.VisitaPeriodo
		if v.ClienteID, err = entero(m["cliente_id"], donde+".cliente_id"); err != nil {
			return c, err
		}
		if !bytes.Equal(bytes.TrimSpace(m["venta_id"]), []byte("null")) {
			id, err := entero(m["venta_id"], donde+".venta_id")
			if err != nil {
				return c, err
			}
			v.VentaID = &id
		}
		if v.TipoVisita, err = cadena(m["tipo_visita"], donde+".tipo_visita"); err != nil {
			return c, err
		}
		if v.FechaHora, err = instante(m["fecha_hora"], donde+".fecha_hora"); err != nil {
			return c, err
		}
		for clave, dst := range map[string]**domain.Fecha{"fecha_cita": &v.FechaCita, "fecha_promesa": &v.FechaPromesa} {
			s, err := cadenaONula(m[clave], donde+"."+clave)
			if err != nil {
				return c, err
			}
			if s != nil {
				f, err := domain.ParseFecha(*s)
				if err != nil {
					return c, fmt.Errorf("%s.%s: %w", donde, clave, err)
				}
				*dst = &f
			}
		}
		hs, err := cadenaONula(m["hora_cita"], donde+".hora_cita")
		if err != nil {
			return c, err
		}
		if hs != nil {
			h, err := domain.ParseHoraDelDia(*hs)
			if err != nil {
				return c, fmt.Errorf("%s.hora_cita: %w", donde, err)
			}
			v.HoraCita = &h
		}
		v.ActividadID = fmt.Sprintf("visita:%d", i)
		c.Visitas = append(c.Visitas, v)
	}

	esperados, err := lista(raiz["esperado"], archivo+".esperado")
	if err != nil {
		return c, err
	}
	c.Esperado = map[int]esperadoJSON{}
	for i, raw := range esperados {
		donde := fmt.Sprintf("%s.esperado[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "estado", "con_algo")
		if err != nil {
			return c, err
		}
		id, err := entero(m["venta_id"], donde+".venta_id")
		if err != nil {
			return c, err
		}
		cod, err := cadena(m["estado"], donde+".estado")
		if err != nil {
			return c, err
		}
		estado, err := domain.ParseEstadoCuenta(cod)
		if err != nil {
			return c, fmt.Errorf("%s.estado: estado desconocido %q", donde, cod)
		}
		conAlgo, err := booleano(m["con_algo"], donde+".con_algo")
		if err != nil {
			return c, err
		}
		if _, dup := c.Esperado[id]; dup {
			return c, fmt.Errorf("%s: esperado repite venta_id %d", archivo, id)
		}
		c.Esperado[id] = esperadoJSON{Estado: estado, ConAlgo: conAlgo}
	}
	if len(c.Esperado) != len(c.Cuentas) {
		return c, fmt.Errorf("%s: esperado debe traer exactamente una entrada por cuenta", archivo)
	}
	for _, cp := range c.Cuentas {
		if _, ok := c.Esperado[cp.VentaID]; !ok {
			return c, fmt.Errorf("%s: esperado no trae la venta %d", archivo, cp.VentaID)
		}
	}
	return c, nil
}

func cargarCasos(t *testing.T) map[string]casoJSON {
	t.Helper()
	out := map[string]casoJSON{}
	for _, n := range archivosDeCasos(t, dirCasos) {
		b, err := os.ReadFile(filepath.Join(dirCasos, n))
		require.NoError(t, err)
		c, err := parsearCaso(n, b)
		require.NoError(t, err)
		out[n] = c
	}
	return out
}

func TestCasosEstadoCuenta_CadaCasoContraElDeriver(t *testing.T) {
	t.Parallel()
	for archivo, c := range cargarCasos(t) {
		t.Run(archivo, func(t *testing.T) {
			t.Parallel()
			d := domain.Derivar(c.Cuentas, c.Pagos, c.Visitas, c.Ventana)
			require.Len(t, d.PorVenta, len(c.Cuentas), c.Nombre)
			for ventaID, e := range c.Esperado {
				r := d.PorVenta[ventaID]
				assert.Equal(t, e.Estado, r.Estado, "%s — venta %d: estado", c.Nombre, ventaID)
				assert.Equal(t, e.ConAlgo, r.ConAlgo, "%s — venta %d: con_algo", c.Nombre, ventaID)
			}
		})
	}
}

// Cobertura mínima exigida por el plan Android (Task 4) y por el spec §7.
func TestCasosEstadoCuenta_CoberturaMinimaDelJuego(t *testing.T) {
	t.Parallel()
	estados := map[domain.EstadoCuenta]bool{}
	conAlgo := map[bool]bool{}
	var rechazado, condonacion, bordeExacto, parcialidadCero, fueraDeCatalogo, alcanceCliente, sinVenta bool
	for _, c := range cargarCasos(t) {
		for _, e := range c.Esperado {
			estados[e.Estado] = true
			conAlgo[e.ConAlgo] = true
		}
		for _, p := range c.Pagos {
			rechazado = rechazado || p.Rechazado
			condonacion = condonacion || p.FormaCobroID == domain.FormaCobroCondonacion
			for _, cp := range c.Cuentas {
				if cp.VentaID == p.VentaID && p.Importe.Equal(cp.Parcialidad) && !p.Rechazado &&
					domain.EsFormaDeDinero(p.FormaCobroID) {
					bordeExacto = true
				}
			}
		}
		for _, cp := range c.Cuentas {
			parcialidadCero = parcialidadCero || cp.Parcialidad.IsZero()
		}
		for _, v := range c.Visitas {
			fueraDeCatalogo = fueraDeCatalogo || !domain.EsTipoVisitaConocido(v.TipoVisita)
			alcanceCliente = alcanceCliente || domain.AlcanceDeTipoVisita(v.TipoVisita, v.FechaCita != nil) == domain.AlcanceCliente
			sinVenta = sinVenta || v.VentaID == nil
		}
	}
	for _, e := range domain.EstadosCuenta() {
		assert.True(t, estados[e], "ningún caso espera el estado %s", e)
	}
	assert.True(t, conAlgo[true] && conAlgo[false], "faltan casos con con_algo true y false")
	assert.True(t, rechazado, "falta un pago rechazado")
	assert.True(t, condonacion, "falta una condonación (forma 137026)")
	assert.True(t, bordeExacto, "falta el borde exacto abono == parcialidad")
	assert.True(t, parcialidadCero, "falta parcialidad 0")
	assert.True(t, fueraDeCatalogo, "falta un literal fuera de catálogo")
	assert.True(t, alcanceCliente, "falta una visita de alcance cliente")
	assert.True(t, sinVenta, "falta una visita sin venta ligada")
}

// ─── El parseo estricto se prueba a sí mismo ─────────────────────────────────

const casoValido = `{ "nombre": "x",
  "ventana": { "inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-14T22:40:00Z" },
  "cuentas": [ { "venta_id": 1, "cliente_id": 10, "parcialidad": "150.00" } ],
  "pagos":   [ { "venta_id": 1, "importe": "150.00", "forma_cobro_id": 157,
                 "fecha_hora": "2026-10-10T15:00:00Z", "rechazado": false } ],
  "visitas": [ { "cliente_id": 10, "venta_id": null, "tipo_visita": "No se encontraba",
                 "fecha_hora": "2026-10-11T16:00:00Z", "fecha_cita": null, "hora_cita": null,
                 "fecha_promesa": null } ],
  "esperado": [ { "venta_id": 1, "estado": "pago", "con_algo": true } ] }`

func TestParsearCaso_Estricto(t *testing.T) {
	t.Parallel()
	_, err := parsearCaso("999-valido.json", []byte(casoValido))
	require.NoError(t, err, "control positivo: el caso válido se parsea")

	malos := []struct{ nombre, de, a, menciona string }{
		{"dinero como número", `"importe": "150.00"`, `"importe": 150.0`, "importe"},
		{"clave desconocida", `"fecha_cita": null`, `"fecha_cit": null`, "fecha_cit"},
		{"clave ausente", `, "rechazado": false`, ``, "rechazado"},
		{"estado desconocido", `"estado": "pago"`, `"estado": "PAGO"`, "PAGO"},
		{"esperado incompleto", `[ { "venta_id": 1, "estado": "pago", "con_algo": true } ]`, `[]`, "esperado"},
		{"rechazado no booleano", `"rechazado": false`, `"rechazado": "false"`, "rechazado"},
		{"ventana invertida", `"fin": "2026-10-14T22:40:00Z"`, `"fin": "2026-10-01T00:00:00Z"`, "parámetro"},
	}
	for _, m := range malos {
		texto := strings.Replace(casoValido, m.de, m.a, 1)
		require.NotEqual(t, casoValido, texto, "control: el reemplazo de %q ocurrió", m.nombre)
		_, err := parsearCaso("999-malo.json", []byte(texto))
		require.Error(t, err, m.nombre)
		assert.Contains(t, err.Error(), m.menciona, m.nombre)
	}
}
