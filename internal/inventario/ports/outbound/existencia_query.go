//nolint:misspell // domain vocabulary is Spanish (existencia, artículo, almacén, etc.) per project convention.
package outbound

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ExistenciaQuery provides stock-level reads from Microsip's SALDOS_IN table.
// It is a read-only projection port — no mutations go through here.
type ExistenciaQuery interface {
	// Existencia returns the current stock for the given artículo in the given
	// almacén. The value is SUM(ENTRADAS_UNIDADES) - SUM(SALIDAS_UNIDADES) from
	// SALDOS_IN. Can be zero or negative when items are oversold.
	Existencia(ctx context.Context, articuloID, almacenID int) (decimal.Decimal, error)

	// ExistenciasPorAlmacen returns a full stock snapshot for all artículos in
	// the given almacén.
	ExistenciasPorAlmacen(ctx context.Context, almacenID int) ([]domain.Existencia, error)
}
