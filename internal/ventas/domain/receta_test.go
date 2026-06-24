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

// ─── helpers ──────────────────────────────────────────────────────────────────

// ventaConCombos builds a Venta that contains combos and their child
// productos. comboSpecs is a slice of (comboID, []articuloID) pairs that
// drive the combos+productos. standalone is a slice of stand-alone
// (non-combo) articulo IDs.
type comboSpec struct {
	id          uuid.UUID
	articuloIDs []int
	unidades    []decimal.Decimal // len must match articuloIDs; use nil for all-ones
}

func buildVentaConCombos(t *testing.T, specs []comboSpec, standalone []int) *domain.Venta {
	t.Helper()

	nom, err := domain.NewNombreCliente("Test Cliente")
	require.NoError(t, err)
	cliente, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(20.0, -100.0)
	require.NoError(t, err)
	montos, err := domain.NewMontoSnapshot(
		decimal.NewFromInt(1000),
		decimal.NewFromInt(800),
		decimal.NewFromInt(500),
	)
	require.NoError(t, err)

	one, two := 1, 2

	var combosIn []domain.CrearVentaComboInput
	var productosIn []domain.CrearVentaProductoInput

	for _, spec := range specs {
		combosIn = append(combosIn, domain.CrearVentaComboInput{
			ID:             spec.id,
			Nombre:         "Combo " + spec.id.String()[:8],
			Precios:        montos,
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		})
		for i, artID := range spec.articuloIDs {
			u := decimal.NewFromInt(1)
			if spec.unidades != nil {
				u = spec.unidades[i]
			}
			comboRef := spec.id
			productosIn = append(productosIn, domain.CrearVentaProductoInput{
				ID:         uuid.New(),
				ArticuloID: artID,
				Articulo:   "Art",
				Cantidad:   u,
				Precios:    montos,
				ComboID:    &comboRef,
			})
		}
	}

	// Stand-alone productos (ComboID == nil).
	for _, artID := range standalone {
		productosIn = append(productosIn, domain.CrearVentaProductoInput{
			ID:             uuid.New(),
			ArticuloID:     artID,
			Articulo:       "Standalone",
			Cantidad:       decimal.NewFromInt(1),
			Precios:        montos,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		})
	}

	// CrearVenta requires at least one producto; the helper always guarantees
	// that via caller convention. Add a fallback stand-alone if both slices
	// are empty.
	if len(productosIn) == 0 {
		productosIn = append(productosIn, domain.CrearVentaProductoInput{
			ID:             uuid.New(),
			ArticuloID:     999,
			Articulo:       "Fallback",
			Cantidad:       decimal.NewFromInt(1),
			Precios:        montos,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		})
	}

	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Combos:     combosIn,
		Productos:  productosIn,
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: uuid.New(),
			Email:     "v@x.com",
			Nombre:    "Vendedor",
		}},
		CreatedBy: uuid.New(),
		Now:       time.Now(),
	})
	require.NoError(t, err)
	return v
}

// ─── RecetaComponente ─────────────────────────────────────────────────────────

func TestNewRecetaComponente_Valid(t *testing.T) {
	t.Parallel()
	rc, err := domain.NewRecetaComponente(42, decimal.NewFromInt(3))
	require.NoError(t, err)
	assert.Equal(t, 42, rc.ArticuloID())
	assert.True(t, rc.Unidades().Equal(decimal.NewFromInt(3)))
}

func TestNewRecetaComponente_InvalidArticuloID(t *testing.T) {
	t.Parallel()
	_, err := domain.NewRecetaComponente(0, decimal.NewFromInt(1))
	require.ErrorIs(t, err, domain.ErrRecetaArticuloIDInvalido)

	_, err = domain.NewRecetaComponente(-5, decimal.NewFromInt(1))
	require.ErrorIs(t, err, domain.ErrRecetaArticuloIDInvalido)
}

func TestNewRecetaComponente_InvalidUnidades(t *testing.T) {
	t.Parallel()
	// zero
	_, err := domain.NewRecetaComponente(1, decimal.Zero)
	require.ErrorIs(t, err, domain.ErrRecetaUnidadesNoPositivas)

	// negative
	_, err = domain.NewRecetaComponente(1, decimal.NewFromInt(-1))
	require.ErrorIs(t, err, domain.ErrRecetaUnidadesNoPositivas)
}

