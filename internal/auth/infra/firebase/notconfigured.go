package firebase

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// NotConfiguredClient is the safe default: every VerifyIDToken call returns
// a stable 401. Use it when the API is deployed without Firebase
// credentials (staging without auth, internal-tooling-only deployments).
type NotConfiguredClient struct{}

// NewNotConfiguredClient constructs the always-error client.
func NewNotConfiguredClient() *NotConfiguredClient {
	return &NotConfiguredClient{}
}

// Compile-time interface assertion.
var _ outbound.FirebaseClient = (*NotConfiguredClient)(nil)

// VerifyIDToken always returns apperror.Unauthorized.
func (c *NotConfiguredClient) VerifyIDToken(_ context.Context, _ string) (*outbound.FirebaseToken, error) {
	return nil, apperror.NewUnauthorized("firebase_not_configured", "firebase no configurado")
}

// DisableUser refuses with an internal-error code: Firebase is not
// configured so we cannot complete the admin operation. The kind is
// Internal (not Unauthorized) because this is a server-side operational
// gap, not a client-side auth failure.
func (c *NotConfiguredClient) DisableUser(_ context.Context, _ string) error {
	return apperror.NewInternal("firebase_not_configured", "firebase no configurado")
}

// EnableUser mirrors DisableUser: refuses with an internal-error code.
func (c *NotConfiguredClient) EnableUser(_ context.Context, _ string) error {
	return apperror.NewInternal("firebase_not_configured", "firebase no configurado")
}
