// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// MoverArticuloCmd contains input to record a manual location change.
type MoverArticuloCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	Ubicacion         string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// MoverArticulo records where the operator physically put an article (R3). It
// only sets the working locations: entregado and baja are the stage's business,
// so the domain rejects them with ErrUbicacionManualNoPermitida.
func (s *Service) MoverArticulo(ctx context.Context, cmd MoverArticuloCmd) (*domain.Garantia, error) {
	ubicacion, err := domain.ParseUbicacion(cmd.Ubicacion)
	if err != nil {
		return nil, err
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoActualizar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.TipoEventoArticuloMovido,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.MoverArticulo(cmd.ArticuloID, ubicacion, actor, s.clock.Now())
		})
}
