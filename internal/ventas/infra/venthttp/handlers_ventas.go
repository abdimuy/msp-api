//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// Handlers groups every Huma handler for the ventas module.
type Handlers struct {
	svc *ventasapp.Service
}

// NewHandlers wires a Handlers with its application service dependency.
func NewHandlers(svc *ventasapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// CrearVenta is the handler for POST /v2/ventas.
//
// Acepta multipart/form-data: campo `datos` (JSON con la venta) + uno o
// más campos `imagen` (archivos). Persiste venta + evidencias en una sola
// tx Firebird vía [ventasapp.Service.CrearVentaConImagenes]; al menos una
// imagen es OBLIGATORIA — toda venta del showroom lleva firma o ID del
// cliente.
//
// El endpoint legacy POST /v2/ventas/{id}/imagenes se mantiene para
// adjuntar evidencia adicional después del POST inicial atómico.
func (h *Handlers) CrearVenta(ctx context.Context, in *CrearVentaInput) (*CrearVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasCrear); err != nil {
		return nil, err
	}

	fields := in.RawBody.Data()
	body, err := decodeCrearVentaDatos(fields.Datos)
	if err != nil {
		return nil, mapAppError(err)
	}
	// Idempotency-Key (if present) is a free-form cache key consumed by the
	// platform idempotency middleware before the request reaches this
	// handler — we do NOT cross-check it against datos.id (unlike cobranza,
	// which uses datos.id as the canonical end-to-end dedupe key).

	input, err := crearVentaBodyToAppInput(body)
	if err != nil {
		return nil, mapAppError(err)
	}

	ventaID, err := uuid.Parse(body.ID)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("venta_id_invalido", "el id de la venta no es un UUID válido").WithError(err),
		)
	}

	imgUploads, openedFiles, err := parseImagenesFromMultipart(ventaID, fields.Imagen, in.RawBody.Form)
	defer func() {
		for _, f := range openedFiles {
			_ = f.Close()
		}
	}()
	if err != nil {
		return nil, mapAppError(err)
	}

	v, err := h.svc.CrearVentaConImagenes(ctx, input, imgUploads, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CrearVentaOutput{Body: toVentaDTO(v)}, nil
}

// decodeCrearVentaDatos validates that `datos` was supplied and parses it
// as JSON into CrearVentaBody. Returns stable apperror codes so the client
// knows whether the field was missing vs. malformed.
func decodeCrearVentaDatos(raw string) (CrearVentaBody, error) {
	if raw == "" {
		return CrearVentaBody{}, apperror.NewValidation(
			"datos_requerido", "el campo multipart 'datos' es obligatorio",
		)
	}
	var body CrearVentaBody
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&body); err != nil {
		return CrearVentaBody{}, apperror.NewValidation(
			"datos_json_invalido", "el campo 'datos' no es un JSON válido",
		).WithError(err)
	}
	return body, nil
}

// ObtenerVenta is the handler for GET /v2/ventas/{id}.
func (h *Handlers) ObtenerVenta(ctx context.Context, in *ObtenerVentaInput) (*ObtenerVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasVer); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.ObtenerVenta(ctx, id)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ObtenerVentaOutput{Body: toVentaDTO(v)}, nil
}

// CancelarVenta is the handler for PATCH /v2/ventas/{id}/cancel.
func (h *Handlers) CancelarVenta(ctx context.Context, in *CancelarVentaInput) (*CancelarVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasCancelar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.CancelarVenta(ctx, id, in.Body.Reason, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CancelarVentaOutput{Body: toVentaDTO(v)}, nil
}

// ListarVentas is the handler for GET /v2/ventas.
func (h *Handlers) ListarVentas(ctx context.Context, in *ListarVentasInput) (*ListarVentasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasListar); err != nil {
		return nil, err
	}
	filters, err := buildListarFilters(in)
	if err != nil {
		return nil, mapAppError(err)
	}
	page, err := h.svc.ListarVentas(ctx, ventasapp.ListarVentasInput{
		Pagination: outbound.ListParams{Cursor: in.Cursor, PageSize: in.Limit},
		Filters:    filters,
	})
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]VentaDTO, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, toVentaDTO(v))
	}
	return &ListarVentasOutput{Body: ListResponse[VentaDTO]{Items: items, NextCursor: page.NextCursor}}, nil
}

