// Integration tests for ClientesRepo against the real Microsip Firebird database.
//
// These tests are SKIPPED when FB_DATABASE is not set (the normal case for
// non-Firebird devs and CI without a DB container). They serve as the live
// verification scaffold for the Fase-1 checkpoint.
//
// All tests run inside fbtestutil.WithTestTransaction which always rolls back —
// no writes are made to the shared dev DB by this read-only repository.
//
//nolint:paralleltest // serial: tests share the rollback-only tx context.
//nolint:misspell    // Spanish domain vocabulary by project convention.
package clientesfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// Conceptos de CUENTAS POR COBRAR usados como semilla. No son filas de
// producción: son entradas del catálogo CONCEPTOS_CC —que `gbak -skip_data`
// conserva— y a la vez las constantes que clientes/domain/categoria.go clasifica.
// Cada prueba que los siembra afirma primero que el dominio los sigue
// clasificando como espera, así que un cambio de mapeo falla de forma explícita.
const (
	conceptoCobranza    = 87327 // "Cobranza en ruta"  → CategoriaIngresoPago
	conceptoEnganche    = 24533 // "Enganche"          → CategoriaIngresoEnganche
	conceptoCondonacion = 27969 // "Condonaciones"     → CategoriaCondonacion
	conceptoPerdida     = 27968 // "Mal Cliente"       → CategoriaPerdida
)

// requireFBEnv skips the test if FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — point it at the dev Microsip DB to run Firebird integration tests")
	}
}

// ─── ObtenerCliente ───────────────────────────────────────────────────────────

// TestClientesRepo_ObtenerCliente_Found verifica que una fila de CLIENTES se
// hidrate correctamente.
//
// Antes usaba al cliente de producción 12387 ("VICTORINO ENRIQUEZ — el
// comprador más frecuente"), elegido porque parecía difícil que lo borraran.
// Ahora la prueba crea el suyo: además de no depender del padrón, puede afirmar
// el NOMBRE y la ZONA que sembró en vez de conformarse con "no viene vacío".
//
// Que ya no lea clientes reales importa por una razón adicional: es lo que
// permitiría quitar los nombres, domicilios y teléfonos reales de la base de
// pruebas de 15 MB (ver "Sobre los datos de clientes" en
// docs/base-de-datos-de-pruebas.md).
func TestClientesRepo_ObtenerCliente_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		const nombre = "GUADALUPE HERNANDEZ SOLIS"
		zona := microsipseed.PrimeraZona(t, q)
		clienteID := microsipseed.ClienteEnZona(t, q, nombre, zona)

		c, err := repo.ObtenerCliente(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, clienteID, c.ClienteID())
		assert.Equal(t, nombre, c.Nombre(), "NOMBRE debe ser el que se sembró")
		assert.Equal(t, "A", c.Estatus(), "ESTATUS debe ser el que se sembró")
		assert.Equal(t, zona, c.ZonaClienteID(), "ZONA_CLIENTE_ID debe ser la que se sembró")
	})
}

// TestClientesRepo_ObtenerCliente_NotFound verifies ErrClienteNotFound sentinel.
func TestClientesRepo_ObtenerCliente_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerCliente(ctx, -999999)
		assert.ErrorIs(t, err, domain.ErrClienteNotFound)
	})
}

// ─── ListarDirectorioCompleto ─────────────────────────────────────────────────

