//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// BandaCLV classifies a client's risk-adjusted customer lifetime value into one
// of three ordered bands derived from the Gamma-Gamma + BG/BB DET CLV formula.
// The bands are named by value tier: ALTO = high CLV (most valuable to retain or
// reactivate), BAJO = low CLV.
//
// Classification thresholds (configured in clv_params.json, not here):
//
//	clv >= alto_min  → ALTO  (high lifetime value client)
//	clv >= medio_min → MEDIO (moderate lifetime value)
//	clv <  medio_min → BAJO  (low projected lifetime value)
//
// Use the string constants for JSON serialisation and display.
type BandaCLV string

const (
	// BandaCLVAlto is the band for high-value clients (CLV >= alto_min pesos).
	BandaCLVAlto BandaCLV = "ALTO"

	// BandaCLVMedio is the band for moderate-value clients (CLV >= medio_min pesos).
	BandaCLVMedio BandaCLV = "MEDIO"

	// BandaCLVBajo is the band for low-value clients (CLV < medio_min pesos).
	BandaCLVBajo BandaCLV = "BAJO"
)

// ParseBandaCLV parses a string into a BandaCLV or returns ErrBandaCLVInvalida.
// Input must match the exact UPPERCASE canonical form.
func ParseBandaCLV(s string) (BandaCLV, error) {
	b := BandaCLV(s)
	if !b.IsValid() {
		return "", ErrBandaCLVInvalida
	}
	return b, nil
}

// IsValid reports whether b is a recognised BandaCLV value.
func (b BandaCLV) IsValid() bool {
	switch b {
	case BandaCLVAlto, BandaCLVMedio, BandaCLVBajo:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (b BandaCLV) String() string { return string(b) }
