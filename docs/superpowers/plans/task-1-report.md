# Task 1 Report — Migration 000040: narrativa IA cache + pending queue

## Status
DONE

## Commit
`fdd978e feat(analytics): migración 000040 tablas de narrativa IA y cola pendiente`

## Files created
- `migrations-firebird/000040_create_msp_an_narrativa.up.sql`
- `migrations-firebird/000040_create_msp_an_narrativa.down.sql`

## DDL — MSP_AN_CLIENTE_NARRATIVA
```sql
CREATE TABLE MSP_AN_CLIENTE_NARRATIVA (
  ID           CHAR(36)                    CHARACTER SET ASCII NOT NULL,
  CLIENTE_ID   INTEGER                                         NOT NULL,
  NARRATIVA    BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  RASGOS       BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  INPUT_HASH   CHAR(64)                    CHARACTER SET ASCII NOT NULL,
  MODELO       VARCHAR(64)                 CHARACTER SET UTF8,
  GENERADA_EN  TIMESTAMP                                       NOT NULL,
  CREATED_AT   TIMESTAMP                                       NOT NULL,
  UPDATED_AT   TIMESTAMP                                       NOT NULL,

  CONSTRAINT PK_MSP_AN_CLIENTE_NARRATIVA  PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AN_CLIENTE_NARR_CLIE  UNIQUE      (CLIENTE_ID)
);
```

## DDL — MSP_AN_NARRATIVA_PENDIENTE
```sql
CREATE TABLE MSP_AN_NARRATIVA_PENDIENTE (
  CLIENTE_ID   INTEGER                     NOT NULL,
  INPUT_HASH   CHAR(64)                    CHARACTER SET ASCII NOT NULL,
  ENCOLADA_EN  TIMESTAMP                                       NOT NULL,

  CONSTRAINT PK_MSP_AN_NARRATIVA_PEND PRIMARY KEY (CLIENTE_ID)
);
```

## Convention matching (000039 / 000035)

| Convention | Applied |
|---|---|
| Spanish `Por qué:` comment header block | Yes — explains materialized LLM cache + bounded idempotent queue, invalidation via INPUT_HASH |
| MSP_MIGRATIONS trailer: `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (..., CURRENT_TIMESTAMP);` | Yes — ID=40, NAME='000040_create_msp_an_narrativa', same column list as 000039 |
| `COMMIT;` after INSERT | Yes |
| down.sql drops tables in safe dependency order, then `DELETE FROM MSP_MIGRATIONS WHERE ID = n;`, then `COMMIT;` | Yes — PENDIENTE dropped first (no FK dependency), then NARRATIVA, then DELETE+COMMIT |
| Intermediate `COMMIT;` between DROP statements (as 000035 does) | Yes — matches 000035 pattern |

## CLAUDE.md §1 checklist

- No DEFAULT on any column — confirmed (no `DEFAULT CURRENT_TIMESTAMP`, no UUID defaults)
- No triggers — confirmed
- No stored procedures — confirmed
- No sequences/generators — confirmed
- No business-rule CHECK constraints — confirmed (only PK and UNIQUE)
- UUID PK is `CHAR(36) CHARACTER SET ASCII` — confirmed
- JSON/text blobs are `BLOB SUB_TYPE TEXT CHARACTER SET UTF8` — confirmed (NARRATIVA, RASGOS)
- Timestamps are `TIMESTAMP` with no default — confirmed (GENERADA_EN, CREATED_AT, UPDATED_AT, ENCOLADA_EN)
- `INPUT_HASH` is `CHAR(64) CHARACTER SET ASCII` for sha256 hex — confirmed

## Indexes
No additional indexes added beyond PK/UNIQUE. The brief noted "only add if the convention shows it" and neither table has obvious filter/sort index candidates at this stage (no boolean flags, no high-cardinality sort columns used in existing queries). Matches the 000039 pattern (ALTER TABLE migration with no new indexes).

## Build verification
`go build ./...` — clean, no output (pure SQL change, no Go code affected).

## Pre-commit hooks
All hooks passed: secrets-check ✔, commit-msg format ✔. Go-specific hooks (lint-staged, vet, build-check, format-check) were correctly skipped as no `.go` files were staged.

## Concerns
None.
