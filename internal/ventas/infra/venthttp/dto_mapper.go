//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// moneyScale is the number of decimal places enforced for every monetary
// field in the response. Firebird stores money columns as NUMERIC(14,2);
// decimal.Decimal.String() strips trailing zeros (so "100.00" becomes "100")
// which breaks downstream clients that expect a fixed-precision string. Use
// StringFixed(moneyScale) for every money projection.
const moneyScale int32 = 2

// cantidadScale is the precision of MSP_VENTAS_PRODUCTOS.CANTIDAD
// (NUMERIC(10,4)). Quantities round-trip with this exact scale.
const cantidadScale int32 = 4

// toVentaDTO projects a domain.Venta into its JSON DTO. Decimal values use
// StringFixed so the response carries a stable, scale-correct representation
// every time — clients parsing "1234.56" never see the value drift to "1234".
func toVentaDTO(v *domain.Venta) VentaDTO {
	a := v.Audit()
	dto := VentaDTO{
		ID:                 v.ID().String(),
		Cliente:            toClienteSnapshotDTO(v.ClienteID(), v.Cliente()),
		Direccion:          toDireccionDTO(v.Direccion()),
		GPS:                toGPSDTO(v.GPS()),
		FechaVenta:         formatTime(v.FechaVenta()),
		TipoVenta:          v.TipoVenta().String(),
		Estado:             v.Estado().String(),
		Situacion:          v.Situacion().String(),
		Sincronizacion:     v.Sincronizacion().String(),
		MicrosipFolio:      v.MicrosipFolio(),
		MicrosipDoctoPVID:  v.MicrosipDoctoPVID(),
		MicrosipAplicadaAt: formatTimePtr(v.MicrosipAplicadaAt()),
		Montos:             toMontosDTO(v.Montos()),
		PlanCredito:        toPlanCreditoDTO(v.PlanCredito()),
		DiaCobranza:        toDiaCobranzaDTO(v.DiaCobranza()),
		Nota:               v.Nota(),
		Combos:             toCombosDTO(v),
		Productos:          toProductosDTO(v),
		Vendedores:         toVendedoresDTO(v),
		Imagenes:           toImagenesDTO(v),
		Cancelacion:        toCancelacionDTO(v.Cancelacion()),
		Aprobacion:         toAprobacionDTO(v.Aprobacion()),
		CreatedAt:          formatTime(a.CreatedAt()),
		UpdatedAt:          formatTime(a.UpdatedAt()),
		CreatedBy:          a.CreatedBy().String(),
		UpdatedBy:          a.UpdatedBy().String(),
	}
	return dto
}

// toClienteSnapshotDTO projects the embedded cliente snapshot together with
// the optional Microsip cliente_id link.
func toClienteSnapshotDTO(clienteID *int, c domain.ClienteSnapshot) ClienteSnapshotDTO {
	var tel *string
	if t := c.Telefono(); t != nil {
		v := t.Value()
		tel = &v
	}
	var aval *string
	if a := c.Aval(); a != nil {
		v := a.Value()
		aval = &v
	}
	return ClienteSnapshotDTO{
		ClienteID: clienteID,
		Nombre:    c.Nombre().Value(),
		Telefono:  tel,
		Aval:      aval,
	}
}

// toDireccionDTO projects the postal-address snapshot.
func toDireccionDTO(d domain.Direccion) DireccionDTO {
	return DireccionDTO{
		Calle:          d.Calle(),
		NumeroExterior: d.NumeroExterior(),
		Colonia:        d.Colonia(),
		Poblacion:      d.Poblacion(),
		Ciudad:         d.Ciudad(),
		ZonaClienteID:  d.ZonaClienteID(),
	}
}

// toGPSDTO projects the GPS coordinates.
func toGPSDTO(g domain.GPSCoords) GPSDTO {
	return GPSDTO{Latitud: g.Latitud(), Longitud: g.Longitud()}
}

// toMontosDTO projects the three-price snapshot.
func toMontosDTO(m domain.MontoSnapshot) MontosDTO {
	return MontosDTO{
		Anual:      m.Anual().StringFixed(moneyScale),
		CortoPlazo: m.CortoPlazo().StringFixed(moneyScale),
		Contado:    m.Contado().StringFixed(moneyScale),
	}
}

// toPlanCreditoDTO projects the optional credit-plan VO. Returns nil when
// the source is nil.
func toPlanCreditoDTO(p *domain.PlanCredito) *PlanCreditoDTO {
	if p == nil {
		return nil
	}
	return &PlanCreditoDTO{
		PlazoMeses:  p.PlazoMeses(),
		Enganche:    p.Enganche().StringFixed(moneyScale),
		Parcialidad: p.Parcialidad().StringFixed(moneyScale),
		FrecPago:    p.FrecPago().String(),
	}
}