// TestClientesRepo_ListarDirectorioCompleto_FilteredByZona verifica que el
// listado del directorio filtre por zona.
//
// Antes pedía la zona de producción 21566 ("la más grande, ~2.5k clientes") y
// sólo podía afirmar que devolvía algo. Ahora la prueba siembra sus clientes en
// una zona y otro en una zona distinta, así que puede afirmar que el filtro
// INCLUYE los primeros y EXCLUYE el último — que es lo que el filtro hace.
func TestClientesRepo_ListarDirectorioCompleto_FilteredByZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaA, zonaB := dosZonas(t, q)

		enZonaA := map[int]bool{
			microsipseed.ClienteEnZona(t, q, "ROSA MARTINEZ AGUILAR", zonaA):  true,
			microsipseed.ClienteEnZona(t, q, "IGNACIO VALDEZ CAMACHO", zonaA): true,
		}
		fueraDeZonaA := microsipseed.ClienteEnZona(t, q, "TERESA NUÑEZ RIVAS", zonaB)

		items, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ZonaClienteID: &zonaA,
		})
		require.NoError(t, err)
		require.NotEmpty(t, items, "la zona sembrada debe devolver filas")

		vistos := map[int]bool{}
		for _, it := range items {
			require.NotNil(t, it.Cliente)
			assert.NotEmpty(t, it.Cliente.Nombre())
			assert.False(t, it.SaldoTotal.IsNegative(), "el saldo debe ser >= 0")
			vistos[it.Cliente.ClienteID()] = true
		}

		for id := range enZonaA {
			assert.Truef(t, vistos[id], "el cliente %d de la zona %d debe aparecer", id, zonaA)
		}
		assert.Falsef(t, vistos[fueraDeZonaA],
			"el cliente %d vive en la zona %d y NO debe aparecer al filtrar por %d",
			fueraDeZonaA, zonaB, zonaA)
	})
}

// dosZonas devuelve dos zonas distintas del catálogo, para poder probar que un
// filtro incluye una y excluye la otra.
func dosZonas(t *testing.T, q firebird.Querier) (int, int) {
	t.Helper()
	rows, err := q.QueryContext(context.Background(),
		`SELECT FIRST 2 ZONA_CLIENTE_ID FROM ZONAS_CLIENTES ORDER BY ZONA_CLIENTE_ID`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	zonas := make([]int, 0, 2)
	for rows.Next() {
		var z int
		require.NoError(t, rows.Scan(&z))
		zonas = append(zonas, z)
	}
	require.NoError(t, rows.Err())
	require.Len(t, zonas, 2, "el catálogo ZONAS_CLIENTES debe tener al menos dos zonas")
	return zonas[0], zonas[1]
}

// TestClientesRepo_ListarDirectorioCompleto_ConSaldo verifica que el filtro
// ConSaldo deje fuera a los clientes sin saldo.
//
// Antes pedía la zona de producción 21566 y recorría lo que viniera. Contra una
// base sin movimientos la lista salía VACÍA y el ciclo no se ejecutaba: la
// prueba pasaba sin verificar nada. Ahora siembra un cliente con saldo y otro
// sin él, así que el filtro tiene que discriminar entre dos casos conocidos.
func TestClientesRepo_ListarDirectorioCompleto_ConSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)

		conSaldo := microsipseed.ClienteEnZona(t, q, "ARTURO BELTRAN OCHOA", zona)
		microsipseed.VentaCredito(t, q, conSaldo, microsipseed.OpcionesVenta{})
		sinSaldo := microsipseed.ClienteEnZona(t, q, "MARIA ELENA PONCE DIAZ", zona)

		items, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ZonaClienteID: &zona,
			ConSaldo:      true,
		})
		require.NoError(t, err)

		vistos := map[int]bool{}
		for _, it := range items {
			assert.Truef(t, it.SaldoTotal.IsPositive(),
				"ConSaldo debe excluir a los clientes de saldo cero (cliente %d traía %s)",
				it.Cliente.ClienteID(), it.SaldoTotal)
			vistos[it.Cliente.ClienteID()] = true
		}

		assert.Truef(t, vistos[conSaldo],
			"el cliente %d tiene una venta a crédito sin abonar y debe aparecer", conSaldo)
		assert.Falsef(t, vistos[sinSaldo],
			"el cliente %d no tiene ninguna venta y NO debe aparecer", sinSaldo)
	})
}

// TestClientesRepo_ListarDirectorioCompleto_Saldo verifica que el saldo agrupado
// del directorio sea exactamente lo comprado menos lo abonado.
//
// Antes usaba al cliente de producción 12440, cuyo saldo se había observado en
// $504,666.60 el 2026-06-16, y por eso sólo podía afirmar "no es negativo": el
// número real cambiaba con cada abono. Ahora la prueba conoce las dos cifras que
// entran, así que exige la RESTA exacta.
func TestClientesRepo_ListarDirectorioCompleto_Saldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		clienteID := microsipseed.Cliente(t, q, "HECTOR RAMIREZ QUEZADA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Total: decimal.NewFromInt(15000),
		})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(3500),
		})

		completo, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ClienteIDs: []int{clienteID},
		})
		require.NoError(t, err)
		require.Len(t, completo, 1)

		esperado := venta.Total.Sub(abono.Importe)
		assert.Truef(t, completo[0].SaldoTotal.Equal(esperado),
			"saldo agrupado esperado %s (venta %s − abono %s), obtenido %s",
			esperado, venta.Total, abono.Importe, completo[0].SaldoTotal)
	})
}

