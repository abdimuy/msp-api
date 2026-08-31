// Controles positivos de codificación para los catálogos que lee microsipfb.
//
// POR QUÉ ESTE ARCHIVO
// ====================
// Las pruebas que ya existían en este paquete afirmaban `NotEmpty(a.Nombre)`.
// Eso pasa igual con el nombre correcto y con el nombre corrompido: sobre ASCII
// puro las dos lecturas coinciden byte a byte, y "MUÃ‘OZ" tampoco está vacío.
// Durante meses ese fue exactamente el estado del repositorio.
//
// Cada prueba de aquí compara lo que devuelve el repositorio contra el nombre
// VERDADERO del catálogo —leído con un CAST que no depende del destino de
// escaneo del repositorio— y EXIGE que el barrido haya pasado por al menos una
// fila con acentos. Sin ese requisito, una base sin eñes volvería a dejar pasar
// el defecto en silencio.
//
// Charsets en juego (verificados contra RDB$FIELDS por
// internal/platform/fbcharset):
//
//	CIUDADES.NOMBRE          ISO8859_1 → string
//	ESTADOS.NOMBRE           ISO8859_1 → sql.NullString
//	ALMACENES.NOMBRE         ISO8859_1 → string
//	ARTICULOS.NOMBRE         ISO8859_1 → string
//	LINEAS_ARTICULOS.NOMBRE  ISO8859_1 → string
//	COBRADORES.NOMBRE        ISO8859_1 → string
//	ZONAS_CLIENTES.NOMBRE    NONE      → firebird.Win1252
//
//nolint:misspell // vocabulario español por convención.
package microsipfb_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// mojibake es la firma del doble-decode: la Ñ (UTF-8 C3 91) releída como
// Windows-1252 se vuelve "Ã‘". Buscar el prefijo "Ã" alcanza para reconocerlo
// en cualquier acento del español.
const mojibake = "Ã"

// exigeSinMojibake falla con un mensaje que nombra la columna cuando el valor
// trae la firma del doble-decode o no es UTF-8 válido.
func exigeSinMojibake(t *testing.T, columna, valor string) {
	t.Helper()
	assert.Truef(t, utf8.ValidString(valor),
		"%s devolvió bytes que no son UTF-8 válido (%q). Si la columna es CHARACTER SET NONE "+
			"hay que leerla con firebird.Win1252.", columna, valor)
	assert.NotContainsf(t, valor, mojibake,
		"%s devolvió %q — la \"Ã\" es la firma del doble-decode. La columna es ISO8859_1: "+
			"Firebird ya la transliteró y hay que escanearla como string, no con firebird.Win1252.",
		columna, valor)
}

func TestCiudadRepo_Listar_NombresAcentuadosIntactos(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewCiudadRepo(pool)

	ctx := context.Background()
	ciudades, err := repo.Listar(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, ciudades, "CIUDADES vacío: la prueba no verificaría nada")

	q := firebird.GetQuerier(ctx, pool.DB)
	verdaderos := fbtestutil.NombresDeCatalogo(ctx, t, q, "CIUDADES", "CIUDAD_ID", "NOMBRE")
	estados := fbtestutil.NombresDeCatalogo(ctx, t, q, "ESTADOS", "ESTADO_ID", "NOMBRE")

	acentuadas := 0
	for _, c := range ciudades {
		esperado, ok := verdaderos[c.ID]
		require.Truef(t, ok, "la ciudad %d no está en el catálogo", c.ID)
		assert.Equalf(t, strings.TrimSpace(esperado), c.Nombre,
			"CIUDADES.NOMBRE del id %d", c.ID)
		exigeSinMojibake(t, "CIUDADES.NOMBRE", c.Nombre)
		if fbtestutil.ContieneNoASCII(esperado) {
			acentuadas++
		}
		if c.EstadoID != 0 && c.Estado != "" {
			assert.Equal(t, strings.TrimSpace(estados[c.EstadoID]), c.Estado, "ESTADOS.NOMBRE")
			exigeSinMojibake(t, "ESTADOS.NOMBRE", c.Estado)
		}
	}

	require.Positivef(t, acentuadas,
		"ninguna de las %d ciudades del catálogo trae caracteres no-ASCII. "+
			"Sobre ASCII puro esta comparación pasa con el defecto puesto: "+
			"corra contra la base de desarrollo completa (trae \"CAÑADA MORELOS\").",
		len(ciudades))
	t.Logf("ciudades verificadas: %d, de ellas %d con acentos", len(ciudades), acentuadas)
}

func TestAlmacenRepo_Listar_NombresAcentuadosIntactos(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42})

	ctx := context.Background()
	almacenes, err := repo.Listar(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, almacenes)

	q := firebird.GetQuerier(ctx, pool.DB)
	verdaderos := fbtestutil.NombresDeCatalogo(ctx, t, q, "ALMACENES", "ALMACEN_ID", "NOMBRE")

	acentuados := 0
	for _, a := range almacenes {
		assert.Equalf(t, verdaderos[a.ID], a.Nombre, "ALMACENES.NOMBRE del id %d", a.ID)
		exigeSinMojibake(t, "ALMACENES.NOMBRE", a.Nombre)
		if fbtestutil.ContieneNoASCII(verdaderos[a.ID]) {
			acentuados++
		}
	}
	require.Positivef(t, acentuados,
		"ninguno de los %d almacenes visibles trae acentos; la comparación no prueba nada. "+
			"La base de desarrollo trae \"Traspaso sucursal - Mercancía en tránsito\".",
		len(almacenes))
}

