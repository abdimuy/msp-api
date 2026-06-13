//nolint:misspell // Spanish SQL column names (ANUAL, RESPONSABLE, PRODUCTOS, DESCRIPCION) by convention.
package ventfb

// ─── Venta (header) ─────────────────────────────────────────────────────────

const ventaColumns = `ID, NOMBRE_CLIENTE, TELEFONO, AVAL_O_RESPONSABLE,
    CALLE, NUMERO_EXTERIOR, COLONIA, POBLACION, CIUDAD, ZONA_CLIENTE_ID,
    LATITUD, LONGITUD,
    FECHA_VENTA, TIPO_VENTA,
    MONTO_ANUAL, MONTO_CORTO_PLAZO, MONTO_CONTADO,
    PLAZO_MESES, ENGANCHE, PARCIALIDAD,
    FREC_PAGO, DIA_COBRANZA_SEMANA, DIA_COBRANZA_MES,
    NOTA,
    CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY,
    CANCELED_AT, CANCELED_BY, CANCEL_REASON,
    CLIENTE_ID, STATUS, APPROVED_AT, APPROVED_BY,
    SITUACION, SINCRONIZACION,
    MICROSIP_DOCTO_PV_ID, MICROSIP_FOLIO, MICROSIP_APLICADA_AT,
    CLIENTE_REFERENCIA`

const insertVenta = `
INSERT INTO MSP_VENTAS
    (ID, NOMBRE_CLIENTE, TELEFONO, AVAL_O_RESPONSABLE,
     CALLE, NUMERO_EXTERIOR, COLONIA, POBLACION, CIUDAD, ZONA_CLIENTE_ID,
     LATITUD, LONGITUD,
     FECHA_VENTA, TIPO_VENTA,
     MONTO_ANUAL, MONTO_CORTO_PLAZO, MONTO_CONTADO,
     PLAZO_MESES, ENGANCHE, PARCIALIDAD,
     FREC_PAGO, DIA_COBRANZA_SEMANA, DIA_COBRANZA_MES,
     NOTA,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY,
     CANCELED_AT, CANCELED_BY, CANCEL_REASON,
     CLIENTE_ID, STATUS, APPROVED_AT, APPROVED_BY,
     SITUACION, SINCRONIZACION,
     MICROSIP_DOCTO_PV_ID, MICROSIP_FOLIO, MICROSIP_APLICADA_AT,
     CLIENTE_REFERENCIA)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// updateVentaHeader writes back the full state-machine surface of a venta:
// the three lifecycle dimensions (STATUS/SITUACION/SINCRONIZACION), the
// Microsip materialization artifacts, the cancelación and aprobación triplets,
// and the audit fields. Every transition command (Cancelar, EnviarARevision,
// Aprobar, RegresarABorrador, AplicarVenta) persists through this statement.
// Field-level edits go through the dedicated update statements below.
const updateVentaHeader = `
UPDATE MSP_VENTAS
SET STATUS               = ?,
    SITUACION            = ?,
    SINCRONIZACION       = ?,
    MICROSIP_DOCTO_PV_ID = ?,
    MICROSIP_FOLIO       = ?,
    MICROSIP_APLICADA_AT = ?,
    CANCELED_AT          = ?,
    CANCELED_BY          = ?,
    CANCEL_REASON        = ?,
    APPROVED_AT          = ?,
    APPROVED_BY          = ?,
    UPDATED_AT           = ?,
    UPDATED_BY           = ?
WHERE ID = ?`

// updateVentaHeaderFull rewrites the editable header fields used by
// ActualizarHeader. TIPO_VENTA, CLIENTE_ID and STATUS are NOT touched here.
const updateVentaHeaderFull = `
UPDATE MSP_VENTAS
SET CALLE                = ?,
    NUMERO_EXTERIOR      = ?,
    COLONIA              = ?,
    POBLACION            = ?,
    CIUDAD               = ?,
    ZONA_CLIENTE_ID      = ?,
    LATITUD              = ?,
    LONGITUD             = ?,
    FECHA_VENTA          = ?,
    MONTO_ANUAL          = ?,
    MONTO_CORTO_PLAZO    = ?,
    MONTO_CONTADO        = ?,
    PLAZO_MESES          = ?,
    ENGANCHE             = ?,
    PARCIALIDAD          = ?,
    FREC_PAGO            = ?,
    DIA_COBRANZA_SEMANA  = ?,
    DIA_COBRANZA_MES     = ?,
    NOTA                 = ?,
    UPDATED_AT           = ?,
    UPDATED_BY           = ?
WHERE ID = ?`

// updateVentaCliente updates the cliente snapshot + cliente_id link.
const updateVentaCliente = `
UPDATE MSP_VENTAS
SET CLIENTE_ID          = ?,
    NOMBRE_CLIENTE      = ?,
    TELEFONO            = ?,
    AVAL_O_RESPONSABLE  = ?,
    CLIENTE_REFERENCIA  = ?,
    UPDATED_AT          = ?,
    UPDATED_BY          = ?
