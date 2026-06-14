# Analytics Winback Bulk Write Scale Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the per-row UPDATE+INSERT loop in `UpsertCandidatos` with a batched `EXECUTE BLOCK` approach (chunked 200-row blocks per call) so that a full 43,399-row winback refresh completes its write phase in well under 30s instead of >120s.

**Architecture:** The current `upsertOne` re-prepares two SQL statements on every row and sends two round-trips per candidate (~86k total round-trips for 43k rows). The optimized path splits the candidatos into chunks of `chunkSize` (e.g. 200), and for each chunk generates a single `EXECUTE BLOCK` statement that does the UPDATE-then-INSERT logic for all rows in the chunk — one round-trip per 200 rows (~220 total round-trips for 43k rows). The `EN_CONTROL`/`COHORTE_FECHA` preserve-flags invariant is maintained: the WHEN-MATCHED branch inside the EXECUTE BLOCK only updates mutable fields, never EN_CONTROL or COHORTE_FECHA.

**Tech Stack:** Go 1.23, `nakagami/firebirdsql`, `database/sql`, `firebird.GetQuerier`/`firebird.RequireTx`, Firebird 5.0 (EXECUTE BLOCK with typed input parameters)

---

## File Map

| File | Change |
|------|--------|
| `internal/analytics/infra/analyticsfb/repo.go` | Replace `upsertOne` loop with `upsertBatch` using chunked `EXECUTE BLOCK` |
| `internal/analytics/infra/analyticsfb/queries.go` | Add `executeBlockUpsert` builder function (or inline in repo.go) |
| `internal/analytics/infra/analyticsfb/repo_test.go` | Add perf benchmark test + preserve-flags invariant test for batched path |

---

## Context for the executor

### What the current code does

`UpsertCandidatos` in `repo.go` loops over candidates and calls `upsertOne` per candidate:
1. `ExecContext(ctx, updateCandidato, ...11 args...)` — attempts UPDATE
2. Checks `RowsAffected` — if 0, calls `ExecContext(ctx, insertCandidato, ...15 args...)`

Every call re-prepares the SQL in the driver. For 43k rows → 43k–86k `ExecContext` calls.

### Why EXECUTE BLOCK works here

Firebird's `EXECUTE BLOCK` supports typed input parameters:
```sql
EXECUTE BLOCK (
  p_id       VARCHAR(36) = ?,
  p_nombre   VARCHAR(100) = ?,
  ...
)
AS BEGIN
  UPDATE MSP_AN_WINBACK_CANDIDATOS SET NOMBRE = :p_nombre ... WHERE CLIENTE_ID = :p_cliente_id;
  IF (ROW_COUNT = 0) THEN
    INSERT INTO MSP_AN_WINBACK_CANDIDATOS (...) VALUES (:p_id, ...);
END
```

Sending N rows as N such blocks in one batch (or better: one block with N UPDATE/INSERT pairs) reduces round-trips from ~2N to 1 per chunk. The nakagami driver's MERGE bug is in `MERGE USING (SELECT ?)` — that is NOT triggered here because we don't use MERGE at all; we use `EXECUTE BLOCK` with its own parameter list.

### Chunking strategy

Rather than a single massive EXECUTE BLOCK for 43k rows (which would exceed statement length limits), we chunk into groups of `chunkSize = 200` rows. Each chunk becomes one EXECUTE BLOCK call with 200×(params per row) positional `?` parameters. For 43k rows this is ~218 round-trips.

### Parameter layout per row (inside the block)

Each row needs 15 args for INSERT and 11 args for UPDATE. Inside an EXECUTE BLOCK we declare ALL params needed for the insert (15 per row) and reference them in both the UPDATE and the INSERT. The param names inside the block use `EXECUTE BLOCK (param TYPE = ?)` syntax, then `:param` inside the body.

Since Firebird EXECUTE BLOCK input parameters are positional `?` in the driver, the Go code must pass all args for all rows in the chunk in the correct order.

### Ambient transaction requirement

`UpsertCandidatos` receives `ctx` that may carry an ambient `*sql.Tx` (from `fbtestutil.WithTestTransaction` or `app.runInTx`). The `EXECUTE BLOCK` must run through the same querier. Since `Querier` interface only has `ExecContext`, and `EXECUTE BLOCK` with parameters is just a regular parameterized `ExecContext` call (not a DDL statement), we can call `q.ExecContext(ctx, blockSQL, args...)` directly. No `PrepareContext` needed.

