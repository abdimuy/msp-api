# Task 1 — Migration 000040: narrativa cache + queue tables

## Where this fits
Fase 2 adds an LLM-generated "analyst reading" + AI traits to the analytics module, cached in Firebird. This task creates ONLY the two tables. No Go code consumes them yet — later tasks do.

## Requirements (use these exact names/types verbatim)

Create FOUR files under `migrations-firebird/`:
- `000040_create_msp_an_narrativa.up.sql`
- `000040_create_msp_an_narrativa.down.sql`

(Confirm the exact filename suffix convention by listing `migrations-firebird/` — match how `000039_*` is named: `000039_<slug>.up.sql` / `.down.sql`. Use slug `create_msp_an_narrativa`.)

### Table 1: `MSP_AN_CLIENTE_NARRATIVA`
One row per client (the materialized narrativa cache).
- `ID CHAR(36) CHARACTER SET ASCII NOT NULL` — PRIMARY KEY (UUID generated in Go; NO default)
- `CLIENTE_ID INTEGER NOT NULL` — UNIQUE constraint
- `NARRATIVA BLOB SUB_TYPE TEXT CHARACTER SET UTF8` — the analyst paragraph
- `RASGOS BLOB SUB_TYPE TEXT CHARACTER SET UTF8` — JSON array of validated trait codes
- `INPUT_HASH CHAR(64) CHARACTER SET ASCII NOT NULL` — sha256 hex of the facts (invalidation key)
- `MODELO VARCHAR(64) CHARACTER SET UTF8` — model id that generated it
- `GENERADA_EN TIMESTAMP NOT NULL` — when generated (Go time)
- `CREATED_AT TIMESTAMP NOT NULL`
- `UPDATED_AT TIMESTAMP NOT NULL`

### Table 2: `MSP_AN_NARRATIVA_PENDIENTE`
The bounded generation queue (idempotent — PK is CLIENTE_ID so re-enqueue is a no-op upsert).
- `CLIENTE_ID INTEGER NOT NULL` — PRIMARY KEY
- `INPUT_HASH CHAR(64) CHARACTER SET ASCII NOT NULL`
- `ENCOLADA_EN TIMESTAMP NOT NULL`

## Hard constraints (CLAUDE.md §1 — NO logic in DB)
- NO `DEFAULT` on any column (no `DEFAULT CURRENT_TIMESTAMP`, no UUID default). All values come from Go.
- NO triggers, NO stored procedures, NO sequences/generators, NO business-rule CHECK constraints on these MSP_* tables.
- Allowed: PRIMARY KEY, UNIQUE, NOT NULL, indexes, column types.
- UUID columns are `CHAR(36) CHARACTER SET ASCII`. JSON payloads are `BLOB SUB_TYPE TEXT CHARACTER SET UTF8`. Timestamps are `TIMESTAMP` (no default).

## Style — match existing migrations exactly
READ first (verbatim templates): `docs/superpowers/plans/anchor-points.md` (Migrations section), and the actual files `migrations-firebird/000039_*.up.sql` and `.down.sql`, and the header of `migrations-firebird/000035_*.up.sql`.
- Reproduce the Spanish `Por qué:` rationale comment header block (explain why these two tables exist: materialized LLM narrativa cache + bounded pending queue, invalidated by INPUT_HASH).
- Reproduce the EXACT trailer: `INSERT INTO MSP_MIGRATIONS (...) VALUES (...);` then `COMMIT;` — copy the column list and value format from 000039's trailer; use the next migration NAME consistent with how 000039 names itself.
- The `.down.sql` drops both tables (in safe order) and removes/handles the MSP_MIGRATIONS row consistently with how `000039_*.down.sql` does it. Match that file's exact down style.
- Add reasonable indexes if 000039 does (e.g. none needed beyond PK/UNIQUE here — only add if the convention shows it; do not over-add).

## Verification
- The migration is plain SQL; there is no Go to build for this task. Confirm the SQL is well-formed and self-consistent.
- If a `make` target or script validates/applies migrations against the local Firebird (`make test-firebird-all` requires `FB_DATABASE`), DO NOT run it (it needs the dev DB and is run later as integration). Just ensure syntax matches the established files exactly.
- Double-check: no DEFAULT, no trigger, no procedure anywhere; every timestamp column has NO default; UUID PK is CHAR(36) ASCII.

## Report
Write your full report to `docs/superpowers/plans/task-1-report.md` (files created, the exact column DDL, how you matched the 000039 header/trailer, confirmation of the no-DB-logic checklist). Return only: status, the commit hash, a one-line summary, and any concerns.

## Commit
Commit on the current branch with a conventional message: `feat(analytics): migración 000040 tablas de narrativa IA y cola pendiente`. Do NOT use --no-verify. (Pre-commit hooks run lint/build on Go; pure-SQL changes should pass.) Omit any Claude attribution footer in the commit message.
