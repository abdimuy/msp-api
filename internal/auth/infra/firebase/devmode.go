package firebase

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// devModeTokenPrefix is the magic string that marks a DevMode token. Real
// Firebase tokens start with "eyJ" (JWT header) so the surface for confusion
// is small.
const devModeTokenPrefix = "dev:"

// devModeTokenTTL is the synthetic expiration window stamped on accepted
// tokens. Long enough to be useful in a dev session, short enough that a
// stale token from a previous debug session does not trivially work today.
const devModeTokenTTL = 12 * time.Hour

// DevModeClient verifies tokens of the form "dev:<uid>[:<email>]".
//
// SECURITY: This client must NEVER be wired up outside APP_ENV=development.
// NewDevModeClient refuses to be constructed otherwise. Every successful
// verification logs an `auth.dev_mode_token_accepted` warning so the bypass
// is visible in any environment whose logs are inspected.
type DevModeClient struct{}

// NewDevModeClient constructs the DevMode client. Refuses with apperror
// if env != EnvDevelopment.
func NewDevModeClient(env config.Environment) (*DevModeClient, error) {
	if env != config.EnvDevelopment {
		return nil, apperror.NewInternal(
			"firebase_devmode_not_permitted",
			"firebase dev mode no permitido fuera de desarrollo",
		).WithField("env", string(env))
	}
	slog.Warn("auth.dev_mode_enabled",
		"client", "firebase",
		"warning", "every Authorization: Bearer dev:<uid> token will be accepted; do not enable in production")
	return &DevModeClient{}, nil
}

var _ outbound.FirebaseClient = (*DevModeClient)(nil)

// VerifyIDToken parses a "dev:<uid>[:<email>]" token. Returns
// apperror.Unauthorized on any failure.
func (c *DevModeClient) VerifyIDToken(ctx context.Context, idToken string) (*outbound.FirebaseToken, error) {
	uid, email, err := parseDevModeToken(idToken)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	slog.WarnContext(ctx, "auth.dev_mode_token_accepted",
		"uid", uid,
		"email", email)
	return &outbound.FirebaseToken{
		UID:       uid,
		Email:     email,
		Name:      "",
		IssuedAt:  now,
		ExpiresAt: now.Add(devModeTokenTTL),
	}, nil
}

// parseDevModeToken validates the "dev:<uid>[:<email>]" shape. Returns
// uid, email, error. apperror.Unauthorized on every failure path so a
// malformed dev token surfaces as a normal 401 from the middleware.
//
//nolint:nonamedreturns // named returns disambiguate two strings (uid vs email).
func parseDevModeToken(s string) (uid, email string, err error) {
	if !strings.HasPrefix(s, devModeTokenPrefix) {
		return "", "", apperror.NewUnauthorized(
			"firebase_token_invalid",
			"token de desarrollo malformado",
		)
	}
	body := strings.TrimPrefix(s, devModeTokenPrefix)
	parts := strings.SplitN(body, ":", 2)
	uid = parts[0]
	if uid == "" {
		return "", "", apperror.NewUnauthorized(
			"firebase_token_invalid",
			"uid vacío en token de desarrollo",
		)
	}
	// UID must be printable ASCII no spaces; matches FirebaseUID VO rules
	// upstream.
	for i := range len(uid) {
		c := uid[i]
		if c < 0x21 || c > 0x7E {
			return "", "", apperror.NewUnauthorized(
				"firebase_token_invalid",
				"uid contiene caracteres no permitidos",
			)
		}
	}
	if len(parts) == 2 {
		email = parts[1]
	}
	return uid, email, nil
}
