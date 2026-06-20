// Package clientesfb implements the Firebird-backed repository for the clientes
// hub. Reads target native Microsip tables; this module owns no MSP_* tables and
// never writes to Microsip. The one read-only exception (user-approved 2026-06-16,
// for performance) is the directory + ficha total saldo, which is read from the
// MSP_SALDOS_VENTAS cobranza cache — a read-model OF native cargo facts, verified
// to match the native saldo formula exactly. See selectDirectorioColsGrouped. The cache
// is a plain MSP_ table (CHARACTER SET UTF8, NUMERIC) — no Win1252 decoding.
// Text columns in native Microsip tables are CHARACTER SET NONE (raw Windows-1252
// bytes) and must be scanned with firebird.Win1252.
//
//nolint:misspell // Spanish domain vocabulary (clientes, directorio, ficha, etc.) by project convention.
package clientesfb

// ─── Cliente identity ─────────────────────────────────────────────────────────

// selectClienteCols is the canonical column list for a single-cliente lookup.
// Order matches clienteRowRaw.scanFrom one-to-one.
//
// DIRS_CLIENTES uses COLONIA (not NOMBRE_CALLE) as the street-level field
// because B1 research shows COLONIA is 99.9% populated while NOMBRE_CALLE
// coverage is lower and CALLE is a composite. The Direccion VO receives
// NOMBRE_CALLE as calle, COLONIA as colonia, and POBLACION as poblacion.
//
// VERIFY-AT-CHECKPOINT: confirm that DIRS_CLIENTES.NOMBRE_CALLE (not CALLE)
// is the right street component for the Direccion.Calle() field. The research
// doc describes CALLE as a composite column (NOMBRE_CALLE + NUM_EXT + \n + …).
// Using NOMBRE_CALLE here keeps it clean; if the app wants the full composite
// swap to CALLE.
//
// All text columns from Microsip are CHARACTER SET NONE (raw Windows-1252
// bytes). COALESCE(none_col, ”) is intentionally OMITTED because mixing a
// NONE column with a UTF-8 connection literal causes Firebird to attempt
// transliteration, failing on bytes such as ñ (0xF1). The columns are selected
// bare and scanned as firebird.Win1252, which handles NULL → "" internally.
// GPS columns (U_LATITUD, U_LONGITUD) are VARCHAR CHARACTER SET NONE in
// LIBRES_CLIENTES — raw ASCII decimal text (e.g. "18.5032044"). Scanned as
// sql.NullString and parsed to float64 by parseUbicacion in rowmappers.go.
const selectClienteCols = `
	c.CLIENTE_ID,
	c.NOMBRE,
	c.LIMITE_CREDITO,
	c.NOTAS,
	c.ESTATUS,
	c.ZONA_CLIENTE_ID,
	z.NOMBRE                   AS ZONA_NOMBRE,
	c.COBRADOR_ID,
	cob.NOMBRE                 AS COBRADOR_NOMBRE,
	d.NOMBRE_CALLE,
	d.COLONIA,
	d.POBLACION,
	e.NOMBRE                   AS ESTADO_NOMBRE,
	d.TELEFONO1,
	lc.U_LATITUD,
	lc.U_LONGITUD`

const clienteFromClause = `
FROM CLIENTES c
LEFT JOIN ZONAS_CLIENTES z    ON z.ZONA_CLIENTE_ID = c.ZONA_CLIENTE_ID
LEFT JOIN COBRADORES cob      ON cob.COBRADOR_ID   = c.COBRADOR_ID
LEFT JOIN DIRS_CLIENTES d     ON d.CLIENTE_ID      = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN ESTADOS e           ON e.ESTADO_ID       = d.ESTADO_ID
LEFT JOIN LIBRES_CLIENTES lc  ON lc.CLIENTE_ID     = c.CLIENTE_ID`

const queryObtenerCliente = `
SELECT ` + selectClienteCols + clienteFromClause + `
WHERE c.CLIENTE_ID = ?`

// ─── Directory listing ────────────────────────────────────────────────────────

// ─── Directory listing (complete / unbounded) ────────────────────────────────

// selectDirectorioColsGrouped is the directory projection backed by an EFFICIENT
// grouped saldo aggregation over the MSP_SALDOS_VENTAS cache instead of the
// per-row correlated subquery used by selectDirectorioCols. A single derived
// table (sal) groups the cache by CLIENTE_ID once and is LEFT JOINed to CLIENTES,
// so saldo costs one aggregation pass for the whole set rather than one sub-select
// per row.
//
// Saldo source — MSP_SALDOS_VENTAS cache (USER-APPROVED EXCEPTION 2026-06-16),
// same source and formula as selectDirectorioCols above. SUM(s.SALDO) over
// non-cancelled cargos (CARGO_CANCELADO='N'), grouped by CLIENTE_ID. VERIFIED
// live to match the native formula exactly (cliente 12440: 504666.60). CAST is
// mandatory (firebirdsql v0.9.19 returns unscaled *big.Int for aggregates).
const selectDirectorioColsGrouped = selectClienteCols + `,
	COALESCE(sal.SALDO_TOTAL, 0) AS SALDO_TOTAL`

