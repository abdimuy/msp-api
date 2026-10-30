// Package garhttp contiene el transporte HTTP del módulo de garantías.
package garhttp

// ListarGarantiasRequest representa los filtros de la bandeja.
// Todos llegan como string porque Huma no maneja bien punteros en query params.
type ListarGarantiasRequest struct {
	Cursor    string `query:"cursor" required:"false"`
	Limit     string `query:"limit" required:"false"`
	Estado    string `query:"estado" required:"false"`
	Origen    string `query:"origen" required:"false"`
	ClienteID string `query:"cliente_id" required:"false"`
	Etapa     string `query:"etapa" required:"false"`
	Ubicacion string `query:"ubicacion" required:"false"`
	Desde     string `query:"desde" required:"false"`
	Hasta     string `query:"hasta" required:"false"`
}

// ObtenerGarantiaRequest representa el UUID recibido en la ruta.
type ObtenerGarantiaRequest struct {
	ID string `path:"id"`
}

// GarantiaDTO es la representación HTTP de un folio.
type GarantiaDTO struct {
	ID             string        `json:"id" format:"uuid"`
	Folio          string        `json:"folio"`
	Origen         string        `json:"origen"`
	ClienteID      *int          `json:"cliente_id,omitempty"`
	VentaID        *int          `json:"venta_id,omitempty"`
	EstadoCuenta   *string       `json:"estado_cuenta,omitempty"`
	Estado         string        `json:"estado"`
	Description    string        `json:"description"`
	VigenciaHasta  *string       `json:"vigencia_hasta,omitempty" format:"date"`
	Calle          *string       `json:"calle,omitempty"`
	NumeroExterior *string       `json:"numero_exterior,omitempty"`
	Colonia        *string       `json:"colonia,omitempty"`
	Localidad      *string       `json:"localidad,omitempty"`
	Ciudad         *string       `json:"ciudad,omitempty"`
	CodigoPostal   *string       `json:"codigo_postal,omitempty"`
	GPSLat         *float64      `json:"gps_lat,omitempty"`
	GPSLon         *float64      `json:"gps_lon,omitempty"`
	AbiertoPor     string        `json:"abierto_por"`
	CerradoEn      *string       `json:"cerrado_en,omitempty" format:"date-time"`
	CreatedAt      string        `json:"created_at" format:"date-time"`
	UpdatedAt      string        `json:"updated_at" format:"date-time"`
	Articulos      []ArticuloDTO `json:"articulos"`
}

// ArticuloDTO es la representación HTTP de un artículo de la garantía.
type ArticuloDTO struct {
	ID          string  `json:"id" format:"uuid"`
	GarantiaID  string  `json:"garantia_id" format:"uuid"`
	Rol         string  `json:"rol"`
	ReemplazaA  *string `json:"reemplaza_a,omitempty" format:"uuid"`
	ArticuloID  *int    `json:"articulo_id,omitempty"`
	Clave       string  `json:"clave"`
	Description string  `json:"description"`
	Ruta        *string `json:"ruta,omitempty"`
	Etapa       string  `json:"etapa"`
	Ubicacion   string  `json:"ubicacion"`
	Dictamen    *string `json:"dictamen,omitempty"`
	Desenlace   *string `json:"desenlace,omitempty"`
	CerradoEn   *string `json:"cerrado_en,omitempty" format:"date-time"`
	CreatedAt   string  `json:"created_at" format:"date-time"`
	UpdatedAt   string  `json:"updated_at" format:"date-time"`
}

// EventoDTO representa una entrada de la línea de tiempo.
type EventoDTO struct {
	ID                string   `json:"id" format:"uuid"`
	GarantiaID        string   `json:"garantia_id" format:"uuid"`
	ArticuloRef       *string  `json:"articulo_ref,omitempty" format:"uuid"`
	Tipo              string   `json:"tipo"`
	Description       string   `json:"description"`
	EtapaDesde        *string  `json:"etapa_desde,omitempty"`
	EtapaHasta        *string  `json:"etapa_hasta,omitempty"`
	Usuario           string   `json:"usuario"`
	RolDecisor        *string  `json:"rol_decisor,omitempty"`
	GPSLat            *float64 `json:"gps_lat,omitempty"`
	GPSLon            *float64 `json:"gps_lon,omitempty"`
	CreatedAt         string   `json:"created_at" format:"date-time"`
	DeviceCreatedAt   string   `json:"device_created_at" format:"date-time"`
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
