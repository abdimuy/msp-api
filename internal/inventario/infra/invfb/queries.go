//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

// ─── DOCTOS_IN (traspaso header) ─────────────────────────────────────────────

// insertDoctoIn inserts a traspaso header into Microsip's DOCTOS_IN table.
// DOCTO_IN_ID = -1 causes the Firebird trigger to generate the real id via
// GEN_DOCTOS_IN_ID; RETURNING gives us that value back.
//
//nolint:gosec,dupword // SQL constant; NULL repetition is intentional SQL syntax.
const insertDoctoIn = `INSERT INTO DOCTOS_IN (
  DOCTO_IN_ID, ALMACEN_ID, CONCEPTO_IN_ID, SUCURSAL_ID,
  FOLIO, NATURALEZA_CONCEPTO, FECHA, ALMACEN_DESTINO_ID,
  CENTRO_COSTO_ID, CANCELADO, APLICADO, DESCRIPCION,
  CUENTA_CONCEPTO, FORMA_EMITIDA, CONTABILIZADO, SISTEMA_ORIGEN,
  USUARIO_CREADOR, FECHA_HORA_CREACION, USUARIO_AUT_CREACION,
  USUARIO_ULT_MODIF, FECHA_HORA_ULT_MODIF, USUARIO_AUT_MODIF,
  USUARIO_CANCELACION, FECHA_HORA_CANCELACION, USUARIO_AUT_CANCELACION
) VALUES (
  -1, ?, ?, ?,
  ?, ?, ?, ?,
  NULL, ?, ?, ?,
  NULL, ?, ?, ?,
  ?, ?, NULL,
  ?, ?, NULL,
  NULL, NULL, NULL
) RETURNING DOCTO_IN_ID`

// insertDoctoInDet inserts one detail line (salida or entrada) into DOCTOS_IN_DET.
// DOCTO_IN_DET_ID = -1; RETURNING gives us the assigned id.
//
//nolint:gosec // SQL constant, not user input.
const insertDoctoInDet = `INSERT INTO DOCTOS_IN_DET (
  DOCTO_IN_DET_ID, DOCTO_IN_ID, ALMACEN_ID, CONCEPTO_IN_ID,
  CLAVE_ARTICULO, ARTICULO_ID, TIPO_MOVTO, UNIDADES,
  COSTO_UNITARIO, COSTO_TOTAL, METODO_COSTEO, CANCELADO,
  APLICADO, COSTEO_PEND, PEDIMENTO_PEND, ROL, FECHA, CENTRO_COSTO_ID
) VALUES (
  -1, ?, ?, ?,
  ?, ?, ?, ?,
  0, 0, ?, ?,
  ?, ?, ?, ?, ?, NULL
) RETURNING DOCTO_IN_DET_ID`

// insertSubMovtoIn links a salida detalle to its corresponding entrada detalle
// (and vice-versa). Two rows are inserted per detalle pair.
const insertSubMovtoIn = `INSERT INTO SUB_MOVTOS_IN (DOCTO_IN_DET_ID, SUB_MOVTO_ID) VALUES (?, ?)`

// executeAplicaDoctoIn runs the Microsip stored procedure that updates
// SALDOS_IN (decrements almacen_origen, increments almacen_destino).
const executeAplicaDoctoIn = `EXECUTE PROCEDURE aplica_docto_in(?)`

// ─── MSP_VENTAS_TRASPASOS (lookup table) ─────────────────────────────────────

//nolint:gosec // SQL constant, not user input.
const insertVentaTraspaso = `INSERT INTO MSP_VENTAS_TRASPASOS (
  ID, VENTA_ID, DOCTO_IN_ID, TIPO, FOLIO,
  ALMACEN_ORIGEN, ALMACEN_DESTINO, CREATED_AT, CREATED_BY
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

// ─── Clave articulo lookup ────────────────────────────────────────────────────

// selectClaveArticulo looks up the canonical display key for an articulo.
// We match the legacy Node API: filter by ROL_CLAVE_ART_ID = 17.
const selectClaveArticulo = `SELECT FIRST 1 CART.CLAVE_ARTICULO
FROM CLAVES_ARTICULOS CART
WHERE CART.ROL_CLAVE_ART_ID = ? AND CART.ARTICULO_ID = ?
ORDER BY CART.CLAVE_ARTICULO_ID`

// ─── DOCTOS_IN reader ─────────────────────────────────────────────────────────

// selectDoctoInByID reads a single DOCTOS_IN header for CONCEPTO_IN_ID = 36.
const selectDoctoInByID = `SELECT D.DOCTO_IN_ID, D.ALMACEN_ID, D.ALMACEN_DESTINO_ID,
  D.FOLIO, D.FECHA, D.DESCRIPCION, D.FECHA_HORA_CREACION, D.USUARIO_CREADOR
