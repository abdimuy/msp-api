// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// IniciarProcesoCmd contains input to move the folio from abierto to en_proceso.
type IniciarProcesoCmd struct {
	GarantiaID        uuid.UUID
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// IniciarProceso starts the warranty process on the folio. The domain rejects
// a folio without articles, so the command does not pre-check it.
func (s *Service) IniciarProceso(ctx context.Context, cmd IniciarProcesoCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoActualizar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.IniciarProceso(actor, s.clock.Now())
		})
}
