//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// TestEnrichVentas_UsaPrecioTotalNoInflaAtraso is the regression for the bug
// where the cobranza calc fed MSP_SALDOS_VENTAS.TOTAL_IMPORTE (sum of payments)
// as the credit total instead of PRECIO_TOTAL. A recent, current sale where the
// client has paid little (SALDO > payments) must NOT show a huge atraso.
//
// Leticia's real data: PRECIO_TOTAL 6000, paid 1000, IMPTE_REST 500, SALDO 4500,
// parcialidad 100, FECHA_CARGO 12-jun. With TOTAL_IMPORTE(=1000) as total the
// formula yielded 40 cuotas de atraso; with PRECIO_TOTAL(=6000) it is 0.
func TestEnrichVentas_UsaPrecioTotalNoInflaAtraso(t *testing.T) {
	t.Parallel()

	fechaInicio := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

	ventas := []rutasdomain.VentaCobranza{{
		VentaID:      1,
		ZonaID:       1,
		Parcialidad:  decimal.NewFromInt(100),
		Frecuencia:   rutasdomain.Semanal,
		AbonoSemana:  decimal.NewFromInt(500),
		Saldo:        decimal.NewFromInt(4500),
		TotalImporte: decimal.NewFromInt(6000), // PRECIO_TOTAL (real credit total)
		FechaCargo:   time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}}

	enrichVentas(ventas, fechaInicio, now)

	assert.True(t, ventas[0].Vencidas.IsZero(),
		"venta reciente al corriente: atraso debe ser 0, got %s", ventas[0].Vencidas)
	assert.True(t, decimal.NewFromInt(1).Equal(ventas[0].Aporte),
		"aporte capeado a 1 (cubrió su cuota), got %s", ventas[0].Aporte)

	// Contraste: con el total mal (=pagos 1000) el atraso se dispararía.
	buggy := []rutasdomain.VentaCobranza{{
		VentaID: 2, ZonaID: 1,
		Parcialidad:  decimal.NewFromInt(100),
		Frecuencia:   rutasdomain.Semanal,
		AbonoSemana:  decimal.NewFromInt(500),
		Saldo:        decimal.NewFromInt(4500),
		TotalImporte: decimal.NewFromInt(1000), // TOTAL_IMPORTE (pagos) — el bug
		FechaCargo:   time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}}
	enrichVentas(buggy, fechaInicio, now)
	assert.True(t, buggy[0].Vencidas.GreaterThan(decimal.NewFromInt(30)),
		"sanity: con el total mal el atraso se infla (got %s)", buggy[0].Vencidas)
}
