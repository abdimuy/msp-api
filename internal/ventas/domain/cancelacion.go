package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxCancelReasonLength is the maximum number of bytes in a cancel reason.
const maxCancelReasonLength = 500

// Cancelacion records the soft-cancellation of a venta. All three fields are
// required.
type Cancelacion struct {
	at     time.Time
	by     uuid.UUID
	reason string
}

// NewCancelacion validates and constructs a Cancelacion.
func NewCancelacion(at time.Time, by uuid.UUID, reason string) (Cancelacion, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Cancelacion{}, ErrReasonCancelacionRequerida
	}
	if len(reason) > maxCancelReasonLength {
		return Cancelacion{}, ErrReasonCancelacionDemasiadoLarga
	}
	return Cancelacion{at: at, by: by, reason: reason}, nil
}

// HydrateCancelacion rebuilds a Cancelacion from persistence without
// validation.
func HydrateCancelacion(at time.Time, by uuid.UUID, reason string) Cancelacion {
	return Cancelacion{at: at, by: by, reason: reason}
}

// At returns the cancellation timestamp.
func (c Cancelacion) At() time.Time { return c.at }

// By returns the user who performed the cancellation.
func (c Cancelacion) By() uuid.UUID { return c.by }

// Reason returns the cancellation reason text.
func (c Cancelacion) Reason() string { return c.reason }
