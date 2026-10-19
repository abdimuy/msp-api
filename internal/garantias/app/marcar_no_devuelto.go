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
	rol, err := rolDecisorPtr(cmd.RolDecisor)
	if err != nil {
		return nil, err
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoAutorizar,
		actorDeConRol(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon, rol),
		domain.TipoEventoDevolucionFallida,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.MarcarNoDevuelto(cmd.ArticuloID, actor, s.clock.Now())
		})
}
