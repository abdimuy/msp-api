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

	"github.com/abdimuy/msp-api/internal/platform/apperror"
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

// TestCrearVenta_RechazaProductoConContadoMayorQueCortoPlazo covers the FIRST
// clause of the rule on its own — contado > corto plazo while contado is still
// ≤ anual — which no other case reaches.
//
// It has to be explicit. Every other negative case here breaks the ordering so
// badly that the second clause (corto > anual) fires first, so deleting
// `contado > cortoPlazo` from validateMontoTierOrder left the whole ventas
// suite green. And the property tests cannot cover it either: they now sort
// the drawn prices, which is right for what they assert but removes exactly
// this region from the space they explore.
//
// The shape is real: 1000 / 500 / 800 is a price list where somebody typed the
// corto plazo below the contado.
func TestCrearVenta_RechazaProductoConContadoMayorQueCortoPlazo(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 1000, 500, 800)

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

// TestVenta_ReemplazarCombos_RechazaTotalesConContadoMayorQueCortoPlazo is the
// same clause at the header level, for the same reason.
func TestVenta_ReemplazarCombos_RechazaTotalesConContadoMayorQueCortoPlazo(t *testing.T) {
	t.Parallel()
	v := ventaHidratadaConLinea(t, lineasMontos(t, 1000, 500, 800))

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

// TestCrearVenta_RechazoNombraLaLineaCulpable is what makes the rejection
// usable. The failed-intent screen shows the message and the fields, so a
// venta of twenty lines has to say WHICH one broke the rule and with what
// numbers — otherwise the operator has to open the code to act.
func TestCrearVenta_RechazoNombraLaLineaCulpable(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Articulo = "Silla Milán tapizada"
	p.Productos[0].Precios = z00002678Precios(t)

	_, err := domain.CrearVenta(p)

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T", err)
	assert.Equal(t, "Silla Milán tapizada", ae.Fields["articulo"],
		"the rejection must name the line, not just the rule")
	assert.Equal(t, "1400.00", ae.Fields["precio_anual"])
	assert.Equal(t, "11200.00", ae.Fields["precio_corto"])
	assert.Equal(t, "11200.00", ae.Fields["precio_contado"])
}

// TestCrearVenta_RechazoDeComboNombraElCombo is the same for the combo path,
// whose lines carry a nombre instead of an articulo.
func TestCrearVenta_RechazoDeComboNombraElCombo(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Combos = []domain.CrearVentaComboInput{{
		ID:             uuid.New(),
		Nombre:         "Combo Recámara",
		Precios:        domain.HydrateMontoSnapshot(decimal.NewFromInt(1400), decimal.NewFromInt(11200), decimal.NewFromInt(11200)),
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}}

	_, err := domain.CrearVenta(p)

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T", err)
	assert.Equal(t, "Combo Recámara", ae.Fields["combo"])
}

// TestVenta_RechazoDeEncabezadoLlevaLosTotales: at header level there is no
// single line to blame, so what has to travel is the totals that broke the
// rule.
func TestVenta_RechazoDeEncabezadoLlevaLosTotales(t *testing.T) {
	t.Parallel()
	v := ventaHidratadaConLinea(t, z00002678Precios(t))

	err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: []domain.CrearVentaComboInput{{
			ID: uuid.New(), Nombre: "Combo sano", Precios: lineasMontos(t, 500, 450, 400),
			Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
		}},
		By: uuid.New(), Now: time.Now().UTC(),
	})

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T", err)
	// 8 sillas invertidas + el combo sano: los totales que el encabezado rechaza.
	assert.Equal(t, "11700.00", ae.Fields["precio_anual"])
	assert.Equal(t, "90050.00", ae.Fields["precio_corto"])
	assert.Equal(t, "90000.00", ae.Fields["precio_contado"])
	assert.Nil(t, ae.Fields["articulo"], "the header rejection blames no single line")
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

