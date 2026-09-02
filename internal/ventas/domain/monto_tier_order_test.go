//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── the sale that motivated the rule ───────────────────────────────────────
//
// Venta Z00002678 carried eight chairs at $1,400 each. The capture screen was
// filled with the LINE TOTAL ($11,200) in the contado and corto plazo tiers
// and the UNIT price ($1,400) in the anual tier. recomputarMontos then
// multiplied all three by the quantity, so the header claimed $89,600 of cash
// price against $11,200 of debt.
//
// Cash is by construction the cheapest tier and the yearly plan the dearest,
// so contado > anual is an economic impossibility, never a legitimate sale.

// z00002678Precios reproduces the inverted snapshot of that venta: unit price
// in anual, line total in the two cheaper tiers.
func z00002678Precios(t *testing.T) domain.MontoSnapshot {
	t.Helper()
	m, err := domain.NewMontoSnapshot(
		decimal.NewFromInt(1400),  // anual — the unit price
		decimal.NewFromInt(11200), // corto plazo — the line total
		decimal.NewFromInt(11200), // contado — the line total
	)
	require.NoError(t, err, "the inverted snapshot is still field-wise valid; only the order is impossible")
	return m
}

// descendingTiers reorders three independently drawn prices into the only
// order a price list can have, returning them as (anual, corto plazo,
// contado). The property tests keep drawing arbitrary magnitudes; they just
// stop asserting that the domain accepts the impossible combination it now
// rejects.
func descendingTiers(a, b, c int64) (int64, int64, int64) {
	v := []int64{a, b, c}
	slices.Sort(v)
	return v[2], v[1], v[0]
}

// ─── line level ─────────────────────────────────────────────────────────────

func TestCrearVenta_RechazaProductoConContadoMayorQueAnual(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = z00002678Precios(t)

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

func TestCrearVenta_RechazaProductoConCortoPlazoMayorQueAnual(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 100, 180, 50) // corto > anual

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

// TestCrearVenta_RechazaComboConTierOrderInvertido covers the path that the
// app layer's NewMontoSnapshot call does NOT: combo prices reach the domain
// through HydrateMontoSnapshot, so the entity constructor is the only choke
// point they cross.
func TestCrearVenta_RechazaComboConTierOrderInvertido(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	comboID := uuid.New()
	p.Combos = []domain.CrearVentaComboInput{{
		ID:             comboID,
		Nombre:         "Combo invertido",
		Precios:        domain.HydrateMontoSnapshot(decimal.NewFromInt(1400), decimal.NewFromInt(11200), decimal.NewFromInt(11200)),
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}}

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

func TestVenta_ReemplazarProductos_RechazaTierOrderInvertido(t *testing.T) {
	t.Parallel()
	v, err := domain.CrearVenta(validCrearVentaParams(t))
	require.NoError(t, err)
	v.ClearPendingEvents()
	montosAntes := v.Montos()

	one, two := 1, 2
	err = v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     9,
			Articulo:       "Silla",
			Cantidad:       decimal.NewFromInt(8),
			Precios:        z00002678Precios(t),
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
		By:  uuid.New(),
		Now: time.Now().UTC(),
	})

	requireCode(t, err, "precio_tier_order_invalid")
	assert.True(t, montosAntes.Equals(v.Montos()), "a rejected mutation must not move the header totals")
	assert.Empty(t, v.PendingEvents(), "a rejected mutation must not emit events")
}

// TestCrearVenta_AceptaTiersIguales pins the boundary: a sale priced the same
// in the three tiers is legitimate, so the rule must be ≤, not <.
func TestCrearVenta_AceptaTiersIguales(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 1400, 1400, 1400)

	v, err := domain.CrearVenta(p)

	require.NoError(t, err)
	assert.True(t, v.Montos().Contado().Equal(decimal.NewFromInt(1400)))
}

// ─── header level ───────────────────────────────────────────────────────────

// ventaHidratadaConLineaInvertida rebuilds a borrador venta straight from
// persistence carrying a producto whose tiers are inverted. Hydration skips
// validation on purpose, so this is the shape a venta captured BEFORE the
// rule existed has when the desktop loads it to edit — the only way the
// header check can fire once every freshly built line is validated.
func ventaHidratadaConLineaInvertida(t *testing.T) *domain.Venta {
	t.Helper()
	base, err := domain.CrearVenta(validCrearVentaParams(t))
	require.NoError(t, err)

	now := time.Now().UTC()
	by := uuid.New()
	one, two := 1, 2
	malo := domain.HydrateProducto(domain.HydrateProductoParams{
		ID:             uuid.New(),
		ArticuloID:     9,
		Articulo:       "Silla capturada antes de la regla",
		Cantidad:       decimal.NewFromInt(8),
		Precios:        z00002678Precios(t),
		AlmacenOrigen:  &one,
		AlmacenDestino: &two,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      by,
		UpdatedBy:      by,
	})

	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             base.ID(),
		Cliente:        base.Cliente(),
		Direccion:      base.Direccion(),
		GPS:            base.GPS(),
		FechaVenta:     base.FechaVenta(),
		TipoVenta:      base.TipoVenta(),
		Montos:         base.Montos(),
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		Productos:      []*domain.Producto{malo},
		Vendedores:     base.VendedoresForRepo(),
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      by,
		UpdatedBy:      by,
	})
}

// TestVenta_ReemplazarCombos_RechazaTotalesInvertidos is the header-level
// half of the rule. ReemplazarCombos rebuilds only the combos, so the
// inverted producto survives the line-level check and the impossible order
// appears for the first time in the recomputed header totals.
func TestVenta_ReemplazarCombos_RechazaTotalesInvertidos(t *testing.T) {
	t.Parallel()
	v := ventaHidratadaConLineaInvertida(t)
	combosAntes := v.CombosCount()
	montosAntes := v.Montos()

	err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: []domain.CrearVentaComboInput{{
			ID:             uuid.New(),
			Nombre:         "Combo sano",
			Precios:        lineasMontos(t, 500, 450, 400),
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		}},
		By:  uuid.New(),
		Now: time.Now().UTC(),
	})

	requireCode(t, err, "monto_tier_order_invalid")
	assert.Equal(t, combosAntes, v.CombosCount(), "a rejected mutation must not install the new combos")
	assert.True(t, montosAntes.Equals(v.Montos()), "a rejected mutation must not move the header totals")
	assert.Empty(t, v.PendingEvents(), "a rejected mutation must not emit events")
}

// TestVenta_ReemplazarCombos_ControlPositivo proves the previous test is not
// vacuous: the same mutation on the same venta shape succeeds once the
// hydrated producto carries a possible tier order.
func TestVenta_ReemplazarCombos_ControlPositivo(t *testing.T) {
	t.Parallel()
	v, err := domain.CrearVenta(validCrearVentaParams(t))
	require.NoError(t, err)
	v.ClearPendingEvents()

	err = v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: []domain.CrearVentaComboInput{{
			ID:             uuid.New(),
			Nombre:         "Combo sano",
			Precios:        lineasMontos(t, 500, 450, 400),
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		}},
		By:  uuid.New(),
		Now: time.Now().UTC(),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, v.CombosCount())
}
