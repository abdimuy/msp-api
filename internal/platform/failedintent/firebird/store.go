// Package firebird provides the Firebird-backed implementation of
// failedintent.Store.
//
// All values (timestamps, ids, status strings) are written explicitly from
// Go — the table has no DB-side defaults beyond structural constraints.
// See CLAUDE.md, "No logic in the database".
//
// Key Firebird differences from the Postgres implementation:
//   - FIRST n replaces LIMIT n; FIRST placeholder is the first positional arg.
//   - No RETURNING on DELETE; PurgeOlderThan uses a SELECT-then-DELETE pair
//     inside a single transaction.
//   - No typed-NULL parameter trick ($1::timestamptz IS NULL); optional filter
//     clauses are built dynamically.
//   - BODY_TRUNCATED is CHAR(1) 'S'/'N', not BOOLEAN.
//   - CHAR(36) columns right-pad with spaces; reads must TrimSpace before
//     parsing as UUID.
package firebird

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Store persists failedintent.Intent records in MSP_FAILED_INTENTS.
type Store struct {
	pool *firebird.Pool
}

// New returns a Store backed by the given firebird pool.
func New(pool *firebird.Pool) *Store { return &Store{pool: pool} }

// Verify Store satisfies the failedintent.Store interface at compile time.
var _ failedintent.Store = (*Store)(nil)

// Default and maximum page sizes, mirroring the Postgres implementation.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Save inserts an intent. A primary-key conflict (duplicate ID) is treated as a
// no-op — mirroring the Postgres ON CONFLICT DO NOTHING semantics — so retries
// of a capture cannot corrupt an existing audit row.
func (s *Store) Save(ctx context.Context, i failedintent.Intent) error {
	const q = `
		INSERT INTO MSP_FAILED_INTENTS (
			ID, RECEIVED_AT, METHOD, PATH, FIREBASE_UID, USUARIO_ID,
			IDEMPOTENCY_KEY, REQUEST_ID, BODY, BODY_TRUNCATED,
			BODY_BLOB_PATH, BODY_CONTENT_TYPE,
			HTTP_STATUS, ERROR_CODE, ERROR_MESSAGE,
			RETRY_COUNT, STATUS, RESOLVED_AT, RESOLVED_BY, NOTES
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?
		)`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	_, err := q2.ExecContext(
		ctx, q,
		i.ID.String(),
		firebird.ToWallClock(i.ReceivedAt),
		i.Method,
		i.Path,
		nullableString(i.FirebaseUID),
		nullableUUID(i.UsuarioID),
		nullableString(i.IdempotencyKey),
		i.RequestID.String(),
		[]byte(i.Body),
		truncatedToChar(i.BodyTruncated),
		nullableString(i.BodyBlobPath),
		nullableString(i.BodyContentType),
		i.HTTPStatus,
		i.ErrorCode,
		i.ErrorMessage,
		i.RetryCount,
		string(i.Status),
		nullableTime(i.ResolvedAt),
		nullableUUID(i.ResolvedBy),
		nullableString(i.Notes),
	)
	if err != nil {
		mapped := firebird.MapError(err)
		if appErr, ok := apperror.As(mapped); ok && appErr.Code == "firebird_unique_violation" {
			return nil // PK conflict → no-op
		}
		return fmt.Errorf("failedintent.firebird: save %s: %w", i.ID, mapped)
	}
	return nil
}

// Get loads an intent by id. Returns (nil, nil) when not found.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (*failedintent.Intent, error) {
	const q = `
		SELECT
			ID, RECEIVED_AT, METHOD, PATH,
			COALESCE(FIREBASE_UID, ''), USUARIO_ID,
			COALESCE(IDEMPOTENCY_KEY, ''), REQUEST_ID,
			BODY, BODY_TRUNCATED, HTTP_STATUS,
			ERROR_CODE, ERROR_MESSAGE, RETRY_COUNT, STATUS,
			RESOLVED_AT, RESOLVED_BY, COALESCE(NOTES, ''),
			COALESCE(BODY_BLOB_PATH, ''), COALESCE(BODY_CONTENT_TYPE, '')
		FROM MSP_FAILED_INTENTS
		WHERE ID = ?`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	row := q2.QueryRowContext(ctx, q, id.String())
	intent, err := scanIntent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // contract: (nil, nil) means not found
	}
	if err != nil {
		return nil, fmt.Errorf("failedintent.firebird: get %s: %w", id, err)
	}
	return &intent, nil
}

