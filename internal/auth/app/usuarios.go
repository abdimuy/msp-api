package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

// Outbox aggregate and event-type constants. Kept here so the strings are
// not free-floating across the package; the linter and grep agree on the
// canonical spelling.
const (
	outboxAggregateUsuario = "usuario"
	outboxAggregateRol     = "rol"

	eventUserCreated        = "user.created"
	eventUserUpdated        = "user.updated"
	eventUserDeactivated    = "user.deactivated"
	eventUserSynced         = "user.synced"
	eventRoleAssigned       = "role.assigned"
	eventRoleRevoked        = "role.revoked"
	eventRoleCreated        = "role.created"
	eventRoleUpdated        = "role.updated"
	eventRoleDeactivated    = "role.deactivated"
	eventRolePermGranted    = "role.permission_granted"
	eventRolePermRevoked    = "role.permission_revoked"
	eventPermCatalogSynced  = "auth.permission_catalog_synced"
	eventRolesCatalogSynced = "auth.roles_catalog_synced"
)

// CrearParams carries the input for registering a usuario from the office.
// FirebaseUID is the uid of the account the caller already created in
// Firebase Auth; binding it at birth is the point of this command — once the
// row carries the uid, the person's first login is resolved by
// FindByFirebaseUID instead of minting a second usuario. Telefono is
// optional: nil or a blank string leaves the column empty.
type CrearParams struct {
	FirebaseUID string
	Email       string
	Nombre      string
	Telefono    *string
}