// ─── ObtenerResumenFicha ──────────────────────────────────────────────────────

// TestClientesRepo_ObtenerResumenFicha verifica la agregación financiera de la
// ficha contra una venta y un abono SEMBRADOS por la prueba.
//
// Antes traía al cliente de producción 2782515 y sólo podía afirmar
// "TotalComprado > 0", porque nadie sabía cuánto debía dar. Ahora la prueba
// conoce las cifras por construcción y puede exigir la IGUALDAD exacta, que es
// lo que de verdad verifica la agregación.
func TestClientesRepo_ObtenerResumenFicha(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		clienteID := microsipseed.Cliente(t, q, "RESUMEN FICHA PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Total: decimal.NewFromInt(10000),
		})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(2500),
		})

		resumen, err := repo.ObtenerResumenFicha(ctx, clienteID, outbound.RangoFechas{})
		require.NoError(t, err)

		assert.True(t, resumen.TotalComprado.Equal(venta.Total),
			"TotalComprado debe ser el total de la única venta sembrada (esperado %s, obtenido %s)",
			venta.Total, resumen.TotalComprado)
		assert.True(t, resumen.TotalAbonado.Equal(abono.Importe),
			"TotalAbonado debe ser el importe del único abono sembrado (esperado %s, obtenido %s)",
			abono.Importe, resumen.TotalAbonado)
		assert.Equal(t, 1, resumen.NumVentas, "NumVentas")
		assert.Equal(t, 1, resumen.NumPagos, "NumPagos")
		assert.False(t, resumen.SaldoTotal.IsNegative(), "SaldoTotal debe ser >= 0")
		assert.False(t, resumen.PctLiquidado.IsNegative(), "PctLiquidado debe ser >= 0")

		t.Logf("ResumenFicha cliente=%d: comprado=%s abonado=%s saldo=%s ventas=%d pagos=%d",
			clienteID, resumen.TotalComprado, resumen.TotalAbonado, resumen.SaldoTotal,
			resumen.NumVentas, resumen.NumPagos)
	})
}

// ─── ListarVentas ─────────────────────────────────────────────────────────────

// TestClientesRepo_ListarVentas verifica la paginación del listado de ventas.
//
// Antes dependía de que el cliente 12387 conservara sus 2,381 ventas en la base
// compartida. Ahora la prueba siembra exactamente doce ventas y pide páginas de
// diez: el corte, el cursor y el conteo de la segunda página son verificables
// porque el total es conocido.
func TestClientesRepo_ListarVentas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := microsipseed.Cliente(t, q, "LISTAR VENTAS PRUEBA")

		const totalVentas = 12
		for i := range totalVentas {
			microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
				Fecha: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
			})
		}

		page, err := repo.ListarVentas(ctx, clienteID, outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		require.Len(t, page.Items, 10, "la primera página debe traer exactamente PageSize filas")
		require.NotEmpty(t, page.NextCursor,
			"con %d ventas y páginas de 10 debe haber cursor a la siguiente página", totalVentas)

		for _, v := range page.Items {
			require.NotNil(t, v)
			assert.Equal(t, clienteID, v.ClienteID())
			assert.True(t, v.Tipo().IsValid(), "tipo debe ser válido")
			assert.False(t, v.SaldoVenta().IsNegative())
		}

		segunda, err := repo.ListarVentas(ctx, clienteID,
			outbound.ListParams{PageSize: 10, Cursor: page.NextCursor})
		require.NoError(t, err)
		assert.Len(t, segunda.Items, totalVentas-10,
			"la segunda página debe traer el resto de las %d ventas sembradas", totalVentas)
		assert.Empty(t, segunda.NextCursor, "la última página no debe traer cursor")
	})
}

// ─── ObtenerVentaDetalle ──────────────────────────────────────────────────────

