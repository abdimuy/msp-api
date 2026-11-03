package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// metrosPorGradoLatitud is the length of one degree of latitude on a meridian,
// the figure the brief uses to turn a distance in metres into a difference in
// degrees. Placing both points on the same meridian keeps the arithmetic
// checkable by hand.
const metrosPorGradoLatitud = 111194.93

// punto builds a coordinate pair from metres north of a fixed origin on the
// same meridian.
func puntoNorte(metros float64) domain.PuntoGPS {
	return domain.PuntoGPS{Lat: 19.4326 + metros/metrosPorGradoLatitud, Lon: -99.1332}
}

// TestAuditarEntrega_NoAplica covers the cases where there is nothing to
// compare: no delivery point, no pickups, or an empty pickup list. The result
// is no_aplica with no distance, and the justification still travels back
// normalized.
func TestAuditarEntrega_NoAplica(t *testing.T) {
	t.Parallel()
	entrega := puntoNorte(0)
	casos := map[string]struct {
		entrega       *domain.PuntoGPS
		recolecciones []domain.PuntoGPS
	}{
		"sin entrega":       {nil, []domain.PuntoGPS{puntoNorte(10)}},
		"sin recolecciones": {&entrega, nil},
		"lista vacia":       {&entrega, []domain.PuntoGPS{}},
		"ambos vacios":      {nil, nil},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			aud, err := domain.AuditarEntrega(c.entrega, c.recolecciones, "  sin datos  ")
			if err != nil {
				t.Fatalf("AuditarEntrega: %v", err)
			}
			if aud.Resultado != domain.AuditoriaGPSNoAplica {
				t.Errorf("resultado = %q, want no_aplica", aud.Resultado)
			}
			if aud.DistanciaM != nil {
				t.Errorf("distancia = %v, want nil", *aud.DistanciaM)
			}
			if aud.Justificacion != "sin datos" {
				t.Errorf("justificacion = %q, want normalizada", aud.Justificacion)
			}
		})
	}
}

// TestAuditarEntrega_UnMinutoDeArco pins the haversine calculation against a
// figure that can be derived by hand: one arc minute of latitude on a meridian
// measures 6,371,000 × π / 10,800 ≈ 1,853.25 m. The justification is supplied
// because that distance is well over the threshold.
func TestAuditarEntrega_UnMinutoDeArco(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	unMinuto := domain.PuntoGPS{Lat: origen.Lat + 1.0/60.0, Lon: origen.Lon}
	const esperado = 6371000.0 * math.Pi / 10800.0

	aud, err := domain.AuditarEntrega(&origen, []domain.PuntoGPS{unMinuto}, "revision manual")
	if err != nil {
		t.Fatalf("AuditarEntrega: %v", err)
	}
	if aud.DistanciaM == nil {
		t.Fatal("distancia = nil, want un minuto de arco")
	}
	if math.Abs(*aud.DistanciaM-esperado) > 0.5 {
		t.Errorf("distancia = %.2f, want ~%.2f", *aud.DistanciaM, esperado)
	}
	if aud.Resultado != domain.AuditoriaGPSConDesvio {
		t.Errorf("resultado = %q, want con_desvio", aud.Resultado)
	}
}

// TestAuditarEntrega_Umbral covers both sides of the threshold with the points
// the brief prescribes: 299.9 m stays sin_desvio and 300.1 m is con_desvio. The
// equality case lives in the internal test of superaUmbralDesvio, where it can
// be pinned with an exact number instead of a coordinate pair.
func TestAuditarEntrega_Umbral(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	casos := map[string]struct {
		metros float64
		quiere domain.AuditoriaGPS
		justif string
	}{
		"bajo el umbral":  {metros: 299.9, quiere: domain.AuditoriaGPSSinDesvio},
		"sobre el umbral": {metros: 300.1, quiere: domain.AuditoriaGPSConDesvio, justif: "el cliente no estaba"},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			recoleccion := puntoNorte(c.metros)
			aud, err := domain.AuditarEntrega(&origen, []domain.PuntoGPS{recoleccion}, c.justif)
			if err != nil {
				t.Fatalf("AuditarEntrega: %v", err)
			}
			if aud.Resultado != c.quiere {
				t.Errorf("resultado = %q, want %q", aud.Resultado, c.quiere)
			}
			if aud.DistanciaM == nil {
				t.Fatal("distancia = nil")
			}
			if math.Abs(*aud.DistanciaM-c.metros) > 0.05 {
				t.Errorf("distancia = %.4f, want ~%.1f", *aud.DistanciaM, c.metros)
			}
		})
	}
}

// TestAuditarEntrega_GanaLaMayor: with several pickups the audit keeps the
// greatest distance, not the first, the last or the smallest.
func TestAuditarEntrega_GanaLaMayor(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	recolecciones := []domain.PuntoGPS{
		puntoNorte(100),
		puntoNorte(500),
		puntoNorte(250),
	}

	aud, err := domain.AuditarEntrega(&origen, recolecciones, "varias recolecciones")
	if err != nil {
		t.Fatalf("AuditarEntrega: %v", err)
	}
	if aud.Resultado != domain.AuditoriaGPSConDesvio {
		t.Errorf("resultado = %q, want con_desvio", aud.Resultado)
	}
	if aud.DistanciaM == nil || math.Abs(*aud.DistanciaM-500) > 0.1 {
		t.Errorf("distancia = %v, want la mayor (~500 m)", aud.DistanciaM)
	}
}

// TestAuditarEntrega_DesvioSinJustificacion: a deviation with no explanation is
// refused and returns no partial audit for the caller to store.
func TestAuditarEntrega_DesvioSinJustificacion(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	aud, err := domain.AuditarEntrega(&origen, []domain.PuntoGPS{puntoNorte(500)}, "   ")
	if !errors.Is(err, domain.ErrJustificacionDesvioRequerida) {
		t.Fatalf("want ErrJustificacionDesvioRequerida, got %v", err)
	}
	if aud.DistanciaM != nil || aud.Resultado != "" || aud.Justificacion != "" {
		t.Errorf("la auditoria rechazada debe volver vacia: %+v", aud)
	}
}

// TestAuditarEntrega_JustificacionNormalizada: the justification travels in
// every outcome, trimmed and in NFC, so the event carries what the operator
// meant to write and not a byte-level variant.
func TestAuditarEntrega_JustificacionNormalizada(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	aud, err := domain.AuditarEntrega(&origen, []domain.PuntoGPS{puntoNorte(500)}, "  desvio\u0301  ")
	if err != nil {
		t.Fatalf("AuditarEntrega: %v", err)
	}
	if aud.Justificacion != "desvió" {
		t.Errorf("justificacion = %q, want el texto NFC y sin espacios", aud.Justificacion)
	}
}

// TestAuditarEntrega_MismaCoordenada: two points at the exact same position
// have distance zero and are sin_desvio, the other end of the range.
func TestAuditarEntrega_MismaCoordenada(t *testing.T) {
	t.Parallel()
	origen := puntoNorte(0)
	aud, err := domain.AuditarEntrega(&origen, []domain.PuntoGPS{origen}, "")
	if err != nil {
		t.Fatalf("AuditarEntrega: %v", err)
	}
	if aud.Resultado != domain.AuditoriaGPSSinDesvio {
		t.Errorf("resultado = %q, want sin_desvio", aud.Resultado)
	}
	if aud.DistanciaM == nil || *aud.DistanciaM != 0 {
		t.Errorf("distancia = %v, want 0", aud.DistanciaM)
	}
}
