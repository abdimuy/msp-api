// Package analyticsfb implements the Firebird-backed repositories for the
// analytics module. It satisfies both outbound.WinbackRepo (reads/writes our
// MSP_AN_* tables, CHARACTER SET UTF8) and outbound.MicrosipReader (read-only
// over legacy Microsip tables, Win1252-encoded text).
//
//nolint:misspell // Spanish domain vocabulary (candidato, cohorte, zona, etc.) by project convention.
package analyticsfb

import "fmt"

// ─── MSP_AN_WINBACK_CANDIDATOS ────────────────────────────────────────────────

// candidatoCols is the canonical SELECT column list for MSP_AN_WINBACK_CANDIDATOS.
// The order must match candidatoRowRaw.scanFrom exactly.
const candidatoCols = `
	ID,
	CLIENTE_ID,
	NOMBRE,
	ZONA,
	TELEFONO,
	FECHA_ULTIMA_COMPRA,
	FRECUENCIA,
	MONETARY,
	SALDO,
	POR_LIQUIDAR_PCT,
	NEXT_BEST_PRODUCT,
	EN_CONTROL,
	COHORTE_FECHA,
	CREATED_AT,
	UPDATED_AT,
	FECHA_ULTIMO_PAGO,
	NUM_PAGOS,
	CADENCIA_DIAS,
	DIAS_ATRASO_PROM,
	PCT_PAGOS_A_TIEMPO,
	FECHA_PROX_PAGO,
	MONTO_PROX_PAGO,
	PAGOS_90D,
	FECHA_PRIMER_CARGO,
	FECHA_PRIMER_VENTA,
	FECHA_ULTIMA_VENTA,
	VENTAS_MESES_DISTINTOS,
	MONETARY_V_PROM`

// Note on upsert strategy: the nakagami/firebirdsql driver returns SQL error
// -804 ("Data type unknown") when parameters appear inside the USING SELECT
// clause of MERGE. We therefore use EXECUTE BLOCK with typed input parameters
// (see repo.go buildUpsertBlock), which batches N rows into a single statement
// and avoids the MERGE limitation. The UPDATE inside the block omits EN_CONTROL
// and COHORTE_FECHA so they are preserved from the original INSERT across
// subsequent refreshes.

const selectCandidatoBase = `SELECT` + candidatoCols + `
FROM MSP_AN_WINBACK_CANDIDATOS`

const countCandidatoBase = `SELECT COUNT(*) FROM MSP_AN_WINBACK_CANDIDATOS`

const selectControlFlags = `SELECT CLIENTE_ID, EN_CONTROL FROM MSP_AN_WINBACK_CANDIDATOS`

// ─── MSP_AN_REFRESH_STATE ─────────────────────────────────────────────────────

const selectRefreshState = `
SELECT JOB, LAST_WATERMARK, LAST_RUN_AT
FROM MSP_AN_REFRESH_STATE
WHERE JOB = ?`

// updateRefreshState updates LAST_WATERMARK and LAST_RUN_AT for an existing
// row. Returns 0 RowsAffected when the row doesn't exist (caller inserts).
//
// Positional args (3): LAST_WATERMARK, LAST_RUN_AT, JOB (WHERE).
const updateRefreshState = `
UPDATE MSP_AN_REFRESH_STATE
SET LAST_WATERMARK = ?,
    LAST_RUN_AT    = ?
WHERE JOB = ?`

// insertRefreshState inserts a new refresh state row.
//
// Positional args (3): JOB, LAST_WATERMARK, LAST_RUN_AT.
const insertRefreshState = `
INSERT INTO MSP_AN_REFRESH_STATE (JOB, LAST_WATERMARK, LAST_RUN_AT)
VALUES (?, ?, ?)`