---

## Task 1: Write the baseline perf test

**Files:**
- Modify: `internal/analytics/infra/analyticsfb/repo_test.go`

- [ ] **Step 1.1: Add the perf benchmark test to `repo_test.go`**

Add at the end of the file (before the final `}`):

```go
// TestRepo_UpsertCandidatos_PerfBaseline measures UpsertCandidatos throughput.
// Run with:
//
//	FB_DATABASE=/firebird/data/MUEBLERA.FDB \
//	  go test ./internal/analytics/infra/analyticsfb/... -run TestRepo_UpsertCandidatos_PerfBaseline -v -timeout 600s
//
// N=5000 and N=20000 are tested; the test logs rows/sec for each.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_UpsertCandidatos_PerfBaseline(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	for _, n := range []int{5000, 20000} {
		n := n
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				repo := analyticsfb.NewRepo(pool)

				// Build N synthetic candidatos with unique negative CLIENTE_IDs
				// to avoid collisions with real Microsip data.
				candidatos := make([]*domain.WinbackCandidato, 0, n)
				for i := 0; i < n; i++ {
					c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
						ClienteID:         -(100000 + i),
						Nombre:            "Perf Test Cliente",
						Zona:              "R/PERF",
						Telefono:          "238 000 0000",
						FechaUltimaCompra: fixedFechaUltima,
						Frecuencia:        i % 20,
						Monetary:          decimal.NewFromFloat(float64(1000 + i)),
						Saldo:             decimal.NewFromFloat(float64(i % 500)),
						PorLiquidarPct:    decimal.NewFromFloat(float64(i % 100)),
						NextBestProduct:   "ROPERO MONARCA",
						EnControl:         i%2 == 0,
						CohorteFecha:      fixedCohorte,
						Now:               fixedNow,
					})
					require.NoError(t, err)
					candidatos = append(candidatos, c)
				}

				// Skip if table not available.
				if skipErr := repo.UpsertCandidatos(ctx, candidatos[:1]); skipErr != nil {
					t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", skipErr)
				}

				start := time.Now()
				err := repo.UpsertCandidatos(ctx, candidatos)
				elapsed := time.Since(start)
				require.NoError(t, err)

				rowsPerSec := float64(n) / elapsed.Seconds()
				projected43k := time.Duration(float64(43399)/rowsPerSec) * time.Second
				t.Logf("PERF N=%d elapsed=%s rows/sec=%.0f projected_43k=%s",
					n, elapsed, rowsPerSec, projected43k)
			})
		})
	}
}
```

Also add `"fmt"` to the import block at the top of `repo_test.go` if not already present.

- [ ] **Step 1.2: Run the baseline test and record results**

```bash
source /Volumes/M2-1TB/Developer/msp-api/.env && \
  FB_DATABASE=/firebird/data/MUEBLERA.FDB \
  go test ./internal/analytics/infra/analyticsfb/... \
  -run TestRepo_UpsertCandidatos_PerfBaseline \
  -v -timeout 600s 2>&1
```

Expected: The test logs `PERF N=5000 elapsed=... rows/sec=...` and `PERF N=20000 elapsed=... rows/sec=...`. Record these numbers. At baseline the rows/sec is likely 300–800 rows/sec (5000 rows ≈ 6–17s; 20000 rows ≈ 25–67s). Exact numbers will appear in the log.

- [ ] **Step 1.3: Commit baseline test**

```bash
git add internal/analytics/infra/analyticsfb/repo_test.go
git commit -m "test(analytics): add UpsertCandidatos perf baseline measurement test"
```

---

## Task 2: Build the EXECUTE BLOCK batch upsert

**Files:**
- Modify: `internal/analytics/infra/analyticsfb/repo.go`

The core optimization: replace `upsertOne` loop with `upsertBatch` that generates chunked `EXECUTE BLOCK` statements.

### How EXECUTE BLOCK upsert works for a chunk of N rows

For each row in the chunk we declare input parameters and perform the UPDATE-then-INSERT. The Firebird `EXECUTE BLOCK` body for a single row looks like:

