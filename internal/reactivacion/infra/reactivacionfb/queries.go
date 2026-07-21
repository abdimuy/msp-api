// Package reactivacionfb implements the Firebird-backed repositories for the
// reactivación module. It satisfies:
//
//   - outbound.CohorteRepo: reads/writes MSP_RX_COHORTE (CHARACTER SET UTF8 —
//     no Win1252 decoding).
//   - outbound.UniversoReader: read-only access to the Microsip read-model
//     (MSP_SALDOS_VENTAS, DOCTOS_PV, CLIENTES, DIRS_CLIENTES). Microsip text
//     columns are CHARACTER SET NONE (raw Win1252 bytes), but because the
//     connection uses charset=UTF8 the server transliterates them to UTF-8 on
//     the wire — scan targets are plain string / sql.NullString, NOT
//     firebird.Win1252 (which would double-decode into mojibake). This mirrors
//     internal/analytics/infra/analyticsfb/queries.go exactly.
//
//nolint:misspell // Spanish domain vocabulary (cohorte, segmento, zona) by project convention.
package reactivacionfb

import "fmt"

// tehuacanCiudadID is the Microsip DIRS_CLIENTES.CIUDAD_ID for Tehuacán, the
// single city the piloto targets (13,635 principal addresses in dev).
const tehuacanCiudadID = 338

// telefonoMinLen is the minimum trimmed length of DIRS_CLIENTES.TELEFONO1 for a
// cliente to be considered reachable by the channel.
const telefonoMinLen = 7

// porLiquidarUmbral is the fraction of the original PRECIO_TOTAL below which a
// still-owing cliente counts as "por_liquidar_hueco" (almost done paying).
const porLiquidarUmbral = "0.20"

