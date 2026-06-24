//nolint:misspell // Spanish vocabulary (articulo, juego, componente, etc.) by convention.
package microsip_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// lineaArticuloIDForTests is a real LINEA_ARTICULO_ID verified to exist in the
// dev Microsip DB (line 11774 observed in docs/microsip-crear-kit-paso-a-paso.md).
const lineaArticuloIDForTests = 11774

// ─── Seeding helpers ──────────────────────────────────────────────────────────

// componenteRow holds minimal info about a real almacenable article.
type componenteRow struct {
	articuloID int
	claveID    int
}

// requireRealComponentes fetches N distinct almacenable, active, non-juego
// ARTICULOS rows that each have at least one CLAVE_ARTICULO. These are the
// real components we can reference in JUEGOS_DET FKs.
//
// Skips if fewer than n rows are available.
func requireRealComponentes(ctx context.Context, t *testing.T, q firebird.Querier, n int) []componenteRow {
	t.Helper()
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
SELECT FIRST %d a.ARTICULO_ID, ca.CLAVE_ARTICULO_ID
FROM ARTICULOS a
JOIN CLAVES_ARTICULOS ca ON ca.ARTICULO_ID = a.ARTICULO_ID
WHERE a.ES_JUEGO = 'N' AND a.ESTATUS = 'A' AND a.ES_ALMACENABLE = 'S'
ORDER BY a.ARTICULO_ID`, n))
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck // best-effort close in test helper

	var result []componenteRow
	for rows.Next() {
		var c componenteRow
		require.NoError(t, rows.Scan(&c.articuloID, &c.claveID))
		result = append(result, c)
	}
	require.NoError(t, rows.Err())

	if len(result) < n {
		t.Skipf("need %d almacenable articulos with claves, only found %d", n, len(result))
	}
	return result
}

// buildReceta constructs a domain.Receta from (articuloID, qty) pairs by
// building a minimal venta with one combo containing those productos, then
// calling RecetaDeCombo. This exercises the canonical buildReceta code path.
func buildReceta(t *testing.T, comps []componenteRow, qtys []string) domain.Receta {
	t.Helper()
	require.Len(t, qtys, len(comps), "comps and qtys must match")

	comboID := uuid.New()

	montos, err := domain.NewMontoSnapshot(
		decimal.NewFromInt(1000),
		decimal.NewFromInt(800),
		decimal.NewFromInt(500),
	)
	require.NoError(t, err)

	nom, err := domain.NewNombreCliente("TEST CLIENTE")
	require.NoError(t, err)
	cliente, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(20.0, -100.0)
	require.NoError(t, err)

	var productosIn []domain.CrearVentaProductoInput
	for i, c := range comps {
		comboRef := comboID
		productosIn = append(productosIn, domain.CrearVentaProductoInput{
			ID:         uuid.New(),
			ArticuloID: c.articuloID,
			Articulo:   fmt.Sprintf("ART%d", c.articuloID),
			Cantidad:   decimal.RequireFromString(qtys[i]),
			Precios:    montos,
			ComboID:    &comboRef,
		})
	}

	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Combos: []domain.CrearVentaComboInput{{
			ID:             comboID,
			Nombre:         "Kit Test",
			Precios:        montos,
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		}},
		Productos: productosIn,
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

	receta, err := v.RecetaDeCombo(comboID)
	require.NoError(t, err)
	return receta
}

// seedJuego inserts a juego into ARTICULOS + JUEGOS_DET + LIBRES_ARTICULOS
// within the ambient transaction for test fixture setup. Returns the ARTICULO_ID.
func seedJuego(ctx context.Context, t *testing.T, q firebird.Querier, comps []componenteRow, qtys []string, nombre string) int {
	t.Helper()
	require.Len(t, qtys, len(comps))

	var articuloID int
	require.NoError(t, q.QueryRowContext(ctx, `SELECT GEN_ID(ID_CATALOGOS, 1) FROM RDB$DATABASE`).Scan(&articuloID))

	_, err := q.ExecContext(ctx, `INSERT INTO ARTICULOS
  (ARTICULO_ID, NOMBRE, ES_JUEGO, ES_ALMACENABLE, ESTATUS, LINEA_ARTICULO_ID,
   APLICAR_FACTOR_VENTA, FACTOR_VENTA, RED_PRECIO_CON_IMPTO,
   IMPRIMIR_COMP, CONTENIDO_UNIDAD_COMPRA, PESO_UNITARIO,
   ES_PESO_VARIABLE, SEGUIMIENTO, DIAS_GARANTIA,
   ES_IMPORTADO, ES_SIEMPRE_IMPORTADO, PCTJE_ARANCEL,
   IMPRIMIR_NOTAS_COMPRAS, IMPRIMIR_NOTAS_VENTAS,
   FACTOR_RED_PRECIO_CON_IMPTO)
VALUES (?, ?, 'S', 'N', 'A', ?, 'N', 0, 'N', 'N', 1, 0, 'N', 'N', 0, 'N', 'S', 0, 'N', 'N', 0.01)`,
		articuloID, firebird.Win1252(nombre), lineaArticuloIDForTests)
	require.NoError(t, err)

	for i, c := range comps {
		_, err := q.ExecContext(ctx, `INSERT INTO JUEGOS_DET
  (ARTICULO_ID, COMPONENTE_ID, CLAVE_ARTICULO_ID, UNIDADES, ES_REEMPLAZABLE, PERMITIR_MODIF_UNID)
VALUES (?, ?, ?, ?, 'N', 'N')`,
			articuloID, c.articuloID, c.claveID,
			decimal.RequireFromString(qtys[i]).StringFixed(6))
		require.NoError(t, err)
	}

	_, err = q.ExecContext(ctx, `INSERT INTO LIBRES_ARTICULOS (ARTICULO_ID, MULTIPLOVENTA, VOLUMEN) VALUES (?, 0, 0)`, articuloID)
	require.NoError(t, err)

	return articuloID
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestJuegoResolver_ExactMatch(t *testing.T) { //nolint:paralleltest // serial: shared Firebird tx
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 2)

		// Seed a juego with components[0]=1 unit, components[1]=2 units.
		seededID := seedJuego(ctx, t, q, comps, []string{"1", "2"}, "MSP TEST JUEGO EXACT 202406")

		// Build the exact same recipe and resolve.
		receta := buildReceta(t, comps, []string{"1", "2"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST JUEGO SHOULD NOT APPEAR",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.Equal(t, seededID, result.ArticuloID, "debe encontrar el juego sembrado")
		require.False(t, result.Creado, "juego ya existía: Creado debe ser false")
	})
}

func TestJuegoResolver_NearMiss_DifferentUnidades(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 2)

		// Seed juego with qty=1 for both.
		seedJuego(ctx, t, q, comps, []string{"1", "1"}, "MSP TEST NEAR MISS UND 202406")

		// Resolve with qty=2 for second → should NOT match.
		receta := buildReceta(t, comps, []string{"1", "2"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST NEAR MISS UND NEW 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "unidades distintas: debe crear nuevo juego")
	})
}

func TestJuegoResolver_NearMiss_ExtraComponent(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 3)

		// Seed juego with only 2 components.
		seedJuego(ctx, t, q, comps[:2], []string{"1", "1"}, "MSP TEST NEAR MISS EXTRA 202406")

		// Resolve with all 3 → extra component in recipe → no match.
		receta := buildReceta(t, comps, []string{"1", "1", "1"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST NEAR MISS EXTRA NEW 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "receta con componente extra: debe crear nuevo juego")
	})
}

func TestJuegoResolver_NearMiss_MissingComponent(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 3)

		// Seed juego with all 3 components.
		seedJuego(ctx, t, q, comps, []string{"1", "1", "1"}, "MSP TEST NEAR MISS FALTANTE 202406")

		// Resolve with only 2 → missing component → no match.
		receta := buildReceta(t, comps[:2], []string{"1", "1"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST NEAR MISS FALTANTE NEW 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "receta incompleta: debe crear nuevo juego")
	})
}

func TestJuegoResolver_NearMiss_DifferentComponent(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 3)

		// Seed juego with components[0]+[1].
		seedJuego(ctx, t, q, comps[:2], []string{"1", "1"}, "MSP TEST NEAR MISS COMP 202406")

		// Resolve with components[0]+[2] → different second component → no match.
		receta := buildReceta(t, []componenteRow{comps[0], comps[2]}, []string{"1", "1"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST NEAR MISS COMP NEW 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "componente diferente: debe crear nuevo juego")
	})
}

func TestJuegoResolver_Create_WritesCorrectly(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 2)

		receta := buildReceta(t, comps, []string{"1", "3"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST CREATE VERIFY 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "juego nuevo: Creado debe ser true")
		require.NotZero(t, result.ArticuloID)

		// Verify ARTICULOS row.
		var esJuego, esAlmacenable, estatus string
		var lineaID int
		err = q.QueryRowContext(ctx,
			`SELECT ES_JUEGO, ES_ALMACENABLE, ESTATUS, LINEA_ARTICULO_ID FROM ARTICULOS WHERE ARTICULO_ID = ?`,
			result.ArticuloID).
			Scan(&esJuego, &esAlmacenable, &estatus, &lineaID)
		require.NoError(t, err)
		require.Equal(t, "S", esJuego)
		require.Equal(t, "N", esAlmacenable)
		require.Equal(t, "A", estatus)
		require.Equal(t, lineaArticuloIDForTests, lineaID)

		// Verify JUEGOS_DET count == 2.
		var detCount int
		err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM JUEGOS_DET WHERE ARTICULO_ID = ?`, result.ArticuloID).
			Scan(&detCount)
		require.NoError(t, err)
		require.Equal(t, 2, detCount)

		// Verify each JUEGOS_DET component row.
		expectedQtys := []string{"1", "3"}
		for i, c := range comps {
			var rawQty any
			err = q.QueryRowContext(ctx,
				`SELECT CAST(UNIDADES AS NUMERIC(18,6)) FROM JUEGOS_DET WHERE ARTICULO_ID = ? AND COMPONENTE_ID = ?`,
				result.ArticuloID, c.articuloID).Scan(&rawQty)
			require.NoError(t, err)
			qty, err := firebird.ScanDecimal(rawQty, 6)
			require.NoError(t, err)
			require.Equal(t,
				decimal.RequireFromString(expectedQtys[i]).StringFixed(6),
				qty.StringFixed(6),
				"componente %d unidades incorrectas", i)
		}

		// Verify LIBRES_ARTICULOS row exists.
		var libresArtID int
		err = q.QueryRowContext(ctx,
			`SELECT ARTICULO_ID FROM LIBRES_ARTICULOS WHERE ARTICULO_ID = ?`,
			result.ArticuloID).Scan(&libresArtID)
		require.NoError(t, err)
		require.Equal(t, result.ArticuloID, libresArtID)
	})
}