```sql
UPDATE MSP_AN_WINBACK_CANDIDATOS
SET NOMBRE=:pN_nombre, ZONA=:pN_zona, TELEFONO=:pN_tel,
    FECHA_ULTIMA_COMPRA=:pN_fuc, FRECUENCIA=:pN_freq,
    MONETARY=:pN_mon, SALDO=:pN_sal, POR_LIQUIDAR_PCT=:pN_plp,
    NEXT_BEST_PRODUCT=:pN_nbp, UPDATED_AT=:pN_upd
WHERE CLIENTE_ID = :pN_cid;
IF (ROW_COUNT = 0) THEN
  INSERT INTO MSP_AN_WINBACK_CANDIDATOS
    (ID, CLIENTE_ID, NOMBRE, ZONA, TELEFONO, FECHA_ULTIMA_COMPRA,
     FRECUENCIA, MONETARY, SALDO, POR_LIQUIDAR_PCT, NEXT_BEST_PRODUCT,
     EN_CONTROL, COHORTE_FECHA, CREATED_AT, UPDATED_AT)
  VALUES (:pN_id, :pN_cid, :pN_nombre, :pN_zona, :pN_tel, :pN_fuc,
          :pN_freq, :pN_mon, :pN_sal, :pN_plp, :pN_nbp,
          :pN_enc, :pN_coh, :pN_cat, :pN_upd);
```

Where `N` is the row index within the chunk (0, 1, 2, ...).

The full `EXECUTE BLOCK` header declares all params:
```sql
EXECUTE BLOCK (
  p0_id VARCHAR(36) = ?, p0_cid INTEGER = ?, p0_nombre VARCHAR(200) = ?,
  p0_zona VARCHAR(100) = ?, p0_tel VARCHAR(50) = ?,
  p0_fuc TIMESTAMP = ?, p0_freq INTEGER = ?,
  p0_mon NUMERIC(18,2) = ?, p0_sal NUMERIC(18,2) = ?,
  p0_plp NUMERIC(5,2) = ?, p0_nbp VARCHAR(120) = ?,
  p0_enc SMALLINT = ?, p0_coh TIMESTAMP = ?,
  p0_cat TIMESTAMP = ?, p0_upd TIMESTAMP = ?,
  p1_id VARCHAR(36) = ?, ...
)
AS BEGIN
  -- row 0 UPDATE then INSERT
  -- row 1 UPDATE then INSERT
  ...
END
```

Go passes the args as a flat `[]any` slice in parameter declaration order.

- [ ] **Step 2.1: Add `upsertBatch` to `repo.go`**

Replace the `upsertOne` helper and the body of `UpsertCandidatos` with the implementation below. Keep `wallClockPtrFromTime` and `nullableWallClockArg` as-is (they are still needed).

In `repo.go`, replace the `UpsertCandidatos` and `upsertOne` methods entirely:

```go
// upsertChunkSize is the number of candidatos per EXECUTE BLOCK call.
// 200 rows × 15 params = 3,000 positional params per block — well within
// Firebird's EXECUTE BLOCK parameter limit and the firebirdsql driver.
// Increasing this reduces round-trips further but enlarges the SQL string.
const upsertChunkSize = 200

// UpsertCandidatos inserts or updates one row per candidato matched by
// CLIENTE_ID. The EXECUTE BLOCK UPDATE branch deliberately omits EN_CONTROL
// and COHORTE_FECHA so existing A/B flags and cohort dates survive across
// refreshes.
//
// For chunks of up to upsertChunkSize rows a single EXECUTE BLOCK statement
// is sent per chunk — one round-trip per 200 rows instead of 2 per row.
//
// All upserts run through the same querier so they are atomic when the caller
// has opened a transaction (e.g. inside RunInTx or WithTestTransaction).
func (r *Repo) UpsertCandidatos(ctx context.Context, candidatos []*domain.WinbackCandidato) error {
	if len(candidatos) == 0 {
		return nil
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for i := 0; i < len(candidatos); i += upsertChunkSize {
		end := i + upsertChunkSize
		if end > len(candidatos) {
			end = len(candidatos)
		}
		chunk := candidatos[i:end]
		if err := r.upsertChunk(ctx, q, chunk); err != nil {
			return err
		}
	}
	return nil
}

// upsertChunk sends a single EXECUTE BLOCK that performs UPDATE-then-INSERT
// for every candidato in chunk.
//
// CRITICAL: the UPDATE clause omits EN_CONTROL and COHORTE_FECHA so they are
// preserved from the original INSERT across subsequent refreshes.
func (r *Repo) upsertChunk(ctx context.Context, q firebird.Querier, chunk []*domain.WinbackCandidato) error {
	sql, args := buildUpsertBlock(chunk)
	if _, err := q.ExecContext(ctx, sql, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}
```

