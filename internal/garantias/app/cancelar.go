// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// CancelarCmd contains input to void a folio.
type CancelarCmd struct {
	GarantiaID        uuid.UUID
	Motivo            string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// Cancelar voids an open or in-process folio. The reason is mandatory and the
// domain is the one that validates it: an empty one comes back as
// ErrMotivoCancelacionObligatorio, and a folio with an article the customer
// still has out (proveedor or en_transito) is refused with
// ErrFolioConArticuloFuera.
func (s *Service) Cancelar(ctx context.Context, cmd CancelarCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCerrar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		huellaEvento{tipo: domain.TipoEventoFolioCancelado},
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.Cancelar(cmd.Motivo, actor, s.clock.Now())
		})
}
