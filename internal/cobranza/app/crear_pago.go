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

// crear_pago.go is kept as the home for [Service.CrearPago] (now a thin
// wrapper) plus the timestamp + cargo validation helpers shared with the
// atomic-multipart sibling in crear_pago_con_imagenes.go.

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

// CrearPago is the single-row entry point: persists a PagoRecibido with no
// comprobantes attached AND pushes it to Microsip, in one transaction. It is
// a thin wrapper over [Service.CrearPagoConImagenes] with imgs nil — the same
// code, not a parallel flow.
//
// There is no longer a best-effort apply behind it. A pago Microsip rejects
// takes the whole transaction down with it and the error reaches the caller:
// the row that used to survive as an invisible ESTADO='P' behind a 2xx is
// what made a cobrador believe the payment had not registered and charge the
// client again.
//
// Idempotency: if a pago with the same UUID already exists, returns the
// existing row instead of an error (fast-path for cliente retries). The
// client's outbox uses the UUID as the dedupe key end-to-end. A pago rolled
// back by a Microsip rejection never reached the table, so its UUID stays
// usable and the phone's retry works.
func (s *Service) CrearPago(ctx context.Context, in CrearPagoInput, by uuid.UUID) (*domain.PagoRecibido, error) {
	return s.CrearPagoConImagenes(ctx, in, nil, by)
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
