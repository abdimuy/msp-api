package authhttp

import (
	"net/http"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// RequirePermission returns a middleware that allows the request to pass only
// when the authenticated principal (planted by AuthnMiddleware) holds every
// permission code in perms. The first missing permission short-circuits with
// a 403 response and a stable "permission_denied" code; the required code is
// attached as a problem-details field so clients can render UI hints.
func RequirePermission(perms ...domain.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cu, ok := auth.CurrentUserFromContext(r.Context())
			if !ok {
				response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
				return
			}
			held := make(map[string]struct{}, len(cu.Permisos))
			for _, p := range cu.Permisos {
				held[p] = struct{}{}
			}
			for _, required := range perms {
				if _, has := held[string(required)]; !has {
					response.Error(w, r,
						apperror.NewForbidden("permission_denied", "permiso denegado").
							WithField("required_permission", string(required)),
					)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
