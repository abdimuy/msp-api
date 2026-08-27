//nolint:misspell,paralleltest // Spanish vocabulary; integration tests share the pool and must not run in parallel with each other.
package ventfb_test

// e2e_lineas_deltas_test.go proves, against a real Firebird, the two claims
// that PUT /v2/ventas/{id}/lineas rests on:
//
//  1. The combined write survives the edit that motivated it — delete a combo
//     and create another with a NEW id, moving the productos across — which
//     the two single-collection writes cannot do in either order because
//     MSP_VENTAS_PRODUCTOS.COMBO_ID has a foreign key onto MSP_VENTAS_COMBOS.
//
//  2. Inventory lands on the FINAL composition and nothing stays reserved.
//     SALDOS_IN existencia is measured before and after, per almacén, and the
//     MSP_VENTAS_TRASPASOS chain is counted.
//
// Gating and isolation follow the rest of the ventfb suite: skip without
// FB_DATABASE, everything inside WithTestTransaction so nothing reaches the
// shared dev DB.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/inventario"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// lineasClock is a Clock returning the current instant, matching the
// production wiring used by the inventario delta tests.
type lineasClock struct{}

func (lineasClock) Now() time.Time { return time.Now().UTC() }

var _ ventasoutbound.Clock = lineasClock{}

// buildVentasServiceConInventario wires a REAL ventas app.Service against the
// Firebird repo plus the real inventario module, exactly as cmd/api does minus
// the pieces these tests do not exercise. txMgr is nil on purpose: the ambient
// WithTestTransaction tx is what everything joins, and Service.runInTx
// short-circuits to fn(ctx) when txMgr is nil.
func buildVentasServiceConInventario(pool *firebird.Pool) *ventasapp.Service {
	repo := ventfb.NewVentaRepo(pool)
	svc := ventasapp.NewService(
		repo, nil, nil,
		noopStorage{},
		lineasClock{},
		&recordingFakeOutbox{},
		imageprocessor.NoOpProcessor{},
		nil, // txMgr: ambient tx
		nil, nil, nil,
	)
	invAdapter := &inventarioAdapterForTest{
		inner:          inventario.NewServiceAdapter(buildInventarioService(pool)),
		almacenDestino: 11058,
	}
	return svc.WithInventario(invAdapter)
}

// buildVentaConCombo builds a borrador CONTADO venta whose only producto is a
// child of a single combo — the shape the desktop edits. Quantities are
// caller-supplied so the inventory delta is exact.
func buildVentaConCombo(
	t *testing.T,
	createdBy uuid.UUID,
	comboID uuid.UUID,
	articuloID, almacenOrigen int,
	cantidadHijo decimal.Decimal,
) *domain.Venta {
	t.Helper()
	const almacenDestino = 11058

	nombre, err := domain.NewNombreCliente("Ramírez Aguilar Beatriz")
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:     "Avenida Reforma",
		Colonia:   "San Pedro",
		Poblacion: "Tehuacán",
		Ciudad:    "Puebla",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(18.4617, -97.3928)
	require.NoError(t, err)
	comboPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("3000.00"),
		decimal.RequireFromString("2700.00"),
		decimal.RequireFromString("2400.00"),
	)
	require.NoError(t, err)
	hijoPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("1000.00"),
		decimal.RequireFromString("900.00"),
		decimal.RequireFromString("800.00"),
	)
	require.NoError(t, err)

	cid := comboID
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre}),
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Now().UTC(),
		TipoVenta:  domain.TipoVentaContado,
		Combos: []domain.CrearVentaComboInput{{
			ID:             comboID,
			Nombre:         "Combo Recámara Original",
			Precios:        comboPrecios,
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  almacenOrigen,
			AlmacenDestino: almacenDestino,
		}},
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     articuloID,
			Articulo:       "Artículo Hijo del Combo",
			Cantidad:       cantidadHijo,
			Precios:        hijoPrecios,
			ComboID:        &cid,
			AlmacenOrigen:  nil, // inherited from the combo
			AlmacenDestino: nil,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: createdBy,
			Email:     "lineas-e2e@muebleriamsp.mx",
			Nombre:    "Vendedor Líneas Prueba",
		}},
		CreatedBy: createdBy,
		Now:       time.Now().UTC(),
	})
	require.NoError(t, err)
	return v
}

