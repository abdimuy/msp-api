//nolint:misspell // Spanish vocabulary (zonas, nombres) by convention.
package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// zonasClientesCatalogo locates the zona display names inside Microsip.
var zonasClientesCatalogo = catalogoNombreQuery{table: "ZONAS_CLIENTES", idColumn: "ZONA_CLIENTE_ID"}

// ZonaNombreRepo implements outbound.ZonaNombreResolver by reading display
// names from Microsip's ZONAS_CLIENTES table. Used to label a venta's
// direccion with the zona NAME instead of the opaque ZONA_CLIENTE_ID.
type ZonaNombreRepo struct {
	pool *firebird.Pool
}

// NewZonaNombreRepo builds a ZonaNombreRepo wired to the given pool.
func NewZonaNombreRepo(pool *firebird.Pool) *ZonaNombreRepo {
	return &ZonaNombreRepo{pool: pool}
}

// Compile-time check: ZonaNombreRepo satisfies the outbound port.
var _ outbound.ZonaNombreResolver = (*ZonaNombreRepo)(nil)

// NombresPorID returns a map from zona id to NOMBRE for every id that has a
// row in ZONAS_CLIENTES. Ids without a row are absent from the map (the
// caller treats them as "unknown zona"). See catalogoNombreQuery.nombresPorID
// for the dedup and encoding contract.
func (r *ZonaNombreRepo) NombresPorID(
	ctx context.Context, ids []int,
) (map[int]string, error) {
	return zonasClientesCatalogo.nombresPorID(ctx, r.pool, ids)
}
