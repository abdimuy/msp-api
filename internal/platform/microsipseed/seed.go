// Package microsipseed siembra documentos de Microsip sintéticos para las
// pruebas de integración de cualquier módulo (clientes, analytics, rutas,
// reactivación).
//
// POR QUÉ EXISTE
// ==============
// Las pruebas de `clientesfb` y `clienteshttp` verificaban su SQL contra filas
// de PRODUCCIÓN traídas por identificador escrito a mano (cliente 24037, venta
// 4070523, pago 4070588). Eso las ataba a que la base compartida conservara una
// fila concreta, y las hacía fallar en bloque contra la base de pruebas de
// 15 MB, que omite los movimientos (`scripts/db-test-skip-tables.txt`).
//
// Este paquete invierte la dependencia: la prueba CONSTRUYE la venta que va a
// verificar, así que conoce sus valores por construcción y no por observación.
// Todo se escribe dentro de la transacción que `fbtestutil.WithTestTransaction`
// revierte siempre, así que no queda rastro en la base compartida.
//
// CÓMO CONSTRUYE LA VENTA
// =======================
// No inserta el cargo de cuentas por cobrar a mano: inserta el documento de
// punto de venta y voltea APLICADO de 'N' a 'S', que es lo que hace el
// writer real (internal/ventas/infra/microsip/venta_writer.go). La cascada de
// Microsip genera entonces el DOCTOS_CC cargo, el puente DOCTOS_ENTRE_SIS y el
// IMPORTES_DOCTOS_CC del cargo. Sembrar por el mismo camino que la aplicación
// significa que la prueba corre sobre la forma REAL del dato, no sobre una
// imitación que podría divergir.
//
// CERO IDENTIFICADORES DE NEGOCIO ESCRITOS A MANO
// ===============================================
// Cliente, almacén, artículo, caja, cobrador y forma de cobro se eligen del
// CATÁLOGO, y sus nombres se devuelven en la struct para que la prueba compare
// contra el catálogo y no contra una cadena copiada. Los catálogos sobreviven a
// `gbak -skip_data`; los movimientos no.
//
//nolint:misspell // vocabulario español por convención del proyecto.
package microsipseed

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// formaCobroCredito es la forma de cobro con la que Microsip marca una venta a
// crédito en DOCTOS_PV_COBROS. No es un dato de negocio elegible: el SQL del
// repositorio la trae escrita (`FORMA_COBRO_ID = 71` en queries.go) para
// derivar TIPO='CREDITO'. Sembrar otra cosa produciría una venta de contado.
const formaCobroCredito = 71

// sucursal es el identificador de sucursal que usan los demás sembradores de la
// suite (ver internal/cobranza/infra/ventfb/saldos_repo_test.go). DOCTOS_PV y
// DOCTOS_CC lo exigen NOT NULL y no tiene llave foránea que validarlo.
const sucursal = 225490

// Venta reúne los documentos que dejó VentaCredito, con los valores que la
// prueba puede afirmar por construcción.
type Venta struct {
	ClienteID      int
	ClienteNombre  string
	DoctoPVID      int
	CargoCCID      int
	Folio          string
	Fecha          time.Time
	Hora           string
	Total          decimal.Decimal
	AlmacenID      int
	AlmacenNombre  string
	ArticuloID     int
	ArticuloNombre string
}

// OpcionesVenta parametriza VentaCredito. El cero de cada campo tiene un valor
// por omisión razonable, así que una prueba que no le importe la fecha puede
// pasar OpcionesVenta{}.
type OpcionesVenta struct {
	// Fecha del documento. Cero → 2024-03-15.
	Fecha time.Time
	// Hora en formato "HH:MM:SS". Vacío → "18:06:49".
	Hora string
	// Total BRUTO de la venta (con IVA). Cero → 8800.00.
	Total decimal.Decimal
	// PlazoMeses del contrato de crédito. Cero → 12.
	PlazoMeses int
	// Enganche del contrato. Cero → 0.
	Enganche decimal.Decimal
}