// countProductosDeVenta counts MSP_VENTAS_PRODUCTOS rows for a venta.
func countProductosDeVenta(ctx context.Context, t *testing.T, q firebird.Querier, ventaID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_VENTAS_PRODUCTOS WHERE VENTA_ID = ?`, ventaID.String()).Scan(&n))
	return n
}

// combosDeVenta returns the combo ids persisted for a venta.
func combosDeVenta(ctx context.Context, t *testing.T, q firebird.Querier, ventaID uuid.UUID) []string {
	t.Helper()
	rows, err := q.QueryContext(ctx,
		`SELECT ID FROM MSP_VENTAS_COMBOS WHERE VENTA_ID = ?`, ventaID.String())
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var out []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

// comboIDsDeProductos returns the COMBO_ID of every producto row of a venta,
// with NULL rendered as the empty string.
func comboIDsDeProductos(ctx context.Context, t *testing.T, q firebird.Querier, ventaID uuid.UUID) []string {
	t.Helper()
	rows, err := q.QueryContext(ctx,
		`SELECT COALESCE(COMBO_ID, '') FROM MSP_VENTAS_PRODUCTOS WHERE VENTA_ID = ?`, ventaID.String())
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var out []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

// ─── 1. the FK ordering ─────────────────────────────────────────────────────

// TestE2E_Lineas_BorrarComboYCrearOtro_Persiste drives the real
// Service.ReemplazarLineas against Firebird and then reads the rows back: the
// old combo is gone, the new one is there, and the producto row points at the
// new combo. If the statement order inside ReplaceLineas were wrong, the FK
// FK_MSP_VENTAS_PRODUCTOS_COMBO would abort the write.
func TestE2E_Lineas_BorrarComboYCrearOtro_Persiste(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		articuloID, almacenOrigen := discoverTestArticuloAndAlmacen(ctx, t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		// No inventario here: this test is about the rows, and a nil
		// InventarioService is a supported wiring.
		svc := ventasapp.NewService(
			repo, nil, nil, noopStorage{}, lineasClock{}, &recordingFakeOutbox{},
			imageprocessor.NoOpProcessor{}, nil, nil, nil, nil,
		)

		viejoComboID := uuid.New()
		venta := buildVentaConCombo(t, userID, viejoComboID, articuloID, almacenOrigen, decimal.NewFromInt(2))
		require.NoError(t, repo.Save(ctx, venta))

		nuevoComboID := uuid.New()
		require.NotEqual(t, viejoComboID, nuevoComboID)

		out, err := svc.ReemplazarLineas(ctx, ventasapp.ReemplazarLineasInput{
			VentaID: venta.ID(),
			Combos: []ventasapp.CrearVentaComboInput{{
				ID:             nuevoComboID,
				Nombre:         "Combo Recámara Nuevo",
				PrecioAnual:    decimal.RequireFromString("3500.00"),
				PrecioCorto:    decimal.RequireFromString("3200.00"),
				PrecioContado:  decimal.RequireFromString("2900.00"),
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  almacenOrigen,
				AlmacenDestino: 11058,
			}},
			Productos: []ventasapp.CrearVentaProductoInput{{
				ID:            uuid.New(),
				ArticuloID:    articuloID,
				Articulo:      "Artículo Hijo Reasignado",
				Cantidad:      decimal.NewFromInt(3),
				PrecioAnual:   decimal.RequireFromString("1200.00"),
				PrecioCorto:   decimal.RequireFromString("1100.00"),
				PrecioContado: decimal.RequireFromString("1000.00"),
				ComboID:       &nuevoComboID,
			}},
		}, userID)
		require.NoError(t, err, "the combined write must survive the productos→combos FK")
		require.Equal(t, 1, out.CombosCount())
		require.Equal(t, 1, out.ProductosCount())

		// Read the rows back from Firebird, not from the aggregate.
		combos := combosDeVenta(ctx, t, q, venta.ID())
		require.Len(t, combos, 1)
		assert.Equal(t, nuevoComboID.String(), combos[0], "the old combo row must be gone")

		require.Equal(t, 1, countProductosDeVenta(ctx, t, q, venta.ID()))
		comboRefs := comboIDsDeProductos(ctx, t, q, venta.ID())
		require.Len(t, comboRefs, 1)
		assert.Equal(t, nuevoComboID.String(), comboRefs[0],
			"the persisted producto must reference the NEW combo")

		// And the aggregate re-read through the repo agrees.
		reread, err := repo.FindByID(ctx, venta.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, reread.CombosCount())
		assert.Equal(t, 1, reread.ProductosCount())
	})
}

// TestE2E_Lineas_ReplaceCombosSoloRompeElFK is the control that proves the
// statement ordering inside ReplaceLineas is load-bearing rather than
// decorative. It takes an aggregate whose combos AND productos were both
// swapped coherently, then asks the repository to write only the combos half —
// which is exactly what the deprecated PUT /combos does. Firebird rejects it:
// the productos rows still on disk reference the combo rows being deleted.
//
// This is the database-level reason no ordering of the two old endpoints can
// work, independent of the domain guard that returns 422 first.
func TestE2E_Lineas_ReplaceCombosSoloRompeElFK(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		articuloID, almacenOrigen := discoverTestArticuloAndAlmacen(ctx, t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		viejoComboID := uuid.New()
		venta := buildVentaConCombo(t, userID, viejoComboID, articuloID, almacenOrigen, decimal.NewFromInt(2))
		require.NoError(t, repo.Save(ctx, venta))

		// Swap the combo coherently in memory: new combo id, productos moved
		// onto it. The aggregate is valid — the DATABASE is what still holds
		// productos rows pointing at the old combo.
		nuevoComboID := uuid.New()
		comboPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("3500.00"),
			decimal.RequireFromString("3200.00"),
			decimal.RequireFromString("2900.00"),
		)
		require.NoError(t, err)
		hijoPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("1200.00"),
			decimal.RequireFromString("1100.00"),
			decimal.RequireFromString("1000.00"),
		)
		require.NoError(t, err)
		require.NoError(t, venta.ReemplazarLineas(domain.ReemplazarLineasParams{
			Combos: []domain.CrearVentaComboInput{{
				ID: nuevoComboID, Nombre: "Combo Nuevo", Precios: comboPrecios,
				Cantidad: decimal.NewFromInt(1), AlmacenOrigen: almacenOrigen, AlmacenDestino: 11058,
			}},
			Productos: []domain.CrearVentaProductoInput{{
				ID: uuid.New(), ArticuloID: articuloID, Articulo: "Hijo Reasignado",
				Cantidad: decimal.NewFromInt(3), Precios: hijoPrecios, ComboID: &nuevoComboID,
			}},
			By: userID, Now: time.Now().UTC(),
		}))

		// Write only the combos half — what PUT /combos issues.
		err = repo.ReplaceCombos(ctx, venta)
		require.Error(t, err, "deleting the combos rows under live productos rows must violate the FK")
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected typed apperror, got %T: %v", err, err)
		assert.Equal(t, "firebird_fk_violation", appErr.Code,
			"got code=%s message=%s", appErr.Code, appErr.Message)
	})
}

// ─── 2. inventory deltas ────────────────────────────────────────────────────

// TestE2E_Lineas_DeltasDeInventario_UnaSolaAplicacion is the money test. It
// measures SALDOS_IN existencia in both almacenes before and after the
// combined edit and asserts the net movement equals the FINAL quantity — not
// the old one, not the sum of both.
//
// What it catches, verified by breaking the code on purpose: replacing the
// resync with a plain CrearTraspasoParaVenta (reserve the new quantity without
// releasing the old) turns Δorigen from −5 into −7 and leaves TWO active
// directos. Both assertions fire.
//
// What it does NOT catch, also measured: calling the resync TWICE in the same
// transaction is absorbed by the inventario module's identical-detalles
// fast-path — the second call finds the same net effect and no-ops, so no
// existencia moves and no second DOCTOS_IN appears. The "exactly once"
// guarantee is therefore pinned where it is observable, in
// TestReemplazarLineas_ResincronizaUnaSolaVez (internal/ventas/app), which
// counts the calls directly. Do not read a green run here as proof that the
// call count is right.
func TestE2E_Lineas_DeltasDeInventario_UnaSolaAplicacion(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		articuloID, almacenOrigen := discoverTestArticuloAndAlmacen(ctx, t, q)
		const almacenDestino = 11058
		userID := seedUsuarioRow(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		svc := buildVentasServiceConInventario(pool)
		invAdapter := &inventarioAdapterForTest{
			inner:          inventario.NewServiceAdapter(buildInventarioService(pool)),
			almacenDestino: almacenDestino,
		}

		// ── Baseline, before any reservation ─────────────────────────────────
		baseOrigen := readExistenciaInTx(ctx, t, pool, articuloID, almacenOrigen)
		baseDestino := readExistenciaInTx(ctx, t, pool, articuloID, almacenDestino)
		t.Logf("baseline: origen=%s destino=%s",
			baseOrigen.StringFixed(5), baseDestino.StringFixed(5))

		// ── Create the venta and its initial reservation (qty 2) ─────────────
		qtyInicial := decimal.NewFromInt(2)
		viejoComboID := uuid.New()
		venta := buildVentaConCombo(t, userID, viejoComboID, articuloID, almacenOrigen, qtyInicial)
		require.NoError(t, repo.Save(ctx, venta))
		_, err := invAdapter.CrearTraspasoParaVenta(ctx, ventasoutbound.InventarioCrearTraspasoParams{
			VentaID:       venta.ID(),
			AlmacenOrigen: almacenOrigen,
			Fecha:         time.Now().UTC(),
			Descripcion:   "lineas e2e: reserva inicial",
			Detalles: []ventasoutbound.InventarioTraspasoDetalle{
				{ArticuloID: articuloID, Cantidad: qtyInicial},
			},
			CreatedBy: userID,
		})
		require.NoError(t, err)

		trasCrear := readExistenciaInTx(ctx, t, pool, articuloID, almacenOrigen)
		require.True(t, trasCrear.Sub(baseOrigen).Equal(qtyInicial.Neg()),
			"reserva inicial: origen debe bajar 2; Δ=%s", trasCrear.Sub(baseOrigen).StringFixed(5))
		require.Equal(t, 1, countTraspasoRows(ctx, t, q, venta.ID(), "directo", "N"))

		// ── The edit: delete the combo, create another, qty 2 → 5 ────────────
		qtyFinal := decimal.NewFromInt(5)
		nuevoComboID := uuid.New()
		_, err = svc.ReemplazarLineas(ctx, ventasapp.ReemplazarLineasInput{
			VentaID: venta.ID(),
			Combos: []ventasapp.CrearVentaComboInput{{
				ID:             nuevoComboID,
				Nombre:         "Combo Recámara Nuevo",
				PrecioAnual:    decimal.RequireFromString("3500.00"),
				PrecioCorto:    decimal.RequireFromString("3200.00"),
				PrecioContado:  decimal.RequireFromString("2900.00"),
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  almacenOrigen,
				AlmacenDestino: almacenDestino,
			}},
			Productos: []ventasapp.CrearVentaProductoInput{{
				ID:            uuid.New(),
				ArticuloID:    articuloID,
				Articulo:      "Artículo Hijo Reasignado",
				Cantidad:      qtyFinal,
				PrecioAnual:   decimal.RequireFromString("1200.00"),
				PrecioCorto:   decimal.RequireFromString("1100.00"),
				PrecioContado: decimal.RequireFromString("1000.00"),
				ComboID:       &nuevoComboID,
			}},
		}, userID)
		require.NoError(t, err)

		// ── Deltas measured after ────────────────────────────────────────────
		finOrigen := readExistenciaInTx(ctx, t, pool, articuloID, almacenOrigen)
		finDestino := readExistenciaInTx(ctx, t, pool, articuloID, almacenDestino)
		deltaOrigen := finOrigen.Sub(baseOrigen)
		deltaDestino := finDestino.Sub(baseDestino)
		t.Logf("tras la edición combinada: Δorigen=%s Δdestino=%s (esperado ∓%s)",
			deltaOrigen.StringFixed(5), deltaDestino.StringFixed(5), qtyFinal.StringFixed(5))

		assert.True(t, deltaOrigen.Equal(qtyFinal.Neg()),
			"origen debe reflejar SÓLO la cantidad final (−5) contra la línea base; Δ=%s. "+
				"Un −7 significa que la reserva vieja no se liberó",
			deltaOrigen.StringFixed(5))
		assert.True(t, deltaDestino.Equal(qtyFinal),
			"destino debe reflejar SÓLO la cantidad final (+5); Δ=%s", deltaDestino.StringFixed(5))

		// ── The traspaso chain: one reverse + one new directo, no more ───────
		assert.Equal(t, 1, countTraspasoRows(ctx, t, q, venta.ID(), "directo", "N"),
			"exactamente un directo activo tras la edición — dos significan que la reserva vieja quedó viva")
		assert.Equal(t, 1, countTraspasoRows(ctx, t, q, venta.ID(), "directo", "S"),
			"el directo anterior debe quedar reversado")
		assert.Equal(t, 1, countTraspasoRows(ctx, t, q, venta.ID(), "reverso", ""),
			"un solo reverso: la resincronización revierte el directo anterior y crea el nuevo")

		// ── And the rows landed on the new combo ─────────────────────────────
		combos := combosDeVenta(ctx, t, q, venta.ID())
		require.Len(t, combos, 1)
		assert.Equal(t, nuevoComboID.String(), combos[0])
	})
}

// TestE2E_Lineas_EdicionIdentica_NoMueveExistencias verifies the no-op guard
// still holds through the combined endpoint: re-sending the SAME composition
// leaves SALDOS_IN untouched and creates no second directo. Without it, every
// save of an unchanged venta would churn DOCTOS_IN.
func TestE2E_Lineas_EdicionIdentica_NoMueveExistencias(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		articuloID, almacenOrigen := discoverTestArticuloAndAlmacen(ctx, t, q)
		const almacenDestino = 11058
		userID := seedUsuarioRow(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		svc := buildVentasServiceConInventario(pool)
		invAdapter := &inventarioAdapterForTest{
			inner:          inventario.NewServiceAdapter(buildInventarioService(pool)),
			almacenDestino: almacenDestino,
		}

		qty := decimal.NewFromInt(2)
		comboID := uuid.New()
		venta := buildVentaConCombo(t, userID, comboID, articuloID, almacenOrigen, qty)
		require.NoError(t, repo.Save(ctx, venta))
		_, err := invAdapter.CrearTraspasoParaVenta(ctx, ventasoutbound.InventarioCrearTraspasoParams{
			VentaID:       venta.ID(),
			AlmacenOrigen: almacenOrigen,
			Fecha:         time.Now().UTC(),
			Descripcion:   "lineas e2e: reserva inicial (no-op)",
			Detalles: []ventasoutbound.InventarioTraspasoDetalle{
				{ArticuloID: articuloID, Cantidad: qty},
			},
			CreatedBy: userID,
		})
		require.NoError(t, err)

		antesOrigen := readExistenciaInTx(ctx, t, pool, articuloID, almacenOrigen)
		antesDestino := readExistenciaInTx(ctx, t, pool, articuloID, almacenDestino)
		doctosAntes := countDoctoInRows(ctx, t, q, venta.ID())

		// Same articulo, same quantity, same almacenes — only the row ids move.
		nuevoComboID := uuid.New()
		_, err = svc.ReemplazarLineas(ctx, ventasapp.ReemplazarLineasInput{
			VentaID: venta.ID(),
			Combos: []ventasapp.CrearVentaComboInput{{
				ID:             nuevoComboID,
				Nombre:         "Combo Recámara Renombrado",
				PrecioAnual:    decimal.RequireFromString("3000.00"),
				PrecioCorto:    decimal.RequireFromString("2700.00"),
				PrecioContado:  decimal.RequireFromString("2400.00"),
				Cantidad:       decimal.NewFromInt(1),
				AlmacenOrigen:  almacenOrigen,
				AlmacenDestino: almacenDestino,
			}},
			Productos: []ventasapp.CrearVentaProductoInput{{
				ID:            uuid.New(),
				ArticuloID:    articuloID,
				Articulo:      "Artículo Hijo del Combo",
				Cantidad:      qty,
				PrecioAnual:   decimal.RequireFromString("1000.00"),
				PrecioCorto:   decimal.RequireFromString("900.00"),
				PrecioContado: decimal.RequireFromString("800.00"),
				ComboID:       &nuevoComboID,
			}},
		}, userID)
		require.NoError(t, err)

		despuesOrigen := readExistenciaInTx(ctx, t, pool, articuloID, almacenOrigen)
		despuesDestino := readExistenciaInTx(ctx, t, pool, articuloID, almacenDestino)
		assert.True(t, despuesOrigen.Equal(antesOrigen),
			"una edición sin cambio neto no debe mover existencias; antes=%s después=%s",
			antesOrigen.StringFixed(5), despuesOrigen.StringFixed(5))
		assert.True(t, despuesDestino.Equal(antesDestino),
			"antes=%s después=%s", antesDestino.StringFixed(5), despuesDestino.StringFixed(5))
		assert.Equal(t, doctosAntes, countDoctoInRows(ctx, t, q, venta.ID()),
			"una edición sin cambio neto no debe crear otro DOCTOS_IN")
		assert.Equal(t, 1, countTraspasoRows(ctx, t, q, venta.ID(), "directo", "N"),
			"sigue habiendo un solo directo activo")

		// The combo swap itself DID persist, even though inventory did not move.
		combos := combosDeVenta(ctx, t, q, venta.ID())
		require.Len(t, combos, 1)
		assert.Equal(t, nuevoComboID.String(), combos[0])
	})
}

// ─── 3. atomicity: a failed edit leaves nothing behind ───────────────────────

// invSiempreFalla is an InventarioService whose resync always fails, so
// ReemplazarLineas can be driven down the "the transaction must roll back"
// path with a real Firebird transaction underneath.
type invSiempreFalla struct{ err error }

func (i *invSiempreFalla) ValidarStockParaVenta(_ context.Context, _ []ventasoutbound.InventarioStockItem) error {
	return nil
}

func (i *invSiempreFalla) CrearTraspasoParaVenta(_ context.Context, _ ventasoutbound.InventarioCrearTraspasoParams) (int, error) {
	return 0, nil
}

func (i *invSiempreFalla) CrearTraspasoReverso(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (i *invSiempreFalla) ResincronizarTraspasoParaVenta(_ context.Context, _ ventasoutbound.InventarioCrearTraspasoParams) (int, error) {
	return 0, i.err
}

var _ ventasoutbound.InventarioService = (*invSiempreFalla)(nil)

// borrarVentaCompleta removes a venta and its children, children first so the
// foreign keys never block the delete. Errors are returned, never discarded —
// swallowing them would leave rows in the shared dev DB while the cleanup
// looked like it worked.
func borrarVentaCompleta(db *sql.DB, ventaID uuid.UUID) error {
	ctx := context.Background()
	for _, stmt := range []string{
		"DELETE FROM MSP_VENTAS_IMAGENES WHERE VENTA_ID = ?",
		"DELETE FROM MSP_VENTAS_PRODUCTOS WHERE VENTA_ID = ?",
		"DELETE FROM MSP_VENTAS_COMBOS WHERE VENTA_ID = ?",
		"DELETE FROM MSP_VENTAS_VENDEDORES WHERE VENTA_ID = ?",
		"DELETE FROM MSP_VENTAS WHERE ID = ?",
	} {
		//nolint:gosec // statements are package-private string constants.
		if _, err := db.ExecContext(ctx, stmt, ventaID.String()); err != nil {
			return err
		}
	}
	return nil
}

// TestE2E_Lineas_TransaccionFallida_NoEscribeNada proves the atomicity claim
// with a REAL Firebird transaction: when the inventory resync fails after the
// lines were written, the rollback takes the line rows with it. The venta ends
// the test with exactly the combo and producto it started with — no half-edit.
//
// This test lives OUTSIDE fbtestutil.WithTestTransaction on purpose: Firebird
// does not nest transactions, so a real BEGIN/ROLLBACK cannot be observed from
// inside an outer test tx (Service.runInTx would simply reuse the ambient one
// and never roll back). It therefore commits one usuario + one venta and
// deletes them on the way out, with a t.Cleanup safety net and a before/after
// row-count snapshot proving the database is intact. Same pattern as
// TestVentaRepo_Save_AtomicViaTxManager in atomicity_test.go.
//
//nolint:paralleltest,funlen // serialized DB writes outside WithTestTransaction; long but linear.
func TestE2E_Lineas_TransaccionFallida_NoEscribeNada(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	txMgr := firebird.NewTxManager(pool.DB)
	repo := ventfb.NewVentaRepo(pool)
	ctx := context.Background()

	scrubAtomicityResidue(t, pool.DB)
	before := snapshotCounts(t, pool.DB)

	// Seed the FK target and a venta with one combo + its child producto.
	root := seedAtomicityUsuario(t, pool.DB)
	t.Cleanup(func() { _ = deleteAtomicityUsuario(pool.DB, root) })

	viejoComboID := uuid.New()
	venta := buildVentaConCombo(t, root, viejoComboID, 1, 19, decimal.NewFromInt(2))
	t.Cleanup(func() { _ = borrarVentaCompleta(pool.DB, venta.ID()) })
	require.NoError(t, txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return repo.Save(ctx, venta)
	}), "seed venta con combo")

	viejoProductoID := ""
	for p := range venta.Productos() {
		viejoProductoID = p.ID().String()
	}
	require.NotEmpty(t, viejoProductoID)

	// A service with a REAL TxManager (not the ambient-tx nil) and an
	// inventario that always rejects the resync.
	boom := apperror.NewValidation("articulo_sin_existencia", "no hay existencia suficiente")
	svc := ventasapp.NewService(
		repo, nil, nil, noopStorage{}, lineasClock{}, &recordingFakeOutbox{},
		imageprocessor.NoOpProcessor{}, txMgr, nil, nil, nil,
	).WithInventario(&invSiempreFalla{err: boom})

	nuevoComboID := uuid.New()
	_, err := svc.ReemplazarLineas(ctx, ventasapp.ReemplazarLineasInput{
		VentaID: venta.ID(),
		Combos: []ventasapp.CrearVentaComboInput{{
			ID: nuevoComboID, Nombre: "Combo Que No Debe Quedar",
			PrecioAnual:   decimal.RequireFromString("3500.00"),
			PrecioCorto:   decimal.RequireFromString("3200.00"),
			PrecioContado: decimal.RequireFromString("2900.00"),
			Cantidad:      decimal.NewFromInt(1), AlmacenOrigen: 19, AlmacenDestino: 11058,
		}},
		Productos: []ventasapp.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 1, Articulo: "Producto Que No Debe Quedar",
			Cantidad:      decimal.NewFromInt(9),
			PrecioAnual:   decimal.RequireFromString("1200.00"),
			PrecioCorto:   decimal.RequireFromString("1100.00"),
			PrecioContado: decimal.RequireFromString("1000.00"),
			ComboID:       &nuevoComboID,
		}},
	}, root)
	require.Error(t, err, "the failing resync must abort the edit")

	// The rows must be EXACTLY what they were before the failed edit. Read in
	// a fresh transaction so we see the post-rollback state.
	require.NoError(t, txMgr.RunInTx(ctx, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)

		combos := combosDeVenta(ctx, t, q, venta.ID())
		require.Len(t, combos, 1, "the venta must still have exactly one combo")
		assert.Equal(t, viejoComboID.String(), combos[0],
			"the ORIGINAL combo must survive a rolled-back edit")

		assert.Equal(t, 1, countProductosDeVenta(ctx, t, q, venta.ID()))
		refs := comboIDsDeProductos(ctx, t, q, venta.ID())
		require.Len(t, refs, 1)
		assert.Equal(t, viejoComboID.String(), refs[0],
			"the producto must still point at the ORIGINAL combo")

		// And no row of the attempted edit leaked through.
		var n int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_VENTAS_COMBOS WHERE ID = ?`, nuevoComboID.String()).Scan(&n))
		assert.Equal(t, 0, n, "the combo from the failed edit must not exist")
		return nil
	}))

	// Explicit cleanup before the final snapshot; the t.Cleanup nets are
	// idempotent no-ops afterwards.
	require.NoError(t, borrarVentaCompleta(pool.DB, venta.ID()), "cleanup venta de prueba")
	require.NoError(t, deleteAtomicityUsuario(pool.DB, root), "cleanup usuario de prueba")

	after := snapshotCounts(t, pool.DB)
	for _, tbl := range ventasTables {
		assert.Equal(t, before[tbl], after[tbl],
			"row count must be identical after the test on table %s (before=%d, after=%d)",
			tbl, before[tbl], after[tbl])
	}
}