// buildListarFilters parses the optional query parameters of a list request
// into the typed filter struct the service consumes. Empty strings are
// treated as "filter not supplied".
func buildListarFilters(in *ListarVentasInput) (outbound.ListVentasFilters, error) {
	filters := outbound.ListVentasFilters{
		TipoVenta:         in.TipoVenta,
		IncluirCanceladas: in.IncluirCanceladas,
	}
	if in.Desde != "" {
		t, err := time.Parse(time.RFC3339, in.Desde)
		if err != nil {
			return outbound.ListVentasFilters{}, apperror.NewValidation(
				"invalid_desde", "el parámetro desde no es una fecha ISO8601 válida",
			).WithError(err)
		}
		filters.Desde = &t
	}
	if in.Hasta != "" {
		t, err := time.Parse(time.RFC3339, in.Hasta)
		if err != nil {
			return outbound.ListVentasFilters{}, apperror.NewValidation(
				"invalid_hasta", "el parámetro hasta no es una fecha ISO8601 válida",
			).WithError(err)
		}
		filters.Hasta = &t
	}
	if in.VendedorUsuarioID != "" {
		id, err := uuid.Parse(in.VendedorUsuarioID)
		if err != nil {
			return outbound.ListVentasFilters{}, apperror.NewValidation(
				"invalid_uuid", "el parámetro vendedor_usuario_id no es un UUID válido",
			).WithError(err)
		}
		filters.VendedorUsuarioID = &id
	}
	if in.ClienteID != "" {
		n, err := strconv.Atoi(in.ClienteID)
		if err != nil || n <= 0 {
			return outbound.ListVentasFilters{}, apperror.NewValidation(
				"invalid_cliente_id", "el parámetro cliente_id debe ser un entero positivo",
			).WithError(err)
		}
		filters.ClienteID = &n
	}
	return filters, nil
}

// parseUUIDField parses a string into a uuid.UUID with a stable apperror.
func parseUUIDField(raw, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(
			"invalid_uuid", "el identificador en la URL no es un UUID válido",
		).WithField("param", name).WithError(err)
	}
	return id, nil
}

// parseDecimalField parses a numeric string into a decimal.Decimal with a
// stable apperror tagged by the field name.
func parseDecimalField(raw, name string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, apperror.NewValidation(
			"invalid_decimal", "el valor numérico no es válido",
		).WithField("field", name).WithError(err)
	}
	return d, nil
}

// parseTimeField parses an RFC3339 timestamp into a time.Time with a stable
// apperror tagged by the field name.
func parseTimeField(raw, name string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, apperror.NewValidation(
			"invalid_datetime", "el valor de fecha-hora no es ISO8601 válido",
		).WithField("field", name).WithError(err)
	}
	return t, nil
}

// crearVentaBodyToAppInput translates the JSON body DTO into the app input.
func crearVentaBodyToAppInput(b CrearVentaBody) (ventasapp.CrearVentaInput, error) {
	id, err := parseUUIDField(b.ID, "id")
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	fecha, err := parseTimeField(b.FechaVenta, "fecha_venta")
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	montos, err := parseMontos(b.Montos)
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	plan, err := parsePlanCreditoDTO(b.PlanCredito)
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	combos, err := parseCombosDTO(b.Combos)
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	productos, err := parseProductosDTO(b.Productos)
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	vendedores, err := parseVendedoresDTO(b.Vendedores)
	if err != nil {
		return ventasapp.CrearVentaInput{}, err
	}
	out := ventasapp.CrearVentaInput{
		ID:             id,
		ClienteID:      b.Cliente.ClienteID,
		ClienteNombre:  b.Cliente.Nombre,
		ClienteTel:     b.Cliente.Telefono,
		ClienteAval:    b.Cliente.Aval,
		Calle:          b.Direccion.Calle,
		NumeroExterior: b.Direccion.NumeroExterior,
		Colonia:        b.Direccion.Colonia,
		Poblacion:      b.Direccion.Poblacion,
		Ciudad:         b.Direccion.Ciudad,
		ZonaClienteID:  b.Direccion.ZonaClienteID,
		Latitud:        b.GPS.Latitud,
		Longitud:       b.GPS.Longitud,
		FechaVenta:     fecha,
		TipoVenta:      b.TipoVenta,
		PrecioAnual:    montos.anual,
		PrecioCorto:    montos.cortoPlazo,
		PrecioContado:  montos.contado,
		PlanCredito:    plan,
		DiaCobranza:    dtoToAppDiaCobranza(b.DiaCobranza),
		Nota:           b.Nota,
		Combos:         combos,
		Productos:      productos,
		Vendedores:     vendedores,
	}
	return out, nil
}

// montosTriple is an internal helper holding the three parsed monto decimals.
type montosTriple struct {
	anual      decimal.Decimal
	cortoPlazo decimal.Decimal
	contado    decimal.Decimal
}

