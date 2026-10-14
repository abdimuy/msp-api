//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// CuentaUniverso es una venta del universo de la semana: crédito (no contado),
// de la zona, no cancelada, con saldo > 0 o con FECHA_ULT_PAGO dentro de la
// semana (el mismo predicado de ventana que el sync del teléfono,
// COBRANZA-SYNC.md §5, con desde = inicio de semana).
type CuentaUniverso struct {
	VentaID       int    // MSP_SALDOS_VENTAS.DOCTO_CC_ID (cargo)
	DoctoPVID     int    // 0 = no se resolvió el puente DOCTOS_ENTRE_SIS
	Folio         string // DOCTOS_PV.FOLIO; "" = sin dato
	ClienteID     int
	ClienteNombre string
	Producto      string // descripción del primer artículo de la venta; "" = sin dato
	Parcialidad   decimal.Decimal
	Saldo         decimal.Decimal
	// PrecioTotal es el precio financiado de la venta
	// (MSP_SALDOS_VENTAS.PRECIO_TOTAL; `totalVenta` en el teléfono).
	PrecioTotal decimal.Decimal
	// Enganche es lo que se dio de entrada (`enganche` en el teléfono).
	Enganche   decimal.Decimal
	FechaCargo time.Time
}

// EsNueva dice si nadie ha abonado a la venta: el saldo sigue siendo la
// diferencia exacta entre el precio total y el enganche. Es la regla del
// teléfono (msp-app-kt
// feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/domain/OrdenDeCobranza.kt:103,
// `sinAbonos = saldo == totalVenta - enganche`), por resolución del arquitecto
// sobre D-API-7. La comparación es por valor: la escala del decimal no cuenta.
func (c CuentaUniverso) EsNueva() bool {
	return c.Saldo.Equal(c.PrecioTotal.Sub(c.Enganche))
}
