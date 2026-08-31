// Controles positivos de codificación para clientesfb.
//
// POR QUÉ ESTE ARCHIVO EXISTE
// ===========================
// Nueve de las columnas que este paquete lee se estaban decodificando dos
// veces: son CHARACTER SET ISO8859_1, Firebird ya las entrega en UTF-8 sobre la
// conexión charset=UTF8, y el repositorio las volvía a pasar por
// firebird.Win1252. La "Ñ" salía como "Ã‘". No lo atrapó ninguna prueba porque
// ningún fixture tenía una: sobre ASCII puro la lectura buena y la mala dan
// exactamente los mismos bytes.
//
// Todo lo que sigue siembra acentos a propósito y compara contra el valor
// VERDADERO. Cada aserción falla con el defecto puesto — verificado
// reintroduciéndolo.
//
// Columnas cubiertas aquí, con su charset (fijado contra RDB$FIELDS por
// internal/platform/fbcharset):
//
//	CLIENTES.NOMBRE       ISO8859_1  → string
//	ALMACENES.NOMBRE      ISO8859_1  → sql.NullString
//	ARTICULOS.NOMBRE      ISO8859_1  → string / sql.NullString
//	DOCTOS_CC.DESCRIPCION NONE       → rama del COALESCE, ver abajo
//	CLIENTES.NOTAS        NONE       → firebird.Win1252
//
//nolint:paralleltest // serie por diseño: comparten la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package clientesfb_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// nombreAcentuado es el nombre que se siembra. La Ñ es el caso que importa:
// 99.7% de los 1,615 clientes con no-ASCII del padrón la traen.
const nombreAcentuado = "MARÍA DEL CARMEN FARIÑO MUÑOZ"

// sinMojibake exige UTF-8 válido y ausencia de la firma del doble-decode.
// "Ã" aparece siempre que un byte UTF-8 de un acento español (C3 xx) se
// reinterpreta como Windows-1252, así que sirve de detector genérico.
func sinMojibake(t *testing.T, columna, valor string) {
	t.Helper()
	assert.Truef(t, utf8.ValidString(valor),
		"%s no devolvió UTF-8 válido: %q. Una columna CHARACTER SET NONE leída en "+
			"plano deja bytes crudos así.", columna, valor)
	assert.NotContainsf(t, valor, "Ã",
		"%s devolvió %q — la \"Ã\" es la firma del doble-decode Windows-1252 sobre "+
			"bytes que ya eran UTF-8.", columna, valor)
}

// TestClientesRepo_Acentos_NombreDeCliente cubre CLIENTES.NOMBRE por los dos
// caminos que lo leen: la ficha y el directorio.
func TestClientesRepo_Acentos_NombreDeCliente(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		require.True(t, fbtestutil.ContieneNoASCII(nombreAcentuado),
			"el fixture tiene que traer acentos o la prueba no demuestra nada")

		zona := microsipseed.PrimeraZona(t, q)
		clienteID := microsipseed.ClienteEnZona(t, q, nombreAcentuado, zona)

		c, err := repo.ObtenerCliente(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, nombreAcentuado, c.Nombre(), "CLIENTES.NOMBRE por ObtenerCliente")
		sinMojibake(t, "CLIENTES.NOMBRE", c.Nombre())

		items, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{ZonaClienteID: &zona})
		require.NoError(t, err)
		encontrado := false
		for _, it := range items {
			if it.Cliente.ClienteID() == clienteID {
				encontrado = true
				assert.Equal(t, nombreAcentuado, it.Cliente.Nombre(),
					"CLIENTES.NOMBRE por ListarDirectorioCompleto")
				sinMojibake(t, "CLIENTES.NOMBRE (directorio)", it.Cliente.Nombre())
			}
		}
		require.True(t, encontrado, "el cliente sembrado debe aparecer en su zona")
	})
}

