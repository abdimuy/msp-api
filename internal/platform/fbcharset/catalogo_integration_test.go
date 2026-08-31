// Pruebas de BARRIDO sobre el catálogo real de Firebird para la clasificación
// de charset de las columnas de texto de Microsip.
//
// QUÉ CUBREN
// ==========
//
//  1. TestCatalogoCharsets_ClasificacionDeColumnas
//     Para CADA columna del registro, lee su charset de RDB$RELATION_FIELDS ×
//     RDB$FIELDS × RDB$CHARACTER_SETS y verifica que la familia declarada por
//     el código coincida. Atrapa: una columna leída con firebird.Win1252 siendo
//     ISO8859_1 (el defecto que motivó todo esto), una columna leída en plano
//     siendo NONE (el defecto inverso), y un cambio de esquema de Microsip que
//     mueva una columna de familia sin que nadie se entere.
//
//  2. TestCatalogoCharsets_ControlPositivoPorFamilia
//     No se conforma con el metadato: LEE datos reales y comprueba el
//     comportamiento. Para cada columna del registro que tenga al menos una
//     fila con bytes no-ASCII, verifica que el escaneo plano produzca UTF-8
//     VÁLIDO si la familia es transliterada, e INVÁLIDO si la familia es NONE.
//     Esa es la diferencia observable entre las dos familias, y es la que
//     rompe cuando alguien elige mal el destino de escaneo.
//
//  3. TestCatalogoCharsets_ElNombreNoPredice
//     Fija el hecho que hace peligroso el atajo mental: dos columnas llamadas
//     igual (NOMBRE) están en familias distintas.
//
// QUÉ **NO** CUBREN
// =================
//   - No descubren columnas por sí solas. El registro es una lista escrita a
//     mano; una consulta nueva que lea una columna nueva SIN registrarla no se
//     detecta aquí. Lo que sí se detecta es el caso frecuente —agregar un
//     destino firebird.Win1252— porque el barrido de fuentes
//     (TestBarridoDeFuentes_TodoWin1252EstaClasificado, sin base de datos)
//     exige que todo destino Win1252 del repositorio esté registrado como NONE,
//     y el barrido inverso
//     (TestBarridoInverso_CadaColumnaNONETieneQuienLaDecodifique) exige que toda
//     columna YA registrada como NONE conserve un sitio que la decodifique.
//     El hueco que queda es una columna NONE que nadie registró nunca y se lee
//     como string plano: para ésa no hay red.
//   - No verifican expresiones (COALESCE, CAST, LIST, SUBSTRING). Firebird
//     resuelve el charset de una expresión con sus propias reglas; la única
//     que este repositorio depende —COALESCE(ISO8859_1, NONE) resuelve a
//     ISO8859_1— está fijada por su propia prueba en clientesfb.
//   - El control positivo se salta las columnas cuyo dato es todo ASCII (hoy
//     ZONAS_CLIENTES.NOMBRE y DIRS_CLIENTES.TELEFONO1). Ahí la clasificación
//     del catálogo es la única red, y por eso la prueba 1 no admite excusas.
//
// PISO CONTRA EL BARRIDO VACÍO
// ============================
// Las tres pruebas exigen un mínimo de columnas verificadas
// (fbcharset.MinimoDeColumnas) y que TODAS las del registro se resuelvan
// contra el catálogo. Un barrido que no encuentre nada falla en vez de pasar.
//
//nolint:paralleltest // serie por diseño: comparten el pool y la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package fbcharset_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbcharset"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// charsetsDelCatalogo lee el charset declarado de cada columna de texto de las
// tablas del registro. Devuelve un mapa "TABLA.COLUMNA" → nombre del charset.
//
// El filtro por RDB$FIELD_TYPE deja pasar CHAR (14), VARCHAR (37) y BLOB (261);
// una columna del registro que no sea de texto no aparecería, y la prueba lo
// reporta como "no encontrada" en vez de darla por buena.
func charsetsDelCatalogo(ctx context.Context, t *testing.T, q firebird.Querier, tablas []string) map[string]string {
	t.Helper()

	marcadores := make([]string, len(tablas))
	args := make([]any, len(tablas))
	for i, tabla := range tablas {
		marcadores[i] = "?"
		args[i] = tabla
	}

	consulta := `
SELECT TRIM(rf.RDB$RELATION_NAME),
       TRIM(rf.RDB$FIELD_NAME),
       TRIM(COALESCE(cs.RDB$CHARACTER_SET_NAME, 'SIN_CHARSET'))
FROM RDB$RELATION_FIELDS rf
JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
LEFT JOIN RDB$CHARACTER_SETS cs ON cs.RDB$CHARACTER_SET_ID = f.RDB$CHARACTER_SET_ID
WHERE f.RDB$FIELD_TYPE IN (14, 37, 261)
  AND TRIM(rf.RDB$RELATION_NAME) IN (` + strings.Join(marcadores, ",") + `)`

	rows, err := q.QueryContext(ctx, consulta, args...)
	require.NoError(t, err, "leyendo RDB$RELATION_FIELDS")
	defer func() { _ = rows.Close() }()

	out := make(map[string]string)
	for rows.Next() {
		var tabla, columna, charset string
		require.NoError(t, rows.Scan(&tabla, &columna, &charset))
		out[tabla+"."+columna] = charset
	}
	require.NoError(t, rows.Err())
	return out
}

