// Package clientesfb implements the Firebird-backed repository for the clientes
// hub. Reads target native Microsip tables; this module owns no MSP_* tables and
// never writes to Microsip. The one read-only exception (user-approved 2026-06-16,
// for performance) is the directory + ficha total saldo, which is read from the
// MSP_SALDOS_VENTAS cobranza cache — a read-model OF native cargo facts, verified
// to match the native saldo formula exactly. See selectDirectorioCols. The cache
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
	d.TELEFONO1`

const clienteFromClause = `
FROM CLIENTES c
LEFT JOIN ZONAS_CLIENTES z    ON z.ZONA_CLIENTE_ID = c.ZONA_CLIENTE_ID
LEFT JOIN COBRADORES cob      ON cob.COBRADOR_ID   = c.COBRADOR_ID
LEFT JOIN DIRS_CLIENTES d     ON d.CLIENTE_ID      = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN ESTADOS e           ON e.ESTADO_ID       = d.ESTADO_ID`

const queryObtenerCliente = `
SELECT ` + selectClienteCols + clienteFromClause + `
WHERE c.CLIENTE_ID = ?`

// ─── Directory listing ────────────────────────────────────────────────────────

// selectDirectorioCols extends selectClienteCols with the aggregated balance.
//
// Saldo source — MSP_SALDOS_VENTAS cache (USER-APPROVED EXCEPTION 2026-06-16):
// Per-client total saldo is SUM(MSP_SALDOS_VENTAS.SALDO) over non-cancelled
// cargos (CARGO_CANCELADO='N'), grouped by CLIENTE_ID.
//
// This is an explicit, user-approved exception to CLAUDE.md hard rule #1
// ("saldo nativo, nunca MSP_*"). MSP_SALDOS_VENTAS is a read-model cache OF the
// native cargo facts (one row per cargo/venta, maintained by cobranza via
// MSP_RECOMPUTE_SALDO_VENTA) — not invented business logic. It encodes the same
// native formula the directory used before:
//
//	SUM(IMPORTE+IMPUESTO WHERE TIPO_IMPTE='C') − SUM(IMPORTE WHERE TIPO_IMPTE='R')
//
// over non-cancelled cargos (CONCEPTO_CC_ID=5). VERIFIED live 2026-06-16 to match
// the native formula exactly (cliente 12440: 504666.60 = 504666.60).
//
// Rationale: the native aggregation scans the full ~3.4M-row IMPORTES_DOCTOS_CC,
// making the unfiltered global directory path ~56s. MSP_SALDOS_VENTAS is small
// (~103k rows) and indexed on CLIENTE_ID → the same path is now sub-second.
//
// CAST(SUM) is mandatory because the firebirdsql v0.9.19 driver returns unscaled
// *big.Int for aggregates; firebird.ScanDecimal(raw, 2) reads the scaled result.
const selectDirectorioCols = selectClienteCols + `,
	COALESCE((
		SELECT CAST(SUM(s.SALDO) AS NUMERIC(18,2))
		FROM MSP_SALDOS_VENTAS s
		WHERE s.CLIENTE_ID = c.CLIENTE_ID
		  AND s.CARGO_CANCELADO = 'N'
	), 0) AS SALDO_TOTAL`

// queryListarDirectorioBase is the SELECT + FROM for the directory listing.
// ESTATUS IN ('A','B') excludes vendor-route pseudo-clients (ESTATUS='V') and
// cancelled clients (ESTATUS='C'). ESTATUS='B' (bloqueados) is intentionally
// kept — 63% of clients with saldo are bloqueados and cobradores need to find
// them.
const queryListarDirectorioBase = `
SELECT FIRST ? ` + selectDirectorioCols + clienteFromClause + `
WHERE c.ESTATUS IN ('A', 'B')`

// queryListarDirectorioInner is the unbounded SELECT (no FIRST ?) used when
// the ConSaldo derived-table wrapper is needed. The derived table applies
// WHERE SALDO_TOTAL > 0 so the outer FIRST ? operates on pre-filtered rows.
const queryListarDirectorioInner = `
SELECT ` + selectDirectorioCols + clienteFromClause + `
WHERE c.ESTATUS IN ('A', 'B')`

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
// is returned. ESTATUS IN ('A','B') matches the paginated listing's rationale.
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

// queryResumenFichaTotales returns the aggregate financial summary for a client.
//
// TotalComprado = SUM of IMPORTE+IMPUESTO of all cargos concepto 5 (native).
// TotalAbonado  = SUM of IMPORTE of all abonos (TIPO_IMPTE='R') (native).
// NumVentas     = COUNT DISTINCT of cargo DOCTO_CC_ACR_ID.
// NumPagos      = COUNT DISTINCT of TIPO_IMPTE='R' rows.
//
// NOTE: SaldoTotal is NOT derived here from TotalComprado − TotalAbonado anymore.
// It is sourced from the MSP_SALDOS_VENTAS cache (queryResumenFichaSaldo below)
// so the ficha saldo equals the directory saldo exactly. TotalComprado /
// TotalAbonado / PctLiquidado stay native (PctLiquidado = abonado/comprado, not
// derived from saldo, so it is unaffected).
//
// All SUM casts are required (v0.9.19 driver scale bug).
const queryResumenFichaTotales = `
SELECT
  CAST(COALESCE(SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END), 0) AS NUMERIC(18,2)) AS TOTAL_COMPRADO,
  CAST(COALESCE(SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE ELSE 0 END), 0) AS NUMERIC(18,2)) AS TOTAL_ABONADO,
  COUNT(DISTINCT CASE WHEN i.TIPO_IMPTE = 'C' THEN i.DOCTO_CC_ACR_ID END)                       AS NUM_VENTAS,
  COUNT(DISTINCT CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPTE_DOCTO_CC_ID END)                      AS NUM_PAGOS
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
WHERE cargo.CLIENTE_ID = ?
  AND cargo.CONCEPTO_CC_ID = 5
  AND cargo.CANCELADO = 'N'
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

