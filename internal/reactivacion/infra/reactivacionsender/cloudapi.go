//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionsender

import (
	"context"
	"errors"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platformwhatsapp "github.com/abdimuy/msp-api/internal/platform/whatsapp"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Sentinel errors returned by CloudAPISender.Enviar. All are produced via
// apperror.New* so err113 is satisfied and the failure participates in the
// typed error model. Codes are snake_case English; messages are lowercase
// Spanish without a trailing period, per CLAUDE.md rule 3 — these strings
// are what MarcarFallido persists and an operator reads later.
//
// The three mapped codes carry Meta's numeric error code inline in the
// message text itself, rather than relying on it surviving inside the
// wrapped cause: that makes the distinction (retry later vs. never retry)
// legible even if only Error() is read, with no chain-walking required.
var (
	// ErrCloudAPIVentanaCerrada maps Meta code 131047: the 24h customer
	// service window is closed, so only a template message may be sent.
	// Permanent for a free-form send — retrying the same body will not
	// succeed until the customer writes again or the message is recomposed
	// as a template.
	ErrCloudAPIVentanaCerrada = apperror.NewValidation(
		"reactivacion_cloudapi_ventana_cerrada",
		"no se pudo enviar: la ventana de 24 horas con el cliente está cerrada (código de meta 131047)",
	)

	// ErrCloudAPINumeroInvalido maps Meta code 131026: the destination
	// number is invalid or has no WhatsApp account. Permanent — this
	// number should not be retried.
	ErrCloudAPINumeroInvalido = apperror.NewValidation(
		"reactivacion_cloudapi_numero_invalido",
		"no se pudo enviar: el número de destino no tiene whatsapp o es inválido (código de meta 131026)",
	)

	// ErrCloudAPILimiteExcedido maps Meta code 130429: Meta's own
	// application-level rate limit. Transient — safe to retry later.
	ErrCloudAPILimiteExcedido = apperror.NewInternal(
		"reactivacion_cloudapi_limite_excedido",
		"no se pudo enviar: se alcanzó el límite de envíos de whatsapp, reintentar más tarde (código de meta 130429)",
	)

	// ErrCloudAPIFalloEnvio is the fallback for any transport failure that
	// does not map to one of Meta's application error codes above (network
	// failure, timeout, HTTP error with no recognized code, disabled
	// channel).
	ErrCloudAPIFalloEnvio = apperror.NewInternal(
		"reactivacion_cloudapi_fallo_envio",
		"no se pudo enviar el mensaje por whatsapp",
	)
)

// CloudAPISender delivers reactivación messages through Meta's WhatsApp
// Cloud API via internal/platform/whatsapp — the real channel per
// docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md. Every message
// is sent as free-form text: reactivación's opener and copiloto bodies are
// composed on demand and are not pre-registered Meta templates.
type CloudAPISender struct {
	client platformwhatsapp.Client
	logger *slog.Logger
}

// NewCloudAPISender builds a CloudAPISender over client. A nil logger falls
// back to slog.Default().
func NewCloudAPISender(client platformwhatsapp.Client, logger *slog.Logger) *CloudAPISender {
	if logger == nil {
		logger = slog.Default()
	}
	return &CloudAPISender{client: client, logger: logger}
}

// Enviar sends cuerpo to dest.Telefono as a free-form WhatsApp text message.
// Returns nil only when Meta accepts the message and hands back a wamid —
// never for a queued or partial outcome. Any transport failure is mapped to
// one of the sentinels above and returned as an error; the wamid on success
// is logged (the audit trail linking this send to Meta's record) rather
// than returned, because outbound.MessageSender's signature (frozen by
// ADR-0010) carries no wamid.
func (s *CloudAPISender) Enviar(ctx context.Context, dest outbound.Destino, cuerpo string) error {
	wamid, err := s.client.SendText(ctx, dest.Telefono, cuerpo)
	if err != nil {
		return classifyCloudAPIError(err).
			WithSource("reactivacionsender.CloudAPISender").
			WithField("cliente_id", dest.ClienteID).
			WithField("telefono", dest.Telefono)
	}

	s.logger.InfoContext(
		ctx, "reactivacion_cloudapi_sender.enviar",
		slog.Int("cliente_id", dest.ClienteID),
		slog.String("telefono", dest.Telefono),
		slog.String("wamid", wamid),
	)
	return nil
}

// classifyCloudAPIError maps a platform/whatsapp transport error to the
// legible, Spanish, Meta-code-carrying sentinel that MarcarFallido
// persists. The original error is attached as cause so errors.Is against
// platformwhatsapp's own sentinels (and IsTransient) still works through
// the wrapped chain.
func classifyCloudAPIError(err error) *apperror.Error {
	switch {
	case errors.Is(err, platformwhatsapp.ErrWindowClosed):
		return ErrCloudAPIVentanaCerrada.WithError(err)
	case errors.Is(err, platformwhatsapp.ErrInvalidNumber):
		return ErrCloudAPINumeroInvalido.WithError(err)
	case errors.Is(err, platformwhatsapp.ErrRateLimited):
		return ErrCloudAPILimiteExcedido.WithError(err)
	default:
		return ErrCloudAPIFalloEnvio.WithError(err)
	}
}

// Kind identifies CloudAPISender as domain.SenderReal — the
// measurement-integrity tag that lets attribution count this send as a
// genuine customer contact.
func (s *CloudAPISender) Kind() domain.SenderKind { return domain.SenderReal }

var _ outbound.MessageSender = (*CloudAPISender)(nil)