FROM DOCTOS_IN D
WHERE D.DOCTO_IN_ID = ? AND D.CONCEPTO_IN_ID = 36`

// selectDoctoInByIDs reads multiple DOCTOS_IN headers; caller builds the IN(…)
// clause dynamically from the id list.
const selectDoctoInByIDsBase = `SELECT D.DOCTO_IN_ID, D.ALMACEN_ID, D.ALMACEN_DESTINO_ID,
  D.FOLIO, D.FECHA, D.DESCRIPCION, D.FECHA_HORA_CREACION, D.USUARIO_CREADOR
FROM DOCTOS_IN D
WHERE D.DOCTO_IN_ID IN `

// selectDoctoInDetBySalidaID reads the salida ('S') detail lines for a set of
// DOCTO_IN_IDs; caller appends the IN(…) clause dynamically.
const selectDoctoInDetSalidaBase = `SELECT DET.DOCTO_IN_DET_ID, DET.DOCTO_IN_ID,
  DET.ARTICULO_ID, DET.UNIDADES
FROM DOCTOS_IN_DET DET
WHERE DET.TIPO_MOVTO = 'S' AND DET.DOCTO_IN_ID IN `

// selectVentaTraspasoIDsByVenta returns all DOCTO_IN_IDs associated with a venta.
const selectVentaTraspasoIDsByVenta = `SELECT DOCTO_IN_ID
FROM MSP_VENTAS_TRASPASOS
WHERE VENTA_ID = ?
ORDER BY DOCTO_IN_ID`

// selectVentaTraspasoRowByDoctoIn reads back the lookup row for a given
// DOCTO_IN_ID to reconstruct the full aggregate (VENTA_ID + TIPO).
const selectVentaTraspasoRowByDoctoIn = `SELECT VENTA_ID, TIPO
FROM MSP_VENTAS_TRASPASOS
WHERE DOCTO_IN_ID = ?`

// ─── SALDOS_IN (existencia queries) ──────────────────────────────────────────

// selectExistencia returns the net stock for one article in one warehouse.
const selectExistencia = `SELECT CAST(COALESCE(SUM(ENTRADAS_UNIDADES - SALIDAS_UNIDADES), 0) AS NUMERIC(18,5))
FROM SALDOS_IN
WHERE ALMACEN_ID = ? AND ARTICULO_ID = ?`

// selectExistenciasPorAlmacen returns stock for every article in a warehouse.
const selectExistenciasPorAlmacen = `SELECT ARTICULO_ID,
  CAST(COALESCE(SUM(ENTRADAS_UNIDADES - SALIDAS_UNIDADES), 0) AS NUMERIC(18,5)) AS EXISTENCIA
FROM SALDOS_IN
WHERE ALMACEN_ID = ?
GROUP BY ARTICULO_ID`

// ─── GEN_MST_FOLIO (folio minter) ────────────────────────────────────────────

// selectNextFolio claims the next value from the shared Microsip folio generator.
const selectNextFolio = `SELECT GEN_ID(GEN_MST_FOLIO, 1) AS NEXT_VAL FROM RDB$DATABASE`

// ─── ALMACENES ────────────────────────────────────────────────────────────────

const selectAlmacenByID = `SELECT ALMACEN_ID, NOMBRE FROM ALMACENES WHERE ALMACEN_ID = ?`

const selectAllAlmacenes = `SELECT ALMACEN_ID, NOMBRE FROM ALMACENES ORDER BY NOMBRE`
