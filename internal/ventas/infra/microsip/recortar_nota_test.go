package microsip

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// El truncado de la nota hacia LIBRES_CARGOS_CC.OBSERVACIONES.
//
// La columna es VARCHAR(99) CHARACTER SET NONE: cuenta BYTES, y lo que el
// escritor bindea es UTF-8, así que cada acento gasta dos. El dominio permite
// 500 caracteres, más de cinco veces lo que cabe.
//
// Antes se rechazaba la venta entera. Es desproporcionado: la nota completa
// vive en MSP_VENTAS.NOTA y lo de Microsip es la copia que enseña en su
// pantalla. Una venta de $27,000 no se bloquea por un comentario que sobra.
//
// El caso real que lo motivó: la venta de una clienta con una nota de 171
// caracteres y 174 BYTES —"TOMÓ", "GRÁFICA", "ASÍ" llevan acento— que llevaba
// días sin poder aplicarse.

func TestRecortarACaben_LoQueCabeNoSeToca(t *testing.T) {
	t.Parallel()
	s := "NOTA CORTA"
	assert.Equal(t, s, recortarACaben(s, maxNotaMicrosip))
}

func TestRecortarACaben_ElTopeExactoNoSeToca(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("A", maxNotaMicrosip)
	assert.Equal(t, s, recortarACaben(s, maxNotaMicrosip),
		"99 bytes es lo último que cabe; recortarlo mutilaría una nota válida")
}

func TestRecortarACaben_LoQueSobraSeRecortaYSeMarca(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("A", 200)

	got := recortarACaben(s, maxNotaMicrosip)

	assert.LessOrEqual(t, len(got), maxNotaMicrosip, "tiene que caber en la columna")
	assert.True(t, strings.HasSuffix(got, marcaDeCorte),
		"un texto cortado sin marca se lee como uno completo que no dice lo que decía")
}

// LA PRUEBA QUE IMPORTA. Cortar por byte parte una "ó" a la mitad y produce
// bytes que no son UTF-8 válido — el mismo defecto que en cobranza da
// "-303 Malformed string" y deja la fila sin motivo.
func TestRecortarACaben_NoParteUnCaracterAlaMitad(t *testing.T) {
	t.Parallel()
	// Cada "ó" son 2 bytes, así que el tope cae necesariamente a media letra
	// para algún largo. Se prueban todos los topes alrededor del límite.
	s := strings.Repeat("ó", 120)

	for tope := 1; tope <= 120; tope++ {
		got := recortarACaben(s, tope)
		require.LessOrEqualf(t, len(got), tope, "tope=%d: se pasó del presupuesto", tope)
		require.Truef(t, utf8.ValidString(got), "tope=%d: produjo UTF-8 inválido: %q", tope, got)
	}
}

// El caso real, con sus bytes exactos.
func TestRecortarACaben_LaNotaQueBloqueoUnaVenta(t *testing.T) {
	t.Parallel()
	nota := "EL DOMICILIO AL QUE SE TOMÓ LA GRÁFICA ES A DONDE SE FUE A DEJAR " +
		"EL MUEBLE, ASÍ MISMO LA PERSONA NO CONTABA CON SU RECIBO DE LUZ A LA " +
		"MANO , POSTERIORMENTE LO PROPORCIONA."
	require.Len(t, []rune(nota), 171, "171 caracteres")
	require.Len(t, nota, 174, "pero 174 BYTES: los acentos cuentan doble")

	got := recortarACaben(nota, maxNotaMicrosip)

	assert.LessOrEqual(t, len(got), maxNotaMicrosip)
	assert.True(t, utf8.ValidString(got))
	assert.True(t, strings.HasSuffix(got, marcaDeCorte))
	assert.True(t, strings.HasPrefix(got, "EL DOMICILIO AL QUE SE TOM"),
		"lo que cabe se conserva desde el principio")
}

// Presupuesto tan chico que ni la marca cabe: devuelve prefijo válido, sin
// marca, y nunca se pasa. No debe reventar ni producir basura.
func TestRecortarACaben_PresupuestoMenorQueLaMarca(t *testing.T) {
	t.Parallel()
	for tope := 0; tope <= len(marcaDeCorte); tope++ {
		got := recortarACaben("óóóó", tope)
		require.LessOrEqualf(t, len(got), tope, "tope=%d", tope)
		require.Truef(t, utf8.ValidString(got), "tope=%d: %q", tope, got)
	}
}

// El aval usa el mismo mecanismo con otro tope.
func TestRecortarACaben_ElAvalTieneSuPropioTope(t *testing.T) {
	t.Parallel()
	got := recortarACaben(strings.Repeat("é", 80), maxAvalMicrosip)
	assert.LessOrEqual(t, len(got), maxAvalMicrosip)
	assert.True(t, utf8.ValidString(got))
}

// ─── lo que el escritor de verdad bindea ────────────────────────────────────
//
// Las pruebas de arriba ejercen recortarACaben en aislamiento, y eso NO dice
// nada sobre si el escritor lo usa. Medido con mutación dirigida: quitando el
// recorte del sitio del bind, las siete seguían en verde. Éstas cierran ese
// hueco.

func TestObservacionesParaMicrosip_SinNotaEsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, observacionesParaMicrosip(nil))
}

func TestObservacionesParaMicrosip_LaNotaLargaLLegaRecortada(t *testing.T) {
	t.Parallel()
	larga := strings.Repeat("ó", 150) // 300 bytes
	nota := larga

	got := observacionesParaMicrosip(&nota)

	s, ok := got.(string)
	require.True(t, ok, "se bindea como cadena")
	assert.LessOrEqual(t, len(s), maxNotaMicrosip,
		"si esto se pasa, el INSERT de la fase 7 revienta con SQLSTATE 22001")
	assert.True(t, utf8.ValidString(s))
	assert.True(t, strings.HasSuffix(s, marcaDeCorte))
}

func TestObservacionesParaMicrosip_LaNotaCortaViajaIntacta(t *testing.T) {
	t.Parallel()
	nota := "ENTREGAR POR LA TARDE"
	got := observacionesParaMicrosip(&nota)
	assert.Equal(t, nota, got)
}
