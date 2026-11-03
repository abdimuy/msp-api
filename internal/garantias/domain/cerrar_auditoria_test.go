package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// folioEntregado rebuilds a delivered cliente folio, the only state Cerrar
// accepts.
func folioEntregado(t *testing.T) *domain.Garantia {
	t.Helper()
	g, _ := hidratarFolioCon(t, domain.OrigenFolioCliente, domain.EstadoFolioEntregado,
		articuloHidratado{etapa: domain.EtapaEntregado, ubicacion: domain.UbicacionEntregado})
	return g
}

// puntoGPS turns metres north of a fixed origin into a coordinate pair on the
// same meridian, so the distance is easy to reason about.
func puntoGPS(metros float64) domain.PuntoGPS {
	return domain.PuntoGPS{Lat: 19.4326 + metros/111194.93, Lon: -99.1332}
}

// TestCerrar_DesvioSinJustificacion: a delivery farther than the threshold from
// the farthest pickup demands an explanation. Without one the folio stays
// delivered and no event is appended.
func TestCerrar_DesvioSinJustificacion(t *testing.T) {
	t.Parallel()
	g := folioEntregado(t)
	pendientes := countPending(g)
	entrega := puntoGPS(0)

	err := g.Cerrar(&entrega, []domain.PuntoGPS{puntoGPS(500)}, "   ", actor("juan"), fixed)
	if !errors.Is(err, domain.ErrJustificacionDesvioRequerida) {
		t.Fatalf("want ErrJustificacionDesvioRequerida, got %v", err)
	}
	if g.Estado() != domain.EstadoFolioEntregado || g.CerradoEn() != nil || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected: estado=%q cerradoEn=%v", g.Estado(), g.CerradoEn())
	}
}

// TestCerrar_ConDesvioGuardaLaAuditoria: with a justification the folio seals
// and the folio_cerrado event carries the outcome, the greatest distance and
// the normalized justification.
func TestCerrar_ConDesvioGuardaLaAuditoria(t *testing.T) {
	t.Parallel()
	g := folioEntregado(t)
	entrega := puntoGPS(0)

	if err := g.Cerrar(&entrega, []domain.PuntoGPS{puntoGPS(100), puntoGPS(500)}, "  el cliente cambio de domicilio  ", actor("juan"), fixed); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	if g.Estado() != domain.EstadoFolioCerrado || g.CerradoEn() == nil {
		t.Fatalf("estado = %q, cerradoEn = %v", g.Estado(), g.CerradoEn())
	}
	e := lastEvent(t, g)
	if e.Tipo() != domain.TipoEventoFolioCerrado {
		t.Fatalf("evento = %q, want folio_cerrado", e.Tipo())
	}
	if e.AuditoriaGPS() == nil || *e.AuditoriaGPS() != domain.AuditoriaGPSConDesvio {
		t.Errorf("auditoria = %v, want con_desvio", e.AuditoriaGPS())
	}
	if e.DistanciaEntregaM() == nil || math.Abs(*e.DistanciaEntregaM()-500) > 0.1 {
		t.Errorf("distancia = %v, want la mayor (~500 m)", e.DistanciaEntregaM())
	}
	if e.Description() != "el cliente cambio de domicilio" {
		t.Errorf("description = %q, want la justificacion normalizada", e.Description())
	}
}

// TestCerrar_SinPuntosNoAplica: with no pickup point there is nothing to
// compare, so the event records no_aplica and no distance.
func TestCerrar_SinPuntosNoAplica(t *testing.T) {
	t.Parallel()
	g := folioEntregado(t)
	entrega := puntoGPS(0)

	if err := g.Cerrar(&entrega, nil, "", actor("juan"), fixed); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	e := lastEvent(t, g)
	if e.AuditoriaGPS() == nil || *e.AuditoriaGPS() != domain.AuditoriaGPSNoAplica {
		t.Errorf("auditoria = %v, want no_aplica", e.AuditoriaGPS())
	}
	if e.DistanciaEntregaM() != nil {
		t.Errorf("distancia = %v, want nil", e.DistanciaEntregaM())
	}
}

// TestCerrar_DentroDelUmbral: a delivery at the pickup point seals as
// sin_desvio, distance zero.
func TestCerrar_DentroDelUmbral(t *testing.T) {
	t.Parallel()
	g := folioEntregado(t)
	entrega := puntoGPS(0)

	if err := g.Cerrar(&entrega, []domain.PuntoGPS{entrega}, "", actor("juan"), fixed); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	e := lastEvent(t, g)
	if e.AuditoriaGPS() == nil || *e.AuditoriaGPS() != domain.AuditoriaGPSSinDesvio {
		t.Errorf("auditoria = %v, want sin_desvio", e.AuditoriaGPS())
	}
	if e.DistanciaEntregaM() == nil || *e.DistanciaEntregaM() != 0 {
		t.Errorf("distancia = %v, want 0", e.DistanciaEntregaM())
	}
}
