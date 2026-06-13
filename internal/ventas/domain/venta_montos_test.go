//nolint:misspell // domain vocabulary is Spanish (productos, combos, montos) per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── recomputarMontos unit tests ───────────────────────────────────────────
//
// recomputarMontos is private; we exercise it indirectly through CrearVenta,
// ReemplazarProductos, and ReemplazarCombos — the three call sites.

// TestRecomputarMontos_StandaloneProductosOnly verifies that when a venta has
// only standalone productos (ComboID == nil), the header montos equal the sum
// of precio × cantidad per tier.
func TestRecomputarMontos_StandaloneProductosOnly(t *testing.T) {
	t.Parallel()

	p1 := decimal.RequireFromString("100.00")
	p2 := decimal.RequireFromString("50.00")
	p1Short := decimal.RequireFromString("80.00")
	p2Short := decimal.RequireFromString("40.00")
	p1Contado := decimal.RequireFromString("60.00")
	p2Contado := decimal.RequireFromString("30.00")
	qty1 := decimal.RequireFromString("2.0000") // 2 units of product 1
	qty2 := decimal.RequireFromString("3.0000") // 3 units of product 2

	prec1, _ := domain.NewMontoSnapshot(p1, p1Short, p1Contado)
	prec2, _ := domain.NewMontoSnapshot(p2, p2Short, p2Contado)
	one, two := 1, 2

	params := validCrearVentaParams(t)
	params.Productos = []domain.CrearVentaProductoInput{
		{
			ID: uuid.New(), ArticuloID: 1, Articulo: "Mesa",
			Cantidad: qty1, Precios: prec1,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		},
		{
			ID: uuid.New(), ArticuloID: 2, Articulo: "Silla",
			Cantidad: qty2, Precios: prec2,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		},
	}
	params.Combos = nil

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// anual = 100×2 + 50×3 = 200 + 150 = 350
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("350.00")),
		"anual: got %s", v.Montos().Anual())
	// cortoPlazo = 80×2 + 40×3 = 160 + 120 = 280
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("280.00")),
		"corto_plazo: got %s", v.Montos().CortoPlazo())
	// contado = 60×2 + 30×3 = 120 + 90 = 210
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("210.00")),
		"contado: got %s", v.Montos().Contado())
}

// TestRecomputarMontos_CombosOnly verifies that when a venta has only combos
// (no standalone productos), the header montos equal the sum of combo
// precio × cantidad per tier.
func TestRecomputarMontos_CombosOnly(t *testing.T) {
	t.Parallel()

	comboID := uuid.New()
	comboPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("500.00"),
		decimal.RequireFromString("450.00"),
		decimal.RequireFromString("400.00"),
	)
	comboQty := decimal.RequireFromString("2.0000")

	childPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("50.00"),
		decimal.RequireFromString("45.00"),
		decimal.RequireFromString("40.00"),
	)

	params := validCrearVentaParams(t)
	params.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Recámara", Precios: comboPrecios,
		Cantidad: comboQty, AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	// Only combo-child producto — no standalone.
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 99, Articulo: "Cama",
		Cantidad: decimal.NewFromInt(1), Precios: childPrecios,
		ComboID: &comboID, // combo child
	}}

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// anual = combo: 500×2 = 1000 (child producto excluded)
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("1000.00")),
		"anual: got %s", v.Montos().Anual())
	// cortoPlazo = 450×2 = 900
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("900.00")),
		"corto_plazo: got %s", v.Montos().CortoPlazo())
	// contado = 400×2 = 800
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("800.00")),
		"contado: got %s", v.Montos().Contado())
}

