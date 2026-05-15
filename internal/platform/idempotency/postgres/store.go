// Package postgres provides the Postgres-backed implementation of
// idempotency.Store.
//
// All values (timestamps, hashes, response payloads) are written explicitly
// from Go — the table has no DB-side defaults (see CLAUDE.md, "No logic in
// the database").
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// Store persists idempotency.Record values in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given pgx pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var _ idempotency.Store = (*Store)(nil)

// Get returns the stored record for the given key, or (nil, nil) if no live
// record exists. Rows whose expires_at has passed are treated as absent so the
// caller re-runs the handler.
func (s *Store) Get(ctx context.Context, key string) (*idempotency.Record, error) {
	const q = `
		SELECT key, method, path, request_hash, response_status, response_body, expires_at
		FROM idempotency_keys
		WHERE key = $1 AND expires_at > $2`
	var rec idempotency.Record
	err := transaction.GetQuerier(ctx, s.pool).QueryRow(ctx, q, key, time.Now()).Scan(
		&rec.Key,
		&rec.Method,
		&rec.Path,
		&rec.RequestHash,
		&rec.ResponseStatus,
		&rec.ResponseBody,
		&rec.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found", matches Store contract
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency: get %q: %w", key, err)
	}
	return &rec, nil
}

// Save stores a record. When a row already exists for the same key the new
// write only overwrites it if the existing row has expired — concurrent
// writers with the same still-valid key cannot trample each other's response.
//
// The middleware only calls Save after Get returned nil for the key, so the
// "row exists and is still valid" branch is effectively a no-op and that is
// the desired outcome (the first writer wins).
func (s *Store) Save(ctx context.Context, rec idempotency.Record) error {
	const q = `
		INSERT INTO idempotency_keys
			(key, method, path, request_hash, response_status, response_body, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (key) DO UPDATE
			SET method          = EXCLUDED.method,
			    path            = EXCLUDED.path,
			    request_hash    = EXCLUDED.request_hash,
			    response_status = EXCLUDED.response_status,
			    response_body   = EXCLUDED.response_body,
			    expires_at      = EXCLUDED.expires_at,
			    created_at      = EXCLUDED.created_at
			WHERE idempotency_keys.expires_at <= $9`
	now := time.Now()
	if _, err := transaction.GetQuerier(ctx, s.pool).Exec(ctx, q,
		rec.Key,
		rec.Method,
		rec.Path,
		rec.RequestHash,
		rec.ResponseStatus,
		[]byte(rec.ResponseBody),
		rec.ExpiresAt,
		now,
		now,
	); err != nil {
		return fmt.Errorf("idempotency: save %q: %w", rec.Key, err)
	}
	return nil
}
