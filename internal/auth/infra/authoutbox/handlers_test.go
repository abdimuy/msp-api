package authoutbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// ---------------------------------------------------------------------------
// Compile-time interface assertion (redundant with handlers.go but explicit).
// ---------------------------------------------------------------------------

var _ outboxfb.Handler = (*UserDeactivatedHandler)(nil)

// ---------------------------------------------------------------------------
// fakeFirebaseClient
// ---------------------------------------------------------------------------

// fakeFirebaseClient is a hand-rolled test double for outbound.FirebaseClient.
// It records which UIDs were passed to DisableUser / EnableUser and returns
// pre-configured errors.
type fakeFirebaseClient struct {
	disabledUIDs []string
	enabledUIDs  []string
	disableErr   error
	enableErr    error
}

func (f *fakeFirebaseClient) VerifyIDToken(_ context.Context, _ string) (*outbound.FirebaseToken, error) {
	panic("fakeFirebaseClient.VerifyIDToken should never be called in handler tests")
}

func (f *fakeFirebaseClient) DisableUser(_ context.Context, uid string) error {
	f.disabledUIDs = append(f.disabledUIDs, uid)
	return f.disableErr
}

func (f *fakeFirebaseClient) EnableUser(_ context.Context, uid string) error {
	f.enabledUIDs = append(f.enabledUIDs, uid)
	return f.enableErr
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeEvent builds an outboxfb.Event with a JSON-marshaled payload.
func makeEvent(t *testing.T, eventType string, payload any) outboxfb.Event {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err, "makeEvent: marshal payload")
	return outboxfb.Event{
		ID:          uuid.New(),
		Aggregate:   "usuario",
		AggregateID: uuid.New(),
		EventType:   eventType,
		Payload:     json.RawMessage(body),
		CreatedAt:   time.Now(),
	}
}

// captureSlog redirects the global slog default to a text-format buffer and
// restores the previous default via t.Cleanup. The returned *bytes.Buffer
// accumulates all log output for the duration of the test.
//
// Tests that call captureSlog must NOT call t.Parallel() because they mutate
// the package-level slog default.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestUserDeactivatedHandler_EventType verifies the routing constant.
func TestUserDeactivatedHandler_EventType(t *testing.T) {
	t.Parallel()
	fb := &fakeFirebaseClient{}
	h := NewUserDeactivatedHandler(fb)
	assert.Equal(t, "user.deactivated", h.EventType())
}

// TestUserDeactivatedHandler_HappyPath: valid payload → DisableUser called,
// log line emitted, nil returned.
func TestUserDeactivatedHandler_HappyPath(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	buf := captureSlog(t)

	fb := &fakeFirebaseClient{}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "abc",
	})

	err := h.Handle(context.Background(), evt)
	require.NoError(t, err)

	require.Len(t, fb.disabledUIDs, 1, "DisableUser must be called exactly once")
	assert.Equal(t, "abc", fb.disabledUIDs[0])
	assert.Contains(t, buf.String(), "auth.firebase_user_disabled")
}

// TestUserDeactivatedHandler_EmptyFirebaseUID_IsNoop: payload has
// firebase_uid="" → no external call, log line emitted, nil returned.
func TestUserDeactivatedHandler_EmptyFirebaseUID_IsNoop(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	buf := captureSlog(t)

	fb := &fakeFirebaseClient{}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "",
	})

	err := h.Handle(context.Background(), evt)
	require.NoError(t, err)

	assert.Empty(t, fb.disabledUIDs, "DisableUser must NOT be called for empty uid")
	assert.Contains(t, buf.String(), "auth.user_deactivated_handler.empty_uid")
}

// TestUserDeactivatedHandler_MissingFirebaseUIDField_IsNoop: payload has no
// firebase_uid key → treated as empty string, same noop behavior.
func TestUserDeactivatedHandler_MissingFirebaseUIDField_IsNoop(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	buf := captureSlog(t)

	fb := &fakeFirebaseClient{}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"usuario_id": uuid.New().String(),
	})

	err := h.Handle(context.Background(), evt)
	require.NoError(t, err)

	assert.Empty(t, fb.disabledUIDs, "DisableUser must NOT be called when firebase_uid is absent")
	assert.Contains(t, buf.String(), "auth.user_deactivated_handler.empty_uid")
}