// Abono es un documento de abono aplicado a una venta.
type Abono struct {
	DoctoCCID    int
	Folio        string
	Fecha        time.Time
	Importe      decimal.Decimal
	IVA          decimal.Decimal
	ConceptoCCID int
	CobradorID   int
}

// OpcionesAbono parametriza AbonoAplicado.
type OpcionesAbono struct {
	// ConceptoCCID del abono. Obligatorio: es lo que la prueba está clasificando.
	ConceptoCCID int
	// Importe BRUTO del abono (IMPORTE + IVA).
	Importe decimal.Decimal
	// IVA incluido en Importe. Cero → todo el importe va sin impuesto.
	IVA decimal.Decimal
	// Fecha del abono. Cero → la fecha de la venta.
	Fecha time.Time
	// ConCobrador asigna un COBRADOR_ID real del catálogo cuando es true.
	ConCobrador bool
	// ConFormaCobro agrega la fila FORMAS_COBRO_DOCTOS que el detalle de pago lee.
	ConFormaCobro bool
}

// folioCorto arma un folio que cabe en DOCTOS_PV.FOLIO y DOCTOS_CC.FOLIO, que
// son CHAR(9). Se conserva la cola del identificador —no la cabeza— porque es la
// parte que varía entre documentos sembrados en la misma corrida.
func folioCorto(prefijo string, id int) string {
	const ancho = 8
	cola := strconv.Itoa(id)
	if len(cola) > ancho {
		cola = cola[len(cola)-ancho:]
	}
	return prefijo + cola
}

// fechaCalendario prepara un valor para una columna DATE de Microsip
// (DOCTOS_PV.FECHA, DOCTOS_CC.FECHA, IMPORTES_DOCTOS_CC.FECHA).
//
// NO usa firebird.ToWallClock a propósito. ToWallClock convierte el instante a
// la zona del negocio, y sobre una fecha UTC a medianoche eso RETROCEDE el día:
// 2020-07-02T00:00Z se vuelve 2020-07-01 19:00 en hora local, y la columna DATE
// —que no guarda hora— termina con el día anterior. Aquí no hay un instante que
// convertir: hay un día del calendario que se quiere escribir tal cual, así que
// se vuelve a estampar la misma fecha de pared en la zona del negocio.
func fechaCalendario(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, firebird.BusinessTZ())
}

