package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestPuntosDeAuditoria_FiltraEventosIncompletos(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	ahora := time.Now().UTC()

	lat := 19.4326
	lon := -99.1332
	recolectado := domain.EtapaRecolectado
	otraEtapa := domain.EtapaEntregado

	evento := func(
		tipo domain.TipoEvento,
		latitud, longitud *float64,
		etapa *domain.Etapa,
	) *domain.Evento {
		return domain.HydrateEvento(domain.HydrateEventoParams{
			ID:              uuid.New(),
			GarantiaID:      garantiaID,
			Tipo:            tipo,
			Usuario:         "Agente",
			GPSLat:          latitud,
			GPSLon:          longitud,
			EtapaHasta:      etapa,
			CreatedAt:       ahora,
			DeviceCreatedAt: ahora,
		})
	}

	casos := []struct {
		nombre        string
		eventos       []*domain.Evento
		esperaEntrega bool
		recolecciones int
	}{
		{
			nombre: "sin eventos",
		},
		{
			nombre: "entrega sin latitud",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoFolioEntregado, nil, &lon, nil),
			},
		},
		{
			nombre: "entrega sin longitud",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoFolioEntregado, &lat, nil, nil),
			},
		},
		{
			nombre: "recoleccion sin etapa",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoEtapaAvanzada, &lat, &lon, nil),
			},
		},
		{
			nombre: "recoleccion en otra etapa",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoEtapaAvanzada, &lat, &lon, &otraEtapa),
			},
		},
		{
			nombre: "recoleccion sin latitud",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoEtapaAvanzada, nil, &lon, &recolectado),
			},
		},
		{
			nombre: "recoleccion sin longitud",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoEtapaAvanzada, &lat, nil, &recolectado),
			},
		},
		{
			nombre: "entrega valida",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoFolioEntregado, &lat, &lon, nil),
			},
			esperaEntrega: true,
		},
		{
			nombre: "recoleccion valida",
			eventos: []*domain.Evento{
				evento(domain.TipoEventoEtapaAvanzada, &lat, &lon, &recolectado),
			},
			recolecciones: 1,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			t.Parallel()

			entrega, recolecciones := puntosDeAuditoria(caso.eventos)

			if (entrega != nil) != caso.esperaEntrega {
				t.Fatalf(
					"entrega presente = %t, esperado = %t",
					entrega != nil,
					caso.esperaEntrega,
				)
			}

			if len(recolecciones) != caso.recolecciones {
				t.Fatalf(
					"recolecciones = %d, esperado = %d",
					len(recolecciones),
					caso.recolecciones,
				)
			}

			if entrega != nil && (entrega.Lat != lat || entrega.Lon != lon) {
				t.Errorf("GPS de entrega incorrecto: %+v", *entrega)
			}

			for _, punto := range recolecciones {
				if punto.Lat != lat || punto.Lon != lon {
					t.Errorf("GPS de recoleccion incorrecto: %+v", punto)
				}
			}
		})
	}
}

func TestPuntosDeAuditoria_UltimaEntregaYMultiplesRecolecciones(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	ahora := time.Now().UTC()
	recolectado := domain.EtapaRecolectado

	nuevo := func(
		tipo domain.TipoEvento,
		lat, lon float64,
		etapa *domain.Etapa,
	) *domain.Evento {
		return domain.HydrateEvento(domain.HydrateEventoParams{
			ID:              uuid.New(),
			GarantiaID:      garantiaID,
			Tipo:            tipo,
			Usuario:         "Agente",
			GPSLat:          &lat,
			GPSLon:          &lon,
			EtapaHasta:      etapa,
			CreatedAt:       ahora,
			DeviceCreatedAt: ahora,
		})
	}

	eventos := []*domain.Evento{
		nuevo(domain.TipoEventoFolioEntregado, 19.1, -99.1, nil),
		nuevo(domain.TipoEventoEtapaAvanzada, 19.2, -99.2, &recolectado),
		nuevo(domain.TipoEventoEtapaAvanzada, 19.3, -99.3, &recolectado),
		nuevo(domain.TipoEventoFolioEntregado, 19.4, -99.4, nil),
	}

	entrega, recolecciones := puntosDeAuditoria(eventos)

	if entrega == nil || entrega.Lat != 19.4 || entrega.Lon != -99.4 {
		t.Fatalf("debe utilizar la ultima entrega, got %+v", entrega)
	}

	if len(recolecciones) != 2 {
		t.Fatalf("recolecciones = %d, want 2", len(recolecciones))
	}

	if recolecciones[0].Lat != 19.2 || recolecciones[1].Lat != 19.3 {
		t.Fatalf("orden de recolecciones incorrecto: %+v", recolecciones)
	}
}