// ─── Microsip read queries ─────────────────────────────────────────────────────
//
// All Microsip text columns (CLIENTES.NOMBRE, ZONAS_CLIENTES.NOMBRE,
// DIRS_CLIENTES.TELEFONO1, ARTICULOS.NOMBRE) are CHARACTER SET NONE (raw
// Win1252 bytes in the DB). Because the Firebird connection uses charset=UTF8,
// the server automatically transliterates those columns to UTF-8 on the wire
// before the driver receives them. Scan targets are therefore plain string /
// sql.NullString — NOT firebird.Win1252. Using Win1252.Scan on already-UTF-8
// bytes would apply a second Win1252→UTF-8 decode, producing mojibake (e.g.
// "Ã'" instead of "ñ"). NULL results from LEFT JOINs are mapped to "" in Go.
//
// COALESCE with string literals is intentionally OMITTED: a '' literal in a
// UTF-8 connection forces Firebird to coerce the NONE column to UTF-8 at the
// point of the expression, which can fail with "Malformed string" on Win1252
// bytes (e.g. accented characters). NULL → "" is handled in Go instead.
//
// Money aggregates use CAST(SUM(…) AS NUMERIC(18,2)) to avoid the nakagami
// driver bug where SUM over NUMERIC returns *big.Int without applying the
// scale (project memory: reference_firebirdsql_sum_scale).
//
// RFM anchored on DOCTOS_PV (ALL sales: contado + crédito), NOT DOCTOS_CC
// (credit-only). Per dictionary and project memory: ~7,200 clients (16% of the
// base) would be mis-dated if RFM used DOCTOS_CC.
//
// ESTATUS='N' filter: the top-level dictionary section states
// "Ventas = TIPO_DOCTO IN ('V','P') AND ESTATUS='N' (421,575)" — we honor
// that spec. The column definition in section 5.1 lists A/C/P; 'N' may be
// the Microsip code for "Normal" (aplicado). The difference vs ESTATUS<>'C'
// is small (422,003) so this is a safe conservative filter.
//
// ZONA: derived from CLIENTES.ZONA_CLIENTE_ID → ZONAS_CLIENTES.NOMBRE.
// ~100% coverage in dev (45,215/45,216 clientes with zona).
//
// Saldo: SUM(TIPO_IMPTE='C': IMPORTE+IMPUESTO) − SUM(TIPO_IMPTE='R': IMPORTE)
// per cargo DOCTOS_CC (concepto 5, not cancelled). Grouped to cliente level.
// Guarded against < 0 by CASE WHEN ... > 0 THEN ... ELSE 0 END.
//
// NextBestProduct (NBP): Most-frequently-purchased ARTICULOS.NOMBRE by client,
// from DOCTOS_PV_DET + ARTICULOS. Implemented as a correlated subquery rather
// than a full join to avoid inflating the main GROUP BY and to keep the query
// single-pass. In cases where the subquery finds no result, NBP is ''.

