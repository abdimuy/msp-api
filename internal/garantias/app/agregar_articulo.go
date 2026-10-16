// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AgregarArticuloCmd contains input to add an article to a warranty.
type AgregarArticuloCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        *int
	Clave             string
	Description       string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// AgregarArticulo adds an article to an existing warranty folio.
func (s *Service) AgregarArticulo(ctx context.Context, cmd AgregarArticuloCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCrear,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.TipoEventoArticuloAgregado,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.AgregarArticulo(domain.AgregarArticuloParams{
				ArticuloID:  cmd.ArticuloID,
				Clave:       cmd.Clave,
				Description: cmd.Description,
			}, actor, s.clock.Now())
		})
}
