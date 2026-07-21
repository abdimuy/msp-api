// Package reactivacionhttp is the reactivación module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount point.
//
//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

// ─── GET /reactivacion/cohorte ──────────────────────────────────────────────────

// ListCohorteInput carries the optional query filters for the cohorte listing.
type ListCohorteInput struct {
	Segmento        string `query:"segmento"         doc:"Filtra por segmento: recien_liquidado o por_liquidar_hueco. Vacío = todos."`
	SoloTratamiento bool   `query:"solo_tratamiento" doc:"Cuando es true, excluye el grupo de control (EN_CONTROL=1)."`
}

// ListCohorteOutput wraps the response body for GET /reactivacion/cohorte.
type ListCohorteOutput struct {
	Body struct {
		Items []CohorteClienteDTO `json:"items"`
	}
}

// CohorteClienteDTO is the wire representation of one cohorte cliente. Money and
// percentage fields are strings to avoid floating-point rounding, consistent with
// the project money convention. Dates are RFC3339 UTC.
type CohorteClienteDTO struct {
	ClienteID             int     `json:"cliente_id"                doc:"ID del cliente en Microsip"`
	Nombre                string  `json:"nombre"                    doc:"Nombre del cliente"`
	Telefono              string  `json:"telefono"                  doc:"Teléfono principal del cliente"`
	Segmento              string  `json:"segmento"                  doc:"recien_liquidado o por_liquidar_hueco"`
	EnControl             bool    `json:"en_control"                doc:"true si el cliente pertenece al grupo de control"`
	FueContactado         bool    `json:"fue_contactado"            doc:"true una vez que el canal contactó al cliente (Fase 3)"`
	CohorteFecha          string  `json:"cohorte_fecha"             doc:"Fecha de ingreso a la cohorte (RFC3339 UTC)"`
	FechaUltimaCompraBase *string `json:"fecha_ultima_compra_base"  doc:"Última compra al construir la cohorte (RFC3339 UTC). Nulo si no hay historial."`
	Saldo                 string  `json:"saldo"                     doc:"Saldo pendiente en pesos (2 decimales)"`
	PorLiquidarPct        string  `json:"por_liquidar_pct"          doc:"Porcentaje del precio total aún pendiente (2 decimales)"`
}

// ─── GET /reactivacion/atribucion ───────────────────────────────────────────────

// AtribucionInput has no query parameters.
type AtribucionInput struct{}

// AtribucionOutput wraps the response body for GET /reactivacion/atribucion.
type AtribucionOutput struct {
	Body AtribucionDTO
}

// AtribucionDTO is the wire representation of the treatment-vs-control summary.
// Rates and uplift are strings with 4 decimals (values in [0,1]).
type AtribucionDTO struct {
	TreatmentTotal       int    `json:"treatment_total"        doc:"Clientes contactados (FUE_CONTACTADO=1)"`
	TreatmentConvertidos int    `json:"treatment_convertidos"  doc:"Contactados que engancharon (recompra tras la cohorte)"`
	ControlTotal         int    `json:"control_total"          doc:"Clientes del grupo de control (EN_CONTROL=1)"`
	ControlConvertidos   int    `json:"control_convertidos"    doc:"Control que engancharon"`
	TasaTreatment        string `json:"tasa_treatment"         doc:"Tasa de conversión de tratamiento [0,1] (4 decimales)"`
	TasaControl          string `json:"tasa_control"           doc:"Tasa de conversión de control [0,1] (4 decimales)"`
	Uplift               string `json:"uplift"                 doc:"tasa_treatment - tasa_control (4 decimales, puede ser negativo)"`
}

// ─── POST /reactivacion/cohorte/construir ────────────────────────────────────────

// ConstruirInput has no body — the build reads its universe from Microsip.
type ConstruirInput struct{}

// ConstruirOutput wraps the 202 response body for POST /reactivacion/cohorte/construir.
type ConstruirOutput struct {
	Body struct {
		Status  string `json:"status"  doc:"aceptado o en_progreso"`
		Mensaje string `json:"mensaje" doc:"Descripción legible del estado de la construcción"`
	}
}
