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

// operacionArticulo names every article-touching operation the aggregate
// exposes. R4 is a single estado × operación table over them; keeping the
// operations typed (instead of a bare bool at each call site) makes the rule
// impossible to spread accidentally across the mutators.
type operacionArticulo int

const (
	opAgregarArticulo operacionArticulo = iota
	opAvanzar
	opRegistrarDiagnostico
	opRegistrarDictamen
	opAutorizarCambioFisico
	opRegistrarDesenlace
	opMoverArticulo
	opMarcarNoDevuelto
)

// folioAdmiteOperacion is the single R4 rule: which operations a folio may run
// on its articles for a given state. The standby exception is the §3.2
// requirement — the folio closes with the original still in standby and its
// final destination is recorded afterwards — so RegistrarDesenlace and
// MoverArticulo survive a closed folio, but only on a standby article.
func folioAdmiteOperacion(estado EstadoFolio, op operacionArticulo, etapa Etapa) bool {
	switch estado {
	case EstadoFolioAbierto:
		// Nothing moves until the process starts; the folio only takes articles.
		return op == opAgregarArticulo
	case EstadoFolioEnProceso:
		return true
	case EstadoFolioListoEntrega:
		// Delivery is imminent: existing articles may still move (a swap, a
		// result), but the folio no longer takes new ones, or MarcarListoEntrega
		// could be undone after the fact.
		return op != opAgregarArticulo
	case EstadoFolioEntregado, EstadoFolioCerrado, EstadoFolioCancelado:
		return (op == opRegistrarDesenlace || op == opMoverArticulo) && etapa == EtapaStandby
	}
	return false
}
