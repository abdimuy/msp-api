// Package clienteshttp is the clientes module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount point.
//
//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

// ─── Generic envelope ────────────────────────────────────────────────────────

// ListResponse is the generic cursor-paginated envelope for list endpoints.
// Items is never nil — an empty result set returns an empty slice.
type ListResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ─── Endpoint 1: GET /clientes ───────────────────────────────────────────────

// ListarClientesInput collects the query parameters for GET /clientes.
// Huma does not support pointer params: Zona and Cobrador use 0 as "not set"
// (Microsip IDs are always positive). ScoreMin uses -1 as "not set" (valid
// scores are in [0, 100]). The handler converts these sentinel values to *int
// before calling the service.
type ListarClientesInput struct {
	Q            string `query:"q"           doc:"Texto de búsqueda de texto completo; vacío activa el modo navegación"`
	Zona         int    `query:"zona"        doc:"Filtra por ID de zona de ventas; 0 = sin filtro"`
	Cobrador     int    `query:"cobrador"    doc:"Filtra por ID de cobrador asignado; 0 = sin filtro"`
	ConSaldo     bool   `query:"con_saldo"   doc:"Cuando true restringe a clientes con saldo pendiente > 0"`
	Segmento     string `query:"segmento"    doc:"Filtra por segmento RFM exacto (e.g. LEAL_POR_LIQUIDAR, DORMIDO_VALIOSO)"`
	EstadoPago   string `query:"estado_pago" doc:"Filtra por señal de solvencia (SIN_CREDITO, LIQUIDADO, AL_CORRIENTE, ATRASADO, MOROSO)"`
	ScoreMin     int    `query:"score_min"   default:"-1" doc:"Mínimo score de pulso [0, 100]; -1 = sin filtro"`
	Tier         string `query:"tier"          doc:"Filtra por tier de riesgo de cobranza (AL_DIA, VIGILANCIA, EN_RIESGO, CRITICO); vacío = sin filtro"`
	BandaCredito string `query:"banda_credito" doc:"Filtra por banda de riesgo crediticio (BAJO, MEDIO, ALTO, CRITICO); vacío = sin filtro"`
	SortBy       string `query:"sort_by"       enum:"nombre,saldo,zona,score,segmento,estado_pago,recencia,puntualidad,prox_pago,score_credito" doc:"Columna de ordenamiento GLOBAL; vacío = orden por defecto (relevancia en búsqueda, nombre al navegar)"`
	SortOrder    string `query:"sort_order"  enum:"asc,desc" default:"asc" doc:"Sentido del ordenamiento"`
	Cursor       string `query:"cursor"      doc:"Cursor de paginación opaco devuelto por la respuesta anterior"`
	Limit        int    `query:"limit"       default:"50" minimum:"1" maximum:"200" doc:"Máximo de registros devueltos"`
}

// DirectorioResponseBody is the response body for GET /clientes.
// It extends the basic paginated list with per-attribute facet counts from
// Meilisearch so the frontend can render filter chips without a second round-trip.
type DirectorioResponseBody struct {
	Items      []ClienteListItemDTO      `json:"items"                  doc:"Página de clientes del directorio"`
	NextCursor string                    `json:"next_cursor,omitempty"  doc:"Cursor opaco para la página siguiente; ausente en la última página"`
	Facets     map[string]map[string]int `json:"facets,omitempty"     doc:"Conteos de valores por atributo (zona_id, cobrador_id, segmento, estado_pago)"`
}

// ListarClientesOutput is the response for GET /clientes.
type ListarClientesOutput struct {
	Body DirectorioResponseBody
}

