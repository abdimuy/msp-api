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
// Convention: only the auth middleware (and tests) call PlantCurrentUser;
// every other caller reads via CurrentUserFromContext.
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
