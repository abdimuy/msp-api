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

// ─── POST /reactivacion/envios/encolar ──────────────────────────────────────

// EncolarInput has no body — the encolado reads the cohorte de tratamiento.
type EncolarInput struct{}

// EncolarOutput wraps the 202 response body for POST /reactivacion/envios/encolar.
type EncolarOutput struct {
	Body struct {
		Status  string `json:"status"  doc:"aceptado o en_progreso"`
		Mensaje string `json:"mensaje" doc:"Descripción legible del estado del encolado"`
	}
}

// ─── POST /reactivacion/envios/drenar ───────────────────────────────────────

// DrenarInput has no body — the batch size is a fixed server-side default.
type DrenarInput struct{}

// DrenarOutput wraps the response body for POST /reactivacion/envios/drenar.
type DrenarOutput struct {
	Body DrenarResultDTO
}

// DrenarResultDTO is the wire representation of one drain batch's outcome.
type DrenarResultDTO struct {
	Enviados   int `json:"enviados"   doc:"Mensajes enviados en esta tanda"`
	Fallidos   int `json:"fallidos"   doc:"Mensajes que el enviador rechazó"`
	Bloqueados int `json:"bloqueados" doc:"Mensajes pausados por el circuit-breaker del gobernador"`
	Saltados   int `json:"saltados"   doc:"Mensajes pendientes que no se procesaron esta tanda"`
}

// ─── GET /reactivacion/envios ────────────────────────────────────────────────

// ListEnviosInput carries the optional query filters for the mensajes listing.
type ListEnviosInput struct {
	Estado   string `query:"estado"   doc:"Filtra por estado: encolado, enviado, fallido o bloqueado. Vacío = todos."`
	Segmento string `query:"segmento" doc:"Filtra por segmento: recien_liquidado o por_liquidar_hueco. Vacío = todos."`
	Limit    int    `query:"limit"    doc:"Máximo de filas a devolver. 0 = sin límite explícito."`
}

// ListEnviosOutput wraps the response body for GET /reactivacion/envios.
type ListEnviosOutput struct {
	Body struct {
		Items []MensajeDTO `json:"items"`
	}
}

// MensajeDTO is the wire representation of one MSP_RX_MENSAJES row. Dates are
// RFC3339 UTC; enviado_en and error are nullable — absent until the mensaje
// is sent (or fails).
type MensajeDTO struct {
	ClienteID  int     `json:"cliente_id"           doc:"ID del cliente en Microsip"`
	Segmento   string  `json:"segmento"             doc:"recien_liquidado o por_liquidar_hueco"`
	Telefono   string  `json:"telefono"             doc:"Teléfono destino"`
	Cuerpo     string  `json:"cuerpo"               doc:"Cuerpo del mensaje"`
	Estado     string  `json:"estado"               doc:"encolado, enviado, fallido o bloqueado"`
	SenderKind string  `json:"sender_kind"          doc:"simulado o real; vacío hasta que se envía"`
	EncoladoEn string  `json:"encolado_en"          doc:"Fecha de encolado (RFC3339 UTC)"`
	EnviadoEn  *string `json:"enviado_en,omitempty" doc:"Fecha de envío (RFC3339 UTC). Nulo hasta que se envía."`
	Error      *string `json:"error,omitempty"      doc:"Motivo de fallido/bloqueado. Nulo en otro caso."`
}