// leerAnclasBase is the fixed, scalable version of the winback anchor query.
//
// Previous implementation had two critical bugs:
//  1. Row explosion: LEFT JOIN to IMPORTES_DOCTOS_CC/DOCTOS_CC at the
//     DOCTO_PV row granularity produced P×K rows per client (P sales × K
//     credit-lines), inflating MONETARY and SALDO aggregates and causing
//     query times >10 min on the full DB.
//  2. Correlated NBP subquery executed once per client (43 k times).
//
// Fix overview:
//
//   (A) saldo_cte: ONE row per client from MSP_SALDOS_VENTAS (the
//       materialized cache maintained by triggers in migration 000010).
//       No FECHA filter — saldo is current-state, not point-in-time.
//       POR_LIQUIDAR_PCT = SUM(SALDO) / NULLIF(SUM(PRECIO_TOTAL),0) * 100,
//       floored at 0.
//
//   (B) NBP: two-pass grouped aggregation (nbp_freq → nbp_max → nbp).
//       nbp_freq groups (CLIENTE_ID, ARTICULO_NOMBRE) → CNT in one scan.
//       nbp_max finds MAX(CNT) per CLIENTE_ID. nbp joins the two to get
//       the top article, using MIN(ARTICULO_NOMBRE) for alphabetic tie-break.
//       Avoids window functions (which force full materialization in
//       Firebird 4 before streaming any rows, causing >20s delays).
//
//   (C) rfm CTE: unchanged — MAX(FECHA), COUNT(DISTINCT), SUM(IMPORTE_NETO)
//       from DOCTOS_PV grouped by CLIENTE_ID.
//
// Watermark / `since` handling:
//   When since != nil the caller injects an extra AND clause into BOTH rfm
//   and nbp_freq (see repo.go). The saldo_cte has NO FECHA filter (current-state).
//
// Column order (must match anclaRowRaw.scanFrom exactly):
//
//	1  cliente_id
//	2  nombre         (string; UTF-8 via server-side transliteration of NONE col)
//	3  zona           (sql.NullString; NULL when client has no zona — Go maps to "")
//	4  telefono       (sql.NullString; NULL when no primary address — Go maps to "")
//	5  fecha_ultima_compra
//	6  frecuencia
//	7  monetary
//	8  saldo          (floored at 0)
//	9  por_liquidar   (NUMERIC(5,2), 0–100)
//	10 next_best_product (sql.NullString; NULL when no purchases — Go maps to "")
//	11 fecha_ultimo_pago (TIMESTAMP, may be NULL)
//	12 fecha_primer_cargo (TIMESTAMP, may be NULL)
//	13 fecha_primer_venta (TIMESTAMP, may be NULL)
//	14 fecha_ultima_venta (TIMESTAMP, may be NULL)
//	15 ventas_meses_distintos (INTEGER, may be NULL)
//	16 monetary_v_prom (NUMERIC(18,2), may be NULL)

// leerAnclasRFMBase is the opening of the WITH block through the end of the
// rfm CTE. The caller appends an optional FECHA predicate then leerAnclasRFMClose.
const leerAnclasRFMBase = `
WITH rfm AS (
  SELECT
    pv.CLIENTE_ID,
    MAX(pv.FECHA)                              AS FECHA_ULTIMA_COMPRA,
    COUNT(DISTINCT pv.DOCTO_PV_ID)             AS FRECUENCIA,
    CAST(SUM(pv.IMPORTE_NETO) AS NUMERIC(18,2)) AS MONETARY
  FROM DOCTOS_PV pv
  WHERE pv.CLIENTE_ID IS NOT NULL
    AND pv.TIPO_DOCTO IN ('V', 'P')
    AND pv.ESTATUS = 'N'`

// leerAnclasRFMClose closes the rfm CTE, opens saldo_cte, ventas_v, then opens nbp_freq.
// saldo_cte and ventas_v have no FECHA filter — both are full-history aggregates
// (current-state read models), consistent with the no-watermark design for saldo.
const leerAnclasRFMClose = `
  GROUP BY pv.CLIENTE_ID
),
saldo_cte AS (
  SELECT
    sv.CLIENTE_ID,
    CASE WHEN CAST(SUM(sv.SALDO) AS NUMERIC(18,2)) > 0
         THEN CAST(SUM(sv.SALDO) AS NUMERIC(18,2))
         ELSE 0
    END                                        AS SALDO,
    CAST(SUM(sv.PRECIO_TOTAL) AS NUMERIC(18,2)) AS PRECIO_TOTAL_SUM,
    MAX(sv.FECHA_ULT_PAGO)                      AS FECHA_ULTIMO_PAGO,
    MIN(sv.FECHA_CARGO)                         AS FECHA_PRIMER_CARGO
  FROM MSP_SALDOS_VENTAS sv
  WHERE sv.CARGO_CANCELADO = 'N'
  GROUP BY sv.CLIENTE_ID
),
ventas_v AS (
  SELECT
    pv.CLIENTE_ID,
    MIN(pv.FECHA)                                                                    AS FECHA_PRIMER_VENTA,
    MAX(pv.FECHA)                                                                    AS FECHA_ULTIMA_VENTA,
    COUNT(DISTINCT (EXTRACT(YEAR FROM pv.FECHA) * 12 + EXTRACT(MONTH FROM pv.FECHA))) AS VENTAS_MESES_DISTINTOS,
    CAST(AVG(CAST(pv.IMPORTE_NETO AS NUMERIC(18,4))) AS NUMERIC(18,2))              AS MONETARY_V_PROM
  FROM DOCTOS_PV pv
  WHERE pv.CLIENTE_ID IS NOT NULL
    AND pv.TIPO_DOCTO = 'V'
    AND pv.ESTATUS = 'N'
  GROUP BY pv.CLIENTE_ID
),`

