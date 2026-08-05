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
	// ErrMotivoSupresionInvalido is returned for any value other than "rebote".
	ErrMotivoSupresionInvalido = apperror.NewValidation(
		"receipt_suppression_reason_invalid",
		"motivo de supresión inválido",
	)
)
