//nolint:misspell // cobranza vocabulary is Spanish (cobrador, importe, etc.) per project convention.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// CrearPagoInput is the request value object for [Service.CrearPago]. The ID
// is the client-generated UUID and acts as the idempotency key end-to-end
// (cliente outbox → server outbox → Microsip).
type CrearPagoInput struct {
	ID             uuid.UUID
	CargoDoctoCCID int
	ClienteID      int
	CobradorID     int
	Cobrador       string
	Importe        decimal.Decimal
	FormaCobroID   int
	FechaHoraPago  time.Time
	Lat, Lon       *string
}

// Timestamp-validation thresholds. Lifted from world-class POS practice
// (Square, Stripe Terminal, Toast): the client is authoritative on the
// payment moment, but the server validates basic bounds + flags suspicious
// gaps.
const (
	// maxFuturoTolerancia is the wall-clock skew the server tolerates before
	// rejecting a future-dated pago. Set to 5 min so phones with mildly
	// misset clocks (no NTP) still succeed.
	maxFuturoTolerancia = 5 * time.Minute
	// maxAtrasoAceptable is the absolute upper bound on backdating; pagos
	// older than this are rejected outright (likely data-entry error or
	// abuse).
	maxAtrasoAceptable = 30 * 24 * time.Hour
	// umbralLateUpload is the threshold above which we log a warning but
	// still accept the pago — typical when a cobrador uploads from a remote
	// area hours/days after capture.
	umbralLateUpload = 24 * time.Hour
)

// CrearPago persists a new PagoRecibido into the outbox and best-effort
// attempts immediate application to Microsip. Returns the persisted pago in
// whatever state it ended up — aplicada on the happy path, pendiente if the
// writer failed (the retry worker will pick it up later).
//
// Idempotency: if a pago with the same UUID already exists, returns the
// existing row instead of an error (fast-path for cliente retries). The
// client's outbox uses the UUID as the dedupe key end-to-end.
func (s *Service) CrearPago(ctx context.Context, in CrearPagoInput, by uuid.UUID) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}

	now := s.clock.Now()
	if err := validateFechaHoraPago(now, in.FechaHoraPago); err != nil {
		return nil, err
	}

	if err := s.validateCargo(ctx, in.CargoDoctoCCID, in.Importe); err != nil {
		return nil, err
	}

	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             in.ID,
		CargoDoctoCCID: in.CargoDoctoCCID,
		ClienteID:      in.ClienteID,
		CobradorID:     in.CobradorID,
		Cobrador:       in.Cobrador,
		Importe:        in.Importe,
		FormaCobroID:   in.FormaCobroID,
		FechaHoraPago:  in.FechaHoraPago,
		Lat:            in.Lat,
		Lon:            in.Lon,
		CreatedBy:      by,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}

	if err := s.pagosRecibidos.Insert(ctx, pago); err != nil {
		// Idempotency fast-path: same UUID → return existing row.
		if errors.Is(err, domain.ErrPagoYaExiste) {
			existing, findErr := s.pagosRecibidos.FindByID(ctx, in.ID)
			if findErr != nil {
				return nil, findErr
			}
			return existing, nil
		}
		return nil, err
	}

	// Best-effort fast-path: try to apply immediately. If the writer fails
	// the row stays ESTADO='P' and the retry worker handles it. We do NOT
	// surface writer errors to the cliente — the pago is already safely
	// persisted, and the cliente can confirm via subsequent sync.
	applied, applyErr := s.AplicarPago(ctx, pago.ID(), by)
	if applyErr != nil {
		slog.WarnContext(ctx, "pago.apply_fast_path_failed",
			slog.String("pago_id", pago.ID().String()),
			slog.String("error", applyErr.Error()),
		)
		// Reload the persisted state — RegistrarFallo updated intentos/error.
		reloaded, findErr := s.pagosRecibidos.FindByID(ctx, pago.ID())
		if findErr != nil {
			return pago, nil //nolint:nilerr // pago was successfully inserted; failure to reload is non-fatal here.
		}
		return reloaded, nil
	}
	return applied, nil
}

// validateFechaHoraPago applies the three world-class timestamp checks
// (futuro, muy antigua, late-upload-warn) against the server clock.
func validateFechaHoraPago(now, fechaHoraPago time.Time) error {
	if fechaHoraPago.IsZero() {
		return domain.ErrPagoFechaMuyAntigua
	}
	if fechaHoraPago.After(now.Add(maxFuturoTolerancia)) {
		return domain.ErrPagoFechaFutura
	}
	if now.Sub(fechaHoraPago) > maxAtrasoAceptable {
		return domain.ErrPagoFechaMuyAntigua
	}
	if now.Sub(fechaHoraPago) > umbralLateUpload {
		slog.Warn("pago.late_upload",
			slog.String("fecha_hora_pago", fechaHoraPago.Format(time.RFC3339)),
			slog.Duration("delay", now.Sub(fechaHoraPago)),
		)
	}
	return nil
}

// validateCargo loads the cargo's saldo and rejects the request if the cargo
// does not exist, was cancelled in Microsip, or the importe exceeds the
// remaining saldo (defense against double-collection from multiple devices).
func (s *Service) validateCargo(ctx context.Context, cargoDoctoCCID int, importe decimal.Decimal) error {
	saldo, err := s.saldos.PorCargo(ctx, cargoDoctoCCID)
	if err != nil {
		if errors.Is(err, domain.ErrSaldoNoEncontrado) {
			return domain.ErrPagoCargoNoEncontrado
		}
		return err
	}
	if saldo.CargoCancelado() {
		return domain.ErrPagoCargoCancelado
	}
	if importe.GreaterThan(saldo.Saldo()) {
		return domain.ErrPagoSaldoInsuficiente
	}
	return nil
}
