package confighttp

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	configapp "github.com/abdimuy/msp-api/internal/config/app"
	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// Handlers holds the config module's HTTP handlers.
type Handlers struct {
	svc *configapp.Service
}

// NewHandlers builds a Handlers wired against the given service.
func NewHandlers(svc *configapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ListarVendedores handles GET /config/vendedores. Requires auth.PermConfigLeer.
func (h *Handlers) ListarVendedores(ctx context.Context, _ *ListVendedoresInput) (*ListVendedoresOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigLeer); err != nil {
		return nil, err
	}

	asignaciones, err := h.svc.ListarVendedores(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListVendedoresOutput{}
	out.Body.Items = toVendedorAsignacionDTOs(asignaciones)
	return out, nil
}

// ListarOpciones handles GET /config/vendedores/opciones. Requires auth.PermConfigLeer.
func (h *Handlers) ListarOpciones(ctx context.Context, _ *ListOpcionesInput) (*ListOpcionesOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigLeer); err != nil {
		return nil, err
	}

	identidades, err := h.svc.ListarIdentidadesMicrosip(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListOpcionesOutput{}
	out.Body.Items = toIdentidadMicrosipDTOs(identidades)
	return out, nil
}

// AsignarVendedor handles PUT /config/vendedores/{usuarioId}. Requires
// auth.PermConfigAdministrar. Returns the re-listed VendedorAsignacion for
// the affected usuario so the caller can refresh without a second GET.
func (h *Handlers) AsignarVendedor(ctx context.Context, in *AsignarVendedorInput) (*AsignarVendedorOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigAdministrar); err != nil {
		return nil, err
	}

	usuarioID, err := uuid.Parse(in.UsuarioID)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("usuario_id inválido")
	}

	if err := h.svc.AsignarVendedor(ctx, usuarioID,
		in.Body.VendedorListaID1, in.Body.VendedorListaID2, in.Body.VendedorListaID3,
	); err != nil {
		return nil, mapAppError(err)
	}

	asignaciones, err := h.svc.ListarVendedores(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &AsignarVendedorOutput{}
	for _, a := range asignaciones {
		if a.UsuarioID == usuarioID {
			out.Body.Item = toVendedorAsignacionDTO(a)
			break
		}
	}
	return out, nil
}

// EliminarVendedor handles DELETE /config/vendedores/{usuarioId}. Requires
// auth.PermConfigAdministrar.
func (h *Handlers) EliminarVendedor(ctx context.Context, in *EliminarVendedorInput) (*EliminarVendedorOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigAdministrar); err != nil {
		return nil, err
	}

	usuarioID, err := uuid.Parse(in.UsuarioID)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("usuario_id inválido")
	}

	if err := h.svc.EliminarVendedor(ctx, usuarioID); err != nil {
		return nil, mapAppError(err)
	}

	out := &EliminarVendedorOutput{}
	out.Body.OK = true
	return out, nil
}

// ─── DTO mapping ─────────────────────────────────────────────────────────────

func toVendedorAsignacionDTOs(asignaciones []configdomain.VendedorAsignacion) []VendedorAsignacionDTO {
	if asignaciones == nil {
		return []VendedorAsignacionDTO{}
	}
	dtos := make([]VendedorAsignacionDTO, len(asignaciones))
	for i, a := range asignaciones {
		dtos[i] = toVendedorAsignacionDTO(a)
	}
	return dtos
}

func toVendedorAsignacionDTO(a configdomain.VendedorAsignacion) VendedorAsignacionDTO {
	return VendedorAsignacionDTO{
		UsuarioID: a.UsuarioID.String(),
		Nombre:    a.Nombre,
		Email:     a.Email,
		Mapping: VendedorMappingDTO{
			V1: toVendedorSlotDTO(a.V1),
			V2: toVendedorSlotDTO(a.V2),
			V3: toVendedorSlotDTO(a.V3),
		},
		Estado: a.Estado,
	}
}

func toVendedorSlotDTO(s *configdomain.VendedorSlot) *VendedorSlotDTO {
	if s == nil {
		return nil
	}
	return &VendedorSlotDTO{ListaID: s.ListaID, Nombre: s.Nombre}
}

func toIdentidadMicrosipDTOs(identidades []configdomain.IdentidadMicrosip) []IdentidadMicrosipDTO {
	if identidades == nil {
		return []IdentidadMicrosipDTO{}
	}
	dtos := make([]IdentidadMicrosipDTO, len(identidades))
	for i, id := range identidades {
		dtos[i] = IdentidadMicrosipDTO{
			Nombre:     id.Nombre,
			V1ListaID:  id.V1ListaID,
			V2ListaID:  id.V2ListaID,
			V3ListaID:  id.V3ListaID,
			MatchCount: id.MatchCount,
		}
	}
	return dtos
}