// Crear registers a usuario from the office. Two rows can be born for the
// same person, so the command has two outcomes:
//
//   - the email is free → a brand-new FIREBASE_USER row is inserted;
//   - the email already belongs to an active VENDEDOR_ONLY row (one that
//     EnsureVendedoresByEmail minted when a cobrador named the person as
//     vendedor of a venta) → that row is promoted IN PLACE: same ID, the
//     alta's firebase_uid is attached, ESTATUS moves to FIREBASE_USER, and
//     the real nombre replaces the placeholder derived from the email's
//     local-part ("humberto.quintana"), which is the defect this closes.
//
// Both outcomes are a success for the caller (201) — for the office the
// result is identical: the person is registered and can log in.
//
// The lookup and the write share one transaction (runInTx, and every repo
// method routes through firebird.GetQuerier so both join it), which makes the
// promotion path's read-check-write one unit: a failed UPDATE leaves the
// vendedor row exactly as it was. It does NOT serialize two concurrent altas
// of the same email — at READ COMMITTED both can legitimately observe "not
// found".
//
// What stops the second row is the UNIQUE index. The lookup by email is an
// ADDITION, not a replacement: the creation path still hands the entity
// straight to Save and lets UQ_MSP_USUARIOS_EMAIL / UQ_MSP_USUARIOS_FIREBASE_UID
// be the authority (mapped to domain.ErrUsuarioYaExiste → 409). That Save is
// the net that catches the race the pre-check cannot.
//
// createdBy is the actor `by`, not the new row's own id — unlike the
// self-registration flow in createFromToken, here there is a real actor, and
// MSP_USUARIOS.CREATED_BY's FK is satisfied by the caller's row. On the
// promotion path CREATED_BY is left untouched (the vendedor row keeps its
// original creator) and only UPDATED_BY moves to the actor.
//
// On success a "user.created" event is enqueued on the outbox (best-effort)
// AFTER the transaction commits, in its own tx — the same placement
// SyncFromFirebase uses for "user.synced". The trade is explicit: a crash
// between COMMIT and Enqueue loses the event rather than recording an alta
// that never happened. Nothing consumes "user.created" (the dispatcher only
// picks up event types with a registered handler, and auth registers only
// "user.deactivated"), so the event is a bitácora line, not a side effect.
//
// The event type is deliberately the SAME on both paths — it means "the
// office registered this person" and downstream consumers care about that,
// not about how the row came to exist. The payload carries a "promoted"
// boolean so a consumer that does care can tell them apart without a second
// event type (which would have required amending the auth event catalog in
// docs/adr/0001-outbox-strategy.md).
func (s *Service) Crear(ctx context.Context, p CrearParams, by uuid.UUID) (*domain.Usuario, error) {
	fuid, err := domain.NewFirebaseUID(p.FirebaseUID)
	if err != nil {
		return nil, err
	}
	email, err := domain.NewEmail(p.Email)
	if err != nil {
		return nil, err
	}
	nombre, err := domain.NewNombre(p.Nombre)
	if err != nil {
		return nil, err
	}
	tel, err := optionalTelefono(p.Telefono)
	if err != nil {
		return nil, err
	}

	var (
		u        *domain.Usuario
		promoted bool
	)
	err = s.runInTx(ctx, func(ctx context.Context) error {
		existing, lookupErr := s.usuarios.FindByEmail(ctx, email.Value())
		switch {
		case lookupErr == nil:
			prom, promErr := s.promoteVendedorForAlta(ctx, existing, fuid, nombre, tel, by)
			if promErr != nil {
				return promErr
			}
			u, promoted = prom, true
			return nil
		case !errors.Is(lookupErr, domain.ErrUsuarioNotFound):
			return lookupErr
		}

		fresh := domain.NewUsuario(uuid.New(), fuid, email, nombre, tel, nil, by, s.clock.Now())
		if saveErr := s.usuarios.Save(ctx, fresh); saveErr != nil {
			return saveErr
		}
		u = fresh
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, u.ID(), eventUserCreated, map[string]any{
		"usuario_id":   u.ID(),
		"email":        u.Email().Value(),
		"firebase_uid": u.FirebaseUID().Value(),
		"created_by":   by,
		"promoted":     promoted,
	})
	return u, nil
}

// promoteVendedorForAlta turns the VENDEDOR_ONLY row `existing` into a
// FIREBASE_USER carrying the alta's identity, in place. Called only from
// Crear, always inside its transaction, with `existing` already resolved by
// the alta's email.
//
// Everything that is NOT a promotable row is a 409 for the office, and every
// one of them carries the SAME code — domain.ErrUsuarioYaExiste
// ("usuario_ya_existe"). The office desktop renders one message for all three
// causes of a collision, so splitting the code buys nothing and risks showing
// the operator the wrong copy:
//
//   - a FIREBASE_USER row (with any uid, equal or not): two Firebase accounts
//     claiming one email is an administrator's decision, not something this
//     endpoint resolves silently;
//   - an inactive row, VENDEDOR_ONLY or not: somebody deactivated it on
//     purpose, and this flow mirrors the rule EnsureVendedoresByEmail already
//     enforces — do NOT silently reactivate. It deliberately does NOT reuse
//     that flow's sentinels: domain.ErrUsuarioInactivo is a 403 whose meaning
//     is already taken (the authn middleware raises it for the CALLER's own
//     account, and the desktop translates it as "your account is disabled"),
//     so returning it here would tell the operator her own session died.
//     Note this branch only ever sees rows deactivated OUTSIDE this API:
//     Service.Desactivar renames EMAIL and FIREBASE_UID to "deleted-<id>-…"
//     precisely to free the UNIQUE slots, so after a baja through the API
//     FindByEmail misses and the alta creates a new row (verified against
//     Firebird — see the app test named for it);
//   - a uid already bound to a DIFFERENT row: without this check the UPDATE
//     below would hit UQ_MSP_USUARIOS_FIREBASE_UID. The repository maps that
//     to ErrUsuarioYaExiste too, so the check is belt-and-braces rather than
//     the only guard, but it keeps the failure legible and cheap.
//
// This is deliberately NOT promoteVendedorOnly (usuarios_sync.go): that one
// serves the login path, has no actor (it attributes the write to the row
// itself) and takes the nombre from Firestore. Here the actor is real and the
// alta's nombre is authoritative. Bending it to serve both would have made
// the production login path pay for this one.
func (s *Service) promoteVendedorForAlta(
	ctx context.Context,
	existing *domain.Usuario,
	fuid domain.FirebaseUID,
	nombre domain.Nombre,
	tel *platform.Telefono,
	by uuid.UUID,
) (*domain.Usuario, error) {
	if existing.Estatus() != domain.EstatusVendedorOnly || !existing.Activo() {
		return nil, domain.ErrUsuarioYaExiste
	}
	owner, uidErr := s.usuarios.FindByFirebaseUID(ctx, fuid.Value())
	switch {
	case uidErr == nil:
		if owner.ID() != existing.ID() {
			return nil, domain.ErrUsuarioYaExiste
		}
	case !errors.Is(uidErr, domain.ErrUsuarioNotFound):
		return nil, uidErr
	}

	// The alta's nombre always wins: replacing the email-derived placeholder
	// is the point of promoting. The telefono only moves when the alta
	// carries one — an omitted phone must not wipe the stored value.
	telefono := existing.Telefono()
	if tel != nil {
		telefono = tel
	}
	now := s.clock.Now()
	existing.Update(domain.UsuarioUpdate{
		Email:     existing.Email(),
		Nombre:    nombre,
		Telefono:  telefono,
		AlmacenID: existing.AlmacenID(),
	}, by, now)
	existing.PromoteToFirebaseUser(fuid, by, now)

	if updErr := s.usuarios.Update(ctx, existing); updErr != nil {
		return nil, updErr
	}
	return existing, nil
}

// ActualizarParams carries the input for editing a usuario's mutable fields.
// Email and Nombre are raw strings; they are validated through the domain
// constructors. Telefono is optional — a nil pointer clears the phone, a
// pointer to an empty string is rejected. AlmacenID is optional.
type ActualizarParams struct {
	ID        uuid.UUID
	Email     string
	Nombre    string
	Telefono  *string
	AlmacenID *int
}

// Actualizar applies a mutation to the given usuario. The usuario is loaded,
// the new VOs are constructed (validating Email/Nombre/Telefono), and the
// repository is asked to persist the change. On success a "user.updated"
// event is enqueued on the outbox.
func (s *Service) Actualizar(ctx context.Context, p ActualizarParams, by uuid.UUID) (*domain.Usuario, error) {
	u, err := s.usuarios.FindByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}

	email, err := domain.NewEmail(p.Email)
	if err != nil {
		return nil, err
	}
	nombre, err := domain.NewNombre(p.Nombre)
	if err != nil {
		return nil, err
	}
	tel, err := optionalTelefono(p.Telefono)
	if err != nil {
		return nil, err
	}

	u.Update(domain.UsuarioUpdate{
		Email:     email,
		Nombre:    nombre,
		Telefono:  tel,
		AlmacenID: p.AlmacenID,
	}, by, s.clock.Now())

	if err := s.usuarios.Update(ctx, u); err != nil {
		return nil, err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, u.ID(), eventUserUpdated, map[string]any{
		"usuario_id": u.ID(),
		"email":      u.Email().Value(),
		"updated_by": by,
	})
	return u, nil
}

