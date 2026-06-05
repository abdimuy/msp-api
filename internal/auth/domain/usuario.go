package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

// Estatus is a discriminator that distinguishes how a Usuario record was
// created and which identity is authoritative for it.
type Estatus string

const (
	// EstatusFirebaseUser marks a Usuario that was created through the normal
	// Firebase Authentication flow. It always has a non-empty FIREBASE_UID.
	EstatusFirebaseUser Estatus = "FIREBASE_USER"
	// EstatusVendedorOnly marks a Usuario that exists solely for sale
	// attribution in the Microsip catalog. It has no Firebase identity
	// (FIREBASE_UID is the zero-value) and cannot authenticate until promoted.
	EstatusVendedorOnly Estatus = "VENDEDOR_ONLY"
)

// Usuario is the auth module's principal entity: an authenticated person
// who may operate the API. Uniqueness is enforced on both email and
// firebase_uid at the repository layer.
//
// The Usuario is a Type-A entity: it lives only inside msp-api and has no
// Microsip counterpart, so it embeds audit.Auditable (no MicrosipSync).
type Usuario struct {
	id          uuid.UUID
	firebaseUID FirebaseUID
	email       Email
	nombre      Nombre
	telefono    *platform.Telefono
	almacenID   *int
	activo      bool
	estatus     Estatus
	audit       audit.Auditable
}

// NewUsuario builds a fresh Usuario. The caller passes `now` (typically
// from a Clock port) so the constructor itself stays deterministic; the
// audit subrecord's timestamps are seeded with that same instant.
func NewUsuario(
	id uuid.UUID,
	firebaseUID FirebaseUID,
	email Email,
	nombre Nombre,
	telefono *platform.Telefono,
	almacenID *int,
	createdBy uuid.UUID,
	now time.Time,
) *Usuario {
	return &Usuario{
		id:          id,
		firebaseUID: firebaseUID,
		email:       email,
		nombre:      nombre,
		telefono:    telefono,
		almacenID:   almacenID,
		activo:      true,
		estatus:     EstatusFirebaseUser,
		audit:       audit.NewAuditable(now, createdBy),
	}
}

// NewVendedorUsuario builds a vendedor-only Usuario — one that exists only
// for sale attribution. It has no Firebase identity (FIREBASE_UID is the
// zero-value) and ESTATUS = VENDEDOR_ONLY. If the vendedor later authenticates
// via Firebase, the row is promoted in place: same ID, the FIREBASE_UID and
// ESTATUS fields move forward. See SyncFromFirebase in the app layer.
func NewVendedorUsuario(
	id uuid.UUID,
	email Email,
	nombre Nombre,
	createdBy uuid.UUID,
	now time.Time,
) *Usuario {
	return &Usuario{
		id:          id,
		firebaseUID: FirebaseUID{}, // zero-value: vendedor has no Firebase identity yet
		email:       email,
		nombre:      nombre,
		telefono:    nil,
		almacenID:   nil,
		activo:      true,
		estatus:     EstatusVendedorOnly,
		audit:       audit.NewAuditable(now, createdBy),
	}
}

