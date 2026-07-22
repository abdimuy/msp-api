//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	reactivaciondomain "github.com/abdimuy/msp-api/internal/reactivacion/domain"
	reactivacionoutbound "github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// MensajeEntrante handles POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante.
// Requires auth.PermReactivacionAdministrar. In Fase 3a there is no real
// inbound WhatsApp channel wired — this endpoint SIMULATES the cliente's
// message so an operator can drive the copiloto's analysis loop.
func (h *Handlers) MensajeEntrante(ctx context.Context, in *MensajeEntranteInput) (*MensajeEntranteOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	res, err := h.svc.ProcesarMensajeEntrante(ctx, in.ClienteID, in.Body.Mensaje)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &MensajeEntranteOutput{}
	out.Body = toDecisionResultDTO(res)
	return out, nil
}

// ListConversaciones handles GET /reactivacion/conversaciones. Requires
// auth.PermReactivacionLeer. When SoloEscaladas is true, Estado is ignored —
// the repo ANDs both filters, so sending them together would otherwise
// silently yield zero rows whenever Estado isn't "escalado".
func (h *Handlers) ListConversaciones(ctx context.Context, in *ListConversacionesInput) (*ListConversacionesOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionLeer); err != nil {
		return nil, err
	}

	params := reactivacionoutbound.ListarConversacionesParams{SoloEscaladas: in.SoloEscaladas}
	if !in.SoloEscaladas {
		params.Estado = in.Estado
	}

	resumenes, err := h.svc.ListarConversaciones(ctx, params)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListConversacionesOutput{}
	out.Body.Items = toConversacionResumenDTOs(resumenes)
	return out, nil
}

// ObtenerConversacion handles GET /reactivacion/conversaciones/{cliente_id}.
// Requires auth.PermReactivacionLeer.
func (h *Handlers) ObtenerConversacion(ctx context.Context, in *ObtenerConversacionInput) (*ObtenerConversacionOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionLeer); err != nil {
		return nil, err
	}

	det, err := h.svc.ObtenerConversacion(ctx, in.ClienteID)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ObtenerConversacionOutput{}
	out.Body = toConversacionDetalleDTO(det)
	return out, nil
}

// AprobarBorrador handles POST /reactivacion/conversaciones/{cliente_id}/aprobar.
// Requires auth.PermReactivacionAdministrar.
func (h *Handlers) AprobarBorrador(ctx context.Context, in *AprobarBorradorInput) (*AprobarBorradorOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	if err := h.svc.AprobarBorrador(ctx, in.ClienteID); err != nil {
		return nil, mapAppError(err)
	}

	out := &AprobarBorradorOutput{}
	out.Body.Ok = true
	return out, nil
}

// EditarBorrador handles POST /reactivacion/conversaciones/{cliente_id}/editar.
// Requires auth.PermReactivacionAdministrar.
func (h *Handlers) EditarBorrador(ctx context.Context, in *EditarInput) (*EditarOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	if err := h.svc.EditarYAprobar(ctx, in.ClienteID, in.Body.Texto); err != nil {
		return nil, mapAppError(err)
	}

	out := &EditarOutput{}
	out.Body.Ok = true
	return out, nil
}

// Dictar handles POST /reactivacion/conversaciones/{cliente_id}/dictar.
// Requires auth.PermReactivacionAdministrar.
func (h *Handlers) Dictar(ctx context.Context, in *DictarInput) (*DictarOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	borrador, err := h.svc.Dictar(ctx, in.ClienteID, in.Body.Intencion)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &DictarOutput{}
	out.Body.Borrador = borrador
	return out, nil
}

// Escalar handles POST /reactivacion/conversaciones/{cliente_id}/escalar.
// Requires auth.PermReactivacionAdministrar.
func (h *Handlers) Escalar(ctx context.Context, in *EscalarInput) (*EscalarOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermReactivacionAdministrar); err != nil {
		return nil, err
	}

	if err := h.svc.Escalar(ctx, in.ClienteID, in.Body.AsignadoA); err != nil {
		return nil, mapAppError(err)
	}

	out := &EscalarOutput{}
	out.Body.Ok = true
	return out, nil
}

// ─── mappers ──────────────────────────────────────────────────────────────────

