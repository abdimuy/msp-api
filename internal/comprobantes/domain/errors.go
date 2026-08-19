//nolint:misspell // domain vocabulary is Spanish (descripción, cobro, etc.) per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the comprobantes domain. All are produced via
// apperror.New* constructors so they participate in the typed error model
// (Kind → HTTPStatus) and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per the project conventions.
var (
	// ErrTipoComprobanteInvalido is returned for any value other than "venta"
	// or "pago".
	ErrTipoComprobanteInvalido = apperror.NewValidation(
		"receipt_type_invalid",
		"tipo de comprobante inválido",
	)
	// ErrEstadoEnvioInvalido is returned for any value outside the delivery
	// state machine.
	ErrEstadoEnvioInvalido = apperror.NewValidation(
		"receipt_delivery_state_invalid",
		"estado de envío inválido",
	)
	// ErrCanalInvalido is returned for any value other than "local" or
	// "whatsapp_business".
	ErrCanalInvalido = apperror.NewValidation(
		"receipt_channel_invalid",
		"canal de envío inválido",
	)

	// ComprobanteVenta content model errors.
	// ErrComprobanteVentaFolioRequerido is returned when the folio is empty.
	ErrComprobanteVentaFolioRequerido = apperror.NewValidation(
		"receipt_comprobante_venta_folio_requerido",
		"el folio es obligatorio",
	)
	// ErrComprobanteVentaClienteRequerido is returned when the client name is
	// empty.
	ErrComprobanteVentaClienteRequerido = apperror.NewValidation(
		"receipt_comprobante_venta_cliente_requerido",
		"el nombre del cliente es obligatorio",
	)
	// ErrComprobanteVentaTotalNegativo is returned when total is negative.
	ErrComprobanteVentaTotalNegativo = apperror.NewValidation(
		"receipt_comprobante_venta_total_negativo",
		"el total no puede ser negativo",
	)
	// ErrComprobanteVentaEngancheNegativo is returned when enganche is
	// negative.
	ErrComprobanteVentaEngancheNegativo = apperror.NewValidation(
		"receipt_comprobante_venta_enganche_negativo",
		"el enganche no puede ser negativo",
	)
	// ErrComprobanteVentaSaldoNegativo is returned when saldo is negative.
	ErrComprobanteVentaSaldoNegativo = apperror.NewValidation(
		"receipt_comprobante_venta_saldo_negativo",
		"el saldo no puede ser negativo",
	)
	// ErrComprobanteVentaSinArticulos is returned when a venta comprobante
	// carries no detail lines.
	ErrComprobanteVentaSinArticulos = apperror.NewValidation(
		"receipt_comprobante_venta_sin_articulos",
		"el comprobante de venta requiere al menos un artículo",
	)

	// ArticuloComprobante content model errors.
	// ErrArticuloDescripcionRequerida is returned when an article has an
	// empty description.
	ErrArticuloDescripcionRequerida = apperror.NewValidation(
		"receipt_articulo_descripcion_requerida",
		"la descripción del artículo es obligatoria",
	)
	// ErrArticuloCantidadNegativa is returned when an article quantity is
	// negative.
	ErrArticuloCantidadNegativa = apperror.NewValidation(
		"receipt_articulo_cantidad_negativa",
		"la cantidad del artículo no puede ser negativa",
	)
	// ErrArticuloPrecioUnitarioNegativo is returned when an article unit
	// price is negative.
	ErrArticuloPrecioUnitarioNegativo = apperror.NewValidation(
		"receipt_articulo_precio_unitario_negativo",
		"el precio unitario del artículo no puede ser negativo",
	)
	// ErrArticuloImporteNegativo is returned when an article line total is
	// negative.
	ErrArticuloImporteNegativo = apperror.NewValidation(
		"receipt_articulo_importe_negativo",
		"el importe del artículo no puede ser negativo",
	)

	// ComprobantePago content model errors.
	// ErrComprobantePagoFolioRequerido is returned when the folio is empty.
	ErrComprobantePagoFolioRequerido = apperror.NewValidation(
		"receipt_comprobante_pago_folio_requerido",
		"el folio es obligatorio",
	)
	// ErrComprobantePagoClienteRequerido is returned when the client name is
	// empty.
	ErrComprobantePagoClienteRequerido = apperror.NewValidation(
		"receipt_comprobante_pago_cliente_requerido",
		"el nombre del cliente es obligatorio",
	)
	// ErrComprobantePagoVentaFolioRequerido is returned when the source venta
	// folio is empty.
	ErrComprobantePagoVentaFolioRequerido = apperror.NewValidation(
		"receipt_comprobante_pago_venta_folio_requerido",
		"el folio de la venta es obligatorio",
	)
	// ErrComprobantePagoMontoNegativo is returned when monto is negative.
	ErrComprobantePagoMontoNegativo = apperror.NewValidation(
		"receipt_comprobante_pago_monto_negativo",
		"el monto no puede ser negativo",
	)
	// ErrComprobantePagoSaldoRestanteNegativo is returned when saldoRestante
	// is negative.
	ErrComprobantePagoSaldoRestanteNegativo = apperror.NewValidation(
		"receipt_comprobante_pago_saldo_restante_negativo",
		"el saldo restante no puede ser negativo",
	)

	// Envio entity errors.
	// ErrEnvioTransicionInvalido is returned when a state transition is not
	// allowed from the current estado. The state does NOT change.
	ErrEnvioTransicionInvalido = apperror.NewValidation(
		"receipt_envio_transicion_invalida",
		"la transición no es válida desde el estado actual",
	)
	// ErrEnvioReferenciaRequerido is returned when the referencia is empty.
	ErrEnvioReferenciaRequerido = apperror.NewValidation(
		"receipt_envio_referencia_requerido",
		"la referencia es obligatoria",
	)
)