// queryAbonosPorMes returns monthly payment totals for the trailing chart.
// Grouped by (EXTRACT YEAR, EXTRACT MONTH) of the pago document's FECHA.
// Ordered ascending so the UI can render a left-to-right timeline.
const queryAbonosPorMes = `
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
  AND i.CANCELADO = 'N'
GROUP BY EXTRACT(YEAR FROM abono.FECHA), EXTRACT(MONTH FROM abono.FECHA)
ORDER BY ANIO, MES`

// queryCompradoVsAbonado returns paired (comprado, abonado) monthly data for
// the dual-series ficha chart.
//
// Comprado is bucketed by cargo.FECHA (the sale date, a DATE column).
// Abonado  is bucketed by abono.FECHA (the payment date, also DATE).
// Two aggregations are UNION-ed then re-grouped at the outer level.
//
// VERIFY-AT-CHECKPOINT: DOCTOS_CC.FECHA is type DATE (not TIMESTAMP) per B2
// research. EXTRACT(YEAR/MONTH FROM DATE) is valid in Firebird — confirm no
// cast needed.
const queryCompradoVsAbonado = `
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
    AND i.CANCELADO = 'N'
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
    AND i.CANCELADO = 'N'
) t
GROUP BY ANIO, MES
ORDER BY ANIO, MES`

// ─── ListarVentas ─────────────────────────────────────────────────────────────

