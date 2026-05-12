// Package authoutbox is the auth module's adapter to the platform outbox.
//
// Per docs/adr/0001-outbox-strategy.md the auth module's enqueuer is
// BEST-EFFORT: the business write commits first to Firebird and the outbox
// row is appended afterwards to Postgres in its own transaction. When the
// Postgres write fails the failure is logged with the full payload so the
// event can be replayed from log archives, and Enqueue returns nil so the
// caller's success path is not perturbed.
package authoutbox

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/outbox"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// txRunner is the subset of transaction.Manager the Enqueuer depends on.
// Exposing it as an interface lets tests inject a fake runner that simulates
// transaction-open failures without needing a live database.
type txRunner interface {
	RunInTx(ctx context.Context, fn func(context.Context) error) error
}

// Enqueuer wraps the platform outbox so the auth module can drop events on
// the queue without owning a *pgxpool.Pool reference directly.
type Enqueuer struct {
	runner txRunner
}

// NewEnqueuer builds an Enqueuer that uses the supplied Postgres transaction
// manager. The manager opens its own tx for every Enqueue call so the outbox
// write is durable even when the business operation that triggered it had to
// commit elsewhere (Firebird).
func NewEnqueuer(txMgr *transaction.Manager) *Enqueuer {
	return &Enqueuer{runner: txMgr}
}

// newEnqueuerWithRunner is the test-only constructor that injects a stub
// transaction runner. Kept unexported so production callers stay on the
// concrete *transaction.Manager path.
func newEnqueuerWithRunner(r txRunner) *Enqueuer {
	return &Enqueuer{runner: r}
}

// Compile-time check: Enqueuer satisfies the outbound port.
var _ outbound.OutboxEnqueuer = (*Enqueuer)(nil)

// Enqueue opens a Postgres transaction via the runner, inserts the event row
// through platformoutbox.Enqueue, and commits. On any failure the payload is
// serialized into a structured log entry and nil is returned so the caller's
// success path is preserved (best-effort contract).
func (e *Enqueuer) Enqueue(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	err := e.runner.RunInTx(ctx, func(ctx context.Context) error {
		return outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload)
	})
	if err != nil {
		body, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			body = []byte(`"<unmarshalable>"`)
		}
		slog.ErrorContext(ctx, "auth.outbox_enqueue_failed",
			"aggregate", aggregate,
			"aggregate_id", aggregateID.String(),
			"event_type", eventType,
			"payload", string(body),
			"error", err,
		)
		return nil
	}
	return nil
}
