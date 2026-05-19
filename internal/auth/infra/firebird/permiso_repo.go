//nolint:misspell // Spanish column names (DESCRIPCION) match the Firebird schema exactly.
package firebird

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// PermisoRepo is the Firebird-backed implementation of outbound.PermisoRepo.
type PermisoRepo struct {
	pool *firebird.Pool
}

// NewPermisoRepo builds a PermisoRepo wired to the given pool.
func NewPermisoRepo(pool *firebird.Pool) *PermisoRepo {
	return &PermisoRepo{pool: pool}
}

// Compile-time check: PermisoRepo satisfies the outbound port.
var _ outbound.PermisoRepo = (*PermisoRepo)(nil)

// UpsertCatalog reconciles MSP_PERMISOS with the in-code catalog using
// Firebird's UPDATE OR INSERT statement (a true upsert keyed on CODIGO).
// Rows whose CODIGO is not in perms are left in place — pruning is mediated
// by FindOrphans.
func (r *PermisoRepo) UpsertCatalog(ctx context.Context, perms []domain.PermissionMeta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, p := range perms {
		if _, err := q.ExecContext(
			ctx, upsertPermiso,
			p.Code.Code(), p.Description, p.Categoria,
		); err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

// FindByCodigo loads a permiso by code. Returns ErrPermisoNotFound on miss.
func (r *PermisoRepo) FindByCodigo(ctx context.Context, codigo domain.Permission) (*domain.Permiso, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, selectPermisoByCodigo, codigo.Code())
	p, err := permisoFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrPermisoNotFound
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return p, nil
}

// FindAll returns every permiso currently in the catalog, ordered by CODIGO.
func (r *PermisoRepo) FindAll(ctx context.Context) ([]*domain.Permiso, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectAllPermisos)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.Permiso
	for rows.Next() {
		p, scanErr := permisoFromRow(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, p)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}

// FindOrphans returns the permission codes persisted in MSP_PERMISOS that
// are NOT in `known`. An empty `known` slice returns every catalog row.
func (r *PermisoRepo) FindOrphans(ctx context.Context, known []domain.Permission) ([]domain.Permission, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	var (
		rows *sql.Rows
		err  error
	)
	if len(known) == 0 {
		rows, err = q.QueryContext(ctx, selectAllPermisoCodigos)
	} else {
		placeholders := make([]string, len(known))
		args := make([]any, len(known))
		for i, k := range known {
			placeholders[i] = "?"
			args[i] = k.Code()
		}
		query := "SELECT CODIGO FROM MSP_PERMISOS WHERE CODIGO NOT IN (" +
			strings.Join(placeholders, ", ") + ") ORDER BY CODIGO"
		rows, err = q.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Permission
	for rows.Next() {
		var code string
		if scanErr := rows.Scan(&code); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, domain.Permission(code))
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}
