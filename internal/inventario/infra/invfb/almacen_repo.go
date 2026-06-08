//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// AlmacenRepoFB implements [outbound.AlmacenRepo] against Microsip's ALMACENES
// table.
type AlmacenRepoFB struct {
	pool *firebird.Pool
}

// NewAlmacenRepo returns an AlmacenRepoFB wired to the given pool.
func NewAlmacenRepo(pool *firebird.Pool) *AlmacenRepoFB {
	return &AlmacenRepoFB{pool: pool}
}

// Compile-time check.
var _ outbound.AlmacenRepo = (*AlmacenRepoFB)(nil)

// FindByID returns the almacén with the given id.
// Returns [domain.ErrAlmacenNoEncontrado] when not found.
func (ar *AlmacenRepoFB) FindByID(ctx context.Context, id int) (*domain.Almacen, error) {
	q := firebird.GetQuerier(ctx, ar.pool.DB)

	var almacenID int
	var nombre string
	err := q.QueryRowContext(ctx, selectAlmacenByID, id).Scan(&almacenID, &nombre)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAlmacenNoEncontrado.WithField("almacen_id", id)
	}
	if err != nil {
		return nil, fmt.Errorf("invfb FindByID almacen_id=%d: %w", id, firebird.MapError(err))
	}
	a := domain.NewAlmacen(almacenID, nombre)
	return &a, nil
}

// ListAll returns the full almacenes catalog sorted by NOMBRE.
func (ar *AlmacenRepoFB) ListAll(ctx context.Context) ([]domain.Almacen, error) {
	q := firebird.GetQuerier(ctx, ar.pool.DB)

	rows, err := q.QueryContext(ctx, selectAllAlmacenes)
	if err != nil {
		return nil, fmt.Errorf("invfb ListAll almacenes: %w", firebird.MapError(err))
	}
	defer func() { _ = rows.Close() }()

	var result []domain.Almacen
	for rows.Next() {
		var id int
		var nombre string
		if err := rows.Scan(&id, &nombre); err != nil {
			return nil, fmt.Errorf("invfb ListAll almacenes scan: %w", err)
		}
		result = append(result, domain.NewAlmacen(id, nombre))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invfb ListAll almacenes rows.Err: %w", firebird.MapError(err))
	}
	return result, nil
}
