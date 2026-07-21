//nolint:misspell // Spanish domain vocabulary (decisión) by project convention.
package reactivacionfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Compile-time check: CopilotoRepo must satisfy outbound.DecisionRepo. This
// file implements that interface (MSP_RX_DECISION).
var _ outbound.DecisionRepo = (*CopilotoRepo)(nil)

// Insertar appends a newly produced decision. Every row is a fresh INSERT —
// never an upsert (MSP_RX_DECISION is an append-only audit log).
func (r *CopilotoRepo) Insertar(ctx context.Context, d *domain.Decision) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	senalesJSON, err := jsonSliceArg(d.Senales())
	if err != nil {
		return err
	}
	evidenciaJSON, err := jsonSliceArg(d.Evidencia())
	if err != nil {
		return err
	}

	_, err = q.ExecContext(
		ctx, insertDecision,
		d.ID(),
		d.ClienteID(),
		nullableStringArg(d.TurnoRef()),
		nullableStringArg(d.Intencion()),
		d.Confianza(),
		senalesJSON,
		d.AccionPropuesta().String(),
		nullableStringArg(d.Borrador()),
		evidenciaJSON,
		nullableStringArg(d.RazonEscalamiento()),
		d.Resultado().String(),
		firebird.ToWallClock(d.CreatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// ListarPorCliente returns every decision for clienteID, ordered by
// CreatedAt ascending (the app treats the LAST element as the newest
// decision — see app.newestDecisionPorClienteID).
//
//nolint:dupl // structurally mirrors CopilotoRepo.ListarTurnos; differs in query + row type + return type — abstraction not worth it
func (r *CopilotoRepo) ListarPorCliente(ctx context.Context, clienteID int) ([]*domain.Decision, error) {
	var result []*domain.Decision
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectDecisionesByCliente, clienteID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw decisionRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			d, aerr := assembleDecision(&raw)
			if aerr != nil {
				return aerr
			}
			result = append(result, d)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
