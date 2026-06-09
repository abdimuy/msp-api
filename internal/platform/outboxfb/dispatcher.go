package outboxfb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// errNoHandlerRegistered is wrapped when no handler is registered for an event type.
var errNoHandlerRegistered = errors.New("outboxfb: no handler registered")

// DispatcherConfig tunes the polling worker. Zero values are replaced with the
// defaults listed in NewDispatcher.
type DispatcherConfig struct {
	// PollInterval between scans of the outbox table. Default: 2s.
	PollInterval time.Duration
	// BatchSize is the maximum number of events fetched per scan. Default: 25.
	BatchSize int
	// MaxAttempts before an event is considered permanently failed. Default: 10.
	MaxAttempts int
	// TickTimeout is the upper bound on one tick's transaction. Default: 30s.
	TickTimeout time.Duration
}

// Dispatcher polls MSP_OUTBOX_EVENTS and routes pending events to handlers.
//
// The dispatcher runs a single goroutine — no SELECT FOR UPDATE SKIP LOCKED is
// needed because Firebird does not support it and a single worker is sufficient
// for the expected throughput.
type Dispatcher struct {
	pool      *firebird.Pool
	registry  Registry
	cfg       DispatcherConfig
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewDispatcher builds a Dispatcher with sensible defaults applied to any
// zero-value fields in cfg.
func NewDispatcher(pool *firebird.Pool, registry Registry, cfg DispatcherConfig) *Dispatcher {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 25
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.TickTimeout <= 0 {
		cfg.TickTimeout = 30 * time.Second
	}
	return &Dispatcher{
		pool:     pool,
		registry: registry,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start launches the dispatcher loop in a goroutine. The parent context is
// detached via context.WithoutCancel so that the loop is not cancelled by a
// parent that goes away during the normal request lifecycle; the internal
// cancel func is then used by Stop to terminate the loop.
//
// Start always returns nil. Calling Start more than once is a no-op after the
// first call.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		d.cancel = cancel
		go d.run(runCtx)
	})
	return nil
}

// Stop signals the loop to exit and waits for it to finish. The passed-in ctx
// sets the deadline for the wait; if the ctx expires before the loop drains,
// Stop returns ctx.Err(). Stop is idempotent: subsequent calls after the first
// skip the signal and proceed directly to waiting on doneCh. Calling Stop on a
// Dispatcher that was never started returns nil immediately.
//
// Any in-flight tick is allowed to drain fully before the goroutine exits.
func (d *Dispatcher) Stop(ctx context.Context) error {
	d.stopOnce.Do(func() {
		close(d.stopCh)
		if d.cancel == nil {
			// Start was never called: close doneCh so waiters unblock immediately.
			close(d.doneCh)
		}
		// When Start was called, doneCh is closed by the run goroutine after the
		// in-flight tick finishes. We intentionally do NOT call d.cancel() here so
		// the current tick's context is not interrupted — the spec requires inflight
		// work to complete gracefully. The run goroutine exits on <-d.stopCh once
		// the tick returns.
	})
	select {
	case <-d.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the dispatcher loop. It owns doneCh and closes it when it exits.
// Each tick runs inside its own context derived from a background parent so
// that Stop closing stopCh does not interrupt an inflight tick.
func (d *Dispatcher) run(_ context.Context) {
	defer close(d.doneCh)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			//nolint:contextcheck // intentional: each tick runs on its own context so Stop does not interrupt in-flight work
			tickCtx, cancel := context.WithTimeout(context.Background(), d.cfg.TickTimeout)
			n, err := d.tick(tickCtx) //nolint:contextcheck // same: derived from the fresh background ctx above
			cancel()
			if err != nil {
				slog.Error("outboxfb: dispatcher tick failed", "error", err)
				continue
			}
			if n > 0 {
				slog.Debug("outboxfb: processed batch", "count", n)
			}
		}
	}
}