// ─── Universo query (Microsip read-model) ──────────────────────────────────────
//
// Segments are classified PER VENTA (per MSP_SALDOS_VENTAS row, CARGO_CANCELADO=
// 'N') and rolled up to the cliente — NOT over a per-cliente SUM. A cliente is:
//   - recien_liquidado-eligible if ANY venta has SALDO = 0.
//   - por_liquidar_hueco-eligible if ANY venta has 0 < SALDO < 20% of PRECIO_TOTAL.
//
// The two sets overlap (a cliente can have a paid-off venta AND a hueco venta);
// the tratable universe is the union, and the assigned segmento is
// por_liquidar_hueco when hueco-eligible, else recien_liquidado. This matches the
// live-verified counts (Tehuacán: 12,467 tratable = 12,304 recien-elig + 512
// hueco-elig − 349 overlap; 6,721 with a ≥7-char phone → 6,270 recien + 451 hueco).
//
// SALDO/POR_LIQUIDAR_PCT reported per cliente are computed over the HUECO ventas
// only (SUM of their saldos and precios): the actionable remaining balance on
// almost-paid ventas. recien_liquidado clientes therefore report SALDO = 0.
//
// agg is the per-cliente rollup; rfm derives FECHA_ULTIMA_COMPRA as
// MAX(DOCTOS_PV.FECHA) over applied V/P sales (matching analyticsfb). The final
// SELECT joins CLIENTES (nombre) and DIRS_CLIENTES (principal Tehuacán address
// with a ≥7-char phone), assigns the segmento, and keeps only tratable clientes.
//
// The phone filter uses `d.TELEFONO1 IS NOT NULL AND CHAR_LENGTH(TRIM(...)) >= 7`
// rather than COALESCE(...,”) because a ” UTF-8 literal against a CHARACTER SET
// NONE column can trigger a "Malformed string" coercion error (see analyticsfb);
// NULL phones are excluded anyway since they can never satisfy the length test.
//
// porLiquidarUmbral is interpolated (not a bound parameter) because it is a
// compile-time constant, not user input — keeping it in the SQL literal avoids a
// driver round-trip type ambiguity on the multiplication.
//
// Column order (must match universoRowRaw.scanFrom exactly):
//
//	1  cliente_id
//	2  nombre               (string; UTF-8 via server-side transliteration of NONE col)
//	3  telefono             (sql.NullString; principal address phone)
//	4  segmento             ('recien_liquidado' | 'por_liquidar_hueco')
//	5  saldo                (NUMERIC(18,2); hueco-venta saldo sum, 0 for recien)
//	6  por_liquidar_pct     (NUMERIC(5,2), 0–100)
//	7  fecha_ultima_compra  (TIMESTAMP, may be NULL)
//
//nolint:gochecknoglobals // query fragment; value is immutable after init.
var selectUniversoTehuacan = fmt.Sprintf(`
WITH agg AS (
  SELECT
    sv.CLIENTE_ID,
    MAX(CASE WHEN sv.SALDO = 0 THEN 1 ELSE 0 END)                                       AS HAS_LIQ,
    MAX(CASE WHEN sv.SALDO > 0 AND sv.SALDO < %[3]s * sv.PRECIO_TOTAL THEN 1 ELSE 0 END) AS HAS_HUECO,
    CAST(SUM(CASE WHEN sv.SALDO > 0 AND sv.SALDO < %[3]s * sv.PRECIO_TOTAL
                  THEN sv.SALDO ELSE 0 END) AS NUMERIC(18,2))                            AS SALDO_HUECO,
    CAST(SUM(CASE WHEN sv.SALDO > 0 AND sv.SALDO < %[3]s * sv.PRECIO_TOTAL
                  THEN sv.PRECIO_TOTAL ELSE 0 END) AS NUMERIC(18,2))                     AS PRECIO_HUECO
  FROM MSP_SALDOS_VENTAS sv
  WHERE sv.CARGO_CANCELADO = 'N'
  GROUP BY sv.CLIENTE_ID
),
rfm AS (
  SELECT
    pv.CLIENTE_ID,
    MAX(pv.FECHA) AS FECHA_ULTIMA_COMPRA
  FROM DOCTOS_PV pv
  WHERE pv.CLIENTE_ID IS NOT NULL
    AND pv.TIPO_DOCTO IN ('V', 'P')
    AND pv.ESTATUS = 'N'
  GROUP BY pv.CLIENTE_ID
)
SELECT
  a.CLIENTE_ID,
  c.NOMBRE                                                              AS NOMBRE,
  d.TELEFONO1                                                          AS TELEFONO,
  CASE WHEN a.HAS_HUECO = 1 THEN 'por_liquidar_hueco'
       ELSE 'recien_liquidado' END                                     AS SEGMENTO,
  CASE WHEN a.HAS_HUECO = 1 THEN a.SALDO_HUECO ELSE 0 END              AS SALDO,
  CASE WHEN a.HAS_HUECO = 1 AND a.PRECIO_HUECO > 0
       THEN CAST(a.SALDO_HUECO / a.PRECIO_HUECO * 100 AS NUMERIC(5,2))
       ELSE 0 END                                                      AS POR_LIQUIDAR_PCT,
  rfm.FECHA_ULTIMA_COMPRA                                              AS FECHA_ULTIMA_COMPRA
FROM agg a
JOIN CLIENTES c        ON c.CLIENTE_ID = a.CLIENTE_ID
JOIN DIRS_CLIENTES d   ON d.CLIENTE_ID = a.CLIENTE_ID
                      AND d.ES_DIR_PPAL = 'S'
                      AND d.CIUDAD_ID = %[1]d
                      AND d.TELEFONO1 IS NOT NULL
                      AND CHAR_LENGTH(TRIM(d.TELEFONO1)) >= %[2]d
LEFT JOIN rfm          ON rfm.CLIENTE_ID = a.CLIENTE_ID
WHERE a.HAS_LIQ = 1 OR a.HAS_HUECO = 1`,
	tehuacanCiudadID, telefonoMinLen, porLiquidarUmbral)

// ─── MSP_RX_COHORTE queries ─────────────────────────────────────────────────────

// cohorteCols is the canonical SELECT column list for MSP_RX_COHORTE.
// The order must match cohorteRowRaw.scanFrom exactly.
const cohorteCols = `
	ID,
	CLIENTE_ID,
	NOMBRE,
	TELEFONO,
	SEGMENTO,
	EN_CONTROL,
	FUE_CONTACTADO,
	COHORTE_FECHA,
	FECHA_ULTIMA_COMPRA_BASE,
	SALDO,
	POR_LIQUIDAR_PCT,
	CREATED_AT,
	UPDATED_AT`

const selectCohorteBase = `SELECT` + cohorteCols + `
FROM MSP_RX_COHORTE`

const selectControlFlags = `SELECT CLIENTE_ID, EN_CONTROL FROM MSP_RX_COHORTE`

const selectContactadoFlags = `SELECT CLIENTE_ID, FUE_CONTACTADO FROM MSP_RX_COHORTE`