// leerAnclasNBPBase is the opening of the nbp_freq CTE. The caller appends an
// optional FECHA predicate then leerAnclasNBPClose.
//
// NBP = Next Best Product: the most-frequently-purchased article per client.
// We use a two-step aggregation instead of ROW_NUMBER() OVER PARTITION BY to
// avoid Firebird 4's full-materialization behaviour with window functions
// (which stalls result streaming for 20+ seconds on the full DB).
//
// Step 1 (nbp_freq): group by (CLIENTE_ID, ARTICULO_NOMBRE) → CNT.
// Step 2 (nbp_max):  find MAX(CNT) per CLIENTE_ID.
// Step 3 (nbp):      join the two and pick MIN(ARTICULO_NOMBRE) to break ties.
const leerAnclasNBPBase = `
nbp_freq AS (
  SELECT
    pv.CLIENTE_ID,
    a.NOMBRE                                   AS ARTICULO_NOMBRE,
    COUNT(*)                                   AS CNT
  FROM DOCTOS_PV pv
  JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
  JOIN ARTICULOS a        ON a.ARTICULO_ID   = det.ARTICULO_ID
  WHERE pv.CLIENTE_ID IS NOT NULL
    AND pv.TIPO_DOCTO IN ('V', 'P')
    AND pv.ESTATUS = 'N'`

// leerAnclasNBPClose closes nbp_freq, adds nbp_max and nbp CTEs, then the
// final SELECT joining rfm + saldo_cte + nbp + dimension tables.
const leerAnclasNBPClose = `
  GROUP BY pv.CLIENTE_ID, a.NOMBRE
),
nbp_max AS (
  SELECT CLIENTE_ID, MAX(CNT) AS MAX_CNT
  FROM nbp_freq
  GROUP BY CLIENTE_ID
),
nbp AS (
  SELECT f.CLIENTE_ID, MIN(f.ARTICULO_NOMBRE) AS ARTICULO_NOMBRE
  FROM nbp_freq f
  JOIN nbp_max m ON m.CLIENTE_ID = f.CLIENTE_ID AND m.MAX_CNT = f.CNT
  GROUP BY f.CLIENTE_ID
)
SELECT
  rfm.CLIENTE_ID,
  c.NOMBRE                                                            AS NOMBRE,
  -- Do NOT COALESCE z.NOMBRE with a '' literal: '' is a UTF-8 connection
  -- literal and would force Firebird to coerce the CHARACTER SET NONE column
  -- to UTF-8, potentially causing "Malformed string" errors on Win1252 bytes.
  -- NULL is handled in Go by nullStringVal (returns "").
  z.NOMBRE                                                           AS ZONA,
  d.TELEFONO1                                                        AS TELEFONO,
  rfm.FECHA_ULTIMA_COMPRA,
  rfm.FRECUENCIA,
  rfm.MONETARY,
  COALESCE(sc.SALDO, 0)                                              AS SALDO,
  CASE WHEN COALESCE(sc.PRECIO_TOTAL_SUM, 0) > 0
            AND COALESCE(sc.SALDO, 0) > 0
       THEN CAST(
              sc.SALDO / sc.PRECIO_TOTAL_SUM * 100
              AS NUMERIC(5,2))
       ELSE 0
  END                                                                AS POR_LIQUIDAR_PCT,
  -- Do NOT COALESCE nbp.ARTICULO_NOMBRE with '' for the same NONE-charset
  -- reason. SUBSTRING returns NULL when the argument is NULL; Go maps NULL→"".
  SUBSTRING(nbp.ARTICULO_NOMBRE FROM 1 FOR 120)                      AS NEXT_BEST_PRODUCT,
  sc.FECHA_ULTIMO_PAGO                                               AS FECHA_ULTIMO_PAGO,
  sc.FECHA_PRIMER_CARGO                                              AS FECHA_PRIMER_CARGO,
  vv.FECHA_PRIMER_VENTA                                             AS FECHA_PRIMER_VENTA,
  vv.FECHA_ULTIMA_VENTA                                             AS FECHA_ULTIMA_VENTA,
  vv.VENTAS_MESES_DISTINTOS                                         AS VENTAS_MESES_DISTINTOS,
  vv.MONETARY_V_PROM                                                AS MONETARY_V_PROM
FROM rfm
JOIN CLIENTES c ON c.CLIENTE_ID = rfm.CLIENTE_ID
LEFT JOIN ZONAS_CLIENTES z   ON z.ZONA_CLIENTE_ID = c.ZONA_CLIENTE_ID
LEFT JOIN DIRS_CLIENTES d    ON d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN saldo_cte sc       ON sc.CLIENTE_ID = rfm.CLIENTE_ID
LEFT JOIN ventas_v vv        ON vv.CLIENTE_ID = rfm.CLIENTE_ID
LEFT JOIN nbp                ON nbp.CLIENTE_ID = rfm.CLIENTE_ID`