// TestClientesRepo_ObtenerVentaDetalle_Found verifica el paquete de detalle de
// una venta sembrada por la prueba.
//
// Antes apuntaba a DOCTO_PV_ID=14941516, una venta de producción. El comentario
// original ya dejaba constancia de que el identificador anterior (15542211) se
// había caído de la base — la fragilidad estaba documentada y seguía ahí.
func TestClientesRepo_ObtenerVentaDetalle_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		clienteID := microsipseed.Cliente(t, q, "VENTA DETALLE PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Total:      decimal.NewFromInt(8800),
			PlazoMeses: 12,
		})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(500),
		})

		detail, err := repo.ObtenerVentaDetalle(ctx, venta.DoctoPVID)
		require.NoError(t, err)
		require.NotNil(t, detail.Venta)

		assert.Equal(t, venta.DoctoPVID, detail.Venta.DoctoPVID())
		assert.Equal(t, clienteID, detail.Venta.ClienteID())
		require.Len(t, detail.Productos, 1, "la venta sembrada tiene una sola línea ROL='N'")
		assert.Equal(t, venta.ArticuloID, detail.Productos[0].ArticuloID())
		assert.Equal(t, venta.ArticuloNombre, detail.Productos[0].Nombre(),
			"el nombre debe venir del catálogo ARTICULOS")
		assert.True(t, detail.Productos[0].Unidades().IsPositive())

		require.Len(t, detail.Pagos, 1, "se sembró exactamente un abono")
		require.NotNil(t, detail.Contrato, "una venta a crédito debe traer contrato")
		assert.Equal(t, 12, detail.Contrato.PlazoMeses, "PlazoMeses del contrato sembrado")

		t.Logf("ObtenerVentaDetalle %d: tipo=%s productos=%d pagos=%d",
			venta.DoctoPVID, detail.Venta.Tipo(), len(detail.Productos), len(detail.Pagos))
	})
}

// TestClientesRepo_ObtenerVentaDetalle_NotFound verifies ErrVentaNotFound.
func TestClientesRepo_ObtenerVentaDetalle_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerVentaDetalle(ctx, -999999)
		assert.ErrorIs(t, err, domain.ErrVentaNotFound)
	})
}

// ─── ObtenerVentaDetalle — concepto/categoria enrichment ─────────────────────

// TestClientesRepo_ObtenerVentaDetalle_PagosEnriquecidos verifica que cada abono
// venga enriquecido con concepto, categoría y cobrador.
//
// Antes leía la venta de producción 4070523 y su cargo 4070585, y afirmaba sobre
// los diez movimientos que esa venta tenía en la base compartida. Ahora la
// prueba siembra un abono por cada categoría del clasificador del dominio y
// exige que el repositorio devuelva EXACTAMENTE esa clasificación.
//
// Los CONCEPTO_CC_ID de abajo NO son filas de producción: son entradas del
// catálogo CONCEPTOS_CC (que `-skip_data` conserva) y constantes del mapeo en
// clientes/domain/categoria.go. La prueba afirma primero que el dominio los
// clasifica como espera, así que un cambio en ese mapeo falla aquí con nombre y
// apellido en vez de producir un resultado silenciosamente distinto.
func TestClientesRepo_ObtenerVentaDetalle_PagosEnriquecidos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptosPorCategoria()...)

		clienteID := microsipseed.Cliente(t, q, "PAGOS ENRIQUECIDOS PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Total: decimal.NewFromInt(20000),
		})

		// Un abono por categoría. El cobrador sólo se asigna en el de cobranza,
		// para poder afirmar las DOS ramas del enriquecimiento de cobrador.
		esperado := map[int]domain.Categoria{}
		for concepto, categoria := range categoriaEsperadaPorConcepto() {
			require.Equalf(t, categoria, domain.ClasificarConcepto(concepto),
				"el clasificador del dominio cambió: ClasificarConcepto(%d) ya no es %q. "+
					"Actualiza esta tabla junto con clientes/domain/categoria.go.", concepto, categoria)
			microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: concepto,
				Importe:      decimal.NewFromInt(500),
				ConCobrador:  categoria == domain.CategoriaIngresoPago,
			})
			esperado[concepto] = categoria
		}

		detail, err := repo.ObtenerVentaDetalle(ctx, venta.DoctoPVID)
		require.NoError(t, err)
		require.NotNil(t, detail.Venta)
		require.Len(t, detail.Pagos, len(esperado),
			"se sembró un abono por categoría; el repositorio debe devolverlos todos")

		vistas := map[domain.Categoria]bool{}
		for _, p := range detail.Pagos {
			assert.NotEmptyf(t, p.Concepto(),
				"Concepto no debe venir vacío (DoctoCCID=%d) — sale de CONCEPTOS_CC", p.DoctoCCID())

			cat := p.Categoria()
			assert.Equalf(t, esperado[p.ConceptoCCID()], cat,
				"el repositorio clasificó el concepto %d como %q; el dominio dice %q",
				p.ConceptoCCID(), cat, esperado[p.ConceptoCCID()])
			esIngresoEsperado := cat != domain.CategoriaCondonacion && cat != domain.CategoriaPerdida
			assert.Equalf(t, esIngresoEsperado, cat.EsIngreso(),
				"condonación y pérdida no son ingreso; el resto sí (categoria=%q, DoctoCCID=%d)",
				cat, p.DoctoCCID())
			vistas[cat] = true

			if cat == domain.CategoriaIngresoPago {
				assert.NotEmpty(t, p.Cobrador(),
					"el abono de cobranza se sembró con cobrador; debe llegar resuelto")
			}
		}

		for concepto, categoria := range esperado {
			assert.Truef(t, vistas[categoria],
				"falta la categoría %q (concepto %d) en el detalle", categoria, concepto)
		}
	})
}

