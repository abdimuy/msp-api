// Package analyticsfb implements the Firebird-backed repositories for the
// analytics module. It satisfies both outbound.WinbackRepo (reads/writes our
// MSP_AN_* tables, CHARACTER SET UTF8) and outbound.MicrosipReader (read-only
// over legacy Microsip tables, Win1252-encoded text).
//
//nolint:misspell // Spanish domain vocabulary (candidato, cohorte, zona, etc.) by project convention.
package analyticsfb

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
	UPDATED_AT`

// updateCandidato updates the mutable fields of an existing candidato row.
//
// CRITICAL: EN_CONTROL and COHORTE_FECHA are intentionally absent from the
// SET clause. They are set once at creation (INSERT) and must survive
// subsequent refreshes unchanged so the A/B flag and cohort date remain stable.
//
// Note on MERGE: the nakagami/firebirdsql driver returns SQL error -804
// ("Data type unknown") when parameters appear inside the USING SELECT clause
// of MERGE (e.g. `USING (SELECT ? FROM RDB$DATABASE)`). This is a known driver
// limitation. We fall back to the established UPDATE-then-INSERT pattern which
// avoids the parameterized USING clause entirely.
//
// Positional args (11):
//
//	NOMBRE, ZONA, TELEFONO, FECHA_ULTIMA_COMPRA, FRECUENCIA,
//	MONETARY, SALDO, POR_LIQUIDAR_PCT, NEXT_BEST_PRODUCT, UPDATED_AT,
//	CLIENTE_ID (WHERE)
const updateCandidato = `
UPDATE MSP_AN_WINBACK_CANDIDATOS
SET
  NOMBRE              = ?,
  ZONA                = ?,
  TELEFONO            = ?,
  FECHA_ULTIMA_COMPRA = ?,
  FRECUENCIA          = ?,
  MONETARY            = ?,
  SALDO               = ?,
  POR_LIQUIDAR_PCT    = ?,
  NEXT_BEST_PRODUCT   = ?,
  UPDATED_AT          = ?
WHERE CLIENTE_ID = ?`

// insertCandidato inserts a new candidato row with ALL columns explicit
// (CLAUDE.md Rule 1: no DB-side defaults for ID, CREATED_AT, UPDATED_AT).
//
// Positional args (15):
//
//	ID, CLIENTE_ID, NOMBRE, ZONA, TELEFONO, FECHA_ULTIMA_COMPRA, FRECUENCIA,
//	MONETARY, SALDO, POR_LIQUIDAR_PCT, NEXT_BEST_PRODUCT, EN_CONTROL,
//	COHORTE_FECHA, CREATED_AT, UPDATED_AT
const insertCandidato = `
INSERT INTO MSP_AN_WINBACK_CANDIDATOS
  (ID, CLIENTE_ID, NOMBRE, ZONA, TELEFONO, FECHA_ULTIMA_COMPRA,
   FRECUENCIA, MONETARY, SALDO, POR_LIQUIDAR_PCT, NEXT_BEST_PRODUCT,
   EN_CONTROL, COHORTE_FECHA, CREATED_AT, UPDATED_AT)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
// DIRS_CLIENTES.TELEFONO1, ARTICULOS.NOMBRE) are Win1252-encoded (CHARACTER
// SET NONE / ISO8859_1). Scan targets for those columns must be firebird.Win1252
// values, not plain string, so the driver round-trips the bytes correctly.
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

