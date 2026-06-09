package authoutbox

// Live-Firebase integration tests for UserDeactivatedHandler.
//
// These tests mutate the real Firebase Auth project (msp-dev-96ff5).
// They are double-gated and will SKIP unless BOTH env vars are set:
//
//	INTEGRATION=1
//	FIREBASE_LIVE_TEST=1
//
// To run them:
//
//	INTEGRATION=1 FIREBASE_LIVE_TEST=1 \
//	    FIREBASE_SERVICE_ACCOUNT_PATH=$PWD/serviceAccountKey.json \
//	    go test -v -count=1 -run TestIntegration \
//	    ./internal/auth/infra/authoutbox/
//
// The four tests that touch the shared vendedor account must run sequentially
// (no t.Parallel) to avoid races on Firebase state. The UID-random test
// (TestIntegration_HandlerOnNonExistentUID) is safe to run in parallel.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

const (
	liveTestEmail    = "vendedor1-test@msp-dev.test"
	liveTestPassword = "test-password-123"
	liveProjectID    = "msp-dev-96ff5"
)

// ---------------------------------------------------------------------------
// Gate helpers
// ---------------------------------------------------------------------------

// requireLiveFirebase skips the calling test unless both INTEGRATION=1 and
// FIREBASE_LIVE_TEST=1 are set. Call at the top of every live test.
func requireLiveFirebase(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION") == "" || os.Getenv("FIREBASE_LIVE_TEST") == "" {
		t.Skip("set INTEGRATION=1 FIREBASE_LIVE_TEST=1 to run live Firebase tests")
	}
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// findRepoRoot walks up from the directory of this source file until it finds
// go.mod, then returns that directory. This makes the default service-account
// path work regardless of where go test is invoked from.
func findRepoRoot() string {
	// Start from the directory containing this source file.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding go.mod.
			return "."
		}
		dir = parent
	}
}

// serviceAccountPath returns the absolute path to the service-account JSON.
// It prefers FIREBASE_SERVICE_ACCOUNT_PATH env var; falls back to
// serviceAccountKey.json at the repo root.
func serviceAccountPath() string {
	if p := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH"); p != "" {
		return p
	}
	return filepath.Join(findRepoRoot(), "serviceAccountKey.json")
}

// projectID returns the Firebase project ID.
// It prefers FIREBASE_PROJECT_ID env var; falls back to the canonical dev ID.
func projectID() string {
	if p := os.Getenv("FIREBASE_PROJECT_ID"); p != "" {
		return p
	}
	return liveProjectID
}

// ---------------------------------------------------------------------------
// Client helpers
// ---------------------------------------------------------------------------

// buildRealClient constructs both a *firebase.RealClient (handler's dependency)
// and a raw *auth.Client (for test setup/assertions: GetUserByEmail, GetUser,
// CreateUser, UpdateUser). Both are backed by the same service-account
// credentials.
func buildRealClient(t *testing.T) (*firebase.RealClient, *auth.Client) {
	t.Helper()
	ctx := context.Background()

	saPath := serviceAccountPath()
	pid := projectID()

	// RealClient via the production constructor.
	rc, err := firebase.NewRealClient(ctx, config.Firebase{
		ProjectID:          pid,
		ServiceAccountPath: saPath,
	})
	require.NoError(t, err, "firebase.NewRealClient failed — check FIREBASE_SERVICE_ACCOUNT_PATH")

	// Raw auth.Client for test assertions.
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: pid},
		option.WithCredentialsFile(saPath),
	)
	require.NoError(t, err, "firebasesdk.NewApp failed")
	rawAuth, err := app.Auth(ctx)
	require.NoError(t, err, "app.Auth failed")

	return rc, rawAuth
}

// ---------------------------------------------------------------------------
// User-management helper
// ---------------------------------------------------------------------------

// ensureTestUser guarantees the test account exists and is enabled, then
// returns its UID. If the account is missing it is created. If it is disabled
// it is re-enabled. The function is idempotent across test runs.
func ensureTestUser(t *testing.T, fbauth *auth.Client, email string) string {
	t.Helper()
	ctx := context.Background()

	user, err := fbauth.GetUserByEmail(ctx, email)
	if err != nil {
		if !auth.IsUserNotFound(err) {
			require.NoError(t, err, "unexpected error looking up test user")
		}
		// User does not exist — create them.
		params := (&auth.UserToCreate{}).
			Email(email).
			Password(liveTestPassword)
		created, createErr := fbauth.CreateUser(ctx, params)
		require.NoError(t, createErr, "CreateUser failed for test account %q", email)
		return created.UID
	}

	// User exists; re-enable if a prior run left it disabled.
	if user.Disabled {
		update := (&auth.UserToUpdate{}).Disabled(false)
		_, updateErr := fbauth.UpdateUser(ctx, user.UID, update)
		require.NoError(t, updateErr, "re-enabling test account %q failed", email)
	}

	return user.UID
}

// ---------------------------------------------------------------------------
// makeDeactivatedEvent builds an outboxfb.Event with the user.deactivated type
// and a payload carrying the given Firebase UID.
// ---------------------------------------------------------------------------

