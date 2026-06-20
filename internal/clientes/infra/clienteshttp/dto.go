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
	Q             string `query:"q"           doc:"Texto de búsqueda de texto completo; vacío activa el modo navegación"`
	Zona          int    `query:"zona"        doc:"Filtra por ID de zona de ventas; 0 = sin filtro"`
	Cobrador      int    `query:"cobrador"    doc:"Filtra por ID de cobrador asignado; 0 = sin filtro"`
	ConSaldo      bool   `query:"con_saldo"   doc:"Cuando true restringe a clientes con saldo pendiente > 0"`
	Segmento      string `query:"segmento"    doc:"Filtra por segmento RFM exacto (e.g. LEAL_POR_LIQUIDAR, DORMIDO_VALIOSO)"`
	EstadoPago    string `query:"estado_pago" doc:"Filtra por señal de solvencia (SIN_CREDITO, LIQUIDADO, AL_CORRIENTE, ATRASADO, MOROSO)"`
	ScoreMin      int    `query:"score_min"   default:"-1" doc:"Mínimo score de pulso [0, 100]; -1 = sin filtro"`
	Tier          string `query:"tier"          doc:"Filtra por tier de riesgo de cobranza (AL_DIA, VIGILANCIA, EN_RIESGO, CRITICO); vacío = sin filtro"`
	BandaCredito  string `query:"banda_credito"  doc:"Filtra por banda de riesgo crediticio (BAJO, MEDIO, ALTO, CRITICO); vacío = sin filtro"`
	BandaRecompra string `query:"banda_recompra" doc:"Filtra por banda de propensión de recompra (ALTA, MEDIA, BAJA); vacío = sin filtro"`
	BandaCLV      string `query:"banda_clv"      doc:"Filtra por banda de CLV (ALTO, MEDIO, BAJO); vacío = sin filtro"`
	SortBy        string `query:"sort_by"        enum:"nombre,saldo,zona,score,segmento,estado_pago,recencia,puntualidad,prox_pago,score_credito,score_recompra,clv" doc:"Columna de ordenamiento GLOBAL; vacío = orden por defecto (relevancia en búsqueda, nombre al navegar)"`
	SortOrder     string `query:"sort_order"  enum:"asc,desc" default:"asc" doc:"Sentido del ordenamiento"`
	Cursor        string `query:"cursor"      doc:"Cursor de paginación opaco devuelto por la respuesta anterior"`
	Limit         int    `query:"limit"       default:"50" minimum:"1" maximum:"200" doc:"Máximo de registros devueltos"`
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
	// Repurchase propensity signals (Fase A). Empty/zero when TienePulso is false or client has no purchase history.
	BandaRecompra string `json:"banda_recompra"       doc:"Banda de propensión de recompra: ALTA, MEDIA, BAJA; vacío si no aplica"`
	ScoreRecompra int    `json:"score_recompra"       doc:"Score de propensión de recompra [0–100] (mayor = más probable); 0 si no aplica"`
	// CLV signals (Fase B). Empty when TienePulso is false or no aplica.
	CLV      string `json:"clv"      doc:"Valor de vida del cliente ajustado por riesgo, en pesos; vacío si no aplica"`
	BandaCLV string `json:"banda_clv" doc:"Banda de CLV: ALTO, MEDIO, BAJO; vacío si no aplica"`
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

	// ─── Repurchase propensity signals (Fase A) ───────────────────────────────────────
	BandaRecompra   string   `json:"banda_recompra"      doc:"Banda de propensión de recompra: ALTA, MEDIA, BAJA; vacío si no aplica"`
	ScoreRecompra   int      `json:"score_recompra"      doc:"Score de propensión de recompra [0–100] (mayor = más probable); 0 si no aplica"`
	RecompraDrivers []string `json:"recompra_drivers"    doc:"Hasta 3 razones (en español) de mayor propensión de recompra; vacío si no aplica"`

	// ─── CLV signals (Fase B) ───────────────────────────────────────────────────────
	CLV      string `json:"clv"      doc:"Valor de vida del cliente ajustado por riesgo, en pesos; vacío si no aplica"`
	BandaCLV string `json:"banda_clv" doc:"Banda de CLV: ALTO, MEDIO, BAJO; vacío si no aplica"`
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
	DoctoPVID      int    `json:"docto_pv_id"      doc:"ID del documento de venta en Microsip"`
	Fecha          string `json:"fecha"            format:"date-time" doc:"RFC3339 UTC de la fecha de la venta"`
	Folio          string `json:"folio"            doc:"Folio del documento en Microsip"`
	Tipo           string `json:"tipo"             doc:"CONTADO o CREDITO"`
	Total          string `json:"total"            doc:"Importe neto de la venta (2 decimales)"`
	SaldoVenta     string `json:"saldo_venta"      doc:"Saldo pendiente de la venta (2 decimales)"`
	NumPagos       int    `json:"num_pagos"        doc:"Número de pagos aplicados"`
	Hora           string `json:"hora"             doc:"hora de registro de la venta (HH:MM:SS, hora local del servidor Microsip, no UTC)"`
	Almacen        string `json:"almacen"          doc:"nombre del almacén o sucursal donde se realizó la venta"`
	PrimerArticulo string `json:"primer_articulo"  doc:"nombre del primer artículo vendido (líneas J/N); vacío si sin detalle"`
	NumArticulos   int    `json:"num_articulos"    doc:"número de líneas de artículos (J/N) en la venta"`
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
	DoctoCCID    int    `json:"docto_cc_id"    doc:"ID del documento de pago en Microsip"`
	Fecha        string `json:"fecha"          format:"date-time" doc:"RFC3339 UTC de la fecha del pago"`
	Importe      string `json:"importe"        doc:"Monto del pago (2 decimales)"`
	FormaCobro   string `json:"forma_cobro"    doc:"Método de cobro (efectivo, transferencia, etc.)"`
	ConceptoCCID int    `json:"concepto_cc_id" doc:"ID del concepto en Microsip (CONCEPTO_CC_ID)"`
	Concepto     string `json:"concepto"       doc:"Nombre del concepto del movimiento"`
	Categoria    string `json:"categoria"      doc:"Categoría derivada del concepto: pago, enganche, condonacion, perdida u otro"`
	Cobrador     string `json:"cobrador"       doc:"Nombre del cobrador o descripción del documento; vacío si no aplica"`
	EsIngreso    bool   `json:"es_ingreso"     doc:"Verdadero si el movimiento representa ingreso real (pago o enganche)"`
}

