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
//
// The origin rule (R1) is a filter applied over this single table, never a
// second table nor an if spread through the mutators: the table is the
// machine, the origin decides which of its edges a folio may walk. Collection
// stages and entregado belong to cliente folios, reingresado_inventario
// belongs to piso folios, and the registrado -> en_revision edge is the only
// one whose restriction is about the source (a cliente folio must pass through
// collection first).
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
	EtapaListoEntrega:     {EtapaEntregado, EtapaReingresadoInventario},

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

// destinoPermitidoPorOrigen applies R1 to a destination stage: collection
// stages and entregado exist only for cliente folios, reingresado_inventario
// only for piso folios, and every other stage is reachable from both origins.
func destinoPermitidoPorOrigen(origen OrigenFolio, hasta Etapa) bool {
	switch hasta { //nolint:exhaustive // R1 pins only four destinations; the rest are reachable from both origins
	case EtapaPendienteRecoleccion, EtapaRecolectado, EtapaEntregado:
		return origen.EsCliente()
	case EtapaReingresadoInventario:
		return origen.EsPiso()
	default:
		return true
	}
}

// transicionPermitidaPorOrigen reports whether the stage machine, filtered by
// origin, allows desde -> hasta (R1). The registrado -> en_revision edge is the
// only one whose restriction is about the source rather than the destination:
// a cliente folio must walk the collection stages first.
func transicionPermitidaPorOrigen(desde, hasta Etapa, origen OrigenFolio) bool {
	if !desde.CanTransitionTo(hasta) {
		return false
	}
	if desde == EtapaRegistrado && hasta == EtapaEnRevision {
		return origen.EsPiso()
	}
	return destinoPermitidoPorOrigen(origen, hasta)
}

// reservadaPorDecision reports whether reaching hasta is reserved for a
// decision command (R2): cambio_autorizado and standby only come from
// AutorizarCambioFisico or MarcarNoDevuelto, entregado is moved by the folio
// (Entregar), and orden_generada/en_taller only from en_revision through
// RegistrarDiagnostico (no other edge enters them).
func reservadaPorDecision(hasta Etapa) bool {
	return hasta == EtapaCambioAutorizado || hasta == EtapaStandby || hasta == EtapaEntregado ||
		hasta == EtapaOrdenGenerada || hasta == EtapaEnTaller
}

// etapaConSalidaReservada reports whether desde is a stage whose exits belong
// exclusively to a decision command (R2): dictamen_recibido leaves only through
// RegistrarDictamen and standby only through RegistrarDesenlace.
func etapaConSalidaReservada(desde Etapa) bool {
	return desde == EtapaDictamenRecibido || desde == EtapaStandby
}

// puedeAvanzar is the single oracle for AvanzarArticulo: the R1 origin filter
// composed with R2's limited reach. The mutator resolves every move here, and
// the screen catalog (R11, SiguientesPorAvanzar) will reuse it, so the machine
// is described once.
func puedeAvanzar(desde, hasta Etapa, origen OrigenFolio) bool {
	if reservadaPorDecision(hasta) || etapaConSalidaReservada(desde) {
		return false
	}
	return transicionPermitidaPorOrigen(desde, hasta, origen)
}

// ubicacionPorEtapa is the R3 table: arriving at one of these stages moves the
// article to the listed location automatically. Stages absent from the table
// leave the location untouched on purpose — dictamen_recibido does not move the
// item because it may still be at the supplier (§3.2), and that return trip is
// recorded by hand with MoverArticulo. It lives next to the stage machine so
// the whole movement rule is edited in one file.
var ubicacionPorEtapa = map[Etapa]Ubicacion{ //nolint:exhaustive // partial on purpose: an absent stage means "no location change" (R3)
	EtapaRecolectado:           UbicacionEnTransito,
	EtapaEnRevision:            UbicacionAlmacenRevision,
	EtapaEnviadoProveedor:      UbicacionProveedor,
	EtapaEnTaller:              UbicacionTaller,
	EtapaEntregado:             UbicacionEntregado,
	EtapaReingresadoInventario: UbicacionAlmacenRevision,
	EtapaSegundaMano:           UbicacionAlmacenSegundaMano,
	EtapaDesarmado:             UbicacionBaja,
	EtapaMerma:                 UbicacionBaja,
}

// ubicacionParaEtapa returns the location an article must hold after arriving
// at e (R3), and whether e imposes one at all.
func ubicacionParaEtapa(e Etapa) (Ubicacion, bool) {
	u, ok := ubicacionPorEtapa[e]
	return u, ok
}

// cambioUbicacionPorEtapa returns the from/to pair an event must record when an
// article arrives at hasta (R3): nil/nil when the stage does not move the item
// or moves it to where it already is, so a non-location event keeps both
// columns NULL.
func cambioUbicacionPorEtapa(actual Ubicacion, hasta Etapa) (*Ubicacion, *Ubicacion) {
	nueva, impone := ubicacionParaEtapa(hasta)
	if !impone || nueva == actual {
		return nil, nil
	}
	desde := actual
	return &desde, &nueva
}

// ubicacionesManualesPermitidas lists what MoverArticulo may set by hand:
// entregado and baja are excluded on purpose — only a stage puts the item
// there (R3), never a direct call.
var ubicacionesManualesPermitidas = map[Ubicacion]bool{ //nolint:exhaustive // allow-list only: entregado/baja are set by a stage, never by hand (R3)
	UbicacionEnTransito:         true,
	UbicacionAlmacenRevision:    true,
	UbicacionTaller:             true,
	UbicacionProveedor:          true,
	UbicacionAlmacenSegundaMano: true,
}

// ubicacionManualPermitida reports whether MoverArticulo may target u.
func ubicacionManualPermitida(u Ubicacion) bool {
	return ubicacionesManualesPermitidas[u]
}

// ubicacionFueraDelTaller reports whether an article in u blocks Cancelar (R5):
// at the supplier or in transit the item is out of our hands, and the folio
// cannot be voided until it comes back. Being at any workshop location does not
// block.
func ubicacionFueraDelTaller(u Ubicacion) bool {
	return u == UbicacionProveedor || u == UbicacionEnTransito
}

// SiguientesPorAvanzar returns the stages AvanzarArticulo permits from etapa for
// that origin, already filtered by R1 and R2 and in the stable stage order (R11).
// It is the read model the screen consumes instead of reimplementing the
// machine: the same puedeAvanzar that resolves the mutator decides every entry,
// so the catalog and the command can never disagree.
//
// The result is never nil, so the wire shape is [] rather than null when a stage
// (dictamen_recibido, standby, a terminal one) has no advance exit.
func SiguientesPorAvanzar(etapa Etapa, origen OrigenFolio) []Etapa {
	siguientes := make([]Etapa, 0, len(etapasOrdenadas))
	for _, hasta := range etapasOrdenadas {
		if puedeAvanzar(etapa, hasta, origen) {
			siguientes = append(siguientes, hasta)
		}
	}
	return siguientes
}