func TestNewRecetaComponente_ScaleValidation(t *testing.T) {
	t.Parallel()
	// > 4 dp should fail (cantidadScaleMax = 4)
	tooManyDp := decimal.RequireFromString("1.00001")
	_, err := domain.NewRecetaComponente(1, tooManyDp)
	require.Error(t, err, "5 dp should be rejected")

	// exactly 4 dp should pass
	fourDp := decimal.RequireFromString("1.0001")
	rc, err := domain.NewRecetaComponente(1, fourDp)
	require.NoError(t, err)
	assert.True(t, rc.Unidades().Equal(fourDp))
}

func TestRecetaComponente_Equal(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewRecetaComponente(10, decimal.NewFromInt(2))
	b, _ := domain.NewRecetaComponente(10, decimal.NewFromInt(2))
	c, _ := domain.NewRecetaComponente(10, decimal.NewFromInt(3))
	d, _ := domain.NewRecetaComponente(20, decimal.NewFromInt(2))

	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))
	assert.False(t, a.Equal(d))
}

// ─── RecetaDeCombo — table tests ──────────────────────────────────────────────

func TestRecetaDeCombo_OneComponent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	v := buildVentaConCombos(t, []comboSpec{
		{id: id, articuloIDs: []int{10}},
	}, nil)

	r, err := v.RecetaDeCombo(id)
	require.NoError(t, err)

	comps := r.Componentes()
	require.Len(t, comps, 1)
	assert.Equal(t, 10, comps[0].ArticuloID())
	assert.True(t, comps[0].Unidades().Equal(decimal.NewFromInt(1)))
}

func TestRecetaDeCombo_NComponents_SortedByArticuloID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	// Intentionally provide in reverse order — must come out sorted ascending.
	v := buildVentaConCombos(t, []comboSpec{
		{
			id:          id,
			articuloIDs: []int{30, 10, 20},
			unidades:    []decimal.Decimal{decimal.NewFromInt(3), decimal.NewFromInt(1), decimal.NewFromInt(2)},
		},
	}, nil)

	r, err := v.RecetaDeCombo(id)
	require.NoError(t, err)

	comps := r.Componentes()
	require.Len(t, comps, 3)
	assert.Equal(t, 10, comps[0].ArticuloID())
	assert.Equal(t, 20, comps[1].ArticuloID())
	assert.Equal(t, 30, comps[2].ArticuloID())
	assert.True(t, comps[0].Unidades().Equal(decimal.NewFromInt(1)))
	assert.True(t, comps[1].Unidades().Equal(decimal.NewFromInt(2)))
	assert.True(t, comps[2].Unidades().Equal(decimal.NewFromInt(3)))
}

func TestRecetaDeCombo_DifferentInsertionOrder_SameFirma(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()

	// Combo 1: articles in ascending order.
	// Combo 2: same articles in descending order. Same montos.
	montos, _ := domain.NewMontoSnapshot(
		decimal.NewFromInt(100),
		decimal.NewFromInt(80),
		decimal.NewFromInt(50),
	)
	one, two := 1, 2
	nom, _ := domain.NewNombreCliente("Test")
	cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	gps, _ := domain.NewGPSCoords(20.0, -100.0)

	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Combos: []domain.CrearVentaComboInput{
			{ID: id1, Nombre: "Combo A", Precios: montos, Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2},
			{ID: id2, Nombre: "Combo B", Precios: montos, Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2},
		},
		Productos: []domain.CrearVentaProductoInput{
			// Combo 1: ascending order
			{ID: uuid.New(), ArticuloID: 10, Articulo: "A", Cantidad: decimal.NewFromInt(1), Precios: montos, ComboID: &id1},
			{ID: uuid.New(), ArticuloID: 20, Articulo: "B", Cantidad: decimal.NewFromInt(2), Precios: montos, ComboID: &id1},
			{ID: uuid.New(), ArticuloID: 30, Articulo: "C", Cantidad: decimal.NewFromInt(3), Precios: montos, ComboID: &id1},
			// Combo 2: descending order (same articuloIDs and unidades as combo 1)
			{ID: uuid.New(), ArticuloID: 30, Articulo: "C", Cantidad: decimal.NewFromInt(3), Precios: montos, ComboID: &id2},
			{ID: uuid.New(), ArticuloID: 20, Articulo: "B", Cantidad: decimal.NewFromInt(2), Precios: montos, ComboID: &id2},
			{ID: uuid.New(), ArticuloID: 10, Articulo: "A", Cantidad: decimal.NewFromInt(1), Precios: montos, ComboID: &id2},
			// Stand-alone to satisfy venta invariant (no, combos already have productos)
			{ID: uuid.New(), ArticuloID: 99, Articulo: "SA", Cantidad: decimal.NewFromInt(1), Precios: montos, AlmacenOrigen: &one, AlmacenDestino: &two},
		},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(), Email: "v@x.com", Nombre: "V",
		}},
		CreatedBy: uuid.New(),
		Now:       time.Now(),
	})
	require.NoError(t, err)

	r1, err := v.RecetaDeCombo(id1)
	require.NoError(t, err)
	r2, err := v.RecetaDeCombo(id2)
	require.NoError(t, err)

	assert.Equal(t, r1.Firma(), r2.Firma(), "same components in different order must produce equal Firma")
	assert.True(t, r1.Equal(r2))
}

