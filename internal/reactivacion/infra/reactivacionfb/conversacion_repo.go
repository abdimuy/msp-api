//nolint:misspell // Spanish domain vocabulary (conversación, cohorte) by project convention.
package reactivacionfb

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Compile-time check: CopilotoRepo must satisfy outbound.ConversacionRepo.
// This file implements that interface (MSP_RX_CONVERSACION + MSP_RX_TURNO).
var _ outbound.ConversacionRepo = (*CopilotoRepo)(nil)

// Get returns the Conversacion for clienteID, or (nil, nil) when the cliente
// has no conversation yet.
func (r *CopilotoRepo) Get(ctx context.Context, clienteID int) (*domain.Conversacion, error) {
	var result *domain.Conversacion
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, selectConversacionByCliente, clienteID)
		var raw conversacionRowRaw
		serr := raw.scanFrom(row)
		if errors.Is(serr, sql.ErrNoRows) {
			return nil
		}
		if serr != nil {
			return firebird.MapError(serr)
		}
		c, aerr := assembleConversacion(&raw)
		if aerr != nil {
			return aerr
		}
		result = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Upsert inserts or updates c, matched by CLIENTE_ID: UPDATE first, and only
// INSERT when RowsAffected==0 (first-time conversation). Never MERGE — the
// nakagami/firebirdsql driver cannot bind parameters inside MERGE's USING
// SELECT clause (SQL error -804); see mensaje_repo.go / buildUpsertBlock.
func (r *CopilotoRepo) Upsert(ctx context.Context, c *domain.Conversacion) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	banderasJSON, err := jsonSliceArg(c.Banderas())
	if err != nil {
		return err
	}

	res, err := q.ExecContext(
		ctx, updateConversacion,
		c.Estado().String(),
		nullableStringArg(c.AsignadoA()),
		nullableStringArg(c.ResumenMemoria()),
		nullableStringArg(c.ContextoNota()),
		banderasJSON,
		nullableStringArg(c.NotaHash()),
		firebird.ToWallClock(c.UpdatedAt()),
		c.ClienteID(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		return nil
	}

	_, err = q.ExecContext(
		ctx, insertConversacion,
		c.ID(),
		c.ClienteID(),
		c.Estado().String(),
		nullableStringArg(c.AsignadoA()),
		nullableStringArg(c.ResumenMemoria()),
		nullableStringArg(c.ContextoNota()),
		banderasJSON,
		nullableStringArg(c.NotaHash()),
		firebird.ToWallClock(c.CreatedAt()),
		firebird.ToWallClock(c.UpdatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// Listar returns conversaciones matching p, ordered by UPDATED_AT DESC (most
// recently active first).
//
//nolint:dupl // structurally mirrors Repo.ListarCohorte / CopilotoRepo.ListarTurnos; differs in query + row type + return type — abstraction not worth it
func (r *CopilotoRepo) Listar(ctx context.Context, p outbound.ListarConversacionesParams) ([]*domain.Conversacion, error) {
	where, args := buildConversacionWhere(p)
	var result []*domain.Conversacion
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		query := selectConversacionBase
		if where != "" {
			query += " WHERE " + where
		}
		query += " ORDER BY UPDATED_AT DESC"
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw conversacionRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			c, aerr := assembleConversacion(&raw)
			if aerr != nil {
				return aerr
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

// buildConversacionWhere builds the WHERE clause and positional args for
// Listar: Estado (if set) filters to that exact state, and SoloEscaladas
// filters to EstadoEscalado. Both may be combined (an AND); it is the
// caller's responsibility to pass a coherent combination.
func buildConversacionWhere(p outbound.ListarConversacionesParams) (string, []any) {
	var clauses []string
	var args []any
	if p.Estado != "" {
		clauses = append(clauses, "ESTADO = ?")
		args = append(args, p.Estado)
	}
	if p.SoloEscaladas {
		clauses = append(clauses, "ESTADO = ?")
		args = append(args, domain.EstadoEscalado.String())
	}
	return strings.Join(clauses, " AND "), args
}

// AppendTurno appends t to the conversation's turn log. Turnos are
// append-only — this is a plain INSERT, never an upsert.
func (r *CopilotoRepo) AppendTurno(ctx context.Context, t *domain.Turno) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(
		ctx, insertTurno,
		t.ID(),
		t.ClienteID(),
		t.Direccion().String(),
		t.Autor().String(),
		t.Cuerpo(),
		nullableStringArg(t.MensajeRef()),
		firebird.ToWallClock(t.CreatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// ListarTurnos returns every turno for clienteID, ordered by CreatedAt
// ascending (chronological) — the app replays the conversation in this
// order.
//
//nolint:dupl // structurally mirrors CopilotoRepo.ListarPorCliente; differs in query + row type + return type — abstraction not worth it
func (r *CopilotoRepo) ListarTurnos(ctx context.Context, clienteID int) ([]*domain.Turno, error) {
	var result []*domain.Turno
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectTurnosByCliente, clienteID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw turnoRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			t, aerr := assembleTurno(&raw)
			if aerr != nil {
				return aerr
			}
			result = append(result, t)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
