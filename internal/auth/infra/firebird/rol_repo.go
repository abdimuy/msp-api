//nolint:misspell // Spanish column names (DESCRIPCION) match the Firebird schema exactly.
package firebird

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// RolRepo is the Firebird-backed implementation of outbound.RolRepo.
type RolRepo struct {
	pool *firebird.Pool
}

// NewRolRepo builds a RolRepo wired to the given pool.
func NewRolRepo(pool *firebird.Pool) *RolRepo {
	return &RolRepo{pool: pool}
}

// Compile-time check: RolRepo satisfies the outbound port.
var _ outbound.RolRepo = (*RolRepo)(nil)

// Save inserts a new rol. Name collision is translated to
// domain.ErrRolYaExiste.
func (r *RolRepo) Save(ctx context.Context, rol *domain.Rol) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	args := rolInsertArgs(rol)
	if _, err := q.ExecContext(ctx, insertRol, args...); err != nil {
		return mapUniqueViolation(firebird.MapError(err), domain.ErrRolYaExiste)
	}
	return nil
}

// rolInsertArgs flattens a rol entity into the parameter list for insertRol,
// keeping Save short enough for funlen.
func rolInsertArgs(rol *domain.Rol) []any {
	return []any{
		rol.ID().String(),
		rol.Nombre(),
		nullableStringArg(rol.Description()),
		rol.Inmutable(),
		rol.Activo(),
		firebird.ToWallClock(rol.CreatedAt()),
		firebird.ToWallClock(rol.UpdatedAt()),
		rol.CreatedBy().String(),
		rol.UpdatedBy().String(),
	}
}

// nullableStringArg returns *s as driver arg, or nil for SQL NULL.
func nullableStringArg(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// Update writes back the mutable columns of r.
func (r *RolRepo) Update(ctx context.Context, rol *domain.Rol) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	res, err := q.ExecContext(
		ctx, updateRol,
		rol.Nombre(),
		nullableStringArg(rol.Description()),
		rol.Inmutable(),
		rol.Activo(),
		firebird.ToWallClock(rol.UpdatedAt()),
		rol.UpdatedBy().String(),
		rol.ID().String(),
	)
	if err != nil {
		return mapUniqueViolation(firebird.MapError(err), domain.ErrRolYaExiste)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n == 0 {
		return domain.ErrRolNotFound
	}
	return nil
}

// FindByID loads a rol by primary key.
func (r *RolRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Rol, error) {
	return r.findOne(ctx, selectRolByID, id.String())
}

// FindByNombre loads a rol by its unique name.
func (r *RolRepo) FindByNombre(ctx context.Context, nombre string) (*domain.Rol, error) {
	return r.findOne(ctx, selectRolByNombre, nombre)
}

func (r *RolRepo) findOne(ctx context.Context, query string, arg any) (*domain.Rol, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, query, arg)
	rol, err := rolFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrRolNotFound
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return rol, nil
}

// List returns a cursor-paginated page of roles ordered by (CREATED_AT, ID).
func (r *RolRepo) List(ctx context.Context, p outbound.ListParams) (outbound.Page[*domain.Rol], error) {
	return queryPage(
		ctx, r.pool, p,
		selectRolesFirstPage, selectRolesAfterCursor,
		rolFromRow,
	)
}

// UpsertInmutableByName implements the catalog-sync flow: insert if missing,
// no-op if an inmutable rol already exists with that name, conflict if a
// non-inmutable rol shadows the seed name.
func (r *RolRepo) UpsertInmutableByName(ctx context.Context, rol *domain.Rol) error {
	existing, err := r.FindByNombre(ctx, rol.Nombre())
	if err == nil {
		if existing.Inmutable() {
			return nil
		}
		return apperror.NewConflict(
			"rol_seed_conflict",
			"ya existe un rol con ese nombre pero no es inmutable",
		).WithField("nombre", rol.Nombre())
	}
	if !errors.Is(err, domain.ErrRolNotFound) {
		return err
	}
	return r.Save(ctx, rol)
}

// AsignarPermiso attaches a permission code to the rol. Idempotent.
func (r *RolRepo) AsignarPermiso(ctx context.Context, rolID uuid.UUID, codigo domain.Permission, by uuid.UUID, now time.Time) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(
		ctx, insertRolPermiso,
		rolID.String(), codigo.Code(), firebird.ToWallClock(now), by.String(),
	)
	if err == nil {
		return nil
	}
	mapped := firebird.MapError(err)
	if isUniqueViolation(mapped) {
		return nil
	}
	if fkViolationOn(mapped, fkConstraintRolesPermisosPermiso) {
		return domain.ErrPermisoNotFound.WithField("codigo", codigo.Code())
	}
	return mapped
}

// RevocarPermiso detaches a permission code from the rol. Idempotent.
func (r *RolRepo) RevocarPermiso(ctx context.Context, rolID uuid.UUID, codigo domain.Permission) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteRolPermiso, rolID.String(), codigo.Code()); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// SyncPermisos replaces the rol's permission set with codigos atomically.
// Expects the caller to wrap the call in a transaction (the GetQuerier
// abstraction lets the existing tx be reused). Unknown codigos surface as
// domain.ErrPermisoNotFound.
func (r *RolRepo) SyncPermisos(ctx context.Context, rolID uuid.UUID, codigos []domain.Permission, by uuid.UUID, now time.Time) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteAllRolPermisos, rolID.String()); err != nil {
		return firebird.MapError(err)
	}
	wall := firebird.ToWallClock(now)
	for _, codigo := range codigos {
		if _, err := q.ExecContext(
			ctx, insertRolPermiso,
			rolID.String(), codigo.Code(), wall, by.String(),
		); err != nil {
			mapped := firebird.MapError(err)
			if fkViolationOn(mapped, fkConstraintRolesPermisosPermiso) {
				return domain.ErrPermisoNotFound.WithField("codigo", codigo.Code())
			}
			return mapped
		}
	}
	return nil
}

// PermisosFor returns every permission code attached to the rol, ordered by
// code for deterministic output.
func (r *RolRepo) PermisosFor(ctx context.Context, rolID uuid.UUID) ([]domain.Permission, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectPermisosForRol, rolID.String())
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

// isFKViolation reports whether err — already MapError-translated — is the
// Firebird foreign-key apperror.
func isFKViolation(err error) bool {
	appErr, ok := apperror.As(err)
	if !ok {
		return false
	}
	return appErr.Code == "firebird_fk_violation"
}

// fkViolationOn reports whether err is an FK violation triggered by the
// named constraint. Firebird's mapped apperror flattens every FK error
// to the same code, so to distinguish "permiso missing" from "usuario
// missing" we match the underlying message — which preserves the
// constraint name verbatim ("violation of FOREIGN KEY constraint
// \"FK_FOO\" on table ..."). Avoids mis-reporting an unrelated FK
// breach as a missing-permiso error.
func fkViolationOn(err error, constraint string) bool {
	if !isFKViolation(err) {
		return false
	}
	return strings.Contains(err.Error(), constraint)
}

// fkConstraintRolesPermisosPermiso is the name (declared in migration
// 000001_create_auth_tables.up.sql) of the FK on
// MSP_ROLES_PERMISOS.PERMISO_CODIGO → MSP_PERMISOS.CODIGO. Used by
// fkViolationOn to single out the "unknown permission code" path.
const fkConstraintRolesPermisosPermiso = "FK_MSP_ROLES_PERMISOS_PERMISO"
