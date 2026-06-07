package failedintenthttp

import (
	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
)

// MountRouter registers the admin endpoints on r. The caller is responsible
// for applying authn middleware to r before MountRouter is called; this
// function adds per-route RequirePermission guards.
//
// Routes:
//
//	GET    /                  → failed_intents:ver      → svc.Listar
//	GET    /{id}              → failed_intents:ver      → svc.Obtener
//	GET    /{id}/blob-parts   → failed_intents:ver      → svc.BlobParts
//	PATCH  /{id}/resolve      → failed_intents:resolver → svc.Resolver
//	POST   /{id}/replay       → failed_intents:resolver → svc.Replay
//	POST   /{id}/replay-with  → failed_intents:resolver → svc.ReplayWith
func MountRouter(r chi.Router, svc *Service) {
	r.With(authhttp.RequirePermission(auth.PermFailedIntentsVer)).
		Get("/", svc.Listar)

	r.With(authhttp.RequirePermission(auth.PermFailedIntentsVer)).
		Get("/{id}", svc.Obtener)

	r.With(authhttp.RequirePermission(auth.PermFailedIntentsVer)).
		Get("/{id}/blob-parts", svc.BlobParts)

	r.With(authhttp.RequirePermission(auth.PermFailedIntentsResolver)).
		Patch("/{id}/resolve", svc.Resolver)

	r.With(authhttp.RequirePermission(auth.PermFailedIntentsResolver)).
		Post("/{id}/replay", svc.Replay)

	r.With(authhttp.RequirePermission(auth.PermFailedIntentsResolver)).
		Post("/{id}/replay-with", svc.ReplayWith)
}

// MountMeRouter mounts the end-user self-service endpoints. The caller must
// apply the authn middleware to r before mounting; no failed_intents
// permission is required — the handler scopes results to the calling user.
//
// Routes:
//
//	GET /  → svc.MeListar
func MountMeRouter(r chi.Router, svc *Service) {
	r.Get("/", svc.MeListar)
}
