package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

func TestHydrateProductoVenta_AllGettersRoundTrip(t *testing.T) {
	t.Parallel()
	unidades := decimal.New(250, -2) // 2.50 — fractional quantity
	precio := decimal.NewFromFloat(450.75)
	total := decimal.NewFromFloat(1126.875)
	descuento := decimal.NewFromFloat(5.0)

	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		ArticuloID:      99,
		Nombre:          "Silla Ejecutiva Modelo X",
		Unidades:        unidades,
		PrecioUnitario:  precio,
		PrecioTotalNeto: total,
		PctjeDscto:      descuento,
	})

	assert.Equal(t, 99, p.ArticuloID())
	assert.Equal(t, "Silla Ejecutiva Modelo X", p.Nombre())
	assert.True(t, unidades.Equal(p.Unidades()), "Unidades round-trip")
	assert.True(t, precio.Equal(p.PrecioUnitario()), "PrecioUnitario round-trip")
	assert.True(t, total.Equal(p.PrecioTotalNeto()), "PrecioTotalNeto round-trip")
	assert.True(t, descuento.Equal(p.PctjeDscto()), "PctjeDscto round-trip")
}

func TestHydrateProductoVenta_ZeroValues(t *testing.T) {
	t.Parallel()
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{})

	assert.Zero(t, p.ArticuloID())
	assert.Empty(t, p.Nombre())
	assert.True(t, decimal.Zero.Equal(p.Unidades()))
	assert.True(t, decimal.Zero.Equal(p.PrecioUnitario()))
	assert.True(t, decimal.Zero.Equal(p.PrecioTotalNeto()))
	assert.True(t, decimal.Zero.Equal(p.PctjeDscto()))
}

func TestHydrateProductoVenta_ReturnsPointer(t *testing.T) {
	t.Parallel()
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{ArticuloID: 1})
	assert.NotNil(t, p)
}

func TestHydrateProductoVenta_FractionalUnidades(t *testing.T) {
	t.Parallel()
	// NUMERIC(18,5) allows quantities like 1.23456 — verify no precision loss.
	unidades, _ := decimal.NewFromString("1.23456")
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		Unidades: unidades,
	})
	assert.True(t, unidades.Equal(p.Unidades()), "5-decimal-place precision preserved")
}

func TestHydrateProductoVenta_NombreUnicode(t *testing.T) {
	t.Parallel()
	// Spanish article names must round-trip with full UTF-8 fidelity.
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		Nombre: "Mueble de Cocina — Ñoño Édition",
	})
	assert.Equal(t, "Mueble de Cocina — Ñoño Édition", p.Nombre())
}

func TestHydrateProductoVenta_ZeroPctjeDscto(t *testing.T) {
	t.Parallel()
	// A 0% discount (no discount) must be stored and returned faithfully.
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		PctjeDscto: decimal.Zero,
	})
	assert.True(t, decimal.Zero.Equal(p.PctjeDscto()))
}

func TestHydrateProductoVenta_FullPctjeDscto(t *testing.T) {
	t.Parallel()
	// A 100% discount is structurally valid (Hydrate does not validate).
	pct := decimal.NewFromInt(100)
	p := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		PctjeDscto: pct,
	})
	assert.True(t, pct.Equal(p.PctjeDscto()))
}