func TestRecetaDeCombo_FractionalUnidades(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	v := buildVentaConCombos(t, []comboSpec{
		{
			id:          id,
			articuloIDs: []int{5},
			unidades:    []decimal.Decimal{decimal.RequireFromString("1.5")},
		},
	}, nil)

	r, err := v.RecetaDeCombo(id)
	require.NoError(t, err)

	comps := r.Componentes()
	require.Len(t, comps, 1)
	assert.True(t, comps[0].Unidades().Equal(decimal.RequireFromString("1.5")))
}

func TestRecetaDeCombo_NoChildren_ReturnsError(t *testing.T) {
	t.Parallel()
	// Build a venta with a combo but zero child productos for that combo.
	// We do this by building the venta with a stand-alone producto only and
	// adding a combo manually via HydrateVenta so we bypass domain validation.
	// Easier: create the combo via CrearVenta with one child, then test by
	// asking for a combo ID that has no children.
	//
	// Actually the simplest approach: ask for a nonexistent comboID → ErrComboNoEncontrado.
	// For ErrComboSinComponentes: we need a combo whose ID maps to zero child productos.
	// That state is hard to reach via CrearVenta (it allows combos only alongside their productos).
	// Use HydrateVenta to construct such a venta directly.

	comboID := uuid.New()
	montos, _ := domain.NewMontoSnapshot(
		decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50),
	)
	one, two := 1, 2
	nom, _ := domain.NewNombreCliente("Test")
	cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	gps, _ := domain.NewGPSCoords(20.0, -100.0)
	now := time.Now()
	creator := uuid.New()

	// Hydrate a venta with one combo and one stand-alone producto (no child for the combo).
	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Montos:     montos,
		Estado:     domain.EstadoActive,
		Situacion:  domain.SituacionBorrador,
		Combos: []*domain.Combo{
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             comboID,
				Nombre:         "Combo Vacío",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now,
				UpdatedAt:      now,
				CreatedBy:      creator,
				UpdatedBy:      creator,
			}),
		},
		Productos: []*domain.Producto{
			// stand-alone, NOT a child of comboID
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:             uuid.New(),
				ArticuloID:     99,
				Articulo:       "SA",
				Cantidad:       decimal.NewFromInt(1),
				Precios:        montos,
				ComboID:        nil,
				AlmacenOrigen:  &one,
				AlmacenDestino: &two,
				CreatedAt:      now,
				UpdatedAt:      now,
				CreatedBy:      creator,
				UpdatedBy:      creator,
			}),
		},
		Vendedores: []*domain.Vendedor{},
		CreatedAt:  now,
		UpdatedAt:  now,
		CreatedBy:  creator,
		UpdatedBy:  creator,
	})

	_, err := v.RecetaDeCombo(comboID)
	require.ErrorIs(t, err, domain.ErrComboSinComponentes)
}

func TestRecetaDeCombo_UnknownComboID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	v := buildVentaConCombos(t, []comboSpec{
		{id: id, articuloIDs: []int{10}},
	}, nil)

	_, err := v.RecetaDeCombo(uuid.New()) // nonexistent
	require.ErrorIs(t, err, domain.ErrComboNoEncontrado)
}

func TestRecetaDeCombo_StandaloneProductosIgnored(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	// Venta has one combo child (articuloID=10) and one stand-alone (articuloID=99).
	v := buildVentaConCombos(t, []comboSpec{
		{id: id, articuloIDs: []int{10}},
	}, []int{99})

	r, err := v.RecetaDeCombo(id)
	require.NoError(t, err)

	comps := r.Componentes()
	require.Len(t, comps, 1)
	assert.Equal(t, 10, comps[0].ArticuloID(), "stand-alone producto must not appear in receta")
}

