//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// BandaCredito classifies a client's credit risk into one of four ordered
// bands derived from the logistic-regression ScoreCredito. The bands are
// named after risk level: BAJO = low risk (good payer), CRITICO = high risk.
//
// Classification thresholds (configured in the scorecard JSON, not here):
//
//	score >= bajo_min  → BAJO    (excellent / low risk)
//	score >= medio_min → MEDIO   (moderate risk — monitor)
//	score >= alto_min  → ALTO    (elevated risk — proactive contact)
//	score <  alto_min  → CRITICO (critical risk — collection priority)
//
// Ordinal() encodes ascending risk: BAJO=0, MEDIO=1, ALTO=2, CRITICO=3.
// Use Ordinal() for sorting or range comparisons; use the string constants
// for JSON serialisation and display.
type BandaCredito string

const (
	// BandaCreditoBajo is the band for low-risk clients (score >= bajo_min).
	// These clients pay on time and have minimal default probability.
	BandaCreditoBajo BandaCredito = "BAJO"

	// BandaCreditoMedio is the band for moderate-risk clients (score >= medio_min).
	// Some payment irregularities — worth monitoring.
	BandaCreditoMedio BandaCredito = "MEDIO"

	// BandaCreditoAlto is the band for elevated-risk clients (score >= alto_min).
	// Proactive collection contact is recommended.
	BandaCreditoAlto BandaCredito = "ALTO"

	// BandaCreditoCritico is the band for critical-risk clients (score < alto_min).
	// Highest default probability — top collection priority.
	BandaCreditoCritico BandaCredito = "CRITICO"
)

// ParseBandaCredito parses a string into a BandaCredito or returns
// ErrBandaCreditoInvalida. Input must match the exact UPPERCASE canonical form.
func ParseBandaCredito(s string) (BandaCredito, error) {
	bc := BandaCredito(s)
	if !bc.IsValid() {
		return "", ErrBandaCreditoInvalida
	}
	return bc, nil
}

// IsValid reports whether bc is a recognised BandaCredito value.
func (bc BandaCredito) IsValid() bool {
	switch bc {
	case BandaCreditoBajo, BandaCreditoMedio, BandaCreditoAlto, BandaCreditoCritico:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (bc BandaCredito) String() string { return string(bc) }

// Ordinal returns an integer encoding ascending credit risk:
//
//	BAJO=0, MEDIO=1, ALTO=2, CRITICO=3
//
// This allows bands to be sorted or compared by risk level. An unrecognised
// BandaCredito value returns -1.
func (bc BandaCredito) Ordinal() int {
	switch bc {
	case BandaCreditoBajo:
		return 0
	case BandaCreditoMedio:
		return 1
	case BandaCreditoAlto:
		return 2
	case BandaCreditoCritico:
		return 3
	default:
		return -1
	}
}