// leerAnclasBase is the complete query for the since=nil (full-DB) case.
// It is assembled by concatenating the CTE parts with no additional predicates.
const leerAnclasBase = leerAnclasRFMBase + leerAnclasRFMClose + leerAnclasNBPBase + leerAnclasNBPClose

// ─── MSP_AN_CLIENTE_NARRATIVA ─────────────────────────────────────────────────

// selectNarrativa fetches the cache row for one CLIENTE_ID.
// Positional args (1): CLIENTE_ID.
const selectNarrativa = `
SELECT NARRATIVA, RASGOS, INPUT_HASH, MODELO
FROM MSP_AN_CLIENTE_NARRATIVA
WHERE CLIENTE_ID = ?`

// updateNarrativa updates the mutable columns of an existing cache row.
// Positional args (7): NARRATIVA, RASGOS, INPUT_HASH, MODELO, GENERADA_EN,
// UPDATED_AT, CLIENTE_ID (WHERE).
const updateNarrativa = `
UPDATE MSP_AN_CLIENTE_NARRATIVA
SET NARRATIVA   = ?,
    RASGOS      = ?,
    INPUT_HASH  = ?,
    MODELO      = ?,
    GENERADA_EN = ?,
    UPDATED_AT  = ?
WHERE CLIENTE_ID = ?`

// insertNarrativa inserts a new cache row.
// Positional args (9): ID, CLIENTE_ID, NARRATIVA, RASGOS, INPUT_HASH, MODELO,
// GENERADA_EN, CREATED_AT, UPDATED_AT.
const insertNarrativa = `
INSERT INTO MSP_AN_CLIENTE_NARRATIVA
  (ID, CLIENTE_ID, NARRATIVA, RASGOS, INPUT_HASH, MODELO, GENERADA_EN, CREATED_AT, UPDATED_AT)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

// ─── MSP_AN_NARRATIVA_PENDIENTE ───────────────────────────────────────────────

// updatePendiente refreshes an existing queue row's hash and timestamp.
// Positional args (3): INPUT_HASH, ENCOLADA_EN, CLIENTE_ID (WHERE).
const updatePendiente = `
UPDATE MSP_AN_NARRATIVA_PENDIENTE
SET INPUT_HASH  = ?,
    ENCOLADA_EN = ?
