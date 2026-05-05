// Package pagination provides cursor-based pagination helpers.
//
// Cursor format: base64url-encoded JSON. Opaque to clients. Servers
// encode the value of the ordering column(s) of the last row returned;
// clients pass it back unchanged in the next request.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	errInvalidLimit  = errors.New("pagination: invalid limit")
	errLimitRange    = errors.New("pagination: limit out of range")
	errInvalidCursor = errors.New("pagination: invalid cursor")
)

const (
	// DefaultLimit is the page size when none is supplied.
	DefaultLimit = 50
	// MaxLimit caps the page size accepted from clients.
	MaxLimit = 500
	// MinLimit is the smallest accepted page size.
	MinLimit = 1
)

// Params is the parsed pagination input from a request.
type Params struct {
	After string // raw cursor (opaque to caller)
	Limit int
}

// FromRequest parses ?after=<cursor>&limit=<int> with bounded defaults.
func FromRequest(r *http.Request) (Params, error) {
	q := r.URL.Query()
	p := Params{After: q.Get("after"), Limit: DefaultLimit}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Params{}, fmt.Errorf("%w: %q", errInvalidLimit, raw)
		}
		if n < MinLimit || n > MaxLimit {
			return Params{}, fmt.Errorf("%w: %d not in [%d,%d]", errLimitRange, n, MinLimit, MaxLimit)
		}
		p.Limit = n
	}
	return p, nil
}

// Page is a generic paginated result.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"` // empty when no more pages
	HasMore    bool   `json:"has_more"`
}

// Cursor wraps the ordering values that identify the last row returned.
// Stored fields are versioned via Schema so the server can reject cursors
// from incompatible clients/migrations.
type Cursor struct {
	Schema    int        `json:"v"`
	UpdatedAt time.Time  `json:"u,omitempty"`
	ID        *uuid.UUID `json:"i,omitempty"`
	Offset    *int       `json:"o,omitempty"` // for offset-based fallback
}

// EncodeCursor returns the base64url-encoded JSON of c.
func EncodeCursor(c Cursor) (string, error) {
	if c.Schema == 0 {
		c.Schema = 1
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("pagination: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeCursor decodes the opaque string back into a Cursor.
// Empty input yields the zero Cursor and no error.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %w", errInvalidCursor, err)
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return Cursor{}, fmt.Errorf("%w: %w", errInvalidCursor, err)
	}
	return c, nil
}
