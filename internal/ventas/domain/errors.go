// Package domain holds the ventas module's aggregate root, child entities,
// value objects, domain events, and sentinel errors. It depends only on the
// standard library, uuid, decimal, and the platform/{domain,apperror,audit}
// packages — never on app, infra, ports, or other modules.
//
//nolint:misspell // domain vocabulary is Spanish (producto, descripcion, etc.) per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the ventas domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind →
// HTTPStatus) and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per the project conventions.
var (
	// ErrVentaNotFound is returned when a venta lookup misses.
	ErrVentaNotFound = apperror.NewNotFound(
		"venta_not_found",
		"venta no encontrada",
	)
	// ErrVentaYaCancelada is returned when attempting to cancel a venta that
	// is already canceled.
	ErrVentaYaCancelada = apperror.NewConflict(
		"venta_ya_cancelada",
		"la venta ya está cancelada",
	)
	// ErrVentaCanceladaInmutable is returned when attempting to mutate a
	// canceled venta (attach images, etc.).
	ErrVentaCanceladaInmutable = apperror.NewConflict(
		"venta_cancelada_inmutable",
		"no se puede modificar una venta cancelada",
	)

	// ErrTipoVentaInvalido is returned for unrecognized TipoVenta values.
	ErrTipoVentaInvalido = apperror.NewValidation(
		"tipo_venta_invalido",
		"el tipo de venta no es válido",
	)
	// ErrFrecPagoInvalida is returned for unrecognized FrecPago values.
	ErrFrecPagoInvalida = apperror.NewValidation(
		"frec_pago_invalida",
		"la frecuencia de pago no es válida",
	)
	// ErrDiaSemanaInvalido is returned for unrecognized DiaSemana values.
	ErrDiaSemanaInvalido = apperror.NewValidation(
		"dia_semana_invalido",
		"el día de la semana no es válido",
	)
	// ErrDiaMesInvalido is returned when DiaCobranzaMes is outside [1,31].
	ErrDiaMesInvalido = apperror.NewValidation(
		"dia_mes_invalido",
		"el día del mes debe estar entre 1 y 31",
	)

	// ErrGPSLatitudInvalida is returned when latitud is outside [-90,90].
	ErrGPSLatitudInvalida = apperror.NewValidation(
		"gps_latitud_invalida",
		"la latitud debe estar entre -90 y 90",
	)
	// ErrGPSLongitudInvalida is returned when longitud is outside [-180,180].
	ErrGPSLongitudInvalida = apperror.NewValidation(
		"gps_longitud_invalida",
		"la longitud debe estar entre -180 y 180",
	)

	// ErrPlanCreditoRequiredEnCredito is returned when a CREDITO venta is
	// missing a plan.
	ErrPlanCreditoRequiredEnCredito = apperror.NewValidation(
		"plan_credito_required_en_credito",
		"una venta a crédito requiere plan de crédito",
	)
	// ErrPlanCreditoNoPermitidoEnContado is returned when a CONTADO venta
	// carries a plan.
	ErrPlanCreditoNoPermitidoEnContado = apperror.NewValidation(
		"plan_credito_no_permitido_en_contado",
		"una venta de contado no admite plan de crédito",
	)
	// ErrDiaCobranzaRequeridoEnCredito is returned when a CREDITO venta is
	// missing the cobranza day VO.
	ErrDiaCobranzaRequeridoEnCredito = apperror.NewValidation(
		"dia_cobranza_required_en_credito",
		"una venta a crédito requiere día de cobranza",
	)
	// ErrDiaCobranzaIncoherenteSemanal is returned when frec_pago=SEMANAL
	// but the cobranza day is not a weekday.
	ErrDiaCobranzaIncoherenteSemanal = apperror.NewValidation(
		"dia_cobranza_incoherente_semanal",
		"una venta semanal requiere día de la semana",
	)
	// ErrDiaCobranzaIncoherenteQuincenalMensual is returned when frec_pago is
	// QUINCENAL/MENSUAL but the cobranza day VO is missing or invalid.
	ErrDiaCobranzaIncoherenteQuincenalMensual = apperror.NewValidation(
		"dia_cobranza_incoherente_quincenal_mensual",
		"una venta quincenal o mensual requiere día de la semana o día del mes",
	)

	// ErrMontoNegativo is returned when a monto value is negative.
	ErrMontoNegativo = apperror.NewValidation(
		"monto_negativo",
		"el monto no puede ser negativo",
	)
	// ErrPlazoNoPositivo is returned when plazo_meses is not > 0.
	ErrPlazoNoPositivo = apperror.NewValidation(
		"plazo_no_positivo",
		"el plazo en meses debe ser mayor a cero",
	)
	// ErrCantidadNoPositiva is returned when a producto cantidad is not > 0.
	ErrCantidadNoPositiva = apperror.NewValidation(
		"cantidad_no_positiva",
		"la cantidad debe ser mayor a cero",
	)

	// ErrVentaProductosVacios is returned when a venta is created without
	// at least one producto line.
	ErrVentaProductosVacios = apperror.NewValidation(
		"venta_productos_vacios",
		"la venta requiere al menos un producto",
	)
	// ErrVentaVendedoresVacios is returned when a venta is created without
	// at least one vendedor.
	ErrVentaVendedoresVacios = apperror.NewValidation(
		"venta_vendedores_vacios",
		"la venta requiere al menos un vendedor",
	)
	// ErrVentaAlmacenesIguales is returned when origen == destino.
	ErrVentaAlmacenesIguales = apperror.NewValidation(
		"venta_almacenes_iguales",
		"los almacenes de origen y destino no pueden ser iguales",
	)

	// ErrCalleRequerida is returned when calle is empty after trim.
	ErrCalleRequerida = apperror.NewValidation(
		"calle_required",
		"la calle es obligatoria",
	)
	// ErrCalleDemasiadoLarga is returned when calle exceeds 300 chars.
	ErrCalleDemasiadoLarga = apperror.NewValidation(
		"calle_too_long",
		"la calle excede 300 caracteres",
	)
	// ErrNumeroExteriorDemasiadoLargo is returned when numero_exterior
	// exceeds 20 chars.
	ErrNumeroExteriorDemasiadoLargo = apperror.NewValidation(
		"numero_exterior_too_long",
		"el número exterior excede 20 caracteres",
	)
	// ErrColoniaRequerida is returned when colonia is empty after trim.
	ErrColoniaRequerida = apperror.NewValidation(
		"colonia_required",
		"la colonia es obligatoria",
	)
	// ErrColoniaDemasiadoLarga is returned when colonia exceeds 120 chars.
	ErrColoniaDemasiadoLarga = apperror.NewValidation(
		"colonia_too_long",
		"la colonia excede 120 caracteres",
	)
	// ErrPoblacionRequerida is returned when poblacion is empty after trim.
	ErrPoblacionRequerida = apperror.NewValidation(
		"poblacion_required",
		"la población es obligatoria",
	)
	// ErrPoblacionDemasiadoLarga is returned when poblacion exceeds 120 chars.
	ErrPoblacionDemasiadoLarga = apperror.NewValidation(
		"poblacion_too_long",
		"la población excede 120 caracteres",
	)
	// ErrCiudadRequerida is returned when ciudad is empty after trim.
	ErrCiudadRequerida = apperror.NewValidation(
		"ciudad_required",
		"la ciudad es obligatoria",
	)
	// ErrCiudadDemasiadoLarga is returned when ciudad exceeds 120 chars.
	ErrCiudadDemasiadoLarga = apperror.NewValidation(
		"ciudad_too_long",
		"la ciudad excede 120 caracteres",
	)

	// ErrNombreClienteRequerido is returned when nombre_cliente is empty.
	ErrNombreClienteRequerido = apperror.NewValidation(
		"nombre_cliente_required",
		"el nombre del cliente es obligatorio",
	)
	// ErrNombreClienteDemasiadoLargo is returned when nombre_cliente exceeds
	// 200 chars.
	ErrNombreClienteDemasiadoLargo = apperror.NewValidation(
		"nombre_cliente_too_long",
		"el nombre del cliente excede 200 caracteres",
	)
	// ErrAvalDemasiadoLargo is returned when aval exceeds 200 chars.
	ErrAvalDemasiadoLargo = apperror.NewValidation(
		"aval_too_long",
		"el aval o responsable excede 200 caracteres",
	)

	// ErrComboNombreRequerido is returned when a combo nombre is empty.
	ErrComboNombreRequerido = apperror.NewValidation(
		"combo_nombre_required",
		"el nombre del combo es obligatorio",
	)
	// ErrComboNombreDemasiadoLargo is returned when combo nombre exceeds 200.
	ErrComboNombreDemasiadoLargo = apperror.NewValidation(
		"combo_nombre_too_long",
		"el nombre del combo excede 200 caracteres",
	)

	// ErrProductoArticuloRequerido is returned when articulo snapshot name
	// is empty.
	ErrProductoArticuloRequerido = apperror.NewValidation(
		"producto_articulo_required",
		"el nombre del artículo es obligatorio",
	)
	// ErrProductoArticuloDemasiadoLargo is returned when articulo snapshot
	// exceeds 200 chars.
	ErrProductoArticuloDemasiadoLargo = apperror.NewValidation(
		"producto_articulo_too_long",
		"el nombre del artículo excede 200 caracteres",
	)

	// ErrVendedorEmailRequerido is returned when vendedor email is empty.
	ErrVendedorEmailRequerido = apperror.NewValidation(
		"vendedor_email_required",
		"el email del vendedor es obligatorio",
	)
	// ErrVendedorEmailDemasiadoLargo is returned when vendedor email exceeds
	// 255 chars.
	ErrVendedorEmailDemasiadoLargo = apperror.NewValidation(
		"vendedor_email_too_long",
		"el email del vendedor excede 255 caracteres",
	)
	// ErrVendedorNombreRequerido is returned when vendedor nombre is empty.
	ErrVendedorNombreRequerido = apperror.NewValidation(
		"vendedor_nombre_required",
		"el nombre del vendedor es obligatorio",
	)
	// ErrVendedorNombreDemasiadoLargo is returned when vendedor nombre
	// exceeds 200 chars.
	ErrVendedorNombreDemasiadoLargo = apperror.NewValidation(
		"vendedor_nombre_too_long",
		"el nombre del vendedor excede 200 caracteres",
	)

	// ErrMimeNoPermitido is returned for a mime outside the allowed image
	// set.
	ErrMimeNoPermitido = apperror.NewValidation(
		"mime_no_permitido",
		"el tipo de imagen no está permitido",
	)
	// ErrStorageKindInvalido is returned for unrecognized StorageKind.
	ErrStorageKindInvalido = apperror.NewValidation(
		"storage_kind_invalido",
		"el tipo de almacenamiento no es válido",
	)
	// ErrStorageKeyInvalida is returned when storage key is empty, exceeds
	// the max length, contains traversal, or has unsafe bytes.
	ErrStorageKeyInvalida = apperror.NewValidation(
		"storage_key_invalida",
		"la llave de almacenamiento no es válida",
	)
	// ErrSizeBytesNegativo is returned for negative file sizes.
	ErrSizeBytesNegativo = apperror.NewValidation(
		"size_bytes_negativo",
		"el tamaño en bytes no puede ser negativo",
	)
	// ErrImagenDescripcionDemasiadoLarga is returned when imagen descripcion
	// exceeds 200 chars.
	ErrImagenDescripcionDemasiadoLarga = apperror.NewValidation(
		"imagen_descripcion_too_long",
		"la descripción de la imagen excede 200 caracteres",
	)

	// ErrImagenNotFound is returned when an imagen lookup misses.
	ErrImagenNotFound = apperror.NewNotFound(
		"imagen_not_found",
		"imagen no encontrada",
	)
	// ErrImagenDemasiadoGrande is returned when an upload exceeds the
	// processor's MAX_INPUT_BYTES cap.
	ErrImagenDemasiadoGrande = apperror.NewValidation(
		"imagen_too_large",
		"la imagen excede el tamaño máximo permitido",
	)
	// ErrImagenDecodeFallo is returned when the processor cannot decode
	// the uploaded image (corrupt or unsupported variant).
	ErrImagenDecodeFallo = apperror.NewValidation(
		"imagen_decode_failed",
		"no se pudo procesar la imagen",
	)
	// ErrReasonCancelacionRequerida is returned when cancel reason is empty.
	ErrReasonCancelacionRequerida = apperror.NewValidation(
		"reason_cancelacion_required",
		"la razón de la cancelación es obligatoria",
	)
	// ErrReasonCancelacionDemasiadoLarga is returned when cancel reason
	// exceeds 500 chars.
	ErrReasonCancelacionDemasiadoLarga = apperror.NewValidation(
		"reason_cancelacion_too_long",
		"la razón de la cancelación excede 500 caracteres",
	)
	// ErrFechaVentaZero is returned when fecha_venta is the zero time value.
	ErrFechaVentaZero = apperror.NewValidation(
		"fecha_venta_zero",
		"la fecha de la venta es obligatoria",
	)
	// ErrNotaDemasiadoLarga is returned when nota exceeds 500 chars.
	ErrNotaDemasiadoLarga = apperror.NewValidation(
		"nota_too_long",
		"la nota excede 500 caracteres",
	)
)