// directorioGroupedSaldoJoin is the single grouped-aggregation derived table that
// supplies SALDO_TOTAL per client from the MSP_SALDOS_VENTAS cache. Joined once
// (not correlated per row). The cache is small (~103k rows) and indexed on
// CLIENTE_ID, so this aggregation is sub-second even unfiltered — the previous
// native aggregation over the ~3.4M-row IMPORTES_DOCTOS_CC took ~56s here.
const directorioGroupedSaldoJoin = `
LEFT JOIN (
	SELECT s.CLIENTE_ID,
		CAST(SUM(s.SALDO) AS NUMERIC(18,2)) AS SALDO_TOTAL
	FROM MSP_SALDOS_VENTAS s
	WHERE s.CARGO_CANCELADO = 'N'
	GROUP BY s.CLIENTE_ID
) sal ON sal.CLIENTE_ID = c.CLIENTE_ID`

// queryListarDirectorioCompletoBase is the SELECT + FROM (with the grouped saldo
// join) for the unbounded directory listing. No FIRST clause — every matching row
// is returned.
//
// ESTATUS IN ('A','B') keeps both ALTA (A = active) and BAJA (B = dado de baja)
// clients, excluding only vendor-route pseudo-clients (V) and cancelled (C).
// Clients dados de baja (B) are intentionally retained: a large share of clients
// that still carry outstanding saldo are ESTATUS='B', and cobradores must be able
// to find them in the directory to collect. (B is "baja", not "bloqueado".)
//
// PERFORMANCE (measured live 2026-06-16, FB 5.0):
//   - Unfiltered (whole padrón, ~38k rows): sub-second now that the grouped saldo
//     join reads the small, indexed MSP_SALDOS_VENTAS cache (~103k rows) instead
//     of aggregating the full ~3.4M-row IMPORTES_DOCTOS_CC table. The unfiltered
//     global path used to take ~56s with the native aggregation.
//   - Zone-filtered (e.g. ~2.5k clients): also sub-second.
//
// A Fase-2 optimization would materialize the pulse columns into MSP_* and
// ORDER BY at the DB level.
const queryListarDirectorioCompletoBase = `
SELECT ` + selectDirectorioColsGrouped + clienteFromClause + directorioGroupedSaldoJoin + `
WHERE c.ESTATUS IN ('A', 'B')`

// ─── ResumenFicha ─────────────────────────────────────────────────────────────

// queryResumenFichaComprado returns TotalComprado and NumVentas for a client,
// filtered by cargo.FECHA (sale date). Optional date-range predicates on
// cargo.FECHA are appended by fetchFichaTotales.
//
// Separated from the abonado query so that TotalAbonado/NumPagos can be
// independently filtered by abono.FECHA (payment date), matching the date
// semantics of queryAbonosPorMesBase and buildCompradoVsAbonadoQuery.
//
// All SUM casts are required (v0.9.19 driver scale bug).
const queryResumenFichaComprado = `
SELECT
  CAST(COALESCE(SUM(i.IMPORTE + i.IMPUESTO), 0) AS NUMERIC(18,2)) AS TOTAL_COMPRADO,
  COUNT(DISTINCT i.DOCTO_CC_ACR_ID)                                AS NUM_VENTAS
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
WHERE cargo.CLIENTE_ID = ?
  AND cargo.CONCEPTO_CC_ID = 5
  AND cargo.CANCELADO = 'N'
  AND i.TIPO_IMPTE = 'C'
  AND i.CANCELADO = 'N'`

// queryResumenFichaAbonado returns TotalAbonado and NumPagos for a client,
// filtered by abono.FECHA (payment date). The abono doc is joined via
// DOCTOS_CC abono ON abono.DOCTO_CC_ID = i.DOCTO_CC_ID so that optional
// date-range predicates appended by fetchFichaTotales target the payment date,
// not the sale/cargo date. This makes the header KPIs definitionally consistent
// with queryAbonosPorMesBase and buildCompradoVsAbonadoQuery.
//
// cargo is still joined for CLIENTE_ID and CONCEPTO_CC_ID scoping.
// All SUM casts are required (v0.9.19 driver scale bug).
const queryResumenFichaAbonado = `
SELECT
  CAST(COALESCE(SUM(i.IMPORTE), 0) AS NUMERIC(18,2)) AS TOTAL_ABONADO,
  COUNT(DISTINCT i.IMPTE_DOCTO_CC_ID)                 AS NUM_PAGOS
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
JOIN DOCTOS_CC abono ON abono.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE cargo.CLIENTE_ID = ?
  AND cargo.CONCEPTO_CC_ID = 5
  AND cargo.CANCELADO = 'N'
  AND i.TIPO_IMPTE = 'R'
  AND i.CANCELADO = 'N'`

// queryResumenFichaSaldo returns the ficha's total saldo from the MSP_SALDOS_VENTAS
// cache (USER-APPROVED EXCEPTION 2026-06-16), the SAME source the directory uses,
// so the ficha saldo equals the directory saldo exactly. SUM(s.SALDO) over
// non-cancelled cargos (CARGO_CANCELADO='N') for the client. VERIFIED live to
// match the native TotalComprado − TotalAbonado formula (cliente 12440: 504666.60).
// CAST is mandatory (firebirdsql v0.9.19 returns unscaled *big.Int for aggregates).
const queryResumenFichaSaldo = `
SELECT COALESCE(CAST(SUM(s.SALDO) AS NUMERIC(18,2)), 0) AS SALDO_TOTAL
FROM MSP_SALDOS_VENTAS s
WHERE s.CLIENTE_ID = ?
  AND s.CARGO_CANCELADO = 'N'`

