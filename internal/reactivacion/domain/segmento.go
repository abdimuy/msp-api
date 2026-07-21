//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Segmento is the value object identifying which slice of the tratable universe
// a cliente belongs to in the reactivación piloto. It is a closed set of two
// canonical values; any other string is invalid.
//
// Segments are computed over MSP_SALDOS_VENTAS (CARGO_CANCELADO='N'):
//   - recien_liquidado:    SALDO = 0 (the client just finished paying).
//   - por_liquidar_hueco:  SALDO > 0 AND SALDO < 20% of PRECIO_TOTAL (almost done).
//
// A client that qualifies for both slices is assigned por_liquidar_hueco by the
// app layer (it carries the actionable "hueco" signal); see the ConstruirCohorte
// command.
type Segmento string

const (
	// SegmentoRecienLiquidado marks a client whose credit balance is fully paid
	// (SALDO = 0) — the highest-intent moment to offer a new sale.
	SegmentoRecienLiquidado Segmento = "recien_liquidado"

	// SegmentoPorLiquidarHueco marks a client whose remaining balance is under
	// 20% of the original PRECIO_TOTAL — close enough to liquidation to reactivate.
	SegmentoPorLiquidarHueco Segmento = "por_liquidar_hueco"
)

// String returns the underlying string representation.
func (s Segmento) String() string { return string(s) }

// Valido reports whether s is one of the canonical segmento values.
func (s Segmento) Valido() bool {
	switch s {
	case SegmentoRecienLiquidado, SegmentoPorLiquidarHueco:
		return true
	default:
		return false
	}
}

// ParseSegmento converts a raw string to a Segmento, returning ErrSegmentoInvalido
// when the value is not one of the canonical constants.
func ParseSegmento(raw string) (Segmento, error) {
	s := Segmento(raw)
	if !s.Valido() {
		return "", ErrSegmentoInvalido
	}
	return s, nil
}