// HydrateUsuarioParams carries the persisted shape of a Usuario for
// repository reconstruction. Fields are read directly without re-running
// constructor validation; repos must guarantee the values were validated on
// write.
type HydrateUsuarioParams struct {
	ID          uuid.UUID
	FirebaseUID FirebaseUID
	Email       Email
	Nombre      Nombre
	Telefono    *platform.Telefono
	AlmacenID   *int
	Activo      bool
	Estatus     Estatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// HydrateUsuario rebuilds a Usuario from persistence without validation.
func HydrateUsuario(p HydrateUsuarioParams) *Usuario {
	return &Usuario{
		id:          p.ID,
		firebaseUID: p.FirebaseUID,
		email:       p.Email,
		nombre:      p.Nombre,
		telefono:    p.Telefono,
		almacenID:   p.AlmacenID,
		activo:      p.Activo,
		estatus:     p.Estatus,
		audit:       audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ─── Accessors ─────────────────────────────────────────────────────────────

// ID returns the usuario's primary key.
func (u *Usuario) ID() uuid.UUID { return u.id }

// FirebaseUID returns the linked Firebase identity provider uid.
func (u *Usuario) FirebaseUID() FirebaseUID { return u.firebaseUID }

// Email returns the usuario's email.
func (u *Usuario) Email() Email { return u.email }

// Nombre returns the usuario's display name.
func (u *Usuario) Nombre() Nombre { return u.nombre }

// Telefono returns the usuario's phone number, or nil if not set.
func (u *Usuario) Telefono() *platform.Telefono { return u.telefono }

// AlmacenID returns the warehouse the usuario is bound to, or nil if not
// scoped to a single warehouse.
func (u *Usuario) AlmacenID() *int { return u.almacenID }

// Activo reports whether the usuario is currently active.
func (u *Usuario) Activo() bool { return u.activo }

// Estatus returns the usuario's discriminator: FIREBASE_USER for normal
// Firebase-authenticated users; VENDEDOR_ONLY for catalog-only vendedores.
func (u *Usuario) Estatus() Estatus { return u.estatus }

// Audit returns a copy of the audit subrecord.
func (u *Usuario) Audit() audit.Auditable { return u.audit }

// CreatedAt proxies the audit subrecord's CreatedAt.
func (u *Usuario) CreatedAt() time.Time { return u.audit.CreatedAt() }

// UpdatedAt proxies the audit subrecord's UpdatedAt.
func (u *Usuario) UpdatedAt() time.Time { return u.audit.UpdatedAt() }

// CreatedBy proxies the audit subrecord's CreatedBy.
func (u *Usuario) CreatedBy() uuid.UUID { return u.audit.CreatedBy() }

// UpdatedBy proxies the audit subrecord's UpdatedBy.
func (u *Usuario) UpdatedBy() uuid.UUID { return u.audit.UpdatedBy() }

// ─── Mutators ──────────────────────────────────────────────────────────────

// UsuarioUpdate carries the mutable fields a caller may change in a single
// Update call. nil-valued pointers mean "leave the field as-is" only for
// fields whose persisted column is itself nullable (telefono, almacenID);
// the required VOs (email, nombre) are always replaced.
type UsuarioUpdate struct {
	Email     Email
	Nombre    Nombre
	Telefono  *platform.Telefono
	AlmacenID *int
}

// Update applies the supplied mutation and bumps the audit subrecord with
// the given user as updatedBy. The `now` parameter is accepted for symmetry
// with the rest of the domain API and to keep the signature stable should
// the audit module gain explicit-clock support in the future.
func (u *Usuario) Update(upd UsuarioUpdate, updatedBy uuid.UUID, _ time.Time) {
	u.email = upd.Email
	u.nombre = upd.Nombre
	u.telefono = upd.Telefono
	u.almacenID = upd.AlmacenID
	u.audit.MarkUpdated(updatedBy)
}

// Desactivar deactivates the usuario. Idempotent: a no-op when the usuario
// is already inactive (the audit subrecord is still bumped so callers can
// rely on UpdatedAt as the "last touched" indicator).
func (u *Usuario) Desactivar(updatedBy uuid.UUID, _ time.Time) {
	u.activo = false
	u.audit.MarkUpdated(updatedBy)
}

// Reactivar marks the usuario as active again. Symmetrical with Desactivar.
func (u *Usuario) Reactivar(updatedBy uuid.UUID, _ time.Time) {
	u.activo = true
	u.audit.MarkUpdated(updatedBy)
}

// RenameForSoftDelete is intended for the repository layer: when a usuario
// is deactivated the unique email and firebase_uid columns are mangled to
// free them for reuse by a future usuario with the same identity. The
// renamed values are not validated — they go through Hydrate to bypass the
// VO constructors which would reject the suffixed forms.
func (u *Usuario) RenameForSoftDelete(newEmail, newFirebaseUID string, updatedBy uuid.UUID, _ time.Time) {
	u.email = HydrateEmail(newEmail)
	u.firebaseUID = HydrateFirebaseUID(newFirebaseUID)
	u.audit.MarkUpdated(updatedBy)
}
