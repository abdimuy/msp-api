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

	// ErrEstadoConversacionInvalido is returned when a string cannot be parsed
	// as an EstadoConversacion.
	ErrEstadoConversacionInvalido = apperror.NewValidation(
		"estado_conversacion_invalido",
		"estado de conversación inválido",
	)

	// ErrSenalInvalido is returned when a string cannot be parsed as a Senal.
	ErrSenalInvalido = apperror.NewValidation(
		"senal_invalido",
		"la señal no es válida",
	)

	// ErrAccionInvalido is returned when a string cannot be parsed as an Accion.
	ErrAccionInvalido = apperror.NewValidation(
		"accion_invalido",
		"la acción no es válida",
	)

	// ErrAutorInvalido is returned when a string cannot be parsed as an Autor.
	ErrAutorInvalido = apperror.NewValidation(
		"autor_invalido",
		"el autor no es válido",
	)

	// ErrDireccionTurnoInvalido is returned when a string cannot be parsed as a
	// DireccionTurno.
	ErrDireccionTurnoInvalido = apperror.NewValidation(
		"direccion_turno_invalido",
		"la dirección del turno no es válida",
	)

	// ErrResultadoDecisionInvalido is returned when a string cannot be parsed
	// as a ResultadoDecision.
	ErrResultadoDecisionInvalido = apperror.NewValidation(
		"resultado_decision_invalido",
		"el resultado de la decisión no es válido",
	)

	// ErrConversacionTransicionInvalida is returned when a Conversacion
	// transition method is called from a state that does not allow it
	// (including any transition attempted from a terminal state).
	ErrConversacionTransicionInvalida = apperror.NewValidation(
		"conversacion_transicion_invalida",
		"la conversación no puede transicionar desde su estado actual",
	)

	// ErrTurnoCuerpoRequerido is returned when CrearTurno's cuerpo is empty.
	ErrTurnoCuerpoRequerido = apperror.NewValidation(
		"turno_cuerpo_requerido",
		"el cuerpo del turno es obligatorio",
	)

	// ErrDecisionConfianzaInvalida is returned when CrearDecision's confianza
	// is outside [0, 100].
	ErrDecisionConfianzaInvalida = apperror.NewValidation(
		"decision_confianza_invalida",
		"la confianza debe estar entre 0 y 100",
	)
)
