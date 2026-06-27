//nolint:misspell // Spanish domain vocabulary (cartera, zona, cobrador, saldo, etc.) by project convention.
package analyticsfb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── SQL — aging aggregations ─────────────────────────────────────────────────
//
// Both aging queries use a derived-table (subquery) pattern to avoid repeating
// the CASE WHEN expression in GROUP BY, which is not allowed in Firebird and
// would require parameter binding twice. The inner SELECT assigns BUCKET per
// row; the outer SELECT aggregates over it.
//
// Bucket boundaries match domain.BucketForDays exactly:
//
//	DATEDIFF(DAY, FECHA_ULT_PAGO, today) ≤ 30  → "0-30"
//	                                    ≤ 60  → "31-60"
//	                                    ≤ 90  → "61-90"
//	                                    > 90  → "90+"
//	FECHA_ULT_PAGO IS NULL              (never paid) → "90+"
//
// The moroso threshold (>30 days) is consistent with scoring.go.
// "today" is passed as a TIMESTAMP via firebird.ToWallClock; DATEDIFF
// between Firebird DATE and TIMESTAMP returns full calendar days.
//
// CAST(SUM(sv.SALDO) AS NUMERIC(18,2)) is required to work around the
// nakagami/firebirdsql driver v0.9.19 bug that returns aggregate NUMERIC
// values unscaled (project memory: reference_firebirdsql_sum_scale).
//
// Only active rows are included: CARGO_CANCELADO='N' AND SALDO>0.
// NULL ZONA_CLIENTE_ID rows are excluded (unlinked legacy cargos).

const agingInnerSelect = `
  SELECT
    sv.ZONA_CLIENTE_ID,
    sv.SALDO,
    CASE
      WHEN sv.FECHA_ULT_PAGO IS NULL
           OR DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 90 THEN '90+'
      WHEN DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 60 THEN '61-90'
      WHEN DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 30 THEN '31-60'
      ELSE '0-30'
    END AS BUCKET
  FROM MSP_SALDOS_VENTAS sv
  WHERE sv.CARGO_CANCELADO = 'N'
    AND sv.SALDO > 0
    AND sv.ZONA_CLIENTE_ID IS NOT NULL`

const agingSaldosByZonaQuery = `
SELECT d.ZONA_CLIENTE_ID, d.BUCKET,
  CAST(SUM(d.SALDO) AS NUMERIC(18,2)) AS SALDO,
  COUNT(*) AS CONTEO
FROM (` + agingInnerSelect + `
) d
GROUP BY d.ZONA_CLIENTE_ID, d.BUCKET`

const agingInnerSelectCobrador = `
  SELECT
    sv.ZONA_CLIENTE_ID,
    c.COBRADOR_ID,
    sv.SALDO,
    CASE
      WHEN sv.FECHA_ULT_PAGO IS NULL
           OR DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 90 THEN '90+'
      WHEN DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 60 THEN '61-90'
      WHEN DATEDIFF(DAY, sv.FECHA_ULT_PAGO, CAST(? AS TIMESTAMP)) > 30 THEN '31-60'
      ELSE '0-30'
    END AS BUCKET
  FROM MSP_SALDOS_VENTAS sv
  LEFT JOIN CLIENTES c ON c.CLIENTE_ID = sv.CLIENTE_ID
  WHERE sv.CARGO_CANCELADO = 'N'
    AND sv.SALDO > 0
    AND sv.ZONA_CLIENTE_ID IS NOT NULL`

const agingSaldosByCobradorQuery = `
SELECT d.ZONA_CLIENTE_ID, d.COBRADOR_ID, d.BUCKET,
  CAST(SUM(d.SALDO) AS NUMERIC(18,2)) AS SALDO,
  COUNT(*) AS CONTEO
FROM (` + agingInnerSelectCobrador + `
) d
GROUP BY d.ZONA_CLIENTE_ID, d.COBRADOR_ID, d.BUCKET`

// ─── SQL — vintage cohort ─────────────────────────────────────────────────────
//
// Cohort month = year×12 + month ordinal, matching domain.VintageCohort.
// Uses derived table to avoid repeating the expression in GROUP BY.

