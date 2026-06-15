// Package analyticsfb is the Firebird-backed implementation of the analytics
// module's outbound ports. It satisfies:
//
//   - outbound.WinbackRepo: reads/writes MSP_AN_WINBACK_CANDIDATOS and
//     MSP_AN_REFRESH_STATE (CHARACTER SET UTF8 — no Win1252 decoding).
//   - outbound.MicrosipReader: read-only access to legacy Microsip tables
//     (CLIENTES, DOCTOS_PV, DOCTOS_CC, DIRS_CLIENTES, ARTICULOS) whose text
//     columns are Win1252-encoded and require firebird.Win1252 scan targets.
//
// All DB access goes through firebird.GetQuerier(ctx, r.pool.DB) so the
// ambient transaction injected by fbtestutil.WithTestTransaction (or by the
// app-layer TxManager) is honoured transparently.
//
//nolint:misspell // Spanish domain vocabulary (candidato, cohorte, zona, etc.) by project convention.
package analyticsfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// watermarkOverlap is the lookback added to the incremental watermark to
// avoid missing rows that landed between the previous run's cut-off and the
// current run's start. 24 h is conservative for a nightly refresh; it means
// a row updated within the last day before the watermark is always re-read.
const watermarkOverlap = 24 * time.Hour

// Repo implements both WinbackRepo and MicrosipReader over the shared pool.
// The two logical roles are kept in one struct because they share the same
// *firebird.Pool; splitting them would require passing the pool twice at
// the wiring site with no architectural benefit.
type Repo struct {
	pool *firebird.Pool
}

// NewRepo builds a Repo wired to the given pool.
func NewRepo(pool *firebird.Pool) *Repo {
	return &Repo{pool: pool}
}

// Compile-time checks: Repo must satisfy both outbound interfaces.
var (
	_ outbound.WinbackRepo    = (*Repo)(nil)
	_ outbound.MicrosipReader = (*Repo)(nil)
)

// ─── WinbackRepo — MSP_AN_WINBACK_CANDIDATOS ─────────────────────────────────

// upsertChunkSize is the number of candidatos sent per EXECUTE BLOCK call.
// 20 rows × 16 params = 320 positional params per block. Each row references
// MSP_AN_WINBACK_CANDIDATOS twice (UPDATE + conditional INSERT), so 20 rows =
// 40 Relation contexts — safely below Firebird's 256-context-per-statement limit.
// Empirically the optimal chunk size for this workload against Firebird 5 is
// 10–20: below 10 the round-trip overhead dominates; above 30 Firebird's
// per-statement parse overhead grows faster than the round-trip savings.
// Each chunk is one DB round-trip instead of up to 2 per row.
const upsertChunkSize = 20

