# Task 7 — Firebird adapter: NarrativaRepo implementation

## Where this fits
Implements the `outbound.NarrativaRepo` interface (Task 6) against the two tables created in migration 000040 (Task 1): `MSP_AN_CLIENTE_NARRATIVA` (cache) and `MSP_AN_NARRATIVA_PENDIENTE` (queue). All IDs/timestamps are produced in Go (CLAUDE.md §1). Lives in `internal/analytics/infra/analyticsfb/`.

## Read first (verbatim patterns)
- `docs/superpowers/plans/anchor-points.md` → "Firebird Repo (infra/analyticsfb)" section.
- The existing `internal/analytics/infra/analyticsfb/` repo files: the `Repo` struct + `NewRepo(pool *firebird.Pool) *Repo` constructor, how it gets a querier (`firebird.GetQuerier(ctx, pool.DB)` — respects an injected test transaction), how writes use `firebird.ToWallClock(t)` and reads use `firebird.ScanUTCTime`, and how an existing upsert is structured. Match these EXACTLY.
- `internal/analytics/ports/outbound/narrativa_repo.go` (Task 6) — the interface + `NarrativaRow`/`PendienteRow` types you implement.
- `internal/analytics/domain/narrativa.go` (Task 5) — the `domain.Narrativa` you persist.
- Migration `migrations-firebird/000040_*.up.sql` (Task 1) — exact column names/types.

## What to build
Add the methods to the existing `analyticsfb.Repo` (or a focused new file `internal/analytics/infra/analyticsfb/narrativa_repo.go` in the same package, matching how the package splits files). The `Repo` must satisfy `outbound.NarrativaRepo` — add a compile-time assertion `var _ outbound.NarrativaRepo = (*Repo)(nil)` if the package uses that idiom.

### Methods
1. **`GetNarrativa(ctx, clienteID) (*outbound.NarrativaRow, error)`** — SELECT NARRATIVA, RASGOS, INPUT_HASH, MODELO FROM MSP_AN_CLIENTE_NARRATIVA WHERE CLIENTE_ID = ?. If no row → return `(nil, nil)` (not an error). Deserialize the `RASGOS` BLOB (JSON array of strings) into `[]string`; tolerate NULL/empty → empty slice. Read the BLOB text safely (follow how the package reads BLOB SUB_TYPE TEXT columns elsewhere; if none exists, scan into a `sql.NullString`/`[]byte` then `json.Unmarshal`).
2. **`UpsertNarrativa(ctx, n domain.Narrativa) error`** — one row per `CLIENTE_ID` (UNIQUE). Use **UPDATE-then-INSERT** (NOT MERGE — the firebirdsql driver v0.9.19 has a `-804` bug binding params in `MERGE USING (SELECT ?)`; see project memory). Algorithm:
   - Serialize `n.Rasgos` to JSON (`json.Marshal`; nil/empty → `"[]"`).
   - `now := time.Now().UTC()`; bind timestamps via `firebird.ToWallClock`.
   - Try `UPDATE MSP_AN_CLIENTE_NARRATIVA SET NARRATIVA=?, RASGOS=?, INPUT_HASH=?, MODELO=?, GENERADA_EN=?, UPDATED_AT=? WHERE CLIENTE_ID=?`. If `RowsAffected()==0`, `INSERT` a new row with `ID = uuid.New().String()`, `CLIENTE_ID`, the same columns, plus `CREATED_AT` and `UPDATED_AT` both = now. Use `n.GeneradaEn` (wall-clock) for `GENERADA_EN`.
   - All timestamps from Go; no SQL functions.
