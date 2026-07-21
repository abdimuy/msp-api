//nolint:misspell // Spanish domain vocabulary (mensaje, segmento) by project convention.
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

// Compile-time check: Repo must satisfy outbound.MensajeRepo. This file
// implements that interface (MSP_RX_MENSAJES) on top of the shared *Repo, and
// also adds CohorteRepo.MarcarContactado (which writes to MSP_RX_COHORTE, not
// MSP_RX_MENSAJES — it lives here because it is the channel's write, not the
// cohort builder's).
var _ outbound.MensajeRepo = (*Repo)(nil)

// insertChunkSize is the number of mensaje rows sent per EXECUTE BLOCK call.
// Each row is a plain INSERT (one relation context), so 40 rows × 12 params =
// 480 positional params per block, well below the driver's practical limits
// and Firebird's 256-context-per-statement ceiling.
const insertChunkSize = 40

// insertParamsPerRow is the count of EXECUTE BLOCK input parameters per row.
const insertParamsPerRow = 12

// Insertar bulk-inserts newly queued mensajes in chunks of insertChunkSize via
// EXECUTE BLOCK — the encolado batch can reach the low thousands (the whole
// Tehuacán treatment group), so per-row round trips would be needlessly slow.
// SENDER_KIND, ENVIADO_EN and ERROR are always NULL at insert time (a fresh
// mensaje is always EstadoEncolado).
func (r *Repo) Insertar(ctx context.Context, mensajes []*domain.Mensaje) error {
	if len(mensajes) == 0 {
		return nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for i := 0; i < len(mensajes); i += insertChunkSize {
		end := i + insertChunkSize
		if end > len(mensajes) {
			end = len(mensajes)
		}
		if err := r.insertChunk(ctx, q, mensajes[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) insertChunk(ctx context.Context, q firebird.Querier, chunk []*domain.Mensaje) error {
	blockSQL, args := buildInsertBlock(chunk)
	if _, err := q.ExecContext(ctx, blockSQL, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// buildInsertBlock generates a Firebird EXECUTE BLOCK that INSERTs every
// mensaje in chunk. EXECUTE BLOCK batches the round trip; MSP_RX_MENSAJES has
// no UNIQUE constraint to violate (every row is a fresh UUID PK), so this is
// a plain INSERT list — no UPDATE-then-INSERT dance like the cohorte upsert.
func buildInsertBlock(chunk []*domain.Mensaje) (string, []any) {
	args := make([]any, 0, len(chunk)*insertParamsPerRow)

	var header strings.Builder
	var body strings.Builder
	_, _ = header.WriteString("EXECUTE BLOCK (\n")
	_, _ = body.WriteString("AS\nBEGIN\n")

	for i, m := range chunk {
		p := fmt.Sprintf("p%d", i)
		if i > 0 {
			_, _ = header.WriteString(",\n")
		}
		appendInsertParamDecls(&header, p)
		appendInsertBodyStmt(&body, p)
		args = appendInsertArgs(args, m)
	}

	_, _ = header.WriteString("\n)")
	_, _ = body.WriteString("END")

	return header.String() + "\n" + body.String(), args
}

// appendInsertParamDecls writes the 12 typed EXECUTE BLOCK input-parameter
// declarations for a single row prefix p into w.
func appendInsertParamDecls(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  %s_id  VARCHAR(36)             = ?,\n"+
			"  %s_cid INTEGER                 = ?,\n"+
			"  %s_seg VARCHAR(24)             = ?,\n"+
			"  %s_tel VARCHAR(40)             = ?,\n"+
			"  %s_cue BLOB SUB_TYPE TEXT       = ?,\n"+
			"  %s_est VARCHAR(16)             = ?,\n"+
			"  %s_snd VARCHAR(12)             = ?,\n"+
			"  %s_enc TIMESTAMP               = ?,\n"+
			"  %s_env TIMESTAMP               = ?,\n"+
			"  %s_err VARCHAR(500)            = ?,\n"+
			"  %s_cat TIMESTAMP               = ?,\n"+
			"  %s_upd TIMESTAMP               = ?",
		p, p, p, p, p,
		p, p, p, p, p,
		p, p,
	)
}

// appendInsertBodyStmt writes the INSERT statement for a single row prefix p
// into w.
func appendInsertBodyStmt(w *strings.Builder, p string) {
	_, _ = fmt.Fprintf(
		w,
		"  INSERT INTO MSP_RX_MENSAJES\n"+
			"    (ID,CLIENTE_ID,SEGMENTO,TELEFONO,CUERPO,ESTADO,SENDER_KIND,\n"+
			"     ENCOLADO_EN,ENVIADO_EN,ERROR,CREATED_AT,UPDATED_AT)\n"+
			"  VALUES(:%s_id,:%s_cid,:%s_seg,:%s_tel,:%s_cue,:%s_est,:%s_snd,\n"+
			"         :%s_enc,:%s_env,:%s_err,:%s_cat,:%s_upd);\n",
		p, p, p, p, p, p, p,
		p, p, p, p, p,
	)
}

// appendInsertArgs appends the 12 bound arguments for mensaje m (in
// param-declaration order) to args and returns the extended slice.
func appendInsertArgs(args []any, m *domain.Mensaje) []any {
	return append(
		args,
		m.ID().String(),                      // _id
		m.ClienteID(),                        // _cid
		m.Segmento().String(),                // _seg
		m.Telefono(),                         // _tel
		m.Cuerpo(),                           // _cue
		m.Estado().String(),                  // _est
		nil,                                  // _snd — always NULL at insert (encolado)
		firebird.ToWallClock(m.EncoladoEn()), // _enc
		nil,                                  // _env — always NULL at insert
		nil,                                  // _err — always NULL at insert
		firebird.ToWallClock(m.CreatedAt()),  // _cat
		firebird.ToWallClock(m.UpdatedAt()),  // _upd
	)
}

// ListarPendientes returns up to limit mensajes with ESTADO='encolado',
// ordered by ENCOLADO_EN ascending. limit <= 0 returns every pendiente.
func (r *Repo) ListarPendientes(ctx context.Context, limit int) ([]*domain.Mensaje, error) {
	query := selectMensajePendientesSinLimite
	var args []any
	if limit > 0 {
		query = selectMensajePendientes
		args = []any{limit}
	}
	return r.queryMensajes(ctx, query, args...)
}

// Actualizar persists m's current state (ESTADO, SENDER_KIND, ENVIADO_EN,
// ERROR, UPDATED_AT) by ID.
func (r *Repo) Actualizar(ctx context.Context, m *domain.Mensaje) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(
		ctx, updateMensaje,
		m.Estado().String(),
		nullableSenderKindArg(m.SenderKind()),
		nullableWallClockArg(wallClockPtrFromTime(m.EnviadoEn())),
		nullableStringArg(m.Motivo()),
		firebird.ToWallClock(m.UpdatedAt()),
		m.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// Listar returns mensajes matching p, ordered by ENCOLADO_EN ASC.
func (r *Repo) Listar(ctx context.Context, p outbound.ListarMensajesParams) ([]*domain.Mensaje, error) {
	where, args := buildMensajeWhere(p)
	query := selectMensajeBase
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY ENCOLADO_EN"
	if p.Limit > 0 {
		query += " ROWS ?"
		args = append(args, p.Limit)
	}
	return r.queryMensajes(ctx, query, args...)
}

// buildMensajeWhere builds the WHERE clause and positional args for Listar.
func buildMensajeWhere(p outbound.ListarMensajesParams) (string, []any) {
	var clauses []string
	var args []any
	if p.Estado != "" {
		clauses = append(clauses, "ESTADO = ?")
		args = append(args, p.Estado.String())
	}
	if p.Segmento != "" {
		clauses = append(clauses, "SEGMENTO = ?")
		args = append(args, p.Segmento.String())
	}
	return strings.Join(clauses, " AND "), args
}

// ContarEnviadosHoy counts mensajes with ESTADO='enviado' AND ENVIADO_EN >= desde.
func (r *Repo) ContarEnviadosHoy(ctx context.Context, desde time.Time) (int, error) {
	var count int
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, selectContarEnviadosHoy, firebird.ToWallClock(desde))
		return firebird.MapError(row.Scan(&count))
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ClientesConMensaje returns the set of clienteIDs with at least one
// MSP_RX_MENSAJES row.
func (r *Repo) ClientesConMensaje(ctx context.Context) (map[int]bool, error) {
	result := make(map[int]bool)
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectClientesConMensaje)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var clienteID int
			if serr := rows.Scan(&clienteID); serr != nil {
				return firebird.MapError(serr)
			}
			result[clienteID] = true
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// queryMensajes runs a mensajeCols-shaped SELECT and assembles the results.
func (r *Repo) queryMensajes(ctx context.Context, query string, args ...any) ([]*domain.Mensaje, error) {
	var result []*domain.Mensaje
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw mensajeRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			m, serr := assembleMensaje(&raw)
			if serr != nil {
				return serr
			}
			result = append(result, m)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ─── CohorteRepo — MarcarContactado ─────────────────────────────────────────

// MarcarContactado sets FUE_CONTACTADO=1 for clienteID, stamping UPDATED_AT
// with now. A single point UPDATE — it never touches EN_CONTROL or
// COHORTE_FECHA.
func (r *Repo) MarcarContactado(ctx context.Context, clienteID int, now time.Time) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(ctx, updateCohorteMarcarContactado, firebird.ToWallClock(now), clienteID)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// ─── Nullable-arg helpers (mensaje-specific) ────────────────────────────────

// nullableSenderKindArg returns nil (SQL NULL) for an empty SenderKind,
// otherwise its string value.
func nullableSenderKindArg(k domain.SenderKind) any {
	if k == "" {
		return nil
	}
	return k.String()
}

// nullableStringArg returns nil (SQL NULL) for an empty string, otherwise the
// string itself.
func nullableStringArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}