WHERE ID = ?`

const selectVentaByID = `
SELECT ` + ventaColumns + `
FROM MSP_VENTAS WHERE ID = ?`

// lockVentaByID takes a pessimistic row lock on a single MSP_VENTAS row.
// Used by AplicarVenta to serialize concurrent materialization attempts on the
// same venta: a second transaction blocks here until the first commits/rolls
// back, then re-reads and sees SINCRONIZACION='aplicada' (idempotent return).
const lockVentaByID = `SELECT 1 FROM MSP_VENTAS WHERE ID = ? WITH LOCK`

// ─── Combo ──────────────────────────────────────────────────────────────────

const comboColumns = `ID, NOMBRE_COMBO,
    PRECIO_ANUAL, PRECIO_CORTO_PLAZO, PRECIO_CONTADO,
    CANTIDAD, ALMACEN_ORIGEN_ID, ALMACEN_DESTINO_ID,
    CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const insertCombo = `
INSERT INTO MSP_VENTAS_COMBOS
    (ID, VENTA_ID, NOMBRE_COMBO,
     PRECIO_ANUAL, PRECIO_CORTO_PLAZO, PRECIO_CONTADO,
     CANTIDAD, ALMACEN_ORIGEN_ID, ALMACEN_DESTINO_ID,
     POSICION,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// selectCombosByVenta orders by POSICION (1,2,3…) — the explicit insertion
// order stamped by the app at INSERT time. CREATED_AT is identical across
// children of one venta (same transaction), so it cannot be the sort key.
const selectCombosByVenta = `
SELECT ` + comboColumns + `
FROM MSP_VENTAS_COMBOS
WHERE VENTA_ID = ?
ORDER BY POSICION`

const deleteCombosByVenta = `DELETE FROM MSP_VENTAS_COMBOS WHERE VENTA_ID = ?`

// ─── Producto ───────────────────────────────────────────────────────────────

const productoColumns = `ID, ARTICULO_ID, ARTICULO, CANTIDAD,
    PRECIO_ANUAL, PRECIO_CORTO_PLAZO, PRECIO_CONTADO,
    COMBO_ID, ALMACEN_ORIGEN_ID, ALMACEN_DESTINO_ID,
    CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const insertProducto = `
INSERT INTO MSP_VENTAS_PRODUCTOS
    (ID, VENTA_ID, ARTICULO_ID, ARTICULO, CANTIDAD,
     PRECIO_ANUAL, PRECIO_CORTO_PLAZO, PRECIO_CONTADO,
     COMBO_ID, ALMACEN_ORIGEN_ID, ALMACEN_DESTINO_ID,
     POSICION,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// selectProductosByVenta orders by POSICION — see selectCombosByVenta.
const selectProductosByVenta = `
SELECT ` + productoColumns + `
FROM MSP_VENTAS_PRODUCTOS
WHERE VENTA_ID = ?
ORDER BY POSICION`

const deleteProductosByVenta = `DELETE FROM MSP_VENTAS_PRODUCTOS WHERE VENTA_ID = ?`

// ─── Vendedor ───────────────────────────────────────────────────────────────

const vendedorColumns = `ID, VENDEDOR_USUARIO_ID, VENDEDOR_EMAIL, VENDEDOR_NOMBRE,
    CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const insertVendedor = `
INSERT INTO MSP_VENTAS_VENDEDORES
    (ID, VENTA_ID, VENDEDOR_USUARIO_ID, VENDEDOR_EMAIL, VENDEDOR_NOMBRE,
     POSICION,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// selectVendedoresByVenta orders by POSICION — see selectCombosByVenta.
const selectVendedoresByVenta = `
SELECT ` + vendedorColumns + `
FROM MSP_VENTAS_VENDEDORES
WHERE VENTA_ID = ?
ORDER BY POSICION`

const deleteVendedoresByVenta = `DELETE FROM MSP_VENTAS_VENDEDORES WHERE VENTA_ID = ?`

// touchVentaHeaderMontos syncs the derived montos and the audit trail on
// MSP_VENTAS after a child-collection replacement (productos, combos, or
// vendedores). The three monto columns mirror the values recomputed by
// domain.Venta.recomputarMontos() so the header row stays consistent with the
// current child rows.
const touchVentaHeaderMontos = `
UPDATE MSP_VENTAS
SET MONTO_ANUAL        = ?,
    MONTO_CORTO_PLAZO  = ?,
    MONTO_CONTADO      = ?,
    UPDATED_AT         = ?,
    UPDATED_BY         = ?
WHERE ID = ?`

// ─── Imagen ─────────────────────────────────────────────────────────────────

const imagenColumns = `ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
    CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const insertImagen = `
INSERT INTO MSP_VENTAS_IMAGENES
    (ID, VENTA_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const deleteImagen = `
DELETE FROM MSP_VENTAS_IMAGENES
WHERE ID = ? AND VENTA_ID = ?`

const selectImagenesByVenta = `
SELECT ` + imagenColumns + `
FROM MSP_VENTAS_IMAGENES
WHERE VENTA_ID = ?
ORDER BY CREATED_AT, ID`

// ─── List ───────────────────────────────────────────────────────────────────
//
// The List queries are built dynamically because every filter is optional.
// The base SELECT/ORDER BY clauses live here as constants; the WHERE chain is
// composed in venta_repo.go from these fragments.

const selectVentasBase = `SELECT FIRST ? ` + ventaColumns + `
FROM MSP_VENTAS v `

// orderClause is the canonical (FECHA_VENTA DESC, ID) ordering for paginated
// ventas. The cursor encodes (fecha_venta, id) of the last item on the page.
const orderClause = `ORDER BY v.FECHA_VENTA DESC, v.ID`

// cursorPredicateDesc is the strict-less-than tuple comparison used in the
// after-cursor query for a DESC-ordered list. We want rows that come AFTER
// the cursor in the (FECHA_VENTA DESC, ID ASC) ordering, i.e. either an
// earlier FECHA_VENTA or the same FECHA_VENTA with a strictly larger ID.
const cursorPredicateDesc = `(v.FECHA_VENTA < ?) OR (v.FECHA_VENTA = ? AND v.ID > ?)`

// ─── Cliente existence ─────────────────────────────────────────────────────

const selectClienteExists = `SELECT FIRST 1 1 FROM CLIENTES WHERE CLIENTE_ID = ?`