// nuevoID reclama el siguiente valor del generador de identificadores de
// Microsip. Es el mismo que usa el writer de producción.
func nuevoID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var id int
	err := q.QueryRowContext(context.Background(),
		`SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&id)
	require.NoError(t, err, "microsipseed: GEN_ID(ID_DOCTOS)")
	return id
}

// primerID devuelve el identificador más bajo de un catálogo. Se usa para no
// escribir ningún identificador de negocio a mano: cualquier fila sirve, y
// tomar la primera por orden hace la elección determinista.
func primerID(t *testing.T, q firebird.Querier, tabla, columna string) int {
	t.Helper()
	var id int
	//nolint:gosec // tabla/columna son literales del propio paquete, no entrada externa.
	sql := fmt.Sprintf("SELECT FIRST 1 %s FROM %s ORDER BY %s", columna, tabla, columna)
	err := q.QueryRowContext(context.Background(), sql).Scan(&id)
	require.NoErrorf(t, err, "microsipseed: el catálogo %s vino vacío — la base no trae catálogos", tabla)
	return id
}

// nombreCatalogo lee NOMBRE de un catálogo legado. El CAST a WIN1252 es
// obligatorio: estas columnas son CHARACTER SET NONE y leerlas verbatim sobre
// una conexión UTF8 truena en las filas con acentos.
func nombreCatalogo(t *testing.T, q firebird.Querier, tabla, columna string, id int) string {
	t.Helper()
	var nombre string
	//nolint:gosec // tabla/columna son literales del propio paquete, no entrada externa.
	sql := fmt.Sprintf(
		"SELECT CAST(NOMBRE AS VARCHAR(200) CHARACTER SET WIN1252) FROM %s WHERE %s = ?", tabla, columna)
	err := q.QueryRowContext(context.Background(), sql, id).Scan(&nombre)
	require.NoErrorf(t, err, "microsipseed: no se pudo leer %s.NOMBRE para %s=%d", tabla, columna, id)
	return nombre
}

// Cliente inserta un cliente sintético y devuelve su identificador.
//
// El identificador sale del generador de Microsip, así que es único y NO puede
// chocar con un cliente real. Eso es lo que permite que una prueba afirme
// "este cliente tiene exactamente una venta": contra la base de desarrollo
// completa, cualquier cliente real tendría cientos.
func Cliente(t *testing.T, q firebird.Querier, nombre string) int {
	t.Helper()
	return ClienteEnZona(t, q, nombre, PrimeraZona(t, q))
}

// PrimeraZona devuelve la zona que Cliente asigna por omisión. Sale del
// catálogo ZONAS_CLIENTES, no de un identificador escrito a mano.
func PrimeraZona(t *testing.T, q firebird.Querier) int {
	t.Helper()
	return primerID(t, q, "ZONAS_CLIENTES", "ZONA_CLIENTE_ID")
}

// ClienteEnZona inserta un cliente sintético en la zona indicada.
func ClienteEnZona(t *testing.T, q firebird.Querier, nombre string, zona int) int {
	t.Helper()
	id := nuevoID(t, q)
	condPago := primerID(t, q, "CONDICIONES_PAGO", "COND_PAGO_ID")

	_, err := q.ExecContext(context.Background(),
		`INSERT INTO CLIENTES
		  (CLIENTE_ID, NOMBRE, SUJETO_IEPS, DIFERIR_CFDI_COBROS,
		   MONEDA_ID, COND_PAGO_ID, ESTATUS, ZONA_CLIENTE_ID)
		VALUES (?, ?, 'N', FALSE,
		        1, ?, 'A', ?)`,
		id, nombre, condPago, zona)
	require.NoError(t, err, "microsipseed: INSERT CLIENTES")
	return id
}

// VentaCredito siembra una venta a crédito completa y devuelve sus datos.
//
// Deja: DOCTOS_PV (aplicado), una línea DOCTOS_PV_DET ROL='N', el cobro a
// crédito en DOCTOS_PV_COBROS, y —vía la cascada de Microsip— el cargo
// DOCTOS_CC con CONCEPTO_CC_ID=5, el puente DOCTOS_ENTRE_SIS y el importe 'C'.
// Encima agrega el contrato LIBRES_CARGOS_CC que el detalle de venta lee.
func VentaCredito(t *testing.T, q firebird.Querier, clienteID int, o OpcionesVenta) Venta {
	t.Helper()
	v := aplicarDefaults(o)
	v.ClienteID = clienteID
	v.AlmacenID = primerID(t, q, "ALMACENES", "ALMACEN_ID")
	v.AlmacenNombre = nombreCatalogo(t, q, "ALMACENES", "ALMACEN_ID", v.AlmacenID)
	v.DoctoPVID = nuevoID(t, q)
	v.Folio = folioCorto("S", v.DoctoPVID)
	v.ArticuloID = articuloRotativo(t, q, v.DoctoPVID)
	v.ArticuloNombre = nombreCatalogo(t, q, "ARTICULOS", "ARTICULO_ID", v.ArticuloID)

	insertarEncabezadoPV(t, q, &v)
	insertarLineaPV(t, q, &v)
	insertarCobroCredito(t, q, &v)
	aplicarPV(t, q, &v)
	v.CargoCCID = leerCargoDeLaCascada(t, q, v.DoctoPVID)
	insertarContrato(t, q, v, o)
	return v
}

// articuloRotativo elige un artículo distinto para cada venta sembrada.
//
// NO es una preferencia estética: aplicar una venta dispara la cascada de
// inventario de Microsip (GENERA_DOCTO_IN_PV → APLICA_DOCTO_IN → AFECTA_SALDOS_IN),
// que escribe en SALDOS_IN con la llave (ARTICULO_ID, ALMACEN_ID, AÑO, MES). Si
// dos ventas de la misma prueba comparten artículo, almacén y mes, la segunda
// revienta con:
//
//	violation of PRIMARY or UNIQUE KEY constraint SALDOS_IN_PK
//	At procedure 'AFECTA_SALDOS_IN'
//
// Rotar el artículo cambia la llave y elimina la colisión sin obligar a cada
// prueba a repartir sus ventas entre meses distintos.
//
// El índice sale del DOCTO_PV_ID, que ya es único por venta, así que sigue sin
// haber ningún identificador de artículo escrito a mano.
func articuloRotativo(t *testing.T, q firebird.Querier, doctoPVID int) int {
	t.Helper()
	var total int
	err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ARTICULOS WHERE ES_JUEGO = 'N'`).Scan(&total)
	require.NoError(t, err, "microsipseed: contando ARTICULOS")
	require.Positive(t, total, "microsipseed: el catálogo ARTICULOS vino vacío")

	var id int
	err = q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 SKIP ? ARTICULO_ID FROM ARTICULOS WHERE ES_JUEGO = 'N' ORDER BY ARTICULO_ID`,
		doctoPVID%total).Scan(&id)
	require.NoError(t, err, "microsipseed: eligiendo artículo rotativo")
	return id
}

// aplicarDefaults traduce el cero de cada opción a un valor utilizable.
func aplicarDefaults(o OpcionesVenta) Venta {
	v := Venta{Fecha: o.Fecha, Hora: o.Hora, Total: o.Total}
	if v.Fecha.IsZero() {
		v.Fecha = time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	}
	if v.Hora == "" {
		v.Hora = "18:06:49"
	}
	if v.Total.IsZero() {
		v.Total = decimal.NewFromInt(8800)
	}
	return v
}

// neto reparte el total bruto en importe neto e impuesto al 16%.
func neto(total decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	importeNeto := total.Div(decimal.NewFromFloat(1.16)).Round(2)
	return importeNeto, total.Sub(importeNeto)
}

func insertarEncabezadoPV(t *testing.T, q firebird.Querier, v *Venta) {
	t.Helper()
	caja := primerID(t, q, "CAJAS", "CAJA_ID")
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO DOCTOS_PV
		  (DOCTO_PV_ID, CAJA_ID, TIPO_DOCTO, SUCURSAL_ID, FOLIO,
		   FECHA, HORA, CLIENTE_ID, CLAVE_CLIENTE,
		   ALMACEN_ID, MONEDA_ID, IMPUESTO_INCLUIDO, TIPO_CAMBIO,
		   TIPO_DSCTO, DSCTO_PCTJE, DSCTO_IMPORTE, ESTATUS, APLICADO,
		   IMPORTE_NETO, TOTAL_IMPUESTOS, SISTEMA_ORIGEN,
		   CONTABILIZADO, TICKET_EMITIDO, CARGAR_SUN, UNID_COMPROM)
		VALUES (?, ?, 'V', ?, ?,
		        ?, ?, ?, '0001',
		        ?, 1, 'S', 1,
		        'P', 0, 0, 'N', 'N',
		        0, 0, 'PV',
		        'N', 'N', 'S', 'N')`,
		v.DoctoPVID, caja, sucursal, v.Folio,
		fechaCalendario(v.Fecha), v.Hora, v.ClienteID,
		v.AlmacenID)
	require.NoError(t, err, "microsipseed: INSERT DOCTOS_PV")
}

