//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the reactivación app layer (the copiloto's deterministic
// policy, inbound loop, and operator actions). All are produced via
// apperror.New* constructors so they participate in the typed error model.
//
// Error codes are snake_case English; messages are lowercase Spanish without a
// trailing period, per project conventions (CLAUDE.md Rule 3).
var (
	// ErrMensajeEntranteVacio is returned by ProcesarMensajeEntrante when the
	// inbound cliente message is empty or whitespace-only.
	ErrMensajeEntranteVacio = apperror.NewValidation(
		"reactivacion_mensaje_entrante_vacio",
		"el mensaje entrante no puede estar vacío",
	)

	// ErrNoHayBorradorPendiente is returned by AprobarBorrador/EditarYAprobar
	// when the cliente's newest decision is not a pending draft (accion
	// responder, resultado propuesto) — including the idempotent case where a
	// previous approval already consumed it.
	ErrNoHayBorradorPendiente = apperror.NewValidation(
		"reactivacion_no_hay_borrador_pendiente",
		"no hay un borrador pendiente para este cliente",
	)

	// ErrClienteSinDatosContacto is returned by AprobarBorrador/EditarYAprobar
	// when ClienteFactsReader has no row for the cliente (no telefono to send to).
	ErrClienteSinDatosContacto = apperror.NewValidation(
		"reactivacion_cliente_sin_datos_contacto",
		"el cliente no tiene datos de contacto",
	)

	// ErrTextoEditadoVacio is returned by EditarYAprobar when the operator's
	// edited text is empty or whitespace-only.
	ErrTextoEditadoVacio = apperror.NewValidation(
		"reactivacion_texto_editado_vacio",
		"el texto editado no puede estar vacío",
	)

	// ErrIntencionVacia is returned by Dictar when the operator's stated intent
	// is empty or whitespace-only.
	ErrIntencionVacia = apperror.NewValidation(
		"reactivacion_intencion_vacia",
		"la intención dictada no puede estar vacía",
	)

	// ErrConversacionNoEncontrada is returned by Escalar/Dictar/ObtenerConversacion
	// when the cliente has no Conversacion yet.
	ErrConversacionNoEncontrada = apperror.NewNotFound(
		"reactivacion_conversacion_no_encontrada",
		"la conversación no existe",
	)
)