// ClienteListItemDTO is the wire representation of one client in the directory
// list. Monetary fields are JSON strings to avoid floating-point rounding.
type ClienteListItemDTO struct {
	ClienteID      int    `json:"cliente_id"            doc:"ID de Microsip del cliente"`
	Nombre         string `json:"nombre"                doc:"Nombre del cliente"`
	Zona           string `json:"zona"                  doc:"Nombre de la zona de ventas"`
	Telefono       string `json:"telefono"              doc:"Teléfono de contacto"`
	DireccionCorta string `json:"direccion_corta"       doc:"Dirección abreviada (calle, colonia, ciudad)"`
	Score          int    `json:"score"                 doc:"Score de pulso [0, 100]; 0 cuando no hay pulso"`
	Segmento       string `json:"segmento"              doc:"Segmento RFM; vacío cuando no hay pulso"`
	EstadoPago     string `json:"estado_pago"           doc:"Señal de solvencia; vacío cuando no hay pulso"`
	TienePulso     bool   `json:"tiene_pulso"           doc:"true cuando el cliente tiene datos materializados de analítica"`
	RecenciaDias   int    `json:"recencia_dias"         doc:"Días desde la última compra; 0 cuando no hay pulso"`
	Saldo          string `json:"saldo"                 doc:"Saldo pendiente total (2 decimales)"`
	// Cobranza intelligence signals (B2). Empty/zero when TienePulso is false.
	TierRiesgo      string `json:"tier_riesgo"          doc:"Tier de riesgo de cobranza: AL_DIA, VIGILANCIA, EN_RIESGO, CRITICO; vacío cuando no hay pulso"`
	PctPagosATiempo string `json:"pct_pagos_a_tiempo"   doc:"Porcentaje de pagos a tiempo (2 decimales); vacío cuando no hay pulso"`
	FechaProxPago   string `json:"fecha_prox_pago"      format:"date-time" doc:"RFC3339 UTC del próximo pago estimado; vacío si sin cadencia"`
	// Credit-risk signals (R3). Empty/zero when TienePulso is false or client has no credit.
	BandaCredito string `json:"banda_credito"        doc:"Banda de riesgo crediticio: BAJO, MEDIO, ALTO, CRITICO; vacío si no aplica"`
	ScoreCredito int    `json:"score_credito"        doc:"Score de riesgo crediticio [0–100] (mayor = menor riesgo); 0 si no aplica"`
}

// ─── Endpoint 2: GET /clientes/{id} ─────────────────────────────────────────

// ObtenerFichaInput collects the path and query parameters for GET /clientes/{id}.
type ObtenerFichaInput struct {
	ID    int    `path:"id"    doc:"ID de Microsip del cliente"`
	Desde string `query:"desde" doc:"Inicio del rango de fechas (YYYY-MM-DD); vacío = sin límite inferior"`
	Hasta string `query:"hasta" doc:"Fin del rango de fechas (YYYY-MM-DD); vacío = sin límite superior"`
}

// ObtenerFichaOutput is the response for GET /clientes/{id}.
type ObtenerFichaOutput struct {
	Body FichaDTO
}

// UbicacionDTO holds the GPS coordinates for a client.
// Disponible is false when the client has no coordinates on record.
type UbicacionDTO struct {
	Lat        float64 `json:"lat"        doc:"Latitud WGS-84"`
	Lng        float64 `json:"lng"        doc:"Longitud WGS-84"`
	Disponible bool    `json:"disponible" doc:"true cuando el cliente tiene coordenadas GPS registradas"`
}

// FichaDTO is the full 360 view of a client.
type FichaDTO struct {
	ClienteID     int          `json:"cliente_id"      doc:"ID de Microsip del cliente"`
	Nombre        string       `json:"nombre"          doc:"Nombre del cliente"`
	Direccion     DireccionDTO `json:"direccion"       doc:"Componentes de la dirección"`
	Ubicacion     UbicacionDTO `json:"ubicacion"       doc:"Coordenadas GPS del domicilio del cliente"`
	Telefono      string       `json:"telefono"        doc:"Teléfono de contacto"`
	LimiteCredito string       `json:"limite_credito"  doc:"Límite de crédito aprobado (2 decimales)"`
	Notas         string       `json:"notas"           doc:"Notas del cliente en Microsip"`
	Zona          string       `json:"zona"            doc:"Nombre de la zona de ventas"`
	Cobrador      string       `json:"cobrador"        doc:"Nombre del cobrador asignado"`
	Estatus       string       `json:"estatus"         doc:"Código de estatus de Microsip (A=activo)"`
	Resumen       ResumenDTO   `json:"resumen"         doc:"Resumen financiero agregado"`
	Series        SeriesDTO    `json:"series"          doc:"Series de tiempo para gráficas"`
	Pulso         *PulsoDTO    `json:"pulso,omitempty" doc:"Pulso de analítica; null cuando no hay datos materializados"`
}