const vintageSaldosQuery = `
SELECT d.ZONA_CLIENTE_ID, d.COHORT_MONTH,
  CAST(SUM(d.SALDO) AS NUMERIC(18,2)) AS SALDO,
  COUNT(*) AS CONTEO
FROM (
  SELECT
    sv.ZONA_CLIENTE_ID,
    sv.SALDO,
    CAST((EXTRACT(YEAR FROM sv.FECHA_CARGO) * 12 + EXTRACT(MONTH FROM sv.FECHA_CARGO)) AS INTEGER) AS COHORT_MONTH
  FROM MSP_SALDOS_VENTAS sv
  WHERE sv.CARGO_CANCELADO = 'N'
    AND sv.SALDO > 0
    AND sv.ZONA_CLIENTE_ID IS NOT NULL
) d
GROUP BY d.ZONA_CLIENTE_ID, d.COHORT_MONTH`

// ─── SQL — collection effectiveness (CEI) ────────────────────────────────────
//
// Restricted to abono concepts 87327/155/11 (see queries.go for rationale).
// LEFT JOIN CLIENTES so clients with no CLIENTES match return COBRADOR_ID=NULL.
// Period is half-open [desde, hasta).

//nolint:gochecknoglobals // query fragment; value is immutable after init.
var coleccionCEIQuery = fmt.Sprintf(`
SELECT
  pv.ZONA_CLIENTE_ID,
  c.COBRADOR_ID,
  CAST(SUM(pv.IMPORTE) AS NUMERIC(18,2)) AS IMPORTE,
  COUNT(DISTINCT pv.CLIENTE_ID) AS CONTEO
FROM MSP_PAGOS_VENTAS pv
LEFT JOIN CLIENTES c ON c.CLIENTE_ID = pv.CLIENTE_ID
WHERE pv.CANCELADO = 'N'
  AND pv.APLICADO = 'S'
  AND pv.CONCEPTO_CC_ID IN (%d, %d, %d)
  AND pv.FECHA >= CAST(? AS TIMESTAMP)
  AND pv.FECHA < CAST(? AS TIMESTAMP)
  AND pv.ZONA_CLIENTE_ID IS NOT NULL
GROUP BY pv.ZONA_CLIENTE_ID, c.COBRADOR_ID`,
	conceptoCobranzaRuta, conceptoCobro155, conceptoCobroGenerico)

// ─── SQL — MSP_AN_CARTERA_SNAPSHOT ───────────────────────────────────────────

const listSnapshotBaseCols = `
  ID, FECHA_CORTE, ZONA_CLIENTE_ID, COBRADOR_ID, BUCKET,
  SALDO, CONTEO, CREATED_AT, UPDATED_AT`

const listSnapshotOrderBy = `
ORDER BY FECHA_CORTE DESC, ZONA_CLIENTE_ID ASC, BUCKET ASC`

// ─── CarteraRepo: aging aggregations ─────────────────────────────────────────

// AgingSaldosByZona returns per-zone aging distribution from MSP_SALDOS_VENTAS.
// Bucket boundaries are identical to domain.BucketForDays.
func (r *Repo) AgingSaldosByZona(ctx context.Context, today time.Time) ([]outbound.AgingRow, error) {
	wall := firebird.ToWallClock(today)
	args := []any{wall, wall, wall}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, agingSaldosByZonaQuery, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanAgingRows(rows, false)
}

// AgingSaldosByCobrador returns per-cobrador aging distribution, adding
// COBRADOR_ID from CLIENTES. Clients with no cobrador return CobradorID==nil.
func (r *Repo) AgingSaldosByCobrador(ctx context.Context, today time.Time) ([]outbound.AgingRow, error) {
	wall := firebird.ToWallClock(today)
	args := []any{wall, wall, wall}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, agingSaldosByCobradorQuery, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanAgingRows(rows, true)
}