// selectVentaClienteCols is the projection for a VentaCliente row.
// tipo is derived from DOCTOS_PV_COBROS: FORMA_COBRO_ID=71 means CREDITO,
// otherwise CONTADO (validated in B4 research). EXISTS subquery is used
// to avoid inflating the result set.
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
	pv.IMPORTE_NETO,
	CASE WHEN EXISTS (
		SELECT 1 FROM DOCTOS_PV_COBROS cob
		WHERE cob.DOCTO_PV_ID = pv.DOCTO_PV_ID AND cob.FORMA_COBRO_ID = 71
	) THEN 'CREDITO' ELSE 'CONTADO' END AS TIPO,
	COALESCE((
		SELECT CAST(
			MAXVALUE(
				SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END)
				- SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE ELSE 0 END),
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
	), 0) AS NUM_PAGOS`

const queryListarVentasBase = `
SELECT FIRST ? ` + selectVentaClienteCols + `
FROM DOCTOS_PV pv
WHERE pv.CLIENTE_ID = ?
  AND pv.TIPO_DOCTO IN ('V', 'P')
  AND pv.ESTATUS = 'N'`

// ─── ObtenerVentaDetalle ──────────────────────────────────────────────────────

// queryVentaHeader fetches the venta header only — used to establish the sale
// exists before fetching children. Reuses selectVentaClienteCols shape.
const queryVentaHeader = `
SELECT ` + selectVentaClienteCols + `
FROM DOCTOS_PV pv
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
// UNIDADES scale=5, PRECIO_UNITARIO scale=6, PRECIO_TOTAL_NETO scale=2,
// PCTJE_DSCTO scale=6 — scanned with appropriate ScanDecimal calls.
const queryProductos = `
SELECT
	det.ARTICULO_ID,
	a.NOMBRE,
	det.UNIDADES,
	det.PRECIO_UNITARIO,
	det.PRECIO_TOTAL_NETO,
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
	CAST(COALESCE(i.IMPORTE, 0) AS NUMERIC(18,2)) AS IMPORTE,
	COALESCE((
		SELECT fc.NOMBRE
		FROM FORMAS_COBRO_DOCTOS fcd
		JOIN FORMAS_COBRO fc ON fc.FORMA_COBRO_ID = fcd.FORMA_COBRO_ID
		WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC'
		  AND fcd.DOCTO_ID = pago.DOCTO_CC_ID
		ROWS 1
	), '') AS FORMA_COBRO,
	des.DOCTO_DEST_ID
FROM DOCTOS_ENTRE_SIS des
JOIN IMPORTES_DOCTOS_CC i   ON i.DOCTO_CC_ACR_ID  = des.DOCTO_DEST_ID
JOIN DOCTOS_CC pago         ON pago.DOCTO_CC_ID   = i.DOCTO_CC_ID
WHERE des.CLAVE_SIS_FTE  = 'PV'
  AND des.CLAVE_SIS_DEST = 'CC'
  AND des.DOCTO_FTE_ID   = ?
  AND pago.NATURALEZA_CONCEPTO = 'R'
  AND pago.CANCELADO     = 'N'
  AND i.TIPO_IMPTE       = 'R'
  AND i.CANCELADO        = 'N'
ORDER BY pago.FECHA`

// ─── BuscarClienteIDsBasico ───────────────────────────────────────────────────

// queryBuscarBasico is the degraded-fallback SQL LIKE search over CLIENTES.NOMBRE.
// Used when the Bleve in-process index is not yet ready.
// UPPER() on both sides makes the comparison case-insensitive in Firebird.
// Restricts to ESTATUS IN ('A','B') to exclude vendedor-ruta pseudo-clientes
// (ESTATUS='V') and cancelled ones (ESTATUS='C').
//
// VERIFY-AT-CHECKPOINT: decide whether to include ESTATUS='B' (bloqueados)
// in search results. Currently included because bloqueados still have saldo
// and the cobrador / office may need to find them. Remove if UX wants only
// activos.
const queryBuscarBasico = `
SELECT FIRST ? c.CLIENTE_ID
FROM CLIENTES c
WHERE c.ESTATUS IN ('A', 'B')
  AND UPPER(c.NOMBRE) LIKE UPPER(?)
ORDER BY c.NOMBRE, c.CLIENTE_ID`

// ─── LeerDocumentosBusqueda ───────────────────────────────────────────────────

// queryLeerDocumentos fetches the text fields needed to build SearchDocs for
// the in-process Bleve index. Returns one row per active client.
//
// Texto is assembled in Go (not in SQL) to allow the repo to decode each
// Win1252 column individually and then concatenate UTF-8 strings.
//
// ESTATUS IN ('A','B') — same rationale as BuscarBasico above.
//
// Address columns (NOMBRE_CALLE, COLONIA, POBLACION) are CHARACTER SET NONE in
// Microsip (raw Windows-1252 bytes). COALESCE(col, ”) is intentionally OMITTED
// here because mixing a NONE column with a UTF-8 literal causes Firebird to
// attempt transliteration, which fails on Win1252 bytes such as ñ (0xF1).
// Instead the bare nullable columns are selected and scanned as firebird.Win1252,
// which handles NULL → "" internally at scan time.
const queryLeerDocumentos = `
SELECT
	c.CLIENTE_ID,
	c.NOMBRE,
	d.NOMBRE_CALLE,
	d.COLONIA,
	d.POBLACION
FROM CLIENTES c
LEFT JOIN DIRS_CLIENTES d ON d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
WHERE c.ESTATUS IN ('A', 'B')
ORDER BY c.CLIENTE_ID`
