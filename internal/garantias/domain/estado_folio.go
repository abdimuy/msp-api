package domain

// Folio lifecycle states. abierto → en_proceso → listo_entrega → entregado
// → cerrado is the happy path; cancelado is the terminal alternate exit
// reachable from any non-terminal state.
const (
	EstadoFolioAbierto      = "abierto"
	EstadoFolioEnProceso    = "en_proceso"
	EstadoFolioListoEntrega = "listo_entrega"
	EstadoFolioEntregado    = "entregado"
	EstadoFolioCerrado      = "cerrado"
	EstadoFolioCancelado    = "cancelado"
)

// EstadoFolio is a value object wrapping the lifecycle state of a warranty
// folio. Only the six states above are valid.
type EstadoFolio struct{ value string }

// NewEstadoFolio validates and constructs an EstadoFolio. Rejects anything
// outside the six recognized states with ErrEstadoFolioInvalido.
func NewEstadoFolio(s string) (EstadoFolio, error) {
	switch s {
	case EstadoFolioAbierto, EstadoFolioEnProceso, EstadoFolioListoEntrega,
		EstadoFolioEntregado, EstadoFolioCerrado, EstadoFolioCancelado:
		return EstadoFolio{value: s}, nil
	default:
		return EstadoFolio{}, ErrEstadoFolioInvalido
	}
}

// HydrateEstadoFolio rebuilds an EstadoFolio from persistence without
// validation. Intended for repository use only.
func HydrateEstadoFolio(s string) EstadoFolio { return EstadoFolio{value: s} }

// Value returns the raw folio state string.
func (e EstadoFolio) Value() string { return e.value }

// String returns the folio state string representation.
func (e EstadoFolio) String() string { return e.value }

// Equals reports whether two EstadoFolio values are identical.
func (e EstadoFolio) Equals(other EstadoFolio) bool { return e.value == other.value }

// IsZero reports whether the EstadoFolio has its zero value (empty string).
func (e EstadoFolio) IsZero() bool { return e.value == "" }

// EsTerminal reports whether the folio is in a state with no further
// transitions: cerrado or cancelado.
func (e EstadoFolio) EsTerminal() bool {
	return e.value == EstadoFolioCerrado || e.value == EstadoFolioCancelado
}

// EsCancelado reports whether the folio was cancelled.
func (e EstadoFolio) EsCancelado() bool { return e.value == EstadoFolioCancelado }
