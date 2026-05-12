// Package venthttp hosts the ventas module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount point. It is the outermost
// adapter layer — nothing inside the ventas module imports it.
//
//nolint:misspell // ventas vocabulary is Spanish (combos, productos, etc.) per project convention.
package venthttp

import (
	"github.com/danielgtaylor/huma/v2"
)

// ─── Sub-DTOs ───────────────────────────────────────────────────────────────

// ClienteSnapshotDTO mirrors the embedded cliente snapshot of a venta.
type ClienteSnapshotDTO struct {
	Nombre   string  `json:"nombre"             doc:"Nombre del cliente"`
	Telefono *string `json:"telefono,omitempty" doc:"Teléfono opcional, formato libre"`
	Aval     *string `json:"aval,omitempty"     doc:"Aval o responsable opcional"`
}

// DireccionDTO mirrors the postal-address snapshot.
type DireccionDTO struct {
	Calle          string  `json:"calle"`
	NumeroExterior *string `json:"numero_exterior,omitempty"`
	Colonia        string  `json:"colonia"`
	Poblacion      string  `json:"poblacion"`
	Ciudad         string  `json:"ciudad"`
	ZonaClienteID  *int    `json:"zona_cliente_id,omitempty"`
}

// GPSDTO captures latitud/longitud as floats.
type GPSDTO struct {
	Latitud  float64 `json:"latitud"  minimum:"-90"  maximum:"90"`
	Longitud float64 `json:"longitud" minimum:"-180" maximum:"180"`
}

// MontosDTO mirrors the three-price MontoSnapshot. Decimal values flow as
// strings so JSON parsing does not lose precision.
type MontosDTO struct {
	Anual      string `json:"anual"       doc:"Precio del plan anual"`
	CortoPlazo string `json:"corto_plazo" doc:"Precio del plan a corto plazo"`
	Contado    string `json:"contado"     doc:"Precio de contado"`
}

// PlanCreditoDTO mirrors the optional credit-plan VO.
type PlanCreditoDTO struct {
	PlazoMeses  int    `json:"plazo_meses"  minimum:"1"`
	Enganche    string `json:"enganche"`
	Parcialidad string `json:"parcialidad"`
	FrecPago    string `json:"frec_pago"    enum:"SEMANAL,QUINCENAL,MENSUAL"`
}

// DiaCobranzaDTO mirrors the optional cobranza-day VO. Exactly one of
// semana/mes must be set when supplied.
type DiaCobranzaDTO struct {
	Semana *string `json:"semana,omitempty" enum:"LUNES,MARTES,MIERCOLES,JUEVES,VIERNES,SABADO,DOMINGO"`
	Mes    *int    `json:"mes,omitempty"    minimum:"1" maximum:"31"`
}

// ComboDTO is one combo line in the request/response.
type ComboDTO struct {
	ID            string `json:"id"             format:"uuid"`
	Nombre        string `json:"nombre"`
	PrecioAnual   string `json:"precio_anual"`
	PrecioCorto   string `json:"precio_corto"`
	PrecioContado string `json:"precio_contado"`
}

// ProductoDTO is one producto line in the request/response.
type ProductoDTO struct {
	ID            string  `json:"id"                  format:"uuid"`
	ArticuloID    int     `json:"articulo_id"`
	Articulo      string  `json:"articulo"`
	Cantidad      string  `json:"cantidad"            doc:"Cantidad decimal, p. ej. \"1.5\""`
	PrecioAnual   string  `json:"precio_anual"`
	PrecioCorto   string  `json:"precio_corto"`
	PrecioContado string  `json:"precio_contado"`
	ComboID       *string `json:"combo_id,omitempty"  format:"uuid"`
}

// VendedorDTO is one vendedor row in the request/response.
type VendedorDTO struct {
	ID        string `json:"id"         format:"uuid"`
	UsuarioID string `json:"usuario_id" format:"uuid"`
	Email     string `json:"email"      format:"email"`
	Nombre    string `json:"nombre"`
}