// List returns a page of intents ordered by RECEIVED_AT DESC, ID DESC.
// Optional filters (Status, UsuarioID) and cursor (CursorReceivedAt, CursorID)
// are built dynamically because Firebird's positional parameters cannot be
// typed-NULL like Postgres's $1::timestamptz IS NULL.
func (s *Store) List(
	ctx context.Context, p failedintent.ListParams,
) (failedintent.Page[failedintent.Intent], error) {
	pageSize := clampPageSize(p.PageSize)

	// FIRST ? must be the very first positional argument.
	args := []any{pageSize + 1}
	var where []string

	if !p.CursorReceivedAt.IsZero() {
		cursorTS := firebird.ToWallClock(p.CursorReceivedAt)
		where = append(where, "(RECEIVED_AT < ? OR (RECEIVED_AT = ? AND ID < ?))")
		args = append(args, cursorTS, cursorTS, p.CursorID.String())
	}
	if p.Status != "" {
		where = append(where, "STATUS = ?")
		args = append(args, string(p.Status))
	}
	if p.UsuarioID != nil {
		where = append(where, "USUARIO_ID = ?")
		args = append(args, p.UsuarioID.String())
	}

	q := buildListQuery(where)
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	rows, err := q2.QueryContext(ctx, q, args...)
	if err != nil {
		return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.firebird: list: %w", firebird.MapError(err))
	}
	defer func() { _ = rows.Close() }()
	return collectPage(rows, pageSize)
}

// buildListQuery assembles the SELECT with optional WHERE clauses. Column names
// are hardcoded so there is no SQL injection risk from the args slice.
func buildListQuery(where []string) string {
	base := `
		SELECT FIRST ?
			ID, RECEIVED_AT, METHOD, PATH,
			COALESCE(FIREBASE_UID, ''), USUARIO_ID,
			COALESCE(IDEMPOTENCY_KEY, ''), REQUEST_ID,
			BODY, BODY_TRUNCATED, HTTP_STATUS,
			ERROR_CODE, ERROR_MESSAGE, RETRY_COUNT, STATUS,
			RESOLVED_AT, RESOLVED_BY, COALESCE(NOTES, ''),
			COALESCE(BODY_BLOB_PATH, ''), COALESCE(BODY_CONTENT_TYPE, '')
		FROM MSP_FAILED_INTENTS`
	if len(where) > 0 {
		base += "\n\t\tWHERE " + strings.Join(where, " AND ")
	}
	base += "\n\t\tORDER BY RECEIVED_AT DESC, ID DESC"
	return base
}

// UpdateStatus moves the intent from expected → next in a single statement.
// Returns apperror.NewConflict("failed_intent_status_conflict", ...) when 0
// rows matched — either the id is gone or the current status differs.
func (s *Store) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	expected, next failedintent.Status,
	resolvedBy uuid.UUID,
	notes string,
	now time.Time,
) error {
	const q = `
		UPDATE MSP_FAILED_INTENTS
		SET STATUS      = ?,
		    RESOLVED_AT = ?,
		    RESOLVED_BY = ?,
		    NOTES       = ?
		WHERE ID = ? AND STATUS = ?`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	res, err := q2.ExecContext(
		ctx, q,
		string(next),
		firebird.ToWallClock(now),
		resolvedBy.String(),
		nullableString(notes),
		id.String(),
		string(expected),
	)
	if err != nil {
		return fmt.Errorf("failedintent.firebird: update status %s: %w", id, firebird.MapError(err))
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperror.NewConflict(
			"failed_intent_status_conflict",
			"el intento fallido ya no se encuentra en el estado esperado",
		).WithError(failedintent.ErrStatusConflict).
			WithField("intent_id", id.String()).
			WithField("expected_status", string(expected))
	}
	return nil
}

// TransitionAfterReplay updates only STATUS. It deliberately does NOT touch
// RESOLVED_AT, RESOLVED_BY or NOTES so the operator-resolution fields stay
// reserved for the operator-driven Resolver endpoint. The replay
// retry_count lives in RETRY_COUNT and is bumped separately by IncrementRetry.
func (s *Store) TransitionAfterReplay(
	ctx context.Context,
	id uuid.UUID,
	expected, next failedintent.Status,
) error {
	const q = `
		UPDATE MSP_FAILED_INTENTS
		SET STATUS = ?
		WHERE ID = ? AND STATUS = ?`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	res, err := q2.ExecContext(
		ctx, q,
		string(next),
		id.String(),
		string(expected),
	)
	if err != nil {
		return fmt.Errorf("failedintent.firebird: transition after replay %s: %w", id, firebird.MapError(err))
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperror.NewConflict(
			"failed_intent_status_conflict",
			"el intento fallido ya no se encuentra en el estado esperado",
		).WithError(failedintent.ErrStatusConflict).
			WithField("intent_id", id.String()).
			WithField("expected_status", string(expected))
	}
	return nil
}