// ─── Endpoint 5: GET /clientes/{id}/ritmo-pago ───────────────────────────────

// ObtenerRitmoPagoInput collects path and query params for GET /clientes/{id}/ritmo-pago.
type ObtenerRitmoPagoInput struct {
	ID    int    `path:"id"    doc:"ID de Microsip del cliente"`
	Desde string `query:"desde" doc:"Inicio del rango de fechas (YYYY-MM-DD); vacío = sin límite inferior"`
	Hasta string `query:"hasta" doc:"Fin del rango de fechas (YYYY-MM-DD); vacío = sin límite superior"`
}

// ObtenerRitmoPagoOutput is the response for GET /clientes/{id}/ritmo-pago.
type ObtenerRitmoPagoOutput struct {
	Body RitmoPagoDTO
}

// RitmoPagoDTO is the wire representation of the weekly payment-rhythm series.
type RitmoPagoDTO struct {
	AnclaDiaRuta string           `json:"ancla_dia_ruta" doc:"Día de ruta modal del cliente (lunes–domingo)"`
	Semanas      []SemanaRitmoDTO `json:"semanas"        doc:"Semanas ordenadas ascendentemente"`
	Eventos      []EventoRitmoDTO `json:"eventos"        doc:"Eventos notables ordenados por fecha"`
	Resumen      ResumenRitmoDTO  `json:"resumen"        doc:"Resumen agregado del período"`
}

// SemanaRitmoDTO is a single weekly bucket in the payment-rhythm series.
type SemanaRitmoDTO struct {
	SemanaInicio string `json:"semana_inicio" format:"date-time" doc:"RFC3339 UTC del inicio de la semana"`
	MontoAbonado string `json:"monto_abonado"                   doc:"Total abonado en la semana (2 decimales)"`
	Saldo        string `json:"saldo"                           doc:"Saldo reconstruido al cierre de la semana (2 decimales)"`
	NumPagos     int    `json:"num_pagos"                       doc:"Número de pagos en la semana"`
	PagoIDs      []int  `json:"pago_ids"                        doc:"IDs de los documentos de abono (DOCTO_CC_ID) aplicados en la semana; heterogéneos por concepto (cobranza, enganche, condonación y pérdida mezclados); para distinguir ingreso real de perdón de deuda consultar es_ingreso/categoria por pago vía GET /clientes/{id}/pagos/{doctoCcId}; arreglo vacío cuando no hay pagos"`
}

// EventoRitmoDTO is a notable event in the payment-rhythm window.
type EventoRitmoDTO struct {
	Fecha      string `json:"fecha"        format:"date-time" doc:"RFC3339 UTC del evento"`
	Tipo       string `json:"tipo"                            doc:"venta_credito, venta_contado o liquidacion"`
	Monto      string `json:"monto"                           doc:"Importe del evento (2 decimales); 0.00 para liquidacion"`
	DoctoPvID  int    `json:"docto_pv_id"                     doc:"ID del documento de venta; 0 para liquidacion"`
	Folio      string `json:"folio"                           doc:"Folio del documento; vacío para liquidacion"`
	PlazoMeses int    `json:"plazo_meses"                     doc:"Plazo del crédito en meses; 0 para contado y liquidacion"`
}

