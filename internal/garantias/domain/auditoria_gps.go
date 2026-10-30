package domain

// AuditoriaGPS is the outcome of the delivery audit Cerrar runs (spec §4.1):
// the distance between where the item was picked up and where it was handed
// back. It is stored on the folio_cerrado event (MSP_GA_EVENTO.AUDITORIA_GPS,
// migración 000071) together with the distance in meters.
//
//   - sin_desvio: the distance is within the threshold.
//   - con_desvio: the distance exceeds the threshold; Cerrar demanded a
//     justification, which travels in the event description.
//   - no_aplica: there is nothing to compare against (a piso folio has no
//     pickup, or no pickup event carries GPS). The folio still closes, and the
//     event says why there is no distance.
type AuditoriaGPS string

// The three audit outcomes.
const (
	AuditoriaGPSSinDesvio AuditoriaGPS = "sin_desvio"
	AuditoriaGPSConDesvio AuditoriaGPS = "con_desvio"
	AuditoriaGPSNoAplica  AuditoriaGPS = "no_aplica"
)

// ParseAuditoriaGPS validates and returns an AuditoriaGPS.
// Returns ErrAuditoriaGPSInvalida if s is not one of the three outcomes.
func ParseAuditoriaGPS(s string) (AuditoriaGPS, error) {
	a := AuditoriaGPS(s)
	if !a.IsValid() {
		return "", ErrAuditoriaGPSInvalida
	}
	return a, nil
}

// IsValid reports whether a is a known AuditoriaGPS value.
func (a AuditoriaGPS) IsValid() bool {
	switch a {
	case AuditoriaGPSSinDesvio, AuditoriaGPSConDesvio, AuditoriaGPSNoAplica:
		return true
	}
	return false
}

// String returns the string representation of a.
func (a AuditoriaGPS) String() string { return string(a) }
