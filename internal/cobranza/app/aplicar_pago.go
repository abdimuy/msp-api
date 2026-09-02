//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// falloPersistTimeout bounds the out-of-band transaction that records a
// writer rejection.
//
// What it actually bounds: the wait for a free connection from the pool, and
// database/sql's own bookkeeping. It does NOT interrupt a query already in
// flight — firebird's driver wrapper (internal/platform/firebird/driverwrap.go)
// re-wraps every driver call in context.WithoutCancel and relies on a
// server-side statement timeout instead, precisely so a cancelled context
// cannot poison a pooled connection. So this is a bound on "how long we wait
// to even start", not a kill switch.
//
// It still earns its place: the context this transaction runs on has had its
// cancellation stripped on purpose, so without a deadline of its own the
// retry worker could block on a saturated pool with nothing to wake it.
const falloPersistTimeout = 5 * time.Second

// ErrAplicarPagoDentroDeTx is returned when AplicarPago is invoked with a
// transaction already open on the context. It needs its own so the failure
// record can outlive the rollback of a rejected apply; joining an outer
// transaction would roll that record back too — the exact defect this flow
// was fixed to stop.
var ErrAplicarPagoDentroDeTx = apperror.NewInternal(
	"aplicar_pago_dentro_de_tx",
	"no se puede aplicar un pago dentro de otra transacción",
)

// AplicarPago materializes an ALREADY-PERSISTED PagoRecibido into Microsip.
// It is the entry point for the retry worker and for the admin
// AplicarPagoForzar endpoint — both of which operate on rows that are already
// committed, hence the pessimistic locking.
//
// The creation flow does NOT come through here: a pago born inside
// CrearPagoConImagenes is pushed by [Service.aplicarPagoRecienCreado] inside
// the transaction that inserted it, with no lock and no reload, because that
// same transaction owns the row.
//
// The flow:
//
//  1. Lock the row (SELECT WITH LOCK).
//  2. Reload the pago and check the state machine — if already aplicada,
//     return idempotent (no writer call).
//  3. Call the writer (5 INSERTs to Microsip in the same tx).
//  4. On writer error: propagate, and record the failed attempt in a
//     SEPARATE transaction that re-takes the lock (see registrarFalloAparte).
//  5. On writer success: MarcarAplicada, persist, return the updated pago.
//
// Serialization, stated precisely because a looser version of this sentence
// hid a defect: EVERY write to the row happens under the row lock, including
// the failure record in step 4, which re-acquires it. Two concurrent attempts
// on the same UUID therefore serialize; the loser sees SincronizacionAplicada
// and short-circuits via the idempotent fast-path, and a failure record can
// never overwrite a pago that somebody else applied in the meantime. No
// double-INSERT to Microsip is possible.
//
// Refuses to run inside another transaction (ErrAplicarPagoDentroDeTx): it
// needs its own, or the failure record would be rolled back with it.
func (s *Service) AplicarPago(ctx context.Context, pagoID, by uuid.UUID) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	if s.microsipPago == nil {
		return nil, errWriteDepsMissing("microsip_pago_writer")
	}
	if s.txMgr == nil {
		return nil, errWriteDepsMissing("tx_manager")
	}
	if s.txMgr.HasTx(ctx) {
		return nil, ErrAplicarPagoDentroDeTx
	}

	// rechazado is set only when Microsip itself refused the pago — the one
	// case that earns an intento. A MarcarAplicada or Update failure is a bug
	// on our side, not a Microsip attempt, and must not inflate the counter.
	var aplicado *domain.PagoRecibido
	var rechazado bool
	err := s.runInTx(ctx, func(ctx context.Context) error {
		if lockErr := s.pagosRecibidos.LockByID(ctx, pagoID); lockErr != nil {
			return lockErr
		}
		pago, findErr := s.pagosRecibidos.FindByID(ctx, pagoID)
		if findErr != nil {
			return findErr
		}
		if pago.IsAplicada() {
			// Idempotent fast-path: another concurrent attempt won the race.
			aplicado = pago
			return nil
		}
		if preErr := pago.PreconditionForAplicar(); preErr != nil {
			return preErr
		}

		res, writerErr := s.microsipPago.Aplicar(ctx, microsipInputFrom(pago))
		if writerErr != nil {
			rechazado = true
			return writerErr
		}
		if markErr := pago.MarcarAplicada(res.DoctoCCID, res.ImpteDoctoCCID, res.Folio, s.clock.Now(), by); markErr != nil {
			return markErr
		}
		if updErr := s.pagosRecibidos.Update(ctx, pago); updErr != nil {
			return updErr
		}
		aplicado = pago
		return nil
	})
	if err != nil {
		if rechazado {
			if falloErr := s.registrarFalloAparte(ctx, pagoID, err, by); falloErr != nil {
				s.logFalloNoPersistido(ctx, pagoID, err, falloErr)
				// Surface both: the rejection explains what Microsip said, the
				// persist error explains why nobody will see it recorded.
				return nil, errors.Join(err, falloErr)
			}
		}
		return nil, err
	}
	return aplicado, nil
}

