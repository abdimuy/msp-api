package firebase

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"firebase.google.com/go/v4/errorutils"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// tokenVerifier is the minimal surface RealClient consumes from the
// Firebase Admin SDK for token verification. Defined here so unit tests
// can substitute a fake without touching the network or the SDK internals.
type tokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error)
}

// userManager is the minimal surface RealClient consumes from the Firebase
// Admin SDK for user management (disable/enable). Defined here so unit
// tests can inject a fake without exercising the network.
type userManager interface {
	UpdateUser(ctx context.Context, uid string, user *auth.UserToUpdate) (*auth.UserRecord, error)
}

// RealClient is the production implementation of outbound.FirebaseClient.
// It verifies Firebase ID tokens against Google's public certificate set
// (cached for ~6h by the SDK) and translates every SDK failure into an
// apperror.Unauthorized so the auth middleware can surface 401 without
// case-splitting on the underlying error.
//
// Revocation: this client uses plain VerifyIDToken (no revocation check).
// Revoked tokens remain valid until their natural expiration (~1h). This
// is a deliberate trade-off in favor of zero extra round-trips per
// request — see ADR-0004.
type RealClient struct {
	verifier  tokenVerifier
	users     userManager
	projectID string
}

// Compile-time interface assertion.
var _ outbound.FirebaseClient = (*RealClient)(nil)

// NewRealClient initializes the Firebase Admin SDK with the configured
// service-account credential and project id, then returns a ready
// RealClient. Fails fast if the service account file is missing or the
// SDK rejects the credential.
func NewRealClient(ctx context.Context, cfg config.Firebase) (*RealClient, error) {
	if _, err := os.Stat(cfg.ServiceAccountPath); err != nil {
		return nil, apperror.NewInternal(
			"firebase_service_account_missing",
			"no se pudo leer el service account de firebase",
		).WithError(err).WithField("path", cfg.ServiceAccountPath)
	}
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.ProjectID},
		option.WithCredentialsFile(cfg.ServiceAccountPath),
	)
	if err != nil {
		return nil, apperror.NewInternal(
			"firebase_app_init_failed",
			"no se pudo inicializar firebase",
		).WithError(err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, apperror.NewInternal(
			"firebase_auth_init_failed",
			"no se pudo inicializar el cliente de auth de firebase",
		).WithError(err)
	}
	slog.Info("auth.firebase_real_client_ready", "project_id", cfg.ProjectID)
	return &RealClient{verifier: authClient, users: authClient, projectID: cfg.ProjectID}, nil
}

// VerifyIDToken delegates to the Firebase Admin SDK and translates any
// failure to apperror.Unauthorized with a code that pinpoints the cause
// for logs/metrics. The middleware ignores the code and returns 401 to
// the client either way.
func (c *RealClient) VerifyIDToken(ctx context.Context, idToken string) (*outbound.FirebaseToken, error) {
	tok, err := c.verifier.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, classifyVerifyError(err)
	}
	return tokenToOutbound(tok), nil
}

// DisableUser flips disabled=true on the Firebase account uid. Idempotent
// in Firebase: calling on an already-disabled user succeeds.
func (c *RealClient) DisableUser(ctx context.Context, uid string) error {
	return c.updateDisabledFlag(ctx, uid, true)
}

// EnableUser flips disabled=false on the Firebase account uid.
func (c *RealClient) EnableUser(ctx context.Context, uid string) error {
	return c.updateDisabledFlag(ctx, uid, false)
}

// updateDisabledFlag is the shared implementation of DisableUser /
// EnableUser. It calls the SDK's UpdateUser with only the Disabled field
// set and routes any failure through classifyAdminError.
func (c *RealClient) updateDisabledFlag(ctx context.Context, uid string, disabled bool) error {
	update := (&auth.UserToUpdate{}).Disabled(disabled)
	_, err := c.users.UpdateUser(ctx, uid, update)
	if err == nil {
		return nil
	}
	return classifyAdminError(err, uid, disabled)
}

// firebaseIsUserNotFound is the predicate used by classifyAdminError to
// detect a user-not-found error from the SDK. It is a package var (rather
// than a direct call to auth.IsUserNotFound) so internal unit tests can
// substitute a stub; the SDK's internal.FirebaseError type is not
// importable, making real-shape errors impossible to fabricate otherwise.
var firebaseIsUserNotFound = auth.IsUserNotFound

// classifyAdminError maps Admin SDK / transport errors into the contract
// documented on outbound.FirebaseClient.
//
//   - user-not-found    → apperror.NewNotFound("firebase_user_not_found", ...)
//   - transient classes → wrapped error matching outbound.ErrFirebaseTransient
//   - everything else   → apperror.NewInternal("firebase_admin_failed", ...)
func classifyAdminError(err error, uid string, disabled bool) error {
	if firebaseIsUserNotFound(err) {
		return apperror.NewNotFound(
			"firebase_user_not_found",
			"el usuario no existe en firebase",
		).WithError(err).WithField("uid", uid)
	}
	if isTransientAdminError(err) {
		// Wrap so callers can match via errors.Is(err, outbound.ErrFirebaseTransient).
		return wrapTransient(err, uid, disabled)
	}
	return apperror.NewInternal(
		"firebase_admin_failed",
		"firebase admin sdk falló",
	).WithError(err).
		WithField("uid", uid).
		WithField("disabled", disabled)
}

