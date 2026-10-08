// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AvanzarArticuloCmd contains input to advance an article stage.
type AvanzarArticuloCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	EtapaDestino      domain.Etapa
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// AvanzarArticulo advances an article to the requested stage. It covers the
// collection trunk: registrado -> pendiente_recoleccion -> recolectado ->
// en_revision, and every other edge the stage machine allows.
func (s *Service) AvanzarArticulo(ctx context.Context, cmd AvanzarArticuloCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoActualizar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.TipoEventoEtapaAvanzada,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.AvanzarArticulo(cmd.ArticuloID, cmd.EtapaDestino, actor, s.clock.Now())
		})
}