// DireccionDTO holds the address components.
type DireccionDTO struct {
	Calle     string `json:"calle"     doc:"Calle y número"`
	Colonia   string `json:"colonia"   doc:"Colonia"`
	Poblacion string `json:"poblacion" doc:"Ciudad o municipio"`
	Estado    string `json:"estado"    doc:"Estado"`
}

// ResumenDTO holds the aggregated financial KPIs.
type ResumenDTO struct {
	TotalComprado  string `json:"total_comprado"  doc:"Suma de totales de ventas (2 decimales)"`
	TotalAbonado   string `json:"total_abonado"   doc:"Suma de pagos recibidos (2 decimales)"`
	Saldo          string `json:"saldo"           doc:"Saldo pendiente (2 decimales)"`
	PctLiquidado   string `json:"pct_liquidado"   doc:"Porcentaje pagado del total comprado (2 decimales)"`
	TicketPromedio string `json:"ticket_promedio" doc:"Importe promedio por venta (2 decimales)"`
	NumVentas      int    `json:"num_ventas"      doc:"Total de ventas registradas"`
	NumPagos       int    `json:"num_pagos"       doc:"Total de pagos recibidos"`
}

// SeriesDTO holds time-series data for ficha charts.
type SeriesDTO struct {
	AbonosPorMes      []PuntoMensualDTO         `json:"abonos_por_mes"        doc:"Abonos mensuales (serie trailing)"`
	CompradoVsAbonado []PuntoCompradoAbonadoDTO `json:"comprado_vs_abonado"  doc:"Comprado vs abonado por mes (dual serie)"`
}

// PuntoMensualDTO is a single (year, month, amount) data point.
type PuntoMensualDTO struct {
	Anio  int    `json:"anio"  doc:"Año"`
	Mes   int    `json:"mes"   doc:"Mes (1–12)"`
	Monto string `json:"monto" doc:"Monto del período (2 decimales)"`
}

// PuntoCompradoAbonadoDTO is a dual-series (year, month) data point.
type PuntoCompradoAbonadoDTO struct {
	Anio     int    `json:"anio"     doc:"Año"`
	Mes      int    `json:"mes"      doc:"Mes (1–12)"`
	Comprado string `json:"comprado" doc:"Total comprado en el período (2 decimales)"`
	Abonado  string `json:"abonado"  doc:"Total abonado en el período (2 decimales)"`
}

