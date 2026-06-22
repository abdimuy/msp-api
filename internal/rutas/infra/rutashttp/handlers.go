//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// Handlers holds the rutas HTTP handlers.
type Handlers struct {
	svc *rutasapp.Service
}

// NewHandlers builds a Handlers wired against the given service.
func NewHandlers(svc *rutasapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ListarRutas handles GET /rutas. Requires auth.PermRutasLeer.
func (h *Handlers) ListarRutas(ctx context.Context, _ *ListRutasInput) (*ListRutasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	rutas, err := h.svc.ListarRutas(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListRutasOutput{}
	out.Body.Items = toRutaResumenDTOs(rutas)
	return out, nil
}

func toRutaResumenDTOs(rutas []rutasdomain.RutaResumen) []RutaResumenDTO {
	if rutas == nil {
		return []RutaResumenDTO{}
	}
	dtos := make([]RutaResumenDTO, len(rutas))
	for i, r := range rutas {
		dtos[i] = RutaResumenDTO{
			ZonaID:         r.ZonaID,
			ZonaNombre:     r.ZonaNombre,
			CobradorID:     r.CobradorID,
			CobradorNombre: r.CobradorNombre,
			NumClientes:    r.NumClientes,
			SaldoTotal:     r.SaldoTotal.StringFixed(2),
		}
	}
	return dtos
}
