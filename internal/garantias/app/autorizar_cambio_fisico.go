// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AutorizarCambioFisicoCmd contains input to authorize a physical swap.
type AutorizarCambioFisicoCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	RolDecisor        string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// AutorizarCambioFisico puts the original article in standby and creates its
// replacement. It is the decision that spends money, so it asks for the
// autorizar permission — never actualizar — and demands a decider role.
func (s *Service) AutorizarCambioFisico(ctx context.Context, cmd AutorizarCambioFisicoCmd) (*domain.Garantia, error) {
	rol, err := rolDecisorPtr(cmd.RolDecisor)
	if err != nil {
		return nil, err
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoAutorizar,
		actorDeConRol(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon, rol),
		domain.TipoEventoCambioAutorizado,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.AutorizarCambioFisico(cmd.ArticuloID, actor, s.clock.Now())
		})
}
