//nolint:misspell // Spanish domain vocabulary (cohorte, segmento) by project convention.
package reactivacionfb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Repo implements both outbound.CohorteRepo (reads/writes MSP_RX_COHORTE) and
// outbound.UniversoReader (read-only over the Microsip read-model) over a shared
// pool. The two roles live in one struct because they share the same
// *firebird.Pool; splitting them would require passing the pool twice at the
// wiring site with no architectural benefit (mirrors analyticsfb.Repo).
type Repo struct {
	pool *firebird.Pool
}

// NewRepo builds a Repo wired to the given pool.
func NewRepo(pool *firebird.Pool) *Repo {
	return &Repo{pool: pool}
}

// Compile-time checks: Repo must satisfy both outbound interfaces.
var (
	_ outbound.CohorteRepo    = (*Repo)(nil)
	_ outbound.UniversoReader = (*Repo)(nil)
)

// ─── UniversoReader ─────────────────────────────────────────────────────────────

// LeerUniversoTehuacan returns the tratable universe of the piloto. Read-only.
func (r *Repo) LeerUniversoTehuacan(ctx context.Context) ([]outbound.ClienteUniverso, error) {
	var result []outbound.ClienteUniverso
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectUniversoTehuacan)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw universoRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			cliente, serr := assembleUniverso(&raw)
			if serr != nil {
				return serr
			}
			result = append(result, cliente)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ─── CohorteRepo — writes ────────────────────────────────────────────────────────

// upsertChunkSize is the number of cohorte rows sent per EXECUTE BLOCK call.
// 30 rows × 13 params = 390 positional params per block. Each row references
// MSP_RX_COHORTE twice (UPDATE + conditional INSERT), so 30 rows = 60 Relation
// contexts — safely below Firebird's 256-context-per-statement limit.
const upsertChunkSize = 30

// upsertParamsPerRow is the count of EXECUTE BLOCK input parameters per cohorte row.
const upsertParamsPerRow = 13

