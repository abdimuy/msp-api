//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

// DesglosePorZonaInput holds the path parameter for GET /rutas/{zona_id}/cobranza.
type DesglosePorZonaInput struct {
	ZonaID int `path:"zona_id" doc:"ID de la zona de ventas"`
}

// DesglosePorZonaOutput wraps the response body for the cobranza breakdown.
type DesglosePorZonaOutput struct {
	Body struct {
		ZonaID            int                `json:"zona_id"`
		FechaInicioSemana *string            `json:"fecha_inicio_semana"`
		Items             []VentaCobranzaDTO `json:"items"`
	}
}

// VentaCobranzaDTO is the wire representation of one venta's cobranza metrics.
// All money fields are strings to avoid floating-point rounding.
type VentaCobranzaDTO struct {
	VentaID     int    `json:"venta_id"     doc:"DOCTO_CC_ID de la venta"`
	ClienteID   int    `json:"cliente_id"   doc:"CLIENTE_ID"`
	Parcialidad string `json:"parcialidad"  doc:"Cuota esperada en pesos (2 decimales)"`
	Frecuencia  string `json:"frecuencia"   doc:"Cadencia de pago: SEMANAL, QUINCENAL o MENSUAL"`
	AbonoSemana string `json:"abono_semana" doc:"Total abonado en la ventana semanal (2 decimales)"`
	Vencidas    string `json:"vencidas"     doc:"Cuotas vencidas al inicio de la ventana (puede ser fracción)"`
	Aporte      string `json:"aporte"       doc:"Aporte calculado para el reporte ponderado (puede ser fracción)"`
	Saldo       string `json:"saldo"        doc:"Saldo pendiente actual (2 decimales)"`
}
