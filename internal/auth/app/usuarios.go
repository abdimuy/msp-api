package app

import (
	"context"
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