// categoriaEsperadaPorConcepto es la tabla concepto → categoría que esta prueba
// siembra. Un concepto por categoría del clasificador, todos presentes en el
// catálogo CONCEPTOS_CC que la base reducida conserva.
func categoriaEsperadaPorConcepto() map[int]domain.Categoria {
	return map[int]domain.Categoria{
		conceptoCobranza:    domain.CategoriaIngresoPago,
		conceptoEnganche:    domain.CategoriaIngresoEnganche,
		conceptoCondonacion: domain.CategoriaCondonacion,
		conceptoPerdida:     domain.CategoriaPerdida,
	}
}

func conceptosPorCategoria() []int {
	ids := make([]int, 0, 4)
	for id := range categoriaEsperadaPorConcepto() {
		ids = append(ids, id)
	}
	return ids
}

// ─── ListarVentas — campos enriquecidos ────────────────────────────────────────

// TestClientesRepo_ListarVentas_CamposEnriquecidos verifica los campos
// enriquecidos (hora, almacén, primer artículo, número de artículos).
//
// Antes fijaba a mano el resultado de la venta de producción 4070523: la hora
// "18:06:49", el almacén "TIENDA DE EXHIBICION" y el nombre completo de una
// lavadora. Ahora la hora es la que la prueba sembró, y el almacén y el artículo
// se comparan contra el CATÁLOGO del que el sembrador los tomó — si Microsip
// renombra un almacén, la prueba sigue pasando porque compara dos lecturas del
// mismo catálogo, no una lectura contra una cadena congelada.
func TestClientesRepo_ListarVentas_CamposEnriquecidos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := microsipseed.Cliente(t, q, "CAMPOS ENRIQUECIDOS PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Hora: "18:06:49",
		})

		page, err := repo.ListarVentas(ctx, clienteID, outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		require.Len(t, page.Items, 1,
			"el cliente sintético tiene exactamente la venta que esta prueba sembró")

		v := page.Items[0]
		require.NotNil(t, v)

		assert.Equal(t, venta.DoctoPVID, v.DoctoPVID(), "DoctoPVID")
		assert.Equal(t, venta.Hora, v.Hora(), "Hora")
		assert.Equal(t, venta.AlmacenNombre, v.Almacen(), "Almacen (contra ALMACENES.NOMBRE)")
		assert.Equal(t, venta.ArticuloNombre, v.PrimerArticulo(), "PrimerArticulo (contra ARTICULOS.NOMBRE)")
		assert.Equal(t, 1, v.NumArticulos(), "NumArticulos — se sembró una sola línea J/N")

		t.Logf("CamposEnriquecidos: doctoPVID=%d hora=%q almacen=%q primerArticulo=%q numArticulos=%d",
			v.DoctoPVID(), v.Hora(), v.Almacen(), v.PrimerArticulo(), v.NumArticulos())
	})
}

