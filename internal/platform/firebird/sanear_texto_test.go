package firebird_test

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// El saneado del texto que va a una columna UTF8 nuestra.
//
// Los mensajes de error de Microsip llegan en Windows-1252; nuestras tablas
// MSP_* son CHARACTER SET UTF8 y rechazan el byte suelto con
// "-303 Malformed string", llevándose la transacción entera.

// win1252 arma bytes crudos que NO son UTF-8 válido. Se construye byte a byte
// porque un literal de Go ya sería UTF-8 y no reproduciría nada.
func win1252(prefijo string, b byte, sufijo string) string {
	return prefijo + string([]byte{b}) + sufijo
}

func TestSanearParaUTF8_ConvierteElAcentoWin1252(t *testing.T) {
	t.Parallel()
	// El mensaje real: "crédito" con la é como 0xE9.
	crudo := win1252("El importe del cr", 0xE9, "dito es mayor al saldo del cargo")
	require.False(t, utf8.ValidString(crudo), "el insumo tiene que ser inválido, o no se prueba nada")

	got := firebird.SanearParaUTF8(crudo)

	assert.True(t, utf8.ValidString(got), "sin esto el UPDATE muere con -303")
	assert.Equal(t, "El importe del crédito es mayor al saldo del cargo", got)
}

// LA PRUEBA QUE PROTEGE DE LA VERSIÓN INGENUA. Decodificar a ciegas nunca
// falla —Windows-1252 asigna carácter a cualquier byte— y destroza en silencio
// lo que ya era UTF-8. Nuestros propios mensajes están en español y pasan por
// aquí igual que los de Microsip.
func TestSanearParaUTF8_NoTocaLoQueYaEsUTF8(t *testing.T) {
	t.Parallel()
	casos := []string{
		"El importe del crédito es mayor al saldo del cargo",
		"la nota ocupa 174 bytes y microsip sólo guarda 99",
		"el artículo no tiene existencia suficiente en el almacén origen",
		"señor Muñoz — ¿cuánto?",
	}
	for _, c := range casos {
		require.True(t, utf8.ValidString(c))
		assert.Equalf(t, c, firebird.SanearParaUTF8(c),
			"un texto ya válido no se toca; decodificarlo daría crÃ©dito: %q", c)
	}
}

func TestSanearParaUTF8_ElVacioSeQuedaVacio(t *testing.T) {
	t.Parallel()
	assert.Empty(t, firebird.SanearParaUTF8(""))
}

func TestSanearParaUTF8_ElASCIISeQuedaIgual(t *testing.T) {
	t.Parallel()
	s := "exception 7\nEX_SALDO_CARGO_EXCEDIDO"
	assert.Equal(t, s, firebird.SanearParaUTF8(s))
}

// Todo byte alto de Windows-1252 tiene que salir en UTF-8 válido. Es la
// garantía de la función: lo que devuelve SIEMPRE se puede guardar.
func TestSanearParaUTF8_TodoByteAltoSaleValido(t *testing.T) {
	t.Parallel()
	for b := 0x80; b <= 0xFF; b++ {
		crudo := win1252("saldo ", byte(b), " excedido")
		got := firebird.SanearParaUTF8(crudo)
		require.Truef(t, utf8.ValidString(got), "byte 0x%02X produjo UTF-8 inválido: %q", b, got)
	}
}

func TestSanearParaUTF8Ptr_NilSeQuedaNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, firebird.SanearParaUTF8Ptr(nil))
}

func TestSanearParaUTF8Ptr_ConvierteYNoMutaElOrigen(t *testing.T) {
	t.Parallel()
	crudo := win1252("cr", 0xE9, "dito")
	original := crudo

	got := firebird.SanearParaUTF8Ptr(&crudo)

	require.NotNil(t, got)
	assert.Equal(t, "crédito", *got)
	assert.Equal(t, original, crudo, "el puntero de entrada no se toca")
}

// ─── el saneo en MapError, que es la frontera ───────────────────────────────
//
// El texto se sanea AHÍ y no en cada sitio que guarda un motivo: es el punto
// donde los bytes ajenos entran a nuestro mundo. Lo delicado es que envolver
// el error no puede romper errors.Is / errors.As, de los que dependen las
// ramas de reintento de medio sistema.

// textoWin1252Error finge un error del driver cuyo texto trae el byte 0xE9.
type textoWin1252Error struct{}

func (textoWin1252Error) Error() string {
	return "exception 7\nEl importe del cr" + string([]byte{0xE9}) + "dito es mayor al saldo"
}

var errCentinelaDePrueba = errors.New("centinela")

type envueltoError struct{ inner error }

func (e envueltoError) Error() string {
	return "cr" + string([]byte{0xE9}) + "dito: " + e.inner.Error()
}
func (e envueltoError) Unwrap() error { return e.inner }

func TestMapError_SaneaElTextoDelDriver(t *testing.T) {
	t.Parallel()
	crudo := textoWin1252Error{}
	require.False(t, utf8.ValidString(crudo.Error()))

	mapeado := firebird.MapError(crudo)

	require.Error(t, mapeado)
	assert.True(t, utf8.ValidString(mapeado.Error()),
		"si el texto sigue inválido, guardarlo en una columna UTF8 da -303 y "+
			"deshace la transacción entera")
	assert.Contains(t, mapeado.Error(), "crédito", "y sale legible, con su acento")
}

// LA PRUEBA QUE PROTEGE LO DELICADO. Envolver el error no puede romper la
// cadena: media docena de ramas de reintento dependen de errors.Is/As.
func TestMapError_SaneadoConservaLaCadenaDeErrores(t *testing.T) {
	t.Parallel()
	crudo := envueltoError{inner: errCentinelaDePrueba}
	require.False(t, utf8.ValidString(crudo.Error()))

	mapeado := firebird.MapError(crudo)

	assert.True(t, utf8.ValidString(mapeado.Error()))
	assert.ErrorIs(t, mapeado, errCentinelaDePrueba,
		"envolver para sanear no puede esconder el error original")
}

// Un error que ya es válido no se envuelve: añadir una capa a cada error de
// base de datos del sistema sin arreglar nada tiene su propio costo.
func TestMapError_NoEnvuelveLoQueYaEsValido(t *testing.T) {
	t.Parallel()
	crudo := errors.New("connection refused")

	mapeado := firebird.MapError(crudo)

	require.ErrorIs(t, mapeado, crudo)
	assert.Contains(t, mapeado.Error(), "connection refused")
}