// scanAgingRows scans result rows from either aging query.
// withCobrador toggles whether to scan a COBRADOR_ID column between ZONA and BUCKET.
func scanAgingRows(rows *sql.Rows, withCobrador bool) ([]outbound.AgingRow, error) {
	var result []outbound.AgingRow
	for rows.Next() {
		row, err := scanOneAgingRow(rows, withCobrador)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		result = append(result, row)
	}
	return result, firebird.MapError(rows.Err())
}

func scanOneAgingRow(s rowScanner, withCobrador bool) (outbound.AgingRow, error) {
	var (
		zonaID   int
		cobrador sql.NullInt64
		bucket   string
		saldoRaw any
		conteo   int
	)
	var err error
	if withCobrador {
		err = s.Scan(&zonaID, &cobrador, &bucket, &saldoRaw, &conteo)
	} else {
		err = s.Scan(&zonaID, &bucket, &saldoRaw, &conteo)
	}
	if err != nil {
		return outbound.AgingRow{}, err
	}
	saldo, err := firebird.ScanDecimal(saldoRaw, 2)
	if err != nil {
		return outbound.AgingRow{}, err
	}
	row := outbound.AgingRow{
		ZonaClienteID: zonaID,
		Bucket:        bucket,
		Saldo:         saldo,
		Conteo:        conteo,
	}
	if withCobrador && cobrador.Valid {
		id := int(cobrador.Int64)
		row.CobradorID = &id
	}
	return row, nil
}

// ─── CarteraRepo: vintage ─────────────────────────────────────────────────────

// VintageSaldos returns saldo by vintage cohort (year×12+month) and zone.
func (r *Repo) VintageSaldos(ctx context.Context) ([]outbound.VintageRow, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, vintageSaldosQuery)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanVintageRows(rows)
}

func scanVintageRows(rows *sql.Rows) ([]outbound.VintageRow, error) {
	var result []outbound.VintageRow
	for rows.Next() {
		var (
			zonaID    int
			cohortRaw any // NUMERIC from arithmetic expression
			saldoRaw  any
			conteo    int
		)
		if err := rows.Scan(&zonaID, &cohortRaw, &saldoRaw, &conteo); err != nil {
			return nil, firebird.MapError(err)
		}
		cohort, err := scanNullableIntDecimal(cohortRaw)
		if err != nil {
			return nil, err
		}
		saldo, err := firebird.ScanDecimal(saldoRaw, 2)
		if err != nil {
			return nil, err
		}
		result = append(result, outbound.VintageRow{
			ZonaClienteID: zonaID,
			CohortMonth:   cohort,
			Saldo:         saldo,
			Conteo:        conteo,
		})
	}
	return result, firebird.MapError(rows.Err())
}

// ─── CarteraRepo: CEI ─────────────────────────────────────────────────────────

// ColeccionCEI returns collection effectiveness by zone and cobrador for the
// half-open period [desde, hasta).
func (r *Repo) ColeccionCEI(ctx context.Context, desde, hasta time.Time) ([]outbound.CEIRow, error) {
	args := []any{firebird.ToWallClock(desde), firebird.ToWallClock(hasta)}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, coleccionCEIQuery, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanCEIRows(rows)
}

func scanCEIRows(rows *sql.Rows) ([]outbound.CEIRow, error) {
	var result []outbound.CEIRow
	for rows.Next() {
		var (
			zonaID     int
			cobrador   sql.NullInt64
			importeRaw any
			conteo     int
		)
		if err := rows.Scan(&zonaID, &cobrador, &importeRaw, &conteo); err != nil {
			return nil, firebird.MapError(err)
		}
		importe, err := firebird.ScanDecimal(importeRaw, 2)
		if err != nil {
			return nil, err
		}
		cei := outbound.CEIRow{
			ZonaClienteID: zonaID,
			Importe:       importe,
			Conteo:        conteo,
		}
		if cobrador.Valid {
			id := int(cobrador.Int64)
			cei.CobradorID = &id
		}
		result = append(result, cei)
	}
	return result, firebird.MapError(rows.Err())
}

// ─── CarteraRepo: snapshot upsert ────────────────────────────────────────────

// snapshotChunkSize is the number of snapshot rows per EXECUTE BLOCK call.
// 9 params × 20 rows = 180 params per block, well within driver limits.
const snapshotChunkSize = 20

