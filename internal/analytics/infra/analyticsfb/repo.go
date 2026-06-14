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

// UpsertCandidatos inserts or updates one row per candidato matched by
// CLIENTE_ID. The MERGE WHEN MATCHED branch deliberately omits EN_CONTROL and
// COHORTE_FECHA so an existing A/B flag and cohort date survive across
// refreshes.
//
// All upserts run through the same querier so they are atomic when the caller
// has opened a transaction (e.g. inside RunInTx or WithTestTransaction).
func (r *Repo) UpsertCandidatos(ctx context.Context, candidatos []*domain.WinbackCandidato) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, c := range candidatos {
		if err := r.upsertOne(ctx, q, c); err != nil {
			return err
		}
	}
	return nil
}

// upsertOne executes an UPDATE-then-INSERT for a single candidato.
//
// Why UPDATE-then-INSERT instead of Firebird MERGE:
// The nakagami/firebirdsql driver returns SQL error -804 ("Data type unknown")
// when parameters appear inside the USING SELECT clause of MERGE. We fall back
// to the established pattern: attempt UPDATE first; if 0 rows were affected
// (row doesn't exist yet) then INSERT.
//
// CRITICAL: the UPDATE statement omits EN_CONTROL and COHORTE_FECHA so they
// are preserved from the original INSERT across subsequent refreshes.
func (r *Repo) upsertOne(ctx context.Context, q firebird.Querier, c *domain.WinbackCandidato) error {
	// ── Attempt UPDATE (sets mutable fields, preserves EN_CONTROL/COHORTE_FECHA) ──
	updateArgs := []any{
		c.Nombre(),
		c.Zona(),
		c.Telefono(),
		nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompra())),
		c.Frecuencia(),
		c.Monetary(),
		c.Saldo(),
		c.PorLiquidarPct(),
		c.NextBestProduct(),
		firebird.ToWallClock(c.UpdatedAt()),
		// WHERE
		c.ClienteID(),
	}
	res, err := q.ExecContext(ctx, updateCandidato, updateArgs...)
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

	// ── Row doesn't exist — INSERT with all columns ───────────────────────────
	enControl := 0
	if c.EnControl() {
		enControl = 1
	}
	insertArgs := []any{
		c.ID().String(),
		c.ClienteID(),
		c.Nombre(),
		c.Zona(),
		c.Telefono(),
		nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompra())),
		c.Frecuencia(),
		c.Monetary(),
		c.Saldo(),
		c.PorLiquidarPct(),
		c.NextBestProduct(),
		enControl,
		firebird.ToWallClock(c.CohorteFecha()),
		firebird.ToWallClock(c.CreatedAt()),
		firebird.ToWallClock(c.UpdatedAt()),
	}
	_, err = q.ExecContext(ctx, insertCandidato, insertArgs...)
	return firebird.MapError(err)
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
	res, err := q.ExecContext(ctx, updateRefreshState,
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
	_, err = q.ExecContext(ctx, insertRefreshState,
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
