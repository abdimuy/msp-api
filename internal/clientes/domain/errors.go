//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the clientes domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind → HTTPStatus)
// and the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per the project conventions.
var (
	// ErrClienteNotFound is returned when a cliente lookup misses.
	ErrClienteNotFound = apperror.NewNotFound(
		"cliente_not_found",
		"cliente no encontrado",
	)
	// ErrVentaNotFound is returned when a venta lookup misses.
	ErrVentaNotFound = apperror.NewNotFound(
		"venta_not_found",
		"venta no encontrada",
	)
	// ErrPagoNotFound is returned when a pago lookup misses.
	ErrPagoNotFound = apperror.NewNotFound(
		"pago_not_found",
		"pago no encontrado",
	)
	// ErrTipoVentaInvalido is returned by ParseTipoVenta when the supplied
	// string does not match a recognized TipoVenta constant.
	ErrTipoVentaInvalido = apperror.NewValidation(
		"tipo_venta_invalido",
		"el tipo de venta no es válido",
	)
)
