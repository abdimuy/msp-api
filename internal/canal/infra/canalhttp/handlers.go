//nolint:misspell // wire vocabulary is Spanish per project convention (Parametros, etc.).
package canalhttp

import (
	"context"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
)

// Handlers holds the canal module's internal Huma API handlers. Matches
// the gold-standard shape (docs/module-standards/08-handlers-routes.md's
// checklist: "Struct Handlers solo tiene svc"): every capability
// Salud/CrearSaliente need — the mailbox backlog count, the outbound send
// path — now lives on canalapp.Service itself.
type Handlers struct {
	svc *canalapp.Service
}

// NewHandlers builds a Handlers wired against svc.
func NewHandlers(svc *canalapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// Salud handles GET /canal/v1/salud. See registerSalud for why this route
// carries no authentication and SaludDTO for what it may never leak.
func (h *Handlers) Salud(ctx context.Context, _ *SaludInput) (*SaludOutput, error) {
	pendientes, err := h.svc.ContarPendientes(ctx)
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
// runs. All the text-vs-template decision and Meta-error classification
// that used to live here now lives in canalapp.Service.EnviarSaliente —
// this handler only translates the wire DTO in, and the resulting
// domain-sentinel error out through the same mapAppError every other
// handler in this module uses.
func (h *Handlers) CrearSaliente(ctx context.Context, in *CrearSalienteInput) (*CrearSalienteOutput, error) {
	wamid, err := h.svc.EnviarSaliente(ctx, toEnviarSalienteParams(in.Body))
	if err != nil {
		return nil, mapAppError(err)
	}
	out := &CrearSalienteOutput{}
	out.Body.Wamid = wamid
	return out, nil
}

// toEnviarSalienteParams converts the wire DTO into the app-layer params
// type. A nil Body.Plantilla stays nil — EnviarSaliente itself rejects that
// combination with canaldomain.ErrSalientePlantillaRequerida when
// Tipo="plantilla".
func toEnviarSalienteParams(body CrearSalienteBody) canalapp.EnviarSalienteParams {
	p := canalapp.EnviarSalienteParams{
		Destinatario: body.Destinatario,
		Tipo:         body.Tipo,
		Texto:        body.Texto,
	}
	if body.Plantilla != nil {
		p.Plantilla = &canalapp.SalientePlantilla{
			Nombre:     body.Plantilla.Nombre,
			Idioma:     body.Plantilla.Idioma,
			Parametros: body.Plantilla.Parametros,
		}
	}
	return p
}
