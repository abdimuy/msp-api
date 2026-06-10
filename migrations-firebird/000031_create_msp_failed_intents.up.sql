-- ============================================================================
-- Migración 000031: MSP_FAILED_INTENTS
-- ============================================================================
--
-- Por qué existe esta tabla:
--   Reemplaza failed_intents (Postgres) para captura del problema
--   "venta-zombie": toda respuesta 4xx/5xx a una request mutante queda
--   persistida con el payload original para que un admin la inspeccione,
--   reintente o resuelva. Ver ADR-0005 + ADR-0008.
--
-- Propiedad:
--   Tabla propia de MSP; ningún trigger Microsip la toca. ID, RECEIVED_AT,
--   REQUEST_ID, ERROR_CODE, BODY se pasan explícitamente desde Go
--   (CLAUDE.md §1).
--
-- BODY vs BODY_BLOB_PATH:
--   Mutuamente excluyentes en práctica — invariante validado en Go:
--     - Ruta JSON:      BODY tiene el payload (posiblemente truncado),
--                       BODY_BLOB_PATH es ''.
--     - Ruta multipart: BODY es null/'null', BODY_BLOB_PATH apunta al
--                       archivo on-disk y BODY_CONTENT_TYPE preserva el
--                       Content-Type original (con boundary multipart).
--   El esquema no enforce esto — la regla vive en
--   internal/platform/failedintent/.
--
-- BODY_TRUNCATED:
--   CHAR(1) 'S'|'N' (no BOOLEAN), igual que el resto de tablas MSP_*.
--   Firebird tiene un tipo BOOLEAN nativo desde 3.0 pero la convención
--   Microsip + nuestras tablas usan el flag char-único.
--
-- Restricciones CHECK:
--   - CK_STATUS    valida la state machine de la captura:
--                  new → {retried_ok | retried_fail | ignored | resolved_manual}
--                  Reglas canónicas en failedintent.Status.
--   - CK_TRUNCATED guardarraíl del flag char-único.
--
-- Índices:
--   - IDX_RECEIVED   DESCENDING — el listado del admin pagina por
--                    RECEIVED_AT DESC.
--   - IDX_STATUS     compuesto STATUS, RECEIVED_AT para filtrar 'new' al tope.
--   - IDX_FB_UID     usado por el endpoint /v2/me/failed-intents.
--   - IDX_REQUEST_ID usado por correlación con el log trace.
-- ============================================================================

CREATE TABLE MSP_FAILED_INTENTS (
  ID                 CHAR(36)     CHARACTER SET ASCII NOT NULL,
  RECEIVED_AT        TIMESTAMP    NOT NULL,
  METHOD             VARCHAR(10)  CHARACTER SET ASCII NOT NULL,
  PATH               VARCHAR(255) CHARACTER SET ASCII NOT NULL,
  FIREBASE_UID       VARCHAR(128) CHARACTER SET ASCII,
  USUARIO_ID         CHAR(36)     CHARACTER SET ASCII,
  IDEMPOTENCY_KEY    VARCHAR(255) CHARACTER SET ASCII,
  REQUEST_ID         CHAR(36)     CHARACTER SET ASCII NOT NULL,
  BODY               BLOB SUB_TYPE TEXT SEGMENT SIZE 4096 CHARACTER SET UTF8 NOT NULL,
  BODY_TRUNCATED     CHAR(1)      CHARACTER SET ASCII NOT NULL,
  BODY_BLOB_PATH     VARCHAR(500) CHARACTER SET ASCII,
  BODY_CONTENT_TYPE  VARCHAR(255) CHARACTER SET ASCII,
  HTTP_STATUS        INTEGER      NOT NULL,
  ERROR_CODE         VARCHAR(80)  CHARACTER SET ASCII NOT NULL,
  ERROR_MESSAGE      BLOB SUB_TYPE TEXT SEGMENT SIZE 512 CHARACTER SET UTF8 NOT NULL,
  RETRY_COUNT        INTEGER      NOT NULL,
  STATUS             VARCHAR(20)  CHARACTER SET ASCII NOT NULL,
  RESOLVED_AT        TIMESTAMP,
  RESOLVED_BY        CHAR(36)     CHARACTER SET ASCII,
  NOTES              BLOB SUB_TYPE TEXT SEGMENT SIZE 256 CHARACTER SET UTF8,

  CONSTRAINT PK_MSP_FAILED_INTENTS PRIMARY KEY (ID),
  CONSTRAINT CK_MSP_FAILED_INTENTS_STATUS CHECK (
    STATUS IN ('new', 'retried_ok', 'retried_fail', 'ignored', 'resolved_manual')
  ),
  CONSTRAINT CK_MSP_FAILED_INTENTS_TRUNCATED CHECK (BODY_TRUNCATED IN ('S', 'N'))
);

CREATE DESCENDING INDEX IDX_MSP_FAILED_INTENTS_RECEIVED   ON MSP_FAILED_INTENTS (RECEIVED_AT);
CREATE INDEX            IDX_MSP_FAILED_INTENTS_STATUS     ON MSP_FAILED_INTENTS (STATUS, RECEIVED_AT);
CREATE INDEX            IDX_MSP_FAILED_INTENTS_FB_UID     ON MSP_FAILED_INTENTS (FIREBASE_UID);
CREATE INDEX            IDX_FAILED_INT_REQUEST_ID ON MSP_FAILED_INTENTS (REQUEST_ID);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (31, '000031_create_msp_failed_intents', CURRENT_TIMESTAMP);
COMMIT;
