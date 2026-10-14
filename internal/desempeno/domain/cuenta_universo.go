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
	// Los tres campos siguientes alimentan NumAtrasos (fórmula del teléfono,
	// resolución del arquitecto sobre D-API-7). Los llena el repositorio del
	// universo (carril B, Tarea B4):
	//   - FrecPago: UPPER(LISTAS_ATRIBUTOS.VALOR_DESPLEGADO) de
	//     LIBRES_CARGOS_CC.FORMA_DE_PAGO — "SEMANAL", "QUINCENAL", "MENSUAL"; el
	//     mismo literal que el sync manda al teléfono como `frec_pago`.
	//   - FechaVenta: fecha del documento PV (el `fecha_venta` del sync); nil =
	//     sin PV, y entonces cuenta FechaCargo (VentaDto.kt: fecha_venta ?: fecha_cargo).
	//   - FechaUltPago: MSP_SALDOS_VENTAS.FECHA_ULT_PAGO; nil = sin pagos.
	FrecPago     string
	FechaVenta   *time.Time
	FechaUltPago *time.Time
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
