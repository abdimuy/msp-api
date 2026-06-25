// Package rutashttp is the rutas module's HTTP transport: handlers, DTOs, and
// the Huma-over-chi router mount point.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

// ListRutasInput has no query parameters — the endpoint returns all zonas.
type ListRutasInput struct{}

// ListRutasOutput wraps the response body for GET /rutas.
type ListRutasOutput struct {
	Body struct {
		Items []RutaResumenDTO `json:"items"`
	}
}

// ListReporteUsuariosInput has no query parameters — returns all active cobradores.
type ListReporteUsuariosInput struct{}

// ListReporteUsuariosOutput wraps the response body for GET /rutas/reporte-usuarios.
type ListReporteUsuariosOutput struct {
	Body struct {
		Items []ReporteUsuarioDTO `json:"items"`
	}
}

// ReporteUsuarioDTO is the wire representation of one cobrador USER's weekly report.
// NumClientes/SaldoTotal are per-zona (shared across users of the same ruta); the
// percentages are per-user, computed over that user's FechaInicio window.
type ReporteUsuarioDTO struct {
	UID                 string  `json:"uid"                   doc:"ID del usuario en Firestore"`
	Nombre              string  `json:"nombre"                doc:"Nombre del cobrador"`
	Email               string  `json:"email"                 doc:"Correo del cobrador"`
	CobradorID          int     `json:"cobrador_id"           doc:"COBRADOR_ID de Microsip"`
	ZonaID              int     `json:"zona_id"               doc:"ZONA_CLIENTE_ID (ruta) del cobrador"`
	ZonaNombre          string  `json:"zona_nombre"           doc:"Nombre de la ruta; vacío si la zona no se resuelve"`
	NumClientes         int     `json:"num_clientes"          doc:"Clientes activos de la ruta (por zona, compartido entre usuarios de la misma ruta)"`
	SaldoTotal          string  `json:"saldo_total"           doc:"Saldo pendiente total de la ruta en pesos (2 decimales)"`
	PctCoberturaSemanal *string `json:"pct_cobertura_semanal" doc:"Cobertura sobre la ventana de ESTE usuario. Nulo si no se pudo calcular."`
	PctPonderadoSemanal *string `json:"pct_ponderado_semanal" doc:"Ponderado sobre la ventana de ESTE usuario. Nulo si no se pudo calcular."`
	CoberturaNum        int     `json:"cobertura_num"         doc:"Ventas que pagaron en la ventana (numerador de cobertura)"`
	CoberturaDen        int     `json:"cobertura_den"         doc:"Total de ventas en cartera activa (divisor de cobertura)"`
	PonderadoDen        int     `json:"ponderado_den"         doc:"Ventas que aplican esta semana (divisor del ponderado)"`
	FechaInicioSemana   string  `json:"fecha_inicio_semana"   doc:"Inicio de ventana del usuario (FECHA_CARGA_INICIAL, RFC3339 UTC)"`
}

// RutaResumenDTO is the wire representation of one zona summary.
// SaldoTotal is a string to avoid floating-point rounding — consistent
// with the money convention used across the project (see clienteshttp DTOs).
type RutaResumenDTO struct {
	ZonaID              int     `json:"zona_id"          doc:"ID de la zona de ventas"`
	ZonaNombre          string  `json:"zona_nombre"      doc:"Nombre de la zona de ventas"`
	CobradorID          *int    `json:"cobrador_id"      doc:"ID del cobrador asignado; nulo cuando la zona no tiene cobrador"`
	CobradorNombre      string  `json:"cobrador_nombre"  doc:"Nombre del cobrador asignado; vacío cuando no hay cobrador"`
	NumClientes         int     `json:"num_clientes"          doc:"Número de clientes activos (ESTATUS A o B) en la zona"`
	SaldoTotal          string  `json:"saldo_total"           doc:"Saldo pendiente total de la zona en pesos (2 decimales)"`
	PctCoberturaSemanal *string `json:"pct_cobertura_semanal" doc:"Porcentaje de cobertura semanal (ventas que pagaron / total). Nulo si el cobrador no tiene fecha de inicio de semana en Firestore."`
	PctPonderadoSemanal *string `json:"pct_ponderado_semanal" doc:"Porcentaje ponderado semanal (aporte total / denominador). Puede superar 100%. Nulo si el cobrador no tiene fecha de inicio."`
	FechaInicioSemana   *string `json:"fecha_inicio_semana"   doc:"Fecha de inicio de semana del cobrador (RFC3339 UTC). Nulo si no configurado."`
}
