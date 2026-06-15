// Package analyticshttp hosts the analytics module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount point. It is the outermost
// adapter layer — nothing inside the analytics module imports it.
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

// ─── Item DTOs ───────────────────────────────────────────────────────────────

// WinbackItemDTO is the wire representation of one winback candidate in the
// list response. Decimal values flow as strings so JSON parsing does not lose
// precision.
type WinbackItemDTO struct {
	ClienteID         int    `json:"cliente_id"          doc:"ID de Microsip del cliente"`
	Nombre            string `json:"nombre"              doc:"Nombre del cliente"`
	Zona              string `json:"zona"                doc:"Zona de ventas"`
	Telefono          string `json:"telefono"            doc:"Teléfono de contacto"`
	FechaUltimaCompra string `json:"fecha_ultima_compra" format:"date-time" doc:"RFC3339 UTC de la última compra; vacío si sin historial"`
	RecenciaDias      int    `json:"recencia_dias"       doc:"Días desde la última compra"`
	Frecuencia        int    `json:"frecuencia"          doc:"Número de compras históricas"`
	Monetary          string `json:"monetary"            doc:"Valor monetario total de compras (2 decimales)"`
	Saldo             string `json:"saldo"               doc:"Saldo pendiente de pago (2 decimales)"`
	PorLiquidarPct    string `json:"por_liquidar_pct"    doc:"Porcentaje del saldo aún por liquidar (2 decimales)"`
	NextBestProduct   string `json:"next_best_product"   doc:"Producto recomendado para siguiente contacto"`
	Segmento          string `json:"segmento"            doc:"Segmento RFM derivado"`
	Score             int    `json:"score"               doc:"Score de prioridad [0, 100]"`
	EnControl         bool   `json:"en_control"          doc:"true cuando el candidato pertenece al grupo de control A/B"`
}

// ─── Input / Output types ────────────────────────────────────────────────────

// ListWinbackInput collects the query parameters for GET /winback.
type ListWinbackInput struct {
	Segmento       string `query:"segmento"        doc:"Filtra por segmento RFM exacto (LEAL_POR_LIQUIDAR, DORMIDO_VALIOSO, ACTIVO, NUEVO, FRIO, PERDIDO)"`
	Zona           string `query:"zona"            doc:"Restringe a candidatos de esta zona de ventas"`
	Limit          int    `query:"limit"           default:"50" minimum:"1" maximum:"500" doc:"Máximo de registros devueltos"`
	IncluirControl bool   `query:"incluir_control" doc:"Incluye candidatos del grupo de control en el resultado"`
}

// ListWinbackOutput is the response wrapper for GET /winback.
type ListWinbackOutput struct {
	Body struct {
		Items []WinbackItemDTO `json:"items"`
	}
}

// AttributionInput collects the query parameters for GET /winback/attribution.
type AttributionInput struct {
	Zona string `query:"zona" doc:"Restringe el cálculo a candidatos de esta zona de ventas"`
}

// AttributionOutput is the response wrapper for GET /winback/attribution.
type AttributionOutput struct {
	Body struct {
		TreatmentTotal       int    `json:"treatment_total"        doc:"Total de candidatos en el grupo de tratamiento"`
		TreatmentConvertidos int    `json:"treatment_convertidos"  doc:"Candidatos del grupo de tratamiento que convirtieron"`
		ControlTotal         int    `json:"control_total"          doc:"Total de candidatos en el grupo de control"`
		ControlConvertidos   int    `json:"control_convertidos"    doc:"Candidatos del grupo de control que convirtieron"`
		TasaTreatment        string `json:"tasa_treatment"         doc:"Tasa de conversión del grupo de tratamiento, fracción en rango [0, 1] (4 decimales)"`
		TasaControl          string `json:"tasa_control"           doc:"Tasa de conversión del grupo de control, fracción en rango [0, 1] (4 decimales)"`
		Uplift               string `json:"uplift"                 doc:"Diferencia incremental tasa_treatment - tasa_control, rango [-1, 1] (4 decimales)"`
	}
}

// RefreshInput is the request body for POST /winback/refresh.
type RefreshInput struct {
	Body struct {
		Full bool `json:"full" doc:"Cuando true ejecuta una reconstrucción completa; false ejecuta un refresco incremental"`
	}
}

// RefreshOutput is the response wrapper for POST /winback/refresh (HTTP 202).
// The refresh runs asynchronously; procesados/watermark are NOT available at
// trigger time. Check server logs for completion details.
type RefreshOutput struct {
	Body struct {
		// Estado is "iniciado" when a new background refresh was started, or
		// "ya_en_progreso" when a refresh was already running and was not
		// started again.
		Estado  string `json:"estado"   doc:"Estado del disparo: 'iniciado' | 'ya_en_progreso'"`
		Mensaje string `json:"mensaje"  doc:"Descripción del estado"`
	}
}
