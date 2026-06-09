//nolint:misspell // Spanish vocabulary (usuarios, nombres) by convention.
package ventfb

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// UsuarioNombreRepo implements outbound.UsuarioNombreResolver by reading
// display names from MSP_USUARIOS. Used to label venta-event timeline entries
// with WHO triggered each event.
type UsuarioNombreRepo struct {
	pool *firebird.Pool
}

// NewUsuarioNombreRepo builds a UsuarioNombreRepo wired to the given pool.
func NewUsuarioNombreRepo(pool *firebird.Pool) *UsuarioNombreRepo {
	return &UsuarioNombreRepo{pool: pool}
}

// Compile-time check: UsuarioNombreRepo satisfies the outbound port.
var _ outbound.UsuarioNombreResolver = (*UsuarioNombreRepo)(nil)

// NombresPorID returns a map from usuario id to NOMBRE for every id that has a
// row in MSP_USUARIOS. Ids without a row are absent from the map (the caller
// treats them as "unknown actor"). Duplicate ids are collapsed before the
// query so the placeholder list stays bounded by the unique count.
func (r *UsuarioNombreRepo) NombresPorID(
	ctx context.Context, ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	unique := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	placeholders := strings.Repeat("?,", len(unique))
	placeholders = placeholders[:len(placeholders)-1] // drop trailing comma
	query := "SELECT ID, NOMBRE FROM MSP_USUARIOS WHERE ID IN (" + placeholders + ")"

	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id.String()
	}

	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rawID, nombre string
		if scanErr := rows.Scan(&rawID, &nombre); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		parsed, parseErr := uuid.Parse(strings.TrimSpace(rawID))
		if parseErr != nil {
			// MSP_USUARIOS.ID is CHAR(36) and only ever holds UUIDs we wrote.
			return nil, firebird.MapError(parseErr)
		}
		out[parsed] = strings.TrimSpace(nombre)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}
