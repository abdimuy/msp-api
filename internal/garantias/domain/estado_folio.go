package domain

// EstadoFolio represents the lifecycle state of a warranty folio.
type EstadoFolio string

// EstadoFolio values represent the lifecycle state of a warranty folio.
const (
	EstadoFolioAbierto      EstadoFolio = "abierto"
	EstadoFolioEnProceso    EstadoFolio = "en_proceso"
	EstadoFolioListoEntrega EstadoFolio = "listo_entrega"
	EstadoFolioEntregado    EstadoFolio = "entregado"
	EstadoFolioCerrado      EstadoFolio = "cerrado"
	EstadoFolioCancelado    EstadoFolio = "cancelado"
)

// validEstadoFolioTransitions defines the allowed state transitions.
var validEstadoFolioTransitions = map[EstadoFolio][]EstadoFolio{
	EstadoFolioAbierto:      {EstadoFolioEnProceso, EstadoFolioCancelado},
	EstadoFolioEnProceso:    {EstadoFolioListoEntrega, EstadoFolioCancelado},
	EstadoFolioListoEntrega: {EstadoFolioEntregado},
	EstadoFolioEntregado:    {EstadoFolioCerrado},
	EstadoFolioCerrado:      {}, // terminal, sin salida
	EstadoFolioCancelado:    {}, // terminal, sin salida
}

// ParseEstadoFolio validates and returns an EstadoFolio.
// Returns ErrEstadoFolioInvalido if s is not one of the six recognized states.
func ParseEstadoFolio(s string) (EstadoFolio, error) {
	e := EstadoFolio(s)
	if !e.IsValid() {
		return "", ErrEstadoFolioInvalido
	}
	return e, nil
}

// IsValid reports whether e is a known EstadoFolio value.
func (e EstadoFolio) IsValid() bool {
	switch e {
	case EstadoFolioAbierto, EstadoFolioEnProceso, EstadoFolioListoEntrega,
		EstadoFolioEntregado, EstadoFolioCerrado, EstadoFolioCancelado:
		return true
	}
	return false
}

// String returns the string representation of e.
func (e EstadoFolio) String() string { return string(e) }

// CanTransitionTo reports whether e can transition to t according to the
// state machine defined in validEstadoFolioTransitions.
func (e EstadoFolio) CanTransitionTo(t EstadoFolio) bool {
	allowed := validEstadoFolioTransitions[e]
	for _, st := range allowed {
		if st == t {
			return true
		}
	}
	return false
}

// IsTerminal reports whether e is a terminal state (cerrado or cancelado).
func (e EstadoFolio) IsTerminal() bool {
	return e == EstadoFolioCerrado || e == EstadoFolioCancelado
}

// EsCancelado reports whether e is cancelado.
func (e EstadoFolio) EsCancelado() bool { return e == EstadoFolioCancelado }
