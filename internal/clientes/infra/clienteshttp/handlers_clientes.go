//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

import (
	"context"
	"time"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"

	"github.com/abdimuy/msp-api/internal/auth"
)

// Handlers holds the clientes HTTP handlers wired against the app service.
type Handlers struct {
	svc *clientesapp.Service
}

// NewHandlers builds a Handlers wired against svc.
func NewHandlers(svc *clientesapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// intPtrOrNil converts a sentinel int (≤ 0) to nil, or wraps it as *int.
// Microsip IDs are always positive, so 0 safely represents "not set".
func intPtrOrNil(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

// scoreMinPtrOrNil converts the sentinel -1 to nil, or wraps non-negative values.
func scoreMinPtrOrNil(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

// ListarClientes handles GET /clientes.
func (h *Handlers) ListarClientes(ctx context.Context, input *ListarClientesInput) (*ListarClientesOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermClientesLeer); err != nil {
		return nil, err
	}

	resultado, err := h.svc.BuscarClientes(ctx, clientesapp.BuscarClientesInput{
		Q:             input.Q,
		ZonaClienteID: intPtrOrNil(input.Zona),
		CobradorID:    intPtrOrNil(input.Cobrador),
		ConSaldo:      input.ConSaldo,
		Segmento:      input.Segmento,
		EstadoPago:    input.EstadoPago,
		ScoreMin:      scoreMinPtrOrNil(input.ScoreMin),
		TierRiesgo:    input.Tier,
		BandaCredito:  input.BandaCredito,
		SortBy:        input.SortBy,
		SortOrder:     input.SortOrder,
		Pagination: outbound.ListParams{
			Cursor:   input.Cursor,
			PageSize: input.Limit,
		},
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	items := make([]ClienteListItemDTO, 0, len(resultado.Items))
	for _, doc := range resultado.Items {
		items = append(items, dirDocToClienteListItemDTO(doc))
	}

	out := &ListarClientesOutput{}
	out.Body.Items = items
	out.Body.NextCursor = resultado.NextCursor
	out.Body.Facets = resultado.Facets
	return out, nil
}

// ObtenerFicha handles GET /clientes/{id}.
func (h *Handlers) ObtenerFicha(ctx context.Context, input *ObtenerFichaInput) (*ObtenerFichaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermClientesLeer); err != nil {
		return nil, err
	}

	rango, err := parseFichaRango(input.Desde, input.Hasta)
	if err != nil {
		return nil, mapAppError(err)
	}

	ficha, err := h.svc.ObtenerFicha(ctx, input.ID, rango)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ObtenerFichaOutput{}
	out.Body = toFichaDTO(ficha)
	return out, nil
}

// parseFichaRango parses optional date-only strings (YYYY-MM-DD) into a RangoFechas.
// Returns a validation error when both dates are present and desde > hasta.
func parseFichaRango(desdeStr, hastaStr string) (outbound.RangoFechas, error) {
	const layout = "2006-01-02"
	var rango outbound.RangoFechas

	if desdeStr != "" {
		t, err := time.Parse(layout, desdeStr)
		if err != nil {
			return outbound.RangoFechas{}, apperror.NewValidation(
				"ficha_rango_desde_invalido",
				"el valor de desde no es una fecha válida (formato esperado: YYYY-MM-DD)",
			)
		}
		tu := t.UTC()
		rango.Desde = &tu
	}
	if hastaStr != "" {
		t, err := time.Parse(layout, hastaStr)
		if err != nil {
			return outbound.RangoFechas{}, apperror.NewValidation(
				"ficha_rango_hasta_invalido",
				"el valor de hasta no es una fecha válida (formato esperado: YYYY-MM-DD)",
			)
		}
		// Set end-of-day (23:59:59 UTC) to make hasta inclusive.
		t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		tu := t.UTC()
		rango.Hasta = &tu
	}

	if rango.Desde != nil && rango.Hasta != nil && rango.Desde.After(*rango.Hasta) {
		return outbound.RangoFechas{}, apperror.NewValidation(
			"ficha_rango_invalido",
			"el rango de fechas es inválido: desde debe ser anterior o igual a hasta",
		)
	}

	return rango, nil
}

// ListarVentasCliente handles GET /clientes/{id}/ventas.
func (h *Handlers) ListarVentasCliente(ctx context.Context, input *ListarVentasClienteInput) (*ListarVentasClienteOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermClientesLeer); err != nil {
		return nil, err
	}

	page, err := h.svc.ListarVentas(ctx, clientesapp.ListarVentasInput{
		ClienteID: input.ID,
		Pagination: outbound.ListParams{
			Cursor:   input.Cursor,
			PageSize: input.Limit,
		},
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	items := make([]VentaListItemDTO, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, toVentaListItemDTO(v))
	}

	out := &ListarVentasClienteOutput{}
	out.Body.Items = items
	out.Body.NextCursor = page.NextCursor
	return out, nil
}

// ObtenerVentaDetalle handles GET /clientes/{id}/ventas/{doctoPvId}.
func (h *Handlers) ObtenerVentaDetalle(ctx context.Context, input *ObtenerVentaDetalleInput) (*ObtenerVentaDetalleOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermClientesLeer); err != nil {
		return nil, err
	}

	// input.ID (clienteID) only shapes the nested route; the sale is fetched by
	// doctoPvID alone. The office app is admin-scoped (the owner/cobrador sees the
	// whole padrón), so the venta is not cross-checked against the path clienteID.
	detalle, err := h.svc.ObtenerVentaDetalle(ctx, input.DoctoPvID)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &ObtenerVentaDetalleOutput{}
	out.Body = toVentaDetalleDTO(detalle)
	return out, nil
}

// RefrescarBusqueda handles POST /clientes/_search/refresh.
func (h *Handlers) RefrescarBusqueda(ctx context.Context, input *RefrescarBusquedaInput) (*RefrescarBusquedaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermClientesReindexar); err != nil {
		return nil, err
	}

	n, err := h.svc.ReconciliarDirectorio(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &RefrescarBusquedaOutput{}
	out.Body.Reindexado = true
	out.Body.Documentos = n
	return out, nil
}

// ─── Compile-time signature assertions ───────────────────────────────────────
// These blank assignments will fail at compile time if any handler signature
// diverges from the huma.HandlerFunc[I, O] constraint.

var (
	_ func(context.Context, *ListarClientesInput) (*ListarClientesOutput, error)           = (*Handlers)(nil).ListarClientes
	_ func(context.Context, *ObtenerFichaInput) (*ObtenerFichaOutput, error)               = (*Handlers)(nil).ObtenerFicha
	_ func(context.Context, *ListarVentasClienteInput) (*ListarVentasClienteOutput, error) = (*Handlers)(nil).ListarVentasCliente
	_ func(context.Context, *ObtenerVentaDetalleInput) (*ObtenerVentaDetalleOutput, error) = (*Handlers)(nil).ObtenerVentaDetalle
	_ func(context.Context, *RefrescarBusquedaInput) (*RefrescarBusquedaOutput, error)     = (*Handlers)(nil).RefrescarBusqueda
)
