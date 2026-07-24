//nolint:misspell // Spanish domain vocabulary (categorias, cliente) by project convention.
package outbound

import "context"

// CategoriasClienteReader reads the set of product lines (categorías) a cliente
// has already purchased. The copiloto's next-best-product logic uses it to bias
// suggestions toward a line the cliente does NOT already own (a personalized
// offer). Implementations live in infra (a Firebird read over the Microsip
// venta history).
type CategoriasClienteReader interface {
	// CategoriasCompradas returns the DISTINCT LINEA_ARTICULO_IDs the cliente
	// has bought, in no particular order. A cliente with no purchase history
	// yields an empty (non-nil) slice, not an error. A non-nil error is a real
	// infra failure — callers may degrade to "no personalization" on error.
	CategoriasCompradas(ctx context.Context, clienteID int) ([]int, error)
}
