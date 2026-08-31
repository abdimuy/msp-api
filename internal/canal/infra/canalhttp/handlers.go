//nolint:misspell // wire vocabulary is Spanish per project convention (Parametros, etc.).
package canalhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// PendienteCounter is the minimal capability GET /canal/v1/salud needs: the
// mailbox's backlog size. canaloutbound.BuzonRepo satisfies it structurally.
// Declared narrowly here — rather than depending on the whole port — so
// Handlers' dependency is exactly what it uses, mirroring the local
// TxRunner interface in internal/canal/app/service.go.
type PendienteCounter interface {
	ContarPendientes(ctx context.Context) (int, error)
}

// Handlers holds the canal module's internal Huma API handlers. Unlike the
// gold-standard shape (Handlers holding only a *{module}app.Service),
// canalapp.Service exposes neither a pending-count query nor a way to send
// an outbound WhatsApp message — Task 4 built it purely around the inbound
// mailbox — so Salud and CrearSaliente reach past it to the narrower
// capabilities they actually need instead.
type Handlers struct {
	pendientes PendienteCounter
	wa         whatsapp.Client
}

// NewHandlers builds a Handlers wired against its dependencies.
func NewHandlers(pendientes PendienteCounter, wa whatsapp.Client) *Handlers {
	return &Handlers{pendientes: pendientes, wa: wa}
}

// Salud handles GET /canal/v1/salud. See registerSalud for why this route
// carries no authentication and SaludDTO for what it may never leak.
func (h *Handlers) Salud(ctx context.Context, _ *SaludInput) (*SaludOutput, error) {
	pendientes, err := h.pendientes.ContarPendientes(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}
	out := &SaludOutput{}
	out.Body.Estado = "ok"
	out.Body.Pendientes = pendientes
	return out, nil
}

// CrearSaliente handles POST /canal/v1/salientes. sharedTokenMiddleware has
// already rejected any request whose token did not match by the time this
// runs.
func (h *Handlers) CrearSaliente(ctx context.Context, in *CrearSalienteInput) (*CrearSalienteOutput, error) {
	wamid, err := h.enviarSaliente(ctx, in.Body)
	if err != nil {
		return nil, mapWhatsAppError(err)
	}
	out := &CrearSalienteOutput{}
	out.Body.Wamid = wamid
	return out, nil
}

// enviarSaliente dispatches to the WhatsApp client by Body.Tipo.
func (h *Handlers) enviarSaliente(ctx context.Context, body CrearSalienteBody) (string, error) {
	if body.Destinatario == "" {
		return "", ErrSalienteDestinatarioRequerido
	}
	switch body.Tipo {
	case tipoSalienteTexto:
		if body.Texto == "" {
			return "", ErrSalienteTextoRequerido
		}
		return h.wa.SendText(ctx, body.Destinatario, body.Texto)
	case tipoSalientePlantilla:
		if body.Plantilla == nil {
			return "", ErrSalientePlantillaRequerida
		}
		return h.wa.SendTemplate(ctx, body.Destinatario, whatsapp.Template{
			Name:         body.Plantilla.Nombre,
			LanguageCode: body.Plantilla.Idioma,
			BodyParams:   body.Plantilla.Parametros,
		})
	default:
		return "", ErrSalienteTipoInvalido
	}
}

// mapWhatsAppError translates internal/platform/whatsapp's sentinel errors
// into the HTTP status a caller of POST /canal/v1/salientes can act on,
// before falling back to mapAppError for anything else (including a plain
// apperror.Error, which enviarSaliente's own validation returns).
func mapWhatsAppError(err error) error {
	switch {
	case errors.Is(err, whatsapp.ErrWindowClosed):
		return huma.Error409Conflict("la ventana de 24 horas para mensajes libres está cerrada")
	case errors.Is(err, whatsapp.ErrInvalidNumber):
		return huma.Error422UnprocessableEntity("el número de destino no es válido o no tiene whatsapp")
	case errors.Is(err, whatsapp.ErrRateLimited):
		return huma.Error429TooManyRequests("whatsapp aplicó un límite de tasa, reintente más tarde")
	case errors.Is(err, whatsapp.ErrWhatsAppDisabled):
		return huma.NewError(http.StatusServiceUnavailable, "el canal de whatsapp está deshabilitado")
	case whatsapp.IsTransient(err):
		return huma.NewError(http.StatusServiceUnavailable, "el envío falló temporalmente, reintente más tarde")
	default:
		return mapAppError(err)
	}
}
