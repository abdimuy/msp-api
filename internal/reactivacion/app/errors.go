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

	// ErrTelefonoInvalido is returned by ResolverClienteIDPorTelefono when
	// telefono has fewer than telefonoSuffixLen digits — too short to be a
	// Mexican national number, so no lookup is even attempted.
	ErrTelefonoInvalido = apperror.NewValidation(
		"reactivacion_telefono_invalido",
		"el teléfono no tiene dígitos suficientes para identificarlo",
	)

	// ErrTelefonoNoResuelto is returned by ResolverClienteIDPorTelefono when
	// telefono matches no row in MSP_RX_COHORTE — either a number the
	// piloto never contacted, or a real cliente who is simply not in the
	// cohorte. The caller must not silently drop the inbound message on
	// this error: it stays in the VPS's durable mailbox (EstadoReenvioFallido)
	// for later investigation.
	ErrTelefonoNoResuelto = apperror.NewNotFound(
		"reactivacion_telefono_no_resuelto",
		"no se encontró un cliente de la cohorte con este teléfono",
	)

	// ErrTelefonoAmbiguo is returned by ResolverClienteIDPorTelefono when
	// telefono matches more than one MSP_RX_COHORTE row — e.g. a shared
	// household landline in DIRS_CLIENTES. Routing to either cliente_id
	// would silently misattribute the conversation, so this is treated the
	// same as "not resolved" rather than guessed at.
	ErrTelefonoAmbiguo = apperror.NewConflict(
		"reactivacion_telefono_ambiguo",
		"el teléfono coincide con más de un cliente de la cohorte",
	)
)