// ─── ObtenerPagoDetalle ───────────────────────────────────────────────────────

// TestClientesRepo_ObtenerPagoDetalle_Found verifica el detalle de un abono de
// enganche sembrado por la prueba.
//
// Antes traía el pago de producción 4070588 y fijaba a mano su folio
// ("000013412"), su cobrador (77486), su importe ($200.00) y su fecha
// (2020-07-02). Todos esos valores ahora los pone la prueba, así que el aserto
// verifica el MAPEO del repositorio en lugar del contenido de una fila ajena.
func TestClientesRepo_ObtenerPagoDetalle_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoEnganche)

		clienteID := microsipseed.Cliente(t, q, "PAGO DETALLE PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{})
		fechaAbono := time.Date(2020, 7, 2, 0, 0, 0, 0, time.UTC)
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID:  conceptoEnganche,
			Importe:       decimal.NewFromInt(200),
			Fecha:         fechaAbono,
			ConCobrador:   true,
			ConFormaCobro: true,
		})

		detalle, err := repo.ObtenerPagoDetalle(ctx, abono.DoctoCCID)
		require.NoError(t, err)

		assert.Equal(t, abono.DoctoCCID, detalle.DoctoCCID, "DoctoCCID")
		assert.Equal(t, abono.Folio, detalle.Folio, "Folio")
		assert.False(t, detalle.Cancelado, "Cancelado")
		assert.True(t, detalle.Aplicado, "Aplicado")
		assert.Equal(t, abono.CobradorID, detalle.CobradorID, "CobradorID")
		assert.Equal(t, conceptoEnganche, detalle.ConceptoCCID, "ConceptoCCID")
		assert.Equal(t, string(domain.CategoriaIngresoEnganche), detalle.Categoria, "Categoria")
		assert.True(t, domain.Categoria(detalle.Categoria).EsIngreso(), "EsIngreso")
		assert.True(t, detalle.Importe.Equal(abono.Importe),
			"Importe esperado %v, obtenido %v", abono.Importe, detalle.Importe)
		assert.Equal(t, venta.CargoCCID, detalle.AplicaACargoID, "AplicaACargoID")
		assert.Equal(t, venta.DoctoPVID, detalle.DoctoPVID, "DoctoPVID (resuelto vía DOCTOS_ENTRE_SIS)")
		assert.Equal(t, "microsip", detalle.Origen, "Origen")
		assert.Equal(t, microsipseed.NombreCobrador(t, q, abono.CobradorID), detalle.Cobrador,
			"Cobrador (contra COBRADORES.NOMBRE)")
		assert.Equal(t, microsipseed.NombreFormaCobro(t, q), detalle.FormaCobro,
			"FormaCobro (contra FORMAS_COBRO.NOMBRE)")

		assert.Equal(t, fechaAbono.Year(), detalle.Fecha.Year(), "Fecha año")
		assert.Equal(t, fechaAbono.Month(), detalle.Fecha.Month(), "Fecha mes")
		assert.Equal(t, fechaAbono.Day(), detalle.Fecha.Day(), "Fecha día")

		assert.True(t, detalle.IVA.IsZero(), "el enganche se sembró sin IVA")

		require.NotNil(t, detalle.SaldoCargo,
			"la cascada de Microsip debe haber poblado MSP_SALDOS_VENTAS para el cargo")
		assert.True(t, detalle.SaldoCargo.Equal(venta.Total.Sub(abono.Importe)),
			"SaldoCargo esperado %v, obtenido %v", venta.Total.Sub(abono.Importe), detalle.SaldoCargo)
	})
}

// TestClientesRepo_ObtenerPagoDetalle_Cobranza verifica el detalle de un abono
// de cobranza — la otra rama de la clasificación — sobre la misma venta.
//
// Antes era el pago de producción 4172481 sobre el cargo 4070585.
func TestClientesRepo_ObtenerPagoDetalle_Cobranza(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		clienteID := microsipseed.Cliente(t, q, "PAGO COBRANZA PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(1500),
			ConCobrador:  true,
		})

		detalle, err := repo.ObtenerPagoDetalle(ctx, abono.DoctoCCID)
		require.NoError(t, err)

		assert.Equal(t, conceptoCobranza, detalle.ConceptoCCID, "ConceptoCCID")
		assert.Equal(t, string(domain.CategoriaIngresoPago), detalle.Categoria, "Categoria")
		assert.True(t, domain.Categoria(detalle.Categoria).EsIngreso(), "EsIngreso")
		assert.Equal(t, venta.CargoCCID, detalle.AplicaACargoID, "AplicaACargoID")
		assert.Equal(t, venta.DoctoPVID, detalle.DoctoPVID, "DoctoPVID")
		assert.Equal(t, "microsip", detalle.Origen, "Origen")
	})
}

