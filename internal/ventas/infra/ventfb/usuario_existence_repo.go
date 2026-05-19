//nolint:misspell // Spanish vocabulary (usuarios, vendedores) by convention.
package ventfb

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// UsuarioExistenceRepo implements outbound.VendedorUsuarioExistenceChecker by
// consulting MSP_USUARIOS for the supplied ids. It is invoked from the ventas
// service before any INSERT into MSP_VENTAS_VENDEDORES so unknown usuario_ids
// surface as a 422 vendedor_usuario_no_encontrado instead of bubbling out of
// the DB as a 409 firebird_fk_violation.
type UsuarioExistenceRepo struct {
	pool *firebird.Pool
}

// NewUsuarioExistenceRepo builds a UsuarioExistenceRepo wired to the given pool.
func NewUsuarioExistenceRepo(pool *firebird.Pool) *UsuarioExistenceRepo {
	return &UsuarioExistenceRepo{pool: pool}
}

// Compile-time check: UsuarioExistenceRepo satisfies the outbound port.
var _ outbound.VendedorUsuarioExistenceChecker = (*UsuarioExistenceRepo)(nil)

// MissingIDs returns the subset of ids that have no matching row in
// MSP_USUARIOS. Duplicate ids in the input are collapsed before the query
// so a (?,?,?…) placeholder list stays bounded by the unique count.
func (r *UsuarioExistenceRepo) MissingIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}

	// Dedupe while preserving insertion order for stable error messages.
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
	query := "SELECT ID FROM MSP_USUARIOS WHERE ID IN (" + placeholders + ")"

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

	found := make(map[uuid.UUID]struct{}, len(unique))
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, firebird.MapError(err)
		}
		parsed, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			// MSP_USUARIOS.ID is CHAR(36) and only ever holds UUIDs we wrote;
			// an unparseable value means the row was tampered with externally.
			return nil, firebird.MapError(err)
		}
		found[parsed] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	missing := make([]uuid.UUID, 0)
	for _, id := range unique {
		if _, ok := found[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}