// TestClientesRepo_Acentos_VentaDetalle cubre ARTICULOS.NOMBRE y ALMACENES.NOMBRE
// eligiendo a propósito un artículo del catálogo que TIENE acentos.
//
// El artículo rotativo por omisión sólo acierta uno acentuado ~2% de las veces
// (121 de 6,113 filas): dejar la cobertura a esa lotería es lo que permitió que
// el defecto viviera meses.
func TestClientesRepo_Acentos_VentaDetalle(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		articuloID := microsipseed.ArticuloConAcento(t, q)
		require.NotZero(t, articuloID,
			"ningún artículo del catálogo trae acentos; sin eso esta prueba no verifica nada")
		nombreArticulo := microsipseed.NombreDeCatalogo(t, q, "ARTICULOS", "ARTICULO_ID", articuloID)
		require.True(t, fbtestutil.ContieneNoASCII(nombreArticulo))

		clienteID := microsipseed.Cliente(t, q, nombreAcentuado)
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			ArticuloID: articuloID,
			Total:      decimal.NewFromInt(8800),
		})

		detalle, err := repo.ObtenerVentaDetalle(ctx, venta.DoctoPVID)
		require.NoError(t, err)
		require.Len(t, detalle.Productos, 1)

		assert.Equal(t, nombreArticulo, detalle.Productos[0].Nombre(),
			"ARTICULOS.NOMBRE en la línea de la venta")
		sinMojibake(t, "ARTICULOS.NOMBRE (producto)", detalle.Productos[0].Nombre())

		assert.Equal(t, nombreArticulo, detalle.Venta.PrimerArticulo(),
			"ARTICULOS.NOMBRE por la subconsulta PRIMER_ARTICULO")
		sinMojibake(t, "ARTICULOS.NOMBRE (primer artículo)", detalle.Venta.PrimerArticulo())

		nombreAlmacen := microsipseed.NombreDeCatalogo(t, q, "ALMACENES", "ALMACEN_ID", venta.AlmacenID)
		assert.Equal(t, nombreAlmacen, detalle.Venta.Almacen(), "ALMACENES.NOMBRE")
		sinMojibake(t, "ALMACENES.NOMBRE", detalle.Venta.Almacen())

		// El mismo par de columnas por el listado paginado.
		page, err := repo.ListarVentas(ctx, clienteID, outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		require.NotEmpty(t, page.Items)
		assert.Equal(t, nombreArticulo, page.Items[0].PrimerArticulo(),
			"ARTICULOS.NOMBRE por ListarVentas")
		assert.Equal(t, nombreAlmacen, page.Items[0].Almacen(), "ALMACENES.NOMBRE por ListarVentas")
	})
}

// TestClientesRepo_Acentos_CoalesceCobradorODescripcion es el control del CASO
// MIXTO: COALESCE(cob.NOMBRE, pago.DESCRIPCION) en queryPagos combina una
// columna ISO8859_1 con una CHARACTER SET NONE en UNA SOLA expresión.
//
// Firebird resuelve esa expresión al charset NO-NONE del par —ISO8859_1— y
// transliterar AMBAS ramas a UTF-8 para la conexión charset=UTF8. Por eso el
// destino de escaneo es un sql.NullString y no hizo falta ni partir el SQL ni
// meter un CAST. La prueba ejercita las dos ramas:
//
//	con cobrador → gana COBRADORES.NOMBRE (ISO8859_1)
//	sin cobrador → cae a DOCTOS_CC.DESCRIPCION (NONE), sembrada con acentos
//	               en bytes Windows-1252, que es como Microsip la guarda
func TestClientesRepo_Acentos_CoalesceCobradorODescripcion(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		const descripcionAcentuada = "Depósito en sucursal — MUÑOZ"
		// El guion largo no existe en ISO8859_1; se usa uno normal.
		descripcion := strings.ReplaceAll(descripcionAcentuada, "—", "-")
		require.True(t, fbtestutil.ContieneNoASCII(descripcion))

		clienteID := microsipseed.Cliente(t, q, nombreAcentuado)
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{})

		// Rama NONE: sin cobrador, el COALESCE cae a DESCRIPCION.
		sinCobrador := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(300),
			Descripcion:  descripcion,
		})
		// Rama ISO8859_1: con cobrador, gana COBRADORES.NOMBRE.
		conCobrador := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza,
			Importe:      decimal.NewFromInt(400),
			ConCobrador:  true,
			Descripcion:  descripcion,
		})

		detalle, err := repo.ObtenerVentaDetalle(ctx, venta.DoctoPVID)
		require.NoError(t, err)
		require.Len(t, detalle.Pagos, 2)

		porID := map[int]string{}
		for _, p := range detalle.Pagos {
			porID[p.DoctoCCID()] = p.Cobrador()
		}

		assert.Equalf(t, descripcion, porID[sinCobrador.DoctoCCID],
			"rama NONE del COALESCE: sin cobrador debe devolver DOCTOS_CC.DESCRIPCION verbatim")
		sinMojibake(t, "COALESCE → DOCTOS_CC.DESCRIPCION", porID[sinCobrador.DoctoCCID])

		esperadoCobrador := microsipseed.NombreCobrador(t, q, conCobrador.CobradorID)
		assert.Equalf(t, esperadoCobrador, porID[conCobrador.DoctoCCID],
			"rama ISO8859_1 del COALESCE: con cobrador debe devolver COBRADORES.NOMBRE")
		sinMojibake(t, "COALESCE → COBRADORES.NOMBRE", porID[conCobrador.DoctoCCID])

		// Y el detalle del pago, que lee cob.NOMBRE y p.DESCRIPCION por
		// SEPARADO (dos destinos de escaneo distintos), tiene que coincidir.
		det, err := repo.ObtenerPagoDetalle(ctx, sinCobrador.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, descripcion, det.Cobrador,
			"ObtenerPagoDetalle sin cobrador cae a DOCTOS_CC.DESCRIPCION (columna NONE, Win1252)")
		sinMojibake(t, "DOCTOS_CC.DESCRIPCION (detalle)", det.Cobrador)
	})
}

