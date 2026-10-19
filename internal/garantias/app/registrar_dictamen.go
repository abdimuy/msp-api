// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// RegistrarDictamenCmd contains input to apply the supplier's verdict.
type RegistrarDictamenCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	Dictamen          string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// RegistrarDictamen applies the supplier's verdict and advances the article in
// the same call. It is not a folio-level decision, so it carries no decider
// role: the domain only admits it from dictamen_recibido on a supplier route.
func (s *Service) RegistrarDictamen(ctx context.Context, cmd RegistrarDictamenCmd) (*domain.Garantia, error) {
	dictamen, err := domain.ParseDictamen(cmd.Dictamen)
	if err != nil {
		return nil, err
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoActualizar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.TipoEventoDictamenRegistrado,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.RegistrarDictamen(cmd.ArticuloID, dictamen, actor, s.clock.Now())
		})
}
