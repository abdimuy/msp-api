package firebird

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// UsuarioRepo is the Firebird-backed implementation of outbound.UsuarioRepo.
//
// Every method routes its query through firebird.GetQuerier so it transparently
// joins an ambient transaction installed in the context (used by application
// services and the test harness) and otherwise falls back to the shared pool.
type UsuarioRepo struct {
	pool *firebird.Pool
}

// NewUsuarioRepo builds a UsuarioRepo wired to the given pool.
func NewUsuarioRepo(pool *firebird.Pool) *UsuarioRepo {
	return &UsuarioRepo{pool: pool}
}

// Compile-time check: UsuarioRepo satisfies the outbound port.
var _ outbound.UsuarioRepo = (*UsuarioRepo)(nil)

// Save inserts a new usuario. Unique violation on EMAIL/FIREBASE_UID is
// translated to domain.ErrUsuarioYaExiste.
func (r *UsuarioRepo) Save(ctx context.Context, u *domain.Usuario) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	var firebaseUID any
	if !u.FirebaseUID().IsZero() {
		firebaseUID = u.FirebaseUID().Value()
	}
	var telefono any
	if u.Telefono() != nil {
		telefono = u.Telefono().Value()
	}
	var almacenID any
	if u.AlmacenID() != nil {
		almacenID = *u.AlmacenID()
	}

	_, err := q.ExecContext(
		ctx, insertUsuario,
		u.ID().String(),
		firebaseUID,
		u.Email().Value(),
		u.Nombre().Value(),
		telefono,
		almacenID,
		u.Activo(),
		string(u.Estatus()),
		firebird.ToWallClock(u.CreatedAt()),
		firebird.ToWallClock(u.UpdatedAt()),
		u.CreatedBy().String(),
		u.UpdatedBy().String(),
	)
	if err != nil {
		return mapUniqueViolation(firebird.MapError(err), domain.ErrUsuarioYaExiste)
	}
	return nil
}

// Update writes back the mutable columns of u.
func (r *UsuarioRepo) Update(ctx context.Context, u *domain.Usuario) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	var firebaseUID any
	if !u.FirebaseUID().IsZero() {
		firebaseUID = u.FirebaseUID().Value()
	}
	var telefono any
	if u.Telefono() != nil {
		telefono = u.Telefono().Value()
	}
	var almacenID any
	if u.AlmacenID() != nil {
		almacenID = *u.AlmacenID()
	}

	res, err := q.ExecContext(
		ctx, updateUsuario,
		firebaseUID,
		u.Email().Value(),
		u.Nombre().Value(),
		telefono,
		almacenID,
		u.Activo(),
		string(u.Estatus()),
		firebird.ToWallClock(u.UpdatedAt()),
		u.UpdatedBy().String(),
		u.ID().String(),
	)
	if err != nil {
		return mapUniqueViolation(firebird.MapError(err), domain.ErrUsuarioYaExiste)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n == 0 {
		return domain.ErrUsuarioNotFound
	}
	return nil
}

// FindByID loads a usuario by primary key.
func (r *UsuarioRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Usuario, error) {
	return r.findOne(ctx, selectUsuarioByID, id.String())
}

// FindByFirebaseUID loads a usuario by its Firebase identity provider uid.
func (r *UsuarioRepo) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*domain.Usuario, error) {
	return r.findOne(ctx, selectUsuarioByFirebaseUID, firebaseUID)
}

// FindByEmail loads a usuario by email.
func (r *UsuarioRepo) FindByEmail(ctx context.Context, email string) (*domain.Usuario, error) {
	return r.findOne(ctx, selectUsuarioByEmail, email)
}

func (r *UsuarioRepo) findOne(ctx context.Context, query string, arg any) (*domain.Usuario, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, query, arg)
	u, err := usuarioFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrUsuarioNotFound
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return u, nil
}

// List returns a cursor-paginated page of usuarios ordered by (CREATED_AT, ID).
func (r *UsuarioRepo) List(ctx context.Context, p outbound.ListParams) (outbound.Page[*domain.Usuario], error) {
	return queryPage(
		ctx, r.pool, p,
		selectUsuariosFirstPage, selectUsuariosAfterCursor,
		usuarioFromRow,
	)
}

// AsignarRol attaches a rol to a usuario. Idempotent — re-assigning the same
// rol returns nil.
func (r *UsuarioRepo) AsignarRol(ctx context.Context, usuarioID, rolID, by uuid.UUID, now time.Time) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(
		ctx, insertUsuarioRol,
		usuarioID.String(), rolID.String(), firebird.ToWallClock(now), by.String(),
	)
	if err == nil {
		return nil
	}
	mapped := firebird.MapError(err)
	if isUniqueViolation(mapped) {
		return nil
	}
	return mapped
}

// RevocarRol detaches a rol from a usuario. Idempotent — revoking an
// already-unassigned rol returns nil.
func (r *UsuarioRepo) RevocarRol(ctx context.Context, usuarioID, rolID uuid.UUID) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteUsuarioRol, usuarioID.String(), rolID.String()); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// PermisosFor returns the flattened union of permission codes granted to the
// usuario via every active rol it owns.
func (r *UsuarioRepo) PermisosFor(ctx context.Context, usuarioID uuid.UUID) ([]domain.Permission, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectPermisosForUsuario, usuarioID.String())
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

// RolesFor returns every rol attached to the usuario, ordered by name.
func (r *UsuarioRepo) RolesFor(ctx context.Context, usuarioID uuid.UUID) ([]*domain.Rol, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectRolesForUsuario, usuarioID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.Rol
	for rows.Next() {
		rol, scanErr := rolFromRow(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, rol)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// isUniqueViolation reports whether err — already passed through MapError —
// is the Firebird unique-violation apperror.
func isUniqueViolation(err error) bool {
	appErr, ok := apperror.As(err)
	if !ok {
		return false
	}
	return appErr.Code == "firebird_unique_violation"
}

// mapUniqueViolation rewrites a generic firebird_unique_violation into the
// supplied domain sentinel so the application layer can pattern-match.
func mapUniqueViolation(err error, replacement *apperror.Error) error {
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return replacement.WithError(err)
	}
	return err
}
