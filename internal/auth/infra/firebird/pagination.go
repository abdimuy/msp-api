package firebird

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ErrInvalidCursor is the apperror sentinel returned when decodeCursor cannot
// parse the supplied opaque cursor string. It is package-private because
// callers only ever surface the wrapped error.
var errInvalidCursor = apperror.NewValidation(
	"invalid_cursor",
	"cursor inválido",
)

// cursorSep is the field separator inside the decoded cursor payload.
const cursorSep = "|"

// minPageSize / maxPageSize bound the page size accepted by every List repo
// method. Callers may ask for any size but the repo clamps the request to
// these bounds.
const (
	minPageSize     = 1
	maxPageSize     = 100
	defaultPageSize = 25
)

// clampPageSize returns the requested size constrained to the [min, max]
// range, falling back to default when the caller passes zero.
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

// encodeCursor encodes the ordering tuple (created_at, id) into an opaque
// base64-url string suitable for an outbound.Page.NextCursor value.
//
// The wire payload is "<rfc3339nano>|<uuid>" with both fields URL-safe
// base64 encoded so they may be passed through HTTP query strings unchanged.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + cursorSep + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// paginatedItem is the minimal surface a List item must satisfy to take
// part in the shared cursor-paginated query helper: it must surface the
// ordering tuple (created_at, id) used to build the next cursor.
type paginatedItem interface {
	CreatedAt() time.Time
	ID() uuid.UUID
}

// queryPage runs a cursor-paginated SELECT against a Firebird querier and
// turns the resulting rows into an outbound.Page[T]. It fetches pageSize+1
// rows so a single extra row signals "more pages remain"; that extra row's
// (created_at, id) becomes the next cursor.
//
// scanRow rebuilds one T from a row scanner. firstPageQuery is used on the
// first page (no cursor); afterCursorQuery handles every subsequent page —
// its placeholders must accept four arguments in this order: page limit,
// cursor.created_at, cursor.created_at (again), cursor.id — matching the
// "(CREATED_AT > ?) OR (CREATED_AT = ? AND ID > ?)" SQL pattern.
func queryPage[T paginatedItem](
	ctx context.Context,
	pool *firebird.Pool,
	params outbound.ListParams,
	firstPageQuery, afterCursorQuery string,
	scanRow func(rowScanner) (T, error),
) (outbound.Page[T], error) {
	size := clampPageSize(params.PageSize)
	curT, curID, err := decodeCursor(params.Cursor)
	if err != nil {
		return outbound.Page[T]{}, err
	}

	q := firebird.GetQuerier(ctx, pool.DB)
	var rows *sql.Rows
	if params.Cursor == "" {
		rows, err = q.QueryContext(ctx, firstPageQuery, size+1)
	} else {
		// The nakagami/firebirdsql driver formats the time.Time parameter
		// using its wall-clock fields without converting to UTC, while
		// ScanUTCTime normalizes the entity's CreatedAt to UTC. Convert back
		// to time.Local so the wire wall-clock matches what's stored in
		// Firebird; otherwise the equality branch of the cursor predicate
		// never matches.
		t := curT.In(time.Local)
		rows, err = q.QueryContext(ctx, afterCursorQuery, size+1, t, t, curID.String())
	}
	if err != nil {
		return outbound.Page[T]{}, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]T, 0, size)
	for rows.Next() {
		item, scanErr := scanRow(rows)
		if scanErr != nil {
			return outbound.Page[T]{}, firebird.MapError(scanErr)
		}
		items = append(items, item)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return outbound.Page[T]{}, firebird.MapError(rowsErr)
	}

	var next string
	if len(items) > size {
		last := items[size-1]
		next = encodeCursor(last.CreatedAt(), last.ID())
		items = items[:size]
	}
	return outbound.Page[T]{Items: items, NextCursor: next}, nil
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
