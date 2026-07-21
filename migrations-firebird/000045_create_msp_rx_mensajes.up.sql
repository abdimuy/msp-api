-- ============================================================================
-- Migración 000045: MSP_RX_MENSAJES
-- ============================================================================
--
-- Por qué:
--   La Fase 2 del módulo de reactivación (R7) materializa la cola de mensajes
--   salientes del canal: un registro por mensaje que se encola, se envía (por un
--   MessageSender — hoy un enviador falso, mañana whatsmeow) y cuyo resultado se
--   audita. El gobernador de envío (anti-baneo) usa ENVIADO_EN para el tope
--   diario; la integridad de la medición usa SENDER_KIND (simulado vs real) para
--   que un envío simulado nunca cuente como contacto real en la atribución.
--   Los cálculos, IDs y timestamps viven en Go (CLAUDE.md §1).
--
-- MSP_RX_MENSAJES:
--   PK = UUID CHAR(36) ASCII generado en Go. NO hay UNIQUE en CLIENTE_ID: un
--   cliente puede recibir hasta 2 toques (opener + recordatorio), así que se
--   admiten varias filas por cliente; la idempotencia del encolado se maneja en
--   Go (no re-encolar a quien ya tiene un mensaje). ESTADO codifica la máquina de
--   estados del envío ('encolado'|'enviado'|'fallido'|'bloqueado'), definida como
--   constantes en el domain (estado_mensaje.go). SENDER_KIND ('simulado'|'real')
--   y ENVIADO_EN son NULL hasta que el mensaje se envía. CUERPO es BLOB TEXT UTF8.
--   CREATED_AT/UPDATED_AT siguen la convención de auditoría estándar de MSP.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y valores se pasan desde Go
--   (CLAUDE.md §1). Sin triggers, procedimientos ni generadores. Los invariantes
--   de negocio viven en el domain entity de Go.
-- ============================================================================

CREATE TABLE MSP_RX_MENSAJES (
  ID           CHAR(36)      CHARACTER SET ASCII                     NOT NULL,
  CLIENTE_ID   INTEGER                                               NOT NULL,
  SEGMENTO     VARCHAR(24)   CHARACTER SET ASCII                     NOT NULL,
  TELEFONO     VARCHAR(40)                                           NOT NULL,
  CUERPO       BLOB SUB_TYPE TEXT CHARACTER SET UTF8                 NOT NULL,
  ESTADO       VARCHAR(16)   CHARACTER SET ASCII                     NOT NULL,
  SENDER_KIND  VARCHAR(12)   CHARACTER SET ASCII,
  ENCOLADO_EN  TIMESTAMP                                             NOT NULL,
  ENVIADO_EN   TIMESTAMP,
  ERROR        VARCHAR(500),
  CREATED_AT   TIMESTAMP                                             NOT NULL,
  UPDATED_AT   TIMESTAMP                                             NOT NULL,

  CONSTRAINT PK_MSP_RX_MENSAJES PRIMARY KEY (ID)
);

CREATE INDEX IDX_MSP_RX_MENSAJES_ESTADO  ON MSP_RX_MENSAJES (ESTADO);
CREATE INDEX IDX_MSP_RX_MENSAJES_CLIENTE ON MSP_RX_MENSAJES (CLIENTE_ID);
CREATE INDEX IDX_MSP_RX_MENSAJES_ENVIADO ON MSP_RX_MENSAJES (ENVIADO_EN);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (45, '000045_create_msp_rx_mensajes', CURRENT_TIMESTAMP);
COMMIT;
