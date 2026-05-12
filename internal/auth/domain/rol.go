package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Column-width caps mirrored from the Firebird schema.
const (
	maxRolNombreLength      = 50
	maxRolDescriptionLength = 255
)

// Rol is a named bundle of permisos assignable to a usuario. The auth
// module ships a small built-in catalog (e.g. "admin", "vendedor") seeded
// with inmutable=true; user-defined roles are inmutable=false and can be
// renamed, redescribed, and deactivated.
//
// Inmutable roles refuse all mutation methods (Update, Desactivar) with
// ErrRolInmutable. This invariant is enforced in the domain, not at the
// repository, so any code path that loads a rol and tries to mutate it is
// safe.
type Rol struct {
	id          uuid.UUID
	nombre      string
	description *string
	inmutable   bool
	activo      bool
	audit       audit.Auditable
}

// NewRol validates and constructs a fresh Rol.
func NewRol(
	id uuid.UUID,
	nombre string,
	description *string,
	inmutable bool,
	createdBy uuid.UUID,
	now time.Time,
) (*Rol, error) {
	cleanNombre, err := validateRolNombre(nombre)
	if err != nil {
		return nil, err
	}
	cleanDescription, err := validateRolDescription(description)
	if err != nil {
		return nil, err
	}
	return &Rol{
		id:          id,
		nombre:      cleanNombre,
		description: cleanDescription,
		inmutable:   inmutable,
		activo:      true,
		audit:       audit.NewAuditable(now, createdBy),
	}, nil
}

// HydrateRolParams carries the persisted shape of a Rol for repository
// reconstruction without validation.
type HydrateRolParams struct {
	ID          uuid.UUID
	Nombre      string
	Description *string
	Inmutable   bool
	Activo      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// HydrateRol rebuilds a Rol from persistence without validation.
func HydrateRol(p HydrateRolParams) *Rol {
	return &Rol{
		id:          p.ID,
		nombre:      p.Nombre,
		description: p.Description,
		inmutable:   p.Inmutable,
		activo:      p.Activo,
		audit:       audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ─── Accessors ─────────────────────────────────────────────────────────────

// ID returns the rol's primary key.
func (r *Rol) ID() uuid.UUID { return r.id }

// Nombre returns the rol's display name.
func (r *Rol) Nombre() string { return r.nombre }

// Description returns the optional rol description.
func (r *Rol) Description() *string { return r.description }

// Inmutable reports whether this rol is system-managed and refuses mutation.
func (r *Rol) Inmutable() bool { return r.inmutable }

// Activo reports whether this rol is currently active.
func (r *Rol) Activo() bool { return r.activo }

// Audit returns a copy of the audit subrecord.
func (r *Rol) Audit() audit.Auditable { return r.audit }

// CreatedAt proxies the audit subrecord's CreatedAt.
func (r *Rol) CreatedAt() time.Time { return r.audit.CreatedAt() }

// UpdatedAt proxies the audit subrecord's UpdatedAt.
func (r *Rol) UpdatedAt() time.Time { return r.audit.UpdatedAt() }

// CreatedBy proxies the audit subrecord's CreatedBy.
func (r *Rol) CreatedBy() uuid.UUID { return r.audit.CreatedBy() }

// UpdatedBy proxies the audit subrecord's UpdatedBy.
func (r *Rol) UpdatedBy() uuid.UUID { return r.audit.UpdatedBy() }

// ─── Mutators ──────────────────────────────────────────────────────────────

// Update mutates the rol's name and description. Refuses with
// ErrRolInmutable if the rol is system-managed.
func (r *Rol) Update(nombre string, description *string, updatedBy uuid.UUID, _ time.Time) error {
	if r.inmutable {
		return ErrRolInmutable
	}
	cleanNombre, err := validateRolNombre(nombre)
	if err != nil {
		return err
	}
	cleanDescription, err := validateRolDescription(description)
	if err != nil {
		return err
	}
	r.nombre = cleanNombre
	r.description = cleanDescription
	r.audit.MarkUpdated(updatedBy)
	return nil
}

// Desactivar marks the rol inactive. Refuses with ErrRolInmutable on
// system-managed roles.
func (r *Rol) Desactivar(updatedBy uuid.UUID, _ time.Time) error {
	if r.inmutable {
		return ErrRolInmutable
	}
	r.activo = false
	r.audit.MarkUpdated(updatedBy)
	return nil
}

// Reactivar marks the rol active. Refuses with ErrRolInmutable on
// system-managed roles (inmutables never need reactivation because they
// cannot be deactivated in the first place).
func (r *Rol) Reactivar(updatedBy uuid.UUID, _ time.Time) error {
	if r.inmutable {
		return ErrRolInmutable
	}
	r.activo = true
	r.audit.MarkUpdated(updatedBy)
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func validateRolNombre(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrRolNombreRequerido
	}
	if len(s) > maxRolNombreLength {
		return "", ErrRolNombreDemasiadoLargo
	}
	return s, nil
}

func validateRolDescription(p *string) (*string, error) {
	if p == nil {
		return nil, nil //nolint:nilnil // optional descriptor: nil is a valid, non-error result
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil, nil //nolint:nilnil // empty trimmed string collapses to "no description"
	}
	if len(s) > maxRolDescriptionLength {
		return nil, ErrRolDescripcionDemasiadoLarga
	}
	return &s, nil
}
