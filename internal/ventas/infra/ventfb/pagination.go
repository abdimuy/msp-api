package ventfb

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// errInvalidCursor is the apperror sentinel surfaced when decodeCursor cannot
// parse the supplied opaque cursor.
var errInvalidCursor = apperror.NewValidation(
	"invalid_cursor",
	"cursor inválido",
)

// cursorSep is the field separator inside the decoded cursor payload.
const cursorSep = "|"

// Page-size bounds applied by clampPageSize.
const (
	minPageSize     = 1
	maxPageSize     = 100
	defaultPageSize = 25
)

// clampPageSize constrains the requested page size to the [min, max] range,
// falling back to default when the caller passes zero.
func clampPageSize(requested int) int {
	if requested <= 0 {
		return defaultPageSize
	}
	if requested < minPageSize {
		return minPageSize
	}
	if requested > maxPageSize {
		return maxPageSize
	}
	return requested
}

// encodeCursor encodes the (fecha_venta, id) ordering tuple into an opaque
// base64-url string suitable for an outbound.Page.NextCursor value.
//
// The wire payload is "<rfc3339nano>|<uuid>" base64-url encoded so it may be
// passed through HTTP query strings unchanged.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + cursorSep + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor reverses encodeCursor. An empty cursor is interpreted as "no
// cursor — first page" and returns zero values with a nil error. A malformed
// cursor returns the invalid_cursor apperror.
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, errInvalidCursor.WithError(err)
	}
	parts := strings.SplitN(string(decoded), cursorSep, 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errInvalidCursor.WithField("reason", "missing separator")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, errInvalidCursor.WithError(err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, errInvalidCursor.WithError(err)
	}
	return t.UTC(), id, nil
}
