package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the garantías domain value objects. English
// snake_case codes, Spanish lowercase messages without a trailing period
// (CLAUDE.md §3).
var (
	// ErrOrigenFolioInvalido is returned by NewOrigenFolio when the input is
	// not "piso" or "cliente".
	ErrOrigenFolioInvalido = apperror.NewValidation(
		"warranty_origin_invalid",
		"origen de folio inválido",
	)

	// ErrEstadoCuentaInvalido is returned by NewEstadoCuenta when the input
	// is not "liquidada" or "saldo_pendiente".
	ErrEstadoCuentaInvalido = apperror.NewValidation(
		"warranty_account_state_invalid",
		"estado de cuenta inválido",
	)

	// ErrEstadoFolioInvalido is returned by NewEstadoFolio when the input is
	// not one of the six recognized folio states.
	ErrEstadoFolioInvalido = apperror.NewValidation(
		"warranty_folio_state_invalid",
		"estado de folio inválido",
	)

	// ErrRutaReparacionInvalida is returned by NewRutaReparacion when the
	// input is not "proveedor" or "taller".
	ErrRutaReparacionInvalida = apperror.NewValidation(
		"warranty_repair_route_invalid",
		"ruta de reparación inválida",
	)

	// ErrDictamenInvalido is returned by NewDictamen when the input is not
	// "aceptada", "rechazada", or "sin_falla".
	ErrDictamenInvalido = apperror.NewValidation(
		"warranty_verdict_invalid",
		"dictamen inválido",
	)

	// ErrRolArticuloInvalido is returned by NewRolArticulo when the input is
	// not "original" or "reemplazo".
	ErrRolArticuloInvalido = apperror.NewValidation(
		"warranty_item_role_invalid",
		"rol de artículo inválido",
	)

	// ErrRolDecisorInvalido is returned by NewRolDecisor when the input is
	// not "carpinteria", "oficina", or "tecnica".
	ErrRolDecisorInvalido = apperror.NewValidation(
		"warranty_decider_role_invalid",
		"rol de quien decide inválido",
	)

	// ErrEtapaInvalida is returned by ParseEtapa when the input is not one
	// of the 19 recognized stages.
	ErrEtapaInvalida = apperror.NewValidation(
		"warranty_stage_invalid",
		"etapa inválida",
	)

	// ErrUbicacionInvalida is returned by ParseUbicacion when the input is
	// not one of the 8 recognized locations.
	ErrUbicacionInvalida = apperror.NewValidation(
		"warranty_location_invalid",
		"ubicación inválida",
	)

	// ErrDesenlaceInvalido is returned by ParseDesenlace when the input is
	// not one of the 6 recognized outcomes.
	ErrDesenlaceInvalido = apperror.NewValidation(
		"warranty_outcome_invalid",
		"desenlace inválido",
	)

	// ErrFolioInvalido is returned by NewFolio/ParseFolio when the folio is
	// not in the canonical GA-XXXXXX form.
	ErrFolioInvalido = apperror.NewValidation(
		"warranty_folio_invalid",
		"folio inválido",
	)

	// ErrTipoEventoInvalido is returned by ParseTipoEvento when the input is
	// not one of the 13 recognized event types.
	ErrTipoEventoInvalido = apperror.NewValidation(
		"warranty_event_type_invalid",
		"tipo de evento inválido",
	)

	// ErrDescriptionObligatoria is returned by AbrirGarantia when the
	// reported-issue description is empty.
	ErrDescriptionObligatoria = apperror.NewValidation(
		"warranty_description_required",
		"descripción obligatoria",
	)

	// ErrAbiertoPorObligatorio is returned by AbrirGarantia when the
	// operator who opens the folio is missing.
	ErrAbiertoPorObligatorio = apperror.NewValidation(
		"warranty_operator_required",
		"operador que abre el folio obligatorio",
	)

	// ErrClienteIDObligatorio is returned by AbrirGarantia for a cliente
	// folio without a client.
	ErrClienteIDObligatorio = apperror.NewValidation(
		"warranty_cliente_required",
		"cliente id obligatorio para folio de cliente",
	)

	// ErrVentaIDObligatorio is returned by AbrirGarantia for a cliente
	// folio without a sale.
	ErrVentaIDObligatorio = apperror.NewValidation(
		"warranty_venta_required",
		"venta id obligatoria para folio de cliente",
	)

	// ErrEstadoCuentaObligatorio is returned by AbrirGarantia for a cliente
	// folio without an account balance state.
	ErrEstadoCuentaObligatorio = apperror.NewValidation(
		"warranty_account_state_required",
		"estado de cuenta obligatorio para folio de cliente",
	)

	// ErrDomicilioObligatorio is returned by AbrirGarantia for a cliente
	// folio without a home address.
	ErrDomicilioObligatorio = apperror.NewValidation(
		"warranty_domicilio_required",
		"domicilio obligatorio para folio de cliente",
	)

	// ErrClienteIDNoPermitido is returned by AbrirGarantia for a piso
	// folio carrying client data.
	ErrClienteIDNoPermitido = apperror.NewValidation(
		"warranty_cliente_not_allowed",
		"cliente id no permitido en folio de piso",
	)

	// ErrVentaIDNoPermitido is returned by AbrirGarantia for a piso folio
	// carrying sale data.
	ErrVentaIDNoPermitido = apperror.NewValidation(
		"warranty_venta_not_allowed",
		"venta id no permitida en folio de piso",
	)

	// ErrEstadoCuentaNoPermitido is returned by AbrirGarantia for a piso
	// folio carrying an account balance state.
	ErrEstadoCuentaNoPermitido = apperror.NewValidation(
		"warranty_account_state_not_allowed",
		"estado de cuenta no permitido en folio de piso",
	)

	// ErrDomicilioNoPermitido is returned by AbrirGarantia for a piso folio
	// carrying a domicile or GPS data.
	ErrDomicilioNoPermitido = apperror.NewValidation(
		"warranty_domicilio_not_allowed",
		"domicilio no permitido en folio de piso",
	)

	// ErrTransicionEtapaNoPermitida is returned when an article cannot move
	// from its current stage to the requested one. The stage machine lives
	// in transiciones.go and is the single source of truth.
	ErrTransicionEtapaNoPermitida = apperror.NewValidation(
		"warranty_stage_transition_forbidden",
		"transición de etapa no permitida",
	)

	// ErrTransicionEstadoNoPermitida is returned when the folio state
	// machine forbids the requested state change.
	ErrTransicionEstadoNoPermitida = apperror.NewValidation(
		"warranty_state_transition_forbidden",
		"transición de estado no permitida",
	)

	// ErrArticuloNoEncontrado is returned when a method references an
	// article that does not belong to the folio.
	ErrArticuloNoEncontrado = apperror.NewValidation(
		"warranty_article_not_found",
		"artículo no encontrado",
	)

	// ErrGarantiaSinArticulos is returned by IniciarProceso when the folio
	// has no articles to repair.
	ErrGarantiaSinArticulos = apperror.NewValidation(
		"warranty_no_articles",
		"el folio necesita al menos un artículo para iniciar el proceso",
	)

	// ErrArticulosNoListosParaEntrega is returned by MarcarListoEntrega when
	// an article is neither in listo_entrega nor standby nor terminal.
	ErrArticulosNoListosParaEntrega = apperror.NewValidation(
		"warranty_article_not_ready",
		"todos los artículos deben estar en listo_entrega, standby o una etapa terminal",
	)

	// ErrArticuloRutaRequerida is returned when leaving en_revision through
	// a repair route without a defined route.
	ErrArticuloRutaRequerida = apperror.NewValidation(
		"warranty_route_required",
		"no se puede salir de en_revision sin ruta definida",
	)

	// ErrArticuloDictamenSoloProveedor is returned when a dictamen is
	// recorded for an article whose route is not proveedor.
	ErrArticuloDictamenSoloProveedor = apperror.NewValidation(
		"warranty_verdict_supplier_only",
		"el dictamen solo aplica a la ruta proveedor",
	)

	// ErrArticuloDesenlaceSoloParalelo is returned when a desenlace not
	// reachable from standby is recorded for an article.
	ErrArticuloDesenlaceSoloParalelo = apperror.NewValidation(
		"warranty_outcome_not_from_standby",
		"desenlace no aplicable desde standby",
	)

	// ErrArticuloDescriptionObligatoria is returned by newArticulo when the
	// article description is empty.
	ErrArticuloDescriptionObligatoria = apperror.NewValidation(
		"warranty_article_description_required",
		"descripción del artículo obligatoria",
	)

	// ErrMotivoCancelacionObligatorio is returned by Cancelar when the
	// cancellation reason is empty.
	ErrMotivoCancelacionObligatorio = apperror.NewValidation(
		"warranty_cancel_reason_required",
		"motivo de cancelación obligatorio",
	)

	// ErrFolioNoAdmiteArticulos is returned by AgregarArticulo when the
	// folio state is not abierto or en_proceso.
	ErrFolioNoAdmiteArticulos = apperror.NewValidation(
		"warranty_folio_not_open",
		"el folio no admite artículos en su estado actual",
	)

	// ErrEventoUsuarioObligatorio is returned by newEvento when the actor
	// who registers the event is missing.
	ErrEventoUsuarioObligatorio = apperror.NewValidation(
		"warranty_event_user_required",
		"usuario del evento obligatorio",
	)

	// ErrEventoClaveIdempotenciaObligatoria is returned by newEvento when
	// the idempotency key is missing.
	ErrEventoClaveIdempotenciaObligatoria = apperror.NewValidation(
		"warranty_event_idempotency_required",
		"clave de idempotencia del evento obligatoria",
	)

	// ErrEventoDeviceCreatedAtObligatorio is returned by newEvento when the
	// device timestamp is missing.
	ErrEventoDeviceCreatedAtObligatorio = apperror.NewValidation(
		"warranty_event_device_time_required",
		"fecha del dispositivo del evento obligatoria",
	)
)
