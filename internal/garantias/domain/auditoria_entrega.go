//nolint:misspell // vocabulario español (auditoría, justificación) por convención del proyecto.
package domain

import (
	"math"
	"strings"
)

// PuntoGPS is one coordinate pair used by the delivery audit of spec §4.1.
type PuntoGPS struct {
	Lat float64
	Lon float64
}

// UmbralDesvioMetros is how far the delivery point may be from the farthest
// pickup point before Cerrar demands a justification. It is adjustable: change
// the constant and run the tests.
const UmbralDesvioMetros = 300.0

// radioTierraMetros is the mean Earth radius used by the haversine formula.
const radioTierraMetros = 6371000.0

// AuditoriaEntrega is the result of comparing the delivery point with the
// pickups. DistanciaM is nil for no_aplica; Justificacion is the normalized
// justification the caller sent, kept in every outcome.
type AuditoriaEntrega struct {
	DistanciaM    *float64
	Resultado     AuditoriaGPS
	Justificacion string
}

// AuditarEntrega computes the delivery audit. With no delivery point or no
// pickup point carrying GPS there is nothing to compare and the outcome is
// no_aplica with a nil distance. Otherwise the result is the greatest haversine
// distance to any pickup: above UmbralDesvioMetros it is con_desvio and a
// non-empty justification is mandatory; at or below it is sin_desvio.
//
// The justification is normalized NFC and trimmed in every outcome, and a
// missing one is only an error when the audit found a deviation. The rule and
// the calculation live here so app only gathers the points.
func AuditarEntrega(entrega *PuntoGPS, recolecciones []PuntoGPS, justificacion string) (AuditoriaEntrega, error) {
	justificacion = normalizarNFC(strings.TrimSpace(justificacion))
	if entrega == nil || len(recolecciones) == 0 {
		return AuditoriaEntrega{
			Resultado:     AuditoriaGPSNoAplica,
			Justificacion: justificacion,
		}, nil
	}

	var mayor float64
	for _, recoleccion := range recolecciones {
		d := distanciaHaversineMetros(*entrega, recoleccion)
		if d > mayor {
			mayor = d
		}
	}

	if superaUmbralDesvio(mayor) {
		if justificacion == "" {
			return AuditoriaEntrega{}, ErrJustificacionDesvioRequerida
		}
		return AuditoriaEntrega{
			DistanciaM:    &mayor,
			Resultado:     AuditoriaGPSConDesvio,
			Justificacion: justificacion,
		}, nil
	}

	return AuditoriaEntrega{
		DistanciaM:    &mayor,
		Resultado:     AuditoriaGPSSinDesvio,
		Justificacion: justificacion,
	}, nil
}

// superaUmbralDesvio reports whether the greatest distance counts as a
// deviation. The comparison is strict: exactly UmbralDesvioMetros is still
// sin_desvio. It is a separate predicate so the equality case can be pinned
// with an exact number instead of a coordinate pair that floating point may or
// may not land on.
func superaUmbralDesvio(metros float64) bool {
	return metros > UmbralDesvioMetros
}

// distanciaHaversineMetros returns the great-circle distance between two
// points. The minimum against 1 protects the domain of Asin against a floating
// point overshoot on antipodal points.
func distanciaHaversineMetros(a, b PuntoGPS) float64 {
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Lon - a.Lon) * math.Pi / 180

	senLat := math.Sin(dLat / 2)
	senLon := math.Sin(dLon / 2)
	h := senLat*senLat + math.Cos(lat1)*math.Cos(lat2)*senLon*senLon

	return 2 * radioTierraMetros * math.Asin(math.Min(1, math.Sqrt(h)))
}
