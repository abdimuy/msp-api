package authoutbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// EventTypeUserDeactivated is the outbox event type this handler consumes.
// Kept in sync with the constant of the same name in the auth/app package;
// duplicated here so the handler does not import the app layer.
const EventTypeUserDeactivated = "user.deactivated"

// handlerCallTimeout caps the time the handler waits on a single
// FirebaseClient call. The dispatcher's own loop has no per-event budget,
// so without this a hung Firebase admin call could pin a worker.
const handlerCallTimeout = 10 * time.Second

// UserDeactivatedHandler consumes "user.deactivated" outbox events and
// propagates the deactivation to Firebase Auth via the FirebaseClient
// port.
//
// Behavior:
//   - Empty firebase_uid in payload → no-op (legacy events emitted before
//     this handler existed don't carry the uid).
//   - Firebase user-not-found → log warn, return nil (idempotent: account
//     already gone from Firebase).
//   - errors.Is(err, outbound.ErrFirebaseTransient) → return
//     outboxfb.ErrTransient so the dispatcher retries with backoff.
//   - Other errors → return as-is (permanent failure, dispatcher marks
//     the row failed).
type UserDeactivatedHandler struct {
	firebase outbound.FirebaseClient
}

// NewUserDeactivatedHandler builds the handler over the supplied
// FirebaseClient.
func NewUserDeactivatedHandler(fb outbound.FirebaseClient) *UserDeactivatedHandler {
	return &UserDeactivatedHandler{firebase: fb}
}

// Compile-time check.
var _ outboxfb.Handler = (*UserDeactivatedHandler)(nil)

// EventType returns the routing key for the outbox dispatcher.
func (h *UserDeactivatedHandler) EventType() string { return EventTypeUserDeactivated }

// userDeactivatedPayload is the JSON shape produced by Service.Desactivar.
// Only the field this handler needs is decoded; extra fields are ignored.
type userDeactivatedPayload struct {
	FirebaseUID string `json:"firebase_uid"`
}

// Handle is invoked by the outbox dispatcher for each pending event of
// type EventTypeUserDeactivated. See the type godoc for behavior.
func (h *UserDeactivatedHandler) Handle(ctx context.Context, e outboxfb.Event) error {
	var payload userDeactivatedPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return apperror.NewInternal(
			"user_deactivated_payload_invalid",
			"payload de user.deactivated inválido",
		).WithError(err).WithField("event_id", e.ID.String())
	}

	if payload.FirebaseUID == "" {
		// Legacy events emitted before the firebase_uid field was added,
		// or usuarios that somehow lacked a FirebaseUID at the time of
		// deactivation. No external action is possible — log and move on.
		slog.WarnContext(ctx, "auth.user_deactivated_handler.empty_uid",
			"event_id", e.ID.String(),
			"aggregate_id", e.AggregateID.String())
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, handlerCallTimeout)
	defer cancel()

	if err := h.firebase.DisableUser(callCtx, payload.FirebaseUID); err != nil {
		return h.classifyHandlerError(ctx, err, payload.FirebaseUID, e.ID.String())
	}

	slog.InfoContext(ctx, "auth.firebase_user_disabled",
		"uid", payload.FirebaseUID,
		"event_id", e.ID.String(),
		"attempts", e.Attempts)
	return nil
}

// classifyHandlerError translates FirebaseClient errors into outbox
// semantics. See the type-level godoc for the contract.
func (h *UserDeactivatedHandler) classifyHandlerError(ctx context.Context, err error, uid, eventID string) error {
	if ae, ok := apperror.As(err); ok && ae.Code == "firebase_user_not_found" {
		slog.WarnContext(ctx, "auth.firebase_user_already_gone",
			"uid", uid,
			"event_id", eventID)
		return nil
	}
	if errors.Is(err, outbound.ErrFirebaseTransient) {
		slog.WarnContext(ctx, "auth.firebase_user_disable_transient",
			"uid", uid,
			"event_id", eventID,
			"error", err.Error())
		return outboxfb.ErrTransient
	}
	return err
}
