//nolint:misspell // wire vocabulary is Spanish per project convention (Parametros, etc.).
package app

import (
	"context"
	"errors"

	canaldomain "github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// Outbound message tipo values EnviarSalienteParams.Tipo accepts. Wire
// values, not Go-idiomatic English, because they travel verbatim in
// canalhttp's JSON DTO and app owns that vocabulary now (see saliente.go's
// callers in internal/canal/infra/canalhttp/handlers.go).
const (
	TipoSalienteTexto     = "texto"
	TipoSalientePlantilla = "plantilla"
)

// EnviarSalienteParams groups the inputs to Service.EnviarSaliente: one
// outbound WhatsApp message to send on the store's behalf, selected by
// Tipo. Exactly one of Texto/Plantilla is read.
type EnviarSalienteParams struct {
	Destinatario string
	Tipo         string
	Texto        string
	Plantilla    *SalientePlantilla
}

// SalientePlantilla is a template-message invocation: the template's
// registered name, the approved language code, and positional body
// parameters. Mirrors internal/platform/whatsapp.Template's shape without
// forcing every caller (canalhttp's DTO layer included) to import that
// package just to build one.
type SalientePlantilla struct {
	Nombre     string
	Idioma     string
	Parametros []string
}

// EnviarSaliente sends one outbound WhatsApp message — text or template —
// on the store's behalf through the Sender port, and translates the
// outcome into canal's own domain sentinels (internal/canal/domain's
// Saliente* errors) so callers never need to know about
// internal/platform/whatsapp's error taxonomy — canalhttp's handler maps
// whatever this returns with the same mapAppError it already uses for
// everything else. Returns the resulting wamid on success.
func (s *Service) EnviarSaliente(ctx context.Context, p EnviarSalienteParams) (string, error) {
	if p.Destinatario == "" {
		return "", canaldomain.ErrSalienteDestinatarioRequerido
	}
	switch p.Tipo {
	case TipoSalienteTexto:
		return s.enviarTexto(ctx, p)
	case TipoSalientePlantilla:
		return s.enviarPlantilla(ctx, p)
	default:
		return "", canaldomain.ErrSalienteTipoInvalido
	}
}

// enviarTexto sends a free-form text message. p.Texto must be non-empty.
func (s *Service) enviarTexto(ctx context.Context, p EnviarSalienteParams) (string, error) {
	if p.Texto == "" {
		return "", canaldomain.ErrSalienteTextoRequerido
	}
	wamid, err := s.sender.SendText(ctx, p.Destinatario, p.Texto)
	if err != nil {
		return "", classifySenderError(err)
	}
	return wamid, nil
}

// enviarPlantilla sends a template message. p.Plantilla must be non-nil.
func (s *Service) enviarPlantilla(ctx context.Context, p EnviarSalienteParams) (string, error) {
	if p.Plantilla == nil {
		return "", canaldomain.ErrSalientePlantillaRequerida
	}
	wamid, err := s.sender.SendTemplate(ctx, p.Destinatario, whatsapp.Template{
		Name:         p.Plantilla.Nombre,
		LanguageCode: p.Plantilla.Idioma,
		BodyParams:   p.Plantilla.Parametros,
	})
	if err != nil {
		return "", classifySenderError(err)
	}
	return wamid, nil
}

// classifySenderError translates internal/platform/whatsapp's sentinel
// errors — whatever the Sender port's real (production) implementation
// returns — into canal's own domain sentinels. Anything not recognized,
// including a plain unclassified error, is returned unchanged: canalhttp's
// mapAppError already falls through to a 500 for any non-apperror error,
// exactly the behaviour this preserves.
func classifySenderError(err error) error {
	switch {
	case errors.Is(err, whatsapp.ErrWindowClosed):
		return canaldomain.ErrSalienteVentanaCerrada
	case errors.Is(err, whatsapp.ErrInvalidNumber):
		return canaldomain.ErrSalienteNumeroInvalido
	case errors.Is(err, whatsapp.ErrRateLimited):
		return canaldomain.ErrSalienteLimiteTasa
	case errors.Is(err, whatsapp.ErrWhatsAppDisabled):
		return canaldomain.ErrSalienteCanalDeshabilitado
	case whatsapp.IsTransient(err):
		return canaldomain.ErrSalienteEnvioFallido
	default:
		return err
	}
}
