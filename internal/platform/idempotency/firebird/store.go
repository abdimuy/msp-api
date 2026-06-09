// Package firebird provides the Firebird-backed implementation of
// idempotency.Store.
//
// All values (timestamps, hashes, response payloads) are written explicitly
// from Go — the table has no DB-side defaults (see CLAUDE.md, "No logic in
// the database").
//
// Column naming: the table column is IDEM_KEY (not KEY) because KEY is a
// Firebird reserved word. The Go side still uses rec.Key per the Store contract.
package firebird

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// Store persists idempotency.Record values in MSP_IDEMPOTENCY_KEYS.
type Store struct {
	pool *firebird.Pool
}

// New returns a Store backed by the given firebird pool.
func New(pool *firebird.Pool) *Store { return &Store{pool: pool} }

// Verify Store satisfies the idempotency.Store interface at compile time.
var _ idempotency.Store = (*Store)(nil)

const getQuery = `
	SELECT IDEM_KEY, METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS, RESPONSE_BODY, EXPIRES_AT
	  FROM MSP_IDEMPOTENCY_KEYS
	 WHERE IDEM_KEY = ? AND EXPIRES_AT > ?`

// replaceExpiredQuery updates an existing row only when its EXPIRES_AT is in
// the past (i.e. the row is expired). Returns RowsAffected == 0 when no
// expired row exists for the key.
const replaceExpiredQuery = `
	UPDATE MSP_IDEMPOTENCY_KEYS
	   SET METHOD          = ?,
	       PATH            = ?,
	       REQUEST_HASH    = ?,
	       RESPONSE_STATUS = ?,
	       RESPONSE_BODY   = ?,
	       CREATED_AT      = ?,
	       EXPIRES_AT      = ?
	 WHERE IDEM_KEY = ? AND EXPIRES_AT <= ?`

const insertQuery = `
	INSERT INTO MSP_IDEMPOTENCY_KEYS
		(IDEM_KEY, METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS, RESPONSE_BODY, CREATED_AT, EXPIRES_AT)
	VALUES
		(?, ?, ?, ?, ?, ?, ?, ?)`

const purgeQuery = `DELETE FROM MSP_IDEMPOTENCY_KEYS WHERE EXPIRES_AT <= ?`

// Get returns the stored record for the given key, or (nil, nil) if no live
// record exists. Rows whose EXPIRES_AT has passed are treated as absent so the
// middleware re-runs the handler.
func (s *Store) Get(ctx context.Context, key string) (*idempotency.Record, error) {
	q := firebird.GetQuerier(ctx, s.pool.DB)
	now := firebird.ToWallClock(time.Now())

	row := q.QueryRowContext(ctx, getQuery, key, now)

	var rec idempotency.Record
	var rawExpiresAt any
	var responseBody []byte

	err := row.Scan(
		&rec.Key,
		&rec.Method,
		&rec.Path,
		&rec.RequestHash,
		&rec.ResponseStatus,
		&responseBody,
		&rawExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found", matches Store contract
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency: get %q: %w", key, firebird.MapError(err))
	}

	expiresAt, err := firebird.ScanUTCTime(rawExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("idempotency: get %q: scan expires_at: %w", key, err)
	}

	rec.ResponseBody = responseBody
	rec.ExpiresAt = expiresAt
	return &rec, nil
}

// Save stores a record with first-writer-wins semantics for unexpired rows.
//
// The sequence inside a single transaction:
//  1. Attempt UPDATE … WHERE IDEM_KEY = ? AND EXPIRES_AT <= ? (replace expired).
//  2. If nothing was updated, attempt INSERT.
//  3. If INSERT fails with a PK/unique violation the row exists and is not
//     expired — first writer already owns it, so we return nil silently.
//
// The whole sequence runs in a firebird.RunInTx call. When an ambient test
// transaction is already in ctx (fbtestutil.WithTestTransaction), RunInTx is
// re-entrant and the operations join the outer tx, rolling back together.
//
// CLAUDE.md §1 reminder: expiry is evaluated in Go (EXPIRES_AT <= now passed as
// a parameter), not via a DB-side expression.
func (s *Store) Save(ctx context.Context, rec idempotency.Record) error {
	return firebird.RunInTx(ctx, s.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, s.pool.DB)
		now := time.Now()
		nowWC := firebird.ToWallClock(now)
		expiresWC := firebird.ToWallClock(rec.ExpiresAt)

		// Step 1: replace an expired row if one exists.
		// Param order: METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS, RESPONSE_BODY,
		// CREATED_AT (now), EXPIRES_AT, then WHERE IDEM_KEY = ?, EXPIRES_AT <= now.
		res, err := q.ExecContext(
			ctx, replaceExpiredQuery,
			rec.Method,
			rec.Path,
			rec.RequestHash,
			rec.ResponseStatus,
			[]byte(rec.ResponseBody),
			nowWC,
			expiresWC,
			rec.Key,
			nowWC,
		)
		if err != nil {
			return fmt.Errorf("idempotency: reemplazar caducada %q: %w", rec.Key, firebird.MapError(err))
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			return nil // expired row replaced; done
		}

		// Step 2: no expired row present — attempt a fresh INSERT.
		_, err = q.ExecContext(
			ctx, insertQuery,
			rec.Key,
			rec.Method,
			rec.Path,
			rec.RequestHash,
			rec.ResponseStatus,
			[]byte(rec.ResponseBody),
			nowWC,
			expiresWC,
		)
		if err != nil {
			mapped := firebird.MapError(err)
			// A PK/unique violation means a still-valid row already exists.
			// First writer wins — treat this as success.
			if appErr, ok := apperror.As(mapped); ok &&
				(appErr.Code == "firebird_unique_violation") {
				return nil
			}
			return fmt.Errorf("idempotency: guardar %q: %w", rec.Key, mapped)
		}
		return nil
	})
}

// PurgeExpired deletes rows whose EXPIRES_AT is at or before now. It returns
// the number of rows deleted. Used by the Janitor.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	q := firebird.GetQuerier(ctx, s.pool.DB)
	res, err := q.ExecContext(ctx, purgeQuery, firebird.ToWallClock(now))
	if err != nil {
		return 0, fmt.Errorf("idempotency: purgar caducadas: %w", firebird.MapError(err))
	}
	n, _ := res.RowsAffected()
	return n, nil
}