// queryAbonosPorMesBase is the date-range-filterable base for the monthly
// payment totals query. It ends after the last WHERE condition so the repo can
// append optional AND abono.FECHA >= ? / <= ? before the GROUP BY / ORDER BY.
const queryAbonosPorMesBase = `
SELECT
  EXTRACT(YEAR FROM abono.FECHA)                                            AS ANIO,
  EXTRACT(MONTH FROM abono.FECHA)                                           AS MES,
  CAST(SUM(i.IMPORTE) AS NUMERIC(18,2))                                     AS MONTO
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
JOIN DOCTOS_CC abono ON abono.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE cargo.CLIENTE_ID = ?
  AND cargo.CONCEPTO_CC_ID = 5
  AND cargo.CANCELADO = 'N'
  AND i.TIPO_IMPTE = 'R'
  AND i.CANCELADO = 'N'`

// queryAbonosPorMesGroupOrder is the GROUP BY + ORDER BY suffix appended after
// any optional date-range conditions.
const queryAbonosPorMesGroupOrder = `
GROUP BY EXTRACT(YEAR FROM abono.FECHA), EXTRACT(MONTH FROM abono.FECHA)
ORDER BY ANIO, MES`

// buildCompradoVsAbonadoQuery returns a dual-series comprado/abonado query with
// optional date range clauses injected into both UNION branches.
//
// compradoExtra is appended after the last WHERE condition of the comprado
// branch (cargo.FECHA); abonadoExtra is appended to the abonado branch
// (abono.FECHA). Both are empty strings when the rango is unbounded.
//
// VERIFY-AT-CHECKPOINT: DOCTOS_CC.FECHA is type DATE (not TIMESTAMP) per B2
// research. EXTRACT(YEAR/MONTH FROM DATE) is valid in Firebird — confirm no
// cast needed.
func buildCompradoVsAbonadoQuery(compradoExtra, abonadoExtra string) string {
	return `
SELECT
  ANIO, MES,
  CAST(SUM(COMPRADO) AS NUMERIC(18,2)) AS COMPRADO,
  CAST(SUM(ABONADO)  AS NUMERIC(18,2)) AS ABONADO
FROM (
  SELECT
    EXTRACT(YEAR FROM cargo.FECHA)  AS ANIO,
    EXTRACT(MONTH FROM cargo.FECHA) AS MES,
    i.IMPORTE + i.IMPUESTO          AS COMPRADO,
    0                               AS ABONADO
  FROM IMPORTES_DOCTOS_CC i
  JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
  WHERE cargo.CLIENTE_ID = ?
    AND cargo.CONCEPTO_CC_ID = 5
    AND cargo.CANCELADO = 'N'
    AND i.TIPO_IMPTE = 'C'
    AND i.CANCELADO = 'N'` + compradoExtra + `
  UNION ALL
  SELECT
    EXTRACT(YEAR FROM abono.FECHA)  AS ANIO,
    EXTRACT(MONTH FROM abono.FECHA) AS MES,
    0                               AS COMPRADO,
    i.IMPORTE                       AS ABONADO
  FROM IMPORTES_DOCTOS_CC i
  JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
  JOIN DOCTOS_CC abono ON abono.DOCTO_CC_ID = i.DOCTO_CC_ID
  WHERE cargo.CLIENTE_ID = ?
    AND cargo.CONCEPTO_CC_ID = 5
    AND cargo.CANCELADO = 'N'
    AND i.TIPO_IMPTE = 'R'
    AND i.CANCELADO = 'N'` + abonadoExtra + `
) t
GROUP BY ANIO, MES
ORDER BY ANIO, MES`
}

// ─── RitmoPago ────────────────────────────────────────────────────────────────

// queryRitmoPagosBase returns individual payment rows (one per IMPORTES_DOCTOS_CC
// abono) for a client's credit accounts. Modeled on queryResumenFichaAbonado but
// without GROUP BY — each row is a single payment event with its date and amount.
// Amount = IMPORTE + IMPUESTO (gross, same formula as the MSP_SALDOS_VENTAS cache).
// CAST is mandatory (firebirdsql v0.9.19 driver scale bug on NUMERIC expressions).
// Optional date-range predicates on abono.FECHA are appended by ObtenerRitmoPagoData.
//
// The NOT EXISTS clause excludes abonos tied to CC contracts whose FORMA_DE_PAGO
// resolves to "Contado" — these are same-day cash-style settlements recorded via
// FORMA_COBRO_ID=71 but with a placeholder contado contract (parcialidad=1, plazo=1,
// enganche=0). Including them would inflate ABONADO totals and break saldo
// reconstruction in the rhythm chart.
const queryRitmoPagosBase = `
SELECT
  abono.FECHA,
  CAST(COALESCE(i.IMPORTE + i.IMPUESTO, 0) AS NUMERIC(18,2)) AS IMPORTE
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
JOIN DOCTOS_CC abono ON abono.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE cargo.CLIENTE_ID = ?
  AND cargo.CONCEPTO_CC_ID = 5
  AND cargo.CANCELADO = 'N'
  AND i.TIPO_IMPTE = 'R'
  AND i.CANCELADO = 'N'
  AND NOT EXISTS (
    SELECT 1 FROM LIBRES_CARGOS_CC lc
    LEFT JOIN LISTAS_ATRIBUTOS la ON la.LISTA_ATRIB_ID = lc.FORMA_DE_PAGO
    WHERE lc.DOCTO_CC_ID = cargo.DOCTO_CC_ID
      AND UPPER(la.VALOR_DESPLEGADO) = 'CONTADO'
  )`