// UpsertCandidatos inserts or updates one row per candidato matched by
// CLIENTE_ID. The EXECUTE BLOCK UPDATE branch deliberately omits EN_CONTROL
// and COHORTE_FECHA so an existing A/B flag and cohort date survive across
// refreshes.
//
// Candidatos are processed in chunks of upsertChunkSize via a single
// EXECUTE BLOCK per chunk — one round-trip per 200 rows instead of up to 2
// per row (~95% fewer round-trips for a full 43k-row refresh).
//
// All upserts run through the same querier so they are atomic when the caller
// has opened a transaction (e.g. inside RunInTx or WithTestTransaction).
func (r *Repo) UpsertCandidatos(ctx context.Context, candidatos []*domain.WinbackCandidato) error {
	if len(candidatos) == 0 {
		return nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for i := 0; i < len(candidatos); i += upsertChunkSize {
		end := i + upsertChunkSize
		if end > len(candidatos) {
			end = len(candidatos)
		}
		if err := r.upsertChunk(ctx, q, candidatos[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// upsertChunk sends one EXECUTE BLOCK that performs UPDATE-then-INSERT for
// every candidato in chunk.
//
// Why EXECUTE BLOCK instead of per-row ExecContext:
// The nakagami/firebirdsql driver cannot bind parameters inside MERGE's USING
// SELECT clause (SQL error -804). EXECUTE BLOCK with typed input parameters
// avoids that limitation entirely and lets us batch N rows in a single
// statement, reducing round-trips from 2N to 1 per chunk.
//
// CRITICAL: the UPDATE SET clause omits EN_CONTROL and COHORTE_FECHA so they
// are preserved from the original INSERT across subsequent refreshes.
func (r *Repo) upsertChunk(ctx context.Context, q firebird.Querier, chunk []*domain.WinbackCandidato) error {
	blockSQL, args := buildUpsertBlock(chunk)
	if _, err := q.ExecContext(ctx, blockSQL, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// buildUpsertBlock generates a Firebird EXECUTE BLOCK statement that performs
// UPDATE-then-INSERT for each candidato in chunk.
//
// Each row i uses params named p{i}_id, p{i}_cid, etc. to avoid collisions
// across rows in the same block body. Args are bound in exact param-declaration
// order (16 per row). The UPDATE omits EN_CONTROL and COHORTE_FECHA so those
// columns are only set on first INSERT and survive subsequent refreshes.
// FECHA_ULTIMO_PAGO IS mutable and IS updated on each refresh.
func buildUpsertBlock(chunk []*domain.WinbackCandidato) (string, []any) {
	n := len(chunk)
	args := make([]any, 0, n*16)

	var header strings.Builder
	var body strings.Builder

	_, _ = header.WriteString("EXECUTE BLOCK (\n")
	_, _ = body.WriteString("AS\nBEGIN\n")

	for i, c := range chunk {
		p := fmt.Sprintf("p%d", i)
		if i > 0 {
			_, _ = header.WriteString(",\n")
		}
		appendUpsertParamDecls(&header, p)
		appendUpsertBodyStmt(&body, p)
		args = appendUpsertArgs(args, c)
	}

	_, _ = header.WriteString("\n)")
	_, _ = body.WriteString("END")

	return header.String() + "\n" + body.String(), args
}

// appendUpsertParamDecls writes the 16 typed EXECUTE BLOCK input-parameter
// declarations for a single row prefix p into w.
func appendUpsertParamDecls(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  %s_id  VARCHAR(36)    = ?,\n"+
			"  %s_cid INTEGER        = ?,\n"+
			"  %s_nom VARCHAR(200)   = ?,\n"+
			"  %s_zon VARCHAR(100)   = ?,\n"+
			"  %s_tel VARCHAR(50)    = ?,\n"+
			"  %s_fuc TIMESTAMP      = ?,\n"+
			"  %s_frq INTEGER        = ?,\n"+
			"  %s_mon NUMERIC(18,2)  = ?,\n"+
			"  %s_sal NUMERIC(18,2)  = ?,\n"+
			"  %s_plp NUMERIC(5,2)   = ?,\n"+
			"  %s_nbp VARCHAR(120)   = ?,\n"+
			"  %s_enc SMALLINT       = ?,\n"+
			"  %s_coh TIMESTAMP      = ?,\n"+
			"  %s_cat TIMESTAMP      = ?,\n"+
			"  %s_upd TIMESTAMP      = ?,\n"+
			"  %s_fup TIMESTAMP      = ?",
		p, p, p, p, p,
		p, p, p, p, p,
		p, p, p, p, p,
		p,
	)
}

// appendUpsertBodyStmt writes the UPDATE+INSERT DML for a single row prefix p
// into w. EN_CONTROL and COHORTE_FECHA are excluded from UPDATE so they are
// preserved from the original INSERT across subsequent refreshes.
func appendUpsertBodyStmt(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  UPDATE MSP_AN_WINBACK_CANDIDATOS SET\n"+
			"    NOMBRE=:%s_nom, ZONA=:%s_zon, TELEFONO=:%s_tel,\n"+
			"    FECHA_ULTIMA_COMPRA=:%s_fuc, FRECUENCIA=:%s_frq,\n"+
			"    MONETARY=:%s_mon, SALDO=:%s_sal,\n"+
			"    POR_LIQUIDAR_PCT=:%s_plp, NEXT_BEST_PRODUCT=:%s_nbp,\n"+
			"    FECHA_ULTIMO_PAGO=COALESCE(:%s_fup, FECHA_ULTIMO_PAGO),\n"+
			"    UPDATED_AT=:%s_upd\n"+
			"  WHERE CLIENTE_ID=:%s_cid;\n"+
			"  IF (ROW_COUNT=0) THEN\n"+
			"    INSERT INTO MSP_AN_WINBACK_CANDIDATOS\n"+
			"      (ID,CLIENTE_ID,NOMBRE,ZONA,TELEFONO,FECHA_ULTIMA_COMPRA,\n"+
			"       FRECUENCIA,MONETARY,SALDO,POR_LIQUIDAR_PCT,NEXT_BEST_PRODUCT,\n"+
			"       EN_CONTROL,COHORTE_FECHA,CREATED_AT,UPDATED_AT,FECHA_ULTIMO_PAGO)\n"+
			"    VALUES(:%s_id,:%s_cid,:%s_nom,:%s_zon,:%s_tel,:%s_fuc,\n"+
			"           :%s_frq,:%s_mon,:%s_sal,:%s_plp,:%s_nbp,\n"+
			"           :%s_enc,:%s_coh,:%s_cat,:%s_upd,:%s_fup);\n",
		p, p, p,
		p, p,
		p, p,
		p, p,
		p,
		p,
		p,
		p, p, p, p, p, p,
		p, p, p, p, p,
		p, p, p, p, p,
	)
}

// appendUpsertArgs appends the 16 bound arguments for candidato c (in
// param-declaration order) to args and returns the extended slice.
func appendUpsertArgs(args []any, c *domain.WinbackCandidato) []any {
	enControl := 0
	if c.EnControl() {
		enControl = 1
	}
	return append(
		args,
		c.ID().String(), // _id
		c.ClienteID(),   // _cid
		c.Nombre(),      // _nom
		c.Zona(),        // _zon
		c.Telefono(),    // _tel
		nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompra())), // _fuc
		c.Frecuencia(),                         // _frq
		c.Monetary(),                           // _mon
		c.Saldo(),                              // _sal
		c.PorLiquidarPct(),                     // _plp
		c.NextBestProduct(),                    // _nbp
		enControl,                              // _enc
		firebird.ToWallClock(c.CohorteFecha()), // _coh
		firebird.ToWallClock(c.CreatedAt()),    // _cat
		firebird.ToWallClock(c.UpdatedAt()),    // _upd
		nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimoPago())), // _fup
	)
}

// wallClockPtrFromTime returns nil when t is the zero value (no purchase
// history), otherwise a pointer to t. Used to pass FECHA_ULTIMA_COMPRA as SQL
// NULL when unknown.
func wallClockPtrFromTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// nullableWallClockArg converts a *time.Time to an arg for ExecContext:
// nil → SQL NULL; non-nil → firebird.ToWallClock(*t).
// Kept here alongside wallClockPtrFromTime as both are write-path arg builders
// (not scan/assemble helpers, which live in rowmappers.go).
func nullableWallClockArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return firebird.ToWallClock(*t)
}

