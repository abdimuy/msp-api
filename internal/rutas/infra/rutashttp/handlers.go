//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"
	"time"

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

// ListarReporteUsuarios handles GET /rutas/reporte-usuarios. Requires auth.PermRutasLeer.
// One row per active cobrador (user), each evaluated over its own window.
func (h *Handlers) ListarReporteUsuarios(ctx context.Context, _ *ListReporteUsuariosInput) (*ListReporteUsuariosOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	usuarios, err := h.svc.ListarReporteUsuarios(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListReporteUsuariosOutput{}
	out.Body.Items = toReporteUsuarioDTOs(usuarios)
	return out, nil
}

func toReporteUsuarioDTOs(usuarios []rutasdomain.ReporteUsuario) []ReporteUsuarioDTO {
	if usuarios == nil {
		return []ReporteUsuarioDTO{}
	}
	dtos := make([]ReporteUsuarioDTO, len(usuarios))
	for i, u := range usuarios {
		dtos[i] = ReporteUsuarioDTO{
			UID:               u.UID,
			Nombre:            u.Nombre,
			Email:             u.Email,
			CobradorID:        u.CobradorID,
			ZonaID:            u.ZonaID,
			ZonaNombre:        u.ZonaNombre,
			NumClientes:       u.NumClientes,
			SaldoTotal:        u.SaldoTotal.StringFixed(2),
			CoberturaNum:      u.CoberturaNum,
			CoberturaDen:      u.CoberturaDen,
			PonderadoDen:      u.PonderadoDen,
			FechaInicioSemana: u.FechaInicio.UTC().Format(time.RFC3339),
		}
		if u.PctCoberturaSemanal != nil {
			s := u.PctCoberturaSemanal.StringFixed(2)
			dtos[i].PctCoberturaSemanal = &s
		}
		if u.PctPonderadoSemanal != nil {
			s := u.PctPonderadoSemanal.StringFixed(2)
			dtos[i].PctPonderadoSemanal = &s
		}
	}
	return dtos
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
		if r.PctCoberturaSemanal != nil {
			s := r.PctCoberturaSemanal.StringFixed(2)
			dtos[i].PctCoberturaSemanal = &s
		}
		if r.PctPonderadoSemanal != nil {
			s := r.PctPonderadoSemanal.StringFixed(2)
			dtos[i].PctPonderadoSemanal = &s
		}
		if r.FechaInicioSemana != nil {
			s := r.FechaInicioSemana.UTC().Format(time.RFC3339)
			dtos[i].FechaInicioSemana = &s
		}
	}
	return dtos
}
