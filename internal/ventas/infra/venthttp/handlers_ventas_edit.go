//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

// parseIfMatch parses an optional If-Match header into the optimistic-
// concurrency precondition the domain expects: nil when the header is
// absent — the compatibility contract the desktop app relies on, since it
// never sends If-Match and must keep writing unconditionally — or the
// version carried inside the entity-tag quotes ("<version>", RFC 7232).
//
// A malformed header (no quotes, non-integer content) is a client mistake,
// not a version race, so it maps to 422 if_match_invalido, never 412: a
// caller that cannot even form the precondition correctly has nothing to
// retry against.
func parseIfMatch(raw string) (*int, error) {
	if raw == "" {
		return nil, nil //nolint:nilnil // absent precondition is a valid, common case.
	}
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, apperror.NewValidation(
			"if_match_invalido",
			"el encabezado if-match debe traer la versión entre comillas, por ejemplo \"3\"",
		).WithField("header", "If-Match")
	}
	v, err := strconv.Atoi(raw[1 : len(raw)-1])
	if err != nil {
		return nil, apperror.NewValidation(
			"if_match_invalido",
			"el encabezado if-match debe traer la versión entre comillas, por ejemplo \"3\"",
		).WithField("header", "If-Match").WithError(err)
	}
	return &v, nil
}

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
	expectedVersion, err := parseIfMatch(in.IfMatch)
	if err != nil {
		return nil, mapAppError(err)
	}
	input, err := actualizarHeaderBodyToAppInput(id, in.Body)
	if err != nil {
		return nil, mapAppError(err)
	}
	input.ExpectedVersion = expectedVersion
	v, err := h.svc.ActualizarHeader(ctx, input, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ActualizarHeaderOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
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
	expectedVersion, err := parseIfMatch(in.IfMatch)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ActualizarCliente(ctx, ventasapp.ActualizarClienteInput{
		VentaID:         id,
		ClienteID:       in.Body.Cliente.ClienteID,
		ClienteNombre:   in.Body.Cliente.Nombre,
		ClienteTel:      in.Body.Cliente.Telefono,
		ClienteAval:     in.Body.Cliente.Aval,
		ExpectedVersion: expectedVersion,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ActualizarClienteOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
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
	return &ReemplazarProductosOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
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
	return &ReemplazarCombosOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
}

// ReemplazarLineas handles PUT /v2/ventas/{id}/lineas — the atomic
// replacement of both line-item collections. Requires PermVentasEditar, the
// same permission the two single-collection endpoints require.
func (h *Handlers) ReemplazarLineas(ctx context.Context, in *ReemplazarLineasInput) (*ReemplazarLineasOutput, error) {
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
	expectedVersion, err := parseIfMatch(in.IfMatch)
	if err != nil {
		return nil, mapAppError(err)
	}
	combos, err := parseCombosDTO(in.Body.Combos)
	if err != nil {
		return nil, mapAppError(err)
	}
	productos, err := parseProductosDTO(in.Body.Productos)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ReemplazarLineas(ctx, ventasapp.ReemplazarLineasInput{
		VentaID: id, Combos: combos, Productos: productos,
		ExpectedVersion: expectedVersion,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReemplazarLineasOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
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
	expectedVersion, err := parseIfMatch(in.IfMatch)
	if err != nil {
		return nil, mapAppError(err)
	}
	vendedores, err := parseVendedoresDTO(in.Body.Vendedores)
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ReemplazarVendedores(ctx, ventasapp.ReemplazarVendedoresInput{
		VentaID: id, Vendedores: vendedores,
		ExpectedVersion: expectedVersion,
	}, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReemplazarVendedoresOutput{Body: toVentaDTO(v, nil, nil, nil)}, nil
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