// TestRecomputarMontos_Mixed verifies the full formula:
// montos = Σ(standalone productos) + Σ(combos), excluding combo children.
func TestRecomputarMontos_Mixed(t *testing.T) {
	t.Parallel()

	comboID := uuid.New()
	comboPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("200.00"),
		decimal.RequireFromString("180.00"),
		decimal.RequireFromString("150.00"),
	)
	comboQty := decimal.RequireFromString("1.0000")

	standalonePrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("100.00"),
		decimal.RequireFromString("90.00"),
		decimal.RequireFromString("75.00"),
	)
	standaloneQty := decimal.RequireFromString("3.0000")

	childPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("999.00"),
		decimal.RequireFromString("999.00"),
		decimal.RequireFromString("999.00"),
	)

	one, two := 1, 2
	params := validCrearVentaParams(t)
	params.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Bundle", Precios: comboPrecios,
		Cantidad: comboQty, AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	params.Productos = []domain.CrearVentaProductoInput{
		{
			ID: uuid.New(), ArticuloID: 1, Articulo: "Standalone",
			Cantidad: standaloneQty, Precios: standalonePrecios,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		},
		{
			ID: uuid.New(), ArticuloID: 2, Articulo: "Child",
			Cantidad: decimal.NewFromInt(5), Precios: childPrecios,
			ComboID: &comboID, // combo child: must NOT count
		},
	}

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// anual = standalone: 100×3 + combo: 200×1 = 300 + 200 = 500 (child ignored)
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("500.00")),
		"anual: got %s", v.Montos().Anual())
	// cortoPlazo = 90×3 + 180×1 = 270 + 180 = 450
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("450.00")),
		"corto_plazo: got %s", v.Montos().CortoPlazo())
	// contado = 75×3 + 150×1 = 225 + 150 = 375
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("375.00")),
		"contado: got %s", v.Montos().Contado())
}

// TestRecomputarMontos_ComboChildrenNeverCounted asserts that even with many
// combo-child productos, the montos are determined solely by the combo price.
func TestRecomputarMontos_ComboChildrenNeverCounted(t *testing.T) {
	t.Parallel()

	comboID := uuid.New()
	comboPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("300.00"),
		decimal.RequireFromString("270.00"),
		decimal.RequireFromString("240.00"),
	)
	// Many expensive children — if counted they would inflate the total.
	childPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("100.00"),
		decimal.RequireFromString("90.00"),
		decimal.RequireFromString("80.00"),
	)

	params := validCrearVentaParams(t)
	params.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Bundle", Precios: comboPrecios,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	// 5 combo-child productos.
	children := make([]domain.CrearVentaProductoInput, 5)
	for i := range 5 {
		children[i] = domain.CrearVentaProductoInput{
			ID: uuid.New(), ArticuloID: i + 10, Articulo: "Child",
			Cantidad: decimal.NewFromInt(2), Precios: childPrecios,
			ComboID: &comboID,
		}
	}
	params.Productos = children

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// montos must equal ONLY the combo price (300/270/240), not combo + 5×(100×2).
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("300.00")),
		"anual must equal combo price only: got %s", v.Montos().Anual())
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("270.00")),
		"corto_plazo must equal combo price only: got %s", v.Montos().CortoPlazo())
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("240.00")),
		"contado must equal combo price only: got %s", v.Montos().Contado())
}

// TestRecomputarMontos_RoundingTo2Decimals verifies that fractional-unit
// multiplications are rounded to 2 decimal places (half-away-from-zero).
func TestRecomputarMontos_RoundingTo2Decimals(t *testing.T) {
	t.Parallel()

	// precio = 10.00, cantidad = 1.5 → 15.00 (exact — no rounding needed)
	// precio = 10.01, cantidad = 1.5 → 15.015 → rounds to 15.02
	precio, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("10.01"),
		decimal.RequireFromString("10.01"),
		decimal.RequireFromString("10.01"),
	)
	qty := decimal.RequireFromString("1.5")
	one, two := 1, 2

	params := validCrearVentaParams(t)
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Item",
		Cantidad: qty, Precios: precio,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}}

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// 10.01 × 1.5 = 15.015 → rounds to 15.02
	expected := decimal.RequireFromString("15.02")
	assert.True(t, v.Montos().Anual().Equal(expected),
		"expected 15.02 (15.015 rounded), got %s", v.Montos().Anual())
}