func TestRecetasDeCombos_TwoCombos_IndependentRecetas(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()
	v := buildVentaConCombos(t, []comboSpec{
		{id: id1, articuloIDs: []int{10, 20}},
		{id: id2, articuloIDs: []int{30}},
	}, nil)

	recetas, err := v.RecetasDeCombos()
	require.NoError(t, err)
	require.Len(t, recetas, 2)

	r1 := recetas[id1]
	r2 := recetas[id2]
	require.Len(t, r1.Componentes(), 2)
	require.Len(t, r2.Componentes(), 1)
	assert.Equal(t, 10, r1.Componentes()[0].ArticuloID())
	assert.Equal(t, 20, r1.Componentes()[1].ArticuloID())
	assert.Equal(t, 30, r2.Componentes()[0].ArticuloID())
}

func TestRecetasDeCombos_NoCombos_EmptyMap(t *testing.T) {
	t.Parallel()
	// Venta with no combos — only stand-alone productos.
	v := buildVentaConCombos(t, nil, []int{10})
	recetas, err := v.RecetasDeCombos()
	require.NoError(t, err)
	assert.Empty(t, recetas)
}

func TestRecetaDeCombo_DuplicateArticuloID_Aggregated(t *testing.T) {
	t.Parallel()
	// A combo where two productos share the same articuloID — unidades must be summed.
	comboID := uuid.New()
	montos, _ := domain.NewMontoSnapshot(
		decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50),
	)
	one, two := 1, 2
	nom, _ := domain.NewNombreCliente("Test")
	cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	gps, _ := domain.NewGPSCoords(20.0, -100.0)
	now := time.Now()
	creator := uuid.New()

	// Hydrate directly so we can craft two productos with the same articuloID
	// in the same combo (CrearVenta does not reject this — it's unusual but valid).
	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Montos:     montos,
		Estado:     domain.EstadoActive,
		Situacion:  domain.SituacionBorrador,
		Combos: []*domain.Combo{
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             comboID,
				Nombre:         "Combo Dup",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now,
				UpdatedAt:      now,
				CreatedBy:      creator,
				UpdatedBy:      creator,
			}),
		},
		Productos: []*domain.Producto{
			// two rows, same articuloID=7, unidades 2 + 3 = 5
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: 7,
				Articulo:   "Art A",
				Cantidad:   decimal.NewFromInt(2),
				Precios:    montos,
				ComboID:    &comboID,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: 7,
				Articulo:   "Art A dup",
				Cantidad:   decimal.NewFromInt(3),
				Precios:    montos,
				ComboID:    &comboID,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			// stand-alone
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:             uuid.New(),
				ArticuloID:     99,
				Articulo:       "SA",
				Cantidad:       decimal.NewFromInt(1),
				Precios:        montos,
				ComboID:        nil,
				AlmacenOrigen:  &one,
				AlmacenDestino: &two,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
		},
		Vendedores: []*domain.Vendedor{},
		CreatedAt:  now,
		UpdatedAt:  now,
		CreatedBy:  creator,
		UpdatedBy:  creator,
	})

	r, err := v.RecetaDeCombo(comboID)
	require.NoError(t, err)

	comps := r.Componentes()
	require.Len(t, comps, 1, "duplicate articuloID must be merged into one component")
	assert.Equal(t, 7, comps[0].ArticuloID())
	assert.True(t, comps[0].Unidades().Equal(decimal.NewFromInt(5)), "2+3=5 expected")
}