// leerAnclasBase returns per-cliente RFM + saldo + NBP for ALL clients with
// at least one DOCTOS_PV sale. When a `since` watermark is used the caller
// appends `AND pv.FECHA >= ?` before the GROUP BY. The query uses
// LEFT JOIN to DOCTOS_CC so clients with no credit history still appear with
// saldo=0 and por_liquidar_pct=0.
//
// Column order (must match anclaRowRaw.scanFrom):
//
//	1  cliente_id
//	2  nombre         (Win1252)
//	3  zona           (Win1252, may be empty)
//	4  telefono       (Win1252, may be NULL)
//	5  fecha_ultima_compra
//	6  frecuencia
//	7  monetary
//	8  saldo          (CASE guarded ≥ 0)
//	9  por_liquidar   (NUMERIC(5,2), 0–100)
//	10 next_best_product (Win1252, may be '')
const leerAnclasBase = `
SELECT
  pv.CLIENTE_ID,
  c.NOMBRE                                                             AS NOMBRE,
  COALESCE(z.NOMBRE, '')                                              AS ZONA,
  d.TELEFONO1                                                         AS TELEFONO,
  MAX(pv.FECHA)                                                       AS FECHA_ULTIMA_COMPRA,
  COUNT(DISTINCT pv.DOCTO_PV_ID)                                      AS FRECUENCIA,
  CAST(SUM(pv.IMPORTE_NETO) AS NUMERIC(18,2))                        AS MONETARY,
  CASE WHEN CAST(
    SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END) -
    SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE             ELSE 0 END)
    AS NUMERIC(18,2)) > 0
    THEN CAST(
      SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END) -
      SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE             ELSE 0 END)
      AS NUMERIC(18,2))
    ELSE 0
  END                                                                  AS SALDO,
  CASE WHEN CAST(
    SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END)
    AS NUMERIC(18,2)) > 0
    AND (SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END) -
         SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE             ELSE 0 END)) > 0
    THEN CAST(
      (SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END) -
       SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN i.IMPORTE             ELSE 0 END))
      / SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN i.IMPORTE + i.IMPUESTO ELSE 0 END)
      * 100 AS NUMERIC(5,2))
    ELSE 0
  END                                                                  AS POR_LIQUIDAR_PCT,
  COALESCE((
    SELECT FIRST 1 a2.NOMBRE
    FROM DOCTOS_PV pv2
    JOIN DOCTOS_PV_DET det2 ON det2.DOCTO_PV_ID = pv2.DOCTO_PV_ID
    JOIN ARTICULOS a2        ON a2.ARTICULO_ID   = det2.ARTICULO_ID
    WHERE pv2.CLIENTE_ID = pv.CLIENTE_ID
      AND pv2.TIPO_DOCTO IN ('V', 'P')
      AND pv2.ESTATUS = 'N'
    GROUP BY a2.NOMBRE
    ORDER BY COUNT(*) DESC, a2.NOMBRE ASC
  ), '')                                                               AS NEXT_BEST_PRODUCT
FROM DOCTOS_PV pv
JOIN CLIENTES c ON c.CLIENTE_ID = pv.CLIENTE_ID
LEFT JOIN ZONAS_CLIENTES z ON z.ZONA_CLIENTE_ID = c.ZONA_CLIENTE_ID
LEFT JOIN DIRS_CLIENTES d ON d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN (
  SELECT i.TIPO_IMPTE, i.IMPORTE, i.IMPUESTO, dc.CLIENTE_ID
  FROM IMPORTES_DOCTOS_CC i
  JOIN DOCTOS_CC dc ON dc.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
  WHERE dc.CONCEPTO_CC_ID = 5 /* concepto 5 = cargo crédito (microsipConceptoCCCargo) */
    AND dc.CANCELADO = 'N'
    AND i.CANCELADO = 'N'
) i ON i.CLIENTE_ID = pv.CLIENTE_ID
WHERE pv.CLIENTE_ID IS NOT NULL
  AND pv.TIPO_DOCTO IN ('V', 'P')
  AND pv.ESTATUS = 'N'`

// leerAnclasGroupBy is appended after any WHERE predicates.
// COALESCE(z.NOMBRE, ”) mirrors the SELECT expression so unzoned clients
// (z.NOMBRE IS NULL) are aggregated into the same ” bucket rather than
// being grouped separately by NULL vs. ” (which Firebird treats as distinct).
const leerAnclasGroupBy = `
GROUP BY pv.CLIENTE_ID, c.NOMBRE, COALESCE(z.NOMBRE, ''), d.TELEFONO1`