// ListCandidatos returns candidatos matching p, ordered by MONETARY DESC.
// Both the COUNT(*) and the SELECT run inside a single RunInSnapshotTx so
// Page.Total and Page.Items observe the same point-in-time snapshot and can
// never be inconsistent due to concurrent writes between the two queries.
// Re-entrant: if ctx already carries an ambient tx (e.g. fbtestutil.WithTestTransaction),
// both inner queries run inside it with no new BEGIN/COMMIT issued.
func (r *Repo) ListCandidatos(ctx context.Context, p outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	where, args := buildCandidatoWhere(p)

	var total int
	var items []*domain.WinbackCandidato

	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		// ── count ──────────────────────────────────────────────────────────
		var cerr error
		total, cerr = r.countCandidatos(ctx, where, args)
		if cerr != nil {
			return cerr
		}
		// ── list ───────────────────────────────────────────────────────────
		var lerr error
		items, lerr = r.listCandidatos(ctx, where, args, p.Limit)
		return lerr
	})
	if err != nil {
		return outbound.Page[*domain.WinbackCandidato]{}, err
	}
	return outbound.Page[*domain.WinbackCandidato]{Items: items, Total: total}, nil
}

// countCandidatos executes COUNT(*) with the same WHERE predicate as the list
// query so Total reflects rows before Limit is applied.
// Must be called from within an outer transaction (e.g. the RunInSnapshotTx in
// ListCandidatos) so it shares the same snapshot as listCandidatos.
func (r *Repo) countCandidatos(ctx context.Context, where string, args []any) (int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	query := countCandidatoBase
	if where != "" {
		query += " WHERE " + where
	}
	var total int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, firebird.MapError(err)
	}
	return total, nil
}