// SaveCarteraSnapshot upserts a batch of CarteraSnapshot rows via EXECUTE BLOCK.
// Zone-level rows (CobradorID==nil) are matched with COBRADOR_ID IS NULL so
// the Firebird NULL≠NULL semantics in the UNIQUE index don't cause duplicates.
func (r *Repo) SaveCarteraSnapshot(ctx context.Context, rows []domain.CarteraSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for i := 0; i < len(rows); i += snapshotChunkSize {
		end := i + snapshotChunkSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := r.saveSnapshotChunk(ctx, q, rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) saveSnapshotChunk(ctx context.Context, q firebird.Querier, chunk []domain.CarteraSnapshot) error {
	blockSQL, args := buildSnapshotBlock(chunk)
	if _, err := q.ExecContext(ctx, blockSQL, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// buildSnapshotBlock generates a Firebird EXECUTE BLOCK that performs
// UPDATE-then-INSERT for each snapshot row in chunk.
//
// The UPDATE WHERE clause uses
//
//	(COBRADOR_ID=:pi_cob OR (:pi_cob IS NULL AND COBRADOR_ID IS NULL))
//
// so that zone-level rows (COBRADOR_ID IS NULL) are matched correctly —
// the UNIQUE constraint cannot deduplicate NULL keys (NULL≠NULL per SQL standard).
func buildSnapshotBlock(chunk []domain.CarteraSnapshot) (string, []any) {
	n := len(chunk)
	args := make([]any, 0, n*9)

	var header strings.Builder
	var body strings.Builder

	_, _ = header.WriteString("EXECUTE BLOCK (\n")
	_, _ = body.WriteString("AS\nBEGIN\n")

	for i := range chunk {
		p := fmt.Sprintf("p%d", i)
		if i > 0 {
			_, _ = header.WriteString(",\n")
		}
		appendSnapshotParamDecls(&header, p)
		appendSnapshotBodyStmt(&body, p)
		args = appendSnapshotArgs(args, &chunk[i])
	}

	_, _ = header.WriteString("\n)")
	_, _ = body.WriteString("END")

	return header.String() + "\n" + body.String(), args
}

// appendSnapshotParamDecls writes the 9 typed EXECUTE BLOCK input-parameter
// declarations for a single snapshot row with prefix p into w.
func appendSnapshotParamDecls(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  %s_id  VARCHAR(36)   = ?,\n"+
			"  %s_fc  TIMESTAMP     = ?,\n"+
			"  %s_zon INTEGER       = ?,\n"+
			"  %s_cob INTEGER       = ?,\n"+
			"  %s_bkt VARCHAR(16)   = ?,\n"+
			"  %s_sal NUMERIC(18,2) = ?,\n"+
			"  %s_cnt INTEGER       = ?,\n"+
			"  %s_cat TIMESTAMP     = ?,\n"+
			"  %s_upd TIMESTAMP     = ?",
		p, p, p, p, p, p, p, p, p,
	)
}

// appendSnapshotBodyStmt writes the UPDATE+INSERT DML for a single snapshot
// row prefix p into w. The UPDATE WHERE uses an explicit IS NULL check for
// COBRADOR_ID so zone-level rows (COBRADOR_ID NULL) are correctly identified.
func appendSnapshotBodyStmt(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  UPDATE MSP_AN_CARTERA_SNAPSHOT SET\n"+
			"    SALDO=:%s_sal, CONTEO=:%s_cnt, UPDATED_AT=:%s_upd\n"+
			"  WHERE FECHA_CORTE=:%s_fc\n"+
			"    AND ZONA_CLIENTE_ID=:%s_zon\n"+
			"    AND (COBRADOR_ID=:%s_cob OR (:%s_cob IS NULL AND COBRADOR_ID IS NULL))\n"+
			"    AND BUCKET=:%s_bkt;\n"+
			"  IF (ROW_COUNT=0) THEN\n"+
			"    INSERT INTO MSP_AN_CARTERA_SNAPSHOT\n"+
			"      (ID, FECHA_CORTE, ZONA_CLIENTE_ID, COBRADOR_ID, BUCKET,\n"+
			"       SALDO, CONTEO, CREATED_AT, UPDATED_AT)\n"+
			"    VALUES (:%s_id, :%s_fc, :%s_zon, :%s_cob, :%s_bkt,\n"+
			"            :%s_sal, :%s_cnt, :%s_cat, :%s_upd);\n",
		p, p, p,
		p, p,
		p, p,
		p,
		p, p, p, p, p,
		p, p, p, p,
	)
}

// appendSnapshotArgs appends the 9 bound arguments for snapshot s (in
// param-declaration order) to args and returns the extended slice.
func appendSnapshotArgs(args []any, s *domain.CarteraSnapshot) []any {
	return append(
		args,
		s.ID().String(),                      // _id
		firebird.ToWallClock(s.FechaCorte()), // _fc
		s.ZonaClienteID(),                    // _zon
		cobradorIDArg(s.CobradorID()),        // _cob (nil → SQL NULL)
		s.Bucket(),                           // _bkt
		s.Saldo(),                            // _sal
		s.Conteo(),                           // _cnt
		firebird.ToWallClock(s.CreatedAt()),  // _cat
		firebird.ToWallClock(s.UpdatedAt()),  // _upd
	)
}

// cobradorIDArg converts a *int CobradorID to an ExecContext arg:
// nil → SQL NULL; non-nil → the integer value.
func cobradorIDArg(id *int) any {
	if id == nil {
		return nil
	}
	return *id
}

// ─── CarteraRepo: snapshot read ──────────────────────────────────────────────

// ListRecentSnapshots returns snapshot rows ordered FECHA_CORTE DESC.
// limit<=0 returns all rows.
func (r *Repo) ListRecentSnapshots(ctx context.Context, limit int) ([]domain.CarteraSnapshot, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	query := "SELECT" + listSnapshotBaseCols + "\nFROM MSP_AN_CARTERA_SNAPSHOT\n" + listSnapshotOrderBy
	if limit > 0 {
		query += fmt.Sprintf(" ROWS %d", limit)
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanSnapshotRows(rows)
}

// snapshotRowRaw is the intermediate scan target for one MSP_AN_CARTERA_SNAPSHOT row.
type snapshotRowRaw struct {
	idRaw         string
	fechaCorteRaw any
	zonaClienteID int
	cobradorID    sql.NullInt64
	bucket        string
	saldoRaw      any
	conteo        int
	createdAtRaw  any
	updatedAtRaw  any
}

func (r *snapshotRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.idRaw,
		&r.fechaCorteRaw,
		&r.zonaClienteID,
		&r.cobradorID,
		&r.bucket,
		&r.saldoRaw,
		&r.conteo,
		&r.createdAtRaw,
		&r.updatedAtRaw,
	)
}

func scanSnapshotRows(rows *sql.Rows) ([]domain.CarteraSnapshot, error) {
	var result []domain.CarteraSnapshot
	for rows.Next() {
		var raw snapshotRowRaw
		if err := raw.scanFrom(rows); err != nil {
			return nil, firebird.MapError(err)
		}
		s, err := assembleSnapshot(&raw)
		if err != nil {
			return nil, err
		}
		result = append(result, *s)
	}
	return result, firebird.MapError(rows.Err())
}

func assembleSnapshot(r *snapshotRowRaw) (*domain.CarteraSnapshot, error) {
	id, err := parseUUIDColumn("ID", r.idRaw)
	if err != nil {
		return nil, err
	}
	fechaCorte, err := firebird.ScanUTCTime(r.fechaCorteRaw)
	if err != nil {
		return nil, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(r.createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(r.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	var cobradorID *int
	if r.cobradorID.Valid {
		id64 := int(r.cobradorID.Int64)
		cobradorID = &id64
	}
	return domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
		ID:            id,
		FechaCorte:    fechaCorte,
		ZonaClienteID: r.zonaClienteID,
		CobradorID:    cobradorID,
		Bucket:        r.bucket,
		Saldo:         saldo,
		Conteo:        r.conteo,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}), nil
}
