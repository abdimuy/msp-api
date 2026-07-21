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

	// ErrEstadoMensajeInvalido is returned when a string cannot be parsed as an
	// EstadoMensaje.
	ErrEstadoMensajeInvalido = apperror.NewValidation(
		"reactivacion_estado_mensaje_invalido",
		"el estado del mensaje no es válido",
	)

	// ErrSenderKindInvalido is returned when a string cannot be parsed as a
	// SenderKind.
	ErrSenderKindInvalido = apperror.NewValidation(
		"reactivacion_sender_kind_invalido",
		"el tipo de enviador no es válido",
	)

	// ErrMensajeClienteIDInvalido is returned when a Mensaje's clienteID <= 0.
	ErrMensajeClienteIDInvalido = apperror.NewValidation(
		"reactivacion_mensaje_cliente_id_invalido",
		"el id de cliente del mensaje debe ser mayor a cero",
	)

	// ErrMensajeTelefonoRequerido is returned when a Mensaje's telefono is empty.
	ErrMensajeTelefonoRequerido = apperror.NewValidation(
		"reactivacion_mensaje_telefono_requerido",
		"el teléfono del mensaje es obligatorio",
	)

	// ErrMensajeCuerpoRequerido is returned when a Mensaje's cuerpo is empty.
	ErrMensajeCuerpoRequerido = apperror.NewValidation(
		"reactivacion_mensaje_cuerpo_requerido",
		"el cuerpo del mensaje es obligatorio",
	)

	// ErrMensajeTransicionInvalida is returned when MarcarEnviado is called on a
	// Mensaje that is not in EstadoEncolado.
	ErrMensajeTransicionInvalida = apperror.NewValidation(
		"reactivacion_mensaje_transicion_invalida",
		"el mensaje no puede marcarse como enviado desde su estado actual",
	)
)
