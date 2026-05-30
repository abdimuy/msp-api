//nolint:misspell // Spanish vocabulary by convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

func TestHydratePago_RoundTrip(t *testing.T) {
	t.Parallel()
	zona := 42
	now := time.Date(2026, 5, 30, 14, 25, 13, 0, time.UTC)
	lat := decimal.RequireFromString("20.12345678")
	lon := decimal.RequireFromString("-103.87654321")

	p := domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: 4611,
		DoctoCCID:      4610,
		DoctoCCAcrID:   4607,
		ClienteID:      12391,
		ZonaClienteID:  &zona,
		Folio:          "cv0000001",
		ConceptoCCID:   87327,
		Fecha:          now,
		Importe:        decimal.RequireFromString("6065.00"),
		Impuesto:       decimal.RequireFromString("12.50"),
		Lat:            &lat,
		Lon:            &lon,
		Cancelado:      false,
		Aplicado:       true,
		UpdatedAt:      now,
	})

	assert.Equal(t, 4611, p.ImpteDoctoCCID())
	assert.Equal(t, 4610, p.DoctoCCID())
	assert.Equal(t, 4607, p.DoctoCCAcrID())
	assert.Equal(t, 12391, p.ClienteID())
	if assert.NotNil(t, p.ZonaClienteID()) {
		assert.Equal(t, 42, *p.ZonaClienteID())
	}
	assert.Equal(t, "cv0000001", p.Folio())
	assert.Equal(t, 87327, p.ConceptoCCID())
	assert.Equal(t, now, p.Fecha())
	assert.True(t, decimal.RequireFromString("6065.00").Equal(p.Importe()))
	assert.True(t, decimal.RequireFromString("12.50").Equal(p.Impuesto()))
	if assert.NotNil(t, p.Lat()) {
		assert.True(t, lat.Equal(*p.Lat()))
	}
	if assert.NotNil(t, p.Lon()) {
		assert.True(t, lon.Equal(*p.Lon()))
	}
	assert.False(t, p.Cancelado())
	assert.True(t, p.Aplicado())
	assert.Equal(t, now, p.UpdatedAt())
}

func TestHydratePago_NilOptionals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)

	p := domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: 1,
		DoctoCCID:      2,
		DoctoCCAcrID:   3,
		ClienteID:      4,
		Fecha:          now,
		Importe:        decimal.NewFromInt(100),
		Impuesto:       decimal.NewFromInt(0),
		UpdatedAt:      now,
	})

	assert.Nil(t, p.ZonaClienteID())
	assert.Nil(t, p.Lat())
	assert.Nil(t, p.Lon())
	assert.Empty(t, p.Folio())
}