// ResumenRitmoDTO holds the aggregated summary of the payment-rhythm window.
type ResumenRitmoDTO struct {
	TotalAbonado   string `json:"total_abonado"    doc:"Suma de pagos en el período (2 decimales)"`
	SemanasConPago int    `json:"semanas_con_pago" doc:"Semanas con al menos un pago"`
	SemanasActivas int    `json:"semanas_activas"  doc:"Semanas desde la primera con pago hasta el final del período"`
	RachaActualSem int    `json:"racha_actual_sem" doc:"Semanas consecutivas con pago al cierre del período"`
	ConstanciaPct  string `json:"constancia_pct"   doc:"Porcentaje de semanas activas con pago (2 decimales)"`
	SaldoActual    string `json:"saldo_actual"     doc:"Saldo vigente (2 decimales)"`
}

// ─── Endpoint 6: POST /clientes/_search/refresh ──────────────────────────────

// RefrescarBusquedaInput is the request for POST /clientes/_search/refresh.
// No body needed — the operation is idempotent and parameterless.
type RefrescarBusquedaInput struct{}

// ─── Endpoint N: GET /clientes/{id}/pagos/{doctoCcId} ────────────────────────

// ObtenerPagoDetalleInput collects the path parameters.
type ObtenerPagoDetalleInput struct {
	ID        int `path:"id"        doc:"ID de Microsip del cliente"`
	DoctoCcID int `path:"doctoCcId" doc:"ID de Microsip del documento de abono (DOCTOS_CC)"`
}

// ObtenerPagoDetalleOutput is the response for GET /clientes/{id}/pagos/{doctoCcId}.
type ObtenerPagoDetalleOutput struct {
	Body PagoDetalleDTO
}

// PagoDetalleDTO is the wire representation of a single payment detail.
type PagoDetalleDTO struct {
	Importe        string  `json:"importe"               doc:"Importe bruto del pago (suma de IMPORTE+IMPUESTO de los importes aplicados)"`
	IVA            string  `json:"iva"                   doc:"IVA del pago"`
	Fecha          string  `json:"fecha"                 format:"date-time" doc:"Fecha del documento de abono (UTC RFC3339)"`
	FormaCobroID   int     `json:"forma_cobro_id"        doc:"ID de la forma de cobro; 0 cuando ausente"`
	FormaCobro     string  `json:"forma_cobro"           doc:"Nombre de la forma de cobro; vacío cuando ausente"`
	Referencia     string  `json:"referencia"            doc:"Referencia de la forma de cobro; vacío cuando ausente"`
	CobradorID     int     `json:"cobrador_id"           doc:"ID del cobrador"`
	Cobrador       string  `json:"cobrador"              doc:"Nombre del cobrador"`
	ConceptoCCID   int     `json:"concepto_cc_id"        doc:"ID del concepto de cuenta corriente"`
	Concepto       string  `json:"concepto"              doc:"Nombre del concepto"`
	Categoria      string  `json:"categoria"             doc:"Categoría derivada: pago, enganche, condonacion, perdida, otro"`
	EsIngreso      bool    `json:"es_ingreso"            doc:"true cuando el pago representa un ingreso real (pago o enganche)"`
	Folio          string  `json:"folio"                 doc:"Folio del documento de abono"`
	Lat            *string `json:"lat,omitempty"         doc:"Latitud GPS del cobro; ausente cuando no disponible"`
	Lon            *string `json:"lon,omitempty"         doc:"Longitud GPS del cobro; ausente cuando no disponible"`
	AplicaACargoID int     `json:"aplica_a_cargo_id"     doc:"DOCTO_CC_ID del cargo al que aplica este abono"`
	SaldoCargo     *string `json:"saldo_cargo,omitempty" doc:"Saldo del cargo (caché MSP_SALDOS_VENTAS); ausente cuando no disponible"`
	DoctoPVID      int     `json:"docto_pv_id"           doc:"DOCTO_PV_ID de la venta original; 0 cuando no resoluble"`
	Cancelado      bool    `json:"cancelado"             doc:"true cuando el documento fue cancelado"`
	Aplicado       bool    `json:"aplicado"              doc:"true cuando el pago fue aplicado"`
	RecibidoAt     string  `json:"recibido_at,omitempty"  format:"date-time" doc:"Timestamp de recepción vía app (RFC3339 UTC); ausente en pagos nativos"`
	AplicadoAt     string  `json:"aplicado_at,omitempty"  format:"date-time" doc:"Timestamp de aplicación vía app (RFC3339 UTC); ausente en pagos nativos"`
	Origen         string  `json:"origen"                doc:"Origen del dato: 'app' cuando MSP_PAGOS_RECIBIDOS tiene registro; 'microsip' en caso contrario"`
}

// RefrescarBusquedaOutput is the response for POST /clientes/_search/refresh.
type RefrescarBusquedaOutput struct {
	Body struct {
		Reindexado bool `json:"reindexado"  doc:"Siempre true cuando la operación completa sin error"`
		Documentos int  `json:"documentos"  doc:"Número de documentos indexados en esta ejecución"`
	}
}
