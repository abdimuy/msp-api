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
)
