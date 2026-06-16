//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// TierRiesgo classifies a client's collection risk based on payment cadence
// relative to their personal payment rhythm. Computed at read time in the app
// layer via computeCobranzaTier; never stored directly.
//
// Classification rules (R1 heuristics — tunable in scoring.go):
//   - AL_DIA:    saldo == 0, OR saldo > 0 AND days_since_payment <= 1×cadencia
//   - VIGILANCIA: saldo > 0 AND 1×cadencia < days_since_payment <= 2×cadencia
//   - EN_RIESGO:  saldo > 0 AND 2×cadencia < days_since_payment <= 3×cadencia
//   - CRITICO:    saldo > 0 AND days_since_payment > 3×cadencia,
//     OR no cadencia data AND EstadoPago == MOROSO
//     SIN_CREDITO / LIQUIDADO (saldo == 0) always map to AL_DIA.
//
// When CADENCIA_DIAS is zero (< 2 payments), falls back to EstadoPago-based
// classification using the fixed thresholds from estadoPagoFor.
type TierRiesgo string

const (
	// TierRiesgoAlDia is the tier for clients current — saldo 0 or paid within 1 cadence.
	TierRiesgoAlDia TierRiesgo = "AL_DIA"

	// TierRiesgoVigilancia is the tier for 1–2 cadences since last payment. Monitor closely.
	TierRiesgoVigilancia TierRiesgo = "VIGILANCIA"

	// TierRiesgoEnRiesgo is the tier for 2–3 cadences since last payment. Proactive contact needed.
	TierRiesgoEnRiesgo TierRiesgo = "EN_RIESGO"

	// TierRiesgoCritico is the tier for >3 cadences since last payment, or no cadence + MOROSO.
	TierRiesgoCritico TierRiesgo = "CRITICO"
)

// ParseTierRiesgo parses a string into a TierRiesgo or returns ErrTierRiesgoInvalido.
// Input must match the exact UPPERCASE canonical form.
func ParseTierRiesgo(s string) (TierRiesgo, error) {
	tr := TierRiesgo(s)
	if !tr.IsValid() {
		return "", ErrTierRiesgoInvalido
	}
	return tr, nil
}

// IsValid reports whether tr is a recognized TierRiesgo value.
func (tr TierRiesgo) IsValid() bool {
	switch tr {
	case TierRiesgoAlDia, TierRiesgoVigilancia, TierRiesgoEnRiesgo, TierRiesgoCritico:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (tr TierRiesgo) String() string { return string(tr) }