- [ ] **Step 2.2: Add `buildUpsertBlock` to `repo.go`**

Add the following function to `repo.go` (after `upsertChunk`):

```go
// buildUpsertBlock generates a Firebird EXECUTE BLOCK statement that performs
// UPDATE-then-INSERT for each candidato in the chunk.
//
// Parameter naming: each row i uses params p{i}_id, p{i}_cid, etc. to avoid
// collisions across rows in the same block body.
//
// Column types chosen to match MSP_AN_WINBACK_CANDIDATOS schema:
//   - ID: CHAR(36) CHARACTER SET UTF8 → VARCHAR(36) in param
//   - CLIENTE_ID: INTEGER
//   - NOMBRE, ZONA, TELEFONO, NEXT_BEST_PRODUCT: VARCHAR
//   - FECHA_ULTIMA_COMPRA, COHORTE_FECHA, CREATED_AT, UPDATED_AT: TIMESTAMP (nullable handled via nil)
//   - FRECUENCIA: INTEGER
//   - MONETARY, SALDO, POR_LIQUIDAR_PCT: NUMERIC(18,2) / NUMERIC(5,2)
//   - EN_CONTROL: SMALLINT (0 or 1)
//
// Args are bound in the exact order the parameters are declared in the header.
func buildUpsertBlock(chunk []*domain.WinbackCandidato) (string, []any) {
	n := len(chunk)
	args := make([]any, 0, n*15)

	var header strings.Builder
	var body strings.Builder

	header.WriteString("EXECUTE BLOCK (\n")
	body.WriteString("AS\nBEGIN\n")

	for i, c := range chunk {
		prefix := fmt.Sprintf("p%d", i)
		if i > 0 {
			header.WriteString(",\n")
		}
		// Declare 15 params per row.
		header.WriteString(fmt.Sprintf(
			"  %s_id VARCHAR(36) = ?,\n"+
				"  %s_cid INTEGER = ?,\n"+
				"  %s_nom VARCHAR(200) = ?,\n"+
				"  %s_zon VARCHAR(100) = ?,\n"+
				"  %s_tel VARCHAR(50) = ?,\n"+
				"  %s_fuc TIMESTAMP = ?,\n"+
				"  %s_frq INTEGER = ?,\n"+
				"  %s_mon NUMERIC(18,2) = ?,\n"+
				"  %s_sal NUMERIC(18,2) = ?,\n"+
				"  %s_plp NUMERIC(5,2) = ?,\n"+
				"  %s_nbp VARCHAR(120) = ?,\n"+
				"  %s_enc SMALLINT = ?,\n"+
				"  %s_coh TIMESTAMP = ?,\n"+
				"  %s_cat TIMESTAMP = ?,\n"+
				"  %s_upd TIMESTAMP = ?",
			prefix, prefix, prefix, prefix, prefix,
			prefix, prefix, prefix, prefix, prefix,
			prefix, prefix, prefix, prefix, prefix,
		))

		// Body: UPDATE (omit EN_CONTROL, COHORTE_FECHA) then conditional INSERT.
		body.WriteString(fmt.Sprintf(
			"  UPDATE MSP_AN_WINBACK_CANDIDATOS SET\n"+
				"    NOMBRE=:%s_nom, ZONA=:%s_zon, TELEFONO=:%s_tel,\n"+
				"    FECHA_ULTIMA_COMPRA=:%s_fuc, FRECUENCIA=:%s_frq,\n"+
				"    MONETARY=:%s_mon, SALDO=:%s_sal,\n"+
				"    POR_LIQUIDAR_PCT=:%s_plp, NEXT_BEST_PRODUCT=:%s_nbp,\n"+
				"    UPDATED_AT=:%s_upd\n"+
				"  WHERE CLIENTE_ID=:%s_cid;\n"+
				"  IF (ROW_COUNT=0) THEN\n"+
				"    INSERT INTO MSP_AN_WINBACK_CANDIDATOS\n"+
				"      (ID,CLIENTE_ID,NOMBRE,ZONA,TELEFONO,FECHA_ULTIMA_COMPRA,\n"+
				"       FRECUENCIA,MONETARY,SALDO,POR_LIQUIDAR_PCT,NEXT_BEST_PRODUCT,\n"+
				"       EN_CONTROL,COHORTE_FECHA,CREATED_AT,UPDATED_AT)\n"+
				"    VALUES(:%s_id,:%s_cid,:%s_nom,:%s_zon,:%s_tel,:%s_fuc,\n"+
				"           :%s_frq,:%s_mon,:%s_sal,:%s_plp,:%s_nbp,\n"+
				"           :%s_enc,:%s_coh,:%s_cat,:%s_upd);\n",
			prefix, prefix, prefix,
			prefix, prefix,
			prefix, prefix,
			prefix, prefix,
			prefix,
			prefix,
			prefix, prefix, prefix, prefix, prefix, prefix,
			prefix, prefix, prefix, prefix, prefix,
			prefix, prefix, prefix, prefix,
		))

		// Bind args in param declaration order (15 per row).
		enControl := 0
		if c.EnControl() {
			enControl = 1
		}
		args = append(args,
			c.ID().String(),                                            // _id
			c.ClienteID(),                                             // _cid
			c.Nombre(),                                                // _nom
			c.Zona(),                                                  // _zon
			c.Telefono(),                                              // _tel
			nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompra())), // _fuc
			c.Frecuencia(),                                            // _frq
			c.Monetary(),                                              // _mon
			c.Saldo(),                                                 // _sal
			c.PorLiquidarPct(),                                        // _plp
			c.NextBestProduct(),                                       // _nbp
			enControl,                                                 // _enc
			firebird.ToWallClock(c.CohorteFecha()),                    // _coh
			firebird.ToWallClock(c.CreatedAt()),                       // _cat
			firebird.ToWallClock(c.UpdatedAt()),                       // _upd
		)
	}

	header.WriteString("\n)")
	body.WriteString("END")

	return header.String() + "\n" + body.String(), args
}
```

