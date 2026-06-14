// Package domain holds the analytics module's aggregate root entities,
// value objects, and sentinel errors. It depends only on the standard
// library, uuid, decimal, and internal/platform/{audit,apperror}.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the analytics domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind → HTTPStatus)
// and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without a
// trailing period, per project conventions (CLAUDE.md Rule 3).
//
// NOTE: Task 02 (value-objects-errors) extends this file with additional
// sentinels for other invariants and value objects in the analytics module.
var (
	// ErrWinbackCandidatoFrecuenciaInvalida is returned when frecuencia < 0.
	ErrWinbackCandidatoFrecuenciaInvalida = apperror.NewValidation(
		"winback_candidato_frecuencia_invalida",
		"la frecuencia del candidato winback no puede ser negativa",
	)

	// ErrWinbackCandidatoMontoInvalido is returned when monetary < 0.
	ErrWinbackCandidatoMontoInvalido = apperror.NewValidation(
		"winback_candidato_monto_invalido",
		"el monto del candidato winback no puede ser negativo",
	)

	// ErrWinbackCandidatoSaldoInvalido is returned when saldo < 0.
	ErrWinbackCandidatoSaldoInvalido = apperror.NewValidation(
		"winback_candidato_saldo_invalido",
		"el saldo del candidato winback no puede ser negativo",
	)

	// ErrWinbackCandidatoCohorteInvalida is returned when cohorteFecha is zero.
	ErrWinbackCandidatoCohorteInvalida = apperror.NewValidation(
		"winback_candidato_cohorte_invalida",
		"la fecha de cohorte del candidato winback es obligatoria",
	)
)