// TestZonaRepo_Listar_NombreDeZonaEsElDelCatalogo compara el nombre de zona
// contra el catálogo. NO es un control positivo y hay que decirlo: hoy ninguna
// de las 46 zonas ni ninguno de los 52 cobradores trae caracteres no-ASCII, así
// que esta comparación pasa igual con el destino de escaneo equivocado. Se
// queda porque es la red que se activa el día que alguien capture una zona con
// acento; la red real para estas dos columnas es la prueba de catálogo
// (internal/platform/fbcharset), que pregunta el charset en vez de mirar datos.
func TestZonaRepo_Listar_NombreDeZonaEsElDelCatalogo(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewZonaRepo(pool)

	ctx := context.Background()
	zonas, err := repo.Listar(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, zonas)

	q := firebird.GetQuerier(ctx, pool.DB)
	zonasVerdaderas := fbtestutil.NombresDeCatalogo(ctx, t, q, "ZONAS_CLIENTES", "ZONA_CLIENTE_ID", "NOMBRE")

	for _, z := range zonas {
		// Listar concatena " - <cobrador>" cuando la zona tiene uno, así que
		// el nombre de la zona es el prefijo.
		base, _, _ := strings.Cut(z.Nombre, " - ")
		assert.Equalf(t, zonasVerdaderas[z.ID], base,
			"ZONAS_CLIENTES.NOMBRE del id %d (CHARACTER SET NONE: se lee con firebird.Win1252)", z.ID)
		exigeSinMojibake(t, "ZONAS_CLIENTES.NOMBRE + COBRADORES.NOMBRE", z.Nombre)
	}
}

// TestAlmacenRepo_ListarArticulos_BuscaPorAcento cubre el camino de ESCRITURA.
//
// El término de búsqueda viaja como parámetro de `ARTICULOS.NOMBRE CONTAINING ?`.
// Mientras se re-codificaba a bytes Windows-1252 antes de mandarlo, buscar algo
// acentuado reventaba con «SQL error code = -303 / Malformed string»: los bytes
// no eran legibles para una sesión UTF-8. Medido revirtiendo el arreglo.
//
// Ninguna prueba lo veía porque las que existían buscaban con el término vacío.
func TestAlmacenRepo_ListarArticulos_BuscaPorAcento(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewAlmacenRepo(pool, []int{42})

	ctx := context.Background()
	q := firebird.GetQuerier(ctx, pool.DB)

	// El fragmento acentuado sale del propio catálogo, no de una constante:
	// así la prueba no depende de que exista un artículo concreto.
	var fragmento string
	nombres := fbtestutil.NombresDeCatalogo(ctx, t, q, "ARTICULOS", "ARTICULO_ID", "NOMBRE")
	for _, n := range nombres {
		if idx := strings.IndexFunc(n, func(r rune) bool { return r > 127 }); idx >= 0 {
			// Tres runas alrededor del acento, para que el CONTAINING sea
			// selectivo pero siga existiendo en varias filas.
			runas := []rune(n[idx:])
			if len(runas) >= 3 {
				fragmento = string(runas[:3])
			} else {
				fragmento = string(runas)
			}
			break
		}
	}
	require.NotEmptyf(t, fragmento,
		"ningún artículo del catálogo trae acentos; sin eso esta prueba no ejercita nada")

	almacenes, err := repo.Listar(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, almacenes)

	// Cuántos artículos contienen el fragmento, según el propio catálogo. Es la
	// cota superior: ListarArticulos además filtra por existencias > 0.
	enCatalogo := 0
	for _, n := range nombres {
		if strings.Contains(n, fragmento) {
			enCatalogo++
		}
	}
	require.Positive(t, enCatalogo)

	encontrados := 0
	for _, a := range almacenes {
		arts, err := repo.ListarArticulos(ctx, a.ID, fragmento)
		require.NoErrorf(t, err, "buscar %q en el almacén %d", fragmento, a.ID)
		for _, art := range arts {
			assert.Containsf(t, art.Articulo, fragmento,
				"CONTAINING %q devolvió %q", fragmento, art.Articulo)
			exigeSinMojibake(t, "ARTICULOS.NOMBRE", art.Articulo)
			exigeSinMojibake(t, "LINEAS_ARTICULOS.NOMBRE", art.LineaArticulo)
			encontrados++
		}
	}

	require.Positivef(t, encontrados,
		"buscar el fragmento acentuado %q no devolvió NINGÚN artículo, aunque %d filas del "+
			"catálogo lo contienen. Eso es exactamente el síntoma de re-codificar el término "+
			"de búsqueda a Windows-1252 antes de mandarlo por una conexión charset=UTF8.",
		fragmento, enCatalogo)
	t.Logf("buscando %q: %d artículos con existencias devueltos (%d en el catálogo)",
		fragmento, encontrados, enCatalogo)
}
