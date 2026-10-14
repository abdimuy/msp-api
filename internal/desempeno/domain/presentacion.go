//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "strconv"

// Presentación de un estado, port de msp-app-kt@44fe4593 feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/
// EstadoCuentaUi.kt (tratoDe :122, etiquetaDe :168, detalleDe :201, cita :315,
// prometio :327) y SegmentoDeCobranza.kt (contiene :82, caePorFecha :115),
// LEÍDO 2026-10-14.
// Las cadenas son las del teléfono, byte a byte: oficina y cobrador leen lo mismo.

// mesesCortos es el formato "MMM" de es-MX que usa el teléfono
// (DateTimeFormatter "d MMM", BUSINESS_LOCALE). LEÍDO: "sept" en
// EstadoCuentaUiTest.kt:128,150; el resto de los meses es INFERIDO de CLDR
// es-MX (ver la decisión D-API-6).
var mesesCortos = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}

func diaYMes(f Fecha) string {
	return strconv.Itoa(f.Dia()) + " " + mesesCortos[f.Mes()-1]
}

// TratoDe es EstadoCuentaUi.tratoDe. Una promesa sin fecha y una cita sin hora
// se tratan como "regresas": nada sostiene sacarlas de la ruta. Un código fuera
// de los ocho se trata como sin trabajar.
func TratoDe(r ResultadoCuenta) Trato {
	switch r.Estado {
	case EstadoPago:
		return TratoPagado
	case EstadoAbonoParcial:
		return TratoParcial
	case EstadoVisiteVuelvo:
		return TratoRegresas
	case EstadoPrometioProxima:
		if r.FechaPromesa == nil {
			return TratoRegresas
		}
		return TratoDiferido
	case EstadoSeNego:
		return TratoEscalar
	case EstadoCitaAUnaHora:
		if r.HoraCita == nil {
			return TratoRegresas
		}
		return TratoCita
	case EstadoNoEstaba:
		return TratoNadie
	case EstadoSinTocar:
		return TratoSinTrabajar
	}
	return TratoSinTrabajar
}

// textoRegresas distingue los tres caminos a "regresas" (promesa sin fecha,
// cita sin hora, el resto).
func textoRegresas(e EstadoCuenta, promesa, cita, otro string) string {
	if e == EstadoPrometioProxima {
		return promesa
	}
	if e == EstadoCitaAUnaHora {
		return cita
	}
	return otro
}

// EtiquetaDe es EstadoCuentaUi.etiquetaDe. El monto prometido no existe en el
// servidor (contrato §7), así que la promesa se lee "Prometió el 25 sept".
func EtiquetaDe(r ResultadoCuenta) string {
	switch TratoDe(r) {
	case TratoPagado:
		return "Pagó esta semana"
	case TratoParcial:
		return "Abonó parcial"
	case TratoRegresas:
		return textoRegresas(r.Estado, "Prometió sin fecha", "Cita sin hora", "Visité, vuelvo")
	case TratoDiferido:
		return "Prometió el " + diaYMes(*r.FechaPromesa)
	case TratoEscalar:
		return "Se negó"
	case TratoCita:
		dia := ""
		if r.FechaCita != nil {
			dia = diaYMes(*r.FechaCita) + " "
		}
		return "Cita " + dia + r.HoraCita.String()
	case TratoNadie:
		return "No estaba"
	case TratoSinTrabajar:
		return "Falta pasar"
	}
	return "Falta pasar"
}

// DetalleDe es EstadoCuentaUi.detalleDe.
func DetalleDe(r ResultadoCuenta) string {
	switch TratoDe(r) {
	case TratoPagado:
		return "No hace falta volver"
	case TratoParcial:
		return "Falta por confirmar"
	case TratoRegresas:
		return textoRegresas(r.Estado, "Sin fecha, regresas", "Sin hora, regresas", "Regresas esta semana")
	case TratoDiferido:
		return "No cae esta semana"
	case TratoEscalar:
		return "Escalar esta cuenta"
	case TratoCita:
		return "Quedaron de verse"
	case TratoNadie:
		return "Regresas esta semana"
	case TratoSinTrabajar:
		return "Todavía no vas"
	}
	return "Todavía no vas"
}

// SegmentoDe es SegmentoDeCobranza.contiene invertido: el segmento de la cuenta
// o ok=false si no cae en ninguno (compromiso con trato propio y sin fecha).
// hoy es el día de negocio (America/Mexico_City).
func SegmentoDe(r ResultadoCuenta, hoy Fecha) (Segmento, bool) {
	switch TratoDe(r) {
	case TratoSinTrabajar:
		return SegmentoSinVisitar, true
	case TratoParcial, TratoRegresas, TratoNadie:
		return SegmentoVolver, true
	case TratoEscalar:
		return SegmentoDespues, true
	case TratoDiferido:
		return segmentoPorFecha(r.FechaPromesa, hoy)
	case TratoCita:
		return segmentoPorFecha(r.FechaCita, hoy)
	case TratoPagado:
		return SegmentoPagados, true
	}
	return SegmentoSinVisitar, true
}

func segmentoPorFecha(f *Fecha, hoy Fecha) (Segmento, bool) {
	if f == nil {
		return "", false
	}
	if f.Despues(hoy) {
		return SegmentoDespues, true
	}
	return SegmentoVolver, true
}