// TestUserDeactivatedHandler_MalformedPayload_ReturnsPermanentError: invalid
// JSON → permanent error with apperror code "user_deactivated_payload_invalid".
func TestUserDeactivatedHandler_MalformedPayload_ReturnsPermanentError(t *testing.T) {
	t.Parallel()

	fb := &fakeFirebaseClient{}
	h := NewUserDeactivatedHandler(fb)

	evt := outboxfb.Event{
		ID:          uuid.New(),
		Aggregate:   "usuario",
		AggregateID: uuid.New(),
		EventType:   EventTypeUserDeactivated,
		Payload:     json.RawMessage("not-json"),
		CreatedAt:   time.Now(),
	}

	err := h.Handle(context.Background(), evt)
	require.Error(t, err)
	require.NotErrorIs(t, err, outboxfb.ErrTransient, "malformed payload must be a permanent error, not transient")

	ae, ok := apperror.As(err)
	require.True(t, ok, "error must be an *apperror.Error")
	assert.Equal(t, "user_deactivated_payload_invalid", ae.Code)

	assert.Empty(t, fb.disabledUIDs, "DisableUser must NOT be called on malformed payload")
}

// TestUserDeactivatedHandler_FirebaseUserNotFound_IsIdempotent: Firebase
// returns "firebase_user_not_found" → handler treats as success (nil), logs
// "auth.firebase_user_already_gone".
func TestUserDeactivatedHandler_FirebaseUserNotFound_IsIdempotent(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	buf := captureSlog(t)

	fb := &fakeFirebaseClient{
		disableErr: apperror.NewNotFound("firebase_user_not_found", "usuario de firebase no encontrado"),
	}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "uid-gone",
	})

	err := h.Handle(context.Background(), evt)
	require.NoError(t, err, "user-not-found must be treated as success (idempotent)")

	assert.Contains(t, buf.String(), "auth.firebase_user_already_gone")
}

// TestUserDeactivatedHandler_FirebaseTransient_ReturnsOutboxTransient: wrapped
// ErrFirebaseTransient → outboxfb.ErrTransient is returned so the dispatcher
// retries.
func TestUserDeactivatedHandler_FirebaseTransient_ReturnsOutboxTransient(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	buf := captureSlog(t)

	fb := &fakeFirebaseClient{
		disableErr: fmt.Errorf("wrap: %w", outbound.ErrFirebaseTransient),
	}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "uid-transient",
	})

	err := h.Handle(context.Background(), evt)
	require.Error(t, err)
	require.ErrorIs(t, err, outboxfb.ErrTransient, "transient firebase error must surface as outboxfb.ErrTransient")
	assert.Contains(t, buf.String(), "auth.firebase_user_disable_transient")
}

// TestUserDeactivatedHandler_FirebaseUnknownError_PropagatesAsPermanent:
// an unexpected plain error → propagated as-is, NOT wrapped in ErrTransient.
func TestUserDeactivatedHandler_FirebaseUnknownError_PropagatesAsPermanent(t *testing.T) {
	t.Parallel()

	weirdErr := errors.New("weird")
	fb := &fakeFirebaseClient{disableErr: weirdErr}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "uid-weird",
	})

	err := h.Handle(context.Background(), evt)
	require.Error(t, err)
	require.ErrorIs(t, err, weirdErr, "unknown error must be propagated as-is")
	assert.NotErrorIs(t, err, outboxfb.ErrTransient, "unknown error must NOT be transient")
}

// TestUserDeactivatedHandler_FirebaseInternalApperror_PropagatesAsPermanent:
// apperror.NewInternal from Firebase → propagated as-is (permanent), not
// transient.
func TestUserDeactivatedHandler_FirebaseInternalApperror_PropagatesAsPermanent(t *testing.T) {
	t.Parallel()

	internalErr := apperror.NewInternal("firebase_admin_failed", "fallo interno de firebase admin")
	fb := &fakeFirebaseClient{disableErr: internalErr}
	h := NewUserDeactivatedHandler(fb)

	evt := makeEvent(t, EventTypeUserDeactivated, map[string]any{
		"firebase_uid": "uid-internal",
	})

	err := h.Handle(context.Background(), evt)
	require.Error(t, err)
	require.ErrorIs(t, err, internalErr, "internal apperror must be propagated as-is")
	require.NotErrorIs(t, err, outboxfb.ErrTransient, "internal apperror must NOT be transient")

	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebase_admin_failed", ae.Code)
}
