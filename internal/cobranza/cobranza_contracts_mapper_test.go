package cobranza_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/cobranza"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

func TestToContract_AllFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	pvID := 55
	zonaID := 3
	fechaCargo := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	fechaUltPago := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2025, 3, 2, 8, 0, 0, 0, time.UTC)

	s := domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:      100,
		DoctoPVID:      &pvID,
		ClienteID:      7,
		ZonaClienteID:  &zonaID,
		Folio:          "Z-9999",
		FechaCargo:     fechaCargo,
		PrecioTotal:    decimal.NewFromInt(20000),
		TotalImporte:   decimal.NewFromInt(8000),
		ImpteRest:      decimal.NewFromInt(1000),
		Saldo:          decimal.NewFromInt(11000),
		NumPagos:       4,
		FechaUltPago:   &fechaUltPago,
		CargoCancelado: false,
		UpdatedAt:      updatedAt,
	})

	got := cobranza.ToContract(s)

	assert.Equal(t, 100, got.DoctoCCID)
	assert.Equal(t, &pvID, got.DoctoPVID)
	assert.Equal(t, 7, got.ClienteID)
	assert.Equal(t, &zonaID, got.ZonaClienteID)
	assert.Equal(t, "Z-9999", got.Folio)
	assert.Equal(t, fechaCargo, got.FechaCargo)
	assert.True(t, decimal.NewFromInt(20000).Equal(got.PrecioTotal))
	assert.True(t, decimal.NewFromInt(8000).Equal(got.TotalImporte))
	assert.True(t, decimal.NewFromInt(1000).Equal(got.ImpteRest))
	assert.True(t, decimal.NewFromInt(11000).Equal(got.Saldo))
	assert.Equal(t, 4, got.NumPagos)
	assert.Equal(t, &fechaUltPago, got.FechaUltPago)
	assert.False(t, got.CargoCancelado)
	assert.Equal(t, updatedAt, got.UpdatedAt)
}

func TestToContract_NilOptionalFields(t *testing.T) {
	t.Parallel()

	s := domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:   1,
		ClienteID:   2,
		Folio:       "A-0000",
		FechaCargo:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal: decimal.NewFromInt(5000),
		Saldo:       decimal.NewFromInt(5000),
		UpdatedAt:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	})

	got := cobranza.ToContract(s)

	assert.Nil(t, got.DoctoPVID)
	assert.Nil(t, got.ZonaClienteID)
	assert.Nil(t, got.FechaUltPago)
}

func TestResumenToContract_AllFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	total := decimal.NewFromFloat(75000.50)
	r := domain.HydrateResumenZona(8, 25, total)

	got := cobranza.ResumenToContract(r)

	assert.Equal(t, 8, got.ZonaID)
	assert.Equal(t, 25, got.TotalVentas)
	assert.True(t, total.Equal(got.SaldoTotal))
}
