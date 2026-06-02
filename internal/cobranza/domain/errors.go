// Package domain holds the cobranza module's value objects and sentinel
// errors. It depends only on the standard library and the platform/apperror
// package — never on app, infra, or other modules.
//
//nolint:misspell // domain vocabulary is Spanish (descripcion, cargo, etc.) per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the cobranza domain. All are produced via apperror.New*
// constructors so they participate in the typed error model and so the err113
// linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per project conventions.
var (
	// ErrSaldoNoEncontrado is returned when a saldo lookup misses.
	ErrSaldoNoEncontrado = apperror.NewNotFound(
		"saldo_no_encontrado",
		"no se encontró saldo para esta venta",
	)

	// ErrZonaInvalida is returned when a zona ID is out of range or unknown.
	ErrZonaInvalida = apperror.NewValidation(
		"cobranza_zona_invalida",
		"zona inválida",
	)

	// ErrVentanaDiasInvalida is returned when ventanaDias falls outside [0, 90].
	ErrVentanaDiasInvalida = apperror.NewValidation(
		"cobranza_ventana_dias_invalida",
		"ventana de días fuera de rango (0-90)",
	)

	// ErrParametrosExcluyentes is returned when both `desde` and `ventana_dias`
	// are supplied to EnRutaPorZona. Only one of the two is accepted per call.
	//
	//nolint:misspell // "parametros" (Spanish) appears in the user-facing code.
	ErrParametrosExcluyentes = apperror.NewValidation(
		"cobranza_parametros_excluyentes",
		"se debe pasar 'desde' o 'ventana_dias' pero no ambos",
	)

	// ErrDesdeInvalido is returned when the `desde` timestamp cannot be parsed
	// or falls outside the supported window.
	ErrDesdeInvalido = apperror.NewValidation(
		"cobranza_desde_invalido",
		"el parámetro 'desde' debe ser una fecha YYYY-MM-DD o un timestamp RFC3339",
	)

	// ErrPagoNoEncontrado is returned when a pago lookup misses.
	ErrPagoNoEncontrado = apperror.NewNotFound(
		"pago_no_encontrado",
		"no se encontró el pago",
	)

	// ErrCursorInvalido is returned when the sync cursor cannot be parsed.
	ErrCursorInvalido = apperror.NewValidation(
		"cobranza_cursor_invalido",
		"el parámetro 'cursor' debe ser un timestamp RFC3339",
	)

	// ErrSincronizacionInvalida is returned when an invalid Sincronizacion
	// string is parsed.
	ErrSincronizacionInvalida = apperror.NewValidation(
		"sincronizacion_invalida",
		"estado de sincronización inválido",
	)

	// ErrMimeNoPermitido is returned for a MIME outside the allowed image/PDF
	// whitelist (image/jpeg, image/png, image/gif, image/webp, application/pdf).
	ErrMimeNoPermitido = apperror.NewValidation(
		"pago_imagen_mime_no_permitido",
		"tipo de archivo no permitido",
	)

	// ErrStorageKindInvalido is returned for unrecognized StorageKind.
	ErrStorageKindInvalido = apperror.NewValidation(
		"pago_imagen_storage_kind_invalido",
		"tipo de almacenamiento inválido",
	)

	// ErrStorageKeyInvalida is returned when storage key is empty, exceeds
	// length, or contains unsafe characters (NUL, leading slash, ".." traversal).
	ErrStorageKeyInvalida = apperror.NewValidation(
		"pago_imagen_storage_key_invalida",
		"clave de almacenamiento inválida",
	)

	// ErrSizeBytesNegativo is returned for negative file sizes.
	ErrSizeBytesNegativo = apperror.NewValidation(
		"pago_imagen_size_bytes_negativo",
		"tamaño de archivo inválido",
	)

	// ErrImagenDescripcionDemasiadoLarga is returned when imagen descripcion
	// exceeds the column limit.
	ErrImagenDescripcionDemasiadoLarga = apperror.NewValidation(
		"pago_imagen_descripcion_demasiado_larga",
		"descripción de imagen demasiado larga",
	)

	// ErrStringUnsafeChars is returned by validateSafeChars when a string
	// contains characters that would corrupt persistence.
	ErrStringUnsafeChars = apperror.NewValidation(
		"pago_string_caracteres_invalidos",
		"el texto contiene caracteres no permitidos",
	)

	// ErrImagenNoEncontrada is returned when a child Imagen lookup by ID misses.
	ErrImagenNoEncontrada = apperror.NewNotFound(
		"pago_imagen_no_encontrada",
		"imagen no encontrada",
	)

	// ErrImagenIDDuplicado is returned when CrearPagoConImagenes receives the
	// same imagen UUID more than once in the same request. Client must
	// deduplicate before retrying.
	ErrImagenIDDuplicado = apperror.NewValidation(
		"pago_imagen_id_duplicado",
		"el id de imagen aparece más de una vez en la solicitud",
	)

	// ─── PagoRecibido aggregate ────────────────────────────────────────────.

	// ErrPagoIDRequerido is returned when the client UUID is the zero value.
	ErrPagoIDRequerido = apperror.NewValidation(
		"pago_id_requerido",
		"el id del pago es obligatorio",
	)

	// ErrPagoYaExiste is returned when CrearPago receives a UUID that already
	// exists in MSP_PAGOS_RECIBIDOS (idempotency collision).
	ErrPagoYaExiste = apperror.NewConflict(
		"pago_ya_existe",
		"el pago con ese id ya existe",
	)

	// ErrPagoYaAplicado is returned when AplicarPago is called on a pago that
	// is already SincronizacionAplicada.
	ErrPagoYaAplicado = apperror.NewConflict(
		"pago_ya_aplicado",
		"el pago ya fue aplicado",
	)

	// ErrPagoImporteInvalido is returned when the importe is non-positive.
	ErrPagoImporteInvalido = apperror.NewValidation(
		"pago_importe_invalido",
		"el importe del pago debe ser mayor a cero",
	)

	// ErrPagoCargoIDInvalido is returned when CargoDoctoCCID is non-positive.
	ErrPagoCargoIDInvalido = apperror.NewValidation(
		"pago_cargo_id_invalido",
		"el id del cargo es inválido",
	)

	// ErrPagoClienteIDInvalido is returned when ClienteID is non-positive.
	ErrPagoClienteIDInvalido = apperror.NewValidation(
		"pago_cliente_id_invalido",
		"el id del cliente es inválido",
	)

	// ErrPagoCobradorIDInvalido is returned when CobradorID is non-positive.
	ErrPagoCobradorIDInvalido = apperror.NewValidation(
		"pago_cobrador_id_invalido",
		"el id del cobrador es inválido",
	)

	// ErrPagoCobradorRequerido is returned when the cobrador name is blank.
	ErrPagoCobradorRequerido = apperror.NewValidation(
		"pago_cobrador_requerido",
		"el nombre del cobrador es obligatorio",
	)

	// ErrPagoCobradorDemasiadoLargo is returned when the cobrador name exceeds
	// the column width.
	ErrPagoCobradorDemasiadoLargo = apperror.NewValidation(
		"pago_cobrador_demasiado_largo",
		"el nombre del cobrador es demasiado largo",
	)

	// ErrPagoFormaCobroInvalida is returned when FormaCobroID is non-positive.
	ErrPagoFormaCobroInvalida = apperror.NewValidation(
		"pago_forma_cobro_invalida",
		"la forma de cobro es inválida",
	)

	// ErrPagoLatLonInvalida is returned when lat/lon exceed their column widths.
	ErrPagoLatLonInvalida = apperror.NewValidation(
		"pago_lat_lon_invalida",
		"coordenadas inválidas",
	)

	// ErrPagoCargoNoEncontrado is returned when the cargo to abonar does not
	// exist (no row in MSP_SALDOS_VENTAS).
	ErrPagoCargoNoEncontrado = apperror.NewValidation(
		"pago_cargo_no_encontrado",
		"el cargo a abonar no existe",
	)

	// ErrPagoCargoCancelado is returned when the cargo to abonar was cancelled
	// in Microsip.
	ErrPagoCargoCancelado = apperror.NewValidation(
		"pago_cargo_cancelado",
		"el cargo a abonar fue cancelado",
	)

	// ErrPagoSaldoInsuficiente is returned when importe > cargo's saldo (defense
	// against double collection from multiple devices).
	ErrPagoSaldoInsuficiente = apperror.NewValidation(
		"pago_saldo_insuficiente",
		"el importe excede el saldo del cargo",
	)

	// ErrPagoFechaFutura is returned when FechaHoraPago is more than 5 minutes
	// ahead of the server clock.
	ErrPagoFechaFutura = apperror.NewValidation(
		"pago_fecha_futura",
		"la fecha del pago no puede ser futura",
	)

	// ErrPagoFechaMuyAntigua is returned when FechaHoraPago is more than 30
	// days behind the server clock.
	ErrPagoFechaMuyAntigua = apperror.NewValidation(
		"pago_fecha_muy_antigua",
		"la fecha del pago excede el horizonte permitido",
	)

	// ErrPagoDoctoCCIDInvalido is returned by MarcarAplicada when the
	// Microsip DOCTO_CC_ID resolved by the writer is non-positive.
	ErrPagoDoctoCCIDInvalido = apperror.NewValidation(
		"pago_docto_cc_id_invalido",
		"docto_cc_id resultante inválido",
	)

	// ErrPagoImpteDoctoCCIDInvalido is returned by MarcarAplicada when the
	// Microsip IMPTE_DOCTO_CC_ID resolved by the writer is non-positive.
	ErrPagoImpteDoctoCCIDInvalido = apperror.NewValidation(
		"pago_impte_docto_cc_id_invalido",
		"impte_docto_cc_id resultante inválido",
	)

	// ErrPagoFolioRequerido is returned by MarcarAplicada when the folio is
	// blank.
	ErrPagoFolioRequerido = apperror.NewValidation(
		"pago_folio_requerido",
		"el folio resultante es obligatorio",
	)

	// ErrPagoFolioDemasiadoLargo is returned by MarcarAplicada when the folio
	// exceeds the column width.
	ErrPagoFolioDemasiadoLargo = apperror.NewValidation(
		"pago_folio_demasiado_largo",
		"el folio resultante es demasiado largo",
	)
)