// PulsoDTO holds the analytics pulse fields shown in the ficha when TienePulso.
type PulsoDTO struct {
	Score             int    `json:"score"               doc:"Score de prioridad [0, 100]"`
	Segmento          string `json:"segmento"            doc:"Segmento RFM derivado"`
	EstadoPago        string `json:"estado_pago"         doc:"Señal de solvencia"`
	RecenciaDias      int    `json:"recencia_dias"       doc:"Días desde la última compra"`
	Frecuencia        int    `json:"frecuencia"          doc:"Número de compras históricas"`
	Monetary          string `json:"monetary"            doc:"Valor monetario total de compras (2 decimales)"`
	Saldo             string `json:"saldo"               doc:"Saldo pendiente de pago (2 decimales)"`
	PorLiquidarPct    string `json:"por_liquidar_pct"    doc:"Porcentaje del saldo por liquidar (2 decimales)"`
	FechaUltimaCompra string `json:"fecha_ultima_compra" format:"date-time" doc:"RFC3339 UTC de la última compra; vacío si sin historial"`
	FechaUltimoPago   string `json:"fecha_ultimo_pago"   format:"date-time" doc:"RFC3339 UTC del último pago; vacío si sin historial de pagos"`
	NextBestProduct   string `json:"next_best_product"   doc:"Producto recomendado para siguiente contacto"`

	// ─── Cobranza intelligence signals ───────────────────────────────────────────
	NumPagos        int      `json:"num_pagos"          doc:"Total de pagos aplicados en historial; 0 si sin datos"`
	CadenciaDias    int      `json:"cadencia_dias"       doc:"Días promedio entre pagos consecutivos; 0 si sin datos"`
	DiasAtrasoProm  int      `json:"dias_atraso_prom"    doc:"Días de atraso promedio respecto a cadencia; 0 si sin datos"`
	PctPagosATiempo string   `json:"pct_pagos_a_tiempo"  doc:"Porcentaje de pagos dentro de cadencia + 7 días (2 decimales); 0.00 si sin datos"`
	FechaProxPago   string   `json:"fecha_prox_pago"     format:"date-time" doc:"RFC3339 UTC del próximo pago estimado; vacío si sin cadencia"`
	MontoProxPago   string   `json:"monto_prox_pago"     doc:"Monto estimado del próximo pago — promedio histórico (2 decimales)"`
	TierRiesgo      string   `json:"tier_riesgo"         doc:"Tier de riesgo de cobranza: AL_DIA, VIGILANCIA, EN_RIESGO, CRITICO"`
	BandaCredito    string   `json:"banda_credito"       doc:"Banda de riesgo crediticio: BAJO, MEDIO, ALTO, CRITICO; vacío si no aplica"`
	ScoreCredito    int      `json:"score_credito"       doc:"Score de riesgo crediticio [0–100] (mayor = menor riesgo); 0 si no aplica"`
	CreditoDrivers  []string `json:"credito_drivers"     doc:"Hasta 3 razones (en español) que más elevan el riesgo crediticio; vacío si no aplica"`
}

// ─── Endpoint 3: GET /clientes/{id}/ventas ───────────────────────────────────

// ListarVentasClienteInput collects path + query params for GET /clientes/{id}/ventas.
type ListarVentasClienteInput struct {
	ID     int    `path:"id"     doc:"ID de Microsip del cliente"`
	Cursor string `query:"cursor" doc:"Cursor de paginación opaco devuelto por la respuesta anterior"`
	Limit  int    `query:"limit"  default:"50" minimum:"1" maximum:"200" doc:"Máximo de registros devueltos"`
}

// ListarVentasClienteOutput is the response for GET /clientes/{id}/ventas.
type ListarVentasClienteOutput struct {
	Body ListResponse[VentaListItemDTO]
}

// VentaListItemDTO is the wire representation of a single sale header.
type VentaListItemDTO struct {
	DoctoPVID  int    `json:"docto_pv_id"  doc:"ID del documento de venta en Microsip"`
	Fecha      string `json:"fecha"        format:"date-time" doc:"RFC3339 UTC de la fecha de la venta"`
	Folio      string `json:"folio"        doc:"Folio del documento en Microsip"`
	Tipo       string `json:"tipo"         doc:"CONTADO o CREDITO"`
	Total      string `json:"total"        doc:"Importe neto de la venta (2 decimales)"`
	SaldoVenta string `json:"saldo_venta"  doc:"Saldo pendiente de la venta (2 decimales)"`
	NumPagos   int    `json:"num_pagos"    doc:"Número de pagos aplicados"`
}

// ─── Endpoint 4: GET /clientes/{id}/ventas/{doctoPvId} ──────────────────────

// ObtenerVentaDetalleInput collects path params for GET /clientes/{id}/ventas/{doctoPvId}.
type ObtenerVentaDetalleInput struct {
	ID        int `path:"id"         doc:"ID de Microsip del cliente (validación de ruta)"`
	DoctoPvID int `path:"doctoPvId"  doc:"ID del documento de venta en Microsip"`
}

// ObtenerVentaDetalleOutput is the response for GET /clientes/{id}/ventas/{doctoPvId}.
type ObtenerVentaDetalleOutput struct {
	Body VentaDetalleDTO
}