// ImagenDTO is one imagen child in the response.
type ImagenDTO struct {
	ID          string  `json:"id"                    format:"uuid"`
	StorageKind string  `json:"storage_kind"          enum:"FILESYSTEM"`
	StorageKey  string  `json:"storage_key"`
	Mime        string  `json:"mime"`
	SizeBytes   int64   `json:"size_bytes"`
	Descripcion *string `json:"descripcion,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CreatedBy   string  `json:"created_by"            format:"uuid"`
	UpdatedBy   string  `json:"updated_by"            format:"uuid"`
}

// CancelacionDTO mirrors the cancellation record on a venta.
type CancelacionDTO struct {
	At     string `json:"at"`
	By     string `json:"by" format:"uuid"`
	Reason string `json:"reason"`
}

// VentaDTO is the full projection of a domain.Venta.
type VentaDTO struct {
	ID             string             `json:"id"                       format:"uuid"`
	Cliente        ClienteSnapshotDTO `json:"cliente"`
	Direccion      DireccionDTO       `json:"direccion"`
	GPS            GPSDTO             `json:"gps"`
	AlmacenOrigen  int                `json:"almacen_origen"`
	AlmacenDestino int                `json:"almacen_destino"`
	FechaVenta     string             `json:"fecha_venta"`
	TipoVenta      string             `json:"tipo_venta"               enum:"CONTADO,CREDITO"`
	Montos         MontosDTO          `json:"montos"`
	PlanCredito    *PlanCreditoDTO    `json:"plan_credito,omitempty"`
	DiaCobranza    *DiaCobranzaDTO    `json:"dia_cobranza,omitempty"`
	Nota           *string            `json:"nota,omitempty"`
	Combos         []ComboDTO         `json:"combos"`
	Productos      []ProductoDTO      `json:"productos"`
	Vendedores     []VendedorDTO      `json:"vendedores"`
	Imagenes       []ImagenDTO        `json:"imagenes"`
	Cancelacion    *CancelacionDTO    `json:"cancelacion,omitempty"`
	CreatedAt      string             `json:"created_at"`
	UpdatedAt      string             `json:"updated_at"`
	CreatedBy      string             `json:"created_by"               format:"uuid"`
	UpdatedBy      string             `json:"updated_by"               format:"uuid"`
}

// ─── Request bodies ─────────────────────────────────────────────────────────

// CrearVentaBody is the JSON body for POST /v2/ventas.
type CrearVentaBody struct {
	ID             string             `json:"id"                       format:"uuid"`
	Cliente        ClienteSnapshotDTO `json:"cliente"`
	Direccion      DireccionDTO       `json:"direccion"`
	GPS            GPSDTO             `json:"gps"`
	AlmacenOrigen  int                `json:"almacen_origen"`
	AlmacenDestino int                `json:"almacen_destino"`
	FechaVenta     string             `json:"fecha_venta"              format:"date-time"`
	TipoVenta      string             `json:"tipo_venta"               enum:"CONTADO,CREDITO"`
	Montos         MontosDTO          `json:"montos"`
	PlanCredito    *PlanCreditoDTO    `json:"plan_credito,omitempty"`
	DiaCobranza    *DiaCobranzaDTO    `json:"dia_cobranza,omitempty"`
	Nota           *string            `json:"nota,omitempty"`
	Combos         []ComboDTO         `json:"combos"`
	Productos      []ProductoDTO      `json:"productos"                minItems:"1"`
	Vendedores     []VendedorDTO      `json:"vendedores"               minItems:"1"`
}

// CancelarVentaBody is the JSON body for PATCH /v2/ventas/{id}/cancel.
type CancelarVentaBody struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500"`
}

// ─── Input wrappers (Huma reads tags off these) ────────────────────────────

// CrearVentaInput wraps the request body and idempotency header.
type CrearVentaInput struct {
	IdempotencyKey string `header:"Idempotency-Key" doc:"Idempotency key opcional"`
	Body           CrearVentaBody
}

// CrearVentaOutput is the response wrapper.
type CrearVentaOutput struct {
	Body VentaDTO
}

// ObtenerVentaInput carries the path parameter.
type ObtenerVentaInput struct {
	ID string `path:"id" format:"uuid"`
}

// ObtenerVentaOutput is the response wrapper.
type ObtenerVentaOutput struct {
	Body VentaDTO
}

// CancelarVentaInput carries the path parameter and reason body.
type CancelarVentaInput struct {
	ID             string `path:"id"                format:"uuid"`
	IdempotencyKey string `header:"Idempotency-Key" doc:"Idempotency key opcional"`
	Body           CancelarVentaBody
}

// CancelarVentaOutput is the response wrapper.
type CancelarVentaOutput struct {
	Body VentaDTO
}

// ListarVentasInput collects the cursor-pagination + filter query params.
// Optional filters use plain string values that are treated as "absent" when
// empty — Huma does not support pointer-typed query parameters.
type ListarVentasInput struct {
	Cursor            string `query:"cursor"                                                       doc:"Cursor opaco devuelto por la página anterior"`
	Limit             int    `query:"limit"               default:"50" minimum:"1" maximum:"500"`
	Desde             string `query:"desde"               format:"date-time"                       doc:"Filtra FECHA_VENTA >= desde"`
	Hasta             string `query:"hasta"               format:"date-time"                       doc:"Filtra FECHA_VENTA < hasta"`
	VendedorUsuarioID string `query:"vendedor_usuario_id" format:"uuid"                            doc:"Restringe a ventas con este vendedor"`
	TipoVenta         string `query:"tipo_venta"          enum:"CONTADO,CREDITO"                   doc:"Filtra por tipo de venta"`
	IncluirCanceladas bool   `query:"incluir_canceladas"                                           doc:"Incluye ventas canceladas en el resultado"`
}

// ListarVentasOutput is the cursor-paginated response.
type ListarVentasOutput struct {
	Body ListResponse[VentaDTO]
}

// ListResponse is the generic cursor-paginated envelope.
type ListResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ─── Imagenes — multipart upload ───────────────────────────────────────────

// ImagenUploadFields is the set of typed multipart fields accepted by
// POST /v2/ventas/{id}/imagenes. Huma populates this from the request via
// MultipartFormFiles[T].
type ImagenUploadFields struct {
	File        huma.FormFile `form:"file"        contentType:"image/jpeg,image/png,image/gif,image/webp"`
	Descripcion string        `form:"descripcion" required:"false" doc:"Descripción opcional"`
}

// AdjuntarImagenInput carries the venta id path param and the multipart body.
type AdjuntarImagenInput struct {
	ID      string `path:"id" format:"uuid"`
	RawBody huma.MultipartFormFiles[ImagenUploadFields]
}

// AdjuntarImagenOutput is the response wrapper for an upload.
type AdjuntarImagenOutput struct {
	Body ImagenDTO
}

// EliminarImagenInput carries the venta and imagen path params.
type EliminarImagenInput struct {
	ID      string `path:"id"     format:"uuid"`
	ImageID string `path:"img_id" format:"uuid"`
}
