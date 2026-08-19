package domain

// validEtapaTransitions defines the allowed stage transitions for an article.
// It encodes the state machine from the design document (§4.2), with the
// following clarified points:
//   - espera_respuesta_cliente has exactly three exits (listo_entrega,
//     cambio_autorizado, standby) and no entries other than from dictamen_recibido.
//   - standby is only reachable from espera_respuesta_cliente and
//     cambio_autorizado (the original article when a physical swap is authorized).
//   - Terminal stages (entregado, reingresado_inventario, segunda_mano,
//     desarmado, merma) have empty transition lists.
//   - registrado has two exits: pendiente_recoleccion for client-origin items,
//     and en_revision for floor-origin items (no collection).
var validEtapaTransitions = map[Etapa][]Etapa{
	// Common trunk
	EtapaRegistrado: {
		EtapaPendienteRecoleccion, // origen cliente: se recoge en domicilio
		EtapaEnRevision,           // origen piso: no se recolecta, entra directo
	},
	EtapaPendienteRecoleccion: {EtapaRecolectado},
	EtapaRecolectado:          {EtapaEnRevision},
	EtapaEnRevision: {
		EtapaOrdenGenerada,
		EtapaEnTaller,
		EtapaReingresadoInventario, // terminal for floor-origin items
	},

	// Supplier route
	EtapaOrdenGenerada:    {EtapaEnviadoProveedor},
	EtapaEnviadoProveedor: {EtapaDictamenRecibido},
	EtapaDictamenRecibido: {
		EtapaReparadoProveedor,
		EtapaListoEntrega,
		EtapaEsperaRespuestaCliente,
	},
	EtapaReparadoProveedor:      {EtapaListoEntrega},
	EtapaEsperaRespuestaCliente: {EtapaListoEntrega, EtapaCambioAutorizado, EtapaStandby},

	// Workshop route
	EtapaEnTaller:       {EtapaReparadoTaller, EtapaCambioAutorizado},
	EtapaReparadoTaller: {EtapaListoEntrega},

	// Convergence and parallel flow
	// CambioAutorizado: the original article leaves the customer path and goes to standby.
	// The replacement article is created directly in listo_entrega (not via transition).
	EtapaCambioAutorizado: {EtapaStandby},
	EtapaListoEntrega:     {EtapaEntregado},

	// Standby → terminal outcomes
	EtapaStandby: {EtapaSegundaMano, EtapaDesarmado, EtapaMerma},

	// Terminal stages – no outgoing transitions
	EtapaEntregado:             {},
	EtapaReingresadoInventario: {},
	EtapaSegundaMano:           {},
	EtapaDesarmado:             {},
	EtapaMerma:                 {},
}

// CanTransitionTo reports whether an article in stage e can move to stage t.
// It returns false for unknown stages (though Etapa.IsValid prevents that in practice).
func (e Etapa) CanTransitionTo(t Etapa) bool {
	allowed := validEtapaTransitions[e]
	for _, st := range allowed {
		if st == t {
			return true
		}
	}
	return false
}
