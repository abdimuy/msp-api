//nolint:misspell // domain vocabulary is Spanish (transición, etc.) per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors of the canal domain. Codes in English snake_case, messages
// in Spanish lowercase without a trailing period (CLAUDE.md §3).
var (
	// EstadoReenvio errors.

	// ErrEstadoReenvioInvalido is returned by ParseEstadoReenvio when the
	// input is not one of the three recognized states.
	ErrEstadoReenvioInvalido = apperror.NewValidation(
		"canal_estado_reenvio_invalido",
		"el estado de reenvío no es válido",
	)

	// MensajeEntrante required-field errors.

	// ErrMensajeEntranteWamidRequerido is returned when the wamid — Meta's
	// own message id and the mailbox's idempotency key — is empty.
	ErrMensajeEntranteWamidRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_wamid_requerido",
		"el wamid del mensaje es obligatorio",
	)

	// ErrMensajeEntranteWamidDemasiadoLargo is returned when the wamid
	// exceeds maxWamidLength codepoints.
	ErrMensajeEntranteWamidDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_wamid_demasiado_largo",
		"el wamid del mensaje es demasiado largo",
	)

	// ErrMensajeEntranteRemitenteRequerido is returned when the sender's
	// phone number is empty.
	ErrMensajeEntranteRemitenteRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_remitente_requerido",
		"el teléfono del remitente es obligatorio",
	)

	// ErrMensajeEntranteRemitenteDemasiadoLargo is returned when the
	// sender's phone number exceeds maxRemitenteLength codepoints.
	ErrMensajeEntranteRemitenteDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_remitente_demasiado_largo",
		"el teléfono del remitente es demasiado largo",
	)

	// ErrMensajeEntrantePhoneNumberIDRequerido is returned when Meta's
	// destination phone_number_id is empty.
	ErrMensajeEntrantePhoneNumberIDRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_phone_number_id_requerido",
		"el phone_number_id de destino es obligatorio",
	)

	// ErrMensajeEntrantePhoneNumberIDDemasiadoLargo is returned when
	// phone_number_id exceeds maxPhoneNumberIDLength codepoints.
	ErrMensajeEntrantePhoneNumberIDDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_phone_number_id_demasiado_largo",
		"el phone_number_id de destino es demasiado largo",
	)

	// ErrMensajeEntranteTipoRequerido is returned when the message type is
	// empty.
	ErrMensajeEntranteTipoRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_tipo_requerido",
		"el tipo de mensaje es obligatorio",
	)

	// ErrMensajeEntranteTipoDemasiadoLargo is returned when the message type
	// exceeds maxTipoLength codepoints.
	ErrMensajeEntranteTipoDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_tipo_demasiado_largo",
		"el tipo de mensaje es demasiado largo",
	)

	// ErrMensajeEntranteContenidoDemasiadoLargo is returned when the message
	// body/media reference exceeds maxContenidoLength codepoints.
	ErrMensajeEntranteContenidoDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_contenido_demasiado_largo",
		"el contenido del mensaje es demasiado largo",
	)

	// ErrMensajeEntranteCaracteresInvalidos is returned when a text field
	// carries invalid UTF-8, a NUL byte, or another unsafe control
	// character.
	ErrMensajeEntranteCaracteresInvalidos = apperror.NewValidation(
		"canal_mensaje_entrante_caracteres_invalidos",
		"el mensaje contiene caracteres no válidos",
	)

	// ErrMensajeEntranteTimestampMetaRequerido is returned when Meta's own
	// message timestamp is the zero value.
	ErrMensajeEntranteTimestampMetaRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_timestamp_meta_requerido",
		"el timestamp de meta es obligatorio",
	)

	// ErrMensajeEntranteRecibidoEnRequerido is returned when the VPS
	// receipt timestamp is the zero value.
	ErrMensajeEntranteRecibidoEnRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_recibido_en_requerido",
		"la fecha de recepción es obligatoria",
	)

	// MensajeEntrante transition and failure-reason errors.

	// ErrMensajeEntranteTransicionInvalida is returned when a transition
	// method is called from a state that does not allow it — see
	// validEstadoReenvioTransitions.
	ErrMensajeEntranteTransicionInvalida = apperror.NewValidation(
		"canal_mensaje_entrante_transicion_invalida",
		"la transición de estado de reenvío no es válida",
	)

	// ErrMensajeEntranteMotivoFalloRequerido is returned by MarcarFallido
	// when motivo is empty — an unreadable failure reason defeats its
	// purpose, since a human reads it.
	ErrMensajeEntranteMotivoFalloRequerido = apperror.NewValidation(
		"canal_mensaje_entrante_motivo_fallo_requerido",
		"el motivo del fallo es obligatorio",
	)

	// ErrMensajeEntranteMotivoFalloDemasiadoLargo is returned when motivo
	// exceeds maxMotivoFalloLength codepoints.
	ErrMensajeEntranteMotivoFalloDemasiadoLargo = apperror.NewValidation(
		"canal_mensaje_entrante_motivo_fallo_demasiado_largo",
		"el motivo del fallo es demasiado largo",
	)
)
