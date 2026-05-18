//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// ─── A3: Money columns are NUMERIC(14,2) ───────────────────────────────────
//
// MSP_VENTAS.MONTO_* and MSP_VENTAS_*.PRECIO_* are NUMERIC(14,2). The domain
// rejects values with scale > 2 and values exceeding MaxMontoVenta. These
// tests pin that contract: values within the contract round-trip exactly,
// and out-of-contract values are rejected at construction time (before the
// driver gets a chance to silently round or store garbage).

func TestVentaRepo_DecimalPrecision_Money_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"zero", "0.00", "0.00"},
		{"two decimals exact", "100.50", "100.50"},
		{"one decimal", "100.5", "100.50"},
		{"max integer width (12 digits)", "999999999999.99", "999999999999.99"},
		{"large half", "100.99", "100.99"},
	}
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				root := seedUsuarioRow(ctx, t, pool)
				v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})

				val := decimal.RequireFromString(tc.input)
				montos, err := domain.NewMontoSnapshot(val, val, val)
				require.NoError(t, err)

				a := v.Audit()
				vv := domain.HydrateVenta(domain.HydrateVentaParams{
					ID: v.ID(), ClienteID: v.ClienteID(), Cliente: v.Cliente(),
					Direccion: v.Direccion(), GPS: v.GPS(), FechaVenta: v.FechaVenta(),
					TipoVenta: v.TipoVenta(), Montos: montos,
					PlanCredito: v.PlanCredito(), DiaCobranza: v.DiaCobranza(), Nota: v.Nota(),
					Status:     v.Status(),
					Combos:     v.CombosForRepo(),
					Productos:  v.ProductosForRepo(),
					Vendedores: v.VendedoresForRepo(),
					Imagenes:   v.ImagenesForRepo(),
					CreatedAt:  a.CreatedAt(), UpdatedAt: a.UpdatedAt(),
					CreatedBy: a.CreatedBy(), UpdatedBy: a.UpdatedBy(),
				})

				require.NoError(t, repo.Save(ctx, vv))
				got, err := repo.FindByID(ctx, vv.ID())
				require.NoError(t, err)

				want := decimal.RequireFromString(tc.want)
				assert.True(t, got.Montos().Anual().Equal(want),
					"want %s, got %s (input %s)", want, got.Montos().Anual(), tc.input)
			})
		})
	}
}

// TestNewMontoSnapshot_RejectsExtraDecimals pins the domain contract: any
// monetary value with > 2 decimal places is rejected at construction time,
// before the driver gets to silently round (which it would: 100.999 → 101).
func TestNewMontoSnapshot_RejectsExtraDecimals(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"100.999", "100.005", "0.001", "1.123456"} {
		val := decimal.RequireFromString(s)
		_, err := domain.NewMontoSnapshot(val, decimal.Zero, decimal.Zero)
		require.Error(t, err, "%s must be rejected", s)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "monto_demasiados_decimales", ae.Code, "input=%s", s)
	}
}

// TestNewMontoSnapshot_RejectsBeyondCap pins the domain contract: monetary
// values exceeding MaxMontoVenta are rejected at construction time. Firebird's
// NUMERIC(14,2) is INT64-backed and would otherwise accept ~9.22 × 10^16 —
// the explicit cap stops magnitude bugs from the FE from being persisted.
func TestNewMontoSnapshot_RejectsBeyondCap(t *testing.T) {
	t.Parallel()
	// 13 integer digits — past MaxMontoVenta (999_999_999_999.99).
	overflow := decimal.RequireFromString("1000000000000.00")
	_, err := domain.NewMontoSnapshot(overflow, decimal.Zero, decimal.Zero)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiado_grande", ae.Code)

	// At the boundary: exactly MaxMontoVenta must succeed.
	atCap := decimal.RequireFromString("999999999999.99")
	_, err = domain.NewMontoSnapshot(atCap, atCap, atCap)
	require.NoError(t, err, "value exactly at MaxMontoVenta must be accepted")
}

// ─── A4: Cantidad column is NUMERIC(10,4) ─────────────────────────────────

func TestVentaRepo_DecimalPrecision_Cantidad_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"one unit", "1.0000", "1.0000"},
		{"min positive", "0.0001", "0.0001"},
		{"max integer width", "999999.9999", "999999.9999"},
	}

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				root := seedUsuarioRow(ctx, t, pool)

				nombre, _ := domain.NewNombreCliente("Cliente Decimal")
				dir, _ := domain.NewDireccion(domain.NewDireccionParams{
					Calle: "Av. X", Colonia: "C", Poblacion: "P", Ciudad: "CDMX",
				})
				gps, _ := domain.NewGPSCoords(19.0, -99.0)
				montos, _ := domain.NewMontoSnapshot(
					decimal.RequireFromString("100.00"),
					decimal.RequireFromString("80.00"),
					decimal.RequireFromString("50.00"),
				)
				cant := decimal.RequireFromString(tc.input)
				one, two := 1, 2
				params := domain.CrearVentaParams{
					ID: uuid.New(),
					Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
						Nombre: nombre,
					}),
					Direccion: dir, GPS: gps, FechaVenta: testNow(),
					TipoVenta: domain.TipoVentaContado, Montos: montos,
					Productos: []domain.CrearVentaProductoInput{{
						ID: uuid.New(), ArticuloID: 1, Articulo: "P",
						Cantidad: cant, Precios: montos,
						AlmacenOrigen:  &one,
						AlmacenDestino: &two,
					}},
					Vendedores: []domain.CrearVentaVendedorInput{{
						ID: uuid.New(), UsuarioID: root,
						Email: "v-" + uuid.NewString() + "@example.invalid", Nombre: "V",
					}},
					CreatedBy: root, Now: testNow(),
				}
				v, err := domain.CrearVenta(params)
				require.NoError(t, err)

				require.NoError(t, repo.Save(ctx, v))
				got, err := repo.FindByID(ctx, v.ID())
				require.NoError(t, err)
				require.Equal(t, 1, got.ProductosCount())

				var gotCant decimal.Decimal
				for p := range got.Productos() {
					gotCant = p.Cantidad()
				}
				want := decimal.RequireFromString(tc.want)
				assert.True(t, gotCant.Equal(want),
					"want %s, got %s (input %s)", want, gotCant, tc.input)
			})
		})
	}
}

