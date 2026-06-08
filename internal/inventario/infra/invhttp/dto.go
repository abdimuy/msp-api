//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp

// ─── Sub-DTOs ───────────────────────────────────────────────────────────────

// TraspasoDetalleResponse is one article line in a TraspasoResponse.
type TraspasoDetalleResponse struct {
	ID         string `json:"id"          doc:"UUID del detalle"           format:"uuid"`
	ArticuloID int    `json:"articulo_id" doc:"ID del artículo en Microsip"`
	Cantidad   string `json:"cantidad"    doc:"Cantidad con 4 decimales, p. ej. \"1.0000\""`
}

// TraspasoResponse is the HTTP projection of a domain.Traspaso (one item).
type TraspasoResponse struct {
	ID             string                    `json:"id"                        doc:"UUID del traspaso"                      format:"uuid"`
	Folio          string                    `json:"folio"                     doc:"Folio del traspaso (p. ej. MSA000001)"`
	DoctoInID      *int                      `json:"docto_in_id,omitempty"     doc:"ID en DOCTOS_IN de Microsip, presente una vez aplicado"`
	AlmacenOrigen  int                       `json:"almacen_origen"            doc:"ID del almacén origen"`
	AlmacenDestino int                       `json:"almacen_destino"           doc:"ID del almacén destino"`
	Fecha          string                    `json:"fecha"                     doc:"Fecha del movimiento (RFC3339 UTC)"`
	Descripcion    string                    `json:"descripcion"               doc:"Descripción del movimiento"`
	VentaID        *string                   `json:"venta_id,omitempty"        doc:"UUID de la venta vinculada, si aplica" format:"uuid"`
	TipoReverso    bool                      `json:"tipo_reverso"              doc:"true cuando este traspaso es el reverso de otro"`
	Detalles       []TraspasoDetalleResponse `json:"detalles"                  doc:"Líneas de artículos del traspaso"`
	CreatedAt      string                    `json:"created_at"                doc:"Fecha de creación (RFC3339 UTC)"`
	CreatedBy      string                    `json:"created_by"                doc:"UUID del usuario que creó el traspaso" format:"uuid"`
}

// StockResponse is the result of a stock query for one artículo/almacén pair.
type StockResponse struct {
	ArticuloID int    `json:"articulo_id" doc:"ID del artículo en Microsip"`
	AlmacenID  int    `json:"almacen_id"  doc:"ID del almacén"`
	Cantidad   string `json:"cantidad"    doc:"Existencia actual con 4 decimales"`
}

// AlmacenResponse is the HTTP projection of a domain.Almacen.
type AlmacenResponse struct {
	ID     int    `json:"id"     doc:"ID del almacén en Microsip"`
	Nombre string `json:"nombre" doc:"Nombre del almacén"`
}

// ─── Input / Output wrappers ────────────────────────────────────────────────

// ObtenerTraspasoInput carries the Microsip DOCTO_IN_ID path parameter.
type ObtenerTraspasoInput struct {
	ID int `path:"id" doc:"ID del traspaso en Microsip (DOCTO_IN_ID)" minimum:"1"`
}

// ObtenerTraspasoOutput wraps the single-traspaso response.
type ObtenerTraspasoOutput struct {
	Body TraspasoResponse
}

// ListarTraspasosPorVentaInput carries the venta_id query parameter.
type ListarTraspasosPorVentaInput struct {
	VentaID string `query:"venta_id" doc:"UUID de la venta cuyos traspasos se consultan" required:"true" format:"uuid"`
}

// ListarTraspasosPorVentaOutput wraps the list of traspasos.
type ListarTraspasosPorVentaOutput struct {
	Body struct {
		Items []TraspasoResponse `json:"items"`
	}
}

// ConsultarStockInput carries the artículo and almacén query parameters.
type ConsultarStockInput struct {
	ArticuloID int `query:"articulo_id" doc:"ID del artículo en Microsip" required:"true" minimum:"1"`
	AlmacenID  int `query:"almacen_id"  doc:"ID del almacén"              required:"true" minimum:"1"`
}

// ConsultarStockOutput wraps the stock response.
type ConsultarStockOutput struct {
	Body StockResponse
}

// ListarAlmacenesOutput wraps the almacenes catalog.
type ListarAlmacenesOutput struct {
	Body struct {
		Items []AlmacenResponse `json:"items"`
	}
}