Note: `fmt` and `strings` must both be in the import block of `repo.go`. Check the current imports — `strings` is already there. Add `fmt` if not present.

- [ ] **Step 2.3: Verify the build**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./internal/analytics/...
```

Expected: no compilation errors.

- [ ] **Step 2.4: Run existing tests (non-FB) to check nothing is broken**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/analytics/... -run "^Test" -short -count=1 -timeout 60s 2>&1 | tail -20
```

Expected: all non-FB tests pass. FB integration tests are skipped (FB_DATABASE not required here).

---

## Task 3: Run FB integration tests after optimization

- [ ] **Step 3.1: Run all analyticsfb integration tests**

```bash
source /Volumes/M2-1TB/Developer/msp-api/.env && \
  FB_DATABASE=/firebird/data/MUEBLERA.FDB \
  go test ./internal/analytics/infra/analyticsfb/... \
  -v -count=1 -timeout 300s 2>&1
```

Expected output (key tests that must pass):
- `TestRepo_UpsertAndList_RoundTrip` — PASS
- `TestRepo_Upsert_PreservesEnControlAndCohorte` — PASS (this is the headline invariant)
- `TestRepo_ListCandidatos_Filters` — PASS
- `TestRepo_ExistingControlFlags_ReturnsCorrectMap` — PASS
- `TestRepo_LeerAnclasDesde_Regression` — PASS
- `TestRepo_UpsertCandidatos_PerfBaseline` — PASS, logs rows/sec for N=5000 and N=20000

If `TestRepo_Upsert_PreservesEnControlAndCohorte` fails, investigate: the EXECUTE BLOCK UPDATE branch may be incorrectly updating EN_CONTROL or COHORTE_FECHA. Check `buildUpsertBlock` — the UPDATE SET clause must NOT include `EN_CONTROL` or `COHORTE_FECHA`.

- [ ] **Step 3.2: Record optimized rows/sec**

From the perf test log lines, record:
- Baseline N=5000: `???` rows/sec
- Baseline N=20000: `???` rows/sec
- Optimized N=5000: `???` rows/sec
- Optimized N=20000: `???` rows/sec

Calculate projected time for 43,399 rows: `43399 / rows_per_sec_optimized`.

If optimized rows/sec < 1,000 rows/sec (projected 43k > 43s), try increasing `upsertChunkSize` to 500 and re-run. If > 2,000 rows/sec (projected 43k < 22s), the 30s HTTP deadline is comfortably met for the write phase alone.

- [ ] **Step 3.3: If optimization doesn't reach target, try chunk size 500**

