//nolint:misspell // Spanish vocabulary (clientes) per project convention.
package domain

// ZonaCliente mirrors a row from Microsip's ZONAS_CLIENTES table. The
// Nombre field is the raw zona name as stored in Microsip — the legacy
// API appends the top cobrador to disambiguate zonas that share a base
// name; that concatenation is done in the repo layer, not here.
type ZonaCliente struct {
	ID     int
	Nombre string
}
