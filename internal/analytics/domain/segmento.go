package domain

// Segmento classifies a winback candidate into one of six RFM-derived segments.
// Values match the canonical UPPERCASE form used in the analytics system.
type Segmento string

const (
	// SegmentoLealPorLiquidar denotes a loyal customer with outstanding balance.
	SegmentoLealPorLiquidar Segmento = "LEAL_POR_LIQUIDAR"
	// SegmentoDormidoValioso denotes a high-value customer who has gone dormant.
	SegmentoDormidoValioso Segmento = "DORMIDO_VALIOSO"
	// SegmentoActivo denotes a currently active customer.
	SegmentoActivo Segmento = "ACTIVO"
	// SegmentoNuevo denotes a recently acquired customer.
	SegmentoNuevo Segmento = "NUEVO"
	// SegmentoFrio denotes a customer who has become cold (low recency/frequency).
	SegmentoFrio Segmento = "FRIO"
	// SegmentoPerdido denotes a lost customer with no recent activity.
	SegmentoPerdido Segmento = "PERDIDO"
)

// ParseSegmento parses a string into a Segmento or returns ErrSegmentoInvalido.
// The input must match exactly the UPPERCASE canonical form (no normalization is
// performed inside the VO — normalize at the boundary if needed).
func ParseSegmento(s string) (Segmento, error) {
	seg := Segmento(s)
	if !seg.IsValid() {
		return "", ErrSegmentoInvalido
	}
	return seg, nil
}

// IsValid reports whether s is a recognized Segmento value.
func (s Segmento) IsValid() bool {
	switch s {
	case SegmentoLealPorLiquidar,
		SegmentoDormidoValioso,
		SegmentoActivo,
		SegmentoNuevo,
		SegmentoFrio,
		SegmentoPerdido:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (s Segmento) String() string { return string(s) }
