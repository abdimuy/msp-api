// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// CerrarCmd contains input to seal a delivered folio.
//
// JustificacionDesvio is the explanation the operator writes when the delivery
// GPS is farther from the pickup than the threshold of spec §4.1. The domain
// only demands it when the audit found a deviation; when it does not, a
// provided one is stored anyway.
type CerrarCmd struct {
	GarantiaID          uuid.UUID
	JustificacionDesvio string
	ClaveIdempotencia   string
	DeviceCreatedAt     time.Time
	GPSLat              *float64
	GPSLon              *float64
}

// Cerrar seals a delivered folio as cerrado. With the folio locked it reads the
// timeline and hands the domain the delivery point and the pickups; the
// distance, the outcome and the justification live in the domain (D3). The
// domain rejects a folio that is not entregado with ErrTransicionEstadoNoPermitida.
func (s *Service) Cerrar(ctx context.Context, cmd CerrarCmd) (*domain.Garantia, error) {
	huella := huellaDe(cmd)
	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCerrar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon, huella),
		huella,
		func(ctx context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			eventos, err := s.eventos.ListarPorGarantia(ctx, g.ID())
			if err != nil {
				return err
			}
			entrega, recolecciones := puntosDeAuditoria(eventos)
			return g.Cerrar(entrega, recolecciones, cmd.JustificacionDesvio, actor, s.clock.Now())
		})
}

// puntosDeAuditoria extracts the points the delivery audit needs from the
// timeline: the GPS of the last folio_entregado and the GPS of every
// etapa_avanzada that reached recolectado. An event without both coordinates
// contributes nothing — half a position is not a place.
func puntosDeAuditoria(eventos []*domain.Evento) (*domain.PuntoGPS, []domain.PuntoGPS) {
	var entrega *domain.PuntoGPS
	var recolecciones []domain.PuntoGPS
	for _, e := range eventos {
		lat, lon := e.GPSLat(), e.GPSLon()
		if lat == nil || lon == nil {
			continue
		}
		punto := domain.PuntoGPS{Lat: *lat, Lon: *lon}
		switch {
		case e.Tipo() == domain.TipoEventoFolioEntregado:
			entrega = &punto
		case e.Tipo() == domain.TipoEventoEtapaAvanzada &&
			e.EtapaHasta() != nil && *e.EtapaHasta() == domain.EtapaRecolectado:
			recolecciones = append(recolecciones, punto)
		}
	}
	return entrega, recolecciones
}
