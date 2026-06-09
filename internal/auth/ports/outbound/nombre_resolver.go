package outbound

import "context"

// NombreResolver resolves a usuario's canonical display name from the source
// of truth that actually holds it: the Firestore `users` collection, keyed by
// Firebase uid (users/{uid}.NOMBRE). The desktop/legacy app writes the real
// name there; the Firebase ID token's `name` claim is frequently empty or set
// to the email, which is why the auth sync must NOT rely on the token alone.
//
// The auth sync prefers this resolver's result over the token name; on an
// empty result or an error it falls back to the token name and finally the
// email local-part. Resolution is therefore best-effort by contract — a
// Firestore hiccup must never block a login.
type NombreResolver interface {
	// ResolveNombre returns the NOMBRE for the given Firebase uid, or "" when
	// the profile document or the NOMBRE field is absent. Implementations
	// that have no Firestore to read (dev mode, unconfigured) return "".
	ResolveNombre(ctx context.Context, uid string) (string, error)
}
