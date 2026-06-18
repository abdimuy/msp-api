//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// BandaRecompra classifies a client's repurchase propensity into one of three
// ordered bands derived from the BG/BB + logistic-regression ScoreRecompra. The
// bands are named after propensity level: ALTA = high propensity (likely to
// repurchase), BAJA = low propensity.
//
// Classification thresholds (configured in the recompra scorecard JSON, not here):
//
//	score >= alta_min  → ALTA  (high repurchase propensity)
//	score >= media_min → MEDIA (moderate propensity — worth targeting)
//	score <  media_min → BAJA  (low propensity — unlikely to repurchase soon)
//
// Ordinal() encodes ascending propensity: BAJA=0, MEDIA=1, ALTA=2.
// Use Ordinal() for sorting or range comparisons; use the string constants
// for JSON serialisation and display.
type BandaRecompra string

const (
	// BandaRecompraAlta is the band for high-propensity clients (score >= alta_min).
	// These clients are likely to make another purchase within the next 12 months.
	BandaRecompraAlta BandaRecompra = "ALTA"

	// BandaRecompraMedia is the band for moderate-propensity clients (score >= media_min).
	// Worth targeting with a winback campaign or next-best-offer.
	BandaRecompraMedia BandaRecompra = "MEDIA"

	// BandaRecompraBaja is the band for low-propensity clients (score < media_min).
	// Unlikely to repurchase soon without significant intervention.
	BandaRecompraBaja BandaRecompra = "BAJA"
)

// ParseBandaRecompra parses a string into a BandaRecompra or returns
// ErrBandaRecompraInvalida. Input must match the exact UPPERCASE canonical form.
func ParseBandaRecompra(s string) (BandaRecompra, error) {
	br := BandaRecompra(s)
	if !br.IsValid() {
		return "", ErrBandaRecompraInvalida
	}
	return br, nil
}

// IsValid reports whether br is a recognised BandaRecompra value.
func (br BandaRecompra) IsValid() bool {
	switch br {
	case BandaRecompraAlta, BandaRecompraMedia, BandaRecompraBaja:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (br BandaRecompra) String() string { return string(br) }

// Ordinal returns an integer encoding ascending repurchase propensity:
//
//	BAJA=0, MEDIA=1, ALTA=2
//
// This allows bands to be sorted or compared by propensity level. An unrecognised
// BandaRecompra value returns -1.
func (br BandaRecompra) Ordinal() int {
	switch br {
	case BandaRecompraBaja:
		return 0
	case BandaRecompraMedia:
		return 1
	case BandaRecompraAlta:
		return 2
	default:
		return -1
	}
}
