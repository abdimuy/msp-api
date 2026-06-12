package microsiphttp

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
)

// Handlers groups every Huma handler exposed by the microsip module.
type Handlers struct {
	svc *microsipapp.Service
}

// NewHandlers wires a Handlers bundle with its application service.
func NewHandlers(svc *microsipapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ListarAlmacenes returns every visible almacen. Authenticated read; no
// extra permission gate (catalog data).
func (h *Handlers) ListarAlmacenes(ctx context.Context, _ *ListarAlmacenesInput) (*ListarAlmacenesOutput, error) {
	almacenes, err := h.svc.ListarAlmacenes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	items := make([]AlmacenDTO, 0, len(almacenes))
	for _, a := range almacenes {
		items = append(items, toAlmacenDTO(a))
	}
	return &ListarAlmacenesOutput{Body: listResponse[AlmacenDTO]{Items: items}}, nil
}

// ObtenerAlmacen returns a single almacen. 404 when the ID does not exist.
func (h *Handlers) ObtenerAlmacen(ctx context.Context, in *ObtenerAlmacenInput) (*ObtenerAlmacenOutput, error) {
	a, err := h.svc.ObtenerAlmacen(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if a == nil {
		return nil, huma.Error404NotFound("almacén no encontrado")
	}
	return &ObtenerAlmacenOutput{Body: toAlmacenDTO(*a)}, nil
}

// ListarArticulosDelAlmacen returns articulos with positive existencias
// for the almacen, optionally filtered by name substring.
func (h *Handlers) ListarArticulosDelAlmacen(ctx context.Context, in *ListarArticulosDelAlmacenInput) (*ListarArticulosDelAlmacenOutput, error) {
	arts, err := h.svc.ListarArticulosDelAlmacen(ctx, in.ID, in.Buscar)
	if err != nil {
		return nil, mapError(err)
	}
	items := make([]ArticuloAlmacenDTO, 0, len(arts))
	for _, a := range arts {
		items = append(items, toArticuloAlmacenDTO(a))
	}
	return &ListarArticulosDelAlmacenOutput{Body: listResponse[ArticuloAlmacenDTO]{Items: items}}, nil
}

// ListarZonasCliente returns the zonas catalog with cobradores appended.
func (h *Handlers) ListarZonasCliente(ctx context.Context, _ *ListarZonasClienteInput) (*ListarZonasClienteOutput, error) {
	zonas, err := h.svc.ListarZonasCliente(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	items := make([]ZonaClienteDTO, 0, len(zonas))
	for _, z := range zonas {
		items = append(items, toZonaClienteDTO(z))
	}
	return &ListarZonasClienteOutput{Body: listResponse[ZonaClienteDTO]{Items: items}}, nil
}

// mapError surfaces repo errors as a generic 500 — the catalog endpoints
// have no expected domain errors, so anything reaching this point is
// unexpected (Firebird unreachable, schema drift, etc.).
func mapError(err error) error {
	if err == nil {
		return nil
	}
	return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno",
		&huma.ErrorDetail{Message: err.Error()})
}