// aplicarPagoRecienCreado pushes to Microsip a pago that the CALLER's already
// open transaction has just inserted. No LockByID, no reload: the row is not
// visible to anybody else yet and the aggregate in hand is the freshest state
// there is.
//
// Any error returned here rolls the caller's transaction back, so the pago
// row disappears with it. That is the point: a pago Microsip rejected must
// not survive as an invisible ESTADO='P' row behind a 2xx. The rejection is
// evidence for MSP_FAILED_INTENTS — one table, not two.
func (s *Service) aplicarPagoRecienCreado(ctx context.Context, pago *domain.PagoRecibido, by uuid.UUID) error {
	res, err := s.microsipPago.Aplicar(ctx, microsipInputFrom(pago))
	if err != nil {
		return err
	}
	if markErr := pago.MarcarAplicada(res.DoctoCCID, res.ImpteDoctoCCID, res.Folio, s.clock.Now(), by); markErr != nil {
		return markErr
	}
	return s.pagosRecibidos.Update(ctx, pago)
}

// registrarFalloAparte persists INTENTOS + ULTIMO_ERROR in a transaction of
// its OWN, after the apply transaction has already rolled back.
//
// Why it has to be separate: RegistrarFallo + Update used to run inside the
// very closure that then returned the writer error, so Firebird rolled them
// back along with everything else. INTENTOS stayed at 0 forever — no backoff
// for the retry worker, and no trace of what Microsip actually said.
//
// Why it re-takes the lock and re-reads instead of writing back the aggregate
// the apply transaction had in hand: that aggregate is a snapshot from BEFORE
// the lock was released, and the repo's Update is a blind UPDATE ... WHERE ID
// that rewrites ESTADO, DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, FOLIO and APLICADO_AT.
// If another caller (AplicarPagoForzar, another worker) applied the pago in
// the window between the two transactions, that blind write would resurrect it
// as pendiente with its Microsip references erased — and the next tick would
// send it to Microsip a SECOND time, because PagoWriter.Aplicar does not
// deduplicate. Re-reading under the lock also keeps INTENTOS monotonic:
// blind snapshot+1 writes from concurrent failures would stall the counter at
// 1 and MaxIntentos would never be reached.
//
// Why a plain second RunInTx is enough (no new platform primitive):
// firebird.runInTx joins an existing transaction only when the CONTEXT
// carries one, and it plants that key on a context local to the call. The
// caller's context never sees it, so a RunInTx issued after the first closure
// returned opens a genuinely new transaction. The HasTx guard at the top of
// AplicarPago is what keeps that true.
//
// Why the context loses its cancellation: same reason as
// failedintent.saveIntent. If the phone hangs up mid-request, the attempt
// still has to be counted, or the worker retries with no backoff and support
// has no idea why the pago failed.
func (s *Service) registrarFalloAparte(
	ctx context.Context, pagoID uuid.UUID, cause error, by uuid.UUID,
) error {
	falloCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), falloPersistTimeout)
	defer cancel()
	return s.runInTx(falloCtx, func(ctx context.Context) error {
		if lockErr := s.pagosRecibidos.LockByID(ctx, pagoID); lockErr != nil {
			return lockErr
		}
		fresh, findErr := s.pagosRecibidos.FindByID(ctx, pagoID)
		if findErr != nil {
			return findErr
		}
		if fresh.IsAplicada() {
			// Somebody else applied it while our lock was released. There is
			// no failed attempt to count against a pago that succeeded, and
			// writing one would erase its Microsip references.
			return nil
		}
		fresh.RegistrarFallo(cause.Error(), s.clock.Now(), by)
		return s.pagosRecibidos.Update(ctx, fresh)
	})
}

// logFalloNoPersistido reports that the failure record could not be written.
// It is the one degradation this flow can still suffer in silence — the pago
// keeps intentos=0, so the worker retries it every tick with no backoff and
// nobody can see what Microsip said. Searchable on purpose: this is the line
// to grep for across the fleet when a pago is being hammered.
func (s *Service) logFalloNoPersistido(ctx context.Context, pagoID uuid.UUID, cause, persistErr error) {
	slog.ErrorContext(ctx, "pago.fallo_no_persistido",
		slog.String("pago_id", pagoID.String()),
		slog.String("rechazo", cause.Error()),
		slog.String("error", persistErr.Error()),
	)
}

// microsipInputFrom projects the aggregate onto the writer's input VO. Shared
// by both apply paths so the two can never drift on which fields Microsip
// receives.
func microsipInputFrom(pago *domain.PagoRecibido) outbound.MicrosipPagoInput {
	return outbound.MicrosipPagoInput{
		CargoDoctoCCID: pago.CargoDoctoCCID(),
		ClienteID:      pago.ClienteID(),
		CobradorID:     pago.CobradorID(),
		Cobrador:       pago.Cobrador(),
		FormaCobroID:   pago.FormaCobroID(),
		ConceptoCCID:   pago.ConceptoCCID(),
		Importe:        pago.Importe(),
		FechaHoraPago:  pago.FechaHoraPago(),
		Lat:            pago.Lat(),
		Lon:            pago.Lon(),
	}
}

// ObtenerPago loads a PagoRecibido by ID. Read-side convenience method for
// the HTTP layer; returns domain.ErrPagoNoEncontrado on miss.
func (s *Service) ObtenerPago(ctx context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	return s.pagosRecibidos.FindByID(ctx, id)
}

// ListarPagosPendientes returns the pendientes drained by the retry worker.
// Exposed on Service so the admin endpoint can inspect the outbox state.
func (s *Service) ListarPagosPendientes(ctx context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	if limit <= 0 {
		limit = 100
	}
	if maxIntentos <= 0 {
		maxIntentos = 10
	}
	return s.pagosRecibidos.ListPendientes(ctx, maxIntentos, limit)
}
