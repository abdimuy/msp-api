package auth

import "context"

// currentUserCtxKey is the unexported type used as the context key for
// CurrentUser. Per the standard-library convention (and the revive
// `context-keys-type` rule) the key must be a distinct named type, not a
// string, to avoid collisions with other packages.
type currentUserCtxKey struct{}

// PlantCurrentUser returns a derived context carrying u as the
// authenticated principal for downstream handlers and services.
//
// ─── TRUST BOUNDARY ─────────────────────────────────────────────────────────
//
// PlantCurrentUser is the *only* way a CurrentUser appears on a request
// context. Downstream middleware and handlers treat a planted user as
// authoritative — most notably AuthnMiddleware short-circuits Firebase
// verification when one is already present, so an unauthorised plant is
// equivalent to forging authentication.
//
// Approved callers (production):
//
//   - internal/auth/infra/authhttp.AuthnMiddleware — plants the user
//     after Firebase ID-token verification + usuario lookup. The primary
//     entry point for every authenticated request.
//   - internal/platform/failedintent/http.Service.buildReplayRequest —
//     re-plants the original requester's user on a synthesized replay
//     request, allowing the dispatched handler chain to authorize the
//     replay as if the user had retried themselves.
//
// Adding a new caller is a security-sensitive change. The whitelist is
// enforced by TestPlantCurrentUser_TrustedCallers (ctxuser_callers_test.go);
// new callers must be added there with justification.
//
// Tests may call PlantCurrentUser freely — the whitelist only constrains
// non-_test.go code.
func PlantCurrentUser(ctx context.Context, u CurrentUser) context.Context {
	return context.WithValue(ctx, currentUserCtxKey{}, u)
}

// CurrentUserFromContext extracts the CurrentUser previously planted by
// PlantCurrentUser. The boolean second return is false when no user is
// present — typically because the request is anonymous or the middleware
// did not run.
func CurrentUserFromContext(ctx context.Context) (CurrentUser, bool) {
	v, ok := ctx.Value(currentUserCtxKey{}).(CurrentUser)
	return v, ok
}
