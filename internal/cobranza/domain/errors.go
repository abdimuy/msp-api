// Package domain holds the cobranza module's value objects and sentinel
// errors. It depends only on the standard library and the platform/apperror
// package — never on app, infra, or other modules.
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
)