func TestClientesRepo_ObtenerPagoDetalle_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerPagoDetalle(ctx, 1)
		assert.ErrorIs(t, err, domain.ErrPagoNotFound)
	})
}

// ─── CompradoVsAbonado — desglose por categoría ───────────────────────────────

// TestClientesRepo_CompradoVsAbonado_DesgloseCategorias verifica que la serie
// mensual separe correctamente los cubos por categoría.
//
// Antes leía los movimientos del cargo de producción 4070585 y sólo podía
// afirmar "cada cubo es positivo", porque los montos reales no se conocían.
// Ahora cada categoría se siembra con un importe DISTINTO, así que la prueba
// exige la igualdad exacta por cubo — que es lo que detecta un abono cayendo en
// la columna equivocada, precisamente el defecto que la serie puede tener.
func TestClientesRepo_CompradoVsAbonado_DesgloseCategorias(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptosPorCategoria()...)

		clienteID := microsipseed.Cliente(t, q, "COMPRADO VS ABONADO PRUEBA")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Fecha: time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
			Total: decimal.NewFromInt(20000),
		})

		// Importes distintos por categoría: si el SQL mete un abono en el cubo
		// equivocado, los totales no cuadran y el aserto dice cuál se movió.
		importes := map[int]decimal.Decimal{
			conceptoCobranza:    decimal.NewFromInt(1100),
			conceptoEnganche:    decimal.NewFromInt(2200),
			conceptoCondonacion: decimal.NewFromInt(3300),
			conceptoPerdida:     decimal.NewFromInt(4400),
		}
		for concepto, importe := range importes {
			microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: concepto,
				Importe:      importe,
				Fecha:        venta.Fecha,
			})
		}

		resumen, err := repo.ObtenerResumenFicha(ctx, clienteID, outbound.RangoFechas{})
		require.NoError(t, err)

		serie := resumen.CompradoVsAbonado
		require.NotEmpty(t, serie, "la venta sembrada debe producir al menos un punto en la serie")

		var totalComprado, totalCobranza, totalEnganche, totalCondonacion, totalPerdida decimal.Decimal
		for _, pt := range serie {
			totalComprado = totalComprado.Add(pt.Comprado)
			totalCobranza = totalCobranza.Add(pt.Cobranza)
			totalEnganche = totalEnganche.Add(pt.Enganche)
			totalCondonacion = totalCondonacion.Add(pt.Condonacion)
			totalPerdida = totalPerdida.Add(pt.Perdida)

			t.Logf("mes %d/%d: comprado=%s cobranza=%s enganche=%s condonacion=%s perdida=%s otro=%s",
				pt.Anio, pt.Mes,
				pt.Comprado, pt.Cobranza, pt.Enganche, pt.Condonacion, pt.Perdida, pt.Otro)
		}

		assert.Truef(t, totalComprado.Equal(venta.Total),
			"Comprado esperado %s, obtenido %s", venta.Total, totalComprado)
		assert.Truef(t, totalCobranza.Equal(importes[conceptoCobranza]),
			"Cobranza esperado %s, obtenido %s", importes[conceptoCobranza], totalCobranza)
		assert.Truef(t, totalEnganche.Equal(importes[conceptoEnganche]),
			"Enganche esperado %s, obtenido %s", importes[conceptoEnganche], totalEnganche)
		assert.Truef(t, totalCondonacion.Equal(importes[conceptoCondonacion]),
			"Condonacion esperado %s, obtenido %s", importes[conceptoCondonacion], totalCondonacion)
		assert.Truef(t, totalPerdida.Equal(importes[conceptoPerdida]),
			"Perdida esperado %s, obtenido %s", importes[conceptoPerdida], totalPerdida)
	})
}