// TestClientesRepo_Acentos_NotasSiguenSiendoWin1252 es el control del defecto
// INVERSO: CLIENTES.NOTAS es CHARACTER SET NONE de verdad, y "arreglarla"
// pasándola a string plano dejaría bytes crudos en el dominio.
//
// No se siembra la nota: NOTAS es un BLOB del padrón y la base de desarrollo ya
// trae 3,458 filas acentuadas de 20,000 muestreadas. La prueba busca una, la
// lee por el repositorio y exige UTF-8 válido.
func TestClientesRepo_Acentos_NotasSiguenSiendoWin1252(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		// Se recorre el padrón buscando la primera nota con bytes no-ASCII.
		// La detección se hace en Go: un predicado SQL con literal UTF-8 sobre
		// una columna NONE revienta con "Malformed string".
		clienteID, crudos := primeraNotaAcentuada(ctx, t, q)

		if clienteID == 0 {
			t.Skip("ninguna CLIENTES.NOTAS del muestreo trae acentos — no hay control positivo que correr")
		}
		require.Falsef(t, utf8.Valid(crudos),
			"CLIENTES.NOTAS del cliente %d llegó como UTF-8 válido. Si Firebird empezó a "+
				"transliterarla, la columna dejó de ser CHARACTER SET NONE y hay que "+
				"reclasificarla en internal/platform/fbcharset.", clienteID)

		c, err := repo.ObtenerCliente(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Truef(t, utf8.ValidString(c.Notas()),
			"el repositorio debe entregar CLIENTES.NOTAS decodificada: %q", c.Notas())
		assert.NotEmpty(t, c.Notas())
		sinMojibake(t, "CLIENTES.NOTAS", c.Notas())
	})
}

// primeraNotaAcentuada recorre el padrón y devuelve el primer CLIENTES.NOTAS
// con bytes no-ASCII, junto con su CLIENTE_ID. Devuelve (0, nil) si no hay.
//
// La detección se hace en Go y no con un predicado SQL a propósito: NOTAS es
// CHARACTER SET NONE, y un literal UTF-8 en el WHERE fuerza una coerción que
// revienta con "Malformed string" justo en las filas que interesan.
func primeraNotaAcentuada(ctx context.Context, t *testing.T, q firebird.Querier) (int, []byte) {
	t.Helper()
	rows, err := q.QueryContext(ctx,
		`SELECT FIRST 5000 CLIENTE_ID, NOTAS FROM CLIENTES WHERE NOTAS IS NOT NULL`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id int
			b  []byte
		)
		require.NoError(t, rows.Scan(&id, &b))
		if fbtestutil.ContieneNoASCII(string(b)) {
			return id, b
		}
	}
	require.NoError(t, rows.Err())
	return 0, nil
}

