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
	// ErrVentaNoEditable is returned when attempting to edit a venta whose
	// status is not 'borrador'.
	ErrVentaNoEditable = apperror.NewConflict(
		"venta_no_editable",
		"la venta no se puede editar en su estado actual",
	)
	// ErrClienteIDInvalido is returned when the supplied cliente_id does not
	// resolve to a row in Microsip's CLIENTES table.
	ErrClienteIDInvalido = apperror.NewValidation(
		"cliente_id_invalido",
		"el cliente_id no es válido",
	)
	// ErrClienteYaAsignado is returned when AsignarClienteMicrosip is called on a
	// venta that already has a Microsip cliente_id linked. Re-assignment is not
	// allowed — the venta's link to Microsip is set exactly once at apply time.
	ErrClienteYaAsignado = apperror.NewConflict(
		"cliente_ya_asignado",
		"la venta ya tiene cliente_id asignado",
	)
	// ErrVendedorUsuarioNoEncontrado is returned when one or more vendedor
	// usuario_ids on a CrearVenta request do not resolve to a row in
	// MSP_USUARIOS. The Details payload carries the offending ids so the
	// HTTP layer can surface which vendedor is unknown.
	ErrVendedorUsuarioNoEncontrado = apperror.NewValidation(
		"vendedor_usuario_no_encontrado",
		"el usuario_id del vendedor no existe",
	)
	// ErrEstadoRegistroInvalido is returned for unrecognized EstadoRegistro
	// values.
	ErrEstadoRegistroInvalido = apperror.NewValidation(
		"venta_estado_registro_invalido",
		"el estado del registro no es válido",
	)
	// ErrSituacionInvalida is returned for unrecognized Situacion values.
	ErrSituacionInvalida = apperror.NewValidation(
		"venta_situacion_invalida",
		"la situación de la venta no es válida",
	)
	// ErrSincronizacionInvalida is returned for unrecognized Sincronizacion
	// values.
	ErrSincronizacionInvalida = apperror.NewValidation(
		"venta_sincronizacion_invalida",
		"la sincronización de la venta no es válida",
	)
	// ErrVentaNoEnviableARevision is returned when EnviarARevision is called on
	// a venta whose situación is not 'borrador'.
	ErrVentaNoEnviableARevision = apperror.NewConflict(
		"venta_no_enviable_a_revision",
		"la venta solo se puede enviar a revisión desde borrador",
	)
	// ErrVentaNoAprobable is returned when Aprobar is called on a venta whose
	// situación is not 'revisada'.
	ErrVentaNoAprobable = apperror.NewConflict(
		"venta_no_aprobable",
		"la venta solo se puede aprobar desde revisada",
	)
	// ErrVentaNoRegresableABorrador is returned when RegresarABorrador is
	// called on a venta whose situación is not 'revisada'.
	ErrVentaNoRegresableABorrador = apperror.NewConflict(
		"venta_no_regresable_a_borrador",
		"la venta solo se puede regresar a borrador desde revisada",
	)
	// ErrVentaNoAplicable is returned when MarcarAplicada is called on a venta
	// that is not active+aprobada+pendiente.
	ErrVentaNoAplicable = apperror.NewConflict(
		"venta_no_aplicable",
		"solo se puede aplicar una venta activa, aprobada y pendiente",
	)
	// ErrVentaYaAplicada is returned when attempting to apply or cancel a venta
	// already materialized in Microsip.
	ErrVentaYaAplicada = apperror.NewConflict(
		"venta_ya_aplicada",
		"la venta ya fue aplicada en microsip",
	)
	// ErrVentaNoActiva is returned when an operation requires an active venta
	// but the registro is 'deleted'.
	ErrVentaNoActiva = apperror.NewConflict(
		"venta_no_activa",
		"la venta no está activa",
	)
	// ErrMicrosipArtefactosRequeridos is returned when MarcarAplicada is given
	// an empty docto id or folio.
	ErrMicrosipArtefactosRequeridos = apperror.NewValidation(
		"microsip_artefactos_requeridos",
		"el docto y folio de microsip son obligatorios",
	)
	// ErrZonaSinCaja is returned when the cliente's zona has no caja/cajero
	// mapping in MSP_CFG_ZONA_CAJA.
	ErrZonaSinCaja = apperror.NewValidation(
		"zona_sin_caja",
		"la zona del cliente no tiene caja configurada",
	)
	// ErrVentaSinZona is returned when applying a venta whose dirección has no
	// zona_cliente_id (required to resolve the caja).
	ErrVentaSinZona = apperror.NewValidation(
		"venta_sin_zona",
		"la venta no tiene zona de cliente para resolver la caja",
	)
	// ErrFrecuenciaSinFormaPago is returned when a credit frequency has no
	// FORMA_DE_PAGO mapping in MSP_CFG_FRECUENCIA_FORMA_PAGO.
	ErrFrecuenciaSinFormaPago = apperror.NewValidation(
		"frecuencia_sin_forma_pago",
		"la frecuencia de pago no tiene forma de pago configurada",
	)
	// ErrPlazoSinCreditoMeses is returned when a credit term has no
	// CREDITO_EN_MESES mapping in MSP_CFG_PLAZO_CREDITO.
	ErrPlazoSinCreditoMeses = apperror.NewValidation(
		"plazo_sin_credito_meses",
		"el plazo en meses no tiene crédito en meses configurado",
	)
	// ErrNumVendedoresSinMapeo is returned when a seller count has no
	// NUMERO_DE_VENDEDORES mapping in MSP_CFG_NUM_VENDEDORES.
	ErrNumVendedoresSinMapeo = apperror.NewValidation(
		"num_vendedores_sin_mapeo",
		"el número de vendedores no tiene mapeo configurado",
	)
	// ErrConfigAplicarFaltante is returned when the singleton MSP_CFG_APLICAR
	// row is missing.
	ErrConfigAplicarFaltante = apperror.NewValidation(
		"config_aplicar_faltante",
		"falta la configuración de aplicación de ventas",
	)
	// ErrVentaSinClienteMicrosip is returned when AplicarVenta is called on a
	// venta that has no cliente_id link to Microsip's CLIENTES table.
	ErrVentaSinClienteMicrosip = apperror.NewValidation(
		"venta_sin_cliente_microsip",
		"la venta no tiene cliente de microsip para aplicar",
	)

	// ErrAprobacionFechaZero is returned when constructing an Aprobacion
	// with a zero timestamp.
	ErrAprobacionFechaZero = apperror.NewValidation(
		"aprobacion_fecha_zero",
		"la fecha de aprobación es obligatoria",
	)
	// ErrAprobacionByRequired is returned when constructing an Aprobacion
	// without an approver.
	ErrAprobacionByRequired = apperror.NewValidation(
		"aprobacion_by_required",
		"el usuario que aprueba es obligatorio",
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
	// ErrClienteReferenciaDemasiadoLarga is returned when referencia exceeds
	// 99 chars (the width of MSP_VENTAS.CLIENTE_REFERENCIA and
	// LIBRES_CLIENTES.REFERENCIA).
	ErrClienteReferenciaDemasiadoLarga = apperror.NewValidation(
		"cliente_referencia_demasiado_larga",
		"la referencia del cliente excede los 99 caracteres",
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
	// ErrProductoAlmacenOrigenRequerido is returned when a producto outside
	// any combo is missing its origin warehouse.
	ErrProductoAlmacenOrigenRequerido = apperror.NewValidation(
		"producto_almacen_origen_required",
		"el almacén de origen del producto es obligatorio",
	)
	// ErrProductoAlmacenDestinoRequerido is returned when a producto outside
	// any combo is missing its destination warehouse.
	ErrProductoAlmacenDestinoRequerido = apperror.NewValidation(
		"producto_almacen_destino_required",
		"el almacén de destino del producto es obligatorio",
	)
	// ErrProductoEnComboNoLlevaAlmacen is returned when a producto that
	// belongs to a combo carries its own almacenes (they should be inherited
	// from the parent combo).
	ErrProductoEnComboNoLlevaAlmacen = apperror.NewValidation(
		"producto_en_combo_no_lleva_almacen",
		"un producto dentro de un combo no lleva almacenes propios",
	)
	// ErrComboCantidadNoPositiva is returned when combo cantidad is not > 0.
	ErrComboCantidadNoPositiva = apperror.NewValidation(
		"combo_cantidad_no_positiva",
		"la cantidad del combo debe ser mayor a cero",
	)
	// ErrComboAlmacenOrigenRequerido is returned when combo almacen_origen
	// is missing or non-positive.
	ErrComboAlmacenOrigenRequerido = apperror.NewValidation(
		"combo_almacen_origen_required",
		"el almacén de origen del combo es obligatorio",
	)
	// ErrComboAlmacenDestinoRequerido is returned when combo almacen_destino
	// is missing or non-positive.
	ErrComboAlmacenDestinoRequerido = apperror.NewValidation(
		"combo_almacen_destino_required",
		"el almacén de destino del combo es obligatorio",
	)
	// ErrProductoComboReferenciaInvalida is returned when a producto.combo_id
	// does not match any combo in the venta.
	ErrProductoComboReferenciaInvalida = apperror.NewValidation(
		"producto_combo_referencia_invalida",
		"el combo referenciado por el producto no existe en la venta",
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

	// ErrVentaEvidenciaRequerida is returned when CrearVentaConImagenes or
	// AplicarVenta receives a venta without at least one comprobante. Every
	// venta del showroom debe llevar firma o ID del cliente; no hay excepción.
	ErrVentaEvidenciaRequerida = apperror.NewValidation(
		"venta_evidencia_requerida",
		"la venta requiere al menos una imagen de evidencia",
	)

	// ErrImagenIDDuplicado is returned when CrearVentaConImagenes receives
	// the same imagen UUID twice in the same request. Client must deduplicate
	// before retrying.
	ErrImagenIDDuplicado = apperror.NewValidation(
		"imagen_id_duplicado",
		"el id de imagen aparece más de una vez en la solicitud",
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
	// ErrStringUnsafeChars is returned when a string field contains a NUL
	// byte or ASCII control characters (other than tab/CR/LF). The columns
	// accept any valid UTF-8 character including emoji, CJK, etc.
	ErrStringUnsafeChars = apperror.NewValidation(
		"string_unsafe_chars",
		"el texto contiene caracteres de control no permitidos",
	)
	// ErrMontoDemasiadosDecimales is returned when a monetary value carries
	// more than 2 decimal places — the storage column is NUMERIC(p, 2) and
	// extra precision would be silently rounded by the driver.
	ErrMontoDemasiadosDecimales = apperror.NewValidation(
		"monto_demasiados_decimales",
		"el monto admite máximo 2 decimales",
	)
	// ErrMontoDemasiadoGrande is returned when a monetary value exceeds the
	// declared NUMERIC(14,2) capacity. Firebird's NUMERIC is INT64-backed so
	// the declared precision is a soft hint — we enforce it explicitly.
	ErrMontoDemasiadoGrande = apperror.NewValidation(
		"monto_demasiado_grande",
		"el monto excede el máximo permitido",
	)
	// ErrCantidadDemasiadosDecimales is returned when a cantidad carries
	// more than 4 decimal places — the storage column is NUMERIC(10,4).
	ErrCantidadDemasiadosDecimales = apperror.NewValidation(
		"cantidad_demasiados_decimales",
		"la cantidad admite máximo 4 decimales",
	)
)
