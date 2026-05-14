package firebase_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Integration tests for the Firebase RealClient that run against a local
// Firebase Auth Emulator. They are gated by the FIREBASE_AUTH_EMULATOR_HOST
// env var: when unset, every test in this file calls t.Skip so the suite
// stays green on machines without Docker / the emulator.
//
// To run locally:
//
//	make fb-emu-up
//	FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 \
//	    go test -count=1 -v -run Integration \
//	    ./internal/auth/infra/firebase/...
//	make fb-emu-down
//
// The emulator accepts any project id prefixed with "demo-" without
// requiring real credentials; the SDK detects FIREBASE_AUTH_EMULATOR_HOST
// and short-circuits signature verification.

const (
	emulatorProjectID = "demo-msp-test"
	emulatorTestEmail = "real-client-test@example.invalid"
	emulatorTestPass  = "test-password-123"
)

// emulatorHost returns the configured emulator host or skips the test.
func emulatorHost(t *testing.T) string {
	t.Helper()
	host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIREBASE_AUTH_EMULATOR_HOST not set; skipping integration test")
	}
	return host
}

// newEmulatorClient builds a RealClient pointing at the emulator. The
// SDK reads FIREBASE_AUTH_EMULATOR_HOST from the env at NewApp time and
// uses an internal "no-op" credential, so no service-account file is
// required.
func newEmulatorClient(ctx context.Context, t *testing.T) *firebase.RealClient {
	t.Helper()
	app, err := firebasesdk.NewApp(ctx, &firebasesdk.Config{ProjectID: emulatorProjectID})
	require.NoError(t, err)
	authClient, err := app.Auth(ctx)
	require.NoError(t, err)
	return firebase.ExportNewRealClientForTest(authClient, emulatorProjectID)
}

// signUpResponse mirrors the relevant fields of the emulator's
// signupNewUser REST response. Field names follow Google's REST API
// (camelCase), not our project's snake_case convention.
//
//nolint:tagliatelle // emulator REST API uses camelCase JSON.
type signUpResponse struct {
	IDToken   string `json:"idToken"`
	LocalID   string `json:"localId"`
	Email     string `json:"email"`
	ExpiresIn string `json:"expiresIn"`
}

// signUpEmulatorUser creates a new user on the emulator and returns its
// idToken. Idempotent across test runs only if a different email is used
// each time — the caller passes a unique email.
func signUpEmulatorUser(t *testing.T, host, email, password string) signUpResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"email":             email,
		"password":          password,
		"returnSecureToken": true,
	})
	require.NoError(t, err)
	url := "http://" + host + "/identitytoolkit.googleapis.com/v1/accounts:signUp?key=fake-api-key"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		t.Fatalf("emulator signUp failed: status=%d body=%s", resp.StatusCode, buf[:n])
	}
	var out signUpResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.IDToken, "emulator did not return idToken")
	return out
}

// uniqueEmail produces a per-run email so signUp does not collide with a
// user left over from a previous test invocation.
func uniqueEmail(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000") + "@example.invalid"
}

func TestIntegration_VerifyIDToken_HappyPath(t *testing.T) {
	t.Parallel()
	host := emulatorHost(t)
	ctx := context.Background()

	resp := signUpEmulatorUser(t, host, uniqueEmail("happy"), emulatorTestPass)
	client := newEmulatorClient(ctx, t)

	tok, err := client.VerifyIDToken(ctx, resp.IDToken)
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.NotEmpty(t, tok.UID)
	assert.Equal(t, resp.LocalID, tok.UID)
	assert.False(t, tok.ExpiresAt.IsZero())
	assert.True(t, tok.ExpiresAt.After(time.Now()), "token should not be already expired")
}

func TestIntegration_VerifyIDToken_Malformed(t *testing.T) {
	t.Parallel()
	host := emulatorHost(t)
	_ = host
	ctx := context.Background()
	client := newEmulatorClient(ctx, t)

	_, err := client.VerifyIDToken(ctx, "not-a-jwt")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror.Error, got %T", err)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	// Malformed token surfaces as firebase_token_invalid (or one of its
	// classified siblings); the middleware returns 401 either way.
	assert.Contains(t, []string{
		"firebase_token_invalid",
		"firebase_token_invalid_signature",
	}, appErr.Code, "unexpected error code %q", appErr.Code)
}

func TestIntegration_VerifyIDToken_EmptyToken(t *testing.T) {
	t.Parallel()
	host := emulatorHost(t)
	_ = host
	ctx := context.Background()
	client := newEmulatorClient(ctx, t)

	_, err := client.VerifyIDToken(ctx, "")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

// TestIntegration_EmulatorReachable is a smoke check: if the env var is
// set, the emulator must answer. Catches misconfigured CI / docker
// before the rest of the suite emits confusing errors.
func TestIntegration_EmulatorReachable(t *testing.T) {
	t.Parallel()
	host := emulatorHost(t)
	url := "http://" + host + "/"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("emulator unreachable at %s: %v (is `make fb-emu-up` running?)", host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Any HTTP response (200, 404, etc.) proves the emulator is up;
	// we only fail on transport-level errors above.
	require.NotNil(t, resp)
}

// Compile-time check that errors.As works on the apperror returned by
// classifyVerifyError. Catches a regression where the error chain stops
// propagating the apperror type.
func TestIntegration_AppErrorChain(t *testing.T) {
	t.Parallel()
	_ = emulatorHost(t)
	ctx := context.Background()
	client := newEmulatorClient(ctx, t)

	_, err := client.VerifyIDToken(ctx, "bogus")
	require.Error(t, err)

	var target *apperror.Error
	require.ErrorAs(t, err, &target, "apperror.Error must be in the chain")
	assert.Equal(t, apperror.KindUnauthorized, target.Kind)
}
