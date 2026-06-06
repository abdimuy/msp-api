// Package postgres provides the Postgres-backed implementation of
// failedintent.Store.
//
// All values (timestamps, ids, status strings) are written explicitly from
// Go — the table only has structural DEFAULTs (status='new', retry_count=0)
// for defence in depth; Save still passes both columns. See CLAUDE.md, "No
// logic in the database".
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// Store persists failedintent.Intent records in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var _ failedintent.Store = (*Store)(nil)

// Default page size when ListParams.PageSize is unset or out of range.
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Save inserts an intent. The primary-key conflict is treated as a no-op so
// retries of a capture (e.g. when CaptureMiddleware fires twice due to a
// hand-off bug) cannot corrupt an existing audit row.
func (s *Store) Save(ctx context.Context, i failedintent.Intent) error {
	const q = `
		INSERT INTO failed_intents (
			id, received_at, method, path, firebase_uid, usuario_id,
			idempotency_key, request_id, body, body_truncated, http_status,
			error_code, error_message, retry_count, status, resolved_at,
			resolved_by, notes, body_blob_path, body_content_type
		)
		VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), $6,
			NULLIF($7, ''), $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, NULLIF($19, ''), NULLIF($20, '')
		)
		ON CONFLICT (id) DO NOTHING`
	if _, err := transaction.GetQuerier(ctx, s.pool).Exec(ctx, q,
		i.ID,
		i.ReceivedAt.UTC(),
		i.Method,
		i.Path,
		i.FirebaseUID,
		i.UsuarioID,
		i.IdempotencyKey,
		i.RequestID,
		[]byte(i.Body),
		i.BodyTruncated,
		i.HTTPStatus,
		i.ErrorCode,
		i.ErrorMessage,
		i.RetryCount,
		string(i.Status),
		nullTime(i.ResolvedAt),
		i.ResolvedBy,
		i.Notes,
		i.BodyBlobPath,
		i.BodyContentType,
	); err != nil {
		return fmt.Errorf("failedintent.postgres: save %s: %w", i.ID, err)
	}
	return nil
}

// Get loads an intent by id. Returns (nil, nil) on miss to match the
// failedintent.Store contract.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (*failedintent.Intent, error) {
	const q = `
		SELECT
			id, received_at, method, path,
			COALESCE(firebase_uid, ''), usuario_id,
			COALESCE(idempotency_key, ''), request_id,
			body, body_truncated, http_status,
			error_code, error_message, retry_count, status,
			resolved_at, resolved_by, COALESCE(notes, ''),
			COALESCE(body_blob_path, ''), COALESCE(body_content_type, '')
		FROM failed_intents
		WHERE id = $1`
	row := transaction.GetQuerier(ctx, s.pool).QueryRow(ctx, q, id)
	intent, err := scanIntent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // contract: (nil, nil) means not found
	}
	if err != nil {
		return nil, fmt.Errorf("failedintent.postgres: get %s: %w", id, err)
	}
	return &intent, nil
}

// List returns a page of intents ordered by received_at DESC, id DESC.
// Cursor is encoded as (CursorReceivedAt, CursorID); the page boundary is
// strictly less-than so cursors are stable under concurrent inserts.
func (s *Store) List(
	ctx context.Context, p failedintent.ListParams,
) (failedintent.Page[failedintent.Intent], error) {
	pageSize := clampPageSize(p.PageSize)
	const q = `
		SELECT
			id, received_at, method, path,
			COALESCE(firebase_uid, ''), usuario_id,
			COALESCE(idempotency_key, ''), request_id,
			body, body_truncated, http_status,
			error_code, error_message, retry_count, status,
			resolved_at, resolved_by, COALESCE(notes, ''),
			COALESCE(body_blob_path, ''), COALESCE(body_content_type, '')
		FROM failed_intents
		WHERE ($1::timestamptz IS NULL
		       OR received_at < $1
		       OR (received_at = $1 AND id < $2))
		  AND ($3::text IS NULL OR status = $3)
		  AND ($4::uuid IS NULL OR usuario_id = $4)
		ORDER BY received_at DESC, id DESC
		LIMIT $5`
	rows, err := transaction.GetQuerier(ctx, s.pool).Query(ctx, q,
		nullableTime(p.CursorReceivedAt),
		p.CursorID,
		nullableStatus(p.Status),
		nullableUUID(p.UsuarioID),
		pageSize+1,
	)
	if err != nil {
		return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.postgres: list: %w", err)
	}
	defer rows.Close()
	return collectPage(rows, pageSize)
}

