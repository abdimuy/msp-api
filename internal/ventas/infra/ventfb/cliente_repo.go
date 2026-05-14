//nolint:misspell // Spanish vocabulary (clientes) by convention.
package ventfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ClienteRepo implements outbound.ClienteExistenceChecker by consulting
// Microsip's native CLIENTES table for the supplied CLIENTE_ID.
type ClienteRepo struct {
	pool *firebird.Pool
}

// NewClienteRepo builds a ClienteRepo wired to the given pool.
func NewClienteRepo(pool *firebird.Pool) *ClienteRepo {
	return &ClienteRepo{pool: pool}
}

// Compile-time check: ClienteRepo satisfies the outbound port.
var _ outbound.ClienteExistenceChecker = (*ClienteRepo)(nil)

// Exists reports whether a row with the supplied CLIENTE_ID exists in
// CLIENTES. Non-positive ids short-circuit to (false, nil) — they cannot
// match a real Microsip cliente identifier.
func (r *ClienteRepo) Exists(ctx context.Context, clienteID int) (bool, error) {
	if clienteID <= 0 {
		return false, nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var one int
	err := q.QueryRowContext(ctx, selectClienteExists, clienteID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, firebird.MapError(err)
	}
	return true, nil
}