// queryRitmoVentasBase returns sale header rows for the RitmoPago series.
// EsCredito is derived via EXISTS on DOCTOS_PV_COBROS (FORMA_COBRO_ID=71 = crédito)
// AND NOT EXISTS a CC contract with FORMA_DE_PAGO resolving to "Contado" — matching
// the same rule as selectVentaClienteCols (see that constant for full rationale).
// PlazoMeses comes from LIBRES_CARGOS_CC.TIEMPO_A_CORTO_PLAZOMESES bridged via
// DOCTOS_ENTRE_SIS; 0 when contado or when the contract row is absent (pre-2018 data).
// TOTAL = IMPORTE_NETO + TOTAL_IMPUESTOS (gross, matching the venta header total).
// CAST is mandatory (firebirdsql v0.9.19 driver scale bug on NUMERIC expressions).
// Optional date-range predicates on pv.FECHA are appended by ObtenerRitmoPagoData.
const queryRitmoVentasBase = `
SELECT
  pv.FECHA,
  CAST(pv.IMPORTE_NETO + pv.TOTAL_IMPUESTOS AS NUMERIC(18,2)) AS TOTAL,
  pv.DOCTO_PV_ID,
  pv.FOLIO,
  CASE WHEN EXISTS (
    SELECT 1 FROM DOCTOS_PV_COBROS cob
    WHERE cob.DOCTO_PV_ID = pv.DOCTO_PV_ID AND cob.FORMA_COBRO_ID = 71
  ) AND NOT EXISTS (
    SELECT 1 FROM DOCTOS_ENTRE_SIS des
    JOIN LIBRES_CARGOS_CC lc ON lc.DOCTO_CC_ID = des.DOCTO_DEST_ID
    LEFT JOIN LISTAS_ATRIBUTOS la ON la.LISTA_ATRIB_ID = lc.FORMA_DE_PAGO
    WHERE des.CLAVE_SIS_FTE  = 'PV'
      AND des.CLAVE_SIS_DEST = 'CC'
      AND des.DOCTO_FTE_ID   = pv.DOCTO_PV_ID
      AND UPPER(la.VALOR_DESPLEGADO) = 'CONTADO'
  ) THEN 'CREDITO' ELSE 'CONTADO' END AS TIPO,
  COALESCE((
    SELECT lc.TIEMPO_A_CORTO_PLAZOMESES
    FROM DOCTOS_ENTRE_SIS des
    JOIN LIBRES_CARGOS_CC lc ON lc.DOCTO_CC_ID = des.DOCTO_DEST_ID
    WHERE des.CLAVE_SIS_FTE  = 'PV'
      AND des.CLAVE_SIS_DEST = 'CC'
      AND des.DOCTO_FTE_ID   = pv.DOCTO_PV_ID
    ROWS 1
  ), 0) AS PLAZO_MESES
FROM DOCTOS_PV pv
WHERE pv.CLIENTE_ID = ?
  AND pv.TIPO_DOCTO IN ('V', 'P')
  AND pv.ESTATUS = 'N'`

// ─── ListarVentas ─────────────────────────────────────────────────────────────

