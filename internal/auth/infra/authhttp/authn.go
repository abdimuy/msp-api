package authhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// AuthnMiddleware extracts and verifies the bearer token, looks up the matching
// usuario, loads their effective permisos, and plants an auth.CurrentUser on
// the request context for downstream handlers.
//
// Anonymous endpoints (POST /auth/login) bypass this middleware by virtue of
// being registered outside the protected subgroup in MountRouter.
type AuthnMiddleware struct {
	firebase    outbound.FirebaseClient
	usuarios    outbound.UsuarioRepo
	provisioner usuarioProvisioner
}

// usuarioProvisioner creates-or-updates a usuario from a Firebase ID token.
// *app.Service satisfies it via SyncFromFirebase. Declared as a local
// interface so the middleware depends on the behavior, not the concrete
// service, and so it can be faked in tests.
type usuarioProvisioner interface {
	SyncFromFirebase(ctx context.Context, idToken string) (*domain.Usuario, error)
}

// NewAuthnMiddleware constructs the middleware with its dependencies. The
// provisioner lazily enrolls valid Firebase users that have no MSP_USUARIOS
// row yet; pass nil to disable that behavior (the middleware then returns
// the original usuario_not_found error).
func NewAuthnMiddleware(fb outbound.FirebaseClient, usuarios outbound.UsuarioRepo, provisioner usuarioProvisioner) *AuthnMiddleware {
	return &AuthnMiddleware{firebase: fb, usuarios: usuarios, provisioner: provisioner}
}

// Handler is the chi-compatible middleware function.
func (m *AuthnMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In-process dispatches (e.g. the failedintent replay dispatcher)
		// plant CurrentUser directly on the request context, bypassing the
		// Firebase verification path. auth.PlantCurrentUser is only callable
		// from trusted server code, so a pre-planted user is authoritative.
		if _, ok := auth.CurrentUserFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		token, err := extractBearer(r)
		if err != nil {
			response.Error(w, r, err)
			return
		}

		ft, err := m.firebase.VerifyIDToken(r.Context(), token)
		if err != nil {
			response.Error(w, r, err)
			return
		}

		u, err := m.usuarios.FindByFirebaseUID(r.Context(), ft.UID)
		if err != nil && errors.Is(err, domain.ErrUsuarioNotFound) && m.provisioner != nil {
			// First authenticated request from a valid Firebase user that has no
			// MSP_USUARIOS row yet: enroll it lazily via the same sync the login
			// endpoint uses, then continue. Any client (appdesk, app, future) is
			// auto-provisioned without having to call POST /auth/login first.
			u, err = m.provisioner.SyncFromFirebase(r.Context(), token)
		}
		if err != nil {
			if _, ok := apperror.As(err); ok {
				response.Error(w, r, err)
				return
			}
			response.Error(w, r, apperror.NewUnauthorized("user_not_found", "usuario no encontrado").WithError(err))
			return
		}
		if !u.Activo() {
			response.Error(w, r, apperror.NewForbidden("user_inactive", "el usuario está inactivo"))
			return
		}

		perms, err := m.usuarios.PermisosFor(r.Context(), u.ID())
		if err != nil {
			response.Error(w, r, err)
			return
		}

		cu := auth.ToContract(u, perms)
		ctx := auth.PlantCurrentUser(r.Context(), cu)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerPrefix is the case-sensitive scheme expected in the Authorization
// header for HTTP bearer tokens.
const bearerPrefix = "Bearer "

// extractBearer returns the token portion of an "Authorization: Bearer ..."
// header, or a typed apperror with stable codes the API contract pins.
func extractBearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", apperror.NewUnauthorized("missing_authorization", "encabezado authorization ausente")
	}
	if !strings.HasPrefix(h, bearerPrefix) {
		return "", apperror.NewUnauthorized("invalid_authorization", "esquema de autorización inválido")
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, bearerPrefix))
	if token == "" {
		return "", apperror.NewUnauthorized("missing_authorization", "token vacío")
	}
	return token, nil
}
