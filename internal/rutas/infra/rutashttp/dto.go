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

// RutaResumenDTO is the wire representation of one zona summary.
// SaldoTotal is a string to avoid floating-point rounding — consistent
// with the money convention used across the project (see clienteshttp DTOs).
type RutaResumenDTO struct {
	ZonaID         int    `json:"zona_id"          doc:"ID de la zona de ventas"`
	ZonaNombre     string `json:"zona_nombre"      doc:"Nombre de la zona de ventas"`
	CobradorID     *int   `json:"cobrador_id"      doc:"ID del cobrador asignado; nulo cuando la zona no tiene cobrador"`
	CobradorNombre string `json:"cobrador_nombre"  doc:"Nombre del cobrador asignado; vacío cuando no hay cobrador"`
	NumClientes    int    `json:"num_clientes"     doc:"Número de clientes activos (ESTATUS A o B) en la zona"`
	SaldoTotal     string `json:"saldo_total"      doc:"Saldo pendiente total de la zona en pesos (2 decimales)"`
}
