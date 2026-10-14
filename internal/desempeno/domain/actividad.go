//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// TipoActividad es uno de los tres tipos del contrato §1.
type TipoActividad string

// Tipos de actividad.
const (
	TipoPago        TipoActividad = "pago"
	TipoVisita      TipoActividad = "visita"
	TipoCondonacion TipoActividad = "condonacion"
)

// OrigenRegistro es "app" (capturado en el teléfono) u "oficina" (capturado
// directo en Microsip, sin fila en MSP_PAGOS_RECIBIDOS).
type OrigenRegistro string

// Orígenes.
const (
	OrigenApp     OrigenRegistro = "app"
	OrigenOficina OrigenRegistro = "oficina"
)

// NombreOficina es lo que muestra `registrado_por.nombre` para origen oficina.
const NombreOficina = "Oficina"

// RegistradoPor es quién registró la actividad (spec §2 "Quién lo registró").
// UsuarioUID vacío = no se pudo resolver; CobradorID 0 = sin dato.
type RegistradoPor struct {
	UsuarioUID string
	CobradorID int
	Nombre     string
	Origen     OrigenRegistro
}

// DatosPago acompaña a una actividad de tipo pago o condonación.
type DatosPago struct {
	Importe      decimal.Decimal
	FormaCobroID int
	// Aplicado: el pago ya está en Microsip (fila en MSP_PAGOS_VENTAS).
	Aplicado bool
	// Rechazado: MSP_PAGOS_RECIBIDOS en 'P' con INTENTOS > 0 — Microsip lo
	// rechazó al menos una vez (aplicar_pago.go:87-88). Ver la decisión D-API-2.
	Rechazado      bool
	DoctoCCID      int    // 0 si no está aplicado
	PagoRecibidoID string // "" si no hay fila en MSP_PAGOS_RECIBIDOS
}

// DatosVisita acompaña a una actividad de tipo visita.
type DatosVisita struct {
	VisitaID   string
	TipoVisita string
	Nota       string // "" se serializa como null
}

// Actividad es un pago, una visita o una condonación (contrato §1).
type Actividad struct {
	ID            string
	Tipo          TipoActividad
	Hora          time.Time
	RecibidoAt    *time.Time
	ClienteID     int
	ClienteNombre string
	VentaID       int    // 0 = sin venta ligada
	DoctoPVID     int    // DOCTO_PV_ID de la venta (puente DOCTOS_ENTRE_SIS); 0 = sin dato
	Folio         string // folio de la venta (DOCTOS_PV.FOLIO); "" = sin dato
	RegistradoPor RegistradoPor
	Lat           *float64
	Lng           *float64
	NumFotos      int
	Pago          *DatosPago
	Visita        *DatosVisita
}

// CapturaTardiaUmbral marca que recibido_at − hora ≥ 2 h es captura tardía (spec §4.2).
const CapturaTardiaUmbral = 2 * time.Hour

// IDPagoAplicado es el id estable de un pago en Microsip: el
// IMPTE_DOCTO_CC_ID (PK de MSP_PAGOS_VENTAS, único aunque un abono se reparta
// entre varios cargos). Ver la decisión D-API-3.
func IDPagoAplicado(impteDoctoCCID int) string { return "pago:" + strconv.Itoa(impteDoctoCCID) }

// IDPagoPendiente es el id de un pago que aún no llega a Microsip: su UUID de
// MSP_PAGOS_RECIBIDOS.
func IDPagoPendiente(pagoRecibidoID string) string { return "pago:" + pagoRecibidoID }

// IDVisita es el id de una visita: su UUID de MSP_VISITAS.
func IDVisita(visitaID string) string { return "visita:" + visitaID }

// ClasificarTipoPago devuelve condonación si la forma es 137026 o el concepto 27969
// (Condonaciones en CONCEPTOS_CC); si no, pago. C0 mide que las dos señales
// coinciden en producción (consulta 3 de desempeno-c0.sql).
func ClasificarTipoPago(formaCobroID, conceptoCCID int) TipoActividad {
	if formaCobroID == FormaCobroCondonacion || conceptoCCID == ConceptoCondonaciones {
		return TipoCondonacion
	}
	return TipoPago
}

// ConGPS dice si hay coordenadas y no son el centinela (0,0) (spec §6).
func (a Actividad) ConGPS() bool {
	if a.Lat == nil || a.Lng == nil {
		return false
	}
	return *a.Lat != 0 || *a.Lng != 0
}

// CuentaComoDinero dice si es dinero cobrado: un pago (no condonación) con
// forma 157/158/52569, no rechazado (contrato §4 `totales.importe`) y aplicado
// (spec §6: "no aplicado o rechazado no cuenta como dinero"). Es el predicado
// de `pagos {n, importe}` del tablero, la jornada, los tramos y la hora.
func (a Actividad) CuentaComoDinero() bool {
	return a.Tipo == TipoPago && a.Pago != nil && EsFormaDeDinero(a.Pago.FormaCobroID) &&
		a.Pago.Aplicado && !a.Pago.Rechazado
}

// CapturaTardia dice si llegó al servidor ≥ 2 h después de la hora del teléfono
// (contrato §1 `captura_tardia`). Sin recibido_at no se puede saber: false.
func (a Actividad) CapturaTardia() bool {
	return a.RecibidoAt != nil && a.RecibidoAt.Sub(a.Hora) >= CapturaTardiaUmbral
}

// NoAplicado dice si es un pago (no condonación) que no cuenta como dinero.
func (a Actividad) NoAplicado() bool {
	return a.Tipo == TipoPago && a.Pago != nil && (!a.Pago.Aplicado || a.Pago.Rechazado)
}

// Importe devuelve el importe del pago o condonación; cero para visitas.
func (a Actividad) Importe() decimal.Decimal {
	if a.Pago == nil {
		return decimal.Zero
	}
	return a.Pago.Importe
}
