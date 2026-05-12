// Package firebird provides the application's Firebird connection pool wrapper.
//
// It wraps [database/sql] with the [github.com/nakagami/firebirdsql] driver and
// adds OpenTelemetry tracing and metrics via [github.com/XSAM/otelsql].
//
// # Role in the architecture
//
// Firebird (Microsip) is the authoritative source of truth for business data.
// Postgres holds read-model projections derived from Firebird. Code in this
// package talks directly to Firebird; Postgres projections are handled by the
// sibling [internal/platform/postgres] package.
//
// # Charset
//
// The default charset is UTF8. The Firebird server transcodes on the wire; Go
// strings remain UTF-8 native throughout the application. Override with the
// FB_CHARSET environment variable only for legacy server installations that
// cannot transcode (e.g. set FB_CHARSET=WIN1252).
//
// # Public API
//
//   - [Pool] — *sql.DB wrapper with Start / Stop / HealthCheck lifecycle hooks.
//   - [TxManager] — runs a function inside a Firebird transaction without
//     exposing *sql.Tx to domain code. [TxManager.RunInTx] uses READ COMMITTED;
//     [TxManager.RunInTxNoWait] adds NOWAIT lock semantics for hot paths.
//   - [GetQuerier] / [HasTx] / [RequireTx] / [InjectTx] — context helpers repos
//     and tests use to access or plant the active *sql.Tx.
//   - [MapError] — translates raw *firebirdsql.FbError values into typed
//     [internal/platform/apperror.Error] values.
//   - [IsTransient] — reports whether an error can usefully be retried.
//   - [Pool.ExecRetry] — runs a write with the default retry policy, retrying
//     only on transient errors.
//
// # Error mapping
//
// GDS code to apperror.Kind mapping:
//
//	335544665 (UNIQUE/PK violation)    → KindConflict   "firebird_unique_violation"
//	335544349 (unique index duplicate) → KindConflict   "firebird_unique_violation"
//	335544466 (FK violation)           → KindConflict   "firebird_fk_violation"
//	335544558 (CHECK constraint)       → KindValidation "firebird_check_failed"
//	335544336 (deadlock)               → KindConflict   "firebird_lock_conflict"  (transient)
//	335544345 (lock conflict no-wait)  → KindConflict   "firebird_lock_conflict"  (transient)
//	335544510 (lock timeout)           → KindConflict   "firebird_lock_conflict"  (transient)
//	335544344 (I/O error)              → KindInternal   "firebird_io_error"       (transient)
//	335544648 (connection lost – pipe) → KindInternal   "firebird_connection_lost"(transient)
//	335544741 (connection lost – DB)   → KindInternal   "firebird_connection_lost"(transient)
//	default                            → KindInternal   "firebird_error"
package firebird