If the N=20000 test shows < 1,400 rows/sec with chunk 200, change `upsertChunkSize` to 500 in `repo.go` and re-run step 3.1.

```go
const upsertChunkSize = 500
```

500 rows × 15 params = 7,500 params per block. This should be safe for Firebird's EXECUTE BLOCK limits (16k params maximum per statement in practice).

Re-run the perf test. If rows/sec improves sufficiently, keep 500. If not (or if driver errors with too many params), revert to 200.

---

## Task 4: Add preserve-flags invariant test for batched path

The existing `TestRepo_Upsert_PreservesEnControlAndCohorte` already tests this via `UpsertCandidatos` (which now uses the batched path). But add an explicit multi-row batched test to confirm the invariant holds when multiple rows are in the same chunk:

**Files:**
- Modify: `internal/analytics/infra/analyticsfb/repo_test.go`

- [ ] **Step 4.1: Add `TestRepo_BatchedUpsert_PreservesEnControlAcrossMultipleRows`**

Add at the end of `repo_test.go`:

```go
// TestRepo_BatchedUpsert_PreservesEnControlAcrossMultipleRows verifies that
// a second batched upsert (with different EN_CONTROL/COHORTE_FECHA in-memory)
// does NOT overwrite the persisted flags for any of the rows in the batch.
// This is the multi-row variant of TestRepo_Upsert_PreservesEnControlAndCohorte.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_BatchedUpsert_PreservesEnControlAcrossMultipleRows(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		// Insert 5 rows with alternating EN_CONTROL values and a fixed cohort date.
		first := make([]*domain.WinbackCandidato, 5)
		clienteIDs := []int{-20001, -20002, -20003, -20004, -20005}
		enControls := []bool{true, false, true, false, true}
		for i, id := range clienteIDs {
			c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         id,
				Nombre:            "Batch Test",
				Zona:              "R/BATCH",
				Frecuencia:        1,
				Monetary:          decimal.NewFromFloat(float64(1000 * (i + 1))),
				Saldo:             decimal.Zero,
				PorLiquidarPct:    decimal.Zero,
				NextBestProduct:   "SILLA",
				EnControl:         enControls[i],
				CohorteFecha:      fixedCohorte,
				Now:               fixedNow,
			})
			require.NoError(t, err)
			first[i] = c
		}

		err := repo.UpsertCandidatos(ctx, first)
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000035 may not be applied: %v", err)
		}

		// Second batch: same cliente IDs, different EN_CONTROL (all false) and different
		// COHORTE_FECHA. The persisted flags must NOT change.
		differentCohorte := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		second := make([]*domain.WinbackCandidato, 5)
		for i, id := range clienteIDs {
			c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         id,
				Nombre:            "Batch Test Actualizado",
				Zona:              "R/BATCH2",
				Frecuencia:        5,
				Monetary:          decimal.NewFromFloat(float64(9000 * (i + 1))),
				Saldo:             decimal.Zero,
				PorLiquidarPct:    decimal.Zero,
				NextBestProduct:   "MESA",
				EnControl:         false,            // different — must NOT be persisted
				CohorteFecha:      differentCohorte, // different — must NOT be persisted
				Now:               fixedNow.Add(time.Hour),
			})
			require.NoError(t, err)
			second[i] = c
		}
		err = repo.UpsertCandidatos(ctx, second)
		require.NoError(t, err)

		// Read back and verify each row.
		page, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{Zona: "R/BATCH2"})
		require.NoError(t, err)

		got := make(map[int]*domain.WinbackCandidato, 5)
		for _, item := range page.Items {
			for _, id := range clienteIDs {
				if item.ClienteID() == id {
					got[id] = item
				}
			}
		}
		require.Len(t, got, 5, "all 5 rows must appear after second batched upsert")

		for i, id := range clienteIDs {
			row := got[id]
			require.NotNil(t, row, "row for clienteID=%d must exist", id)

			// Mutable field was updated.
			assert.Equal(t, "Batch Test Actualizado", row.Nombre(),
				"NOMBRE must be updated for clienteID=%d", id)

			// EN_CONTROL must retain the first-insert value.
			assert.Equal(t, enControls[i], row.EnControl(),
				"EN_CONTROL must be preserved for clienteID=%d: want=%v got=%v",
				id, enControls[i], row.EnControl())

			// COHORTE_FECHA must retain the first-insert value.
			assert.WithinDuration(t, fixedCohorte, row.CohorteFecha(), time.Second,
				"COHORTE_FECHA must be preserved for clienteID=%d: want=%s got=%s",
				id, fixedCohorte, row.CohorteFecha())
		}

		t.Logf("batched preserve-flags ok: %d rows verified", len(got))
	})
}
```

