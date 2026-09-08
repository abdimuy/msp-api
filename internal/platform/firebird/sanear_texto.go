package firebird

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// SanearParaUTF8 devuelve s en UTF-8 válido, apto para una columna nuestra
// CHARACTER SET UTF8.
//
// ── POR QUÉ EXISTE ──────────────────────────────────────────────────────────
//
// Los mensajes de error que devuelve Firebird cuando Microsip rechaza algo
// llegan con bytes WINDOWS-1252, no UTF-8. La "é" de "crédito" viaja como
// 0xE9. Nuestras tablas MSP_* son CHARACTER SET UTF8, así que escribir ese
// texto tal cual muere con:
//
//	Dynamic SQL Error / SQL error code = -303 / Malformed string
//
// y se lleva por delante la transacción ENTERA. Medido en producción el
// 2026-09-04 sobre MSP_PAGOS_RECIBIDOS: el registro del fallo se deshacía
// completo, así que la fila quedaba con ULTIMO_ERROR nulo Y con INTENTOS en 0
// — sin motivo que leer y sin backoff, de modo que el worker reintentaba cada
// 60 segundos sin rendirse jamás.
//
// ── POR QUÉ SÓLO CONVIERTE LO QUE NO ES UTF-8 VÁLIDO ────────────────────────
//
// La guarda `utf8.ValidString` no es una optimización: es la corrección.
// Windows-1252 asigna un carácter a CUALQUIER byte, así que decodificar a
// ciegas nunca falla — y aplicado a un texto que ya era UTF-8 lo destroza en
// silencio: "crédito" (c3 a9) saldría como "crÃ©dito". Nuestros propios
// mensajes de error están en español y en UTF-8, y pasan por aquí igual que
// los de Microsip.
//
// Un texto que sea válido en las dos codificaciones se deja intacto. No es
// pérdida: ya se podía guardar, que es lo único que esta función promete.
//
// ── LO QUE NO HACE ──────────────────────────────────────────────────────────
//
// No trunca. El ancho de la columna es problema de quien escribe, y mezclar
// las dos cosas escondería un desbordamiento detrás de un saneado.
func SanearParaUTF8(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	decodificado, err := charmap.Windows1252.NewDecoder().String(s)
	if err != nil {
		// Windows-1252 decodifica cualquier byte, así que llegar aquí es
		// improbable. Aun así no se devuelve el original —que es justo lo que
		// no se puede guardar—: se reemplazan los bytes inválidos y el
		// mensaje sobrevive mutilado en vez de perderse entero.
		return strings.ToValidUTF8(s, "�")
	}
	return decodificado
}

// SanearParaUTF8Ptr es la forma para columnas nulables: nil se queda nil.
func SanearParaUTF8Ptr(s *string) *string {
	if s == nil {
		return nil
	}
	saneado := SanearParaUTF8(*s)
	return &saneado
}