// TestVentaRepo_DecimalPrecision_Money_AtMaxValue_Firebird inserts a venta
// at exactly MaxMontoVenta (999999999999.99) and asserts byte-equal
// round-trip — pins the upper boundary of the NUMERIC(14,2) money columns
// against silent driver-side rounding.
func TestVentaRepo_DecimalPrecision_Money_AtMaxValue_Firebird(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})

		maxMonto := domain.MaxMontoVenta // 999999999999.99
		montos, err := domain.NewMontoSnapshot(maxMonto, maxMonto, maxMonto)
		require.NoError(t, err)
		a := v.Audit()
		vv := domain.HydrateVenta(domain.HydrateVentaParams{
			ID: v.ID(), ClienteID: v.ClienteID(), Cliente: v.Cliente(),
			Direccion: v.Direccion(), GPS: v.GPS(), FechaVenta: v.FechaVenta(),
			TipoVenta: v.TipoVenta(), Montos: montos,
			PlanCredito: v.PlanCredito(), DiaCobranza: v.DiaCobranza(), Nota: v.Nota(),
			Status:     v.Status(),
			Combos:     v.CombosForRepo(),
			Productos:  v.ProductosForRepo(),
			Vendedores: v.VendedoresForRepo(),
			Imagenes:   v.ImagenesForRepo(),
			CreatedAt:  a.CreatedAt(), UpdatedAt: a.UpdatedAt(),
			CreatedBy: a.CreatedBy(), UpdatedBy: a.UpdatedBy(),
		})
		require.NoError(t, repo.Save(ctx, vv))
		got, err := repo.FindByID(ctx, vv.ID())
		require.NoError(t, err)

		assert.True(t, got.Montos().Anual().Equal(maxMonto),
			"expected %s, got %s", maxMonto, got.Montos().Anual())
		assert.True(t, got.Montos().CortoPlazo().Equal(maxMonto))
		assert.True(t, got.Montos().Contado().Equal(maxMonto))
	})
}

// TestParseVentaMontos_ScaleOverflow_DomainRejects documents the contract:
// values with scale > 2 are rejected at domain construction time, before the
// driver gets a chance to silently round (which would corrupt data —
// 0.005 → 0.01 vs 0.005 → 0.00 depending on rounding mode). The rejection
// happens in NewMontoSnapshot, so the driver never sees an out-of-scale
// monto. This pins the boundary explicitly so a future "let the DB decide"
// refactor cannot regress silently.
func TestParseVentaMontos_ScaleOverflow_DomainRejects(t *testing.T) {
	t.Parallel()
	overflow := decimal.RequireFromString("0.005") // 3 decimal places
	_, err := domain.NewMontoSnapshot(overflow, decimal.Zero, decimal.Zero)
	require.Error(t, err, "scale > 2 must be rejected at construction; the driver must never see it")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiados_decimales", ae.Code)
}

// TestCantidad_RejectsExtraDecimals pins the domain contract: cantidad
// values with > 4 decimal places are rejected before reaching the driver.
func TestCantidad_RejectsExtraDecimals(t *testing.T) {
	t.Parallel()
	nombre, _ := domain.NewNombreCliente("Cliente")
	dir, _ := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "X", Colonia: "C", Poblacion: "P", Ciudad: "CDMX",
	})
	gps, _ := domain.NewGPSCoords(0, 0)
	montos, _ := domain.NewMontoSnapshot(
		decimal.RequireFromString("100.00"),
		decimal.RequireFromString("80.00"),
		decimal.RequireFromString("50.00"),
	)
	one, two := 1, 2
	params := domain.CrearVentaParams{
		ID: uuid.New(),
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion: dir, GPS: gps, FechaVenta: testNow(),
		TipoVenta: domain.TipoVentaContado, Montos: montos,
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 1, Articulo: "P",
			Cantidad:       decimal.RequireFromString("1.99999"), // 5 decimals
			Precios:        montos,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(), Email: "v@x.com", Nombre: "V",
		}},
		CreatedBy: uuid.New(), Now: testNow(),
	}
	_, err := domain.CrearVenta(params)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cantidad_demasiados_decimales", ae.Code)
}
