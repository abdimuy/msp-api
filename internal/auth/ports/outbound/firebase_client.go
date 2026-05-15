package outbound

import (
	"context"
	"errors"
	"time"
)

// FirebaseToken is the verified payload of a Firebase ID token. Mirrors a
// small, intentional subset of firebase.auth.Token so the auth module does
// not leak the firebase-admin Go SDK types into its domain or app code.
type FirebaseToken struct {
	// UID is the Firebase Authentication user id.
	UID string
	// Email is the verified email claim, if present.
	Email string
	// Name is the display name claim, if present.
	Name string
	// IssuedAt is the iat claim as a Go time.
	IssuedAt time.Time
	// ExpiresAt is the exp claim as a Go time.
	ExpiresAt time.Time
}

// ErrFirebaseTransient is the sentinel that FirebaseClient implementations
// return (wrapped) when an admin-side call fails due to a temporary
// condition: network error, 5xx response, rate limit, or canceled
// deadline. Callers (e.g. outbox handlers) detect it via errors.Is and
// translate to a retry-friendly status.
var ErrFirebaseTransient = errors.New("firebase: transient failure")

// FirebaseClient is the auth module's outbound port to Firebase
// Authentication. The concrete implementation in infra/firebase wraps the
// firebase-admin Go SDK.
type FirebaseClient interface {
	// VerifyIDToken verifies the signature, issuer, audience, and
	// expiration of idToken. Returns an apperror.Error of kind
	// Unauthorized on any failure so callers can surface 401 without
	// case-splitting.
	VerifyIDToken(ctx context.Context, idToken string) (*FirebaseToken, error)

	// DisableUser sets disabled=true on the Firebase Authentication account
	// identified by uid. Idempotent: calling on an already-disabled account
	// is a no-op success.
	//
	// Errors:
	//   - apperror.NewNotFound("firebase_user_not_found", ...) when uid
	//     does not exist. Callers may treat as no-op.
	//   - A wrapped error that errors.Is matches ErrFirebaseTransient for
	//     network / 5xx / rate-limit failures. Callers should retry.
	//   - apperror.NewInternal("firebase_admin_failed", ...) for any other
	//     SDK failure (permanent).
	DisableUser(ctx context.Context, uid string) error

	// EnableUser is the inverse of DisableUser. Error semantics match
	// DisableUser. Reserved for future reactivation flows; not currently
	// invoked by any service.
	EnableUser(ctx context.Context, uid string) error
}
