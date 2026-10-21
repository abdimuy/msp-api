// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// RegistrarDiagnosticoCmd contains input to fix an article's repair route.
type RegistrarDiagnosticoCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	Ruta              string
	RolDecisor        string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

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

// RegistrarDiagnostico fixes the article's repair route and advances it in the
// same call. It is a decision, so the decider role travels with the command and
// a missing one is rejected by the domain with ErrRolDecisorObligatorio.
func (s *Service) RegistrarDiagnostico(ctx context.Context, cmd RegistrarDiagnosticoCmd) (*domain.Garantia, error) {
	ruta, err := domain.ParseRutaReparacion(cmd.Ruta)
	if err != nil {
		return nil, err
	}
	return s.ejecutarDecision(ctx,
		nuevoDecisionCmd(cmd.GarantiaID, cmd.ArticuloID, cmd.RolDecisor,
			cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.PermisoActualizar,
		domain.TipoEventoDiagnosticoRegistrado,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.RegistrarDiagnostico(cmd.ArticuloID, ruta, actor, ahora)
		})
}

// RegistrarDesenlace records the final fate of an article that is in standby,
// advancing it to the matching terminal stage in the same call. It is a
// decision, so it asks for the autorizar permission and demands a decider role.
// It shares the command envelope with RegistrarDiagnostico, so both live here.
func (s *Service) RegistrarDesenlace(ctx context.Context, cmd RegistrarDesenlaceCmd) (*domain.Garantia, error) {
	desenlace, err := domain.ParseDesenlace(cmd.Desenlace)
	if err != nil {
		return nil, err
	}
	return s.ejecutarDecision(ctx,
		nuevoDecisionCmd(cmd.GarantiaID, cmd.ArticuloID, cmd.RolDecisor,
			cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		domain.PermisoAutorizar,
		domain.TipoEventoDesenlaceRegistrado,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.RegistrarDesenlace(cmd.ArticuloID, desenlace, actor, ahora)
		})
}
