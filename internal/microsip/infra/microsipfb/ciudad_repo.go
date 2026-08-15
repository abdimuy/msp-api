//nolint:misspell // Spanish vocabulary (ciudades, estados) per project convention.
package microsipfb

import (
	"context"
	"database/sql"
	"strings"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
	"github.com/abdimuy/msp-api/internal/microsip/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// CiudadRepo is the Firebird-backed implementation of outbound.CiudadRepo.
//
// CIUDADES is a legacy Microsip table, so names are decoded with
// firebird.Win1252 like every other legacy read in this module.
type CiudadRepo struct {
	pool *firebird.Pool
}

// NewCiudadRepo wires a CiudadRepo to the given pool.
func NewCiudadRepo(pool *firebird.Pool) *CiudadRepo {
	return &CiudadRepo{pool: pool}
}

// Compile-time check.
var _ outbound.CiudadRepo = (*CiudadRepo)(nil)

// Listar returns the full ciudades catalog, each row carrying its own estado.
//
// Names are trimmed: the production catalog has rows stored with a trailing
// space ("COYOMEAPAN ", "ESPERANZA "), and an exact-match lookup against the
// captured text would miss them.
func (r *CiudadRepo) Listar(ctx context.Context) ([]domain.Ciudad, error) {
	rows, err := r.pool.QueryContext(ctx, selectCiudades)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Ciudad
	for rows.Next() {
		var (
			id       int
			nombre   firebird.Win1252
			estadoID sql.NullInt64
			estado   *firebird.Win1252
		)
		if err := rows.Scan(&id, &nombre, &estadoID, &estado); err != nil {
			return nil, firebird.MapError(err)
		}
		c := domain.Ciudad{
			ID:     id,
			Nombre: strings.TrimSpace(string(nombre)),
		}
		if estadoID.Valid {
			c.EstadoID = int(estadoID.Int64)
		}
		if estado != nil {
			c.Estado = strings.TrimSpace(string(*estado))
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}