// parseMontos parses the three top-level monto strings into decimals.
func parseMontos(m MontosDTO) (montosTriple, error) {
	anual, err := parseDecimalField(m.Anual, "montos.anual")
	if err != nil {
		return montosTriple{}, err
	}
	corto, err := parseDecimalField(m.CortoPlazo, "montos.corto_plazo")
	if err != nil {
		return montosTriple{}, err
	}
	contado, err := parseDecimalField(m.Contado, "montos.contado")
	if err != nil {
		return montosTriple{}, err
	}
	return montosTriple{anual: anual, cortoPlazo: corto, contado: contado}, nil
}

// parsePlanCreditoDTO parses the optional plan-credito block.
func parsePlanCreditoDTO(p *PlanCreditoDTO) (*ventasapp.CrearVentaPlanCreditoInput, error) {
	if p == nil {
		return nil, nil //nolint:nilnil // optional value.
	}
	enganche, err := parseDecimalField(p.Enganche, "plan_credito.enganche")
	if err != nil {
		return nil, err
	}
	parc, err := parseDecimalField(p.Parcialidad, "plan_credito.parcialidad")
	if err != nil {
		return nil, err
	}
	return &ventasapp.CrearVentaPlanCreditoInput{
		PlazoMeses:  p.PlazoMeses,
		Enganche:    enganche,
		Parcialidad: parc,
		FrecPago:    p.FrecPago,
	}, nil
}

// dtoToAppDiaCobranza translates the optional dia-cobranza DTO into the app
// shape. Returns nil when the DTO is absent — the domain validator enforces
// the rule against TipoVenta.
func dtoToAppDiaCobranza(d *DiaCobranzaDTO) *ventasapp.CrearVentaDiaCobranzaInput {
	if d == nil {
		return nil
	}
	return &ventasapp.CrearVentaDiaCobranzaInput{Semana: d.Semana, Mes: d.Mes}
}

// parseCombosDTO translates the JSON combos slice into the app shape.
func parseCombosDTO(in []ComboDTO) ([]ventasapp.CrearVentaComboInput, error) {
	out := make([]ventasapp.CrearVentaComboInput, 0, len(in))
	for i, c := range in {
		comboID, err := parseUUIDField(c.ID, fieldRef("combos", i, "id"))
		if err != nil {
			return nil, err
		}
		anual, err := parseDecimalField(c.PrecioAnual, fieldRef("combos", i, "precio_anual"))
		if err != nil {
			return nil, err
		}
		corto, err := parseDecimalField(c.PrecioCorto, fieldRef("combos", i, "precio_corto"))
		if err != nil {
			return nil, err
		}
		contado, err := parseDecimalField(c.PrecioContado, fieldRef("combos", i, "precio_contado"))
		if err != nil {
			return nil, err
		}
		cantidad, err := parseDecimalField(c.Cantidad, fieldRef("combos", i, "cantidad"))
		if err != nil {
			return nil, err
		}
		out = append(out, ventasapp.CrearVentaComboInput{
			ID: comboID, Nombre: c.Nombre,
			PrecioAnual: anual, PrecioCorto: corto, PrecioContado: contado,
			Cantidad:       cantidad,
			AlmacenOrigen:  c.AlmacenOrigenID,
			AlmacenDestino: c.AlmacenDestinoID,
		})
	}
	return out, nil
}

