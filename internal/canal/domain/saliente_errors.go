package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Saliente (outbound) message errors — canal's own classification of what
// can go wrong sending one outbound WhatsApp message through
// outbound.Sender. app.Service.EnviarSaliente is the only place that
// produces these; canalhttp's handler never inspects an outbound-send
// error directly, it only calls mapAppError, exactly as it does for every
// other error apperror.Kind already knows how to translate.
//
// These sentinels are canal's own, deliberately independent of
// internal/platform/whatsapp's error taxonomy (ErrWindowClosed,
// ErrInvalidNumber, ErrRateLimited, ErrWhatsAppDisabled): domain must not
// import a transport package (see doc.go), so app.classifySenderError
// translates from one taxonomy to the other. Codes in English snake_case,
// messages in Spanish lowercase without a trailing period (CLAUDE.md §3),
// matching errors.go.
var (
	// ErrSalienteDestinatarioRequerido is returned when the outbound
	// message's destinatario is empty.
	ErrSalienteDestinatarioRequerido = apperror.NewValidation(
		"canal_outbound_message_recipient_required",
		"el destinatario del mensaje es obligatorio",
	)

	// ErrSalienteTipoInvalido is returned when the outbound message's tipo
	// is neither "texto" nor "plantilla".
	ErrSalienteTipoInvalido = apperror.NewValidation(
		"canal_outbound_message_type_invalid",
		"el tipo de mensaje saliente no es válido",
	)

	// ErrSalienteTextoRequerido is returned when tipo="texto" but texto is
	// empty.
	ErrSalienteTextoRequerido = apperror.NewValidation(
		"canal_outbound_message_text_required",
		"el texto del mensaje es obligatorio para tipo=texto",
	)

	// ErrSalientePlantillaRequerida is returned when tipo="plantilla" but
	// plantilla is absent.
	ErrSalientePlantillaRequerida = apperror.NewValidation(
		"canal_outbound_message_template_required",
		"los datos de la plantilla son obligatorios para tipo=plantilla",
	)

	// ErrSalienteVentanaCerrada is returned when WhatsApp's 24-hour
	// customer-service window is closed and only a template message may be
	// sent — canal's own mirror of
	// internal/platform/whatsapp.ErrWindowClosed.
	ErrSalienteVentanaCerrada = apperror.NewConflict(
		"canal_outbound_message_window_closed",
		"la ventana de 24 horas para mensajes libres está cerrada",
	)

	// ErrSalienteNumeroInvalido is returned when the destination number is
	// invalid or has no WhatsApp account — canal's own mirror of
	// internal/platform/whatsapp.ErrInvalidNumber.
	ErrSalienteNumeroInvalido = apperror.NewValidation(
		"canal_outbound_message_invalid_number",
		"el número de destino no es válido o no tiene whatsapp",
	)

	// ErrSalienteLimiteTasa is returned when WhatsApp applied its own
	// application-level rate limit — canal's own mirror of
	// internal/platform/whatsapp.ErrRateLimited.
	ErrSalienteLimiteTasa = apperror.NewTooManyRequests(
		"canal_outbound_message_rate_limited",
		"whatsapp aplicó un límite de tasa, reintente más tarde",
	)

	// ErrSalienteCanalDeshabilitado is returned when the WhatsApp channel
	// is disabled (WHATSAPP_ENABLED=false) — canal's own mirror of
	// internal/platform/whatsapp.ErrWhatsAppDisabled.
	ErrSalienteCanalDeshabilitado = apperror.NewServiceUnavailable(
		"canal_outbound_message_channel_disabled",
		"el canal de whatsapp está deshabilitado",
	)

	// ErrSalienteEnvioFallido is returned for any other transient send
	// failure (network/timeout, WhatsApp 5xx) — safe for the caller to
	// retry.
	ErrSalienteEnvioFallido = apperror.NewServiceUnavailable(
		"canal_outbound_message_send_failed",
		"el envío falló temporalmente, reintente más tarde",
	)
)