// tablasDelRegistro devuelve los nombres de tabla únicos del registro.
func tablasDelRegistro() []string {
	vistas := map[string]struct{}{}
	var out []string
	for _, c := range fbcharset.Registro() {
		if _, ok := vistas[c.Tabla]; ok {
			continue
		}
		vistas[c.Tabla] = struct{}{}
		out = append(out, c.Tabla)
	}
	sort.Strings(out)
	return out
}

func TestCatalogoCharsets_ClasificacionDeColumnas(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		registro := fbcharset.Registro()
		require.GreaterOrEqualf(t, len(registro), fbcharset.MinimoDeColumnas(),
			"el registro trae %d columnas, menos que el piso de %d — un barrido casi vacío no puede pasar por bueno",
			len(registro), fbcharset.MinimoDeColumnas())

		catalogo := charsetsDelCatalogo(ctx, t, q, tablasDelRegistro())
		require.NotEmpty(t, catalogo, "RDB$RELATION_FIELDS no devolvió nada: la consulta del barrido está rota, no el esquema")

		verificadas := 0
		for _, col := range registro {
			charset, ok := catalogo[col.Clave()]
			require.Truef(t, ok,
				"%s está en el registro pero no existe como columna de texto en el catálogo. "+
					"O la renombraron en Microsip, o el registro tiene una errata. La leen: %s",
				col.Clave(), strings.Join(col.LeidoPor, ", "))

			familiaReal, err := fbcharset.FamiliaDeCharset(charset)
			require.NoErrorf(t, err, "%s tiene charset %q", col.Clave(), charset)

			assert.Equalf(t, col.Familia, familiaReal,
				"\n%s es CHARACTER SET %s → familia %s.\n"+
					"El código la trata como %s y por lo tanto la escanea con %s.\n"+
					"Destino correcto: %s.\n"+
					"La leen: %s.",
				col.Clave(), charset, familiaReal,
				col.Familia, col.Familia.DestinoDeEscaneo(),
				familiaReal.DestinoDeEscaneo(),
				strings.Join(col.LeidoPor, ", "))
			verificadas++
		}

		require.Equalf(t, len(registro), verificadas,
			"se verificaron %d de %d columnas del registro", verificadas, len(registro))
	})
}

// hayNoASCII indica si b contiene algún byte fuera de ASCII.
func hayNoASCII(b []byte) bool {
	for _, c := range b {
		if c > 127 {
			return true
		}
	}
	return false
}

// primeraFilaNoASCII devuelve los bytes de la primera fila de tabla.columna que
// contenga al menos un byte no-ASCII, o nil si ninguna de las primeras `limite`
// filas lo hace.
//
// La búsqueda se hace en Go, no en SQL, a propósito: un predicado SQL sobre una
// columna CHARACTER SET NONE con un literal UTF-8 revienta con "Malformed
// string", que es justo el comportamiento que la prueba está estudiando.
func primeraFilaNoASCII(ctx context.Context, t *testing.T, q firebird.Querier, tabla, columna string, limite int) []byte {
	t.Helper()
	//nolint:gosec // tabla/columna vienen del registro de este paquete, no de entrada externa.
	consulta := fmt.Sprintf("SELECT FIRST %d %s FROM %s WHERE %s IS NOT NULL", limite, columna, tabla, columna)
	rows, err := q.QueryContext(ctx, consulta)
	require.NoErrorf(t, err, "leyendo %s.%s", tabla, columna)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var v []byte
		require.NoError(t, rows.Scan(&v))
		if hayNoASCII(v) {
			return v
		}
	}
	require.NoError(t, rows.Err())
	return nil
}