// isTransientAdminError reports whether err is a class the dispatcher
// should retry rather than mark as permanently failed.
func isTransientAdminError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// SDK-classified transient categories.
	if errorutils.IsDeadlineExceeded(err) ||
		errorutils.IsUnavailable(err) ||
		errorutils.IsInternal(err) ||
		errorutils.IsAborted(err) ||
		errorutils.IsResourceExhausted(err) ||
		errorutils.IsUnknown(err) {
		return true
	}
	// Transport-level failures the SDK wraps in net.OpError / url.Error.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// transientError wraps the underlying SDK error so the contract sentinel
// outbound.ErrFirebaseTransient is reachable through errors.Is.
type transientError struct {
	uid      string
	disabled bool
	cause    error
}

func (e *transientError) Error() string {
	return "firebase: transient failure updating user " + e.uid + ": " + e.cause.Error()
}

func (e *transientError) Unwrap() error { return e.cause }

// Is reports true for outbound.ErrFirebaseTransient. Other targets fall
// through to errors.Is on the wrapped cause via Unwrap.
func (e *transientError) Is(target error) bool {
	return target == outbound.ErrFirebaseTransient
}

func wrapTransient(err error, uid string, disabled bool) error {
	return &transientError{uid: uid, disabled: disabled, cause: err}
}

// classifyVerifyError maps the SDK's typed errors onto specific
// apperror.Unauthorized codes. The SDK's helpers are layered (expired
// implies invalid), so check the most specific cases first. Anything we
// cannot classify falls into firebase_token_invalid.
func classifyVerifyError(err error) error {
	switch {
	case auth.IsIDTokenExpired(err):
		return apperror.NewUnauthorized(
			"firebase_token_expired",
			"el token ha expirado",
		).WithError(err)
	case auth.IsIDTokenRevoked(err):
		return apperror.NewUnauthorized(
			"firebase_token_revoked",
			"el token fue revocado",
		).WithError(err)
	case auth.IsCertificateFetchFailed(err):
		return apperror.NewUnauthorized(
			"firebase_token_invalid",
			"no se pudo verificar el token",
		).WithError(err)
	case auth.IsIDTokenInvalid(err):
		return classifyInvalidToken(err)
	}
	return apperror.NewUnauthorized(
		"firebase_token_invalid",
		"token inválido",
	).WithError(err)
}

// classifyInvalidToken inspects the message of an IDTokenInvalid error to
// distinguish signature problems and audience/issuer mismatches from the
// generic "malformed token" bucket. The SDK does not expose typed
// sub-errors for these so we string-match its formatted messages.
func classifyInvalidToken(err error) error {
	msg := err.Error()
	switch {
	case containsAny(msg, "signature is invalid", "invalid signature"):
		return apperror.NewUnauthorized(
			"firebase_token_invalid_signature",
			"firma del token inválida",
		).WithError(err)
	case containsAny(msg, "'aud'", "audience", "'iss'", "issuer"):
		return apperror.NewUnauthorized(
			"firebase_token_wrong_audience",
			"token no emitido para este proyecto",
		).WithError(err)
	}
	return apperror.NewUnauthorized(
		"firebase_token_invalid",
		"token inválido",
	).WithError(err)
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// tokenToOutbound converts the SDK's auth.Token into the auth-module's
// port-shaped FirebaseToken. Missing claims become empty strings (the
// upstream middleware treats them as optional).
func tokenToOutbound(tok *auth.Token) *outbound.FirebaseToken {
	return &outbound.FirebaseToken{
		UID:       tok.UID,
		Email:     claimString(tok.Claims, "email"),
		Name:      claimString(tok.Claims, "name"),
		IssuedAt:  time.Unix(tok.IssuedAt, 0).UTC(),
		ExpiresAt: time.Unix(tok.Expires, 0).UTC(),
	}
}

// claimString returns the string-typed value for key, or "" if the claim
// is missing or not a string.
func claimString(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

// newRealClientWithVerifier is an internal helper to inject a custom
// tokenVerifier. Used by unit tests; production code should always use
// NewRealClient. The userManager is left nil; tests that exercise the
// admin code path must use newRealClientWithDeps instead.
func newRealClientWithVerifier(v tokenVerifier, projectID string) *RealClient {
	return &RealClient{verifier: v, projectID: projectID}
}

// newRealClientWithDeps is an internal helper to inject both the verifier
// and the user manager. Used by admin-path unit tests.
func newRealClientWithDeps(v tokenVerifier, u userManager, projectID string) *RealClient {
	return &RealClient{verifier: v, users: u, projectID: projectID}
}
