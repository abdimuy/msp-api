//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "time"

// Divisores de PARCIALIDADES_TRANSCURRIDAS por frecuencia (overdue_payments_view).
const (
	diasPorPeriodoSemanal   = 7
	diasPorPeriodoQuincenal = 15
	diasPorPeriodoMensual   = 30
	diasPorPeriodoOtro      = 1
	horasPorDia             = 24
)

// NumAtrasos es `num_atrasos` de Cuentas (contrato §5): port fiel de la vista
// `overdue_payments_view` del teléfono, por resolución del arquitecto sobre
// D-API-7. Fuente (msp-app-kt@44fe4593, LEÍDO 2026-10-14):
// core/database/src/main/kotlin/com/example/msp_app/core/database/migrations/Migration22to23.kt
// (igual en core/database/schemas/.../AppDatabase/33.json, `overdue_payments_view`),
// leída como CAST(NUM_PAGOS_ATRASADOS AS INTEGER) en dao/sale/SaleDao.kt:114 y
// mapeada con `?: 0` en feature/pagos/.../data/adapter/RoomVentasAdapter.kt:111.
//
//	T   = (JULIANDAY(ref) − JULIANDAY(FECHA)) / divisor
//	ref = MAX(FECHA_HORA_PAGO) si SALDO_REST = 0, si no DATE('now')
//	divisor = 7 SEMANAL · 15 QUINCENAL · 30 MENSUAL · 1 cualquier otro
//	NUM = (T·P − (PT − S)) / P > S / P  ?  S / P  :  (T·P − (PT − S − E)) / P
//
// con P = PARCIALIDAD, PT = PRECIO_TOTAL, S = SALDO_REST, E = ENGANCHE. La
// asimetría (el ENGANCHE solo en la rama ELSE) es del teléfono y se copia tal
// cual. Detalles de SQLite que se replican:
//   - La aritmética es REAL (float64): las columnas de dinero son REAL en Room y
//     JULIANDAY devuelve REAL; por eso aquí se usa float64 y no decimal.
//   - CAST(REAL AS INTEGER) trunca hacia cero: puede salir negativo (cuenta
//     adelantada); el teléfono lo muestra así.
//   - DATE('now') es el día UTC a las 00:00, no el día de CDMX.
//   - P = 0 divide entre cero → NULL → 0; liquidada sin pagos → NULL → 0.
//   - FECHA es `fecha_venta ?: fecha_cargo` (VentaDto.kt:101).
func NumAtrasos(c CuentaUniverso, ahora time.Time) int {
	p := c.Parcialidad.InexactFloat64()
	if p == 0 {
		return 0
	}
	fecha := c.FechaCargo
	if c.FechaVenta != nil {
		fecha = *c.FechaVenta
	}
	var ref time.Time
	if c.Saldo.IsZero() {
		if c.FechaUltPago == nil {
			return 0
		}
		ref = *c.FechaUltPago
	} else {
		u := ahora.UTC()
		ref = time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	}
	t := ref.Sub(fecha).Hours() / horasPorDia / divisorDeFrecuencia(c.FrecPago)
	pt := c.PrecioTotal.InexactFloat64()
	s := c.Saldo.InexactFloat64()
	e := c.Enganche.InexactFloat64()
	tope := s / p
	if (t*p-(pt-s))/p > tope {
		return int(tope)
	}
	return int((t*p - (pt - s - e)) / p)
}

func divisorDeFrecuencia(frecPago string) float64 {
	switch frecPago {
	case "SEMANAL":
		return diasPorPeriodoSemanal
	case "QUINCENAL":
		return diasPorPeriodoQuincenal
	case "MENSUAL":
		return diasPorPeriodoMensual
	default:
		return diasPorPeriodoOtro
	}
}