// listCandidatos executes the SELECT with optional WHERE and ROWS limit.
// Must be called from within an outer transaction so it shares the same
// snapshot as countCandidatos.
func (r *Repo) listCandidatos(ctx context.Context, where string, args []any, limit int) ([]*domain.WinbackCandidato, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	query := selectCandidatoBase
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY MONETARY DESC"
	if limit > 0 {
		query += fmt.Sprintf(" ROWS %d", limit)
	}
	rows, qerr := q.QueryContext(ctx, query, args...)
	if qerr != nil {
		return nil, firebird.MapError(qerr)
	}
	defer func() { _ = rows.Close() }()
	return scanCandidatoRows(rows)
}

// buildCandidatoWhere builds the WHERE clause and positional args for
// ListCandidatos / countCandidatos based on ListWinbackParams.
func buildCandidatoWhere(p outbound.ListWinbackParams) (string, []any) {
	var clauses []string
	var args []any
	if p.Zona != "" {
		clauses = append(clauses, "ZONA = ?")
		args = append(args, p.Zona)
	}
	if p.ExcluirControl {
		clauses = append(clauses, "EN_CONTROL = 0")
	}
	return strings.Join(clauses, " AND "), args
}

// GetRefreshState returns the execution state for the named job.
// Returns domain.ErrRefreshStateNotFound when no row exists.
func (r *Repo) GetRefreshState(ctx context.Context, job string) (outbound.RefreshState, error) {
	var st outbound.RefreshState
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		var (
			jobVal        string
			lastWatermark any // TIMESTAMP nullable
			lastRunAtRaw  any // TIMESTAMP NOT NULL
		)
		err := q.QueryRowContext(ctx, selectRefreshState, job).Scan(&jobVal, &lastWatermark, &lastRunAtRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrRefreshStateNotFound
		}
		if err != nil {
			return firebird.MapError(err)
		}
		lastRunAt, err := firebird.ScanUTCTime(lastRunAtRaw)
		if err != nil {
			return err
		}
		wm, err := firebird.ScanNullUTCTime(lastWatermark)
		if err != nil {
			return err
		}
		st = outbound.RefreshState{
			Job:       jobVal,
			LastRunAt: lastRunAt,
		}
		if wm.Valid {
			t := wm.Time
			st.LastWatermark = &t
		}
		return nil
	})
	return st, err
}

// SaveRefreshState upserts the execution state for st.Job.
//
// Uses UPDATE-then-INSERT because the nakagami/firebirdsql driver cannot bind
// parameters inside the USING SELECT clause of a MERGE statement (SQL error -804).
func (r *Repo) SaveRefreshState(ctx context.Context, st outbound.RefreshState) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	// ── Attempt UPDATE ────────────────────────────────────────────────────────
	res, err := q.ExecContext(
		ctx, updateRefreshState,
		nullableWallClockArg(st.LastWatermark),
		firebird.ToWallClock(st.LastRunAt),
		st.Job, // WHERE
	)
	if err != nil {
		return firebird.MapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n > 0 {
		return nil // row existed and was updated
	}

	// ── Row doesn't exist — INSERT ────────────────────────────────────────────
	_, err = q.ExecContext(
		ctx, insertRefreshState,
		st.Job,
		nullableWallClockArg(st.LastWatermark),
		firebird.ToWallClock(st.LastRunAt),
	)
	return firebird.MapError(err)
}

