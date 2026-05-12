package outbound

import (
	"context"
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

// FirebaseClient verifies inbound Firebase ID tokens. The concrete
// implementation in infra/clients wraps the firebase-admin Go SDK.
type FirebaseClient interface {
	// VerifyIDToken verifies the signature, issuer, audience, and
	// expiration of idToken. Returns an apperror.Error of kind
	// Unauthorized on any failure so callers can surface 401 without
	// case-splitting.
	VerifyIDToken(ctx context.Context, idToken string) (*FirebaseToken, error)
}
