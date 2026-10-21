// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// MarcarNoDevueltoCmd contains input for the "could not return it" branch.
type MarcarNoDevueltoCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	RolDecisor        string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// MarcarNoDevuelto moves an article from espera_respuesta_cliente to standby
// without creating a replacement (R8). It is a decision, so it asks for the
// autorizar permission and a decider role, and it emits devolucion_fallida.
func (s *Service) MarcarNoDevuelto(ctx context.Context, cmd MarcarNoDevueltoCmd) (*domain.Garantia, error) {
	return s.ejecutarDecision(ctx,
		nuevoDecisionCmd(cmd.GarantiaID, cmd.ArticuloID, cmd.RolDecisor,
			cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.PermisoAutorizar,
		domain.TipoEventoDevolucionFallida,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.MarcarNoDevuelto(cmd.ArticuloID, actor, ahora)
		})
}
