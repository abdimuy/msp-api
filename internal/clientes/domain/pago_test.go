package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// fixedPagoFecha is the canonical deterministic timestamp used across pago tests.
var fixedPagoFecha = time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)

func TestHydratePago_AllGettersRoundTrip(t *testing.T) {
	t.Parallel()
	importe := decimal.NewFromFloat(2500.00)

	p := domain.HydratePago(domain.HydratePagoParams{
		DoctoCCID:      55,
		Fecha:          fixedPagoFecha,
		Importe:        importe,
		FormaCobro:     "Efectivo",
		AplicaACargoID: 200,
	})

	assert.Equal(t, 55, p.DoctoCCID())
	assert.Equal(t, fixedPagoFecha, p.Fecha())
	assert.True(t, importe.Equal(p.Importe()), "Importe round-trip")
	assert.Equal(t, "Efectivo", p.FormaCobro())
	assert.Equal(t, 200, p.AplicaACargoID())
}

func TestHydratePago_FechaUTCNormalization(t *testing.T) {
	t.Parallel()
	// Pass a non-UTC time; the constructor must normalize to UTC.
	cdmx, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Skip("America/Mexico_City tz unavailable:", err)
	}
	// 08:00 CDMX (UTC-6) = 14:00 UTC
	localTime := time.Date(2026, 3, 10, 8, 0, 0, 0, cdmx)
	expected := localTime.UTC()

	p := domain.HydratePago(domain.HydratePagoParams{
		Fecha: localTime,
	})

	got := p.Fecha()
	assert.Equal(t, time.UTC, got.Location(), "Fecha Location must be UTC")
	assert.True(t, expected.Equal(got), "Fecha instant must be preserved after UTC normalization")
}

func TestHydratePago_FechaIsUTC_WhenAlreadyUTC(t *testing.T) {
	t.Parallel()
	p := domain.HydratePago(domain.HydratePagoParams{
		Fecha: fixedPagoFecha,
	})
	assert.Equal(t, time.UTC, p.Fecha().Location())
	assert.Equal(t, fixedPagoFecha, p.Fecha())
}

func TestHydratePago_AplicaACargoID_Zero_WhenUnknown(t *testing.T) {
	t.Parallel()
	// AplicaACargoID = 0 signals "unknown cargo" — must round-trip cleanly.
	p := domain.HydratePago(domain.HydratePagoParams{
		Fecha:          fixedPagoFecha,
		AplicaACargoID: 0,
	})
	assert.Zero(t, p.AplicaACargoID())
}

func TestHydratePago_ZeroValues(t *testing.T) {
	t.Parallel()
	p := domain.HydratePago(domain.HydratePagoParams{})

	assert.Zero(t, p.DoctoCCID())
	assert.True(t, p.Fecha().IsZero())
	assert.True(t, decimal.Zero.Equal(p.Importe()))
	assert.Empty(t, p.FormaCobro())
	assert.Zero(t, p.AplicaACargoID())
}

func TestHydratePago_ReturnsPointer(t *testing.T) {
	t.Parallel()
	p := domain.HydratePago(domain.HydratePagoParams{DoctoCCID: 1, Fecha: fixedPagoFecha})
	assert.NotNil(t, p)
}

func TestHydratePago_DecimalPrecision(t *testing.T) {
	t.Parallel()
	// Centavo precision must be preserved.
	importe, _ := decimal.NewFromString("1234.99")
	p := domain.HydratePago(domain.HydratePagoParams{
		Importe: importe,
		Fecha:   fixedPagoFecha,
	})
	assert.True(t, importe.Equal(p.Importe()), "Importe decimal precision")
}

func TestHydratePago_FormaCobroUnicode(t *testing.T) {
	t.Parallel()
	// Payment method names with accented characters must round-trip.
	p := domain.HydratePago(domain.HydratePagoParams{
		FormaCobro: "Transferencia Bancária",
		Fecha:      fixedPagoFecha,
	})
	assert.Equal(t, "Transferencia Bancária", p.FormaCobro())
}

func TestHydratePago_MultipleVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		formaCobro string
		importe    decimal.Decimal
	}{
		{"efectivo", "Efectivo", decimal.NewFromFloat(500)},
		{"transferencia", "Transferencia", decimal.NewFromFloat(10000.50)},
		{"cheque", "Cheque", decimal.NewFromFloat(25000)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := domain.HydratePago(domain.HydratePagoParams{
				FormaCobro: tc.formaCobro,
				Importe:    tc.importe,
				Fecha:      fixedPagoFecha,
			})
			assert.Equal(t, tc.formaCobro, p.FormaCobro())
			assert.True(t, tc.importe.Equal(p.Importe()))
		})
	}
}