func TestReceta_Firma_OneVsOneDotZero(t *testing.T) {
	t.Parallel()
	// unidades 1 and 1.00 must produce the same Firma (both become "1.000000").
	id1, id2 := uuid.New(), uuid.New()
	montos, _ := domain.NewMontoSnapshot(
		decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50),
	)
	one, two := 1, 2
	nom, _ := domain.NewNombreCliente("Test")
	cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	gps, _ := domain.NewGPSCoords(20.0, -100.0)
	now := time.Now()
	creator := uuid.New()

	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Montos:     montos,
		Estado:     domain.EstadoActive,
		Situacion:  domain.SituacionBorrador,
		Combos: []*domain.Combo{
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             id1,
				Nombre:         "Combo A",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             id2,
				Nombre:         "Combo B",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
		},
		Productos: []*domain.Producto{
			// Combo A: unidades = 1 (integer)
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: 5,
				Articulo:   "Art",
				Cantidad:   decimal.NewFromInt(1),
				Precios:    montos,
				ComboID:    &id1,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			// Combo B: unidades = 1.00 (explicit 2dp)
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: 5,
				Articulo:   "Art",
				Cantidad:   decimal.RequireFromString("1.00"),
				Precios:    montos,
				ComboID:    &id2,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			// stand-alone
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:             uuid.New(),
				ArticuloID:     99,
				Articulo:       "SA",
				Cantidad:       decimal.NewFromInt(1),
				Precios:        montos,
				AlmacenOrigen:  &one,
				AlmacenDestino: &two,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
		},
		Vendedores: []*domain.Vendedor{},
		CreatedAt:  now,
		UpdatedAt:  now,
		CreatedBy:  creator,
		UpdatedBy:  creator,
	})

	r1, err := v.RecetaDeCombo(id1)
	require.NoError(t, err)
	r2, err := v.RecetaDeCombo(id2)
	require.NoError(t, err)

	assert.Equal(t, r1.Firma(), r2.Firma(), "integer 1 and 1.00 must produce the same Firma")
}

func TestRecetasDeCombos_PropagatesComboSinComponentes(t *testing.T) {
	t.Parallel()
	// A venta with two combos where one has no child productos must propagate
	// ErrComboSinComponentes from RecetasDeCombos.
	id1, id2 := uuid.New(), uuid.New()
	montos, _ := domain.NewMontoSnapshot(
		decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50),
	)
	one, two := 1, 2
	nom, _ := domain.NewNombreCliente("Test")
	cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	gps, _ := domain.NewGPSCoords(20.0, -100.0)
	now := time.Now()
	creator := uuid.New()

	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Montos:     montos,
		Estado:     domain.EstadoActive,
		Situacion:  domain.SituacionBorrador,
		Combos: []*domain.Combo{
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             id1,
				Nombre:         "Combo Con Hijos",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			domain.HydrateCombo(domain.HydrateComboParams{
				ID:             id2,
				Nombre:         "Combo Sin Hijos",
				Precios:        montos,
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  1,
				AlmacenDestino: 2,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
		},
		Productos: []*domain.Producto{
			// child of id1 only
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: 10,
				Articulo:   "Art",
				Cantidad:   decimal.NewFromInt(1),
				Precios:    montos,
				ComboID:    &id1,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
			// stand-alone
			domain.HydrateProducto(domain.HydrateProductoParams{
				ID:             uuid.New(),
				ArticuloID:     99,
				Articulo:       "SA",
				Cantidad:       decimal.NewFromInt(1),
				Precios:        montos,
				AlmacenOrigen:  &one,
				AlmacenDestino: &two,
				CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			}),
		},
		Vendedores: []*domain.Vendedor{},
		CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
	})

	_, err := v.RecetasDeCombos()
	require.ErrorIs(t, err, domain.ErrComboSinComponentes)
}

// ─── Property tests ───────────────────────────────────────────────────────────

