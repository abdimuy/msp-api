//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// Catálogo cerrado de TIPO_VISITA y su estado, copia de
// msp-app-kt@44fe4593 core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/TipoVisitaCatalogo.kt:55-96 (literales) y :122-151
// (literal → estado), LEÍDO 2026-10-14 (que a su vez copia
// internal/visitas/domain/tipo_visita.go). Los literales viajan por el cable y
// no se editan: la comparación es byte a byte, con acentos.
const (
	TipoVisitaNoSeEncontraba        = "No se encontraba"
	TipoVisitaCasaCerrada           = "Casa cerrada con candado"
	TipoVisitaSoloMenores           = "Solo había menores"
	TipoVisitaNoVaADarPago          = "No pagará esta ocasión"
	TipoVisitaPideTiempo            = "Pidió que regrese otro día"
	TipoVisitaTienePeroNoPaga       = "Tiene dinero pero no quiso pagar"
	TipoVisitaFueGrosero            = "Fue grosero o agresivo"
	TipoVisitaSeEsconde             = "Se asomó pero no salió"
	TipoVisitaNoResponde            = "No responde aunque está"
	TipoVisitaSeEscuchanRuidos      = "Se escuchan ruidos pero no abre"
	TipoVisitaPideReagendar         = "Pidió reagendar visita"
	TipoVisitaLegacyNoVaADarPago    = "No va a dar pago"
	TipoVisitaLegacyDijoQueNoPagara = "Dijo que no va a pagar"
	TipoVisitaLegacySeEscondeNoSale = "Se esconde y no sale"
)

// EstadoTipoVisitaDesconocido es el estado de un literal fuera del catálogo
// (incluida la cadena vacía): el más angosto que sigue siendo cierto. Igual
// que TipoVisitaCatalogo.ESTADO_DESCONOCIDO (TipoVisitaCatalogo.kt:113). Su alcance es VENTA.
const EstadoTipoVisitaDesconocido = EstadoVisiteVuelvo

var estadoPorTipoVisita = map[string]EstadoCuenta{
	TipoVisitaNoSeEncontraba:        EstadoNoEstaba,
	TipoVisitaCasaCerrada:           EstadoNoEstaba,
	TipoVisitaSoloMenores:           EstadoNoEstaba,
	TipoVisitaSeEsconde:             EstadoVisiteVuelvo,
	TipoVisitaNoResponde:            EstadoVisiteVuelvo,
	TipoVisitaSeEscuchanRuidos:      EstadoVisiteVuelvo,
	TipoVisitaPideTiempo:            EstadoVisiteVuelvo,
	TipoVisitaNoVaADarPago:          EstadoSeNego,
	TipoVisitaTienePeroNoPaga:       EstadoSeNego,
	TipoVisitaFueGrosero:            EstadoSeNego,
	TipoVisitaPideReagendar:         EstadoPrometioProxima,
	TipoVisitaLegacyNoVaADarPago:    EstadoSeNego,
	TipoVisitaLegacyDijoQueNoPagara: EstadoSeNego,
	TipoVisitaLegacySeEscondeNoSale: EstadoVisiteVuelvo,
}

// EsTipoVisitaConocido dice si el literal está en el catálogo de 14.
func EsTipoVisitaConocido(tipoVisita string) bool {
	_, ok := estadoPorTipoVisita[tipoVisita]
	return ok
}

// EstadoDeTipoVisita es TipoVisitaCatalogo.estadoDe (TipoVisitaCatalogo.kt:220-223): función TOTAL. Una visita
// con día de cita es CITA_A_UNA_HORA venga con el literal que venga.
func EstadoDeTipoVisita(tipoVisita string, tieneCita bool) EstadoCuenta {
	if tieneCita {
		return EstadoCitaAUnaHora
	}
	if e, ok := estadoPorTipoVisita[tipoVisita]; ok {
		return e
	}
	return EstadoTipoVisitaDesconocido
}

// AlcanceDeTipoVisita se deriva del estado, igual que en Kotlin
// (TipoVisitaCatalogo.alcanceDe, TipoVisitaCatalogo.kt:233-234), para que nunca
// se contradigan.
func AlcanceDeTipoVisita(tipoVisita string, tieneCita bool) Alcance {
	return EstadoDeTipoVisita(tipoVisita, tieneCita).Alcance()
}

// TipoVisitaCatalogo es una entrada de GET /desempeno/catalogos (contrato §4b).
type TipoVisitaCatalogo struct {
	Literal string
	Estado  EstadoCuenta
	Grupo   string
}

// literalesEnOrden lista los 14, vigentes primero y retirados después (el orden
// estable de internal/visitas/domain/tipo_visita.go y de
// TipoVisitaCatalogo.LITERALES, TipoVisitaCatalogo.kt:158-173).
var literalesEnOrden = []string{
	TipoVisitaNoSeEncontraba, TipoVisitaCasaCerrada, TipoVisitaSoloMenores, TipoVisitaNoVaADarPago,
	TipoVisitaPideTiempo, TipoVisitaTienePeroNoPaga, TipoVisitaFueGrosero, TipoVisitaSeEsconde,
	TipoVisitaNoResponde, TipoVisitaSeEscuchanRuidos, TipoVisitaPideReagendar,
	TipoVisitaLegacyNoVaADarPago, TipoVisitaLegacyDijoQueNoPagara, TipoVisitaLegacySeEscondeNoSale,
}

// GrupoDeEstado nombra el grupo de un estado alcanzable desde un literal
// (comentarios de agrupación de TipoVisitaCatalogo.kt). Para el resto, "".
func GrupoDeEstado(e EstadoCuenta) string {
	switch e {
	case EstadoNoEstaba:
		return "No estaba"
	case EstadoVisiteVuelvo:
		return "Visité, vuelvo"
	case EstadoSeNego:
		return "Se negó"
	case EstadoPrometioProxima:
		return "Prometió"
	case EstadoPago, EstadoAbonoParcial, EstadoCitaAUnaHora, EstadoSinTocar:
		return ""
	}
	return ""
}

// CatalogoTiposVisita devuelve los 14 literales con su estado y grupo.
func CatalogoTiposVisita() []TipoVisitaCatalogo {
	out := make([]TipoVisitaCatalogo, 0, len(literalesEnOrden))
	for _, l := range literalesEnOrden {
		e := EstadoDeTipoVisita(l, false)
		out = append(out, TipoVisitaCatalogo{Literal: l, Estado: e, Grupo: GrupoDeEstado(e)})
	}
	return out
}