func insertarLineaPV(t *testing.T, q firebird.Querier, v *Venta) {
	t.Helper()
	importeNeto, _ := neto(v.Total)
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO DOCTOS_PV_DET
		  (DOCTO_PV_DET_ID, DOCTO_PV_ID, ARTICULO_ID, CLAVE_ARTICULO,
		   UNIDADES, UNIDADES_DEV, PRECIO_UNITARIO, PRECIO_UNITARIO_IMPTO,
		   PCTJE_DSCTO, PRECIO_TOTAL_NETO, PRECIO_MODIFICADO, PCTJE_COMIS,
		   ROL, POSICION, TIPO_CONTAB_UNID, ES_TRAN_ELECT, IMPUESTO_POR_UNIDAD)
		VALUES (?, ?, ?, ?,
		        1, 0, ?, ?,
		        0, ?, 'N', 0,
		        'N', -1, '0', 'N', 0)`,
		nuevoID(t, q), v.DoctoPVID, v.ArticuloID, strconv.Itoa(v.ArticuloID),
		importeNeto, v.Total, importeNeto)
	require.NoError(t, err, "microsipseed: INSERT DOCTOS_PV_DET")
}

func insertarCobroCredito(t *testing.T, q firebird.Querier, v *Venta) {
	t.Helper()
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO DOCTOS_PV_COBROS
		  (DOCTO_PV_COBRO_ID, DOCTO_PV_ID, TIPO, FORMA_COBRO_ID,
		   IMPORTE, IMPORTE_MON_DOC, TIPO_CAMBIO)
		VALUES (?, ?, 'C', ?, ?, ?, 1)`,
		nuevoID(t, q), v.DoctoPVID, formaCobroCredito, v.Total, v.Total)
	require.NoError(t, err, "microsipseed: INSERT DOCTOS_PV_COBROS")
}

