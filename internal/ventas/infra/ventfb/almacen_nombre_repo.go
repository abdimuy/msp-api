//nolint:misspell // Spanish vocabulary (almacenes, nombres) by convention.
package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// almacenesCatalogo locates the almacén display names inside Microsip.
var almacenesCatalogo = catalogoNombreQuery{table: "ALMACENES", idColumn: "ALMACEN_ID"}

// AlmacenNombreRepo implements outbound.AlmacenNombreResolver by reading
// display names from Microsip's ALMACENES table. Used to label venta-event
// timeline entries for traspasos with WHERE the stock moved (camioneta →
// tienda) instead of opaque ALMACEN_IDs.
type AlmacenNombreRepo struct {
	pool *firebird.Pool
}

// NewAlmacenNombreRepo builds an AlmacenNombreRepo wired to the given pool.
func NewAlmacenNombreRepo(pool *firebird.Pool) *AlmacenNombreRepo {
	return &AlmacenNombreRepo{pool: pool}
}

// Compile-time check: AlmacenNombreRepo satisfies the outbound port.
var _ outbound.AlmacenNombreResolver = (*AlmacenNombreRepo)(nil)

// NombresPorID returns a map from almacén id to NOMBRE for every id that has a
// row in ALMACENES. Ids without a row are absent from the map (the caller
// treats them as "unknown almacén"). See catalogoNombreQuery.nombresPorID for
// the dedup and encoding contract.
func (r *AlmacenNombreRepo) NombresPorID(
	ctx context.Context, ids []int,
) (map[int]string, error) {
	return almacenesCatalogo.nombresPorID(ctx, r.pool, ids)
}
