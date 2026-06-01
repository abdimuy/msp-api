//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// AplicarPago materializes a PagoRecibido into Microsip via the
// MicrosipPagoWriter, in a single Firebird transaction with pessimistic
// row-level locking to serialize concurrent attempts.
//
// The flow:
//
//  1. Lock the row (SELECT WITH LOCK).
//  2. Reload the pago and check the state machine — if already aplicada,
//     return idempotent (no writer call).
//  3. Build writer input from the aggregate.
//  4. Call the writer (5 INSERTs to Microsip in the same tx).
//  5. On writer error: RegistrarFallo, persist, return error.
//  6. On writer success: MarcarAplicada, persist, return the updated pago.
//
// Two concurrent attempts on the same UUID serialize on the lock; the second
// one sees SincronizacionAplicada after the first commits and short-circuits
// via the idempotent fast-path. No double-INSERT to Microsip is possible.
func (s *Service) AplicarPago(ctx context.Context, pagoID, by uuid.UUID) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	if s.microsipPago == nil {
		return nil, errWriteDepsMissing("microsip_pago_writer")
	}

	var aplicado *domain.PagoRecibido
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

		writerIn := outbound.MicrosipPagoInput{
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
		res, writerErr := s.microsipPago.Aplicar(ctx, writerIn)
		if writerErr != nil {
			// Record the failure on the aggregate and persist — the row stays
			// ESTADO='P' so the retry worker picks it up later. Propagate the
			// original error so the caller (HTTP handler / retry worker) can
			// log it.
			pago.RegistrarFallo(writerErr.Error(), s.clock.Now(), by)
			if updErr := s.pagosRecibidos.Update(ctx, pago); updErr != nil {
				// If we can't even persist the failure record, surface the
				// update error (more actionable than the writer error,
				// which is already lost).
				return errors.Join(writerErr, updErr)
			}
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
		return nil, err
	}
	return aplicado, nil
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