// aplicarPV voltea APLICADO de 'N' a 'S'. Ese cambio es el que dispara la
// cascada de Microsip que genera el cargo en cuentas por cobrar.
func aplicarPV(t *testing.T, q firebird.Querier, v *Venta) {
	t.Helper()
	importeNeto, impuestos := neto(v.Total)
	_, err := q.ExecContext(context.Background(),
		`UPDATE DOCTOS_PV
		 SET APLICADO = 'S', IMPORTE_NETO = ?, TOTAL_IMPUESTOS = ?
		 WHERE DOCTO_PV_ID = ?`,
		importeNeto, impuestos, v.DoctoPVID)
	require.NoError(t, err, "microsipseed: UPDATE DOCTOS_PV aplicado")
}

// leerCargoDeLaCascada devuelve el DOCTO_CC_ID que Microsip generó para la
// venta. Si viene vacío, la cascada no corrió y sembrar más sería inútil.
func leerCargoDeLaCascada(t *testing.T, q firebird.Querier, doctoPVID int) int {
	t.Helper()
	var cargoID int
	err := q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 d.DOCTO_CC_ID
		 FROM DOCTOS_ENTRE_SIS e
		 JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = e.DOCTO_DEST_ID
		 WHERE e.CLAVE_SIS_FTE = 'PV' AND e.CLAVE_SIS_DEST = 'CC'
		   AND e.DOCTO_FTE_ID = ?`, doctoPVID).Scan(&cargoID)
	require.NoErrorf(t, err,
		"microsipseed: la cascada de Microsip no generó el cargo CC de la venta %d — "+
			"sin cargo no hay saldo, ni abonos que aplicar, ni contrato", doctoPVID)
	return cargoID
}

// insertarContrato agrega la fila LIBRES_CARGOS_CC que el detalle de venta lee
// para plazo, parcialidad y enganche.
func insertarContrato(t *testing.T, q firebird.Querier, v Venta, o OpcionesVenta) {
	t.Helper()
	plazo := o.PlazoMeses
	if plazo == 0 {
		plazo = 12
	}
	// PARCIALIDAD es SMALLINT y MONTO_A_CORTO_PLAZO es NUMERIC(5,0): ninguna
	// admite decimales, y pasarles un decimal escalado revienta con "numeric
	// value is out of range". Se redondean a entero a propósito.
	parcialidad := v.Total.Div(decimal.NewFromInt(int64(plazo))).IntPart()
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO LIBRES_CARGOS_CC
		  (DOCTO_CC_ID, PARCIALIDAD, CREDITO_EN_MESES,
		   TIEMPO_A_CORTO_PLAZOMESES, MONTO_A_CORTO_PLAZO,
		   NUMERO_DE_VENDEDORES, ENGANCHE, PRECIO_DE_CONTADO)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		v.CargoCCID, parcialidad, plazo,
		plazo, v.Total.IntPart(), o.Enganche, v.Total)
	require.NoError(t, err, "microsipseed: INSERT LIBRES_CARGOS_CC")
}

// AbonoAplicado siembra un documento de abono aplicado al cargo de la venta.
//
// Deja el DOCTOS_CC con NATURALEZA_CONCEPTO='R' y su IMPORTES_DOCTOS_CC con
// TIPO_IMPTE='R' apuntando al cargo — que es exactamente la forma en que el
// repositorio localiza los pagos de una venta.
func AbonoAplicado(t *testing.T, q firebird.Querier, v Venta, o OpcionesAbono) Abono {
	t.Helper()
	a := Abono{
		DoctoCCID:    nuevoID(t, q),
		Fecha:        o.Fecha,
		Importe:      o.Importe,
		IVA:          o.IVA,
		ConceptoCCID: o.ConceptoCCID,
	}
	if a.Fecha.IsZero() {
		a.Fecha = v.Fecha
	}
	a.Folio = folioCorto("A", a.DoctoCCID)
	if o.ConCobrador {
		a.CobradorID = primerID(t, q, "COBRADORES", "COBRADOR_ID")
	}

	insertarAbonoCC(t, q, v, &a)
	insertarImporteAbono(t, q, v, a)
	if o.ConFormaCobro {
		insertarFormaCobro(t, q, a)
	}
	return a
}

func insertarAbonoCC(t *testing.T, q firebird.Querier, v Venta, a *Abono) {
	t.Helper()
	var cobrador *int
	if a.CobradorID != 0 {
		cobrador = &a.CobradorID
	}
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE, COBRADOR_ID,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, ?, ?, 'R',
		        ?, ?, ?, '0001', ?,
		        1, 'Abono sembrado por microsipseed',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		a.DoctoCCID, a.ConceptoCCID, a.Folio,
		sucursal, fechaCalendario(a.Fecha), v.ClienteID, cobrador)
	require.NoError(t, err, "microsipseed: INSERT DOCTOS_CC abono")
}

func insertarImporteAbono(t *testing.T, q firebird.Querier, v Venta, a Abono) {
	t.Helper()
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, ?, ?, 'S', 'N', 'N')`,
		nuevoID(t, q), a.DoctoCCID, fechaCalendario(a.Fecha),
		v.CargoCCID, a.Importe.Sub(a.IVA), a.IVA)
	require.NoError(t, err, "microsipseed: INSERT IMPORTES_DOCTOS_CC abono")
}

