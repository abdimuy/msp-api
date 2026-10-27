//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func TestRepartirVentas_ReglaDeVentasPorZona(t *testing.T) {
	t.Parallel()
	loc := firebird.BusinessTZ()
	desde := time.Date(2026, 2, 12, 20, 0, 0, 0, loc).UTC()
	hasta := time.Date(2026, 2, 18, 20, 0, 0, 0, loc).UTC()
	pared := func(s string) time.Time { v, _ := time.Parse("2006-01-02 15:04", s); return v }
	d := decimal.RequireFromString
	venta := func(id, zona int, saldo string) rutasdomain.VentaCobranza {
		return rutasdomain.VentaCobranza{VentaID: id, ZonaID: zona, Saldo: d(saldo), AbonoSemana: decimal.Zero}
	}
	candidatas := []rutasdomain.VentaCobranza{
		venta(30, 7, "100.00"), // con saldo, sin abono
		venta(10, 7, "0.00"),   // saldada con abono en la ventana (sale dos veces: dos abonos)
		venta(10, 7, "0.00"),
		venta(20, 7, "0.00"),  // saldada con abono solo ANTES de la ventana
		venta(40, 8, "50.00"), // otra zona
	}
	abonos := map[claveVentaZona][]abonoRuta{
		{zona: 7, venta: 10}: {{fecha: pared("2026-02-13 10:00"), importe: d("150.00")}, {fecha: pared("2026-02-18 20:00"), importe: d("50.00")}},
		{zona: 7, venta: 20}: {{fecha: pared("2026-02-12 19:59"), importe: d("200.00")}},
		{zona: 9, venta: 30}: {{fecha: pared("2026-02-14 10:00"), importe: d("99.00")}}, // abono de otra zona: no cuenta
	}
	got := repartirVentas(candidatas, abonos, 7, nuevaVentanaPared(desde, hasta))
	if assert.Len(t, got, 2) {
		assert.Equal(t, 10, got[0].VentaID)
		assert.True(t, got[0].AbonoSemana.Equal(d("200.00")), "suma los dos abonos de la ventana, extremos incluidos")
		assert.Equal(t, 30, got[1].VentaID)
		assert.True(t, got[1].AbonoSemana.IsZero())
		assert.Equal(t, int32(-2), got[1].AbonoSemana.Exponent(), "mismo exponente que CAST(... AS NUMERIC(18,2))")
	}
	assert.True(t, candidatas[0].AbonoSemana.IsZero(), "cada consulta recibe copias: las candidatas no se tocan")
}
