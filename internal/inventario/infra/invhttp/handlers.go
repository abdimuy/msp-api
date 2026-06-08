//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	inventarioapp "github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Handlers groups every Huma handler for the inventario module.
type Handlers struct {
	svc *inventarioapp.Service
}

// NewHandlers wires a Handlers with its application service dependency.
func NewHandlers(svc *inventarioapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ObtenerTraspaso is the handler for GET /traspasos/{id}.
func (h *Handlers) ObtenerTraspaso(ctx context.Context, in *ObtenerTraspasoInput) (*ObtenerTraspasoOutput, error) {
	if err := requireCurrentUser(ctx, auth.PermTraspasosVer); err != nil {
		return nil, err
	}
	tr, err := h.svc.ObtenerTraspaso(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	return &ObtenerTraspasoOutput{Body: toTraspasoResponse(tr)}, nil
}

// ListarTraspasosPorVenta is the handler for GET /traspasos?venta_id={uuid}.
func (h *Handlers) ListarTraspasosPorVenta(ctx context.Context, in *ListarTraspasosPorVentaInput) (*ListarTraspasosPorVentaOutput, error) {
	if err := requireCurrentUser(ctx, auth.PermTraspasosVer); err != nil {
		return nil, err
	}
	ventaID, err := uuid.Parse(in.VentaID)
	if err != nil {
		return nil, mapErr(apperror.NewValidation(
			"venta_id_invalido", "el parámetro venta_id no es un UUID válido",
		).WithError(err))
	}
	list, err := h.svc.ListarTraspasosPorVenta(ctx, ventaID)
	if err != nil {
		return nil, mapErr(err)
	}
	items := make([]TraspasoResponse, 0, len(list))
	for _, tr := range list {
		items = append(items, toTraspasoResponse(tr))
	}
	out := &ListarTraspasosPorVentaOutput{}
	out.Body.Items = items
	return out, nil
}

// ConsultarStock is the handler for GET /inventario/stock?articulo_id={int}&almacen_id={int}.
func (h *Handlers) ConsultarStock(ctx context.Context, in *ConsultarStockInput) (*ConsultarStockOutput, error) {
	if err := requireCurrentUser(ctx, auth.PermStockConsultar); err != nil {
		return nil, err
	}
	cantidad, err := h.svc.ConsultarExistencia(ctx, in.ArticuloID, in.AlmacenID)
	if err != nil {
		return nil, mapErr(err)
	}
	return &ConsultarStockOutput{Body: toStockResponse(in.ArticuloID, in.AlmacenID, cantidad)}, nil
}

// ListarAlmacenes is the handler for GET /inventario/almacenes.
func (h *Handlers) ListarAlmacenes(ctx context.Context, _ *struct{}) (*ListarAlmacenesOutput, error) {
	if err := requireCurrentUser(ctx, auth.PermInventarioVer); err != nil {
		return nil, err
	}
	almacenes, err := h.svc.ListarAlmacenes(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	items := make([]AlmacenResponse, 0, len(almacenes))
	for _, a := range almacenes {
		items = append(items, toAlmacenResponse(a))
	}
	out := &ListarAlmacenesOutput{}
	out.Body.Items = items
	return out, nil
}

// ─── Auth helpers ───────────────────────────────────────────────────────────

// requireCurrentUser extracts the CurrentUser from ctx and asserts it holds
// every permission in perms. Returns 401 when no user is planted, 403 on
// the first missing permission.
func requireCurrentUser(ctx context.Context, perms ...auth.Permission) error {
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		return huma.Error401Unauthorized("no autenticado")
	}
	held := make(map[string]struct{}, len(cu.Permisos))
	for _, p := range cu.Permisos {
		held[p] = struct{}{}
	}
	for _, required := range perms {
		if _, ok := held[string(required)]; !ok {
			return huma.NewError(http.StatusForbidden, "permiso denegado",
				&huma.ErrorDetail{
					Message:  "el principal no tiene el permiso requerido",
					Location: "header.authorization",
					Value:    string(required),
				})
		}
	}
	return nil
}

// mapErr translates a typed apperror.Error into a huma.StatusError. Non-typed
// errors fall through as 500.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno",
			&huma.ErrorDetail{Message: err.Error()})
	}
	status := ae.Kind.HTTPStatus()
	detail := &huma.ErrorDetail{Message: "code=" + ae.Code}
	return huma.NewError(status, ae.Message, detail)
}

// Compile-time assertions: handler signatures match Huma's expected shape.
var (
	_ func(context.Context, *ObtenerTraspasoInput) (*ObtenerTraspasoOutput, error)                 = (*Handlers)(nil).ObtenerTraspaso
	_ func(context.Context, *ListarTraspasosPorVentaInput) (*ListarTraspasosPorVentaOutput, error) = (*Handlers)(nil).ListarTraspasosPorVenta
	_ func(context.Context, *ConsultarStockInput) (*ConsultarStockOutput, error)                   = (*Handlers)(nil).ConsultarStock
	_ func(context.Context, *struct{}) (*ListarAlmacenesOutput, error)                             = (*Handlers)(nil).ListarAlmacenes
)