// toDiaCobranzaDTO projects the optional cobranza-day VO.
func toDiaCobranzaDTO(d *domain.DiaCobranza) *DiaCobranzaDTO {
	if d == nil {
		return nil
	}
	var semana *string
	if s := d.Semana(); s != nil {
		v := s.String()
		semana = &v
	}
	return &DiaCobranzaDTO{Semana: semana, Mes: d.Mes()}
}

// toCombosDTO projects every combo child of v.
func toCombosDTO(v *domain.Venta) []ComboDTO {
	out := make([]ComboDTO, 0, v.CombosCount())
	for c := range v.Combos() {
		out = append(out, toComboDTO(c))
	}
	return out
}

// toComboDTO projects a single combo.
func toComboDTO(c *domain.Combo) ComboDTO {
	pr := c.Precios()
	return ComboDTO{
		ID:               c.ID().String(),
		Nombre:           c.Nombre(),
		PrecioAnual:      pr.Anual().StringFixed(moneyScale),
		PrecioCorto:      pr.CortoPlazo().StringFixed(moneyScale),
		PrecioContado:    pr.Contado().StringFixed(moneyScale),
		Cantidad:         c.Cantidad().StringFixed(cantidadScale),
		AlmacenOrigenID:  c.AlmacenOrigen(),
		AlmacenDestinoID: c.AlmacenDestino(),
	}
}

// toProductosDTO projects every producto child of v.
func toProductosDTO(v *domain.Venta) []ProductoDTO {
	out := make([]ProductoDTO, 0, v.ProductosCount())
	for p := range v.Productos() {
		out = append(out, toProductoDTO(p))
	}
	return out
}

// toProductoDTO projects a single producto.
func toProductoDTO(p *domain.Producto) ProductoDTO {
	pr := p.Precios()
	var comboID *string
	if c := p.ComboID(); c != nil {
		v := c.String()
		comboID = &v
	}
	return ProductoDTO{
		ID:               p.ID().String(),
		ArticuloID:       p.ArticuloID(),
		Articulo:         p.Articulo(),
		Cantidad:         p.Cantidad().StringFixed(cantidadScale),
		PrecioAnual:      pr.Anual().StringFixed(moneyScale),
		PrecioCorto:      pr.CortoPlazo().StringFixed(moneyScale),
		PrecioContado:    pr.Contado().StringFixed(moneyScale),
		ComboID:          comboID,
		AlmacenOrigenID:  p.AlmacenOrigen(),
		AlmacenDestinoID: p.AlmacenDestino(),
	}
}

// toVendedoresDTO projects every vendedor child of v.
func toVendedoresDTO(v *domain.Venta) []VendedorDTO {
	out := make([]VendedorDTO, 0, v.VendedoresCount())
	for vd := range v.Vendedores() {
		out = append(out, toVendedorDTO(vd))
	}
	return out
}

// toVendedorDTO projects a single vendedor.
func toVendedorDTO(v *domain.Vendedor) VendedorDTO {
	s := v.Snapshot()
	return VendedorDTO{
		ID:        v.ID().String(),
		UsuarioID: s.UsuarioID().String(),
		Email:     s.Email(),
		Nombre:    s.Nombre(),
	}
}

// toImagenesDTO projects every imagen child of v.
func toImagenesDTO(v *domain.Venta) []ImagenDTO {
	out := make([]ImagenDTO, 0, v.ImagenesCount())
	for i := range v.Imagenes() {
		out = append(out, toImagenDTO(i))
	}
	return out
}

// toImagenDTO projects a single imagen.
func toImagenDTO(i *domain.Imagen) ImagenDTO {
	a := i.Audit()
	return ImagenDTO{
		ID:          i.ID().String(),
		StorageKind: i.Storage().Kind().String(),
		StorageKey:  i.Storage().Key(),
		Mime:        i.Mime(),
		SizeBytes:   i.SizeBytes(),
		Descripcion: i.Descripcion(),
		CreatedAt:   formatTime(a.CreatedAt()),
		UpdatedAt:   formatTime(a.UpdatedAt()),
		CreatedBy:   a.CreatedBy().String(),
		UpdatedBy:   a.UpdatedBy().String(),
	}
}

// toCancelacionDTO projects the optional cancellation record.
func toCancelacionDTO(c *domain.Cancelacion) *CancelacionDTO {
	if c == nil {
		return nil
	}
	return &CancelacionDTO{
		At:     formatTime(c.At()),
		By:     c.By().String(),
		Reason: c.Reason(),
	}
}

// toAprobacionDTO projects the optional approval record.
func toAprobacionDTO(a *domain.Aprobacion) *AprobacionDTO {
	if a == nil {
		return nil
	}
	return &AprobacionDTO{
		At: formatTime(a.At()),
		By: a.By().String(),
	}
}

// formatTime renders a timestamp as RFC3339Nano in UTC. Zero values map to
// the empty string so optional fields stay omitted by the JSON encoder.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// formatTimePtr renders an optional timestamp as RFC3339Nano in UTC, or nil
// when the pointer is nil (so the field is omitted from the JSON response).
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}