// UpdateStatus moves the intent from expected→next in a single statement.
// Returns apperror.NewConflict("failed_intent_status_conflict", ...) when no
// row matched — either because the id is gone or the current status differs.
func (s *Store) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	expected, next failedintent.Status,
	resolvedBy uuid.UUID,
	notes string,
	now time.Time,
) error {
	const q = `
		UPDATE failed_intents
		SET status = $2,
		    resolved_at = $3,
		    resolved_by = $4,
		    notes = NULLIF($5, '')
		WHERE id = $1 AND status = $6`
	tag, err := transaction.GetQuerier(ctx, s.pool).Exec(ctx, q,
		id, string(next), now.UTC(), resolvedBy, notes, string(expected),
	)
	if err != nil {
		return fmt.Errorf("failedintent.postgres: update status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewConflict(
			"failed_intent_status_conflict",
			"el intento fallido ya no se encuentra en el estado esperado",
		).WithError(failedintent.ErrStatusConflict).
			WithField("intent_id", id.String()).
			WithField("expected_status", string(expected))
	}
	return nil
}

// IncrementRetry bumps retry_count by 1.
func (s *Store) IncrementRetry(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE failed_intents SET retry_count = retry_count + 1 WHERE id = $1`
	if _, err := transaction.GetQuerier(ctx, s.pool).Exec(ctx, q, id); err != nil {
		return fmt.Errorf("failedintent.postgres: increment retry %s: %w", id, err)
	}
	return nil
}

// PurgeOlderThan deletes rows with received_at < before. The DELETE uses
// RETURNING body_blob_path so the caller gets both the row count and the
// list of on-disk blob paths to clean up in a single statement — no
// post-hoc SELECT, no chance of skew between DB and filesystem.
func (s *Store) PurgeOlderThan(
	ctx context.Context, before time.Time,
) (failedintent.PurgeResult, error) {
	const q = `DELETE FROM failed_intents WHERE received_at < $1 RETURNING body_blob_path`
	rows, err := transaction.GetQuerier(ctx, s.pool).Query(ctx, q, before.UTC())
	if err != nil {
		return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge: %w", err)
	}
	defer rows.Close()

	var result failedintent.PurgeResult
	for rows.Next() {
		var path *string
		if scanErr := rows.Scan(&path); scanErr != nil {
			return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge scan: %w", scanErr)
		}
		result.RowsDeleted++
		if path != nil && *path != "" {
			result.BlobPaths = append(result.BlobPaths, *path)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge rows: %w", rowsErr)
	}
	return result, nil
}

// ReferencedPaths returns every non-NULL body_blob_path currently in the
// table. Used by the boot-time orphan sweep.
func (s *Store) ReferencedPaths(ctx context.Context) ([]string, error) {
	const q = `SELECT body_blob_path FROM failed_intents WHERE body_blob_path IS NOT NULL`
	rows, err := transaction.GetQuerier(ctx, s.pool).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failedintent.postgres: referenced paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if scanErr := rows.Scan(&p); scanErr != nil {
			return nil, fmt.Errorf("failedintent.postgres: referenced paths scan: %w", scanErr)
		}
		if p != "" {
			paths = append(paths, p)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failedintent.postgres: referenced paths rows: %w", rowsErr)
	}
	return paths, nil
}

// --- helpers ---------------------------------------------------------------

func clampPageSize(size int) int {
	if size <= 0 {
		return defaultPageSize
	}
	if size > maxPageSize {
		return maxPageSize
	}
	return size
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func nullableStatus(s failedintent.Status) any {
	if s == "" {
		return nil
	}
	return string(s)
}

func nullableUUID(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return *u
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// scanIntent decodes a single row into an Intent.
func scanIntent(row pgx.Row) (failedintent.Intent, error) {
	var (
		i        failedintent.Intent
		statusS  string
		body     []byte
		resolved *time.Time
	)
	err := row.Scan(
		&i.ID,
		&i.ReceivedAt,
		&i.Method,
		&i.Path,
		&i.FirebaseUID,
		&i.UsuarioID,
		&i.IdempotencyKey,
		&i.RequestID,
		&body,
		&i.BodyTruncated,
		&i.HTTPStatus,
		&i.ErrorCode,
		&i.ErrorMessage,
		&i.RetryCount,
		&statusS,
		&resolved,
		&i.ResolvedBy,
		&i.Notes,
		&i.BodyBlobPath,
		&i.BodyContentType,
	)
	if err != nil {
		return failedintent.Intent{}, err
	}
	i.Body = body
	i.Status = failedintent.Status(statusS)
	if resolved != nil {
		ut := resolved.UTC()
		i.ResolvedAt = &ut
	}
	return i, nil
}

// collectPage drains rows and applies the +1 page-size trick to detect a
// next page without an extra COUNT(*).
func collectPage(
	rows pgx.Rows, pageSize int,
) (failedintent.Page[failedintent.Intent], error) {
	out := make([]failedintent.Intent, 0, pageSize+1)
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.postgres: scan: %w", err)
		}
		out = append(out, intent)
	}
	if err := rows.Err(); err != nil {
		return failedintent.Page[failedintent.Intent]{}, fmt.Errorf("failedintent.postgres: rows: %w", err)
	}

	page := failedintent.Page[failedintent.Intent]{Items: out, HasMore: false}
	if len(out) > pageSize {
		page.Items = out[:pageSize]
		page.HasMore = true
		last := out[pageSize-1]
		page.NextReceivedAt = last.ReceivedAt
		page.NextID = last.ID
	}
	return page, nil
}