// selectVentaClienteCols is the projection for a VentaCliente row.
// TIPO is derived from DOCTOS_PV_COBROS: FORMA_COBRO_ID=71 means CREDITO UNLESS
// the linked CC contract has FORMA_DE_PAGO resolving to "Contado" — which happens
// for ~10% of sales where Microsip records a placeholder contado contract
// (parcialidad=1, plazo=1, enganche=0, paid same day). Rule [A]: CRÉDITO only if
// FORMA_COBRO_ID=71 AND NOT EXISTS a contado contract for this PV.
// EXISTS subquery is used to avoid inflating the result set.
//
// Saldo per sale is still computed natively from IMPORTES_DOCTOS_CC, bridged
// via DOCTOS_ENTRE_SIS (PV → CC) to find the cargo DOCTO_CC_ID for this PV.
// NumPagos counts the distinct abono rows applied to that cargo.
//
// FOLLOW-UP: for full consistency with the directory + ficha (which now read the
// MSP_SALDOS_VENTAS cache), this per-venta saldo could also be sourced from
// MSP_SALDOS_VENTAS.SALDO keyed by DOCTO_PV_ID. Left native for now — it is a
// single-client, fast path and out of scope for the directory perf change.
//
// VERIFY-AT-CHECKPOINT: confirm that for a contado sale (no CC cargo),
// the saldo subquery correctly returns 0 (no JOIN match → COALESCE to 0).
const selectVentaClienteCols = `
	pv.DOCTO_PV_ID,
	pv.CLIENTE_ID,
	pv.FECHA,
	pv.FOLIO,
	-- Total = importe neto + impuestos (lo que adeuda el cliente). IMPORTE_NETO
	-- es el precio SIN IVA; el cliente debe el bruto, que además coincide con el
	-- saldo (computado abajo con i.IMPORTE+i.IMPUESTO) y con el cargo en CC.
	-- VERIFICADO vivo: N00002192 7586.21 + 1213.79 = 8800.00 (cerrado).
	-- CAST por el bug de escala del driver firebirdsql en expresiones NUMERIC.
	CAST(pv.IMPORTE_NETO + pv.TOTAL_IMPUESTOS AS NUMERIC(18,2)) AS TOTAL,
	CASE WHEN EXISTS (
		SELECT 1 FROM DOCTOS_PV_COBROS cob
		WHERE cob.DOCTO_PV_ID = pv.DOCTO_PV_ID AND cob.FORMA_COBRO_ID = 71
	) AND NOT EXISTS (
		SELECT 1 FROM DOCTOS_ENTRE_SIS des
		JOIN LIBRES_CARGOS_CC lc ON lc.DOCTO_CC_ID = des.DOCTO_DEST_ID
		LEFT JOIN LISTAS_ATRIBUTOS la ON la.LISTA_ATRIB_ID = lc.FORMA_DE_PAGO
		WHERE des.CLAVE_SIS_FTE  = 'PV'
		  AND des.CLAVE_SIS_DEST = 'CC'
		  AND des.DOCTO_FTE_ID   = pv.DOCTO_PV_ID
		  AND UPPER(la.VALOR_DESPLEGADO) = 'CONTADO'
	) THEN 'CREDITO' ELSE 'CONTADO' END AS TIPO,
	COALESCE((
		SELECT CAST(
			MAXVALUE(
				SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END)
				- SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END),
				0
			) AS NUMERIC(18,2))
		FROM DOCTOS_ENTRE_SIS des
		JOIN IMPORTES_DOCTOS_CC i   ON i.DOCTO_CC_ACR_ID = des.DOCTO_DEST_ID
		JOIN DOCTOS_CC cargo        ON cargo.DOCTO_CC_ID  = des.DOCTO_DEST_ID
		WHERE des.CLAVE_SIS_FTE  = 'PV'
		  AND des.CLAVE_SIS_DEST = 'CC'
		  AND des.DOCTO_FTE_ID   = pv.DOCTO_PV_ID
		  AND cargo.CANCELADO    = 'N'
		  AND i.CANCELADO        = 'N'
	), 0) AS SALDO_VENTA,
	COALESCE((
		SELECT COUNT(DISTINCT i.IMPTE_DOCTO_CC_ID)
		FROM DOCTOS_ENTRE_SIS des
		JOIN IMPORTES_DOCTOS_CC i ON i.DOCTO_CC_ACR_ID = des.DOCTO_DEST_ID
		WHERE des.CLAVE_SIS_FTE  = 'PV'
		  AND des.CLAVE_SIS_DEST = 'CC'
		  AND des.DOCTO_FTE_ID   = pv.DOCTO_PV_ID
		  AND i.TIPO_IMPTE       = 'R'
		  AND i.CANCELADO        = 'N'
	), 0) AS NUM_PAGOS,
	-- BE-2 enrichment: hora de la venta (wall-clock local, display string only).
	-- DOCTOS_PV.HORA is a Firebird TIME column; CAST to VARCHAR(13) then SUBSTRING
	-- to extract "HH:MM:SS" without driver-specific time handling.
	SUBSTRING(CAST(pv.HORA AS VARCHAR(13)) FROM 1 FOR 8) AS HORA,
	-- BE-2 enrichment: almacén/ruta name via ALMACENES.
	-- ALMACENES.NOMBRE is CHARACTER SET NONE (Win1252) — scanned with firebird.Win1252.
	alm.NOMBRE AS ALMACEN_NOMBRE,
	-- BE-2 enrichment: name of first J/N line item (kit-header or normal article).
	-- Correlated scalar subquery with ROWS 1 sidesteps the firebirdsql v0.9.19
	-- param bug (cannot bind ? inside FROM-clause derived tables). COALESCE to ''
	-- covers sales with no J/N lines. ARTICULOS.NOMBRE is Win1252.
	COALESCE((
		SELECT a.NOMBRE
		FROM DOCTOS_PV_DET d
		JOIN ARTICULOS a ON a.ARTICULO_ID = d.ARTICULO_ID
		WHERE d.DOCTO_PV_ID = pv.DOCTO_PV_ID
		  AND d.ROL IN ('J', 'N')
		ORDER BY d.POSICION
		ROWS 1
	), '') AS PRIMER_ARTICULO,
	-- BE-2 enrichment: count of J/N lines (excludes ROL='C' kit-component rows).
	COALESCE((
		SELECT COUNT(*)
		FROM DOCTOS_PV_DET d
		WHERE d.DOCTO_PV_ID = pv.DOCTO_PV_ID
		  AND d.ROL IN ('J', 'N')
	), 0) AS NUM_ARTICULOS`

const queryListarVentasBase = `
SELECT FIRST ? ` + selectVentaClienteCols + `
FROM DOCTOS_PV pv
LEFT JOIN ALMACENES alm ON alm.ALMACEN_ID = pv.ALMACEN_ID
WHERE pv.CLIENTE_ID = ?
  AND pv.TIPO_DOCTO IN ('V', 'P')
  AND pv.ESTATUS = 'N'`

// ─── ObtenerVentaDetalle ──────────────────────────────────────────────────────

// queryVentaHeader fetches the venta header only — used to establish the sale
// exists before fetching children. Reuses selectVentaClienteCols shape.
// The LEFT JOIN ALMACENES is required because selectVentaClienteCols now
// references alm.NOMBRE (BE-2 enrichment). The venta-detalle header carries
// hora, almacen, primer_articulo and num_articulos just like the list row.
const queryVentaHeader = `
SELECT ` + selectVentaClienteCols + `
FROM DOCTOS_PV pv
LEFT JOIN ALMACENES alm ON alm.ALMACEN_ID = pv.ALMACEN_ID
WHERE pv.DOCTO_PV_ID = ?
  AND pv.TIPO_DOCTO IN ('V', 'P')
  AND pv.ESTATUS = 'N'`

