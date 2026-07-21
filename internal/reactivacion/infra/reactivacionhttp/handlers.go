//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	reactivaciondomain "github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// Handlers holds the reactivación HTTP handlers.
type Handlers struct {
	svc *reactivacionapp.Service
}

// NewHandlers builds a Handlers wired against the given service.
func NewHandlers(svc *reactivacionapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ListCohorte handles GET /reactivacion/cohorte. Requires auth.PermReactivacionLeer.
func (h *Handlers) ListCohorte(ctx context.Context, in *ListCohorteInput) (*ListCohorteOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionLeer); err != nil {
		return nil, err
	}

	cohorte, err := h.svc.ListarCohorte(ctx, reactivacionapp.ListarCohorteParams{
		Segmento:        in.Segmento,
		SoloTratamiento: in.SoloTratamiento,
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListCohorteOutput{}
	out.Body.Items = toCohorteDTOs(cohorte)
	return out, nil
}

// Atribucion handles GET /reactivacion/atribucion. Requires auth.PermReactivacionLeer.
func (h *Handlers) Atribucion(ctx context.Context, _ *AtribucionInput) (*AtribucionOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionLeer); err != nil {
		return nil, err
	}

	res, err := h.svc.Atribucion(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &AtribucionOutput{}
	out.Body = AtribucionDTO{
		TreatmentTotal:       res.TreatmentTotal,
		TreatmentConvertidos: res.TreatmentConvertidos,
		ControlTotal:         res.ControlTotal,
		ControlConvertidos:   res.ControlConvertidos,
		TasaTreatment:        res.TasaTreatment.StringFixed(4),
		TasaControl:          res.TasaControl.StringFixed(4),
		Uplift:               res.Uplift.StringFixed(4),
	}
	return out, nil
}

// Construir handles POST /reactivacion/cohorte/construir. Requires
// auth.PermReactivacionAdministrar. It launches the rebuild in the background and
// returns 202 immediately.
func (h *Handlers) Construir(ctx context.Context, _ *ConstruirInput) (*ConstruirOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	out := &ConstruirOutput{}
	//nolint:contextcheck // intentional: ConstruirEnSegundoPlano uses context.Background() internally
	if h.svc.ConstruirEnSegundoPlano() {
		out.Body.Status = "aceptado"
		out.Body.Mensaje = "construcción de la cohorte iniciada en segundo plano"
	} else {
		out.Body.Status = "en_progreso"
		out.Body.Mensaje = "ya hay una construcción de la cohorte en curso"
	}
	return out, nil
}

// toCohorteDTOs maps domain cohorte entities to their wire representation.
func toCohorteDTOs(cohorte []*reactivaciondomain.CohorteCliente) []CohorteClienteDTO {
	dtos := make([]CohorteClienteDTO, 0, len(cohorte))
	for _, c := range cohorte {
		dto := CohorteClienteDTO{
			ClienteID:      c.ClienteID(),
			Nombre:         c.Nombre(),
			Telefono:       c.Telefono(),
			Segmento:       c.Segmento().String(),
			EnControl:      c.EnControl(),
			FueContactado:  c.FueContactado(),
			CohorteFecha:   c.CohorteFecha().UTC().Format(time.RFC3339),
			Saldo:          c.Saldo().StringFixed(2),
			PorLiquidarPct: c.PorLiquidarPct().StringFixed(2),
		}
		if !c.FechaUltimaCompraBase().IsZero() {
			s := c.FechaUltimaCompraBase().UTC().Format(time.RFC3339)
			dto.FechaUltimaCompraBase = &s
		}
		dtos = append(dtos, dto)
	}
	return dtos
}
