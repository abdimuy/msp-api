// Package failedintenthttp provides the admin HTTP transport for the
// failedintent platform package: handlers, DTOs, and the chi router mount.
package failedintenthttp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// errCursorFormat is the sentinel returned by decodeCursor when the decoded
// payload does not split into the expected two parts.
var errCursorFormat = errors.New("decodeCursor: unexpected format")

// IntentDTO is the JSON projection of a failedintent.Intent.
type IntentDTO struct {
	ID             string  `json:"id"`
	ReceivedAt     string  `json:"received_at"`
	Method         string  `json:"method"`
	Path           string  `json:"path"`
	FirebaseUID    string  `json:"firebase_uid,omitempty"`
	UsuarioID      *string `json:"usuario_id,omitempty"`
	IdempotencyKey string  `json:"idempotency_key,omitempty"`
	RequestID      string  `json:"request_id"`
	Body           any     `json:"body"`
	BodyTruncated  bool    `json:"body_truncated"`
	HTTPStatus     int     `json:"http_status"`
	ErrorCode      string  `json:"error_code,omitempty"`
	ErrorMessage   string  `json:"error_message,omitempty"`
	RetryCount     int     `json:"retry_count"`
	Status         string  `json:"status"`
	ResolvedAt     *string `json:"resolved_at,omitempty"`
	ResolvedBy     *string `json:"resolved_by,omitempty"`
	Notes          string  `json:"notes,omitempty"`
}

// ListResponse is the cursor-paginated envelope returned by the list endpoint.
// NextCursor is the empty string when there are no more pages.
type ListResponse struct {
	Items      []IntentDTO `json:"items"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
}

// ResolveRequest is the body accepted by PATCH /{id}/resolve.
type ResolveRequest struct {
	Status string `json:"status"`
	Notes  string `json:"notes"`
}

// ReplayResponse is the body returned by POST /{id}/replay and POST /{id}/replay-with.
type ReplayResponse struct {
	Outcome           string `json:"outcome"`
	ReplayHTTPStatus  int    `json:"replay_http_status"`
	ReplayBodyPreview string `json:"replay_body_preview"`
}

// ReplayWithRequest is the body accepted by POST /{id}/replay-with.
// Body is the corrected request payload to use instead of the captured one.
type ReplayWithRequest struct {
	Body json.RawMessage `json:"body"`
}

// intentToDTO maps a domain Intent to its JSON projection.
func intentToDTO(i failedintent.Intent) IntentDTO {
	dto := IntentDTO{
		ID:             i.ID.String(),
		ReceivedAt:     i.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Method:         i.Method,
		Path:           i.Path,
		FirebaseUID:    i.FirebaseUID,
		RequestID:      i.RequestID.String(),
		BodyTruncated:  i.BodyTruncated,
		HTTPStatus:     i.HTTPStatus,
		ErrorCode:      i.ErrorCode,
		ErrorMessage:   i.ErrorMessage,
		RetryCount:     i.RetryCount,
		Status:         string(i.Status),
		Notes:          i.Notes,
		IdempotencyKey: i.IdempotencyKey,
	}

	// Body is already json.RawMessage; embed as-is so it stays a JSON object
	// rather than a base64 string. If null/invalid, keep it as null.
	if len(i.Body) > 0 {
		dto.Body = i.Body
	}

	if i.UsuarioID != nil {
		s := i.UsuarioID.String()
		dto.UsuarioID = &s
	}
	if i.ResolvedAt != nil {
		s := i.ResolvedAt.UTC().Format(time.RFC3339Nano)
		dto.ResolvedAt = &s
	}
	if i.ResolvedBy != nil {
		s := i.ResolvedBy.String()
		dto.ResolvedBy = &s
	}
	return dto
}

// encodeCursor produces a base64url (no padding) cursor from the two pagination
// fields. The format is "<received_at_RFC3339Nano>|<id_uuid>".
func encodeCursor(receivedAt time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%s|%s", receivedAt.UTC().Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a base64url cursor. An empty string is a valid sentinel
// meaning "start from the newest row" and returns zero values without an error.
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.UUID{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: base64: %w", err)
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.UUID{}, errCursorFormat
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: uuid: %w", err)
	}
	return t, id, nil
}