WHERE CLIENTE_ID = ?`

// insertPendiente inserts a new queue row.
// Positional args (3): CLIENTE_ID, INPUT_HASH, ENCOLADA_EN.
const insertPendiente = `
INSERT INTO MSP_AN_NARRATIVA_PENDIENTE (CLIENTE_ID, INPUT_HASH, ENCOLADA_EN)
VALUES (?, ?, ?)`

// deletePendiente removes a client from the queue.
// Positional args (1): CLIENTE_ID.
const deletePendiente = `
DELETE FROM MSP_AN_NARRATIVA_PENDIENTE
WHERE CLIENTE_ID = ?`

// ─── Cobranza signals: abono concept filter ────────────────────────────────────
//
// MSP_PAGOS_VENTAS mixes recurring customer payments (abonos) with one-time or
// non-payment transactions (enganches, condonaciones, write-offs, refunds).
// Only abono concepts reflect the customer's actual payment cadence and
// punctuality. All other concepts distort cadence, punctuality %, and
// MONTO_PROX_PAGO if included.
//
// The three recognized abono concepts (94% of rows by volume):
//
//	87327 – Cobranza en ruta   (~1.71M rows): cobrador collects at customer site.
//	  155 – Cobro en mostrador (~320k rows):  customer pays at the store counter.
//	   11 – Cobro              (~13k rows):   generic legacy payment concept.
//
// Excluded (must NOT appear in cadence/punctuality aggregation):
//
//	Enganche:      down-payment concept (24533 and others); recorded once at
//	               sale creation, not a recurring payment — inflates cadence.
//	Condonación:   partial or full debt forgiveness; not a customer payment.
//	Write-off:     bad-debt cancellation; not a customer payment.
//	Refund/credit: reversal concepts; distort the running average importe.
const (
	// conceptoCobranzaRuta is the "Cobranza en ruta" abono concept (1.71M rows).
	// Cobrador collects payment at the customer's location.
	conceptoCobranzaRuta = 87327

	// conceptoCobro155 is the "Cobro en mostrador" abono concept (320k rows).
	// Customer pays at the store counter (mostrador).
	conceptoCobro155 = 155

	// conceptoCobroGenerico is the legacy generic "Cobro" abono concept (13k rows).
	conceptoCobroGenerico = 11
)

// ─── Cobranza signals query ────────────────────────────────────────────────────
//
// leerCobranzaBase / leerCobranzaClose query MSP_PAGOS_VENTAS to compute
// per-client cadence and punctuality facts using a three-CTE windowed approach:
//
//   (1) gaps CTE: LAG(FECHA) OVER (PARTITION BY CLIENTE_ID ORDER BY FECHA)
//       computes consecutive gap in days for each payment.
//   (2) cadencias CTE: AVG(gap_dias) per client → CADENCIA_DIAS, plus
//       AVG(IMPORTE) → MONTO_PROX_PAGO and MAX(FECHA) → ULTIMA_FECHA.
//       HAVING COUNT(*) >= 1 ensures at least one gap exists (i.e. ≥2 payments).
//   (3) atrasos CTE: joins gaps back to cadencias to classify each gap as
//       on-time (≤ cadencia+7d) or late, and computes per-gap lateness.
//   Final SELECT: groups over cadencias + atrasos to produce per-client signals.
//
// Performance: IDX_MSP_PAGOS_CLIENTE_FECHA (CLIENTE_ID, FECHA) covers the
// partition and order key used by the LAG window function. Because window
// functions force a full table materialization in Firebird before streaming,
// the full-lifetime scan of 2.17M rows takes ~2m43s (verified live, dev machine).
// This is acceptable for a nightly/manual refresh job but not for ad-hoc queries.
//
// Concept filter: restricted to abono concepts only (conceptoCobranzaRuta=87327,
// conceptoCobro155=155, conceptoCobroGenerico=11). Enganche, condonaciones,
// write-offs, and refunds are excluded because they are not recurring customer
// payments and would distort cadence, punctuality %, and MONTO_PROX_PAGO.
//
// Driver gotcha (reference_firebirdsql_sum_scale): AVG() over NUMERIC returns
// unscaled *big.Int. Pattern: CAST(AVG(CAST(col AS NUMERIC(18,4))) AS NUMERIC(N,0)).
//
// Watermark handling: no watermark is applied — the full payment history is always
// scanned so cadence and punctuality are computed over the client's lifetime.
//
// Column order (must match cobranzaRowRaw.scanFrom exactly):
//
//	1  cliente_id
//	2  num_pagos          (NUM_GAPS + 1: total payments including the first)
//	3  cadencia_dias      (INTEGER avg gap)
//	4  dias_atraso_prom   (INTEGER avg positive lateness)
//	5  pct_pagos_a_tiempo (NUMERIC(5,2))
//	6  ultima_fecha       (TIMESTAMP, raw last payment date for Go-side next-pago derivation)
//	7  monto_prox_pago    (NUMERIC(18,2) avg importe)
//	8  pagos_90d          (INTEGER COALESCE to 0; param: cutoff date 90 days before refresh_now)

// leerCobranzaBase opens the WITH block and the gaps CTE through its WHERE clause.
// The caller appends leerCobranzaClose. Built via fmt.Sprintf so the abono concept
// constants are referenced directly, keeping the SQL literals and the named
// constants in sync at compile time.
//
//nolint:gochecknoglobals // query fragment; value is immutable after init.
var leerCobranzaBase = fmt.Sprintf(`
WITH gaps AS (
  SELECT
    CLIENTE_ID,
    FECHA,
    IMPORTE,
    DATEDIFF(DAY, LAG(FECHA) OVER (PARTITION BY CLIENTE_ID ORDER BY FECHA), FECHA) AS GAP_DIAS
  FROM MSP_PAGOS_VENTAS
  WHERE CANCELADO = 'N'
    AND APLICADO  = 'S'
    AND CONCEPTO_CC_ID IN (%d, %d, %d)`,
	conceptoCobranzaRuta, conceptoCobro155, conceptoCobroGenerico)

// leerCobranzaClose closes the gaps CTE, adds cadencias (HAVING ≥1 gap → ≥2
// payments), atrasos and pagos90, then the per-client SELECT — and finally
// UNION ALLs a cheap single-payment branch so clients with exactly one
// qualifying payment are NOT dropped (bug #2a). The single-payment branch emits
// NUM_PAGOS=1, ULTIMA_FECHA, MONTO and a 0/1 PAGOS_90D, with CADENCIA / DIAS_ATRASO
// = 0 and PCT NULL (no cadence derivable from one payment).
//
// Two positional ? params (both bound to the same cutoff): the pagos90 CTE and
// the single-payment branch's trailing-90-day count.
//
// Column order (must match cobranzaRowRaw.scanFrom exactly):
//
//	1  cliente_id
//	2  num_pagos          (NUM_GAPS + 1 ≥2-payment branch; literal 1 single-payment branch)
//	3  cadencia_dias      (INTEGER avg gap; 0 for single-payment)
//	4  dias_atraso_prom   (INTEGER avg positive lateness; 0 for single-payment)
//	5  pct_pagos_a_tiempo (NUMERIC(5,2); NULL for single-payment)
//	6  ultima_fecha       (TIMESTAMP, last payment date)
//	7  monto_prox_pago    (NUMERIC(18,2) avg importe)
//	8  pagos_90d          (INTEGER COALESCE to 0; param: cutoff date 90 days before refresh_now)
//
//nolint:gochecknoglobals // query fragment; value is immutable after init.
var leerCobranzaClose = fmt.Sprintf(`
),
cadencias AS (
  SELECT
    CLIENTE_ID,
    COUNT(*)                                                                      AS NUM_GAPS,
    CAST(AVG(CAST(GAP_DIAS AS NUMERIC(18,4))) AS NUMERIC(10,0))                  AS CADENCIA_DIAS,
    CAST(AVG(CAST(IMPORTE  AS NUMERIC(18,4))) AS NUMERIC(18,2))                  AS AVG_IMPORTE,
    MAX(FECHA)                                                                    AS ULTIMA_FECHA
  FROM gaps
  WHERE GAP_DIAS IS NOT NULL
  GROUP BY CLIENTE_ID
  HAVING COUNT(*) >= 1
),
atrasos AS (
  SELECT
    g.CLIENTE_ID,
    CASE WHEN g.GAP_DIAS > (c.CADENCIA_DIAS + 7)
         THEN g.GAP_DIAS - c.CADENCIA_DIAS
         ELSE 0 END                                                               AS ATRASO_DIAS,
    CASE WHEN g.GAP_DIAS <= (c.CADENCIA_DIAS + 7) THEN 1 ELSE 0 END              AS A_TIEMPO
  FROM gaps g
  JOIN cadencias c ON c.CLIENTE_ID = g.CLIENTE_ID
  WHERE g.GAP_DIAS IS NOT NULL
),
pagos90 AS (
  SELECT CLIENTE_ID, COUNT(*) AS PAGOS_90D
  FROM MSP_PAGOS_VENTAS
  WHERE CANCELADO = 'N'
    AND APLICADO  = 'S'
    AND CONCEPTO_CC_ID IN (%d, %d, %d)
    AND FECHA >= ?
  GROUP BY CLIENTE_ID
)
SELECT
  c.CLIENTE_ID,
  (c.NUM_GAPS + 1)                                                                AS NUM_PAGOS,
  c.CADENCIA_DIAS,
  CAST(AVG(CAST(a.ATRASO_DIAS AS NUMERIC(18,4))) AS NUMERIC(10,0))               AS DIAS_ATRASO_PROM,
  CAST(
    100.0 * SUM(a.A_TIEMPO) / NULLIF(CAST(COUNT(*) AS NUMERIC(18,4)), 0)
    AS NUMERIC(5,2)
  )                                                                               AS PCT_PAGOS_A_TIEMPO,
  c.ULTIMA_FECHA                                                                  AS ULTIMA_FECHA,
  c.AVG_IMPORTE                                                                   AS MONTO_PROX_PAGO,
  COALESCE(p90.PAGOS_90D, 0)                                                      AS PAGOS_90D