// parseProductosDTO translates the JSON productos slice into the app shape.
func parseProductosDTO(in []ProductoDTO) ([]ventasapp.CrearVentaProductoInput, error) {
	out := make([]ventasapp.CrearVentaProductoInput, 0, len(in))
	for i, p := range in {
		row, err := parseProductoDTO(p, i)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// parseProductoDTO translates a single producto row.
func parseProductoDTO(p ProductoDTO, idx int) (ventasapp.CrearVentaProductoInput, error) {
	id, err := parseUUIDField(p.ID, fieldRef("productos", idx, "id"))
	if err != nil {
		return ventasapp.CrearVentaProductoInput{}, err
	}
	cantidad, err := parseDecimalField(p.Cantidad, fieldRef("productos", idx, "cantidad"))
	if err != nil {
		return ventasapp.CrearVentaProductoInput{}, err
	}
	anual, err := parseDecimalField(p.PrecioAnual, fieldRef("productos", idx, "precio_anual"))
	if err != nil {
		return ventasapp.CrearVentaProductoInput{}, err
	}
	corto, err := parseDecimalField(p.PrecioCorto, fieldRef("productos", idx, "precio_corto"))
	if err != nil {
		return ventasapp.CrearVentaProductoInput{}, err
	}
	contado, err := parseDecimalField(p.PrecioContado, fieldRef("productos", idx, "precio_contado"))
	if err != nil {
		return ventasapp.CrearVentaProductoInput{}, err
	}
	var comboID *uuid.UUID
	if p.ComboID != nil && *p.ComboID != "" {
		v, parseErr := parseUUIDField(*p.ComboID, fieldRef("productos", idx, "combo_id"))
		if parseErr != nil {
			return ventasapp.CrearVentaProductoInput{}, parseErr
		}
		comboID = &v
	}
	return ventasapp.CrearVentaProductoInput{
		ID: id, ArticuloID: p.ArticuloID, Articulo: p.Articulo,
		Cantidad: cantidad, PrecioAnual: anual, PrecioCorto: corto, PrecioContado: contado,
		ComboID:        comboID,
		AlmacenOrigen:  p.AlmacenOrigenID,
		AlmacenDestino: p.AlmacenDestinoID,
	}, nil
}

// parseVendedoresDTO translates the JSON vendedores slice into the app shape.
func parseVendedoresDTO(in []VendedorDTO) ([]ventasapp.CrearVentaVendedorInput, error) {
	out := make([]ventasapp.CrearVentaVendedorInput, 0, len(in))
	for i, v := range in {
		vID, err := parseUUIDField(v.ID, fieldRef("vendedores", i, "id"))
		if err != nil {
			return nil, err
		}
		uID, err := parseUUIDField(v.UsuarioID, fieldRef("vendedores", i, "usuario_id"))
		if err != nil {
			return nil, err
		}
		out = append(out, ventasapp.CrearVentaVendedorInput{
			ID: vID, UsuarioID: uID, Email: v.Email, Nombre: v.Nombre,
		})
	}
	return out, nil
}

// fieldRef builds a JSON-pointer-like label for error reporting.
func fieldRef(arr string, idx int, leaf string) string {
	return arr + "[" + strconv.Itoa(idx) + "]." + leaf
}

// RevisarVenta is the handler for POST /v2/ventas/{id}/revisar.
func (h *Handlers) RevisarVenta(ctx context.Context, in *RevisarVentaInput) (*RevisarVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasRevisar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.EnviarARevision(ctx, id, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &RevisarVentaOutput{Body: toVentaDTO(v)}, nil
}

// AprobarVenta is the handler for POST /v2/ventas/{id}/aprobar.
func (h *Handlers) AprobarVenta(ctx context.Context, in *AprobarVentaInput) (*AprobarVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasAprobar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.Aprobar(ctx, id, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &AprobarVentaOutput{Body: toVentaDTO(v)}, nil
}

// RegresarBorradorVenta is the handler for POST /v2/ventas/{id}/regresar-borrador.
func (h *Handlers) RegresarBorradorVenta(ctx context.Context, in *RegresarBorradorVentaInput) (*RegresarBorradorVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasAprobar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.RegresarABorrador(ctx, id, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &RegresarBorradorVentaOutput{Body: toVentaDTO(v)}, nil
}

// AplicarVenta is the handler for POST /v2/ventas/{id}/aplicar.
func (h *Handlers) AplicarVenta(ctx context.Context, in *AplicarVentaInput) (*AplicarVentaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasAplicar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	v, err := h.svc.AplicarVenta(ctx, id, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &AplicarVentaOutput{Body: toVentaDTO(v)}, nil
}

// Compile-time assertions: handler signatures match Huma's expected shape.
var (
	_ func(context.Context, *CrearVentaInput) (*CrearVentaOutput, error)                       = (*Handlers)(nil).CrearVenta
	_ func(context.Context, *ObtenerVentaInput) (*ObtenerVentaOutput, error)                   = (*Handlers)(nil).ObtenerVenta
	_ func(context.Context, *CancelarVentaInput) (*CancelarVentaOutput, error)                 = (*Handlers)(nil).CancelarVenta
	_ func(context.Context, *ListarVentasInput) (*ListarVentasOutput, error)                   = (*Handlers)(nil).ListarVentas
	_ func(context.Context, *RevisarVentaInput) (*RevisarVentaOutput, error)                   = (*Handlers)(nil).RevisarVenta
	_ func(context.Context, *AprobarVentaInput) (*AprobarVentaOutput, error)                   = (*Handlers)(nil).AprobarVenta
	_ func(context.Context, *RegresarBorradorVentaInput) (*RegresarBorradorVentaOutput, error) = (*Handlers)(nil).RegresarBorradorVenta
	_ func(context.Context, *AplicarVentaInput) (*AplicarVentaOutput, error)                   = (*Handlers)(nil).AplicarVenta
)