3. **`Encolar(ctx, clienteID, inputHash) error`** — idempotent enqueue into `MSP_AN_NARRATIVA_PENDIENTE` (PK CLIENTE_ID). UPDATE-then-INSERT: `UPDATE ... SET INPUT_HASH=?, ENCOLADA_EN=? WHERE CLIENTE_ID=?`; if 0 rows, INSERT (CLIENTE_ID, INPUT_HASH, ENCOLADA_EN=ToWallClock(now)). Re-enqueuing an already-queued client just refreshes its hash/time — no duplicate, no error.
4. **`ListarPendientes(ctx, limit) ([]outbound.PendienteRow, error)`** — `SELECT FIRST ? CLIENTE_ID, INPUT_HASH FROM MSP_AN_NARRATIVA_PENDIENTE ORDER BY ENCOLADA_EN` (Firebird `FIRST n` syntax — match how the codebase does limited selects). Return rows (possibly empty).
5. **`BorrarPendiente(ctx, clienteID) error`** — `DELETE FROM MSP_AN_NARRATIVA_PENDIENTE WHERE CLIENTE_ID=?`.

## Constraints
- CLAUDE.md §1: every INSERT passes ID/CREATED_AT/UPDATED_AT (and GENERADA_EN/ENCOLADA_EN) explicitly from Go; no DB defaults/functions.
- UUIDs via `uuid.New().String()`; timestamps `time.Now().UTC()` wrapped with `firebird.ToWallClock` on write; `firebird.ScanUTCTime` (or the package's read helper) on read.
- Encoding: MSP_* columns are UTF8; store Spanish text as-is (NFC); do NOT use `firebird.Win1252`/`EncodeWin1252` (those are for legacy Microsip tables only).
- Use the package's querier accessor so `fbtestutil.WithTestTransaction` injects its tx.
- CLAUDE.md §3: English identifiers/comments; any user-facing error message Spanish (these repo errors are internal — wrap with English context like existing repo methods do).

## Tests — Firebird integration (`internal/analytics/infra/analyticsfb/narrativa_repo_test.go`)
Use `fbtestutil.NewTestFirebirdPool(t)` (skips cleanly when `FB_DATABASE` is unset) + `fbtestutil.WithTestTransaction(t, pool, func(ctx){...})` (rollback-only). Study `internal/analytics/infra/analyticsfb/repo_test.go` for the EXACT harness: how it gets the pool, the negative-clienteID convention to avoid prod collisions, and especially its `Skipf("... migration 000035 may not be applied ...")` pattern.

**Critical — graceful skip when migration 000040 is not yet applied:** the dev DB will NOT have the new tables when these tests first run. At the start of each integration test (inside the tx), probe the table (e.g. a `GetNarrativa` for a negative id, or a `SELECT FIRST 1 ... FROM MSP_AN_CLIENTE_NARRATIVA`) and on a "table unknown"/error, `t.Skipf("MSP_AN_CLIENTE_NARRATIVA missing — migration 000040 may not be applied: %v", err)` — mirroring exactly how repo_test.go skips on missing 000035. This keeps the suite green in skip-mode now; the controller applies 000040 and runs them for real at a later checkpoint.

Cover:
- **Get miss** → `(nil, nil)`.
- **Upsert insert then Get** → round-trips Texto, Rasgos (incl. a multi-element slice and an empty slice), InputHash, Modelo.
- **Upsert update** (same CLIENTE_ID twice) → second call updates in place, still ONE row, new values returned, no duplicate-key error.
- **Encolar idempotent** → enqueue same client twice → ListarPendientes returns it once with the latest hash.
- **ListarPendientes limit + order** → enqueue several, `limit` caps the result.
- **BorrarPendiente** → after delete, ListarPendientes no longer returns it.
Do NOT leave persistent rows (the rollback tx handles it; never use raw committed INSERTs — project rule). Keep output pristine. If `FB_DATABASE` is unset the test should skip cleanly like the existing ones.

## Verification
- `go build ./...`
- `golangci-lint run ./internal/analytics/...`
- `go test ./internal/analytics/infra/analyticsfb/...` — run with `FB_DATABASE` UNSET so the new integration tests SKIP cleanly (confirm they skip, not fail). Do NOT set FB_DATABASE, do NOT apply migrations, do NOT touch the shared dev DB — the controller runs the real Firebird integration at a later snapshotted checkpoint. Report the skip output as evidence.

## Commit
`feat(analytics): repositorio Firebird de narrativa IA y cola pendiente`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-7-report.md` (include whether the Firebird integration tests actually ran or skipped, and why). Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