// UpsertCohorte inserts or updates one row per cohorte cliente matched by
// CLIENTE_ID. The EXECUTE BLOCK UPDATE branch deliberately omits EN_CONTROL,
// FUE_CONTACTADO and COHORTE_FECHA so those columns survive from the original
// INSERT across rebuilds (the A/B flag, the channel's contact flag and the
// cohort date must never be rewritten).
//
// All upserts run through the same querier so they are atomic when the caller has
// opened a transaction (e.g. inside RunInTx or WithTestTransaction).
func (r *Repo) UpsertCohorte(ctx context.Context, cohorte []*domain.CohorteCliente) error {
	if len(cohorte) == 0 {
		return nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for i := 0; i < len(cohorte); i += upsertChunkSize {
		end := i + upsertChunkSize
		if end > len(cohorte) {
			end = len(cohorte)
		}
		if err := r.upsertChunk(ctx, q, cohorte[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) upsertChunk(ctx context.Context, q firebird.Querier, chunk []*domain.CohorteCliente) error {
	blockSQL, args := buildUpsertBlock(chunk)
	if _, err := q.ExecContext(ctx, blockSQL, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// buildUpsertBlock generates a Firebird EXECUTE BLOCK that performs
// UPDATE-then-INSERT for each cohorte cliente in chunk. EXECUTE BLOCK is used
// instead of MERGE because the nakagami/firebirdsql driver cannot bind
// parameters inside MERGE's USING SELECT clause (SQL error -804).
func buildUpsertBlock(chunk []*domain.CohorteCliente) (string, []any) {
	args := make([]any, 0, len(chunk)*upsertParamsPerRow)

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

// appendUpsertParamDecls writes the 13 typed EXECUTE BLOCK input-parameter
// declarations for a single row prefix p into w.
func appendUpsertParamDecls(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  %s_id  VARCHAR(36)   = ?,\n"+
			"  %s_cid INTEGER       = ?,\n"+
			"  %s_nom VARCHAR(200)  = ?,\n"+
			"  %s_tel VARCHAR(40)   = ?,\n"+
			"  %s_seg VARCHAR(24)   = ?,\n"+
			"  %s_enc SMALLINT      = ?,\n"+
			"  %s_con SMALLINT      = ?,\n"+
			"  %s_coh TIMESTAMP     = ?,\n"+
			"  %s_fuc TIMESTAMP     = ?,\n"+
			"  %s_sal NUMERIC(18,2) = ?,\n"+
			"  %s_plp NUMERIC(5,2)  = ?,\n"+
			"  %s_cat TIMESTAMP     = ?,\n"+
			"  %s_upd TIMESTAMP     = ?",
		p, p, p, p, p,
		p, p, p, p, p,
		p, p, p,
	)
}

// appendUpsertBodyStmt writes the UPDATE+INSERT DML for a single row prefix p
// into w. EN_CONTROL, FUE_CONTACTADO and COHORTE_FECHA are excluded from UPDATE
// so they are preserved from the original INSERT across subsequent rebuilds.
func appendUpsertBodyStmt(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  UPDATE MSP_RX_COHORTE SET\n"+
			"    NOMBRE=:%s_nom, TELEFONO=:%s_tel, SEGMENTO=:%s_seg,\n"+
			"    FECHA_ULTIMA_COMPRA_BASE=:%s_fuc, SALDO=:%s_sal,\n"+
			"    POR_LIQUIDAR_PCT=:%s_plp, UPDATED_AT=:%s_upd\n"+
			"  WHERE CLIENTE_ID=:%s_cid;\n"+
			"  IF (ROW_COUNT=0) THEN\n"+
			"    INSERT INTO MSP_RX_COHORTE\n"+
			"      (ID,CLIENTE_ID,NOMBRE,TELEFONO,SEGMENTO,EN_CONTROL,FUE_CONTACTADO,\n"+
			"       COHORTE_FECHA,FECHA_ULTIMA_COMPRA_BASE,SALDO,POR_LIQUIDAR_PCT,CREATED_AT,UPDATED_AT)\n"+
			"    VALUES(:%s_id,:%s_cid,:%s_nom,:%s_tel,:%s_seg,:%s_enc,:%s_con,\n"+
			"           :%s_coh,:%s_fuc,:%s_sal,:%s_plp,:%s_cat,:%s_upd);\n",
		p, p, p,
		p, p,
		p, p,
		p,
		p, p, p, p, p, p, p,
		p, p, p, p, p, p,
	)
}

// appendUpsertArgs appends the 13 bound arguments for cohorte cliente c (in
// param-declaration order) to args and returns the extended slice.
func appendUpsertArgs(args []any, c *domain.CohorteCliente) []any {
	enControl := 0
	if c.EnControl() {
		enControl = 1
	}
	fueContactado := 0
	if c.FueContactado() {
		fueContactado = 1
	}
	return append(
		args,
		c.ID().String(),                        // _id
		c.ClienteID(),                          // _cid
		c.Nombre(),                             // _nom
		c.Telefono(),                           // _tel
		c.Segmento().String(),                  // _seg
		enControl,                              // _enc
		fueContactado,                          // _con
		firebird.ToWallClock(c.CohorteFecha()), // _coh
		nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompraBase())), // _fuc
		c.Saldo(),                           // _sal
		c.PorLiquidarPct(),                  // _plp
		firebird.ToWallClock(c.CreatedAt()), // _cat
		firebird.ToWallClock(c.UpdatedAt()), // _upd
	)
}

// wallClockPtrFromTime returns nil when t is the zero value, otherwise a pointer
// to t. Used to pass FECHA_ULTIMA_COMPRA_BASE as SQL NULL when unknown.
func wallClockPtrFromTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// nullableWallClockArg converts a *time.Time to an ExecContext arg: nil → SQL
// NULL; non-nil → firebird.ToWallClock(*t).
func nullableWallClockArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return firebird.ToWallClock(*t)
}

// ─── CohorteRepo — reads ─────────────────────────────────────────────────────────

// ListarCohorte returns cohorte rows matching p, ordered by CLIENTE_ID ASC.
func (r *Repo) ListarCohorte(ctx context.Context, p outbound.ListarCohorteParams) ([]*domain.CohorteCliente, error) {
	where, args := buildCohorteWhere(p)
	var result []*domain.CohorteCliente
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		query := selectCohorteBase
		if where != "" {
			query += " WHERE " + where
		}
		query += " ORDER BY CLIENTE_ID"
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw cohorteRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			c, serr := assembleCohorte(&raw)
			if serr != nil {
				return serr
			}
			result = append(result, c)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// buildCohorteWhere builds the WHERE clause and positional args for ListarCohorte.
func buildCohorteWhere(p outbound.ListarCohorteParams) (string, []any) {
	var clauses []string
	var args []any
	if p.Segmento != "" {
		clauses = append(clauses, "SEGMENTO = ?")
		args = append(args, p.Segmento.String())
	}
	if p.SoloTratamiento {
		clauses = append(clauses, "EN_CONTROL = 0")
	}
	return strings.Join(clauses, " AND "), args
}

// ExistingControlFlags returns a map[clienteID]EN_CONTROL for every MSP_RX_COHORTE
// row, so a rebuild carries forward the A/B assignment.
func (r *Repo) ExistingControlFlags(ctx context.Context) (map[int]bool, error) {
	return r.readFlags(ctx, selectControlFlags)
}

// ExistingContactadoFlags returns a map[clienteID]FUE_CONTACTADO for every
// MSP_RX_COHORTE row, so a rebuild carries forward the channel's contact flag.
func (r *Repo) ExistingContactadoFlags(ctx context.Context) (map[int]bool, error) {
	return r.readFlags(ctx, selectContactadoFlags)
}

// readFlags runs a two-column (CLIENTE_ID, SMALLINT flag) query and collects the
// result into a map. Shared by ExistingControlFlags and ExistingContactadoFlags.
func (r *Repo) readFlags(ctx context.Context, query string) (map[int]bool, error) {
	result := make(map[int]bool)
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var clienteID int
			var flag int16
			if serr := rows.Scan(&clienteID, &flag); serr != nil {
				return firebird.MapError(serr)
			}
			result[clienteID] = flag != 0
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
