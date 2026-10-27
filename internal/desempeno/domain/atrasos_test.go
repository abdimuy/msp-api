package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Resolución del arquitecto sobre D-API-7: num_atrasos es la fórmula de
// `overdue_payments_view` del teléfono (msp-app-kt Migration22to23 / esquema
// Room 33, NUM_PAGOS_ATRASADOS, leído con CAST(... AS INTEGER) en SaleDao.kt:114).
// Las cifras esperadas se calcularon a mano con la fórmula de la vista.
func TestNumAtrasos_FormulaDelTelefono(t *testing.T) {
	t.Parallel()
	utc := func(s string) time.Time {
		tt, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return tt
	}
	pt := func(s string) *time.Time { v := utc(s); return &v }
	base := domain.CuentaUniverso{
		Parcialidad: dinero("150"), PrecioTotal: dinero("4000"), Enganche: dinero("400"),
		Saldo: dinero("3100"), FrecPago: "SEMANAL",
		FechaCargo: utc("2026-09-01T00:00:00Z"),
	}
	hoy := utc("2026-10-14T16:40:00Z") // DATE('now') = 2026-10-14 00:00 UTC

	casos := []struct {
		nombre string
		mod    func(c *domain.CuentaUniverso)
		ahora  time.Time
		want   int
	}{
		// 43 días / 7 = 6.142857; (6.142857·150 − (4000−3100−400)) / 150 = 2.8095 → 2.
		{"semanal, rama ELSE", func(*domain.CuentaUniverso) {}, hoy, 2},
		// FechaVenta (fecha_venta del PV) gana a FECHA_CARGO: 2026-09-15 → 29 d → 4.142857 → 0.809 → 0.
		{"fecha de venta gana al cargo", func(c *domain.CuentaUniverso) { c.FechaVenta = pt("2026-09-15T00:00:00Z") }, hoy, 0},
		// 43 / 15 = 2.8667; (430 − 500)/150 = −0.467 → CAST trunca hacia cero → 0.
		{"quincenal", func(c *domain.CuentaUniverso) { c.FrecPago = "QUINCENAL" }, hoy, 0},
		// 43 / 30 = 1.4333 con parcialidad 100: (143.33 − 500)/100 = −3.57 → −3 (adelantado).
		{"mensual adelantado da negativo, como la vista", func(c *domain.CuentaUniverso) {
			c.FrecPago, c.Parcialidad = "MENSUAL", dinero("100")
		}, hoy, -3},
		// Frecuencia desconocida divide entre 1: T = 43; (43·150 − 900)/150 = 37 > 3100/150 =
		// 20.67 → tope → 20.
		{"frecuencia desconocida divide entre 1", func(c *domain.CuentaUniverso) { c.FrecPago = "" }, hoy, 20},
		// Rama del tope: 2025-01-01 → 651 d → 93; (93·150 − (4000−150))/150 = 67.3 > 150/150 = 1 → 1.
		{"tope en saldo/parcialidad", func(c *domain.CuentaUniverso) {
			c.FechaCargo, c.Saldo = utc("2025-01-01T00:00:00Z"), dinero("150")
		}, hoy, 1},
		// Liquidada: referencia = último pago. 28 d → 4; a = (600 − 4000)/150 < 0 = tope →
		// ELSE (600 − 3600)/150 = −20.
		{"liquidada usa la fecha del último pago", func(c *domain.CuentaUniverso) {
			c.Saldo, c.FechaUltPago = dinero("0"), pt("2026-09-29T00:00:00Z")
		}, hoy, -20},
		{"liquidada sin último pago es NULL → 0", func(c *domain.CuentaUniverso) { c.Saldo = dinero("0.00") }, hoy, 0},
		{"parcialidad cero divide entre cero → NULL → 0", func(c *domain.CuentaUniverso) { c.Parcialidad = dinero("0") }, hoy, 0},
	}
	for _, c := range casos {
		cu := base
		c.mod(&cu)
		assert.Equal(t, c.want, domain.NumAtrasos(cu, c.ahora), c.nombre)
	}
}

// DATE('now') del teléfono es el día UTC: a las 19:00 de CDMX ya es mañana.
// Venta 2026-08-29 06:00Z, saldo 3200: a las 23:00Z del 14 van 45.75 d
// (6.5357 → 3.869 → 3); a la 01:00Z del 15 (aún día 14 en CDMX) van 46.75 d
// (6.6786 → 4.012 → 4).
func TestNumAtrasos_HoyEsElDiaUTCComoSQLite(t *testing.T) {
	t.Parallel()
	cu := domain.CuentaUniverso{
		Parcialidad: dinero("150"), PrecioTotal: dinero("4000"), Enganche: dinero("400"),
		Saldo: dinero("3200"), FrecPago: "SEMANAL",
		FechaCargo: time.Date(2026, 8, 29, 6, 0, 0, 0, time.UTC),
	}
	assert.Equal(t, 3, domain.NumAtrasos(cu, time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)))
	assert.Equal(t, 4, domain.NumAtrasos(cu, time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)))
}
