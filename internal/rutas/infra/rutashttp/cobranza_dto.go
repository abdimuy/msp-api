//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

// DesglosePorZonaInput holds the path parameter for GET /rutas/{zona_id}/cobranza.
type DesglosePorZonaInput struct {
	ZonaID int `path:"zona_id" doc:"ID de la zona de ventas"`
}

// DesglosePorUsuarioInput holds the path parameter for
// GET /rutas/usuarios/{uid}/cobranza.
type DesglosePorUsuarioInput struct {
	UID string `path:"uid" doc:"UID del usuario (cobrador) en Firestore"`
}

// DesglosePorZonaOutput wraps the response body for the cobranza breakdown.
type DesglosePorZonaOutput struct {
	Body struct {
		ZonaID            int                 `json:"zona_id"`
		FechaInicioSemana *string             `json:"fecha_inicio_semana"`
		Items             []VentaCobranzaDTO  `json:"items"`
		Resumen           ResumenPonderadoDTO `json:"resumen"`
	}
}

// VentaCobranzaDTO is the wire representation of one venta's cobranza metrics.
// All money fields are strings to avoid floating-point rounding.
type VentaCobranzaDTO struct {
	VentaID             int    `json:"venta_id"              doc:"DOCTO_CC_ID de la venta"`
	ClienteID           int    `json:"cliente_id"            doc:"CLIENTE_ID"`
	ClienteNombre       string `json:"cliente_nombre"        doc:"Nombre del cliente"`
	Folio               string `json:"folio"                 doc:"Folio de la venta (DOCTOS_PV), vacío si no se resuelve"`
	DoctoPVID           int    `json:"docto_pv_id"           doc:"ID de la venta PV para cargar productos (0 si no se resuelve)"`
	Parcialidad         string `json:"parcialidad"           doc:"Cuota esperada en pesos (2 decimales)"`
	Frecuencia          string `json:"frecuencia"            doc:"Cadencia de pago: SEMANAL, QUINCENAL o MENSUAL"`
	AbonoSemana         string `json:"abono_semana"          doc:"Total abonado en la ventana semanal (2 decimales)"`
	Vencidas            string `json:"vencidas"              doc:"Cuotas vencidas al inicio de la ventana (puede ser fracción)"`
	Aporte              string `json:"aporte"                doc:"Aporte calculado para el reporte ponderado (puede ser fracción)"`
	Saldo               string `json:"saldo"                 doc:"Saldo pendiente actual (2 decimales)"`
	AplicaPonderado     bool   `json:"aplica_ponderado"      doc:"Si la venta cuenta en el denominador del % ponderado esta semana"`
	AtrasoAntesCuotas   string `json:"atraso_antes_cuotas"   doc:"Cuotas vencidas al inicio de la ventana"`
	AtrasoAntesPesos    string `json:"atraso_antes_pesos"    doc:"Atraso antes en pesos"`
	PagoCuotas          string `json:"pago_cuotas"           doc:"Pago de la semana en cuotas"`
	AtrasoDespuesCuotas string `json:"atraso_despues_cuotas" doc:"Cuotas que siguen vencidas tras el pago"`
	AtrasoDespuesPesos  string `json:"atraso_despues_pesos"  doc:"Atraso después en pesos"`
}

// ResumenPonderadoDTO summarises the weighted percentage calculation for the zona.
type ResumenPonderadoDTO struct {
	Numerador    string  `json:"numerador"      doc:"Σ aporte de las ventas que aplican (4 decimales)"`
	Denominador  int     `json:"denominador"    doc:"Número de ventas que aplican"`
	PctPonderado *string `json:"pct_ponderado"  doc:"Porcentaje ponderado (2 decimales) o null si denominador 0"`
}