// Desactivar soft-deletes a usuario. The email and firebase_uid columns are
// mangled so the unique slots are freed for re-creation; the usuario row
// itself is kept around for audit purposes. The "user.deactivated" event is
// enqueued on success with the ORIGINAL firebase_uid attached so downstream
// consumers (e.g. the Firebase Auth disable handler) can locate the
// external account — by the time the event is processed, the row already
// carries the renamed "deleted-<id>" placeholder.
func (s *Service) Desactivar(ctx context.Context, id, by uuid.UUID) error {
	u, err := s.usuarios.FindByID(ctx, id)
	if err != nil {
		return err
	}

	originalFirebaseUID := u.FirebaseUID().Value()

	now := s.clock.Now()
	newEmail := "deleted-" + id.String() + "-" + u.Email().Value()
	newFUID := "deleted-" + id.String()
	u.RenameForSoftDelete(newEmail, newFUID, by, now)
	u.Desactivar(by, now)

	if err := s.usuarios.Update(ctx, u); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, id, eventUserDeactivated, map[string]any{
		"usuario_id":     id,
		"deactivated_by": by,
		"firebase_uid":   originalFirebaseUID,
	})
	return nil
}

// Obtener loads a single usuario by ID. Returns ErrUsuarioNotFound on miss.
func (s *Service) Obtener(ctx context.Context, id uuid.UUID) (*domain.Usuario, error) {
	return s.usuarios.FindByID(ctx, id)
}