func makeDeactivatedEvent(t *testing.T, firebaseUID string) outboxfb.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"firebase_uid": firebaseUID})
	require.NoError(t, err)
	return outboxfb.Event{
		ID:          uuid.New(),
		Aggregate:   "usuario",
		AggregateID: uuid.New(),
		EventType:   EventTypeUserDeactivated,
		Payload:     json.RawMessage(payload),
		CreatedAt:   time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestIntegration_HandlerDisablesRealFirebaseUser exercises the full
// handler→Firebase round-trip: call Handle with a real UID and verify the
// account is left disabled in Firebase Auth.
func TestIntegration_HandlerDisablesRealFirebaseUser(t *testing.T) { //nolint:paralleltest // shares vendedor1-test fixture across tests
	// NOT t.Parallel() — shares vendedor account with other sequential tests.
	requireLiveFirebase(t)

	ctx := context.Background()
	rc, rawAuth := buildRealClient(t)

	uid := ensureTestUser(t, rawAuth, liveTestEmail)

	// Cleanup: always re-enable after the test so the next run finds the
	// account in a clean state.
	t.Cleanup(func() {
		update := (&auth.UserToUpdate{}).Disabled(false)
		_, _ = rawAuth.UpdateUser(ctx, uid, update)
	})

	handler := NewUserDeactivatedHandler(rc)
	evt := makeDeactivatedEvent(t, uid)

	err := handler.Handle(ctx, evt)
	require.NoError(t, err, "handler.Handle must succeed against a real Firebase user")

	// Verify via the raw admin client.
	record, err := rawAuth.GetUser(ctx, uid)
	require.NoError(t, err, "GetUser after Handle failed")
	assert.True(t, record.Disabled, "user must be disabled after Handle")
}

// TestIntegration_HandlerIdempotent verifies that calling Handle twice with
// the same payload returns nil on both invocations (Firebase is idempotent
// on disabling an already-disabled user).
func TestIntegration_HandlerIdempotent(t *testing.T) { //nolint:paralleltest // shares vendedor1-test fixture across tests
	// NOT t.Parallel() — shares vendedor account.
	requireLiveFirebase(t)

	ctx := context.Background()
	rc, rawAuth := buildRealClient(t)

	uid := ensureTestUser(t, rawAuth, liveTestEmail)

	t.Cleanup(func() {
		update := (&auth.UserToUpdate{}).Disabled(false)
		_, _ = rawAuth.UpdateUser(ctx, uid, update)
	})

	handler := NewUserDeactivatedHandler(rc)
	evt := makeDeactivatedEvent(t, uid)

	err := handler.Handle(ctx, evt)
	require.NoError(t, err, "first Handle call must succeed")

	err = handler.Handle(ctx, evt)
	require.NoError(t, err, "second Handle call must also return nil (idempotent)")
}

// TestIntegration_HandlerOnNonExistentUID verifies that passing a UID that
// does not exist in Firebase is treated as idempotent success (nil) by the
// handler — the user is already gone, nothing to disable.
func TestIntegration_HandlerOnNonExistentUID(t *testing.T) {
	t.Parallel() // safe — uses a unique UID, no shared state.
	requireLiveFirebase(t)

	ctx := context.Background()
	rc, _ := buildRealClient(t)

	nonExistentUID := "never-existed-" + uuid.NewString()

	handler := NewUserDeactivatedHandler(rc)
	evt := makeDeactivatedEvent(t, nonExistentUID)

	err := handler.Handle(ctx, evt)
	assert.NoError(t, err, "handler must return nil for a non-existent UID (user-not-found is idempotent)")
}

// TestIntegration_EnableUserReverses exercises the EnableUser path on
// RealClient directly: disable the vendor account then enable it and verify
// the final state is Disabled==false. This is separate from the handler
// (which only does Disable) and covers the EnableUser branch of RealClient.
func TestIntegration_EnableUserReverses(t *testing.T) { //nolint:paralleltest // shares vendedor1-test fixture across tests
	// NOT t.Parallel() — shares vendedor account.
	requireLiveFirebase(t)

	ctx := context.Background()
	rc, rawAuth := buildRealClient(t)

	uid := ensureTestUser(t, rawAuth, liveTestEmail)

	t.Cleanup(func() {
		update := (&auth.UserToUpdate{}).Disabled(false)
		_, _ = rawAuth.UpdateUser(ctx, uid, update)
	})

	// Step 1: disable via RealClient.
	err := rc.DisableUser(ctx, uid)
	require.NoError(t, err, "DisableUser must succeed")

	record, err := rawAuth.GetUser(ctx, uid)
	require.NoError(t, err)
	require.True(t, record.Disabled, "user must be disabled after DisableUser")

	// Step 2: re-enable via RealClient.
	err = rc.EnableUser(ctx, uid)
	require.NoError(t, err, "EnableUser must succeed")

	record, err = rawAuth.GetUser(ctx, uid)
	require.NoError(t, err)
	assert.False(t, record.Disabled, "user must be enabled after EnableUser")
}
