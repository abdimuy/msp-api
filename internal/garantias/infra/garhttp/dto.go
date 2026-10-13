// Package garhttp contiene el transporte HTTP del módulo de garantías.
package garhttp

// ListarGarantiasRequest representa los filtros de la bandeja.
// Todos llegan como string porque Huma no maneja bien punteros en query params.
type ListarGarantiasRequest struct {
	Cursor    string `query:"cursor" required:"false"`
	Limit     string `query:"limit" required:"false"`
	Estado    string `query:"estado" required:"false" enum:"abierto,en_proceso,listo_entrega,entregado,cerrado,cancelado"`
	Origen    string `query:"origen" required:"false" enum:"piso,cliente"`
	ClienteID string `query:"cliente_id" required:"false"`
	Etapa     string `query:"etapa" required:"false" enum:"registrado,pendiente_recoleccion,recolectado,en_revision,orden_generada,enviado_proveedor,dictamen_recibido,reparado_proveedor,espera_respuesta_cliente,en_taller,reparado_taller,cambio_autorizado,listo_entrega,entregado,reingresado_inventario,standby,segunda_mano,desarmado,merma"`
	Ubicacion string `query:"ubicacion" required:"false" enum:"domicilio_cliente,en_transito,almacen_revision,taller,proveedor,almacen_segunda_mano,entregado,baja"`
	Desde     string `query:"desde" required:"false"`
	Hasta     string `query:"hasta" required:"false"`
}

// ObtenerGarantiaRequest representa el UUID recibido en la ruta.
type ObtenerGarantiaRequest struct {
	ID string `path:"id" format:"uuid"`
}

// GarantiaDTO es la representación HTTP de un folio.
type GarantiaDTO struct {
	ID             string        `json:"id"`
	Folio          string        `json:"folio"`
	Origen         string        `json:"origen"`
	ClienteID      *int          `json:"cliente_id,omitempty"`
	VentaID        *int          `json:"venta_id,omitempty"`
	EstadoCuenta   *string       `json:"estado_cuenta,omitempty"`
	Estado         string        `json:"estado"`
	Description    string        `json:"description"`
	VigenciaHasta  *string       `json:"vigencia_hasta,omitempty"`
	Calle          *string       `json:"calle,omitempty"`
	NumeroExterior *string       `json:"numero_exterior,omitempty"`
	Colonia        *string       `json:"colonia,omitempty"`
	Localidad      *string       `json:"localidad,omitempty"`
	Ciudad         *string       `json:"ciudad,omitempty"`
	CodigoPostal   *string       `json:"codigo_postal,omitempty"`
	GPSLat         *float64      `json:"gps_lat,omitempty"`
	GPSLon         *float64      `json:"gps_lon,omitempty"`
	AbiertoPor     string        `json:"abierto_por"`
	CerradoEn      *string       `json:"cerrado_en,omitempty"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	Articulos      []ArticuloDTO `json:"articulos"`
}

// ArticuloDTO es la representación HTTP de un artículo de la garantía.
type ArticuloDTO struct {
	ID          string  `json:"id"`
	GarantiaID  string  `json:"garantia_id"`
	Rol         string  `json:"rol"`
	ReemplazaA  *string `json:"reemplaza_a,omitempty"`
	ArticuloID  *int    `json:"articulo_id,omitempty"`
	Clave       string  `json:"clave"`
	Description string  `json:"description"`
	Ruta        *string `json:"ruta,omitempty"`
	Etapa       string  `json:"etapa"`
	Ubicacion   string  `json:"ubicacion"`
	Dictamen    *string `json:"dictamen,omitempty"`
	Desenlace   *string `json:"desenlace,omitempty"`
	CerradoEn   *string `json:"cerrado_en,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// EventoDTO representa una entrada de la línea de tiempo.
type EventoDTO struct {
	ID                string   `json:"id"`
	GarantiaID        string   `json:"garantia_id"`
	ArticuloRef       *string  `json:"articulo_ref,omitempty"`
	Tipo              string   `json:"tipo"`
	Description       string   `json:"description"`
	EtapaDesde        *string  `json:"etapa_desde,omitempty"`
	EtapaHasta        *string  `json:"etapa_hasta,omitempty"`
	Usuario           string   `json:"usuario"`
	RolDecisor        *string  `json:"rol_decisor,omitempty"`
	GPSLat            *float64 `json:"gps_lat,omitempty"`
	GPSLon            *float64 `json:"gps_lon,omitempty"`
	CreatedAt         string   `json:"created_at"`
	DeviceCreatedAt   string   `json:"device_created_at"`
	ClaveIdempotencia string   `json:"clave_idempotencia"`
}

// ListarGarantiasResponse es la respuesta paginada de la bandeja.
type ListarGarantiasResponse struct {
	Body struct {
		Items      []GarantiaDTO `json:"items"`
		NextCursor string        `json:"next_cursor"`
		HasMore    bool          `json:"has_more"`
	}
}

// ObtenerGarantiaResponse devuelve el folio y su línea de tiempo.
type ObtenerGarantiaResponse struct {
	Body struct {
		Garantia GarantiaDTO `json:"garantia"`
		Eventos  []EventoDTO `json:"eventos"`
	}
}
