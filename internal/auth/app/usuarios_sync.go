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
