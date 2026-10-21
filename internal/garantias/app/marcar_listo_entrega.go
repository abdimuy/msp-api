// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// MarcarListoEntregaCmd contains input to declare the folio ready to deliver.
type MarcarListoEntregaCmd struct {
	GarantiaID        uuid.UUID
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// MarcarListoEntrega moves the folio to listo_entrega once every article is
// ready. Since R7 it emits its own folio_listo_entrega event instead of the
// stage event it used to share with IniciarProceso.
func (s *Service) MarcarListoEntrega(ctx context.Context, cmd MarcarListoEntregaCmd) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoActualizar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		huellaEvento{tipo: domain.TipoEventoFolioListoEntrega},
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.MarcarListoEntrega(actor, s.clock.Now())
		})
}
