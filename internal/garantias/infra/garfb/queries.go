//nolint:misspell // DESCRIPCION is the actual Firebird column name.
package garfb

const insertarGarantiaSQL = `
INSERT INTO MSP_GA_GARANTIA (
	ID,
	FOLIO,
	ORIGEN,
	CLIENTE_ID,
	VENTA_ID,
	ESTADO_CUENTA,
	ESTADO,
	DESCRIPCION,
	VIGENCIA_HASTA,
	CALLE,
	NUMERO_EXTERIOR,
	COLONIA,
	LOCALIDAD,
	CIUDAD,
	CODIGO_POSTAL,
	GPS_LAT,
	GPS_LON,
	ABIERTO_POR,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)`

const actualizarGarantiaSQL = `
UPDATE MSP_GA_GARANTIA
SET
	ESTADO = ?,
	CERRADO_EN = ?,
	UPDATED_AT = ?
WHERE ID = ?`

const obtenerGarantiaSQL = `
SELECT
	ID,
	FOLIO,
	ORIGEN,
	CLIENTE_ID,
	VENTA_ID,
	ESTADO_CUENTA,
	ESTADO,
	DESCRIPCION,
	VIGENCIA_HASTA,
	CALLE,
	NUMERO_EXTERIOR,
	COLONIA,
	LOCALIDAD,
	CIUDAD,
	CODIGO_POSTAL,
	GPS_LAT,
	GPS_LON,
	ABIERTO_POR,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT
FROM MSP_GA_GARANTIA
WHERE ID = ?`

const obtenerGarantiaPorFolioSQL = `
SELECT
	ID,
	FOLIO,
	ORIGEN,
	CLIENTE_ID,
	VENTA_ID,
	ESTADO_CUENTA,
	ESTADO,
	DESCRIPCION,
	VIGENCIA_HASTA,
	CALLE,
	NUMERO_EXTERIOR,
	COLONIA,
	LOCALIDAD,
	CIUDAD,
	CODIGO_POSTAL,
	GPS_LAT,
	GPS_LON,
	ABIERTO_POR,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT
FROM MSP_GA_GARANTIA
WHERE FOLIO = ?`

const obtenerGarantiaParaActualizarSQL = `
SELECT
	ID,
	FOLIO,
	ORIGEN,
	CLIENTE_ID,
	VENTA_ID,
	ESTADO_CUENTA,
	ESTADO,
	DESCRIPCION,
	VIGENCIA_HASTA,
	CALLE,
	NUMERO_EXTERIOR,
	COLONIA,
	LOCALIDAD,
	CIUDAD,
	CODIGO_POSTAL,
	GPS_LAT,
	GPS_LON,
	ABIERTO_POR,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT
FROM MSP_GA_GARANTIA
WHERE ID = ?
WITH LOCK`

const insertarArticuloSQL = `
INSERT INTO MSP_GA_ARTICULO (
	ID,
	GARANTIA_ID,
	ROL,
	ARTICULO_ID,
	CLAVE,
	DESCRIPCION,
	RUTA,
	ETAPA,
	UBICACION,
	DICTAMEN,
	DESENLACE,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT,
	REEMPLAZA_A
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)`

const actualizarArticuloSQL = `
UPDATE MSP_GA_ARTICULO
SET
	RUTA = ?,
	ETAPA = ?,
	UBICACION = ?,
	DICTAMEN = ?,
	DESENLACE = ?,
	CERRADO_EN = ?,
	UPDATED_AT = ?
WHERE ID = ? AND GARANTIA_ID = ?`

const listarArticulosPorGarantiaSQL = `
SELECT
	ID,
	GARANTIA_ID,
	ROL,
	ARTICULO_ID,
	CLAVE,
	DESCRIPCION,
	RUTA,
	ETAPA,
	UBICACION,
	DICTAMEN,
	DESENLACE,
	CERRADO_EN,
	CREATED_AT,
	UPDATED_AT,
	REEMPLAZA_A
FROM MSP_GA_ARTICULO
WHERE GARANTIA_ID = ?
ORDER BY CREATED_AT, ID`

const insertarEventoSQL = `
INSERT INTO MSP_GA_EVENTO (
	ID,
	GARANTIA_ID,
	ARTICULO_REF,
	TIPO,
	DESCRIPCION,
	ETAPA_DESDE,
	ETAPA_HASTA,
	UBICACION_DESDE,
	UBICACION_HASTA,
	USUARIO,
	ROL_DECISOR,
	GPS_LAT,
	GPS_LON,
	CREATED_AT,
	DEVICE_CREATED_AT,
	CLAVE_IDEMPOTENCIA
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)`

// listarEventosPorGarantiaSQL returns the folio timeline in the order the port
// promises: device time, then creation time, then insertion order. The ID is a
// random UUID, so using it as the last key would scatter events that share both
// timestamps in an arbitrary order. RDB$DB_KEY points at the physical slot the
// row was written to, which on this append-only table is the order the command
// wrote them in. It is a physical address, not a logical sequence: a rebuilt
// table or a sweep could renumber it, so a causal sequence column would be the
// durable fix; until then it is the only ordering key the schema offers.
const listarEventosPorGarantiaSQL = `
SELECT
	ID,
	GARANTIA_ID,
	ARTICULO_REF,
	TIPO,
	DESCRIPCION,
	ETAPA_DESDE,
	ETAPA_HASTA,
	UBICACION_DESDE,
	UBICACION_HASTA,
	USUARIO,
	ROL_DECISOR,
	GPS_LAT,
	GPS_LON,
	CREATED_AT,
	DEVICE_CREATED_AT,
	CLAVE_IDEMPOTENCIA
FROM MSP_GA_EVENTO
WHERE GARANTIA_ID = ?
ORDER BY DEVICE_CREATED_AT, CREATED_AT, RDB$DB_KEY`

const obtenerEventoPorClaveIdempotenciaSQL = `
SELECT
	ID,
	GARANTIA_ID,
	ARTICULO_REF,
	TIPO,
	DESCRIPCION,
	ETAPA_DESDE,
	ETAPA_HASTA,
	UBICACION_DESDE,
	UBICACION_HASTA,
	USUARIO,
	ROL_DECISOR,
	GPS_LAT,
	GPS_LON,
	CREATED_AT,
	DEVICE_CREATED_AT,
	CLAVE_IDEMPOTENCIA
FROM MSP_GA_EVENTO
WHERE CLAVE_IDEMPOTENCIA = ?`
