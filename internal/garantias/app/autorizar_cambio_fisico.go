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
	return s.ejecutarDecision(ctx,
		nuevoDecisionCmd(cmd.GarantiaID, cmd.ArticuloID, cmd.RolDecisor,
			cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.PermisoAutorizar,
		domain.TipoEventoCambioAutorizado,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.AutorizarCambioFisico(cmd.ArticuloID, actor, ahora)
		})
}