// TestRecomputarMontos_UpdatedByReemplazarProductos verifies that calling
// ReemplazarProductos updates the derived montos.
func TestRecomputarMontos_UpdatedByReemplazarProductos(t *testing.T) {
	t.Parallel()

	one, two := 1, 2
	initialPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("100.00"),
		decimal.RequireFromString("90.00"),
		decimal.RequireFromString("80.00"),
	)

	params := validCrearVentaParams(t)
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Inicial",
		Cantidad: decimal.NewFromInt(1), Precios: initialPrecios,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}}
	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// Initial montos = 100 / 90 / 80.
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("100.00")),
		"initial anual: got %s", v.Montos().Anual())

	// Replace with different prices.
	newPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("250.00"),
		decimal.RequireFromString("220.00"),
		decimal.RequireFromString("200.00"),
	)
	require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 2, Articulo: "Nuevo",
			Cantidad: decimal.NewFromInt(2), Precios: newPrecios,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
		By: uuid.New(), Now: time.Now(),
	}))

	// Montos must be recomputed: 250×2 = 500.
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("500.00")),
		"after reemplazar anual: got %s", v.Montos().Anual())
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("440.00")),
		"after reemplazar corto_plazo: got %s", v.Montos().CortoPlazo())
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("400.00")),
		"after reemplazar contado: got %s", v.Montos().Contado())
}

// TestRecomputarMontos_UpdatedByReemplazarCombos verifies that calling
// ReemplazarCombos recomputes the montos from the new combo set.
func TestRecomputarMontos_UpdatedByReemplazarCombos(t *testing.T) {
	t.Parallel()

	initialComboID := uuid.New()
	initialComboPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("300.00"),
		decimal.RequireFromString("270.00"),
		decimal.RequireFromString("240.00"),
	)
	childPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("50.00"),
		decimal.RequireFromString("45.00"),
		decimal.RequireFromString("40.00"),
	)

	params := validCrearVentaParams(t)
	params.Combos = []domain.CrearVentaComboInput{{
		ID: initialComboID, Nombre: "Bundle Inicial", Precios: initialComboPrecios,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Child",
		Cantidad: decimal.NewFromInt(1), Precios: childPrecios,
		ComboID: &initialComboID,
	}}
	v, err := domain.CrearVenta(params)
	require.NoError(t, err)

	// Initial montos = 300 (combo only, child excluded).
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("300.00")),
		"initial anual: got %s", v.Montos().Anual())

	// First reemplazar productos to standalone so combos can be replaced.
	one, two := 1, 2
	standalonePrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("10.00"),
		decimal.RequireFromString("9.00"),
		decimal.RequireFromString("8.00"),
	)
	require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 2, Articulo: "Standalone",
			Cantidad: decimal.NewFromInt(1), Precios: standalonePrecios,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
		By: uuid.New(), Now: time.Now(),
	}))

	// Now replace combos with a more expensive bundle.
	newComboPrecios, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("1000.00"),
		decimal.RequireFromString("900.00"),
		decimal.RequireFromString("800.00"),
	)
	require.NoError(t, v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: []domain.CrearVentaComboInput{{
			ID: uuid.New(), Nombre: "Bundle Nuevo", Precios: newComboPrecios,
			Cantidad: decimal.NewFromInt(2), AlmacenOrigen: 1, AlmacenDestino: 2,
		}},
		By: uuid.New(), Now: time.Now(),
	}))

	// montos = standalone(10×1) + combo(1000×2) = 10 + 2000 = 2010
	assert.True(t, v.Montos().Anual().Equal(decimal.RequireFromString("2010.00")),
		"after reemplazar combos anual: got %s", v.Montos().Anual())
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.RequireFromString("1809.00")),
		"after reemplazar combos corto_plazo: got %s", v.Montos().CortoPlazo())
	assert.True(t, v.Montos().Contado().Equal(decimal.RequireFromString("1608.00")),
		"after reemplazar combos contado: got %s", v.Montos().Contado())
}

// ─── Property tests ────────────────────────────────────────────────────────

