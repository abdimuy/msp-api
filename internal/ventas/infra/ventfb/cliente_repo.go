//nolint:misspell // Spanish vocabulary (clientes) by convention.
package ventfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
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

// Compile-time check: ClienteRepo satisfies the outbound ports.
var _ outbound.ClienteExistenceChecker = (*ClienteRepo)(nil)

// Compile-time: ClienteRepo also satisfies ClienteZonaReader.
var _ outbound.ClienteZonaReader = (*ClienteRepo)(nil)

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

// ZonaDeCliente reads the ZONA_CLIENTE_ID from CLIENTES for the given
// clienteID. Returns (nil, nil) when the row exists but ZONA_CLIENTE_ID is
// NULL — the caller treats nil as "no zona constraint". Returns
// (nil, domain.ErrClienteNotFoundInMicrosip) when no row exists.
func (r *ClienteRepo) ZonaDeCliente(ctx context.Context, clienteID int) (*int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var zona sql.NullInt32
	err := q.QueryRowContext(ctx, selectClienteZona, clienteID).Scan(&zona)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrClienteNotFoundInMicrosip
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	if !zona.Valid {
		return nil, nil //nolint:nilnil // NULL zona means "no constraint" — both nil is the intended sentinel.
	}
	z := int(zona.Int32)
	return &z, nil
}