// ExistingControlFlags returns a map[clienteID]bool of EN_CONTROL values for
// every row currently in MSP_AN_WINBACK_CANDIDATOS. The refresh command uses
// this snapshot to carry forward the A/B flag when building the new candidato
// set so a full refresh does not accidentally flip existing assignments.
func (r *Repo) ExistingControlFlags(ctx context.Context) (map[int]bool, error) {
	result := make(map[int]bool)
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectControlFlags)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var clienteID int
			var enControl int16
			if serr := rows.Scan(&clienteID, &enControl); serr != nil {
				return firebird.MapError(serr)
			}
			result[clienteID] = enControl != 0
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ─── MicrosipReader ───────────────────────────────────────────────────────────

// LeerAnclasDesde returns per-cliente RFM + saldo + NBP anchor facts from
// the Microsip tables. It is read-only and MUST NOT write to any table.
//
// since == nil: full read (all clients with at least one DOCTOS_PV row).
// since != nil: restricts to clients whose MAX(DOCTOS_PV.FECHA) >=
//
//	(since - watermarkOverlap), to handle overlap between runs.
//	The FECHA predicate is applied inside BOTH the rfm CTE and the nbp_raw
//	CTE. The saldo_cte has NO date filter — it reflects current-state saldo
//	from MSP_SALDOS_VENTAS regardless of when the last sale occurred.
//
// Column mapping:
//   - RFM anchored on DOCTOS_PV (contado + crédito) — NOT DOCTOS_CC.
//   - Saldo from MSP_SALDOS_VENTAS (trigger-maintained materialized cache,
//     migration 000010). ONE row per cargo; no row explosion.
//   - NBP: most-frequently-purchased ARTICULOS.NOMBRE per cliente, computed
//     in a single pass using ROW_NUMBER() OVER PARTITION BY CLIENTE_ID.
//   - Text columns decoded via firebird.Win1252 (CLIENTES.NOMBRE, ZONAS_CLIENTES.NOMBRE,
//     DIRS_CLIENTES.TELEFONO1, ARTICULOS.NOMBRE are CHARACTER SET NONE / Win1252).
func (r *Repo) LeerAnclasDesde(ctx context.Context, since *time.Time) ([]outbound.AnclaCliente, error) {
	// Build query from CTE parts. When since != nil we inject an extra AND
	// predicate into both the rfm CTE and the nbp_raw CTE before their GROUP BY.
	// The saldo_cte never receives a date filter (current-state read model).
	var query string
	var args []any

	if since == nil {
		// Full-DB case: use the pre-assembled constant (no extra predicates).
		query = leerAnclasBase
	} else {
		// Incremental case: inject FECHA >= ? into rfm and nbp_raw.
		// Apply overlap window: look back an extra 24 h before the watermark.
		boundary := since.Add(-watermarkOverlap)
		// DOCTOS_PV.FECHA is DATE in Microsip; ToWallClock converts the UTC
		// boundary to the local wall-clock value Firebird stores.
		datePredicate := "\n    AND pv.FECHA >= ?"
		query = leerAnclasRFMBase + datePredicate +
			leerAnclasRFMClose +
			leerAnclasNBPBase + datePredicate +
			leerAnclasNBPClose
		// Two CTEs reference the same boundary; bind the parameter twice.
		args = append(args, firebird.ToWallClock(boundary), firebird.ToWallClock(boundary))
	}

	var result []outbound.AnclaCliente
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw anclaRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			ancla, serr := assembleAncla(&raw)
			if serr != nil {
				return serr
			}
			result = append(result, ancla)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
