// Package app contains the use cases of the garantias module.
//
//nolint:dupl // article-scoped non-decision commands with near-identical envelope/parse/delegate shape; each differs in its parsed value and aggregate call.
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
//
//nolint:dupl // same envelope as RegistrarDictamen (parse, delegate); each parses its own field and calls its own aggregate method.
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
