package authhttp

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// MountRouter wires the auth module's HTTP routes into the given chi router.
// The router is expected to be the /v2 sub-router so all paths are relative.
//
// The function partitions endpoints into two groups:
//
//   - Anonymous: POST /auth/login — does not require an Authorization header.
//   - Authenticated: every other endpoint is wrapped by AuthnMiddleware and
//     additionally guarded by RequirePermission for the specific permission
//     codes the route demands.
//
// Mutating endpoints (POST, PATCH) additionally pass through the
// idempotency middleware so callers can safely retry with the same
// Idempotency-Key header without producing duplicate side effects. The header
// is opt-in for v1 (RequireKey: false) to keep ergonomics for existing clients.
func MountRouter(
	r chi.Router,
	svc *app.Service,
	fb outbound.FirebaseClient,
	usuarios outbound.UsuarioRepo,
	idemStore idempotency.Store,
) {
	h := NewHandlers(svc, usuarios)
	authn := NewAuthnMiddleware(fb, usuarios, svc)
	idem := idempotency.Middleware(idempotency.Config{
		Store:      idemStore,
		TTL:        24 * time.Hour,
		Methods:    []string{http.MethodPost, http.MethodPatch},
		RequireKey: false,
	})

	// Anonymous endpoints. /auth/login is a POST so it benefits from
	// idempotency too — repeated submits with the same key replay the response
	// instead of issuing a second Firebase token.
	r.With(idem).Post("/auth/login", h.Login)

	// Authenticated subgroup.
	r.Group(func(r chi.Router) {
		r.Use(authn.Handler)

		r.Get("/me", h.Me)

		r.Route("/usuarios", func(r chi.Router) {
			r.With(RequirePermission(domain.PermUsuariosListar)).Get("/", h.ListarUsuarios)
			r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}", h.ObtenerUsuario)
			r.With(idem, RequirePermission(domain.PermUsuariosActualizar)).Patch("/{id}", h.ActualizarUsuario)
			r.With(RequirePermission(domain.PermUsuariosDesactivar)).Delete("/{id}", h.DesactivarUsuario)
			r.With(idem, RequirePermission(domain.PermUsuariosAsignarRol)).Post("/{id}/roles", h.AsignarRolAUsuario)
			r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Delete("/{id}/roles/{rol_id}", h.RevocarRolDeUsuario)
			// No RequirePermission: cobrador calls this on every venta to resolve
			// vendedor emails; any authenticated user may call it. No idem: the
			// service is idempotent by construction — same list, same result.
			r.Post("/ensure-vendedores-by-email", h.EnsureVendedoresByEmail)
		})

		r.Route("/roles", func(r chi.Router) {
			r.With(RequirePermission(domain.PermRolesListar)).Get("/", h.ListarRoles)
			r.With(RequirePermission(domain.PermRolesListar)).Get("/{id}", h.ObtenerRol)
			r.With(idem, RequirePermission(domain.PermRolesCrear)).Post("/", h.CrearRol)
			r.With(idem, RequirePermission(domain.PermRolesActualizar)).Patch("/{id}", h.ActualizarRol)
			r.With(RequirePermission(domain.PermRolesActualizar)).Delete("/{id}", h.DesactivarRol)
			r.With(idem, RequirePermission(domain.PermRolesAsignarPermiso)).Post("/{id}/permisos", h.AsignarPermisoARol)
			r.With(RequirePermission(domain.PermRolesAsignarPermiso)).Delete("/{id}/permisos/{codigo}", h.RevocarPermisoDeRol)
		})

		r.With(RequirePermission(domain.PermPermisosListar)).Get("/permisos", h.ListarPermisos)
	})
}
