// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// eventoPunto builds a stored event carrying a GPS position, the only thing the
// delivery audit reads from the timeline. etapa names the arrival stage for an
// etapa_avanzada and is nil for the folio events.
func eventoPunto(
	garantiaID uuid.UUID,
	tipo domain.TipoEvento,
	lat, lon float64,
	etapa *domain.Etapa,
	now time.Time,
) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:              uuid.New(),
		GarantiaID:      garantiaID,
		Tipo:            tipo,
		Usuario:         "Juan",
		GPSLat:          &lat,
		GPSLon:          &lon,
		EtapaHasta:      etapa,
		CreatedAt:       now,
		DeviceCreatedAt: now,
	})
}

// eventoRecoleccion is the pickup event of an article that reached recolectado.
func eventoRecoleccion(garantiaID uuid.UUID, lat, lon float64, now time.Time) *domain.Evento {
	etapa := domain.EtapaRecolectado
	return eventoPunto(garantiaID, domain.TipoEventoEtapaAvanzada, lat, lon, &etapa, now)
}

// TestCerrar_CaminoFeliz seals a delivered folio and stamps when it happened.
func TestCerrar_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)

	res, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("Cerrar: %v", err)
	}

	if res.Estado() != domain.EstadoFolioCerrado {
		t.Errorf("estado = %s, want cerrado", res.Estado())
	}
	if res.CerradoEn() == nil {
		t.Error("cerradoEn debe quedar marcado")
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoFolioCerrado {
		t.Errorf("evento = %s, want folio_cerrado", ev.Tipo())
	}
}

// TestCerrar_EstadoNoCorresponde only seals what was delivered.
func TestCerrar_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestCerrar_DesvioSinJustificacion: when the delivery is farther than the
// threshold from the farthest pickup, sealing without an explanation is refused
// before anything is written.
func TestCerrar_DesvioSinJustificacion(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)
	erepo.addEvento(eventoPunto(s.g.ID(), domain.TipoEventoFolioEntregado, 19.4326, -99.1332, nil, clk.Now()))
	erepo.addEvento(eventoRecoleccion(s.g.ID(), 19.4426, -99.1332, clk.Now()))

	_, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrJustificacionDesvioRequerida) {
		t.Fatalf("want ErrJustificacionDesvioRequerida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestCerrar_ConDesvioGuardaLaAuditoria: with a justification the folio seals
// and the folio_cerrado event carries the distance and the outcome, so the
// audit is part of the paper trail.
func TestCerrar_ConDesvioGuardaLaAuditoria(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)
	erepo.addEvento(eventoPunto(s.g.ID(), domain.TipoEventoFolioEntregado, 19.4326, -99.1332, nil, clk.Now()))
	erepo.addEvento(eventoRecoleccion(s.g.ID(), 19.4426, -99.1332, clk.Now()))

	res, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:          s.g.ID(),
		JustificacionDesvio: "  el cliente cambio de domicilio  ",
		ClaveIdempotencia:   clave(),
		DeviceCreatedAt:     clk.Now(),
	})
	if err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	if res.Estado() != domain.EstadoFolioCerrado {
		t.Errorf("estado = %s, want cerrado", res.Estado())
	}
	ev := eventoEscrito(t, grepo)
	if ev.AuditoriaGPS() == nil || *ev.AuditoriaGPS() != domain.AuditoriaGPSConDesvio {
		t.Errorf("auditoria = %v, want con_desvio", ev.AuditoriaGPS())
	}
	if ev.DistanciaEntregaM() == nil || *ev.DistanciaEntregaM() <= domain.UmbralDesvioMetros {
		t.Errorf("distancia = %v, want mayor al umbral", ev.DistanciaEntregaM())
	}
	if ev.Description() != "el cliente cambio de domicilio" {
		t.Errorf("description = %q, want la justificacion normalizada", ev.Description())
	}
}

// TestCerrar_SinRecoleccionesNoAplica: with no pickup point there is nothing to
// compare, so the audit is no_aplica and carries no distance.
func TestCerrar_SinRecoleccionesNoAplica(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)
	erepo.addEvento(eventoPunto(s.g.ID(), domain.TipoEventoFolioEntregado, 19.4326, -99.1332, nil, clk.Now()))

	if _, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	}); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	ev := eventoEscrito(t, grepo)
	if ev.AuditoriaGPS() == nil || *ev.AuditoriaGPS() != domain.AuditoriaGPSNoAplica {
		t.Errorf("auditoria = %v, want no_aplica", ev.AuditoriaGPS())
	}
	if ev.DistanciaEntregaM() != nil {
		t.Errorf("distancia = %v, want nil", ev.DistanciaEntregaM())
	}
}

// TestCerrar_SinDesvio: a delivery at the pickup point is within the threshold
// and seals with sin_desvio, distance zero.
func TestCerrar_SinDesvio(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)
	erepo.addEvento(eventoPunto(s.g.ID(), domain.TipoEventoFolioEntregado, 19.4326, -99.1332, nil, clk.Now()))
	erepo.addEvento(eventoRecoleccion(s.g.ID(), 19.4326, -99.1332, clk.Now()))

	if _, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	}); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	ev := eventoEscrito(t, grepo)
	if ev.AuditoriaGPS() == nil || *ev.AuditoriaGPS() != domain.AuditoriaGPSSinDesvio {
		t.Errorf("auditoria = %v, want sin_desvio", ev.AuditoriaGPS())
	}
	if ev.DistanciaEntregaM() == nil || *ev.DistanciaEntregaM() != 0 {
		t.Errorf("distancia = %v, want 0", ev.DistanciaEntregaM())
	}
}

// TestCerrar_ErrorAlLeerLaLinea: a failure reading the timeline is propagated
// and nothing is written; it must not be mistaken for "no audit data".
func TestCerrar_ErrorAlLeerLaLinea(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	erepo.listErr = errors.New("linea de tiempo caida")
	s := sembrar(t, grepo, domain.EstadoFolioEntregado, cliente(), domain.EtapaEntregado)

	_, err := svc.Cerrar(context.Background(), app.CerrarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "linea de tiempo caida" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
