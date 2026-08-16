# Task 7 Report — Firebird adapter: NarrativaRepo

## Status
DONE. All hooks passed. Integration tests SKIPPED (FB_DATABASE unset, as required).

## Commit
`50c49e4  feat(analytics): repositorio Firebird de narrativa IA y cola pendiente`

## Files changed
| File | Change |
|------|--------|
| `internal/analytics/infra/analyticsfb/narrativa_repo.go` | New — 5 methods implementing `outbound.NarrativaRepo` on `*Repo` |
| `internal/analytics/infra/analyticsfb/queries.go` | Added SQL constants for all 7 queries (selectNarrativa, updateNarrativa, insertNarrativa, updatePendiente, insertPendiente, deletePendiente) |
| `internal/analytics/infra/analyticsfb/narrativa_repo_test.go` | New — 8 integration tests with migration-missing skip guard |

## Implementation

### Methods
1. **GetNarrativa** — SELECT NARRATIVA, RASGOS, INPUT_HASH, MODELO; `sql.ErrNoRows` → `(nil, nil)` (nolint:nilnil per port contract). RASGOS BLOB scanned into `sql.NullString` then `json.Unmarshal`; null/empty → empty slice.
2. **UpsertNarrativa** — UPDATE-then-INSERT (MERGE avoided per -804 driver bug). Rasgos marshaled to JSON; nil/empty → `"[]"`. All timestamps from `time.Now().UTC()` + `firebird.ToWallClock`.
3. **Encolar** — UPDATE-then-INSERT on `MSP_AN_NARRATIVA_PENDIENTE`. Re-enqueue refreshes INPUT_HASH + ENCOLADA_EN; no duplicate, no error.
4. **ListarPendientes** — `SELECT FIRST ? CLIENTE_ID, INPUT_HASH ... ORDER BY ENCOLADA_EN` (Firebird `FIRST n` syntax, limit injected via `fmt.Sprintf`).
5. **BorrarPendiente** — `DELETE FROM MSP_AN_NARRATIVA_PENDIENTE WHERE CLIENTE_ID = ?`. No-op when absent.

### Compile-time assertion
`var _ outbound.NarrativaRepo = (*Repo)(nil)` in `narrativa_repo.go` (plus one in the test file).

## Tests
8 integration tests in `narrativa_repo_test.go`:
- `TestNarrativaRepo_GetNarrativa_Miss`
- `TestNarrativaRepo_UpsertInsert_RoundTrip` (multi-element + empty Rasgos)
- `TestNarrativaRepo_UpsertUpdate_InPlace`
- `TestNarrativaRepo_Encolar_Idempotent`
- `TestNarrativaRepo_ListarPendientes_LimitAndOrder`
- `TestNarrativaRepo_BorrarPendiente`
- `TestNarrativaRepo_ListarPendientes_Empty`
- `TestNarrativaRepo_UpsertNarrativa_NilRasgos`

### Skip behavior (FB_DATABASE unset)
All 8 tests SKIPPED cleanly via `requireFBEnv(t)` (the existing package helper). Each test additionally calls `skipIfMigration000040Missing(ctx, t, repo)` inside the transaction to skip when migration 000040 is not yet applied — mirroring how `repo_test.go` skips on missing 000035.

```
--- SKIP: TestNarrativaRepo_GetNarrativa_Miss (0.00s)
--- SKIP: TestNarrativaRepo_UpsertInsert_RoundTrip (0.00s)
--- SKIP: TestNarrativaRepo_UpsertUpdate_InPlace (0.00s)
--- SKIP: TestNarrativaRepo_Encolar_Idempotent (0.00s)
--- SKIP: TestNarrativaRepo_ListarPendientes_LimitAndOrder (0.00s)
--- SKIP: TestNarrativaRepo_BorrarPendiente (0.00s)
--- SKIP: TestNarrativaRepo_ListarPendientes_Empty (0.00s)
--- SKIP: TestNarrativaRepo_UpsertNarrativa_NilRasgos (0.00s)
PASS
ok  github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb  0.435s
```

## Verification
- `go build ./...` — clean (0 errors)
- `golangci-lint run ./internal/analytics/...` — 0 issues
- `go test ./internal/analytics/infra/analyticsfb/...` (FB_DATABASE unset) — 8 new tests SKIP, 3 unit tests PASS, suite exits ok

## Concerns
None. The `isMissingTableError` substring-match helper is intentionally simple — it covers Firebird's "Table unknown" message and the -204 code string. If the error message ever changes, the test will fail rather than skip, which is the safer fallback.
