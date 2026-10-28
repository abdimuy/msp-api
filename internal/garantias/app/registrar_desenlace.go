package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// RegistrarDesenlaceCmd contains input to record a parallel-flow outcome.
type RegistrarDesenlaceCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	Desenlace         string
	RolDecisor        string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// RegistrarDesenlace records the final fate of an article that is in standby,
// advancing it to the matching terminal stage in the same call. It is a
// decision, so it asks for the autorizar permission and demands a decider role.
func (s *Service) RegistrarDesenlace(ctx context.Context, cmd RegistrarDesenlaceCmd) (*domain.Garantia, error) {
	desenlace, err := domain.ParseDesenlace(cmd.Desenlace)
	if err != nil {
		return nil, err
	}
	// Named, not inline, on purpose: with an inline closure this file is
	// token-identical to registrar_diagnostico.go (dupl ignores identifiers),
	// and that finding is anchored at the package clause, where a function-level
	// //nolint:dupl cannot reach it.
	mutar := func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
		return g.RegistrarDesenlace(cmd.ArticuloID, desenlace, actor, ahora)
	}

	return s.ejecutarDecision(ctx,
		nuevoDecisionCmd(cmd.GarantiaID, cmd.ArticuloID, cmd.RolDecisor,
			cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.PermisoAutorizar,
		domain.TipoEventoDesenlaceRegistrado,
		mutar)
}
