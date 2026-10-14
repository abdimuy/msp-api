//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// EstadoCuenta es uno de los ocho estados por venta del teléfono
// (msp-app-kt@44fe4593 core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuenta.kt:34-58, LEÍDO 2026-10-14). Los valores son
// los códigos del contrato §1, byte a byte.
type EstadoCuenta string

// Los ocho estados.
const (
	EstadoPago            EstadoCuenta = "pago"
	EstadoAbonoParcial    EstadoCuenta = "abono_parcial"
	EstadoVisiteVuelvo    EstadoCuenta = "visite_vuelvo"
	EstadoPrometioProxima EstadoCuenta = "prometio_proxima"
	EstadoSeNego          EstadoCuenta = "se_nego"
	EstadoCitaAUnaHora    EstadoCuenta = "cita_a_una_hora"
	EstadoNoEstaba        EstadoCuenta = "no_estaba"
	EstadoSinTocar        EstadoCuenta = "sin_tocar"
)

// EstadosCuenta devuelve los ocho, en el orden del enum de Kotlin.
func EstadosCuenta() []EstadoCuenta {
	return []EstadoCuenta{
		EstadoPago, EstadoAbonoParcial, EstadoVisiteVuelvo, EstadoPrometioProxima,
		EstadoSeNego, EstadoCitaAUnaHora, EstadoNoEstaba, EstadoSinTocar,
	}
}

// ParseEstadoCuenta valida un código del contrato.
func ParseEstadoCuenta(s string) (EstadoCuenta, error) {
	for _, e := range EstadosCuenta() {
		if string(e) == s {
			return e, nil
		}
	}
	return "", ErrParametroInvalido.WithField("estado", s)
}

// Alcance de una visita sobre las ventas del cliente (VisitScope.kt).
type Alcance int

// AlcanceVenta toca solo la venta ligada; AlcanceCliente toca todas las del cliente.
const (
	AlcanceVenta Alcance = iota
	AlcanceCliente
)

// Alcance de cada estado (columna "Alcance" de EstadoCuenta.kt:36-57): solo
// CITA_A_UNA_HORA y NO_ESTABA son de cliente.
func (e EstadoCuenta) Alcance() Alcance {
	if e == EstadoCitaAUnaHora || e == EstadoNoEstaba {
		return AlcanceCliente
	}
	return AlcanceVenta
}

// Trato es cómo se pinta un estado (msp-app-kt@44fe4593 feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/EstadoCuentaUi.kt,
// enum TratoDelEstado).
type Trato string

// Los ocho tratos, con los códigos del contrato §1.
const (
	TratoPagado      Trato = "pagado"
	TratoParcial     Trato = "parcial"
	TratoRegresas    Trato = "regresas"
	TratoDiferido    Trato = "diferido"
	TratoEscalar     Trato = "escalar"
	TratoCita        Trato = "cita"
	TratoNadie       Trato = "nadie"
	TratoSinTrabajar Trato = "sin_trabajar"
)

// Segmento es el chip de la lista de cuentas (msp-app-kt@44fe4593 feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/SegmentoDeCobranza.kt).
type Segmento string

// Los cuatro segmentos del contrato §1. Una cuenta puede no caer en ninguno
// (cita o promesa con trato propio pero sin fecha): entonces el JSON lleva null.
const (
	SegmentoSinVisitar Segmento = "sin_visitar"
	SegmentoVolver     Segmento = "volver"
	SegmentoDespues    Segmento = "despues"
	SegmentoPagados    Segmento = "pagados"
)
