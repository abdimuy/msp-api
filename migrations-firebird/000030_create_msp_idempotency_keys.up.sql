-- ============================================================================
-- Migración 000030: MSP_IDEMPOTENCY_KEYS
-- ============================================================================
--
-- Por qué existe esta tabla:
--   Reemplaza idempotency_keys (Postgres) para que el middleware de
--   Idempotency-Key consulte y persista en la misma BD que la escritura de
--   negocio. Ver ADR-0008.
--
-- Propiedad:
--   Tabla propia de MSP; ningún trigger Microsip la toca. IDEM_KEY, CREATED_AT,
--   EXPIRES_AT, REQUEST_HASH, RESPONSE_BODY se pasan explícitamente desde Go
--   (CLAUDE.md §1).
--
-- Nombre de la columna IDEM_KEY:
--   "KEY" es palabra reservada en Firebird (igual que en SQL estándar).
--   La columna pública del Store sigue llamándose Key en Go; el SQL la mapea
--   a IDEM_KEY.
--
-- Restricciones CHECK:
--   - CK_TTL     guardarraíl: EXPIRES_AT > CREATED_AT (la TTL siempre es
--                positiva).
--   - CK_STATUS  guardarraíl: sólo persistimos 2xx. Sigue la política de
--                caching del middleware (idempotency.go §package doc): los
--                4xx no se cachean para que el cliente pueda corregir y
--                reintentar; los 5xx no se cachean para no pinear errores
--                transitorios 24h.
--   La regla canónica vive en internal/platform/idempotency/.
--
-- Índices:
--   - IDX_EXPIRES  usado por el janitor para borrar rows expiradas.
-- ============================================================================

CREATE TABLE MSP_IDEMPOTENCY_KEYS (
  IDEM_KEY         VARCHAR(255) CHARACTER SET ASCII NOT NULL,
  USUARIO_ID       CHAR(36)     CHARACTER SET ASCII,
  METHOD           VARCHAR(10)  CHARACTER SET ASCII NOT NULL,
  PATH             VARCHAR(255) CHARACTER SET ASCII NOT NULL,
  REQUEST_HASH     CHAR(64)     CHARACTER SET ASCII NOT NULL,
  RESPONSE_STATUS  INTEGER      NOT NULL,
  RESPONSE_BODY    BLOB SUB_TYPE TEXT SEGMENT SIZE 1024 CHARACTER SET UTF8 NOT NULL,
  CREATED_AT       TIMESTAMP    NOT NULL,
  EXPIRES_AT       TIMESTAMP    NOT NULL,

  CONSTRAINT PK_MSP_IDEMPOTENCY_KEYS PRIMARY KEY (IDEM_KEY),
  CONSTRAINT CK_MSP_IDEMPOTENCY_KEYS_TTL    CHECK (EXPIRES_AT > CREATED_AT),
  CONSTRAINT CK_MSP_IDEMPOTENCY_KEYS_STATUS CHECK (RESPONSE_STATUS BETWEEN 200 AND 299)
);

CREATE INDEX IDX_MSP_IDEMPOTENCY_EXPIRES ON MSP_IDEMPOTENCY_KEYS (EXPIRES_AT);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (30, '000030_create_msp_idempotency_keys', CURRENT_TIMESTAMP);
COMMIT;
