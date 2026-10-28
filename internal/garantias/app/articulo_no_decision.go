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
	return s.ejecutarArticulo(ctx,
		articuloCmd{
			GarantiaID:        cmd.GarantiaID,
			ArticuloID:        cmd.ArticuloID,
			ClaveIdempotencia: cmd.ClaveIdempotencia,
			DeviceCreatedAt:   cmd.DeviceCreatedAt,
			GPSLat:            cmd.GPSLat,
			GPSLon:            cmd.GPSLon,
		},
		domain.PermisoActualizar,
		domain.TipoEventoArticuloMovido,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.MoverArticulo(cmd.ArticuloID, ubicacion, actor, ahora)
		})
}

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
	return s.ejecutarArticulo(ctx,
		articuloCmd{
			GarantiaID:        cmd.GarantiaID,
			ArticuloID:        cmd.ArticuloID,
			ClaveIdempotencia: cmd.ClaveIdempotencia,
			DeviceCreatedAt:   cmd.DeviceCreatedAt,
			GPSLat:            cmd.GPSLat,
			GPSLon:            cmd.GPSLon,
		},
		domain.PermisoActualizar,
		domain.TipoEventoDictamenRegistrado,
		func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error {
			return g.RegistrarDictamen(cmd.ArticuloID, dictamen, actor, ahora)
		})
}