// queryProductos fetches the sale line items for a given DOCTO_PV_ID.
//
// ROL filter: Microsip records kit/juego sales with three ROL values:
//   - ROL='N': normal single-article line — always shown.
//   - ROL='J': the priced kit header line — shown (carries the kit total price).
//   - ROL='C': zero-price kit component lines (stock deduction) — EXCLUDED.
//
// Keeping ROL IN ('N', 'J') and excluding ROL='C' avoids showing duplicate
// zero-price component rows to the user while still showing the kit as one
// priced line (ROL='J'). Source: project memory reference_microsip_juegos_kits
// and B3 research §5.2.
//
// VERIFY-AT-CHECKPOINT: verify the exact ROL values against DOCTOS_PV_DET.ROL
// in the live DB. The memory doc states ROL='J' (kit priced), ROL='C' (kit
// component zero-price), ROL='N' (normal). Confirm there are no other values
// (e.g. ROL='D' for devolucion) that should also be filtered.
//
// Precio unitario e IMPORTE se muestran CON IVA (bruto), igual que el total de
// la venta. PRECIO_UNITARIO_IMPTO es el precio unitario con impuesto (NUMERIC(18,6),
// misma escala que PRECIO_UNITARIO), 100% poblado (verificado: 121,811/121,811
// líneas) y redondo (ej. moto G52: 7413.79 neto → 8600.00 con IVA). El IMPORTE de
// línea = precio_unitario_impto * unidades * (1 - dscto/100), espejo de cómo
// PRECIO_TOTAL_NETO se deriva del neto. CAST por el bug de escala del driver.
//
// UNIDADES scale=5, PRECIO_UNITARIO_IMPTO scale=6, IMPORTE scale=2,
// PCTJE_DSCTO scale=6 — scanned with appropriate ScanDecimal calls.
const queryProductos = `
SELECT
	det.ARTICULO_ID,
	a.NOMBRE,
	det.UNIDADES,
	det.PRECIO_UNITARIO_IMPTO,
	CAST(det.PRECIO_UNITARIO_IMPTO * det.UNIDADES * (1 - det.PCTJE_DSCTO / 100) AS NUMERIC(18,2)) AS IMPORTE,
	det.PCTJE_DSCTO
FROM DOCTOS_PV_DET det
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
WHERE det.DOCTO_PV_ID = ?
  AND det.ROL IN ('N', 'J')
ORDER BY det.POSICION`

// queryContrato fetches the credit contract data for the cargo CC associated
// with a PV sale, using the DOCTOS_ENTRE_SIS bridge.
//
// VENDEDOR_1/2/3 in LIBRES_CARGOS_CC are IDs into the LISTAS_ATRIBUTOS table.
// LISTAS_ATRIBUTOS.VALOR_DESPLEGADO holds the human-readable name.
// LEFT JOIN LISTAS_ATRIBUTOS lv1/lv2/lv3 resolves the vendedor names.
//
// FORMA_DE_PAGO in LIBRES_CARGOS_CC is also an ID into LISTAS_ATRIBUTOS.
// We resolve it to VALOR_DESPLEGADO for FormaDePago.
//
// TIEMPO_A_CORTO_PLAZOMESES is used as PlazoMeses (months of the financing
// term). Per B2 research, CREDITO_EN_MESES is a FK to a plan ID, not a raw
// month count, so TIEMPO_A_CORTO_PLAZOMESES is the best available month count.
//
// VERIFY-AT-CHECKPOINT: verify that TIEMPO_A_CORTO_PLAZOMESES is indeed the
// correct plazo-meses column (not CREDITO_EN_MESES which is an opaque FK).
// Also verify that LISTAS_ATRIBUTOS resolves FORMA_DE_PAGO IDs to
// SEMANAL/QUINCENAL/MENSUAL text values.
//
// ORDER BY + ROWS 1 ensures a deterministic result when multiple DOCTOS_ENTRE_SIS
// bridge rows exist for the same PV sale (e.g. duplicate bridge rows).
// CONFIRMED (live DB): DOCTOS_ENTRE_SIS has no surrogate PK. Columns are:
// DOCTO_DEST_ID, CLAVE_SIS_DEST, CLAVE_SIS_FTE, DOCTO_FTE_ID, TIPO_DOCTO.
// ORDER BY DOCTO_DEST_ID DESC uses the cargo CC ID as a tiebreaker (highest wins).
// CONFIRMED (live DB): TIEMPO_A_CORTO_PLAZOMESES exists in LIBRES_CARGOS_CC
// alongside CREDITO_EN_MESES (opaque FK); TIEMPO_A_CORTO_PLAZOMESES is correct.
// Note: VALOR_DESPLEGADO columns are CHARACTER SET NONE in Microsip. COALESCE
// and UPPER() with a UTF-8 connection literal would force transliteration on
// those bytes, failing on Win1252 characters. Bare column selects are used
// instead; NULL → "" is handled in Go by firebird.Win1252.Scan. UPPER() on
// vendedor names is also done in Go (strings.ToUpper) rather than SQL.
const queryContrato = `
SELECT
	lc.PARCIALIDAD,
	lc.ENGANCHE,
	lc.PRECIO_DE_CONTADO,
	lc.TIEMPO_A_CORTO_PLAZOMESES,
	lfp.VALOR_DESPLEGADO,
	lv1.VALOR_DESPLEGADO,
	lv2.VALOR_DESPLEGADO,
	lv3.VALOR_DESPLEGADO
FROM DOCTOS_ENTRE_SIS des
JOIN DOCTOS_CC cargo             ON cargo.DOCTO_CC_ID  = des.DOCTO_DEST_ID
JOIN LIBRES_CARGOS_CC lc         ON lc.DOCTO_CC_ID     = des.DOCTO_DEST_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp   ON lfp.LISTA_ATRIB_ID = lc.FORMA_DE_PAGO
LEFT JOIN LISTAS_ATRIBUTOS lv1   ON lv1.LISTA_ATRIB_ID = lc.VENDEDOR_1
LEFT JOIN LISTAS_ATRIBUTOS lv2   ON lv2.LISTA_ATRIB_ID = lc.VENDEDOR_2
LEFT JOIN LISTAS_ATRIBUTOS lv3   ON lv3.LISTA_ATRIB_ID = lc.VENDEDOR_3
WHERE des.CLAVE_SIS_FTE  = 'PV'
  AND des.CLAVE_SIS_DEST = 'CC'
  AND des.DOCTO_FTE_ID   = ?
  AND cargo.CANCELADO    = 'N'
ORDER BY des.DOCTO_DEST_ID DESC
ROWS 1`

