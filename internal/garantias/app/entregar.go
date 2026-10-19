// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// EntregarCmd contains input to hand the folio to the client.
type EntregarCmd struct {
	GarantiaID        uuid.UUID
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// Entregar hands the folio to the client. The agent's GPS is the delivery
// audit of §4.1, so a delivery without coordinates is refused before the
// transaction opens: the folio is not even locked for a request that cannot be
// delivered. The domain keeps the same rule for the aggregate's own invariant.
func (s *Service) Entregar(ctx context.Context, cmd EntregarCmd) (*domain.Garantia, error) {
	if cmd.GPSLat == nil || cmd.GPSLon == nil {
		return nil, domain.ErrEntregaGPSRequerido
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCerrar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.TipoEventoFolioEntregado,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.Entregar(actor, s.clock.Now())
		})
}
