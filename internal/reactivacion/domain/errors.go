// Package domain holds the reactivación module's entities, value objects, and
// sentinel errors. It depends only on the standard library, uuid, decimal, and
// internal/platform/{audit,apperror}.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the reactivación domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind → HTTPStatus)
// and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without a
// trailing period, per project conventions (CLAUDE.md Rule 3).
var (
	// ErrSegmentoInvalido is returned when a string cannot be parsed as a Segmento.
	ErrSegmentoInvalido = apperror.NewValidation(
		"reactivacion_segmento_invalido",
		"el segmento de reactivación no es válido",
	)

	// ErrCohorteClienteIDInvalido is returned when clienteID <= 0.
	ErrCohorteClienteIDInvalido = apperror.NewValidation(
		"reactivacion_cohorte_cliente_id_invalido",
		"el id de cliente de la cohorte debe ser mayor a cero",
	)

	// ErrCohorteSaldoInvalido is returned when saldo < 0.
	ErrCohorteSaldoInvalido = apperror.NewValidation(
		"reactivacion_cohorte_saldo_invalido",
		"el saldo de la cohorte no puede ser negativo",
	)

	// ErrCohorteFechaInvalida is returned when cohorteFecha is zero.
	ErrCohorteFechaInvalida = apperror.NewValidation(
		"reactivacion_cohorte_fecha_invalida",
		"la fecha de cohorte es obligatoria",
	)
)
