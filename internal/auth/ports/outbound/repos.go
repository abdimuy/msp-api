// Package outbound declares the interfaces the auth module needs from the
// outside world. Implementations live in internal/auth/infra/* and are
// wired together at composition root via fx providers.
package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// ListParams is the cursor-pagination input accepted by every List method
// on every repo in this package. Cursor is opaque to the caller (server
// encodes/decodes it); PageSize is the desired page size, with the repo
// applying its own minimum/maximum if necessary.
type ListParams struct {
	Cursor   string
	PageSize int
}

// Page is the generic cursor-paginated result returned by List methods.
// NextCursor is the empty string when there are no more pages.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// UsuarioRepo persists and retrieves Usuario entities and their
// rol/permiso associations. All methods accept a context so callers can
// thread tracing, deadlines, and transactions down.
//
//nolint:interfacebloat // contract defined by auth module phase-1 spec
type UsuarioRepo interface {
	// Save inserts a new usuario. Returns ErrUsuarioYaExiste if a row with
	// the same email or firebase_uid already exists.
	Save(ctx context.Context, u *domain.Usuario) error

	// Update writes back all mutable fields of u, including any
	// rename-for-soft-delete the domain method emitted.
	Update(ctx context.Context, u *domain.Usuario) error

	// FindByID loads a usuario by primary key. Returns ErrUsuarioNotFound
	// on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Usuario, error)

	// FindByFirebaseUID loads a usuario by its Firebase Authentication uid.
	// Used by the auth middleware on every authenticated request.
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*domain.Usuario, error)

	// FindByEmail loads a usuario by email. Used at registration time and
	// for admin lookups.
	FindByEmail(ctx context.Context, email string) (*domain.Usuario, error)

	// List returns a cursor-paginated page of usuarios ordered by a stable
	// key (typically created_at, id).
	List(ctx context.Context, p ListParams) (Page[*domain.Usuario], error)

	// AsignarRol attaches a rol to a usuario. Idempotent: re-assigning the
	// same rol returns nil. `by` is recorded as the granting user.
	AsignarRol(ctx context.Context, usuarioID, rolID, by uuid.UUID, now time.Time) error

	// RevocarRol detaches a rol from a usuario. Idempotent: revoking an
	// already-unassigned rol returns nil.
	RevocarRol(ctx context.Context, usuarioID, rolID uuid.UUID) error

	// PermisosFor returns the flattened union of permisos granted to the
	// usuario via every active rol it owns. Used by the auth middleware.
	PermisosFor(ctx context.Context, usuarioID uuid.UUID) ([]domain.Permission, error)

	// RolesFor returns every rol attached to the usuario.
	RolesFor(ctx context.Context, usuarioID uuid.UUID) ([]*domain.Rol, error)
}

// RolRepo persists and retrieves Rol entities along with their permiso
// catalog entries.
//
//nolint:interfacebloat // see UsuarioRepo: the surface is defined by spec.
type RolRepo interface {
	// Save inserts a new rol. Returns ErrRolYaExiste on name collision.
	Save(ctx context.Context, r *domain.Rol) error

	// Update writes back mutable fields of r. Domain-side inmutable
	// enforcement runs in Rol.Update, so the repo can assume the caller
	// already verified the rol is mutable.
	Update(ctx context.Context, r *domain.Rol) error

	// FindByID loads a rol by primary key. Returns ErrRolNotFound on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Rol, error)

	// FindByNombre loads a rol by its unique name. Used by the
	// inmutable-catalog sync routine at boot.
	FindByNombre(ctx context.Context, nombre string) (*domain.Rol, error)

	// List returns a cursor-paginated page of roles.
	List(ctx context.Context, p ListParams) (Page[*domain.Rol], error)

	// UpsertInmutableByName creates the rol if it does not exist (matched
	// by name) or refreshes its description otherwise. Used by the
	// catalog-sync routine that ships built-in roles.
	UpsertInmutableByName(ctx context.Context, r *domain.Rol) error

	// AsignarPermiso attaches a permission code to the rol. Idempotent.
	AsignarPermiso(ctx context.Context, rolID uuid.UUID, codigo domain.Permission, by uuid.UUID, now time.Time) error

	// RevocarPermiso detaches a permission code from the rol. Idempotent.
	RevocarPermiso(ctx context.Context, rolID uuid.UUID, codigo domain.Permission) error

	// SyncPermisos replaces the rol's permission set with codigos in a
	// single transaction. Used when assigning the canonical set to a
	// rol via the admin UI.
	SyncPermisos(ctx context.Context, rolID uuid.UUID, codigos []domain.Permission, by uuid.UUID, now time.Time) error

	// PermisosFor returns every permission code attached to the rol.
	PermisosFor(ctx context.Context, rolID uuid.UUID) ([]domain.Permission, error)
}

// PermisoRepo persists and retrieves the global permission catalog. The
// catalog is a snapshot of domain.AllPermissions() applied at boot.
type PermisoRepo interface {
	// UpsertCatalog reconciles MSP_PERMISOS with the in-code catalog: rows
	// in `perms` are inserted or updated. Existing rows whose code is no
	// longer in `perms` are left alone (orphans) — pruning is a separate
	// administrator action mediated by FindOrphans.
	UpsertCatalog(ctx context.Context, perms []domain.PermissionMeta) error

	// FindByCodigo loads a permiso by code. Returns ErrPermisoNotFound on
	// miss.
	FindByCodigo(ctx context.Context, codigo domain.Permission) (*domain.Permiso, error)

	// FindAll returns every permiso currently in the catalog, ordered by
	// codigo for deterministic output.
	FindAll(ctx context.Context) ([]*domain.Permiso, error)

	// FindOrphans returns the permission codes persisted in MSP_PERMISOS
	// that are NOT in the supplied list of known codes. Used by tooling
	// that detects codes removed from the source.
	FindOrphans(ctx context.Context, known []domain.Permission) ([]domain.Permission, error)
}
