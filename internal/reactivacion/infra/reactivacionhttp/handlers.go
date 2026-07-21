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

// drenarBatchSize is the number of pendientes DrenarCola processes per manual
// POST /reactivacion/envios/drenar call (the automatic EnvioWorker uses its
// own configured batch — see cmd/api/reactivacion_wiring.go).
const drenarBatchSize = 200

// Encolar handles POST /reactivacion/envios/encolar. Requires
// auth.PermReactivacionAdministrar. It launches the encolado in the
// background and returns 202 immediately.
func (h *Handlers) Encolar(ctx context.Context, _ *EncolarInput) (*EncolarOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	out := &EncolarOutput{}
	//nolint:contextcheck // intentional: EncolarEnSegundoPlano uses context.Background() internally
	if h.svc.EncolarEnSegundoPlano() {
		out.Body.Status = "aceptado"
		out.Body.Mensaje = "encolado de mensajes de reactivación iniciado en segundo plano"
	} else {
		out.Body.Status = "en_progreso"
		out.Body.Mensaje = "ya hay un encolado de mensajes en curso"
	}
	return out, nil
}

// Drenar handles POST /reactivacion/envios/drenar. Requires
// auth.PermReactivacionAdministrar. It runs one drain batch synchronously
// (useful for demo/manual operation) and reports the outcome.
func (h *Handlers) Drenar(ctx context.Context, _ *DrenarInput) (*DrenarOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	res, err := h.svc.DrenarCola(ctx, drenarBatchSize)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &DrenarOutput{}
	out.Body = DrenarResultDTO{
		Enviados:   res.Enviados,
		Fallidos:   res.Fallidos,
		Bloqueados: res.Bloqueados,
		Saltados:   res.Saltados,
	}
	return out, nil
}

// ListEnvios handles GET /reactivacion/envios. Requires auth.PermReactivacionLeer.
func (h *Handlers) ListEnvios(ctx context.Context, in *ListEnviosInput) (*ListEnviosOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionLeer); err != nil {
		return nil, err
	}

	mensajes, err := h.svc.ListarMensajes(ctx, reactivacionapp.ListarMensajesParams{
		Estado:   in.Estado,
		Segmento: in.Segmento,
		Limit:    in.Limit,
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListEnviosOutput{}
	out.Body.Items = toMensajeDTOs(mensajes)
	return out, nil
}

// toMensajeDTOs maps domain Mensaje entities to their wire representation.
func toMensajeDTOs(mensajes []*reactivaciondomain.Mensaje) []MensajeDTO {
	dtos := make([]MensajeDTO, 0, len(mensajes))
	for _, m := range mensajes {
		dto := MensajeDTO{
			ClienteID:  m.ClienteID(),
			Segmento:   m.Segmento().String(),
			Telefono:   m.Telefono(),
			Cuerpo:     m.Cuerpo(),
			Estado:     m.Estado().String(),
			SenderKind: m.SenderKind().String(),
			EncoladoEn: m.EncoladoEn().UTC().Format(time.RFC3339),
		}
		if !m.EnviadoEn().IsZero() {
			s := m.EnviadoEn().UTC().Format(time.RFC3339)
			dto.EnviadoEn = &s
		}
		if m.Motivo() != "" {
			e := m.Motivo()
			dto.Error = &e
		}
		dtos = append(dtos, dto)
	}
	return dtos
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
