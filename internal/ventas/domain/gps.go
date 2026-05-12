package domain

// GPS bounds for latitude/longitude validation.
const (
	latMin = -90.0
	latMax = 90.0
	lngMin = -180.0
	lngMax = 180.0
)

// GPSCoords is a (latitud, longitud) pair within WGS-84 bounds.
type GPSCoords struct {
	lat float64
	lng float64
}

// NewGPSCoords validates and constructs a GPSCoords. Inputs that are NaN or
// outside the geographic bounds are rejected.
func NewGPSCoords(lat, lng float64) (GPSCoords, error) {
	if isNaN(lat) || lat < latMin || lat > latMax {
		return GPSCoords{}, ErrGPSLatitudInvalida
	}
	if isNaN(lng) || lng < lngMin || lng > lngMax {
		return GPSCoords{}, ErrGPSLongitudInvalida
	}
	return GPSCoords{lat: lat, lng: lng}, nil
}

// HydrateGPSCoords rebuilds a GPSCoords from persistence without validation.
func HydrateGPSCoords(lat, lng float64) GPSCoords {
	return GPSCoords{lat: lat, lng: lng}
}

// Latitud returns the latitude.
func (g GPSCoords) Latitud() float64 { return g.lat }

// Longitud returns the longitude.
func (g GPSCoords) Longitud() float64 { return g.lng }

// Equals reports whether two GPSCoords values are identical.
func (g GPSCoords) Equals(other GPSCoords) bool {
	return g.lat == other.lat && g.lng == other.lng
}

// isNaN reports whether x is a NaN value (x != x is the canonical IEEE-754
// NaN test and avoids pulling in math just for math.IsNaN).
func isNaN(x float64) bool { return x != x }
