package firebase

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// recordedUpdateCall captures a single UpdateUser invocation so tests can
// assert how the client called the SDK.
type recordedUpdateCall struct {
	uid     string
	payload *auth.UserToUpdate
}

// fakeUserManager is a hand-rolled stub for the userManager interface.
type fakeUserManager struct {
	calls []recordedUpdateCall
	err   error
}

func (f *fakeUserManager) UpdateUser(_ context.Context, uid string, u *auth.UserToUpdate) (*auth.UserRecord, error) {
	f.calls = append(f.calls, recordedUpdateCall{uid: uid, payload: u})
	if f.err != nil {
		return nil, f.err
	}
	return &auth.UserRecord{UserInfo: &auth.UserInfo{UID: uid}}, nil
}

// withStubIsUserNotFound swaps the package-level seam for the duration of
// the test, restoring it via t.Cleanup. Tests that exercise the not-found
// branch use this to bypass the SDK's internal-only FirebaseError type.
func withStubIsUserNotFound(t *testing.T, stub func(error) bool) {
	t.Helper()
	prev := firebaseIsUserNotFound
	firebaseIsUserNotFound = stub
	t.Cleanup(func() { firebaseIsUserNotFound = prev })
}

func TestRealClient_DisableUser_HappyPath(t *testing.T) { //nolint:paralleltest // mutates package seam in some tests
	users := &fakeUserManager{}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-123")

	require.NoError(t, err)
	require.Len(t, users.calls, 1)
	assert.Equal(t, "uid-123", users.calls[0].uid)
	// We can't introspect the UserToUpdate map directly (unexported), but
	// we can prove the payload was non-nil.
	assert.NotNil(t, users.calls[0].payload)
}

func TestRealClient_EnableUser_HappyPath(t *testing.T) { //nolint:paralleltest // mutates package seam in some tests
	users := &fakeUserManager{}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.EnableUser(context.Background(), "uid-456")

	require.NoError(t, err)
	require.Len(t, users.calls, 1)
	assert.Equal(t, "uid-456", users.calls[0].uid)
}

func TestRealClient_DisableUser_UserNotFound_ReturnsAppErrorNotFound(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return true })
	users := &fakeUserManager{err: errors.New("sdk: user not found")}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "missing-uid")

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok, "expected apperror, got %T", err)
	assert.Equal(t, apperror.KindNotFound, ae.Kind)
	assert.Equal(t, "firebase_user_not_found", ae.Code)
	assert.Equal(t, "missing-uid", ae.Fields["uid"])
	// errors.Is must NOT match the transient sentinel.
	assert.NotErrorIs(t, err, outbound.ErrFirebaseTransient)
}

func TestRealClient_DisableUser_ContextDeadline_IsTransient(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	users := &fakeUserManager{err: context.DeadlineExceeded}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-1")

	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrFirebaseTransient)
	// Underlying cause is preserved through Unwrap.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRealClient_DisableUser_ContextCanceled_IsTransient(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	users := &fakeUserManager{err: context.Canceled}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-1")

	require.Error(t, err)
	assert.ErrorIs(t, err, outbound.ErrFirebaseTransient)
}

func TestRealClient_DisableUser_NetOpError_IsTransient(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	netErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	users := &fakeUserManager{err: netErr}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-1")

	require.Error(t, err)
	assert.ErrorIs(t, err, outbound.ErrFirebaseTransient)
}

func TestRealClient_DisableUser_URLError_IsTransient(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	urlErr := &url.Error{Op: "POST", URL: "https://example.invalid", Err: errors.New("eof")}
	users := &fakeUserManager{err: urlErr}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-1")

	require.Error(t, err)
	assert.ErrorIs(t, err, outbound.ErrFirebaseTransient)
}

func TestRealClient_DisableUser_UnknownError_IsPermanent(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	users := &fakeUserManager{err: errors.New("some weird sdk failure")}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.DisableUser(context.Background(), "uid-1")

	require.Error(t, err)
	require.NotErrorIs(t, err, outbound.ErrFirebaseTransient,
		"unknown errors must NOT be classified as transient")
	ae, ok := apperror.As(err)
	require.True(t, ok, "expected apperror, got %T", err)
	assert.Equal(t, apperror.KindInternal, ae.Kind)
	assert.Equal(t, "firebase_admin_failed", ae.Code)
	assert.Equal(t, "uid-1", ae.Fields["uid"])
	assert.Equal(t, true, ae.Fields["disabled"])
}

func TestRealClient_EnableUser_UnknownError_RecordsDisabledFalse(t *testing.T) { //nolint:paralleltest // mutates seam
	withStubIsUserNotFound(t, func(_ error) bool { return false })
	users := &fakeUserManager{err: errors.New("boom")}
	c := newRealClientWithDeps(nil, users, "test-project")

	err := c.EnableUser(context.Background(), "uid-2")

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, false, ae.Fields["disabled"])
}

func TestClassifyAdminError_TransientErrorIsHelpsErrorsIs(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying")
	wrapped := wrapTransient(cause, "uid-z", true)

	require.Error(t, wrapped)
	require.ErrorIs(t, wrapped, outbound.ErrFirebaseTransient)
	require.ErrorIs(t, wrapped, cause, "Unwrap must expose the cause")
	assert.Contains(t, wrapped.Error(), "uid-z")
}

func TestIsTransientAdminError_NilReturnsFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, isTransientAdminError(nil))
}