FROM cadencias c
JOIN atrasos a ON a.CLIENTE_ID = c.CLIENTE_ID
LEFT JOIN pagos90 p90 ON p90.CLIENTE_ID = c.CLIENTE_ID
GROUP BY c.CLIENTE_ID, c.NUM_GAPS, c.CADENCIA_DIAS, c.AVG_IMPORTE, c.ULTIMA_FECHA, p90.PAGOS_90D
UNION ALL
SELECT
  CLIENTE_ID,
  CAST(1 AS BIGINT)                                                               AS NUM_PAGOS,
  CAST(0 AS NUMERIC(10,0))                                                        AS CADENCIA_DIAS,
  CAST(0 AS NUMERIC(10,0))                                                        AS DIAS_ATRASO_PROM,
  CAST(NULL AS NUMERIC(5,2))                                                      AS PCT_PAGOS_A_TIEMPO,
  MAX(FECHA)                                                                      AS ULTIMA_FECHA,
  CAST(AVG(CAST(IMPORTE AS NUMERIC(18,4))) AS NUMERIC(18,2))                      AS MONTO_PROX_PAGO,
  CAST(SUM(CASE WHEN FECHA >= ? THEN 1 ELSE 0 END) AS BIGINT)                     AS PAGOS_90D
FROM MSP_PAGOS_VENTAS
WHERE CANCELADO = 'N'
  AND APLICADO  = 'S'
  AND CONCEPTO_CC_ID IN (%d, %d, %d)
GROUP BY CLIENTE_ID
HAVING COUNT(*) = 1`,
	conceptoCobranzaRuta, conceptoCobro155, conceptoCobroGenerico,
	conceptoCobranzaRuta, conceptoCobro155, conceptoCobroGenerico)
