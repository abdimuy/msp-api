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
		"warranty_customer_required",
		"cliente id obligatorio para folio de cliente",
	)

	// ErrVentaIDObligatorio is returned by AbrirGarantia for a cliente
	// folio without a sale.
	ErrVentaIDObligatorio = apperror.NewValidation(
		"warranty_sale_required",
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
		"warranty_address_required",
		"domicilio obligatorio para folio de cliente",
	)

	// ErrClienteIDNoPermitido is returned by AbrirGarantia for a piso
	// folio carrying client data.
	ErrClienteIDNoPermitido = apperror.NewValidation(
		"warranty_customer_not_allowed",
		"cliente id no permitido en folio de piso",
	)

	// ErrVentaIDNoPermitido is returned by AbrirGarantia for a piso folio
	// carrying sale data.
	ErrVentaIDNoPermitido = apperror.NewValidation(
		"warranty_sale_not_allowed",
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
		"warranty_address_not_allowed",
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

	// ErrGarantiaNoEncontrada is returned by the repository and the
	// commands when the folio does not exist.
	ErrGarantiaNoEncontrada = apperror.NewNotFound(
		"warranty_not_found",
		"garantía no encontrada",
	)

	// ErrClaveIdempotenciaDuplicada is returned by the repository when the
	// idempotency key already exists. A command treats it as a repetition
	// (brief decision 6), not as a failure: the phone re-sent the same
	// request.
	ErrClaveIdempotenciaDuplicada = apperror.NewConflict(
		"warranty_idempotency_key_duplicate",
		"clave de idempotencia duplicada",
	)

	// ErrClaveIdempotenciaDeOtroFolio is returned when the idempotency key
	// belongs to a DIFFERENT folio. Unlike the plain duplicate, this is a
	// real error: the caller is confused about which folio it is writing
	// to, and hiding that would write the change to the wrong expediente.
	ErrClaveIdempotenciaDeOtroFolio = apperror.NewConflict(
		"warranty_idempotency_key_other_folio",
		"la clave de idempotencia pertenece a otro folio",
	)

	// ErrRolDecisorObligatorio is returned by the three decision methods
	// (RegistrarDiagnostico, AutorizarCambioFisico, RegistrarDesenlace)
	// when the actor carries no role. Permission says WHETHER someone may
	// decide; the role says FROM WHICH area they did, and without it the
	// expediente cannot say who authorized an expensive swap.
	ErrRolDecisorObligatorio = apperror.NewValidation(
		"warranty_decision_role_required",
		"rol de quien decide obligatorio",
	)

	// ErrEventoGPSInvalido is returned by newEvento when the coordinates
	// are half a pair, out of range, or not finite.
	ErrEventoGPSInvalido = apperror.NewValidation(
		"warranty_event_gps_invalid",
		"coordenadas gps inválidas",
	)

	// ErrPermisoInvalido is returned by ParsePermiso when the input is not
	// one of the five recognized permission codes.
	ErrPermisoInvalido = apperror.NewValidation(
		"warranty_permission_invalid",
		"permiso inválido",
	)

	// ErrPermisoDenegado is returned by the commands when the authenticated
	// user does not hold the permission the action requires.
	ErrPermisoDenegado = apperror.NewForbidden(
		"warranty_permission_denied",
		"sin permiso para esta acción",
	)

	// ErrUsuarioNoAutenticado is returned by the commands when there is no
	// authenticated user behind the request. No event can be written
	// without one, so it fails before the folio is even loaded.
	ErrUsuarioNoAutenticado = apperror.NewUnauthorized(
		"warranty_user_unauthenticated",
		"usuario no autenticado",
	)

	// ErrImagenRutaInvalida is returned by NewImagen when the blob path is
	// empty, absolute, or escapes STORAGE_DIR through a ".." segment.
	ErrImagenRutaInvalida = apperror.NewValidation(
		"warranty_image_path_invalid",
		"ruta de imagen inválida",
	)

	// ErrImagenSubidaPorObligatorio is returned by NewImagen when the
	// operator who uploaded the evidence is missing.
	ErrImagenSubidaPorObligatorio = apperror.NewValidation(
		"warranty_image_uploader_required",
		"quien sube la imagen obligatorio",
	)

	// ErrImagenEventoObligatorio is returned by NewImagen when the event the
	// evidence hangs from is missing (uuid.Nil).
	ErrImagenEventoObligatorio = apperror.NewValidation(
		"warranty_image_event_required",
		"evento de la imagen obligatorio",
	)

	// ErrImagenCreatedAtObligatorio is returned by NewImagen when the
	// registration timestamp is missing.
	ErrImagenCreatedAtObligatorio = apperror.NewValidation(
		"warranty_image_time_required",
		"fecha de la imagen obligatoria",
	)

	// ErrEventoUsuarioMuyLargo is returned by newEvento when the actor
	// name exceeds the 64-character column width (MSP_GA_EVENTO.USUARIO).
	ErrEventoUsuarioMuyLargo = apperror.NewValidation(
		"warranty_event_user_too_long",
		"usuario del evento demasiado largo",
	)

	// ErrAbiertoPorMuyLargo is returned by AbrirGarantia when the operator
	// name exceeds the 64-character column width (MSP_GA_GARANTIA.ABIERTO_POR).
	ErrAbiertoPorMuyLargo = apperror.NewValidation(
		"warranty_operator_too_long",
		"operador que abre el folio demasiado largo",
	)

	// ErrImagenSubidaPorMuyLargo is returned by NewImagen when the uploader
	// name exceeds the 64-character column width (MSP_GA_IMAGEN.SUBIDA_POR).
	ErrImagenSubidaPorMuyLargo = apperror.NewValidation(
		"warranty_image_uploader_too_long",
		"quien sube la imagen demasiado largo",
	)

	// ErrImagenRutaMuyLarga is returned by NewImagen when the blob path
	// exceeds the 500-character column width (MSP_GA_IMAGEN.RUTA).
	ErrImagenRutaMuyLarga = apperror.NewValidation(
		"warranty_image_path_too_long",
		"ruta de imagen demasiado larga",
	)

	// ErrImagenDescriptionMuyLarga is returned by NewImagen when the caption
	// exceeds the 500-character column width (MSP_GA_IMAGEN.DESCRIPCION).
	//nolint:misspell // column name in Spanish per project vocabulary
	ErrImagenDescriptionMuyLarga = apperror.NewValidation(
		"warranty_image_caption_too_long",
		"descripción de imagen demasiado larga",
	)

	// ErrArticuloClaveMuyLarga is returned by newArticulo when the article
	// key exceeds the 30-character column width (MSP_GA_ARTICULO.CLAVE).
	ErrArticuloClaveMuyLarga = apperror.NewValidation(
		"warranty_article_key_too_long",
		"clave del artículo demasiado larga",
	)

	// ErrEventoClaveIdempotenciaInvalida is returned by newEvento when the
	// idempotency key is not a UUID. The phone generates it as a UUID and
	// the column is CHAR(36); anything else is a client bug that would
	// poison the UNIQUE index with garbage. The key is stored in canonical
	// form so every spelling of the same retry collapses into one value.
	ErrEventoClaveIdempotenciaInvalida = apperror.NewValidation(
		"warranty_event_idempotency_invalid",
		"clave de idempotencia del evento inválida",
	)

	// ErrCalleMuyLarga is returned by AbrirGarantia when the street line
	// exceeds the 300-character column width (MSP_GA_GARANTIA.CALLE).
	ErrCalleMuyLarga = apperror.NewValidation(
		"warranty_street_too_long",
		"calle demasiado larga",
	)

	// ErrNumeroExteriorMuyLargo is returned by AbrirGarantia when the
	// exterior number exceeds the 20-character column width.
	ErrNumeroExteriorMuyLargo = apperror.NewValidation(
		"warranty_street_number_too_long",
		"número exterior demasiado largo",
	)

	// ErrColoniaMuyLarga is returned by AbrirGarantia when the neighborhood
	// exceeds the 100-character column width (MSP_GA_GARANTIA.COLONIA).
	ErrColoniaMuyLarga = apperror.NewValidation(
		"warranty_neighborhood_too_long",
		"colonia demasiado larga",
	)

	// ErrLocalidadMuyLarga is returned by AbrirGarantia when the locality
	// exceeds the 100-character column width (MSP_GA_GARANTIA.LOCALIDAD).
	ErrLocalidadMuyLarga = apperror.NewValidation(
		"warranty_locality_too_long",
		"localidad demasiado larga",
	)

	// ErrCiudadMuyLarga is returned by AbrirGarantia when the city exceeds
	// the 100-character column width (MSP_GA_GARANTIA.CIUDAD).
	ErrCiudadMuyLarga = apperror.NewValidation(
		"warranty_city_too_long",
		"ciudad demasiado larga",
	)

	// ErrCodigoPostalMuyLargo is returned by AbrirGarantia when the postal
	// code exceeds the 10-character column width (MSP_GA_GARANTIA.CODIGO_POSTAL).
	ErrCodigoPostalMuyLargo = apperror.NewValidation(
		"warranty_postal_code_too_long",
		"código postal demasiado largo",
	)

	// ErrArticuloDescriptionMuyLarga is returned by newArticulo when the
	// article description exceeds the 300-character column width
	// (MSP_GA_ARTICULO.DESCRIPCION).
	//nolint:misspell // column name in Spanish per project vocabulary
	ErrArticuloDescriptionMuyLarga = apperror.NewValidation(
		"warranty_article_description_too_long",
		"descripción del artículo demasiado larga",
	)
)
