// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// CerrarCmd contains input to seal a delivered folio.
type CerrarCmd struct {
	GarantiaID        uuid.UUID
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// Cerrar seals a delivered folio as cerrado. The domain rejects a folio that is
// not entregado with ErrTransicionEstadoNoPermitida.
func (s *Service) Cerrar(ctx context.Context, cmd CerrarCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCerrar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		huellaEvento{tipo: domain.TipoEventoFolioCerrado},
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.Cerrar(actor, s.clock.Now())
		})
}
