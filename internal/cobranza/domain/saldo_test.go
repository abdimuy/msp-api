package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

func TestHydrateSaldo_RoundTripsAllFields(t *testing.T) {
	t.Parallel()

	pvID := 99
	zonaID := 3
	fechaCargo := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	fechaUltPago := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2025, 4, 2, 12, 0, 0, 0, time.UTC)

	params := domain.HydrateSaldoParams{
		DoctoCCID:      42,
		DoctoPVID:      &pvID,
		ClienteID:      7,
		ZonaClienteID:  &zonaID,
		Folio:          "A-0001",
		FechaCargo:     fechaCargo,
		PrecioTotal:    decimal.NewFromInt(15000),
		TotalImporte:   decimal.NewFromInt(5000),
		ImpteRest:      decimal.NewFromInt(500),
		Saldo:          decimal.NewFromInt(9500),
		NumPagos:       3,
		FechaUltPago:   &fechaUltPago,
		CargoCancelado: false,
		UpdatedAt:      updatedAt,
	}

	s := domain.HydrateSaldo(params)

	assert.Equal(t, 42, s.DoctoCCID())
	assert.Equal(t, &pvID, s.DoctoPVID())
	assert.Equal(t, 7, s.ClienteID())
	assert.Equal(t, &zonaID, s.ZonaClienteID())
	assert.Equal(t, "A-0001", s.Folio())
	assert.Equal(t, fechaCargo, s.FechaCargo())
	assert.True(t, decimal.NewFromInt(15000).Equal(s.PrecioTotal()))
	assert.True(t, decimal.NewFromInt(5000).Equal(s.TotalImporte()))
	assert.True(t, decimal.NewFromInt(500).Equal(s.ImpteRest()))
	assert.True(t, decimal.NewFromInt(9500).Equal(s.Saldo()))
	assert.Equal(t, 3, s.NumPagos())
	assert.Equal(t, &fechaUltPago, s.FechaUltPago())
	assert.False(t, s.CargoCancelado())
	assert.Equal(t, updatedAt, s.UpdatedAt())
}

func TestHydrateSaldo_NilOptionalFields(t *testing.T) {
	t.Parallel()

	params := domain.HydrateSaldoParams{
		DoctoCCID:      1,
		DoctoPVID:      nil,
		ClienteID:      5,
		ZonaClienteID:  nil,
		Folio:          "B-0002",
		FechaCargo:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal:    decimal.NewFromInt(8000),
		TotalImporte:   decimal.Zero,
		ImpteRest:      decimal.Zero,
		Saldo:          decimal.NewFromInt(8000),
		NumPagos:       0,
		FechaUltPago:   nil,
		CargoCancelado: true,
		UpdatedAt:      time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	s := domain.HydrateSaldo(params)

	assert.Nil(t, s.DoctoPVID())
	assert.Nil(t, s.ZonaClienteID())
	assert.Nil(t, s.FechaUltPago())
	assert.True(t, s.CargoCancelado())
	assert.Equal(t, 0, s.NumPagos())
}

func TestHydrateResumenZona_RoundTrips(t *testing.T) {
	t.Parallel()

	total := decimal.NewFromFloat(123456.78)
	r := domain.HydrateResumenZona(5, 42, total)

	assert.Equal(t, 5, r.ZonaID())
	assert.Equal(t, 42, r.TotalVentas())
	assert.True(t, total.Equal(r.SaldoTotal()))
}
