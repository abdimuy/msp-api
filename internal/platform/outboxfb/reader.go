package outboxfb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ReadByAggregateID returns every event recorded for the given aggregate,
// ordered by CREATED_AT ascending (oldest first). This is the read side of
// the outbox — distinct from Enqueue (write) and the dispatcher's internal
// fetchPending. It exists so modules can surface an entity's event timeline
// to operators (e.g. the venta detail screen's "Historial").
//
// q is the fallback Querier (the pool); the function prefers
// firebird.GetQuerier(ctx, q) so it joins an ambient tx when one exists
// (tests) and otherwise reads through the pool. Callers that want a clean
// read transaction should wrap the call in firebird.RunInReadTx.
//
// The returned events carry the dispatcher status fields (ProcessedAt,
// FailedAt, LastError, Attempts) so admin views can show delivery health;
// operator-facing views can ignore them.
func ReadByAggregateID(
	ctx context.Context,
	q firebird.Querier,
	aggregateID uuid.UUID,
) ([]Event, error) {
	const query = `
		SELECT ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD,
		       CREATED_AT, PROCESSED_AT, FAILED_AT, LAST_ERROR, ATTEMPTS
		  FROM MSP_OUTBOX_EVENTS
		 WHERE AGGREGATE_ID = ?
		 ORDER BY CREATED_AT ASC`

	events, err := queryEvents(ctx, q, query, aggregateID.String())
	if err != nil {
		return nil, fmt.Errorf("outboxfb: read by aggregate %s: %w", aggregateID, err)
	}
	return events, nil
}

// ReadByAggregateAndPayloadContaining returns events for the given AGGREGATE
// whose PAYLOAD contains needle, oldest first. It complements
// ReadByAggregateID for the case where an entity's timeline must also include
// events emitted by a *different* aggregate that merely reference the entity
// inside their JSON payload — e.g. a traspaso event (AGGREGATE='traspaso')
// carrying the venta_id it was created for. The match uses Firebird's
// CONTAINING (case-insensitive substring), so callers should pass a
// discriminating fragment such as `"venta_id":"<uuid>"` to avoid accidental
// hits. Returns an empty slice (not an error) when nothing matches.
func ReadByAggregateAndPayloadContaining(
	ctx context.Context,
	q firebird.Querier,
	aggregate, needle string,
) ([]Event, error) {
	const query = `
		SELECT ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD,
		       CREATED_AT, PROCESSED_AT, FAILED_AT, LAST_ERROR, ATTEMPTS
		  FROM MSP_OUTBOX_EVENTS
		 WHERE AGGREGATE = ?
		   AND PAYLOAD CONTAINING ?
		 ORDER BY CREATED_AT ASC`

	events, err := queryEvents(ctx, q, query, aggregate, needle)
	if err != nil {
		return nil, fmt.Errorf("outboxfb: read aggregate %q by payload: %w", aggregate, err)
	}
	return events, nil
}

// queryEvents runs an outbox SELECT (the canonical column projection above)
// and decodes every row into an Event. It prefers the ambient tx querier so
// reads join an open transaction when one exists.
func queryEvents(
	ctx context.Context,
	q firebird.Querier,
	query string,
	args ...any,
) (_ []Event, err error) {
	querier := firebird.GetQuerier(ctx, q)

	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("outboxfb: close rows: %w", closeErr)
		}
	}()

	var events []Event
	for rows.Next() {
		e, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, e)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("outboxfb: read rows: %w", rowsErr)
	}
	return events, nil
}

// rowScanner is the subset of *sql.Rows / *sql.Row that scanEvent needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEvent decodes one MSP_OUTBOX_EVENTS row into an Event. CHAR(36)
// columns are right-padded by Firebird, so the UUID columns are trimmed
// before parsing; the nullable timestamp + last_error columns are decoded
// into pointers.
func scanEvent(row rowScanner) (Event, error) {
	var (
		rawID          string
		rawAggregate   string
		rawAggregateID string
		rawEventType   string
		payload        []byte
		rawCreatedAt   any
		rawProcessedAt any
		rawFailedAt    any
		lastError      *string
		attempts       int
	)
	if err := row.Scan(
		&rawID,
		&rawAggregate,
		&rawAggregateID,
		&rawEventType,
		&payload,
		&rawCreatedAt,
		&rawProcessedAt,
		&rawFailedAt,
		&lastError,
		&attempts,
	); err != nil {
		return Event{}, fmt.Errorf("outboxfb: scan event: %w", err)
	}

	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return Event{}, fmt.Errorf("outboxfb: parse id %q: %w", rawID, err)
	}
	aggregateID, err := uuid.Parse(strings.TrimSpace(rawAggregateID))
	if err != nil {
		return Event{}, fmt.Errorf("outboxfb: parse aggregate_id %q: %w", rawAggregateID, err)
	}
	createdAt, err := firebird.ScanUTCTime(rawCreatedAt)
	if err != nil {
		return Event{}, fmt.Errorf("outboxfb: scan created_at for %s: %w", id, err)
	}

	e := Event{
		ID:          id,
		Aggregate:   strings.TrimSpace(rawAggregate),
		AggregateID: aggregateID,
		EventType:   strings.TrimSpace(rawEventType),
		Payload:     json.RawMessage(payload),
		CreatedAt:   createdAt,
		LastError:   lastError,
		Attempts:    attempts,
	}
	if processedAt, ok := optionalTime(rawProcessedAt); ok {
		e.ProcessedAt = &processedAt
	}
	if failedAt, ok := optionalTime(rawFailedAt); ok {
		e.FailedAt = &failedAt
	}
	return e, nil
}

// optionalTime decodes a nullable TIMESTAMP column. A nil src (SQL NULL)
// returns ok=false; otherwise the wall-clock value is converted to UTC.
func optionalTime(src any) (time.Time, bool) {
	if src == nil {
		return time.Time{}, false
	}
	t, err := firebird.ScanUTCTime(src)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
