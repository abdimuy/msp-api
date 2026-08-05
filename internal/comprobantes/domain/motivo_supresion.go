package domain

// MotivoSupresionRebote identifies a delivery suppressed because the channel
// reported a bounce.
const MotivoSupresionRebote = "rebote"

// MotivoSupresion is a value object wrapping why a delivery was suppressed.
// Modeled as a type instead of a loose constant because more reasons are
// expected to appear. Only "rebote" is valid for now.
type MotivoSupresion struct{ value string }

// NewMotivoSupresion validates and constructs a MotivoSupresion. Rejects
// anything else with ErrMotivoSupresionInvalido.
func NewMotivoSupresion(s string) (MotivoSupresion, error) {
	if s != MotivoSupresionRebote {
		return MotivoSupresion{}, ErrMotivoSupresionInvalido
	}
	return MotivoSupresion{value: s}, nil
}

// HydrateMotivoSupresion rebuilds one from persistence without validation.
// Intended for repository use only.
func HydrateMotivoSupresion(s string) MotivoSupresion { return MotivoSupresion{value: s} }

// Value returns the raw suppression reason string.
func (m MotivoSupresion) Value() string { return m.value }

// String returns the suppression reason string representation.
func (m MotivoSupresion) String() string { return m.value }

// Equals reports whether two MotivoSupresion values are identical.
func (m MotivoSupresion) Equals(other MotivoSupresion) bool { return m.value == other.value }

// IsZero reports whether the MotivoSupresion has its zero value (empty string).
func (m MotivoSupresion) IsZero() bool { return m.value == "" }

// EsRebote reports whether the delivery was suppressed because of a channel
// bounce.
func (m MotivoSupresion) EsRebote() bool { return m.value == MotivoSupresionRebote }
