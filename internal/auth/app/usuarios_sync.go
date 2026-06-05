package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// errFirebaseTokenInvalid is the apperror returned for any token verification
// failure surfaced by the FirebaseClient. It wraps the underlying error so
// callers and logs can recover the root cause via errors.Unwrap.
var errFirebaseTokenInvalid = apperror.NewUnauthorized(
	"firebase_token_invalid",
	"token de firebase inválido",
)

// SyncFromFirebase is the login path: verify the Firebase ID token, look up
// the usuario by FirebaseUID, and create-or-update it. The first-time login
// creates a new usuario row with the data from the token and seeds CREATED_BY
// to the usuario's own ID (self-created). Returns the live *domain.Usuario.
//
// All read+write work happens inside a single Firebird transaction so a
// concurrent first-login does not produce two rows for the same Firebase uid.
// After the transaction commits a "user.synced" outbox event is enqueued with
// a "first_login" flag in the payload.
func (s *Service) SyncFromFirebase(ctx context.Context, idToken string) (*domain.Usuario, error) {
	token, err := s.firebase.VerifyIDToken(ctx, idToken)
	if err != nil {
		// If the firebase client already returned a typed apperror, propagate
		// it; otherwise wrap with the canonical Unauthorized sentinel.
		if _, ok := apperror.As(err); ok {
			return nil, err
		}
		return nil, errFirebaseTokenInvalid.WithError(err)
	}

	var (
		out        *domain.Usuario
		firstLogin bool
	)

	err = s.runInTx(ctx, func(ctx context.Context) error {
		u, lookupErr := s.usuarios.FindByFirebaseUID(ctx, token.UID)
		if lookupErr == nil {
			if !u.Activo() {
				return domain.ErrUsuarioInactivo
			}
			out = u
			return nil
		}
		if !errors.Is(lookupErr, domain.ErrUsuarioNotFound) {
			return lookupErr
		}

		fuid, vErr := domain.NewFirebaseUID(token.UID)
		if vErr != nil {
			return vErr
		}
		email, vErr := domain.NewEmail(token.Email)
		if vErr != nil {
			return vErr
		}
		nombre, vErr := domain.NewNombre(deriveNombreFromToken(token.Name, token.Email))
		if vErr != nil {
			return vErr
		}

		id := uuid.New()
		now := s.clock.Now()
		fresh := domain.NewUsuario(id, fuid, email, nombre, nil, nil, id, now)
		if saveErr := s.usuarios.Save(ctx, fresh); saveErr != nil {
			return saveErr
		}
		out = fresh
		firstLogin = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.enqueueEvent(ctx, outboxAggregateUsuario, out.ID(), eventUserSynced, map[string]any{
		"usuario_id":   out.ID(),
		"firebase_uid": out.FirebaseUID().Value(),
		"first_login":  firstLogin,
	})
	return out, nil
}

// VendedorEnsureResult pairs an input email with the resolved usuario_id —
// either pre-existing (active) or freshly created.
type VendedorEnsureResult struct {
	Email     string
	UsuarioID uuid.UUID
}

// EnsureVendedoresByEmail resolves a list of vendedor emails to MSP_USUARIOS
// IDs, creating missing rows on demand as VENDEDOR_ONLY users. The flow per
// email is:
//
//  1. Validate the email syntactically (domain.NewEmail).
//  2. Look up by email.
//  3. If found and active (any ESTATUS): reuse — return its ID.
//     A cobrador may also be a vendedor of his own ventas; this is idempotent.
//  4. If found and inactive: return domain.ErrVendedorEmailInactivo so the
//     office can re-activate it explicitly. Do NOT silently reactivate.
//  5. If not found: build a NewVendedorUsuario(uuid.New(), email, nombre,
//     createdBy, now). The nombre is derived from the email's local-part via
//     the existing deriveNombreFromToken("", email) helper. Save it.
//
// The method is idempotent BY CONSTRUCTION: each email is processed
// independently. If processing email[i] fails, prior successful saves stay
// committed. A re-invocation with the same list resumes from where it stopped.
// No outer transaction is needed.
//
// The first validation/conflict error in the list aborts and is returned to
// the caller (so the Android app can show a meaningful message).
func (s *Service) EnsureVendedoresByEmail(
	ctx context.Context,
	emails []string,
	createdBy uuid.UUID,
) ([]VendedorEnsureResult, error) {
	out := make([]VendedorEnsureResult, 0, len(emails))
	for _, raw := range emails {
		email, err := domain.NewEmail(raw)
		if err != nil {
			return nil, err
		}

		existing, lookupErr := s.usuarios.FindByEmail(ctx, email.Value())
		if lookupErr == nil {
			if !existing.Activo() {
				return nil, domain.ErrVendedorEmailInactivo
			}
			out = append(out, VendedorEnsureResult{
				Email:     email.Value(),
				UsuarioID: existing.ID(),
			})
			continue
		}
		if !errors.Is(lookupErr, domain.ErrUsuarioNotFound) {
			return nil, lookupErr
		}

		nombre, vErr := domain.NewNombre(deriveNombreFromToken("", email.Value()))
		if vErr != nil {
			return nil, vErr
		}

		fresh := domain.NewVendedorUsuario(
			uuid.New(), email, nombre, createdBy, s.clock.Now(),
		)
		if saveErr := s.usuarios.Save(ctx, fresh); saveErr != nil {
			return nil, saveErr
		}
		out = append(out, VendedorEnsureResult{
			Email:     email.Value(),
			UsuarioID: fresh.ID(),
		})
	}
	return out, nil
}

// deriveNombreFromToken returns the token's display name when present,
// otherwise the local-part of the email (everything before the '@'). The
// resulting candidate is validated by domain.NewNombre at the caller.
func deriveNombreFromToken(name, email string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return email
	}
	return email[:at]
}
