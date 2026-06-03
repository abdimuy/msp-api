-- ============================================================================
-- Migración 000022: tablas changelog + columnas TX_ID en cachés
-- ============================================================================
--
-- Objetivo: sentar las bases estructurales del push channel + watermark xmin
-- (plan completo en ADR-0007).  Esta migración es puramente DDL — no contiene
-- lógica de negocio ni triggers; los procedures que escriben en los changelogs
-- se agregan en el commit 2 del sprint.
--
-- ─── Qué se crea ────────────────────────────────────────────────────────────
--
-- MSP_PAGOS_CHANGELOG / MSP_SALDOS_CHANGELOG
--   Tablas append-only de eventos confirmados.  Cada fila representa "un cambio
--   en el caché fue confirmado (COMMIT) en la transacción TX_ID".
--
--   SEQ_ID  — clave primaria monotónica generada con GEN_ID(GEN_MSP_*_SEQ, 1).
--             Permite al listener Go hacer probe de máximo SEQ_ID sin necesidad
--             de leer TX_ID del monitor: el startup watermark se obtiene con
--             SELECT FIRST 1 SEQ_ID FROM MSP_*_CHANGELOG ORDER BY SEQ_ID DESC.
--
--   TX_ID   — RDB$GET_CONTEXT('SYSTEM','CURRENT_TRANSACTION') en el momento del
--             INSERT.  Dado que Firebird asigna transaction-id antes del COMMIT,
--             este valor es *la* identidad de la transacción confirmante, no la
--             de la transacción lectora.  El listener usa
--             WHERE TX_ID < :watermark para descartar filas de txns aún abiertas
--             o confirmadas después del snapshot del lector.
--
--   COMMIT_AT — CURRENT_TIMESTAMP al INSERT.  Mejor esfuerzo (aproximación a
--               la hora de COMMIT); útil para diagnóstico y alertas de lag.
--
-- ─── Columnas TX_ID en MSP_PAGOS_VENTAS / MSP_SALDOS_VENTAS ────────────────
--
--   Las columnas TX_ID en los cachés permiten al cursor de sincronización
--   incremental filtrar filas cuya transacción aún no está confirmada:
--     WHERE TX_ID < :watermark
--   Se escribirán desde los procedures de recompute en el commit 2.
--   El valor DEFAULT 0 en las filas existentes es semánticamente "committeada
--   en el pasado lejano": 0 < cualquier MIN(MON$TRANSACTION_ID) WHERE
--   MON$STATE = 1, por lo que siempre cae dentro del watermark y el cursor
--   incremental las incluirá correctamente.
--
-- ─── Sin índices en TX_ID de los cachés ────────────────────────────────────
--
--   El índice sobre MSP_PAGOS_VENTAS.TX_ID y MSP_SALDOS_VENTAS.TX_ID se
--   agrega en el commit 7 del sprint, cuando se introduce la query que lo usa.
--   Agregar índices prematuros en un caché que se escribe continuamente
--   añade overhead de mantenimiento sin beneficio todavía.
--
-- ─── Excepción a CLAUDE.md §1 ───────────────────────────────────────────────
--
--   Esta migración forma parte del adapter Firebird (migrations-firebird/) y
--   está exenta de la regla "sin lógica en la BD" conforme al ADR-0006.
--   Los generadores son estructurales: equivalentes a SERIAL en Postgres,
--   sin lógica de negocio embebida.
-- ============================================================================

-- ─── Generadores ──────────────────────────────────────────────────────────────

CREATE GENERATOR GEN_MSP_PAGOS_CHANGELOG_SEQ;
COMMIT;

CREATE GENERATOR GEN_MSP_SALDOS_CHANGELOG_SEQ;
COMMIT;

-- ─── MSP_PAGOS_CHANGELOG ─────────────────────────────────────────────────────

CREATE TABLE MSP_PAGOS_CHANGELOG (
  SEQ_ID              BIGINT        NOT NULL,
  IMPTE_DOCTO_CC_ID   INTEGER       NOT NULL,  -- PK de MSP_PAGOS_VENTAS cuyo estado cambió
  TX_ID               BIGINT        NOT NULL,  -- RDB$GET_CONTEXT('SYSTEM','CURRENT_TRANSACTION')
  COMMIT_AT           TIMESTAMP     NOT NULL,  -- CURRENT_TIMESTAMP al insertar; mejor esfuerzo
  CONSTRAINT PK_MSP_PAGOS_CHANGELOG PRIMARY KEY (SEQ_ID)
);
COMMIT;

-- Índice para watermark scans: WHERE TX_ID < :watermark
CREATE INDEX IDX_MSP_PAGOS_CHANGELOG_TX_ID ON MSP_PAGOS_CHANGELOG (TX_ID);
COMMIT;

-- ─── MSP_SALDOS_CHANGELOG ────────────────────────────────────────────────────

CREATE TABLE MSP_SALDOS_CHANGELOG (
  SEQ_ID          BIGINT        NOT NULL,
  DOCTO_CC_ID     INTEGER       NOT NULL,  -- PK de MSP_SALDOS_VENTAS cuyo estado cambió
  TX_ID           BIGINT        NOT NULL,  -- RDB$GET_CONTEXT('SYSTEM','CURRENT_TRANSACTION')
  COMMIT_AT       TIMESTAMP     NOT NULL,  -- CURRENT_TIMESTAMP al insertar; mejor esfuerzo
  CONSTRAINT PK_MSP_SALDOS_CHANGELOG PRIMARY KEY (SEQ_ID)
);
COMMIT;

-- Índice para watermark scans: WHERE TX_ID < :watermark
CREATE INDEX IDX_MSP_SALDOS_CHANGELOG_TX_ID ON MSP_SALDOS_CHANGELOG (TX_ID);
COMMIT;

-- ─── Columnas TX_ID en cachés existentes ─────────────────────────────────────
--
-- Firebird backfilla automáticamente todas las filas existentes al valor DEFAULT
-- cuando se agrega una columna NOT NULL con DEFAULT.  DEFAULT 0 es correcto:
-- 0 < cualquier transaction-id activo o futuro, por lo que el cursor incremental
-- las incluirá en cualquier watermark scan.

ALTER TABLE MSP_PAGOS_VENTAS ADD TX_ID BIGINT DEFAULT 0 NOT NULL;
COMMIT;

ALTER TABLE MSP_SALDOS_VENTAS ADD TX_ID BIGINT DEFAULT 0 NOT NULL;
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (22, '000022_changelog_and_tx_id', CURRENT_TIMESTAMP);
COMMIT;