// queryPagos fetches the payment history for a given PV sale, bridged through
// DOCTOS_ENTRE_SIS to the cargo DOCTO_CC_ID.
//
// Pagos are DOCTOS_CC with NATURALEZA_CONCEPTO='R' (abono) linked to the
// cargo via IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID.
//
// The real amount is IMPORTES_DOCTOS_CC.IMPORTE (not DOCTOS_CC.IMPORTE_COBRO
// which is always 0, confirmed by B2 research and cobranza pago writer).
//
// FormaCobro is resolved via a correlated scalar subquery (ROWS 1) instead of
// a LEFT JOIN, to prevent fan-out on split payments (a single pago row with
// multiple FORMAS_COBRO_DOCTOS entries). The scalar subquery returns exactly
// one forma-cobro name per pago, picking the first by FORMA_COBRO_ID.
//
// VERIFY-AT-CHECKPOINT: confirm that joining FORMAS_COBRO_DOCTOS
// (NOM_TABLA_DOCTOS='DOCTOS_CC', DOCTO_ID=pago.DOCTO_CC_ID) gives the correct
// forma cobro name.
const queryPagos = `
SELECT
	pago.DOCTO_CC_ID,
	pago.FECHA,
	-- Importe del pago = lo que el cliente realmente abonó = IMPORTE + IMPUESTO
	-- (los abonos R traen su porción de IVA en i.IMPUESTO). Verificado: abono
	-- 7241.38 + 1158.62 = 8400.00. Esto cuadra con el saldo per-venta, que ahora
	-- también resta el abono bruto (cargo bruto − abono bruto = SALDO del cache).
	CAST(COALESCE(i.IMPORTE + i.IMPUESTO, 0) AS NUMERIC(18,2)) AS IMPORTE,
	COALESCE((
		SELECT fc.NOMBRE
		FROM FORMAS_COBRO_DOCTOS fcd
		JOIN FORMAS_COBRO fc ON fc.FORMA_COBRO_ID = fcd.FORMA_COBRO_ID
		WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC'
		  AND fcd.DOCTO_ID = pago.DOCTO_CC_ID
		ROWS 1
	), '') AS FORMA_COBRO,
	des.DOCTO_DEST_ID,
	pago.CONCEPTO_CC_ID,
	COALESCE(conc.NOMBRE, '') AS CONCEPTO_NOMBRE,
	COALESCE(cob.NOMBRE, pago.DESCRIPCION, '') AS COBRADOR_NOMBRE
FROM DOCTOS_ENTRE_SIS des
JOIN IMPORTES_DOCTOS_CC i   ON i.DOCTO_CC_ACR_ID  = des.DOCTO_DEST_ID
JOIN DOCTOS_CC pago         ON pago.DOCTO_CC_ID   = i.DOCTO_CC_ID
LEFT JOIN CONCEPTOS_CC conc ON conc.CONCEPTO_CC_ID = pago.CONCEPTO_CC_ID
LEFT JOIN COBRADORES cob    ON cob.COBRADOR_ID     = pago.COBRADOR_ID
WHERE des.CLAVE_SIS_FTE  = 'PV'
  AND des.CLAVE_SIS_DEST = 'CC'
  AND des.DOCTO_FTE_ID   = ?
  AND pago.NATURALEZA_CONCEPTO = 'R'
  AND pago.CANCELADO     = 'N'
  AND i.TIPO_IMPTE       = 'R'
  AND i.CANCELADO        = 'N'
ORDER BY pago.FECHA`

// ─── ObtenerPagoDetalle ───────────────────────────────────────────────────────