// Listar returns a page of usuarios using cursor pagination.
func (s *Service) Listar(ctx context.Context, p outbound.ListParams) (outbound.Page[*domain.Usuario], error) {
	return s.usuarios.List(ctx, p)
}

// RolesDeUsuario returns every rol assigned to the usuario. The usuario is
// verified to exist first so callers get a proper ErrUsuarioNotFound instead
// of an empty list on a bad ID.
func (s *Service) RolesDeUsuario(ctx context.Context, usuarioID uuid.UUID) ([]*domain.Rol, error) {
	if _, err := s.usuarios.FindByID(ctx, usuarioID); err != nil {
		return nil, err
	}
	return s.usuarios.RolesFor(ctx, usuarioID)
}

// PermisosEfectivosDeUsuario returns the effective union of permisos granted
// to the usuario via every active rol it owns, enriched with catalog
// metadata (description, categoria). The usuario is verified to exist first.
func (s *Service) PermisosEfectivosDeUsuario(ctx context.Context, usuarioID uuid.UUID) ([]*domain.Permiso, error) {
	if _, err := s.usuarios.FindByID(ctx, usuarioID); err != nil {
		return nil, err
	}
	codes, err := s.usuarios.PermisosFor(ctx, usuarioID)
	if err != nil {
		return nil, err
	}
	return s.enrichPermisos(ctx, codes)
}

// AsignarRolAUsuario attaches a rol to a usuario. The usuario and rol are
// both verified to exist first. Idempotent at the repository level. Emits a
// "role.assigned" event on success.
func (s *Service) AsignarRolAUsuario(ctx context.Context, usuarioID, rolID, by uuid.UUID) error {
	if _, err := s.usuarios.FindByID(ctx, usuarioID); err != nil {
		return err
	}
	if _, err := s.roles.FindByID(ctx, rolID); err != nil {
		return err
	}

	now := s.clock.Now()
	if err := s.usuarios.AsignarRol(ctx, usuarioID, rolID, by, now); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, usuarioID, eventRoleAssigned, map[string]any{
		"usuario_id": usuarioID,
		"rol_id":     rolID,
		"granted_by": by,
	})
	return nil
}

// RevocarRolDeUsuario detaches a rol from a usuario. Idempotent at the
// repository. Emits "role.revoked" on success.
func (s *Service) RevocarRolDeUsuario(ctx context.Context, usuarioID, rolID uuid.UUID) error {
	if _, err := s.usuarios.FindByID(ctx, usuarioID); err != nil {
		return err
	}
	if err := s.usuarios.RevocarRol(ctx, usuarioID, rolID); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, usuarioID, eventRoleRevoked, map[string]any{
		"usuario_id": usuarioID,
		"rol_id":     rolID,
	})
	return nil
}

// optionalTelefono converts a raw optional string into a *platform.Telefono.
// nil input → nil output (clearing the phone); a non-nil pointer is parsed
// through platform.NewTelefono. A pointer to a blank/whitespace-only string
// is treated as "clear" rather than "invalid".
func optionalTelefono(s *string) (*platform.Telefono, error) {
	if s == nil {
		return nil, nil //nolint:nilnil // optional value: nil signals "clear field"
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil, nil //nolint:nilnil // optional value: empty input clears the phone
	}
	t, err := platform.NewTelefono(trimmed)
	if err != nil {
		return nil, apperror.NewValidation("telefono_invalid", "el teléfono no es válido").WithError(err)
	}
	return &t, nil
}

// enqueueEvent best-effort enqueues an outbox event. Failures are logged
// with the payload but never block the business write — consistent with the
// platform/outbox contract.
func (s *Service) enqueueEvent(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) {
	if s.outbox == nil {
		return
	}
	if err := s.outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload); err != nil {
		slog.WarnContext(
			ctx, "auth.outbox_enqueue_failed",
			"aggregate", aggregate,
			"aggregate_id", aggregateID,
			"event_type", eventType,
			"error", err,
		)
	}
}