// VentaDetalleDTO is the full detail bundle for a single sale.
type VentaDetalleDTO struct {
	Venta     VentaHeaderDTO     `json:"venta"              doc:"Encabezado de la venta"`
	Productos []ProductoVentaDTO `json:"productos"          doc:"Líneas de detalle de la venta"`
	Contrato  *ContratoDTO       `json:"contrato,omitempty" doc:"Contrato de crédito; null para ventas de contado"`
	Pagos     []PagoDTO          `json:"pagos"              doc:"Historial de pagos aplicados"`
}

// VentaHeaderDTO is the header fields of a sale, replicated in the detail view.
type VentaHeaderDTO struct {
	DoctoPVID  int    `json:"docto_pv_id"  doc:"ID del documento de venta en Microsip"`
	ClienteID  int    `json:"cliente_id"   doc:"ID de Microsip del cliente"`
	Fecha      string `json:"fecha"        format:"date-time" doc:"RFC3339 UTC de la fecha de la venta"`
	Folio      string `json:"folio"        doc:"Folio del documento en Microsip"`
	Tipo       string `json:"tipo"         doc:"CONTADO o CREDITO"`
	Total      string `json:"total"        doc:"Importe neto de la venta (2 decimales)"`
	SaldoVenta string `json:"saldo_venta"  doc:"Saldo pendiente de la venta (2 decimales)"`
	NumPagos   int    `json:"num_pagos"    doc:"Número de pagos aplicados"`
}

// ProductoVentaDTO is a single sale line item.
type ProductoVentaDTO struct {
	ArticuloID      int    `json:"articulo_id"       doc:"ID del artículo en Microsip"`
	Nombre          string `json:"nombre"            doc:"Nombre del artículo"`
	Unidades        string `json:"unidades"          doc:"Cantidad vendida (5 decimales)"`
	PrecioUnitario  string `json:"precio_unitario"   doc:"Precio unitario (2 decimales)"`
	PrecioTotalNeto string `json:"precio_total_neto" doc:"Total neto de la línea tras descuento (2 decimales)"`
	PctjeDscto      string `json:"pctje_dscto"       doc:"Porcentaje de descuento aplicado (2 decimales)"`
}

// ContratoDTO holds the credit contract details for a credit sale.
type ContratoDTO struct {
	Parcialidad     string   `json:"parcialidad"       doc:"Cuota periódica pactada (2 decimales)"`
	Enganche        string   `json:"enganche"          doc:"Enganche cobrado al momento de la venta (2 decimales)"`
	PrecioDeContado string   `json:"precio_de_contado" doc:"Precio al contado en el momento de la venta (2 decimales)"`
	PlazoMeses      int      `json:"plazo_meses"       doc:"Plazo del crédito en meses"`
	FormaDePago     string   `json:"forma_de_pago"     doc:"Frecuencia de pago (mensual, quincenal, etc.)"`
	Vendedores      []string `json:"vendedores"        doc:"Nombres de vendedores asignados"`
}

// PagoDTO is a single payment entry in the sale's payment history.
type PagoDTO struct {
	DoctoCCID  int    `json:"docto_cc_id"  doc:"ID del documento de pago en Microsip"`
	Fecha      string `json:"fecha"        format:"date-time" doc:"RFC3339 UTC de la fecha del pago"`
	Importe    string `json:"importe"      doc:"Monto del pago (2 decimales)"`
	FormaCobro string `json:"forma_cobro"  doc:"Método de cobro (efectivo, transferencia, etc.)"`
}

// ─── Endpoint 5: POST /clientes/_search/refresh ──────────────────────────────

// RefrescarBusquedaInput is the request for POST /clientes/_search/refresh.
// No body needed — the operation is idempotent and parameterless.
type RefrescarBusquedaInput struct{}

// RefrescarBusquedaOutput is the response for POST /clientes/_search/refresh.
type RefrescarBusquedaOutput struct {
	Body struct {
		Reindexado bool `json:"reindexado"  doc:"Siempre true cuando la operación completa sin error"`
		Documentos int  `json:"documentos"  doc:"Número de documentos indexados en esta ejecución"`
	}
}
