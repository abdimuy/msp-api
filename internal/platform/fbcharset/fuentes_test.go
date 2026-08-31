// Barrido de FUENTES: ningún destino de escaneo firebird.Win1252 puede existir
// en el repositorio sin estar clasificado.
//
// POR QUÉ HACE FALTA ADEMÁS DE LA PRUEBA DE CATÁLOGO
// ==================================================
// La prueba de catálogo (catalogo_integration_test.go) verifica lo que está EN
// el registro. No puede ver lo que alguien omitió registrar. Este barrido cierra
// la mitad más frecuente de ese hueco: lee el código fuente, encuentra cada
// declaración de un destino firebird.Win1252, y exige que venga anotada con la
// columna que lee y que esa columna esté registrada como CHARACTER SET NONE.
//
// El resultado: agregar `algoRaw firebird.Win1252` a una consulta obliga a
// escribir de qué columna sale, y esa columna queda entonces bajo la prueba de
// catálogo, que va a preguntarle a RDB$FIELDS si de verdad es NONE. Una eñe
// mal leída deja de poder entrar en silencio.
//
// QUÉ **NO** CUBRE — dicho sin adornos
// ====================================
//   - El caso inverso. Un `string` o `sql.NullString` es el destino de casi
//     todas las columnas del repositorio, así que no se puede distinguir por
//     sintaxis "leer una ISO8859_1 (bien)" de "leer una NONE en plano (mal)".
//     Agregar una columna NONE a una consulta y escanearla en plano NO lo
//     detecta este barrido. Lo detecta el control positivo del catálogo sólo si
//     la columna ya está registrada.
//   - Los archivos _test.go quedan fuera: usan firebird.Win1252 para construir
//     bytes de prueba, no para leer columnas.
//   - internal/platform/firebird queda fuera: ahí vive el tipo.
//
// PISO CONTRA EL BARRIDO VACÍO
// ============================
// Si el barrido encuentra menos de minimoDeSitios declaraciones, falla. Un
// refactor que rompa el recorrido de archivos (o un patrón que deje de
// coincidir) se manifiesta como fallo, no como una prueba verde que ya no mira
// nada. En el momento de escribirla había 24 sitios.
//
//nolint:misspell // vocabulario español por convención.
package fbcharset_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbcharset"
)

// minimoDeSitios es el piso de declaraciones firebird.Win1252 que el barrido
// tiene que encontrar para considerarse válido. Bájelo sólo si de verdad se
// eliminaron sitios, y diga cuáles en el commit.
const minimoDeSitios = 20

// declWin1252 encuentra una DECLARACIÓN cuyo tipo es firebird.Win1252:
//
//	nombreRaw     firebird.Win1252
//	var notaRaw   firebird.Win1252
//	estado        *firebird.Win1252
//
// Deja fuera las conversiones (`firebird.Win1252(x)`) y las menciones dentro de
// comentarios, que no son destinos de escaneo.
var declWin1252 = regexp.MustCompile(`^\s*(?:var\s+)?[A-Za-z_][A-Za-z0-9_]*(?:,\s*[A-Za-z_][A-Za-z0-9_]*)*\s+\*?firebird\.Win1252\s*(?://.*)?$`)