// selectClienteFacts reads the copiloto-relevant snapshot fields off
// MSP_RX_COHORTE for outbound.ClienteFactsReader. Column order must match
// Repo.GetFacts' Scan call exactly.
const selectClienteFacts = `SELECT NOMBRE, SEGMENTO, TELEFONO FROM MSP_RX_COHORTE WHERE CLIENTE_ID = ?`

// ─── MSP_RX_CONVERSACION queries ────────────────────────────────────────────────

// conversacionCols is the canonical SELECT column list for MSP_RX_CONVERSACION.
// The order must match conversacionRowRaw.scanFrom exactly.
const conversacionCols = `
	ID,
	CLIENTE_ID,
	ESTADO,
	ASIGNADO_A,
	RESUMEN_MEMORIA,
	CONTEXTO_NOTA,
	BANDERAS,
	NOTA_HASH,
	CREATED_AT,
	UPDATED_AT`

const selectConversacionBase = `SELECT` + conversacionCols + `
FROM MSP_RX_CONVERSACION`

const selectConversacionByCliente = selectConversacionBase + ` WHERE CLIENTE_ID = ?`

// updateConversacion updates every mutable column matched by CLIENTE_ID. Used
// as the first half of ConversacionRepo.Upsert's UPDATE-then-INSERT sequence
// (never MERGE — see mensaje_repo.go's EXECUTE BLOCK comment for the driver's
// -804 param-binding bug inside MERGE's USING clause).
const updateConversacion = `
	UPDATE MSP_RX_CONVERSACION SET
		ESTADO = ?, ASIGNADO_A = ?, RESUMEN_MEMORIA = ?, CONTEXTO_NOTA = ?,
		BANDERAS = ?, NOTA_HASH = ?, UPDATED_AT = ?
	WHERE CLIENTE_ID = ?`

const insertConversacion = `
	INSERT INTO MSP_RX_CONVERSACION
		(ID, CLIENTE_ID, ESTADO, ASIGNADO_A, RESUMEN_MEMORIA, CONTEXTO_NOTA,
		 BANDERAS, NOTA_HASH, CREATED_AT, UPDATED_AT)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// ─── MSP_RX_TURNO queries ────────────────────────────────────────────────────────

// turnoCols is the canonical SELECT column list for MSP_RX_TURNO. The order
// must match turnoRowRaw.scanFrom exactly.
const turnoCols = `
	ID,
	CLIENTE_ID,
	DIRECCION,
	AUTOR,
	CUERPO,
	MENSAJE_REF,
	CREATED_AT`

const selectTurnoBase = `SELECT` + turnoCols + `
FROM MSP_RX_TURNO`

// selectTurnosByCliente orders ascending (chronological) per
// outbound.ConversacionRepo.ListarTurnos' documented contract — the app
// relies on this order to replay the conversation.
const selectTurnosByCliente = selectTurnoBase + ` WHERE CLIENTE_ID = ? ORDER BY CREATED_AT`

const insertTurno = `
	INSERT INTO MSP_RX_TURNO
		(ID, CLIENTE_ID, DIRECCION, AUTOR, CUERPO, MENSAJE_REF, CREATED_AT)
	VALUES (?, ?, ?, ?, ?, ?, ?)`

// ─── MSP_RX_DECISION queries ─────────────────────────────────────────────────────

// decisionCols is the canonical SELECT column list for MSP_RX_DECISION. The
// order must match decisionRowRaw.scanFrom exactly.
const decisionCols = `
	ID,
	CLIENTE_ID,
	TURNO_REF,
	INTENCION,
	CONFIANZA,
	SENALES,
	ACCION_PROPUESTA,
	BORRADOR,
	EVIDENCIA,
	RAZON_ESCALAMIENTO,
	RESULTADO,
	CREATED_AT`

const selectDecisionBase = `SELECT` + decisionCols + `
FROM MSP_RX_DECISION`

// selectDecisionesByCliente orders ascending (chronological) per
// outbound.DecisionRepo.ListarPorCliente's documented contract — the app
// treats the LAST element as the newest decision.
const selectDecisionesByCliente = selectDecisionBase + ` WHERE CLIENTE_ID = ? ORDER BY CREATED_AT`

const insertDecision = `
	INSERT INTO MSP_RX_DECISION
		(ID, CLIENTE_ID, TURNO_REF, INTENCION, CONFIANZA, SENALES,
		 ACCION_PROPUESTA, BORRADOR, EVIDENCIA, RAZON_ESCALAMIENTO, RESULTADO, CREATED_AT)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