// tick claims a batch of pending events and processes them inside a single
// Firebird transaction. The batch is committed atomically; if the commit fails
// the individual mark* updates are lost and the rows remain pending for the
// next tick.
func (d *Dispatcher) tick(ctx context.Context) (int, error) {
	processed := 0
	err := firebird.RunInTx(ctx, d.pool.DB, func(ctx context.Context) error {
		events, err := d.fetchPending(ctx)
		if err != nil {
			return fmt.Errorf("fetch pending: %w", err)
		}

		for _, e := range events {
			if procErr := d.process(ctx, e); procErr != nil {
				slog.ErrorContext(
					ctx, "outboxfb: process failed",
					"id", e.ID,
					"type", e.EventType,
					"error", procErr,
				)
				continue
			}
			processed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}

// pendingQuery builds the SELECT query and its arguments for fetching pending
// events. It uses Firebird's FIRST clause (not ROWS) for compatibility with
// the driver's parameterized query handling.
//
// The query always includes an EVENT_TYPE IN (...) filter built from the
// registry's known types. fetchPending guarantees this is never called with an
// empty type set.
func (d *Dispatcher) pendingQuery() (string, []any) {
	knownTypes := d.registry.KnownTypes()
	// Build a comma-separated list of placeholders: ?,?,?
	placeholders := make([]byte, 0, len(knownTypes)*2)
	for i := range knownTypes {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	sel := fmt.Sprintf(`
		SELECT FIRST ? ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD, CREATED_AT, ATTEMPTS
		  FROM MSP_OUTBOX_EVENTS
		 WHERE PROCESSED_AT IS NULL
		   AND FAILED_AT IS NULL
		   AND EVENT_TYPE IN (%s)
		 ORDER BY CREATED_AT ASC`, string(placeholders))
	// FIRST ? is the first placeholder — it must be the first argument.
	args := make([]any, 0, len(knownTypes)+1)
	args = append(args, d.cfg.BatchSize)
	for _, t := range knownTypes {
		args = append(args, t)
	}
	return sel, args
}

// fetchPending queries MSP_OUTBOX_EVENTS for the next batch of pending rows.
// CHAR(36) padding is trimmed on the returned events. Returns nil when the
// registry has no handlers (nothing to claim).
func (d *Dispatcher) fetchPending(ctx context.Context) (_ []Event, err error) {
	if len(d.registry.KnownTypes()) == 0 {
		return nil, nil
	}

	q := firebird.GetQuerier(ctx, d.pool.DB)

	sel, args := d.pendingQuery()
	rows, err := q.QueryContext(ctx, sel, args...)
	if err != nil {
		return nil, fmt.Errorf("select pending: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close rows: %w", closeErr)
		}
	}()

	var events []Event
	for rows.Next() {
		var (
			rawID          string
			rawAggregate   string
			rawAggregateID string
			rawEventType   string
			payload        []byte
			rawCreatedAt   any
			attempts       int
		)
		if err := rows.Scan(
			&rawID,
			&rawAggregate,
			&rawAggregateID,
			&rawEventType,
			&payload,
			&rawCreatedAt,
			&attempts,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		id, err := uuid.Parse(strings.TrimSpace(rawID))
		if err != nil {
			return nil, fmt.Errorf("parse id %q: %w", rawID, err)
		}
		aggregateID, err := uuid.Parse(strings.TrimSpace(rawAggregateID))
		if err != nil {
			return nil, fmt.Errorf("parse aggregate_id %q: %w", rawAggregateID, err)
		}
		createdAt, err := firebird.ScanUTCTime(rawCreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan created_at for %s: %w", id, err)
		}

		events = append(events, Event{
			ID:          id,
			Aggregate:   strings.TrimSpace(rawAggregate),
			AggregateID: aggregateID,
			EventType:   strings.TrimSpace(rawEventType),
			Payload:     json.RawMessage(payload),
			CreatedAt:   createdAt,
			Attempts:    attempts,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return events, nil
}

// process routes one event to its handler and updates its status row.
func (d *Dispatcher) process(ctx context.Context, e Event) error {
	h := d.registry.Lookup(e.EventType)
	if h == nil {
		return d.markFailed(ctx, e, fmt.Errorf("%w for %q", errNoHandlerRegistered, e.EventType))
	}

	err := h.Handle(ctx, e)
	switch {
	case err == nil:
		return d.markProcessed(ctx, e)
	case errors.Is(err, ErrTransient):
		return d.markRetry(ctx, e, err)
	default:
		return d.markFailed(ctx, e, err)
	}
}

func (d *Dispatcher) markProcessed(ctx context.Context, e Event) error {
	q := firebird.GetQuerier(ctx, d.pool.DB)
	const upd = `UPDATE MSP_OUTBOX_EVENTS SET PROCESSED_AT = ?, ATTEMPTS = ? WHERE ID = ?`
	_, err := q.ExecContext(
		ctx, upd,
		firebird.ToWallClock(time.Now()),
		e.Attempts+1,
		e.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("mark processed %s: %w", e.ID, err)
	}
	return nil
}

func (d *Dispatcher) markRetry(ctx context.Context, e Event, cause error) error {
	if e.Attempts+1 >= d.cfg.MaxAttempts {
		return d.markFailed(ctx, e, fmt.Errorf("max attempts exceeded: %w", cause))
	}
	q := firebird.GetQuerier(ctx, d.pool.DB)
	const upd = `UPDATE MSP_OUTBOX_EVENTS SET ATTEMPTS = ?, LAST_ERROR = ? WHERE ID = ?`
	_, err := q.ExecContext(
		ctx, upd,
		e.Attempts+1,
		cause.Error(),
		e.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("mark retry %s: %w", e.ID, err)
	}
	return nil
}

func (d *Dispatcher) markFailed(ctx context.Context, e Event, cause error) error {
	q := firebird.GetQuerier(ctx, d.pool.DB)
	const upd = `UPDATE MSP_OUTBOX_EVENTS SET FAILED_AT = ?, ATTEMPTS = ?, LAST_ERROR = ? WHERE ID = ?`
	_, err := q.ExecContext(
		ctx, upd,
		firebird.ToWallClock(time.Now()),
		e.Attempts+1,
		cause.Error(),
		e.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("mark failed %s: %w", e.ID, err)
	}
	return nil
}

// Ensure Dispatcher satisfies the lifecycle.Hooks interface at compile time.
var _ interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
} = (*Dispatcher)(nil)