// insertarFormaCobro agrega la fila que el detalle de pago lee para nombrar la
// forma de cobro. La forma se toma del catálogo, no de un identificador escrito.
func insertarFormaCobro(t *testing.T, q firebird.Querier, a Abono) {
	t.Helper()
	forma := primerID(t, q, "FORMAS_COBRO", "FORMA_COBRO_ID")
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO FORMAS_COBRO_DOCTOS
		  (FORMA_COBRO_DOC_ID, NOM_TABLA_DOCTOS, DOCTO_ID,
		   FORMA_COBRO_ID, CLAVE_SIS_FORMA_COB, IMPORTE)
		VALUES (?, 'DOCTOS_CC', ?, ?, 'CC', ?)`,
		nuevoID(t, q), a.DoctoCCID, forma, a.Importe)
	require.NoError(t, err, "microsipseed: INSERT FORMAS_COBRO_DOCTOS")
}

// NombreFormaCobro devuelve el nombre de la forma de cobro que insertarFormaCobro
// usa, para que la prueba lo compare contra el catálogo en vez de contra una
// cadena copiada.
func NombreFormaCobro(t *testing.T, q firebird.Querier) string {
	t.Helper()
	return nombreCatalogo(t, q, "FORMAS_COBRO", "FORMA_COBRO_ID",
		primerID(t, q, "FORMAS_COBRO", "FORMA_COBRO_ID"))
}

// NombreConcepto devuelve el nombre que el catálogo CONCEPTOS_CC le da a un
// concepto. Sirve para que una prueba afirme sobre el nombre sin copiarlo.
func NombreConcepto(t *testing.T, q firebird.Querier, conceptoCCID int) string {
	t.Helper()
	return nombreCatalogo(t, q, "CONCEPTOS_CC", "CONCEPTO_CC_ID", conceptoCCID)
}

// NombreCobrador devuelve el nombre del cobrador que AbonoAplicado asigna
// cuando ConCobrador es true.
func NombreCobrador(t *testing.T, q firebird.Querier, cobradorID int) string {
	t.Helper()
	return nombreCatalogo(t, q, "COBRADORES", "COBRADOR_ID", cobradorID)
}

// OpcionesDireccion parametriza DireccionPrincipal.
type OpcionesDireccion struct {
	// CiudadID de DIRS_CLIENTES. Cero → la primera del catálogo CIUDADES.
	CiudadID int
	// Telefono1. Vacío → un número sintético de 10 dígitos.
	Telefono string
}

// DireccionPrincipal inserta la dirección principal (ES_DIR_PPAL='S') de un
// cliente sembrado y devuelve la ciudad y el teléfono que quedaron.
//
// Hace falta para las consultas que segmentan por ciudad y exigen teléfono
// —el universo de reactivación, por ejemplo—, que hasta ahora sólo podían
// probarse contra los domicilios y teléfonos REALES de DIRS_CLIENTES.
func DireccionPrincipal(t *testing.T, q firebird.Querier, clienteID int, o OpcionesDireccion) (int, string) {
	t.Helper()
	ciudad := o.CiudadID
	if ciudad == 0 {
		ciudad = primerID(t, q, "CIUDADES", "CIUDAD_ID")
	}
	telefono := o.Telefono
	if telefono == "" {
		telefono = "2381234567"
	}

	_, err := q.ExecContext(context.Background(),
		`INSERT INTO DIRS_CLIENTES
		  (DIR_CLI_ID, CLIENTE_ID, NOMBRE_CONSIG, ES_DIR_PPAL,
		   CIUDAD_ID, TELEFONO1)
		VALUES (?, ?, 'DOMICILIO PRUEBA', 'S', ?, ?)`,
		nuevoID(t, q), clienteID, ciudad, telefono)
	require.NoError(t, err, "microsipseed: INSERT DIRS_CLIENTES")
	return ciudad, telefono
}

// RequiereCiudad falla si la ciudad no está en el catálogo CIUDADES.
func RequiereCiudad(t *testing.T, q firebird.Querier, ciudadID int) {
	t.Helper()
	var n int
	err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM CIUDADES WHERE CIUDAD_ID = ?`, ciudadID).Scan(&n)
	require.NoError(t, err, "microsipseed: consultando CIUDADES")
	require.NotZerof(t, n, "microsipseed: la ciudad %d no existe en el catálogo CIUDADES", ciudadID)
}

// RequiereConceptos falla si alguno de los conceptos no está en CONCEPTOS_CC.
//
// DOCTOS_CC.CONCEPTO_CC_ID no tiene llave foránea, así que sembrar un concepto
// inexistente NO truena: deja el nombre en NULL y la prueba falla después con un
// mensaje que no señala la causa. Este guardián la señala.
func RequiereConceptos(t *testing.T, q firebird.Querier, ids ...int) {
	t.Helper()
	for _, id := range ids {
		var n int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM CONCEPTOS_CC WHERE CONCEPTO_CC_ID = ?`, id).Scan(&n)
		require.NoError(t, err, "microsipseed: consultando CONCEPTOS_CC")
		require.NotZerof(t, n,
			"microsipseed: el concepto %d no existe en CONCEPTOS_CC. La prueba lo siembra "+
				"porque el clasificador del dominio (clientes/domain/categoria.go) le asigna una "+
				"categoría; si Microsip lo retiró del catálogo, hay que revisar ese mapeo.", id)
	}
}