- [ ] **Step 4.2: Run the new invariant test**

```bash
source /Volumes/M2-1TB/Developer/msp-api/.env && \
  FB_DATABASE=/firebird/data/MUEBLERA.FDB \
  go test ./internal/analytics/infra/analyticsfb/... \
  -run TestRepo_BatchedUpsert_PreservesEnControlAcrossMultipleRows \
  -v -count=1 -timeout 120s 2>&1
```

Expected: PASS with log `batched preserve-flags ok: 5 rows verified`.

---

## Task 5: Lint and final verification

- [ ] **Step 5.1: Run golangci-lint on the analytics module**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && \
  golangci-lint run ./internal/analytics/... 2>&1
```

Expected: no errors. Common lint issues to watch for:
- `fmt.Sprintf` in a loop body that could use `strings.Builder` more efficiently — acceptable here since we're already using one.
- Unused variables from removed `upsertOne`.
- If `upsertOne` is removed, ensure no references remain. The `upsertCandidato` constant in `queries.go` references `updateCandidato` and `insertCandidato` — these constants can be REMOVED since `buildUpsertBlock` embeds the SQL inline. OR keep them as documentation. If kept but unused by non-test code, golangci-lint may flag them. Remove the constants `updateCandidato` and `insertCandidato` from `queries.go` if the linter complains.

- [ ] **Step 5.2: Run full analytics test suite**

```bash
source /Volumes/M2-1TB/Developer/msp-api/.env && \
  FB_DATABASE=/firebird/data/MUEBLERA.FDB \
  go test ./internal/analytics/... \
  -v -count=1 -timeout 600s 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL|SKIP)|FAIL|ok|PERF)" | head -60
```

Expected: all tests PASS or SKIP (FB tests skip when migration not present), none FAIL.

- [ ] **Step 5.3: Build for production target (Windows)**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && \
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./... 2>&1
```

Expected: exits 0, no errors.

---

## Task 6: Commit

- [ ] **Step 6.1: Stage and commit the optimization**

```bash
git add \
  internal/analytics/infra/analyticsfb/repo.go \
  internal/analytics/infra/analyticsfb/repo_test.go
```

If `queries.go` was modified (to remove unused constants):
```bash
git add internal/analytics/infra/analyticsfb/queries.go
```

```bash
git commit -m "perf(analytics): batch winback upsert to scale full refresh write"
```

---

## Self-Review Checklist

### Spec coverage

| Requirement | Task |
|-------------|------|
| Baseline perf test (N=5000, N=20000) | Task 1 |
| Optimize bulk write (EXECUTE BLOCK chunked) | Task 2 |
| Re-measure after optimization | Task 3 |
| Preserve EN_CONTROL/COHORTE_FECHA invariant via batched path | Task 4 |
| Existing analyticsfb tests remain green | Task 3.1 |
| `go build ./...` clean | Task 5.3 |
| `golangci-lint` clean | Task 5.1 |
| Conventional commit, no --no-verify, no AI attribution | Task 6 |
| Ambient tx honored | Task 2.1 (uses `q.ExecContext`, same as before) |
| Rule 1 (ID/timestamps from Go) | Task 2.2 (args bound from Go, no DB defaults) |

### Notes on architectural question (async vs sync)

After optimization, the full refresh pipeline is:
- Read: 10.77s (measured)
- Write (optimized): projected based on rows/sec measurement in Task 3

If write reaches ~2,000 rows/sec → 43,399 rows ≈ 21.7s → total ~32s (exceeds 30s HTTP deadline).
If write reaches ~3,000 rows/sec → 43,399 rows ≈ 14.5s → total ~25s (fits 30s).
If write reaches ~5,000 rows/sec → 43,399 rows ≈ 8.7s → total ~19.5s (comfortably fits).

**Verdict:** Report the actual numbers from Task 3. Even if total fits in 30s, the background worker (which already exists as `refresh_worker.go`) is the safer long-term home for a 20-30s operation since HTTP clients may time out on slower servers. The optimization makes the synchronous path viable for the dev environment; production deployment should use the worker.