// refColumna reconoce un identificador "TABLA.COLUMNA" en mayúsculas dentro de
// un comentario. Es la anotación que el barrido exige.
var refColumna = regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})\.([A-Z][A-Z0-9_]{2,})\b`)

// La anotación se busca, en este orden:
//
//  1. En el comentario de la MISMA línea de la declaración. Si ahí hay una
//     referencia TABLA.COLUMNA, es la única que cuenta — es la que el autor
//     escribió para esa columna.
//  2. Si no hay ninguna en la línea, en el bloque de comentario contiguo justo
//     encima (saltando a lo sumo una línea de apertura `var (`).
//
// Deliberadamente NO se mira una ventana de N líneas: en un struct, la línea de
// arriba es OTRO campo con OTRA columna, y aceptarla haría que un campo sin
// anotar pasara con la anotación de su vecino. Es el error que tuvo la primera
// versión de este barrido.
const maxLineasDeComentario = 12

type sitio struct {
	archivo string
	linea   int
	texto   string
}

// archivosDeFuente devuelve los .go de producción bajo internal/, excluyendo
// pruebas y el paquete que define el tipo.
func archivosDeFuente(t *testing.T, raiz string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(raiz, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || strings.HasSuffix(path, filepath.Join("platform", "firebird")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	require.NoError(t, err, "recorriendo %s", raiz)
	sort.Strings(out)
	return out
}

// raizInternal ubica internal/ desde el directorio del paquete
// (internal/platform/fbcharset → ../..).
func raizInternal(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	raiz := filepath.Clean(filepath.Join(wd, "..", ".."))
	info, err := os.Stat(raiz)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, "internal", filepath.Base(raiz), "el barrido esperaba estar bajo internal/")
	return raiz
}

func TestBarridoDeFuentes_TodoWin1252EstaClasificado(t *testing.T) {
	t.Parallel()

	raiz := raizInternal(t)
	archivos := archivosDeFuente(t, raiz)
	require.NotEmpty(t, archivos, "el recorrido de %s no encontró archivos .go", raiz)

	porClave := fbcharset.PorClave()

	var sitios []sitio
	var sinAnotar []sitio
	var malClasificados []string

	for _, archivo := range archivos {
		contenido, err := os.ReadFile(archivo) //nolint:gosec // rutas derivadas del propio árbol del repo.
		require.NoError(t, err)
		lineas := strings.Split(string(contenido), "\n")

		for i, linea := range lineas {
			if !declWin1252.MatchString(linea) {
				continue
			}
			s := sitio{archivo: archivo, linea: i + 1, texto: strings.TrimSpace(linea)}
			sitios = append(sitios, s)

			var registradas []fbcharset.Columna
			var ajenas []fbcharset.Columna
			for _, clave := range clavesAnotadas(lineas, i) {
				col, ok := porClave[clave]
				if !ok {
					continue
				}
				if col.Familia == fbcharset.FamiliaNone {
					registradas = append(registradas, col)
					continue
				}
				ajenas = append(ajenas, col)
			}
			switch {
			case len(registradas) > 0:
				// Anotado y bien clasificado.
			case len(ajenas) > 0:
				malClasificados = append(malClasificados, formatoMalClasificado(s, ajenas[0]))
			default:
				sinAnotar = append(sinAnotar, s)
			}
		}
	}

	require.GreaterOrEqualf(t, len(sitios), minimoDeSitios,
		"el barrido encontró sólo %d declaraciones firebird.Win1252 bajo %s (piso: %d). "+
			"O se eliminaron sitios de verdad —y hay que bajar el piso a conciencia— o el "+
			"recorrido/patrón dejó de funcionar y esta prueba ya no está mirando nada.",
		len(sitios), raiz, minimoDeSitios)

	assert.Emptyf(t, sinAnotar,
		"hay destinos firebird.Win1252 sin columna clasificada:\n%s\n\n"+
			"firebird.Win1252 sólo es correcto para una columna CHARACTER SET NONE. "+
			"Anote la columna que lee —con la forma TABLA.COLUMNA— en el comentario de "+
			"la propia línea o en el bloque de comentario justo encima, y agréguela al "+
			"registro de internal/platform/fbcharset con su familia. "+
			"La prueba de catálogo verificará contra RDB$FIELDS que de verdad sea NONE.",
		formatoSitios(sinAnotar))

	assert.Emptyf(t, malClasificados,
		"hay destinos firebird.Win1252 sobre columnas que el registro NO clasifica como "+
			"CHARACTER SET NONE:\n%s\n\n"+
			"Sobre una columna transliterada, firebird.Win1252 aplica un SEGUNDO decode "+
			"y parte la Ñ en Ã‘.",
		strings.Join(malClasificados, "\n"))

	t.Logf("barrido: %d destinos firebird.Win1252 revisados en %d archivos", len(sitios), len(archivos))
}

// clavesAnotadas devuelve las referencias TABLA.COLUMNA que anotan la
// declaración en lineas[i], siguiendo la regla documentada arriba.
func clavesAnotadas(lineas []string, i int) []string {
	extraer := func(txt string) []string {
		var out []string
		for _, m := range refColumna.FindAllStringSubmatch(txt, -1) {
			out = append(out, m[1]+"."+m[2])
		}
		return out
	}

	if idx := strings.Index(lineas[i], "//"); idx >= 0 {
		if claves := extraer(lineas[i][idx:]); len(claves) > 0 {
			return claves
		}
	}

	// Bloque de comentario contiguo hacia arriba, tolerando una línea `var (`
	// entre la declaración y el comentario.
	j := i - 1
	if j >= 0 && strings.HasSuffix(strings.TrimSpace(lineas[j]), "var (") {
		j--
	}
	var claves []string
	for n := 0; j >= 0 && n < maxLineasDeComentario; j, n = j-1, n+1 {
		linea := strings.TrimSpace(lineas[j])
		if !strings.HasPrefix(linea, "//") {
			break
		}
		claves = append(claves, extraer(linea)...)
	}
	return claves
}

func formatoSitios(ss []sitio) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, "  "+s.archivo+":"+itoa(s.linea)+"  "+s.texto)
	}
	return strings.Join(out, "\n")
}

func formatoMalClasificado(s sitio, col fbcharset.Columna) string {
	return "  " + s.archivo + ":" + itoa(s.linea) + "  " + s.texto +
		"\n      " + col.Clave() + " está registrada como " + col.Familia.String() +
		" → destino correcto: " + col.Familia.DestinoDeEscaneo()
}

// itoa evita importar strconv sólo para esto.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// castWin1252 reconoce la OTRA forma legítima de leer una columna
// CHARACTER SET NONE: castearla en el propio SQL a un charset declarado, como
// hace cobranza/infra/ventfb con
// `CAST(dc.DESCRIPCION AS VARCHAR(200) CHARACTER SET WIN1252)`. Ahí el destino
// de escaneo es —correctamente— un string plano, porque quien transliterar es
// Firebird. Un barrido que sólo mirara `firebird.Win1252` daría ese sitio por
// roto.
var castWin1252 = regexp.MustCompile(`(?i)CAST\(\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?([A-Z][A-Z0-9_]+)\s+AS[^)]*CHARACTER\s+SET\s+WIN1252`)

// TestBarridoInverso_CadaColumnaNONETieneQuienLaDecodifique cierra la mitad
// aprovechable del hueco que el barrido de arriba admite.
//
// EL HUECO
// ========
// El barrido directo mira los destinos `firebird.Win1252` y exige que estén
// clasificados. No puede mirar el caso contrario —una columna NONE escaneada
// como `string` pelado— porque `string` es el destino de casi todo y no hay
// nada en la sintaxis que lo distinga. Comprobado rompiéndolo: cambiar un campo
// `firebird.Win1252` por `string` deja el barrido directo en verde.
//
// LO QUE ESTA PRUEBA SÍ PUEDE
// ===========================
// Para una columna que YA está en el registro como NONE, exige que exista al
// menos un sitio de producción que la decodifique: un destino
// `firebird.Win1252` anotado con ella, o un `CAST(... CHARACTER SET WIN1252)`
// sobre ella en el SQL. Si alguien cambia ese destino a `string`, el último
// sitio desaparece y esta prueba falla nombrando la columna.
//
// Lo que sigue SIN cubrir, y hay que decirlo: una columna NONE que nadie
// registró nunca. Para ésa no hay red; por eso el registro se escribe a mano y
// agregar una columna a una consulta obliga a registrarla.
func TestBarridoInverso_CadaColumnaNONETieneQuienLaDecodifique(t *testing.T) {
	t.Parallel()

	raiz := raizInternal(t)
	archivos := archivosDeFuente(t, raiz)
	require.NotEmpty(t, archivos)

	decodificada := map[string]bool{}
	for _, archivo := range archivos {
		contenido, err := os.ReadFile(archivo) //nolint:gosec // rutas del propio árbol.
		require.NoError(t, err)
		lineas := strings.Split(string(contenido), "\n")

		for i, linea := range lineas {
			if !declWin1252.MatchString(linea) {
				continue
			}
			for _, clave := range clavesAnotadas(lineas, i) {
				decodificada[clave] = true
			}
		}
		// El CAST nombra la columna pero no su tabla; se marca por sufijo.
		for _, m := range castWin1252.FindAllStringSubmatch(string(contenido), -1) {
			decodificada["*."+strings.ToUpper(m[1])] = true
		}
	}

	var huerfanas []string
	revisadas := 0
	for _, col := range fbcharset.Registro() {
		if col.Familia != fbcharset.FamiliaNone {
			continue
		}
		revisadas++
		if decodificada[col.Clave()] || decodificada["*."+col.Columna] {
			continue
		}
		huerfanas = append(huerfanas,
			"  "+col.Clave()+" — la leen: "+strings.Join(col.LeidoPor, ", "))
	}

	require.GreaterOrEqualf(t, revisadas, 10,
		"el registro sólo trae %d columnas CHARACTER SET NONE; con tan pocas esta "+
			"prueba no está mirando nada", revisadas)

	assert.Emptyf(t, huerfanas,
		"hay columnas registradas como CHARACTER SET NONE que ningún sitio de "+
			"producción decodifica:\n%s\n\n"+
			"O alguien cambió su destino de escaneo a un `string` pelado —y entonces "+
			"los bytes crudos Windows-1252 están entrando al dominio como UTF-8 "+
			"inválido— o la consulta dejó de leerlas y el registro quedó mintiendo. "+
			"Las dos cosas hay que arreglarlas: no basta con borrar la fila del registro "+
			"sin mirar la consulta.",
		strings.Join(huerfanas, "\n"))

	t.Logf("barrido inverso: %d columnas NONE, todas con sitio que las decodifica", revisadas)
}