// queryPagoDetalle fetches the rich detail for a single payment document (DOCTOS_CC
// abono). Amounts are aggregated over IMPORTES_DOCTOS_CC rows with TIPO_IMPTE='R'
// and CANCELADO='N'. The primary cargo is resolved via a correlated ROWS 1 scalar
// subquery (not a derived-table JOIN with a ? param — the driver v0.9.19 cannot
// bind ? inside a FROM-clause derived table). The linked PV sale and MSP cache
// entries are then JOINed by literal column references.
//
// SUM casts are mandatory (firebirdsql v0.9.19 scale bug on NUMERIC aggregates).
// CANCELADO and APLICADO are CHAR(1) ('S'/'N') — scanned as string and converted
// to bool in pagoDetalleRowRaw.assemble().
// Text columns from Microsip (CONCEPTOS_CC.NOMBRE, COBRADORES.NOMBRE,
// DOCTOS_CC.DESCRIPCION, FORMAS_COBRO.NOMBRE, FORMAS_COBRO_DOCTOS.REFERENCIA)
// are CHARACTER SET NONE (Win1252). MSP_PAGOS_RECIBIDOS columns are UTF8.
//
// GPS: native DOCTOS_CC has no LAT/LON columns; GPS comes only from
// MSP_PAGOS_RECIBIDOS.LAT/LON (VARCHAR(20), ASCII decimal text).
//
// NOTE: doctoCCID is bound once — in the outer WHERE clause.
// APLICA_A_CARGO_ID and DOCTO_PV_ID are resolved via correlated scalar subqueries
// using ROWS 1, matching the pattern from queryPagos (FormaCobro correlated scalar).
const queryPagoDetalle = `
SELECT
  p.DOCTO_CC_ID,
  p.FECHA,
  p.FOLIO,
  p.CANCELADO,
  p.APLICADO,
  p.CONCEPTO_CC_ID,
  CAST(COALESCE(SUM(i.IMPORTE + i.IMPUESTO), 0) AS NUMERIC(18,2)) AS IMPORTE,
  CAST(COALESCE(SUM(i.IMPUESTO), 0)             AS NUMERIC(18,2)) AS IVA,
  COALESCE(p.COBRADOR_ID, 0)                    AS COBRADOR_ID,
  conc.NOMBRE                                   AS CONCEPTO_NOMBRE,
  cob.NOMBRE                                    AS COBRADOR_NOMBRE,
  p.DESCRIPCION                                 AS DESCRIPCION,
  COALESCE(fcd.FORMA_COBRO_ID, 0)               AS FORMA_COBRO_ID,
  fc.NOMBRE                                     AS FORMA_COBRO_NOMBRE,
  fcd.REFERENCIA                                AS REFERENCIA,
  COALESCE((
    SELECT i2.DOCTO_CC_ACR_ID
    FROM IMPORTES_DOCTOS_CC i2
    WHERE i2.DOCTO_CC_ID = p.DOCTO_CC_ID
      AND i2.TIPO_IMPTE = 'R'
      AND i2.CANCELADO  = 'N'
    ORDER BY i2.IMPORTE + i2.IMPUESTO DESC
    ROWS 1
  ), 0)                                         AS APLICA_A_CARGO_ID,
  (
    SELECT sv.SALDO
    FROM MSP_SALDOS_VENTAS sv
    WHERE sv.DOCTO_CC_ID = (
      SELECT i2.DOCTO_CC_ACR_ID
      FROM IMPORTES_DOCTOS_CC i2
      WHERE i2.DOCTO_CC_ID = p.DOCTO_CC_ID
        AND i2.TIPO_IMPTE = 'R'
        AND i2.CANCELADO  = 'N'
      ORDER BY i2.IMPORTE + i2.IMPUESTO DESC
      ROWS 1
    )
    ROWS 1
  )                                             AS SALDO_CARGO,
  COALESCE((
    SELECT des.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS des
    WHERE des.CLAVE_SIS_FTE  = 'PV'
      AND des.CLAVE_SIS_DEST = 'CC'
      AND des.DOCTO_DEST_ID  = (
        SELECT i2.DOCTO_CC_ACR_ID
        FROM IMPORTES_DOCTOS_CC i2
        WHERE i2.DOCTO_CC_ID = p.DOCTO_CC_ID
          AND i2.TIPO_IMPTE = 'R'
          AND i2.CANCELADO  = 'N'
        ORDER BY i2.IMPORTE + i2.IMPUESTO DESC
        ROWS 1
      )
    ROWS 1
  ), 0)                                         AS DOCTO_PV_ID,
  r.COBRADOR_ID                                 AS MSP_COBRADOR_ID,
  r.COBRADOR                                    AS MSP_COBRADOR,
  r.FORMA_COBRO_ID                              AS MSP_FORMA_COBRO_ID,
  r.LAT                                         AS MSP_LAT,
  r.LON                                         AS MSP_LON,
  r.RECEIVED_AT                                 AS MSP_RECEIVED_AT,
  r.APLICADO_AT                                 AS MSP_APLICADO_AT
FROM DOCTOS_CC p
JOIN IMPORTES_DOCTOS_CC i ON i.DOCTO_CC_ID = p.DOCTO_CC_ID
  AND i.TIPO_IMPTE = 'R'
  AND i.CANCELADO  = 'N'
LEFT JOIN CONCEPTOS_CC conc ON conc.CONCEPTO_CC_ID = p.CONCEPTO_CC_ID
LEFT JOIN COBRADORES cob    ON cob.COBRADOR_ID     = p.COBRADOR_ID
LEFT JOIN FORMAS_COBRO_DOCTOS fcd ON fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC'
  AND fcd.DOCTO_ID = p.DOCTO_CC_ID
LEFT JOIN FORMAS_COBRO fc   ON fc.FORMA_COBRO_ID  = fcd.FORMA_COBRO_ID
LEFT JOIN MSP_PAGOS_RECIBIDOS r ON r.DOCTO_CC_ID  = p.DOCTO_CC_ID
WHERE p.DOCTO_CC_ID = ?
GROUP BY
  p.DOCTO_CC_ID, p.FECHA, p.FOLIO, p.CANCELADO, p.APLICADO, p.CONCEPTO_CC_ID,
  p.COBRADOR_ID, p.DESCRIPCION,
  conc.NOMBRE, cob.NOMBRE,
  fcd.FORMA_COBRO_ID, fc.NOMBRE, fcd.REFERENCIA,
  r.COBRADOR_ID, r.COBRADOR, r.FORMA_COBRO_ID, r.LAT, r.LON,
  r.RECEIVED_AT, r.APLICADO_AT`
