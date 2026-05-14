package domain

import (
	"time"

	"github.com/google/uuid"
)

// Aprobacion records the moment a venta transitioned from 'borrador' to
// 'aprobada'. Mirrors Cancelacion in shape but without a free-form reason
// — the approval flow is internal and identified by who/when only.
//
// Reserved by the schema: there is no app-level command to produce one yet.
// The fields are populated by the (future) approval flow that promotes the
// venta to Microsip.
type Aprobacion struct {
	at time.Time
	by uuid.UUID
}

// NewAprobacion validates and constructs an Aprobacion.
func NewAprobacion(at time.Time, by uuid.UUID) (Aprobacion, error) {
	if at.IsZero() {
		return Aprobacion{}, ErrAprobacionFechaZero
	}
	if by == uuid.Nil {
		return Aprobacion{}, ErrAprobacionByRequired
	}
	return Aprobacion{at: at, by: by}, nil
}

// HydrateAprobacion rebuilds an Aprobacion from persistence without
// validation.
func HydrateAprobacion(at time.Time, by uuid.UUID) Aprobacion {
	return Aprobacion{at: at, by: by}
}

// At returns the approval timestamp.
func (a Aprobacion) At() time.Time { return a.at }

// By returns the user that approved the venta.
func (a Aprobacion) By() uuid.UUID { return a.by }
