package canalhttp

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors this package returns to internal-API callers. Codes in
// English snake_case, messages in Spanish lowercase without a trailing
// period (CLAUDE.md §3), matching internal/canal/domain/errors.go.
var (
	// ErrSalienteDestinatarioRequerido is returned when the outbound
	// message's destinatario is empty.
	ErrSalienteDestinatarioRequerido = apperror.NewValidation(
		"canal_outbound_message_recipient_required",
		"el destinatario del mensaje es obligatorio",
	)

	// ErrSalienteTipoInvalido is returned when tipo is neither "texto" nor
	// "plantilla".
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
)
