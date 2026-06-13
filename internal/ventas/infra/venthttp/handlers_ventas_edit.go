//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

// ActualizarHeader handles PATCH /v2/ventas/{id}. Requires PermVentasEditar.
func (h *Handlers) ActualizarHeader(ctx context.Context, in *ActualizarHeaderInput) (*ActualizarHeaderOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEditar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	input, err := actualizarHeaderBodyToAppInput(id, in.Body)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ActualizarHeader(ctx, input, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ActualizarHeaderOutput{Body: toVentaDTO(v, nil)}, nil
}

// ActualizarCliente handles PATCH /v2/ventas/{id}/cliente.
func (h *Handlers) ActualizarCliente(ctx context.Context, in *ActualizarClienteInput) (*ActualizarClienteOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEditar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ActualizarCliente(ctx, ventasapp.ActualizarClienteInput{
		VentaID:       id,
		ClienteID:     in.Body.Cliente.ClienteID,
		ClienteNombre: in.Body.Cliente.Nombre,
		ClienteTel:    in.Body.Cliente.Telefono,
		ClienteAval:   in.Body.Cliente.Aval,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ActualizarClienteOutput{Body: toVentaDTO(v, nil)}, nil
}

// ReemplazarProductos handles PUT /v2/ventas/{id}/productos.
func (h *Handlers) ReemplazarProductos(ctx context.Context, in *ReemplazarProductosInput) (*ReemplazarProductosOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEditar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	productos, err := parseProductosDTO(in.Body.Productos)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ReemplazarProductos(ctx, ventasapp.ReemplazarProductosInput{
		VentaID: id, Productos: productos,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReemplazarProductosOutput{Body: toVentaDTO(v, nil)}, nil
}

// ReemplazarCombos handles PUT /v2/ventas/{id}/combos.
func (h *Handlers) ReemplazarCombos(ctx context.Context, in *ReemplazarCombosInput) (*ReemplazarCombosOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEditar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	combos, err := parseCombosDTO(in.Body.Combos)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ReemplazarCombos(ctx, ventasapp.ReemplazarCombosInput{
		VentaID: id, Combos: combos,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReemplazarCombosOutput{Body: toVentaDTO(v, nil)}, nil
}

// ReemplazarVendedores handles PUT /v2/ventas/{id}/vendedores.
func (h *Handlers) ReemplazarVendedores(ctx context.Context, in *ReemplazarVendedoresInput) (*ReemplazarVendedoresOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEditar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	vendedores, err := parseVendedoresDTO(in.Body.Vendedores)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ReemplazarVendedores(ctx, ventasapp.ReemplazarVendedoresInput{
		VentaID: id, Vendedores: vendedores,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReemplazarVendedoresOutput{Body: toVentaDTO(v, nil)}, nil
}

// actualizarHeaderBodyToAppInput translates the JSON body for the header
// edit into the app input shape, mirroring crearVentaBodyToAppInput minus
// non-editable fields. Montos are not forwarded — they are derived from
// line items by the domain.
func actualizarHeaderBodyToAppInput(ventaID uuid.UUID, b ActualizarHeaderBody) (ventasapp.ActualizarHeaderInput, error) {
	fecha, err := parseTimeField(b.FechaVenta, "fecha_venta")
	if err != nil {
		return ventasapp.ActualizarHeaderInput{}, err
	}
	plan, err := parsePlanCreditoDTO(b.PlanCredito)
	if err != nil {
		return ventasapp.ActualizarHeaderInput{}, err
	}
	return ventasapp.ActualizarHeaderInput{
		VentaID:        ventaID,
		Calle:          b.Direccion.Calle,
		NumeroExterior: b.Direccion.NumeroExterior,
		Colonia:        b.Direccion.Colonia,
		Poblacion:      b.Direccion.Poblacion,
		Ciudad:         b.Direccion.Ciudad,
		ZonaClienteID:  b.Direccion.ZonaClienteID,
		Latitud:        b.GPS.Latitud,
		Longitud:       b.GPS.Longitud,
		FechaVenta:     fecha,
		PlanCredito:    plan,
		DiaCobranza:    dtoToAppDiaCobranza(b.DiaCobranza),
		Nota:           b.Nota,
	}, nil
}
