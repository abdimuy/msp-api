//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// falloPersistTimeout bounds the out-of-band transaction that records a
// writer rejection. It exists because that transaction deliberately runs on a
// context stripped of cancellation (see registrarFalloAparte): without an
// upper bound, a wedged Firebird would park the retry worker forever.
const falloPersistTimeout = 5 * time.Second

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
//     SEPARATE transaction (see registrarFalloAparte).
//  5. On writer success: MarcarAplicada, persist, return the updated pago.
//
// Two concurrent attempts on the same UUID serialize on the lock; the second
// one sees SincronizacionAplicada after the first commits and short-circuits
// via the idempotent fast-path. No double-INSERT to Microsip is possible.
//
// Must not be invoked from inside another transaction: it needs its own so
// the failure record can outlive a rollback.
func (s *Service) AplicarPago(ctx context.Context, pagoID, by uuid.UUID) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	if s.microsipPago == nil {
		return nil, errWriteDepsMissing("microsip_pago_writer")
	}

	// rechazado is set only when Microsip itself refused the pago — the one
	// case that earns an intento. A MarcarAplicada or Update failure is a bug
	// on our side, not a Microsip attempt, and must not inflate the counter.
	var aplicado, rechazado *domain.PagoRecibido
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
			rechazado = pago
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
		if rechazado != nil {
			if falloErr := s.registrarFalloAparte(ctx, rechazado, err, by); falloErr != nil {
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
// Why a plain second RunInTx is enough (no new platform primitive):
// firebird.runInTx joins an existing transaction only when the CONTEXT
// carries one, and it plants that key on a context local to the call. The
// caller's context never sees it, so a RunInTx issued after the first closure
// returned opens a genuinely new transaction. AplicarPago is documented as
// not callable from inside another transaction, which is what keeps that
// true.
//
// Why the context loses its cancellation: same reason as
// failedintent.saveIntent. If the phone hangs up mid-request, the attempt
// still has to be counted, or the worker retries with no backoff and support
// has no idea why the pago failed. falloPersistTimeout keeps that from
// becoming an unbounded wait.
func (s *Service) registrarFalloAparte(
	ctx context.Context, pago *domain.PagoRecibido, cause error, by uuid.UUID,
) error {
	pago.RegistrarFallo(cause.Error(), s.clock.Now(), by)
	falloCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), falloPersistTimeout)
	defer cancel()
	return s.runInTx(falloCtx, func(ctx context.Context) error {
		return s.pagosRecibidos.Update(ctx, pago)
	})
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