// toDecisionResultDTO maps a ProcesarMensajeEntrante outcome to its wire
// representation. Borrador comes from res.Borrador (the guardado draft,
// empty when escalada) rather than res.Decision.Borrador() (the LLM's raw
// proposal, which may be non-empty even when triar escalated the turn).
func toDecisionResultDTO(res reactivacionapp.ProcesarResult) DecisionResultDTO {
	d := res.Decision
	return DecisionResultDTO{
		Intencion:         d.Intencion(),
		Confianza:         d.Confianza(),
		Senales:           d.Senales(),
		Accion:            d.AccionPropuesta().String(),
		Borrador:          res.Borrador,
		Evidencia:         d.Evidencia(),
		RazonEscalamiento: d.RazonEscalamiento(),
		Resultado:         d.Resultado().String(),
		Escalada:          res.Escalada,
	}
}

// toConversacionResumenDTOs maps the bandeja queue to its wire representation.
func toConversacionResumenDTOs(resumenes []reactivacionapp.ConversacionResumen) []ConversacionResumenDTO {
	dtos := make([]ConversacionResumenDTO, 0, len(resumenes))
	for _, r := range resumenes {
		dto := ConversacionResumenDTO{
			ClienteID:     r.Conversacion.ClienteID(),
			Estado:        r.Conversacion.Estado().String(),
			AsignadoA:     r.Conversacion.AsignadoA(),
			UpdatedAt:     r.Conversacion.UpdatedAt().UTC().Format(time.RFC3339),
			Nombre:        r.Nombre,
			Segmento:      r.Segmento,
			UltimoMensaje: r.UltimoMensaje,
		}
		if r.UltimaDecision != nil {
			d := r.UltimaDecision
			dto.UltimaDecision = &UltimaDecisionDTO{
				Intencion:         d.Intencion(),
				Confianza:         d.Confianza(),
				Accion:            d.AccionPropuesta().String(),
				Resultado:         d.Resultado().String(),
				RazonEscalamiento: d.RazonEscalamiento(),
			}
		}
		dtos = append(dtos, dto)
	}
	return dtos
}

// toConversacionDetalleDTO maps a ConversacionDetalle to its wire representation.
func toConversacionDetalleDTO(det reactivacionapp.ConversacionDetalle) ConversacionDetalleDTO {
	dto := toConversacionDTO(det.Conversacion)
	dto.Nombre = det.Nombre
	dto.Segmento = det.Segmento
	dto.Telefono = det.Telefono
	return ConversacionDetalleDTO{
		Conversacion: dto,
		Turnos:       toTurnoDTOs(det.Turnos),
		Decisiones:   toDecisionDTOs(det.Decisiones),
	}
}

// toConversacionDTO maps a Conversacion header to its wire representation.
// Nombre/Segmento/Telefono are NOT set here — they come from
// ConversacionDetalle's bandeja enrichment, filled in by the caller.
func toConversacionDTO(c *reactivaciondomain.Conversacion) ConversacionDTO {
	return ConversacionDTO{
		ClienteID:      c.ClienteID(),
		Estado:         c.Estado().String(),
		AsignadoA:      c.AsignadoA(),
		ContextoNota:   c.ContextoNota(),
		Banderas:       c.Banderas(),
		ResumenMemoria: c.ResumenMemoria(),
		CreatedAt:      c.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

// toTurnoDTOs maps the turno thread to its wire representation.
func toTurnoDTOs(turnos []*reactivaciondomain.Turno) []TurnoDTO {
	dtos := make([]TurnoDTO, 0, len(turnos))
	for _, t := range turnos {
		dtos = append(dtos, TurnoDTO{
			Direccion:  t.Direccion().String(),
			Autor:      t.Autor().String(),
			Cuerpo:     t.Cuerpo(),
			MensajeRef: t.MensajeRef(),
			CreatedAt:  t.CreatedAt().UTC().Format(time.RFC3339),
		})
	}
	return dtos
}

// toDecisionDTOs maps the decision audit trail to its wire representation.
func toDecisionDTOs(decisiones []*reactivaciondomain.Decision) []DecisionDTO {
	dtos := make([]DecisionDTO, 0, len(decisiones))
	for _, d := range decisiones {
		dtos = append(dtos, DecisionDTO{
			Intencion:         d.Intencion(),
			Confianza:         d.Confianza(),
			Senales:           d.Senales(),
			Accion:            d.AccionPropuesta().String(),
			Borrador:          d.Borrador(),
			Evidencia:         d.Evidencia(),
			RazonEscalamiento: d.RazonEscalamiento(),
			Resultado:         d.Resultado().String(),
			CreatedAt:         d.CreatedAt().UTC().Format(time.RFC3339),
		})
	}
	return dtos
}