// TestClientesRepo_Acentos_FormaCobroEnHistorialDePagos cubre la columna
// FORMA_COBRO de queryPagos, que era la única mina que quedaba sin desactivar.
//
// FORMAS_COBRO.NOMBRE es CHARACTER SET NONE y la expresión era
// `COALESCE((SELECT fc.NOMBRE ...), ”)`. Ese literal ” está en el charset de
// la conexión (UTF-8) y arrastra la expresión entera a UTF-8, así que Firebird
// tiene que coercionar los bytes crudos de la columna: sobre "Crédito" —1 de
// las 4 formas del catálogo— la consulta muere entera con
// "SQL error code = -303 / Malformed string". No una celda vacía: el historial
// de pagos completo devuelve 500.
//
// Nada lo había disparado porque FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID no empata
// con NINGUNA fila de FORMAS_COBRO en la base (157/158/52569/137026 contra
// 67/68/71/27773), así que la subconsulta siempre daba NULL. La siembra crea el
// empate que en producción sólo hace falta que Microsip escriba una vez.
func TestClientesRepo_Acentos_FormaCobroEnHistorialDePagos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		forma := microsipseed.FormaCobroAcentuada(t, q)
		esperado := microsipseed.NombreDeCatalogo(t, q, "FORMAS_COBRO", "FORMA_COBRO_ID", forma)
		require.True(t, fbtestutil.ContieneNoASCII(esperado),
			"la forma de cobro elegida debe traer acentos o la prueba no mide nada")

		clienteID := microsipseed.Cliente(t, q, nombreAcentuado)
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID:  conceptoCobranza,
			Importe:       decimal.NewFromInt(500),
			ConFormaCobro: true,
			FormaCobroID:  forma,
		})

		detalle, err := repo.ObtenerVentaDetalle(ctx, venta.DoctoPVID)
		require.NoError(t, err,
			"el historial de pagos no puede caerse por una forma de cobro acentuada")
		require.Len(t, detalle.Pagos, 1)
		assert.Equal(t, esperado, detalle.Pagos[0].FormaCobro())
		sinMojibake(t, "FORMAS_COBRO.NOMBRE (historial)", detalle.Pagos[0].FormaCobro())

		// El detalle del pago lee la MISMA columna por otra consulta y otro
		// destino de escaneo; los dos canales tienen que coincidir.
		det, err := repo.ObtenerPagoDetalle(ctx, abono.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, esperado, det.FormaCobro)
		sinMojibake(t, "FORMAS_COBRO.NOMBRE (detalle)", det.FormaCobro)
	})
}

// TestClientesRepo_Acentos_TelefonoEsColumnaNONE es el control positivo que le
// faltaba a DIRS_CLIENTES.TELEFONO1, la otra columna donde el cambio fue en
// dirección CONTRARIA (de string plano a firebird.Win1252).
//
// La columna es CHARACTER SET NONE y hoy NINGUNA de sus 43,835 filas trae
// no-ASCII, así que no hay dato en la base que distinga el destino correcto del
// equivocado: cualquier prueba sobre teléfonos reales pasa con los dos. La
// única forma de convertirla en una verificación es sembrar un valor con
// acentos, en bytes Windows-1252, dentro de la transacción que revierte.
//
// El valor es un teléfono absurdo a propósito: lo que se está fijando es el
// charset de la columna, no el formato del dato.
func TestClientesRepo_Acentos_TelefonoEsColumnaNONE(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		const telefonoAcentuado = "238 EXT. ÑOÑO 12"
		require.True(t, fbtestutil.ContieneNoASCII(telefonoAcentuado))

		clienteID := microsipseed.Cliente(t, q, "TELÉFONO NONE PRUEBA "+nombreAcentuado)
		microsipseed.DireccionPrincipal(t, q, clienteID, microsipseed.OpcionesDireccion{
			Telefono: telefonoAcentuado,
		})

		c, err := repo.ObtenerCliente(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, telefonoAcentuado, c.Telefono(),
			"DIRS_CLIENTES.TELEFONO1 es CHARACTER SET NONE: Firebird entrega los bytes "+
				"Windows-1252 crudos y hay que decodificarlos con firebird.Win1252. "+
				"Un string pelado deja UTF-8 inválido en el dominio.")
		sinMojibake(t, "DIRS_CLIENTES.TELEFONO1", c.Telefono())
	})
}
