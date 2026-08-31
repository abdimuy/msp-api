//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
)

// tipoTextoEntrante is the only Meta message type this store-side route
// hands to the copiloto. Every other type (image, audio, document, video,
// sticker, location, button, interactive, ...) either carries no usable
// text or carries a media reference id that must never be treated as the
// cliente's words — see MensajeEntrante's doc comment.
const tipoTextoEntrante = "text"

// tiendaHandlers holds the store-side machine-to-machine handler. Kept as
// its own small struct — separate from Handlers, the Firebase-authenticated
// surface — so the two trust boundaries never share a struct, and so
// nothing about this one assumes a auth.CurrentUser is ever on the request
// context (it never is: tiendaTokenMiddleware is the only gate).
type tiendaHandlers struct {
	svc *reactivacionapp.Service
}

// MensajeEntrante handles POST /reactivacion/tienda/mensaje-entrante.
// tiendaTokenMiddleware (and, if configured, tiendaIPMiddleware) have
// already authenticated the caller as the VPS by the time this runs — see
// registerTiendaMensajeEntrante.
//
// It resolves in.Body.Remitente to a cliente_id against the piloto cohorte
// and, on success, hands the message to the SAME
// reactivacionapp.Service.ProcesarMensajeEntrante the Firebase-gated
// simulate endpoint (Handlers.MensajeEntrante in copiloto_handlers.go)
// already uses — no new business logic here, only a new way in for a real
// WhatsApp reply instead of an operator's simulated one.
func (h *tiendaHandlers) MensajeEntrante(
	ctx context.Context, in *TiendaMensajeEntranteInput,
) (*TiendaMensajeEntranteOutput, error) {
	if in.Body.Tipo != tipoTextoEntrante {
		return nil, huma.NewError(http.StatusUnprocessableEntity, "tipo de mensaje no soportado",
			&huma.ErrorDetail{Message: "sólo se procesan mensajes de texto en fase 3a"})
	}

	clienteID, err := h.svc.ResolverClienteIDPorTelefono(ctx, in.Body.Remitente, in.Body.Wamid)
	if err != nil {
		return nil, mapAppError(err)
	}

	res, err := h.svc.ProcesarMensajeEntrante(ctx, clienteID, in.Body.Contenido)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &TiendaMensajeEntranteOutput{}
	out.Body = toDecisionResultDTO(res)
	return out, nil
}
