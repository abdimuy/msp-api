package confighttp

import (
	"context"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// ─── DTOs ────────────────────────────────────────────────────────────────────

// CatalogoRefDTO is a resolved catalog reference (id + display name).
type CatalogoRefDTO struct {
	ID     int    `json:"id"     doc:"Id de Microsip"`
	Nombre string `json:"nombre" doc:"Nombre en Microsip"`
}

// ZonaCajaAsignacionDTO is one row of the zonas/cajas administration screen.
type ZonaCajaAsignacionDTO struct {
	ZonaClienteID int             `json:"zona_cliente_id" doc:"Id de la zona de cliente"`
	ZonaNombre    string          `json:"zona_nombre"     doc:"Nombre de la zona"`
	Caja          *CatalogoRefDTO `json:"caja"             doc:"Caja asignada; nulo si no está asignada"`
	Cajero        *CatalogoRefDTO `json:"cajero"           doc:"Cajero asignado; nulo si no está asignado"`
	Vendedor      *CatalogoRefDTO `json:"vendedor"         doc:"Vendedor asignado; nulo si no está asignado"`
	Cobrador      *CatalogoRefDTO `json:"cobrador"         doc:"Cobrador asignado; nulo si no está asignado"`
}

// ListZonasCajasInput has no query parameters — the endpoint returns every
// client zone.
type ListZonasCajasInput struct{}

// ListZonasCajasOutput wraps the response body for GET /config/zonas-cajas.
type ListZonasCajasOutput struct {
	Body struct {
		Items []ZonaCajaAsignacionDTO `json:"items"`
	}
}

// ListOpcionesZonasCajasInput has no query parameters — the endpoint returns
// every catalog needed by the dropdowns.
type ListOpcionesZonasCajasInput struct{}

// ListOpcionesZonasCajasOutput wraps the response body for
// GET /config/zonas-cajas/opciones.
type ListOpcionesZonasCajasOutput struct {
	Body struct {
		Zonas      []CatalogoRefDTO `json:"zonas"`
		Cajas      []CatalogoRefDTO `json:"cajas"`
		Cajeros    []CatalogoRefDTO `json:"cajeros"`
		Vendedores []CatalogoRefDTO `json:"vendedores"`
		Cobradores []CatalogoRefDTO `json:"cobradores"`
	}
}

// AsignarZonaCajaInput is the payload for PUT /config/zonas-cajas/{zonaClienteId}.
type AsignarZonaCajaInput struct {
	ZonaClienteID string `path:"zonaClienteId" doc:"Id (entero) de la zona de cliente"`
	Body          struct {
		CajaID     int `json:"caja_id"     doc:"Id de la caja de Microsip; -1 para dejarla sin asignar"`
		CajeroID   int `json:"cajero_id"   doc:"Id del cajero de Microsip; -1 para dejarlo sin asignar"`
		VendedorID int `json:"vendedor_id" doc:"Id del vendedor de Microsip; -1 para dejarlo sin asignar"`
		CobradorID int `json:"cobrador_id" doc:"Id del cobrador de Microsip; -1 para dejarlo sin asignar"`
	}
}

// AsignarZonaCajaOutput wraps the response body for
// PUT /config/zonas-cajas/{zonaClienteId}. Returns the re-resolved
// ZonaCajaAsignacion so the caller can refresh without a second GET.
type AsignarZonaCajaOutput struct {
	Body struct {
		Item ZonaCajaAsignacionDTO `json:"item"`
	}
}

// ─── handlers ────────────────────────────────────────────────────────────────

// ListarZonasCajas handles GET /config/zonas-cajas. Requires auth.PermConfigLeer.
func (h *Handlers) ListarZonasCajas(ctx context.Context, _ *ListZonasCajasInput) (*ListZonasCajasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigLeer); err != nil {
		return nil, err
	}

	asignaciones, err := h.svc.ListarZonasCajas(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListZonasCajasOutput{}
	out.Body.Items = toZonaCajaAsignacionDTOs(asignaciones)
	return out, nil
}

// ListarOpcionesZonasCajas handles GET /config/zonas-cajas/opciones. Requires
// auth.PermConfigLeer.
func (h *Handlers) ListarOpcionesZonasCajas(
	ctx context.Context, _ *ListOpcionesZonasCajasInput,
) (*ListOpcionesZonasCajasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigLeer); err != nil {
		return nil, err
	}

	opciones, err := h.svc.ListarOpcionesZonasCajas(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ListOpcionesZonasCajasOutput{}
	out.Body.Zonas = toCatalogoRefDTOs(opciones.Zonas)
	out.Body.Cajas = toCatalogoRefDTOs(opciones.Cajas)
	out.Body.Cajeros = toCatalogoRefDTOs(opciones.Cajeros)
	out.Body.Vendedores = toCatalogoRefDTOs(opciones.Vendedores)
	out.Body.Cobradores = toCatalogoRefDTOs(opciones.Cobradores)
	return out, nil
}

// AsignarZonaCaja handles PUT /config/zonas-cajas/{zonaClienteId}. Requires
// auth.PermConfigAdministrar. Returns the re-listed ZonaCajaAsignacion for
// the affected zone so the caller can refresh without a second GET.
func (h *Handlers) AsignarZonaCaja(ctx context.Context, in *AsignarZonaCajaInput) (*AsignarZonaCajaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermConfigAdministrar); err != nil {
		return nil, err
	}

	zonaClienteID, err := strconv.Atoi(in.ZonaClienteID)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("zona_cliente_id inválido")
	}

	if err := h.svc.AsignarZonaCaja(ctx, zonaClienteID,
		in.Body.CajaID, in.Body.CajeroID, in.Body.VendedorID, in.Body.CobradorID,
	); err != nil {
		return nil, mapAppError(err)
	}

	asignaciones, err := h.svc.ListarZonasCajas(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &AsignarZonaCajaOutput{}
	for _, a := range asignaciones {
		if a.ZonaClienteID == zonaClienteID {
			out.Body.Item = toZonaCajaAsignacionDTO(a)
			break
		}
	}
	return out, nil
}

