package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// fixedFecha is the canonical deterministic timestamp used across venta tests.
var fixedFecha = time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

func TestHydrateVentaCliente_AllGettersRoundTrip(t *testing.T) {
	t.Parallel()
	total := decimal.NewFromFloat(12500.00)
	saldo := decimal.NewFromFloat(4000.00)

	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:  101,
		ClienteID:  42,
		Fecha:      fixedFecha,
		Folio:      "PV-000123",
		Tipo:       domain.TipoVentaCredito,
		Total:      total,
		SaldoVenta: saldo,
		NumPagos:   3,
	})

	assert.Equal(t, 101, v.DoctoPVID())
	assert.Equal(t, 42, v.ClienteID())
	assert.Equal(t, fixedFecha, v.Fecha())
	assert.Equal(t, "PV-000123", v.Folio())
	assert.Equal(t, domain.TipoVentaCredito, v.Tipo())
	assert.True(t, total.Equal(v.Total()), "Total round-trip")
	assert.True(t, saldo.Equal(v.SaldoVenta()), "SaldoVenta round-trip")
	assert.Equal(t, 3, v.NumPagos())
}

func TestHydrateVentaCliente_FechaUTCNormalization(t *testing.T) {
	t.Parallel()
	// Pass a non-UTC time; the constructor must normalize to UTC.
	cdmx, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Skip("America/Mexico_City tz unavailable:", err)
	}
	localTime := time.Date(2026, 1, 15, 4, 30, 0, 0, cdmx) // 04:30 CDMX = 10:30 UTC
	expected := localTime.UTC()

	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		Fecha: localTime,
	})

	got := v.Fecha()
	assert.Equal(t, time.UTC, got.Location(), "Fecha Location must be UTC")
	assert.True(t, expected.Equal(got), "Fecha instant must be preserved after UTC normalization")
}

func TestHydrateVentaCliente_FechaIsUTC_WhenAlreadyUTC(t *testing.T) {
	t.Parallel()
	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		Fecha: fixedFecha,
	})
	assert.Equal(t, time.UTC, v.Fecha().Location())
	assert.Equal(t, fixedFecha, v.Fecha())
}

func TestHydrateVentaCliente_TipoContado(t *testing.T) {
	t.Parallel()
	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		Tipo:  domain.TipoVentaContado,
		Fecha: fixedFecha,
	})
	assert.Equal(t, domain.TipoVentaContado, v.Tipo())
}

func TestHydrateVentaCliente_ZeroValues(t *testing.T) {
	t.Parallel()
	// Zero Fecha should survive as zero UTC.
	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{})

	assert.Zero(t, v.DoctoPVID())
	assert.Zero(t, v.ClienteID())
	assert.True(t, v.Fecha().IsZero())
	assert.Empty(t, v.Folio())
	assert.Equal(t, domain.TipoVenta(""), v.Tipo())
	assert.True(t, decimal.Zero.Equal(v.Total()))
	assert.True(t, decimal.Zero.Equal(v.SaldoVenta()))
	assert.Zero(t, v.NumPagos())
}

func TestHydrateVentaCliente_ReturnsPointer(t *testing.T) {
	t.Parallel()
	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{DoctoPVID: 1, Fecha: fixedFecha})
	assert.NotNil(t, v)
}

func TestHydrateVentaCliente_DecimalPrecision(t *testing.T) {
	t.Parallel()
	total := decimal.NewFromFloat(1234567.89)
	saldo := decimal.NewFromFloat(0.01)

	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		Total:      total,
		SaldoVenta: saldo,
		Fecha:      fixedFecha,
	})

	assert.True(t, total.Equal(v.Total()), "Total decimal precision")
	assert.True(t, saldo.Equal(v.SaldoVenta()), "SaldoVenta decimal precision")
}
