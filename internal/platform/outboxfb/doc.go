// Package outboxfb implements the transactional outbox pattern backed by
// Firebird (MSP_OUTBOX_EVENTS), replacing the Postgres [outbox] package per
// ADR-0008.
//
// # Motivation
//
// Postgres outbox_events lives in a different database from the Firebird
// business data. That means every state change requires a two-phase commit or
// an accept-and-then-enqueue strategy with a gap window where the two stores
// can diverge. outboxfb closes that gap: the INSERT into MSP_OUTBOX_EVENTS
// runs inside the same [firebird.TxManager.RunInTx] transaction as the
// business write. The COMMIT covers both atomically — no distributed
// transaction, no gap.
//
// # Usage
//
// Call [Enqueue] from any application service that already holds an open
// Firebird transaction. Enqueue checks [firebird.HasTx] and returns an error
// if no ambient transaction is found — callers that invoke it outside a tx
// lose the dual-write guarantee and must not do so.
//
// # Dispatcher
//
// The dispatcher that reads MSP_OUTBOX_EVENTS and calls registered [Handler]
// implementations is introduced in a separate commit (ADR-0008, phase 2). This
// package only defines the enqueueing side and the handler contract
// ([Handler], [HandlerRegistry], [ErrTransient]).
//
// # Thread safety
//
// [HandlerRegistry] is populated at startup and read-only during request
// handling. [Enqueue] is safe for concurrent use.
package outboxfb
