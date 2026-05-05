package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errNoHandler is wrapped when a handler is missing for an event type.
var errNoHandler = errors.New("outbox: no handler registered")

// DispatcherConfig tunes the polling worker.
type DispatcherConfig struct {
	// PollInterval between scans of the outbox table.
	PollInterval time.Duration
	// BatchSize is the maximum number of events claimed per scan.
	BatchSize int
	// MaxAttempts before an event is considered permanently failed.
	MaxAttempts int
	// LockTimeout is the upper bound on `SELECT ... FOR UPDATE SKIP LOCKED`.
	LockTimeout time.Duration
}

// Dispatcher polls the outbox table and routes pending events to handlers.
//
// Concurrency: multiple dispatcher instances can run in parallel — they
// claim disjoint events using `SELECT ... FOR UPDATE SKIP LOCKED`.
type Dispatcher struct {
	pool     *pgxpool.Pool
	registry *HandlerRegistry
	cfg      DispatcherConfig
	cancel   context.CancelFunc
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewDispatcher builds a Dispatcher with sensible defaults applied.
func NewDispatcher(pool *pgxpool.Pool, registry *HandlerRegistry, cfg DispatcherConfig) *Dispatcher {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 25
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = 30 * time.Second
	}
	return &Dispatcher{
		pool:     pool,
		registry: registry,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start launches the dispatcher loop in a goroutine. The context is cloned
// (with cancel) and used as the parent of every tick so that Stop can cancel
// in-flight DB work.
func (d *Dispatcher) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d.cancel = cancel
	go d.run(runCtx)
	return nil
}

// Stop signals the loop to exit and waits up to ctx deadline for it to finish.
func (d *Dispatcher) Stop(ctx context.Context) error {
	close(d.stopCh)
	if d.cancel != nil {
		d.cancel()
	}
	select {
	case <-d.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) run(parent context.Context) {
	defer close(d.doneCh)
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-parent.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(parent, d.cfg.LockTimeout)
			n, err := d.tick(tickCtx)
			cancel()
			if err != nil {
				slog.ErrorContext(parent, "outbox: dispatcher tick failed", "error", err)
				continue
			}
			if n > 0 {
				slog.DebugContext(parent, "outbox: processed batch", "count", n)
			}
		}
	}
}

// tick claims a batch of pending events and processes them one by one.
// Each event is processed in its own short-lived transaction so partial
// failures don't roll back the whole batch.
func (d *Dispatcher) tick(ctx context.Context) (int, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const sel = `
		SELECT id, aggregate, aggregate_id, event_type, payload, created_at, attempts
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND failed_at IS NULL
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`
	rows, err := tx.Query(ctx, sel, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("select pending: %w", err)
	}

	var events []Event
	for rows.Next() {
		var e Event
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Aggregate, &e.AggregateID, &e.EventType, &payload, &e.CreatedAt, &e.Attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows: %w", err)
	}

	processed := 0
	for _, e := range events {
		if err := d.process(ctx, tx, e); err != nil {
			slog.Error("outbox: process failed", "id", e.ID, "type", e.EventType, "error", err)
			continue
		}
		processed++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return processed, nil
}

// process routes the event to its handler and updates its status.
func (d *Dispatcher) process(ctx context.Context, tx pgx.Tx, e Event) error {
	h := d.registry.Lookup(e.EventType)
	if h == nil {
		return d.markFailed(ctx, tx, e, fmt.Errorf("%w: %q", errNoHandler, e.EventType))
	}

	err := h.Handle(ctx, e)
	switch {
	case err == nil:
		return d.markProcessed(ctx, tx, e)
	case errors.Is(err, ErrTransient):
		return d.markRetry(ctx, tx, e, err)
	default:
		return d.markFailed(ctx, tx, e, err)
	}
}

func (d *Dispatcher) markProcessed(ctx context.Context, tx pgx.Tx, e Event) error {
	const q = `UPDATE outbox_events SET processed_at = $2, attempts = $3 WHERE id = $1`
	_, err := tx.Exec(ctx, q, e.ID, time.Now(), e.Attempts+1)
	return err
}

func (d *Dispatcher) markRetry(ctx context.Context, tx pgx.Tx, e Event, cause error) error {
	if e.Attempts+1 >= d.cfg.MaxAttempts {
		return d.markFailed(ctx, tx, e, fmt.Errorf("max attempts exceeded: %w", cause))
	}
	const q = `UPDATE outbox_events SET attempts = $2, error = $3 WHERE id = $1`
	_, err := tx.Exec(ctx, q, e.ID, e.Attempts+1, cause.Error())
	return err
}

func (d *Dispatcher) markFailed(ctx context.Context, tx pgx.Tx, e Event, cause error) error {
	const q = `UPDATE outbox_events SET failed_at = $2, attempts = $3, error = $4 WHERE id = $1`
	_, err := tx.Exec(ctx, q, e.ID, time.Now(), e.Attempts+1, cause.Error())
	return err
}
