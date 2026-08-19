package domain

// Etapa represents the stage of a warranty item (the article's lifecycle step).
type Etapa string

// All 19 stages from the design, grouped by phase for readability.
const (
	// Common trunk.
	EtapaRegistrado           Etapa = "registrado"
	EtapaPendienteRecoleccion Etapa = "pendiente_recoleccion"
	EtapaRecolectado          Etapa = "recolectado"
	EtapaEnRevision           Etapa = "en_revision"

	// Supplier route.
	EtapaOrdenGenerada          Etapa = "orden_generada"
	EtapaEnviadoProveedor       Etapa = "enviado_proveedor"
	EtapaDictamenRecibido       Etapa = "dictamen_recibido"
	EtapaReparadoProveedor      Etapa = "reparado_proveedor"
	EtapaEsperaRespuestaCliente Etapa = "espera_respuesta_cliente"

	// Workshop route.
	EtapaEnTaller       Etapa = "en_taller"
	EtapaReparadoTaller Etapa = "reparado_taller"

	// Convergence.
	EtapaCambioAutorizado      Etapa = "cambio_autorizado"
	EtapaListoEntrega          Etapa = "listo_entrega"
	EtapaEntregado             Etapa = "entregado"
	EtapaReingresadoInventario Etapa = "reingresado_inventario"

	// Parallel flow (standby and terminal outcomes).
	EtapaStandby     Etapa = "standby"
	EtapaSegundaMano Etapa = "segunda_mano"
	EtapaDesarmado   Etapa = "desarmado"
	EtapaMerma       Etapa = "merma"
)

// ParseEtapa validates and returns an Etapa.
// Returns ErrEtapaInvalida if s is not one of the 19 recognized stages.
func ParseEtapa(s string) (Etapa, error) {
	e := Etapa(s)
	if !e.IsValid() {
		return "", ErrEtapaInvalida
	}
	return e, nil
}

// IsValid reports whether e is a known Etapa value.
func (e Etapa) IsValid() bool {
	switch e {
	case EtapaRegistrado, EtapaPendienteRecoleccion, EtapaRecolectado,
		EtapaEnRevision, EtapaOrdenGenerada, EtapaEnviadoProveedor,
		EtapaDictamenRecibido, EtapaReparadoProveedor,
		EtapaEsperaRespuestaCliente, EtapaEnTaller, EtapaReparadoTaller,
		EtapaCambioAutorizado, EtapaListoEntrega, EtapaEntregado,
		EtapaReingresadoInventario, EtapaStandby, EtapaSegundaMano,
		EtapaDesarmado, EtapaMerma:
		return true
	}
	return false
}

// String returns the string representation of e.
func (e Etapa) String() string { return string(e) }

// EsTerminal reports whether e is a terminal stage (entregado, reingresado_inventario,
// segunda_mano, desarmado, merma). Standby is NOT terminal.
func (e Etapa) EsTerminal() bool {
	switch e {
	case EtapaEntregado, EtapaReingresadoInventario,
		EtapaSegundaMano, EtapaDesarmado, EtapaMerma:
		return true
	case EtapaRegistrado, EtapaPendienteRecoleccion, EtapaRecolectado,
		EtapaEnRevision, EtapaOrdenGenerada, EtapaEnviadoProveedor,
		EtapaDictamenRecibido, EtapaReparadoProveedor,
		EtapaEsperaRespuestaCliente, EtapaEnTaller, EtapaReparadoTaller,
		EtapaCambioAutorizado, EtapaListoEntrega, EtapaStandby:
		return false
	}
	// This should never be reached because IsValid() ensures only known values.
	return false
}
