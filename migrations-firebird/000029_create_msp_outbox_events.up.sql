-- ============================================================================
-- Migración 000029: MSP_OUTBOX_EVENTS
-- ============================================================================
--
-- Por qué existe esta tabla:
--   Reemplaza outbox_events (Postgres) para que el evento se inserte en la
--   MISMA transacción que la escritura de negocio. firebird.TxManager.RunInTx
--   es re-entrante: el INSERT del evento corre dentro de la tx del service,
--   y el COMMIT cubre ambos. Cero gap. Ver ADR-0008.
--
-- Propiedad:
--   Tabla propia de MSP; ningún trigger Microsip la toca. ID, PAYLOAD,
--   CREATED_AT, ATTEMPTS se pasan explícitamente desde Go (CLAUDE.md §1).
--
-- Restricciones CHECK:
--   - CK_ATTEMPTS  guardarraíl defensivo contra contadores negativos.
--   - CK_TERMINAL  invariante de estado: PROCESSED_AT y FAILED_AT son
--                  mutuamente excluyentes; la combinación pendiente
--                  (ambos NULL) también es válida.
--   Las reglas canónicas viven en internal/platform/outboxfb/; los CHECK
--   sólo previenen inconsistencias por escrituras manuales a la BD.
--
-- Índices:
--   - IDX_PENDING    usado por el dispatcher para tomar el batch ordenado
--                    por CREATED_AT ASC entre rows con PROCESSED_AT IS NULL
--                    AND FAILED_AT IS NULL.
--   - IDX_AGGREGATE  usado por queries de debug / auditoría que listan los
--                    eventos de un agregado (venta, cliente, traspaso).
-- ============================================================================

CREATE TABLE MSP_OUTBOX_EVENTS (
  ID            CHAR(36)    CHARACTER SET ASCII NOT NULL,
  AGGREGATE     VARCHAR(50) CHARACTER SET ASCII NOT NULL,
  AGGREGATE_ID  CHAR(36)    CHARACTER SET ASCII NOT NULL,
  EVENT_TYPE    VARCHAR(80) CHARACTER SET ASCII NOT NULL,
  PAYLOAD       BLOB SUB_TYPE TEXT SEGMENT SIZE 1024 CHARACTER SET UTF8 NOT NULL,
  CREATED_AT    TIMESTAMP   NOT NULL,
  PROCESSED_AT  TIMESTAMP,
  FAILED_AT     TIMESTAMP,
  ATTEMPTS      INTEGER     NOT NULL,
  LAST_ERROR    BLOB SUB_TYPE TEXT SEGMENT SIZE 256 CHARACTER SET UTF8,

  CONSTRAINT PK_MSP_OUTBOX_EVENTS PRIMARY KEY (ID),
  CONSTRAINT CK_MSP_OUTBOX_EVENTS_ATTEMPTS  CHECK (ATTEMPTS >= 0),
  CONSTRAINT CK_MSP_OUTBOX_EVENTS_TERMINAL  CHECK (
    (PROCESSED_AT IS NULL     AND FAILED_AT IS NULL) OR
    (PROCESSED_AT IS NOT NULL AND FAILED_AT IS NULL) OR
    (PROCESSED_AT IS NULL     AND FAILED_AT IS NOT NULL)
  )
);

CREATE INDEX IDX_MSP_OUTBOX_EVENTS_PENDING   ON MSP_OUTBOX_EVENTS (CREATED_AT);
CREATE INDEX IDX_MSP_OUTBOX_EVENTS_AGGREGATE ON MSP_OUTBOX_EVENTS (AGGREGATE, AGGREGATE_ID);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (29, '000029_create_msp_outbox_events', CURRENT_TIMESTAMP);
COMMIT;