// TestReceta_Firma_InvariantUnderPermutation asserts that any random set of
// RecetaComponente produces the same Firma regardless of insertion order into
// the source productos slice — i.e., Firma is a canonical deterministic hash.
func TestReceta_Firma_InvariantUnderPermutation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(t, "n")
		// Generate n distinct articuloIDs to avoid duplicate-merging complexity.
		artIDs := make([]int, n)
		seen := make(map[int]struct{}, n)
		for i := range n {
			var id int
			for {
				id = rapid.IntRange(1, 1000).Draw(t, "artID")
				if _, ok := seen[id]; !ok {
					break
				}
			}
			seen[id] = struct{}{}
			artIDs[i] = id
		}

		// Build a venta with combos listed in the original order and
		// in the reversed order; both should produce the same Firma.
		montos, _ := domain.NewMontoSnapshot(
			decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50),
		)
		one, two := 1, 2
		nom, _ := domain.NewNombreCliente("Test")
		cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
		dir, _ := domain.NewDireccion(domain.NewDireccionParams{
			Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
		})
		gps, _ := domain.NewGPSCoords(20.0, -100.0)
		now := time.Now()
		creator := uuid.New()

		id1, id2 := uuid.New(), uuid.New()

		// Build all productos: combo1 (original order) + combo2 (reversed) + stand-alone.
		// Use a single pre-sized slice filled by index to avoid makezero lint issues.
		allProductos := make([]*domain.Producto, 0, 2*n+1)
		for i, artID := range artIDs {
			p1 := domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: artID,
				Articulo:   "Art",
				Cantidad:   decimal.NewFromInt(1),
				Precios:    montos,
				ComboID:    &id1,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			})
			rev := artIDs[n-1-i]
			p2 := domain.HydrateProducto(domain.HydrateProductoParams{
				ID:         uuid.New(),
				ArticuloID: rev,
				Articulo:   "Art",
				Cantidad:   decimal.NewFromInt(1),
				Precios:    montos,
				ComboID:    &id2,
				CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
			})
			allProductos = append(allProductos, p1, p2)
		}

		// Stand-alone producto so the venta itself remains valid.
		standAlone := domain.HydrateProducto(domain.HydrateProductoParams{
			ID:             uuid.New(),
			ArticuloID:     9999,
			Articulo:       "SA",
			Cantidad:       decimal.NewFromInt(1),
			Precios:        montos,
			ComboID:        nil,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
			CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
		})
		allProductos = append(allProductos, standAlone)

		v := domain.HydrateVenta(domain.HydrateVentaParams{
			ID:         uuid.New(),
			Cliente:    cliente,
			Direccion:  dir,
			GPS:        gps,
			FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			TipoVenta:  domain.TipoVentaContado,
			Montos:     montos,
			Estado:     domain.EstadoActive,
			Situacion:  domain.SituacionBorrador,
			Combos: []*domain.Combo{
				domain.HydrateCombo(domain.HydrateComboParams{
					ID:             id1,
					Nombre:         "C1",
					Precios:        montos,
					Cantidad:       decimal.NewFromInt(1),
					AlmacenOrigen:  1,
					AlmacenDestino: 2,
					CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
				}),
				domain.HydrateCombo(domain.HydrateComboParams{
					ID:             id2,
					Nombre:         "C2",
					Precios:        montos,
					Cantidad:       decimal.NewFromInt(1),
					AlmacenOrigen:  1,
					AlmacenDestino: 2,
					CreatedAt:      now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
				}),
			},
			Productos:  allProductos,
			Vendedores: []*domain.Vendedor{},
			CreatedAt:  now, UpdatedAt: now, CreatedBy: creator, UpdatedBy: creator,
		})

		r1, err := v.RecetaDeCombo(id1)
		if err != nil {
			t.Fatalf("RecetaDeCombo id1: %v", err)
		}
		r2, err := v.RecetaDeCombo(id2)
		if err != nil {
			t.Fatalf("RecetaDeCombo id2: %v", err)
		}
		if r1.Firma() != r2.Firma() {
			t.Fatalf("Firma not invariant under permutation: %q vs %q", r1.Firma(), r2.Firma())
		}
	})
}

// ─── Fuzz tests ───────────────────────────────────────────────────────────────

// FuzzNewRecetaComponente verifies that NewRecetaComponente never panics and
// that accepted values satisfy the documented invariants.
func FuzzNewRecetaComponente(f *testing.F) {
	type seed struct {
		mantissa, units int64
		exp             int32
	}
	for _, s := range []seed{
		{1, 1, 0},
		{0, 1, 0},
		{-1, 1, 0},
		{100, 0, 0},
		{100, -5, 0},
		{1, 1000000, 4},
	} {
		f.Add(s.mantissa, s.units, s.exp)
	}
	f.Fuzz(func(t *testing.T, mantissa, units int64, exp int32) {
		if exp < -10 {
			exp = -10
		}
		if exp > 10 {
			exp = 10
		}
		u := decimal.New(units, exp)
		rc, err := domain.NewRecetaComponente(int(mantissa), u)
		if err != nil {
			return
		}
		if rc.ArticuloID() <= 0 {
			t.Fatalf("accepted articuloID <= 0: %d", rc.ArticuloID())
		}
		if rc.Unidades().Sign() <= 0 {
			t.Fatalf("accepted non-positive unidades: %s", rc.Unidades())
		}
	})
}

// FuzzReceta_Firma verifies that the RecetaComponente accessors never panic
// for arbitrary integer inputs that pass validation.
func FuzzReceta_Firma(f *testing.F) {
	for _, s := range []struct {
		artID int
		units int64
	}{
		{1, 1},
		{999, 9999},
		{1, 0},
	} {
		f.Add(s.artID, s.units)
	}
	f.Fuzz(func(t *testing.T, artID int, units int64) {
		rc, err := domain.NewRecetaComponente(artID, decimal.NewFromInt(units))
		if err != nil {
			return
		}
		_ = rc.ArticuloID()
		_ = rc.Unidades().String()
	})
}