func TestCatalogoCharsets_ControlPositivoPorFamilia(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		// Cuántas filas mirar antes de rendirse en una columna. Suficiente para
		// que las columnas con acentos aparezcan y acotado para que la prueba
		// no barra tablas de millones de filas.
		const limiteDeFilas = 50000

		conDatosAcentuados := 0
		var sinDatos []string

		for _, col := range fbcharset.Registro() {
			bytes := primeraFilaNoASCII(ctx, t, q, col.Tabla, col.Columna, limiteDeFilas)
			if bytes == nil {
				sinDatos = append(sinDatos, col.Clave())
				continue
			}
			conDatosAcentuados++

			valido := utf8.Valid(bytes)
			switch col.Familia {
			case fbcharset.FamiliaTransliterada:
				assert.Truef(t, valido,
					"%s está registrada como transliterada, así que Firebird debería entregar UTF-8 válido "+
						"sobre esta conexión charset=UTF8. Llegó %q (bytes % x). "+
						"Si de verdad es CHARACTER SET NONE, hay que leerla con firebird.Win1252.",
					col.Clave(), string(bytes), bytes)
			case fbcharset.FamiliaNone:
				assert.Falsef(t, valido,
					"%s está registrada como CHARACTER SET NONE, así que sus bytes crudos Windows-1252 "+
						"NO deberían ser UTF-8 válido. Llegaron como %q. "+
						"Si Firebird ya la transliteró, leerla con firebird.Win1252 la corrompe.",
					col.Clave(), string(bytes))
				// Y el destino correcto sí la recupera.
				var w firebird.Win1252
				require.NoError(t, w.Scan(bytes))
				assert.Truef(t, utf8.ValidString(string(w)),
					"firebird.Win1252 debería devolver UTF-8 válido para %s", col.Clave())
			}
		}

		// El piso contra el barrido vacío: si NINGUNA columna trajo datos
		// acentuados, esta prueba no verificó nada y no puede pasar.
		require.GreaterOrEqualf(t, conDatosAcentuados, 8,
			"sólo %d columnas del registro trajeron datos con no-ASCII (%v sin datos acentuados). "+
				"Contra una base sin acentos esta prueba no demuestra nada: "+
				"corra contra la base de desarrollo completa, o siembre acentos.",
			conDatosAcentuados, sinDatos)

		t.Logf("control positivo ejercido en %d columnas; sin datos acentuados: %v",
			conDatosAcentuados, sinDatos)
	})
}

func TestCatalogoCharsets_ElNombreNoPredice(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		catalogo := charsetsDelCatalogo(ctx, t, q, []string{"CIUDADES", "ZONAS_CLIENTES"})

		ciudades, ok := catalogo["CIUDADES.NOMBRE"]
		require.True(t, ok, "CIUDADES.NOMBRE no está en el catálogo")
		zonas, ok := catalogo["ZONAS_CLIENTES.NOMBRE"]
		require.True(t, ok, "ZONAS_CLIENTES.NOMBRE no está en el catálogo")

		assert.NotEqualf(t, ciudades, zonas,
			"CIUDADES.NOMBRE (%s) y ZONAS_CLIENTES.NOMBRE (%s) tienen el MISMO charset. "+
				"Si eso llegara a ser cierto, el contraejemplo que sostiene la regla "+
				"—que el charset no se deduce del nombre de la columna— deja de existir "+
				"y hay que buscar otro antes de relajar nada.",
			ciudades, zonas)

		familiaCiudades, err := fbcharset.FamiliaDeCharset(ciudades)
		require.NoError(t, err)
		familiaZonas, err := fbcharset.FamiliaDeCharset(zonas)
		require.NoError(t, err)
		assert.Equal(t, fbcharset.FamiliaTransliterada, familiaCiudades)
		assert.Equal(t, fbcharset.FamiliaNone, familiaZonas)
	})
}
