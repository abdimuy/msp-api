//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ExistenciaQuerier implements [outbound.ExistenciaQuery] against Microsip's
// SALDOS_IN table.
type ExistenciaQuerier struct {
	pool *firebird.Pool
}

// NewExistenciaQuerier returns an ExistenciaQuerier wired to the given pool.
func NewExistenciaQuerier(pool *firebird.Pool) *ExistenciaQuerier {
	return &ExistenciaQuerier{pool: pool}
}

// Compile-time check.
var _ outbound.ExistenciaQuery = (*ExistenciaQuerier)(nil)

// Existencia returns the net stock for one article in one warehouse.
// A non-existent article/warehouse combination returns decimal.Zero.
func (eq *ExistenciaQuerier) Existencia(ctx context.Context, articuloID, almacenID int) (decimal.Decimal, error) {
	q := firebird.GetQuerier(ctx, eq.pool.DB)

	var raw any
	if err := q.QueryRowContext(ctx, selectExistencia, almacenID, articuloID).Scan(&raw); err != nil {
		return decimal.Zero, fmt.Errorf("invfb Existencia articuloID=%d almacenID=%d: %w",
			articuloID, almacenID, firebird.MapError(err))
	}
	d, err := firebird.ScanDecimal(raw, existenciaScale)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invfb Existencia scan decimal: %w", err)
	}
	return d, nil
}

// ExistenciasPorAlmacen returns current stock for every article in the given
// warehouse. Returns an empty slice when the warehouse has no SALDOS_IN rows.
func (eq *ExistenciaQuerier) ExistenciasPorAlmacen(ctx context.Context, almacenID int) ([]domain.Existencia, error) {
	q := firebird.GetQuerier(ctx, eq.pool.DB)

	rows, err := q.QueryContext(ctx, selectExistenciasPorAlmacen, almacenID)
	if err != nil {
		return nil, fmt.Errorf("invfb ExistenciasPorAlmacen almacenID=%d: %w", almacenID, firebird.MapError(err))
	}
	defer func() { _ = rows.Close() }()

	var result []domain.Existencia
	for rows.Next() {
		var articuloID int
		var cantRaw any
		if err := rows.Scan(&articuloID, &cantRaw); err != nil {
			return nil, fmt.Errorf("invfb ExistenciasPorAlmacen scan: %w", err)
		}
		cant, err := firebird.ScanDecimal(cantRaw, existenciaScale)
		if err != nil {
			return nil, fmt.Errorf("invfb ExistenciasPorAlmacen decimal articulo_id=%d: %w", articuloID, err)
		}
		result = append(result, domain.Existencia{
			ArticuloID: articuloID,
			AlmacenID:  almacenID,
			Cantidad:   cant,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invfb ExistenciasPorAlmacen rows.Err: %w", firebird.MapError(err))
	}
	return result, nil
}