func TestJuegoResolver_Idempotency(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 2)

		receta := buildReceta(t, comps, []string{"5", "2"})
		in := outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST IDEM 202406",
			LineaArticuloID: lineaArticuloIDForTests,
		}

		// First call → creates.
		r1, err := resolver.Resolve(ctx, in)
		require.NoError(t, err)
		require.True(t, r1.Creado)

		// Second call with same recipe → matches (idempotent).
		r2, err := resolver.Resolve(ctx, in)
		require.NoError(t, err)
		require.False(t, r2.Creado, "segunda llamada con misma receta: Creado debe ser false")
		require.Equal(t, r1.ArticuloID, r2.ArticuloID, "debe retornar el mismo ArticuloID")
	})
}

func TestJuegoResolver_NombreCollision_RetriesWithSuffix(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 3)

		nombre := "MSP TEST NOMBRE COLISION 202406"

		// Seed a juego with components[0]+[1] using the target nombre.
		seedJuego(ctx, t, q, comps[:2], []string{"1", "1"}, nombre)

		// Resolve a DIFFERENT recipe (components[0]+[2]) with the SAME nombre.
		// No match on recipe → must create, but NOMBRE is taken → suffix retry.
		receta := buildReceta(t, []componenteRow{comps[0], comps[2]}, []string{"1", "1"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: nombre,
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.True(t, result.Creado, "debe crear con nombre sufijado")
		require.NotZero(t, result.ArticuloID)

		// Verify the stored NOMBRE contains the original nombre + "-" prefix of suffix.
		var storedNombre firebird.Win1252
		err = q.QueryRowContext(ctx,
			`SELECT NOMBRE FROM ARTICULOS WHERE ARTICULO_ID = ?`,
			result.ArticuloID).Scan(&storedNombre)
		require.NoError(t, err)
		require.Contains(t, string(storedNombre), nombre+"-",
			"nombre almacenado debe comenzar con el nombre propuesto seguido de guión")
	})
}

func TestJuegoResolver_DifferentComponentOrder_StillMatches(t *testing.T) { //nolint:paralleltest
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	resolver := microsip.NewJuegoResolver(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		comps := requireRealComponentes(ctx, t, q, 2)

		// Seed with [0, 1] order.
		seededID := seedJuego(ctx, t, q, comps, []string{"1", "2"}, "MSP TEST ORDER 202406")

		// Resolve with [1, 0] order in the buildReceta call — Receta sorts
		// by articuloID so the canonical recipe is identical regardless of
		// the input order to buildReceta.
		receta := buildReceta(t, []componenteRow{comps[1], comps[0]}, []string{"2", "1"})
		result, err := resolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: "MSP TEST ORDER SHOULD NOT APPEAR",
			LineaArticuloID: lineaArticuloIDForTests,
		})
		require.NoError(t, err)
		require.Equal(t, seededID, result.ArticuloID, "orden diferente: debe hacer match al mismo juego")
		require.False(t, result.Creado)
	})
}