// ventaHidratadaConLinea rebuilds a borrador venta straight from persistence
// carrying a producto with the given prices. Hydration skips
// validation on purpose, so this is the shape a venta captured BEFORE the
// rule existed has when the desktop loads it to edit — the only way the
// header check can fire once every freshly built line is validated.
func ventaHidratadaConLinea(t *testing.T, precios domain.MontoSnapshot) *domain.Venta {
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
		Precios:        precios,
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
	v := ventaHidratadaConLinea(t, z00002678Precios(t))
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

// ─── escalones ausentes ─────────────────────────────────────────────────────
//
// Medido en producción sobre 1,049 líneas de producto: de las 160 líneas de
// venta CONTADO, 103 llevan `anual` y `corto plazo` en cero. No es un defecto
// de captura — una venta de contado no tiene precios a crédito, así que esos
// escalones no existen. Una regla que exigiera los tres presentes rechazaría
// esas 103 líneas en el mostrador, delante del cliente.
//
// El orden se valida entonces sólo entre los escalones PRESENTES, en su orden
// canónico contado → corto plazo → anual. Lo que la regla rechaza sigue siendo
// lo imposible; lo inusual (un precio ocho veces la lista) lo juzga la persona.

// TestCrearVenta_AceptaContadoSinEscalonesDeCredito es el control positivo de
// las 103 líneas: sólo hay precio de contado, así que no hay nada que comparar.
func TestCrearVenta_AceptaContadoSinEscalonesDeCredito(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 0, 0, 500) // anual 0, corto 0, contado 500

	v, err := domain.CrearVenta(p)

	require.NoError(t, err, "una venta de contado sin precios a crédito es captura legítima")
	assert.True(t, v.Montos().Contado().Equal(decimal.NewFromInt(500)))
	assert.True(t, v.Montos().Anual().IsZero())
}

// TestCrearVenta_AceptaSoloAnual es el simétrico: una venta a crédito sin
// precio de contado capturado.
func TestCrearVenta_AceptaSoloAnual(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 1400, 0, 0)

	_, err := domain.CrearVenta(p)

	require.NoError(t, err)
}

// TestCrearVenta_AceptaDosEscalonesPresentesEnOrden: con dos escalones
// presentes y crecientes no hay nada que rechazar, aunque el tercero falte.
func TestCrearVenta_AceptaDosEscalonesPresentesEnOrden(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 0, 600, 500) // anual ausente

	_, err := domain.CrearVenta(p)

	require.NoError(t, err)
}

// TestCrearVenta_AceptaLosTresEscalonesCrecientes fija que el caso sano de
// tres escalones sigue pasando: es el 100% de las 889 líneas de CRÉDITO.
func TestCrearVenta_AceptaLosTresEscalonesCrecientes(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	m, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("1400"),
		decimal.RequireFromString("1112.50"),
		decimal.RequireFromString("962.50"),
	)
	require.NoError(t, err)
	p.Productos[0].Precios = m

	_, err = domain.CrearVenta(p)

	require.NoError(t, err)
}

// TestCrearVenta_RechazaCortoMayorQueAnualConLosTresPresentes es el defecto
// real que la regla existe para atrapar, con los tres escalones capturados.
func TestCrearVenta_RechazaCortoMayorQueAnualConLosTresPresentes(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 1400, 8900, 7700)

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

// TestCrearVenta_RechazaContadoMayorQueAnualConCortoAusente es la prueba que
// cierra el agujero que abriría la implementación ingenua.
//
// Saltarse los ceros NO puede degradarse a "comparar pares adyacentes y
// omitir el par que toque un cero": con el corto plazo ausente, eso dejaría
// pasar contado 7700 contra anual 1400 — exactamente la forma de la venta de
// las 8 sillas si el capturista hubiera dejado el escalón de en medio vacío.
// El contado tiene que seguir midiéndose contra el anual.
func TestCrearVenta_RechazaContadoMayorQueAnualConCortoAusente(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 1400, 0, 7700)

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

// TestCrearVenta_RechazaContadoMayorQueCortoConAnualAusente: el mismo cierre
// para el otro escalón ausente.
func TestCrearVenta_RechazaContadoMayorQueCortoConAnualAusente(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 0, 500, 800)

	_, err := domain.CrearVenta(p)

	requireCode(t, err, "precio_tier_order_invalid")
}

// TestCrearVenta_AceptaTodosLosEscalonesEnCero: una línea sin ningún precio no
// tiene orden que romper. Que se acepte aquí no la vuelve válida — el monto
// cero lo juzgan otras reglas, no ésta.
func TestCrearVenta_AceptaTodosLosEscalonesEnCero(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Precios = lineasMontos(t, 0, 0, 0)

	_, err := domain.CrearVenta(p)

	require.NoError(t, err)
}