// ─── DTO mapping ─────────────────────────────────────────────────────────────

func toZonaCajaAsignacionDTOs(asignaciones []configdomain.ZonaCajaAsignacion) []ZonaCajaAsignacionDTO {
	if asignaciones == nil {
		return []ZonaCajaAsignacionDTO{}
	}
	dtos := make([]ZonaCajaAsignacionDTO, len(asignaciones))
	for i, a := range asignaciones {
		dtos[i] = toZonaCajaAsignacionDTO(a)
	}
	return dtos
}

func toZonaCajaAsignacionDTO(a configdomain.ZonaCajaAsignacion) ZonaCajaAsignacionDTO {
	return ZonaCajaAsignacionDTO{
		ZonaClienteID: a.ZonaClienteID,
		ZonaNombre:    a.ZonaNombre,
		Caja:          toCatalogoRefDTO(a.Caja),
		Cajero:        toCatalogoRefDTO(a.Cajero),
		Vendedor:      toCatalogoRefDTO(a.Vendedor),
		Cobrador:      toCatalogoRefDTO(a.Cobrador),
	}
}

func toCatalogoRefDTO(r *configdomain.CatalogoRef) *CatalogoRefDTO {
	if r == nil {
		return nil
	}
	return &CatalogoRefDTO{ID: r.ID, Nombre: r.Nombre}
}

func toCatalogoRefDTOs(refs []configdomain.CatalogoRef) []CatalogoRefDTO {
	if refs == nil {
		return []CatalogoRefDTO{}
	}
	dtos := make([]CatalogoRefDTO, len(refs))
	for i, r := range refs {
		dtos[i] = CatalogoRefDTO{ID: r.ID, Nombre: r.Nombre}
	}
	return dtos
}