// TestRecomputarMontos_Property_SumInvariant is a rapid property test that
// generates random sets of standalone productos, combo-child productos, and
// combos, builds a Venta, and asserts:
//  1. Montos() == Σ(standalone productos: precio_tier × qty) + Σ(combos: precio_tier × qty)
//  2. Combo children do NOT contribute to any tier.
//  3. All tiers are ≥ 0.
//  4. Montos are exactly 2 decimal places (the Round(2) invariant holds).
//  5. ReemplazarProductos and ReemplazarCombos keep the invariant.
func TestRecomputarMontos_Property_SumInvariant(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		numStandalone := rapid.IntRange(1, 5).Draw(rt, "num_standalone")
		numCombos := rapid.IntRange(0, 3).Draw(rt, "num_combos")

		type lineItem struct {
			anual, corto, contado, qty decimal.Decimal
		}

		// Generate standalone productos.
		standalones := make([]lineItem, numStandalone)
		standaloneDTOs := make([]domain.CrearVentaProductoInput, numStandalone)
		orig, dest := 1, 2
		for i := range numStandalone {
			a := rapid.Int64Range(0, 999_999).Draw(rt, "sa_a")
			c := rapid.Int64Range(0, 999_999).Draw(rt, "sa_c")
			k := rapid.Int64Range(0, 999_999).Draw(rt, "sa_k")
			q := rapid.Int64Range(1, 100).Draw(rt, "sa_q")
			anual := decimal.New(a, 0)
			corto := decimal.New(c, 0)
			contado := decimal.New(k, 0)
			qty := decimal.New(q, 0)
			standalones[i] = lineItem{anual, corto, contado, qty}
			prec, _ := domain.NewMontoSnapshot(anual, corto, contado)
			standaloneDTOs[i] = domain.CrearVentaProductoInput{
				ID: uuid.New(), ArticuloID: i + 1, Articulo: "Standalone",
				Cantidad: qty, Precios: prec,
				AlmacenOrigen: &orig, AlmacenDestino: &dest,
			}
		}

		// Generate combos and combo-child productos.
		combos := make([]lineItem, numCombos)
		comboDTOs := make([]domain.CrearVentaComboInput, numCombos)
		var childDTOs []domain.CrearVentaProductoInput
		comboIDs := make([]uuid.UUID, numCombos)
		for i := range numCombos {
			ca := rapid.Int64Range(0, 999_999).Draw(rt, "co_a")
			cc := rapid.Int64Range(0, 999_999).Draw(rt, "co_c")
			ck := rapid.Int64Range(0, 999_999).Draw(rt, "co_k")
			cq := rapid.Int64Range(1, 10).Draw(rt, "co_q")
			anual := decimal.New(ca, 0)
			corto := decimal.New(cc, 0)
			contado := decimal.New(ck, 0)
			qty := decimal.New(cq, 0)
			combos[i] = lineItem{anual, corto, contado, qty}
			prec, _ := domain.NewMontoSnapshot(anual, corto, contado)
			id := uuid.New()
			comboIDs[i] = id
			comboDTOs[i] = domain.CrearVentaComboInput{
				ID: id, Nombre: "Combo", Precios: prec,
				Cantidad: qty, AlmacenOrigen: 1, AlmacenDestino: 2,
			}
			// Add 1–2 combo children with deliberately large prices to prove
			// they never leak into the total.
			numChildren := rapid.IntRange(1, 2).Draw(rt, "num_children")
			for range numChildren {
				bigPrec, _ := domain.NewMontoSnapshot(
					decimal.New(999_999, 0),
					decimal.New(999_999, 0),
					decimal.New(999_999, 0),
				)
				childDTOs = append(childDTOs, domain.CrearVentaProductoInput{
					ID: uuid.New(), ArticuloID: 99, Articulo: "Child",
					Cantidad: decimal.NewFromInt(1), Precios: bigPrec,
					ComboID: &id,
				})
			}
		}

		// All productos = standalone + children.
		allProductos := append(standaloneDTOs, childDTOs...)

		params := validCrearVentaParams(t)
		params.Combos = comboDTOs
		params.Productos = allProductos

		v, err := domain.CrearVenta(params)
		if err != nil {
			rt.Fatalf("CrearVenta failed: %v", err)
		}

		// Independently compute expected totals.
		computeExpected := func(lines []lineItem, comboLines []lineItem) (decimal.Decimal, decimal.Decimal, decimal.Decimal) {
			var a, c, k decimal.Decimal
			for _, l := range lines {
				a = a.Add(l.anual.Mul(l.qty))
				c = c.Add(l.corto.Mul(l.qty))
				k = k.Add(l.contado.Mul(l.qty))
			}
			for _, l := range comboLines {
				a = a.Add(l.anual.Mul(l.qty))
				c = c.Add(l.corto.Mul(l.qty))
				k = k.Add(l.contado.Mul(l.qty))
			}
			return a.Round(2), c.Round(2), k.Round(2)
		}

		wantA, wantC, wantK := computeExpected(standalones, combos)

		if !v.Montos().Anual().Equal(wantA) {
			rt.Fatalf("anual mismatch: got=%s want=%s", v.Montos().Anual(), wantA)
		}
		if !v.Montos().CortoPlazo().Equal(wantC) {
			rt.Fatalf("corto_plazo mismatch: got=%s want=%s", v.Montos().CortoPlazo(), wantC)
		}
		if !v.Montos().Contado().Equal(wantK) {
			rt.Fatalf("contado mismatch: got=%s want=%s", v.Montos().Contado(), wantK)
		}

		// All tiers ≥ 0.
		if v.Montos().Anual().Sign() < 0 || v.Montos().CortoPlazo().Sign() < 0 || v.Montos().Contado().Sign() < 0 {
			rt.Fatalf("negative monto: anual=%s corto=%s contado=%s",
				v.Montos().Anual(), v.Montos().CortoPlazo(), v.Montos().Contado())
		}

		// Tiers have ≤ 2 decimal places.
		for _, d := range []decimal.Decimal{v.Montos().Anual(), v.Montos().CortoPlazo(), v.Montos().Contado()} {
			if d.Exponent() < -2 {
				rt.Fatalf("monto has more than 2 decimal places: %s (exp=%d)", d, d.Exponent())
			}
		}

		// ReemplazarProductos: replace with a fresh standalone and assert recompute.
		newPrice, _ := domain.NewMontoSnapshot(
			decimal.RequireFromString("7.00"),
			decimal.RequireFromString("6.00"),
			decimal.RequireFromString("5.00"),
		)
		require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{{
				ID: uuid.New(), ArticuloID: 1, Articulo: "Nuevo",
				Cantidad: decimal.NewFromInt(1), Precios: newPrice,
				AlmacenOrigen: &orig, AlmacenDestino: &dest,
			}},
			By: uuid.New(), Now: time.Now(),
		}))

		// After replacing, combo totals remain; standalone changes to 7/6/5.
		var comboA, comboC, comboK decimal.Decimal
		for _, l := range combos {
			comboA = comboA.Add(l.anual.Mul(l.qty))
			comboC = comboC.Add(l.corto.Mul(l.qty))
			comboK = comboK.Add(l.contado.Mul(l.qty))
		}
		wantA2 := comboA.Add(decimal.RequireFromString("7.00")).Round(2)
		wantC2 := comboC.Add(decimal.RequireFromString("6.00")).Round(2)
		wantK2 := comboK.Add(decimal.RequireFromString("5.00")).Round(2)

		if !v.Montos().Anual().Equal(wantA2) {
			rt.Fatalf("after reemplazar anual: got=%s want=%s", v.Montos().Anual(), wantA2)
		}
		if !v.Montos().CortoPlazo().Equal(wantC2) {
			rt.Fatalf("after reemplazar corto_plazo: got=%s want=%s", v.Montos().CortoPlazo(), wantC2)
		}
		if !v.Montos().Contado().Equal(wantK2) {
			rt.Fatalf("after reemplazar contado: got=%s want=%s", v.Montos().Contado(), wantK2)
		}
	})
}
