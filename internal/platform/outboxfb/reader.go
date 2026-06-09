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
) (_ []Event, err error) {
	querier := firebird.GetQuerier(ctx, q)

	const query = `
		SELECT ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD,
		       CREATED_AT, PROCESSED_AT, FAILED_AT, LAST_ERROR, ATTEMPTS
		  FROM MSP_OUTBOX_EVENTS
		 WHERE AGGREGATE_ID = ?
		 ORDER BY CREATED_AT ASC`

	rows, err := querier.QueryContext(ctx, query, aggregateID.String())
	if err != nil {
		return nil, fmt.Errorf("outboxfb: read by aggregate %s: %w", aggregateID, firebird.MapError(err))
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