// IncrementRetry bumps RETRY_COUNT by 1 without changing STATUS. Used on each
// replay attempt start so the count reflects attempts, not outcomes.
func (s *Store) IncrementRetry(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE MSP_FAILED_INTENTS SET RETRY_COUNT = RETRY_COUNT + 1 WHERE ID = ?`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	if _, err := q2.ExecContext(ctx, q, id.String()); err != nil {
		return fmt.Errorf("failedintent.firebird: increment retry %s: %w", id, firebird.MapError(err))
	}
	return nil
}

// PurgeOlderThan deletes rows whose RECEIVED_AT is strictly less than before.
// Because Firebird has no RETURNING clause, we perform two statements inside a
// single transaction: first SELECT the paths, then DELETE the rows. The
// transaction prevents new rows from slipping between the two statements.
func (s *Store) PurgeOlderThan(
	ctx context.Context, before time.Time,
) (failedintent.PurgeResult, error) {
	var result failedintent.PurgeResult
	err := firebird.RunInTx(ctx, s.pool.DB, func(ctx context.Context) error {
		q2 := firebird.GetQuerier(ctx, s.pool.DB)
		beforeWC := firebird.ToWallClock(before)

		// Step 1: collect blob paths of rows about to be deleted.
		pathRows, err := q2.QueryContext(
			ctx,
			`SELECT BODY_BLOB_PATH FROM MSP_FAILED_INTENTS WHERE RECEIVED_AT < ? AND BODY_BLOB_PATH IS NOT NULL`,
			beforeWC,
		)
		if err != nil {
			return fmt.Errorf("failedintent.firebird: purge select paths: %w", firebird.MapError(err))
		}
		defer func() { _ = pathRows.Close() }()

		for pathRows.Next() {
			var p sql.NullString
			if scanErr := pathRows.Scan(&p); scanErr != nil {
				return fmt.Errorf("failedintent.firebird: purge scan path: %w", scanErr)
			}
			if p.Valid && p.String != "" {
				result.BlobPaths = append(result.BlobPaths, p.String)
			}
		}
		if rowsErr := pathRows.Err(); rowsErr != nil {
			return fmt.Errorf("failedintent.firebird: purge paths rows: %w", rowsErr)
		}

		// Step 2: delete all matching rows.
		res, err := q2.ExecContext(
			ctx,
			`DELETE FROM MSP_FAILED_INTENTS WHERE RECEIVED_AT < ?`,
			beforeWC,
		)
		if err != nil {
			return fmt.Errorf("failedintent.firebird: purge delete: %w", firebird.MapError(err))
		}
		n, _ := res.RowsAffected()
		result.RowsDeleted = n
		return nil
	})
	if err != nil {
		return failedintent.PurgeResult{}, err
	}
	return result, nil
}

// ReferencedPaths returns every non-NULL BODY_BLOB_PATH currently in the table.
// Used by the boot-time orphan sweep to detect on-disk blob files that have no
// database referent.
func (s *Store) ReferencedPaths(ctx context.Context) ([]string, error) {
	const q = `SELECT BODY_BLOB_PATH FROM MSP_FAILED_INTENTS WHERE BODY_BLOB_PATH IS NOT NULL`
	q2 := firebird.GetQuerier(ctx, s.pool.DB)
	rows, err := q2.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failedintent.firebird: referenced paths: %w", firebird.MapError(err))
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var p sql.NullString
		if scanErr := rows.Scan(&p); scanErr != nil {
			return nil, fmt.Errorf("failedintent.firebird: referenced paths scan: %w", scanErr)
		}
		if p.Valid && p.String != "" {
			paths = append(paths, p.String)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failedintent.firebird: referenced paths rows: %w", rowsErr)
	}
	return paths, nil
}

// --- helpers ------------------------------------------------------------------

// clampPageSize clamps size to [1, maxPageSize], returning defaultPageSize when
// size is zero or negative.
func clampPageSize(size int) int {
	if size <= 0 {
		return defaultPageSize
	}
	if size > maxPageSize {
		return maxPageSize
	}
	return size
}

// truncatedToChar converts a bool to Firebird's CHAR(1) convention for
// BODY_TRUNCATED: true → "S", false → "N".
func truncatedToChar(b bool) string {
	if b {
		return "S"
	}
	return "N"
}

// charToTruncated converts Firebird's CHAR(1) BODY_TRUNCATED to bool.
// Trims whitespace because CHAR columns right-pad with spaces.
func charToTruncated(s string) bool {
	return strings.TrimSpace(s) == "S"
}

// nullableString returns nil when s is empty, otherwise s. This mirrors the
// Postgres NULLIF($N, "") semantics for optional VARCHAR/TEXT columns.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableUUID returns nil when u is nil, otherwise the UUID string.
func nullableUUID(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

// nullableTime returns nil when t is nil, otherwise the wall-clock value.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return firebird.ToWallClock(*t)
}

// rawIntentRow holds the raw column values scanned from a single DB row before
// type normalization. Extracted to keep scanIntent under the funlen limit.
type rawIntentRow struct {
	id            string
	receivedAt    any
	usuarioID     sql.NullString
	requestID     string
	resolvedAt    any
	resolvedBy    sql.NullString
	body          []byte
	truncatedChar string
	statusStr     string
	partial       failedintent.Intent // fields that scan directly into domain types
}

// scanIntent decodes one row (from QueryRowContext or a *sql.Rows scan) into an
// Intent. The caller is responsible for mapping sql.ErrNoRows.
func scanIntent(row interface {
	Scan(dest ...any) error
},
) (failedintent.Intent, error) {
	var r rawIntentRow
	err := row.Scan(
		&r.id,
		&r.receivedAt,
		&r.partial.Method,
		&r.partial.Path,
		&r.partial.FirebaseUID,
		&r.usuarioID,
		&r.partial.IdempotencyKey,
		&r.requestID,
		&r.body,
		&r.truncatedChar,
		&r.partial.HTTPStatus,
		&r.partial.ErrorCode,
		&r.partial.ErrorMessage,
		&r.partial.RetryCount,
		&r.statusStr,
		&r.resolvedAt,
		&r.resolvedBy,
		&r.partial.Notes,
		&r.partial.BodyBlobPath,
		&r.partial.BodyContentType,
	)
	if err != nil {
		return failedintent.Intent{}, err
	}
	return normalizeIntentRow(r)
}

// normalizeIntentRow converts the raw scanned values in r into a fully typed
// failedintent.Intent. Extracted from scanIntent to respect the funlen limit.
func normalizeIntentRow(r rawIntentRow) (failedintent.Intent, error) {
	i := r.partial

	// ID — CHAR(36) right-pads with spaces.
	parsedID, err := uuid.Parse(strings.TrimSpace(r.id))
	if err != nil {
		return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: parse id %q: %w", r.id, err)
	}
	i.ID = parsedID

	// REQUEST_ID — CHAR(36), same treatment.
	parsedReqID, err := uuid.Parse(strings.TrimSpace(r.requestID))
	if err != nil {
		return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: parse request_id %q: %w", r.requestID, err)
	}
	i.RequestID = parsedReqID

	// RECEIVED_AT — TIMESTAMP, convert wall-clock → UTC.
	if r.receivedAt != nil {
		t, scanErr := firebird.ScanUTCTime(r.receivedAt)
		if scanErr != nil {
			return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: scan received_at: %w", scanErr)
		}
		i.ReceivedAt = t
	}

	// USUARIO_ID — nullable CHAR(36).
	if r.usuarioID.Valid {
		uid, parseErr := uuid.Parse(strings.TrimSpace(r.usuarioID.String))
		if parseErr != nil {
			return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: parse usuario_id %q: %w", r.usuarioID.String, parseErr)
		}
		i.UsuarioID = &uid
	}

	// RESOLVED_AT — nullable TIMESTAMP.
	if r.resolvedAt != nil {
		t, scanErr := firebird.ScanUTCTime(r.resolvedAt)
		if scanErr != nil {
			return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: scan resolved_at: %w", scanErr)
		}
		i.ResolvedAt = &t
	}

	// RESOLVED_BY — nullable CHAR(36).
	if r.resolvedBy.Valid {
		rbID, parseErr := uuid.Parse(strings.TrimSpace(r.resolvedBy.String))
		if parseErr != nil {
			return failedintent.Intent{}, fmt.Errorf("failedintent.firebird: parse resolved_by %q: %w", r.resolvedBy.String, parseErr)
		}
		i.ResolvedBy = &rbID
	}

	i.Body = r.body
	i.BodyTruncated = charToTruncated(r.truncatedChar)
	i.Status = failedintent.Status(r.statusStr)

	return i, nil
}

// collectPage drains a *sql.Rows result set and applies the +1 page-size trick
// to detect a next page without an extra COUNT(*) query.
func collectPage(rows *sql.Rows, pageSize int) (failedintent.Page[failedintent.Intent], error) {
	out := make([]failedintent.Intent, 0, pageSize+1)
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.firebird: scan: %w", err)
		}
		out = append(out, intent)
	}
	if err := rows.Err(); err != nil {
		return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.firebird: rows: %w", err)
	}

	page := failedintent.Page[failedintent.Intent]{Items: out}
	if len(out) > pageSize {
		page.Items = out[:pageSize]
		page.HasMore = true
		last := out[pageSize-1]
		page.NextReceivedAt = last.ReceivedAt
		page.NextID = last.ID
	}
	return page, nil
}
