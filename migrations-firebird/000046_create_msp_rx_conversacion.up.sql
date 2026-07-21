-- ============================================================================
-- Migración 000046: MSP_RX_CONVERSACION + MSP_RX_TURNO + MSP_RX_DECISION
-- ============================================================================
--
-- Por qué:
--   La Fase 3a del módulo de reactivación (R7) construye el copiloto de IA: por
--   cada mensaje entrante del cliente, la IA propone (intención + acción +
--   borrador) y una capa de política determinista decide si el bot responde o
--   escala a un humano. Todo en modo sombra (la IA propone, el humano confirma)
--   y con log auditable por decisión. Estas tres tablas materializan el estado y
--   la evidencia de ese flujo, reusando MSP_RX_COHORTE (Fase 1) y enlazando a
--   MSP_RX_MENSAJES (Fase 2, salientes). Los cálculos, IDs y timestamps viven en
--   Go (CLAUDE.md §1).
--
-- MSP_RX_CONVERSACION:
--   Una fila por cliente (garantizada por UNIQUE(CLIENTE_ID)). PK = UUID CHAR(36)
--   ASCII generado en Go. ESTADO codifica el estado en el flujo de venta
--   ('contactado'|'respondio'|'conversando'|'escalado'|'interesado'|'enganche'|
--   'descartado'), definido como constantes en el domain VO (estado_conversacion.go).
--   ASIGNADO_A es el operador humano al escalar (NULL mientras el bot maneja).
--   RESUMEN_MEMORIA es el resumen estructurado para el LLM (no el chat crudo).
--   CONTEXTO_NOTA + BANDERAS son el destilado gobernado de la nota del cobrador
--   (CLIENTES.NOTAS): contexto privado que informa el tono, nunca se cita al
--   cliente; BANDERAS es un arreglo JSON de banderas de venta. NOTA_HASH es el
--   sha256 de la nota destilada (lazy cache: re-destilar sólo si cambia el hash).
--   CREATED_AT/UPDATED_AT siguen la convención de auditoría estándar de MSP.
--
-- MSP_RX_TURNO:
--   Un turno del hilo de conversación (entrante o saliente). PK = UUID CHAR(36)
--   ASCII generado en Go. DIRECCION ('entrante'|'saliente') y AUTOR
--   ('cliente'|'ia'|'humano') son constantes del domain. CUERPO es BLOB TEXT UTF8.
--   MENSAJE_REF enlaza (opcional) al registro de envío de Fase 2 (MSP_RX_MENSAJES.ID)
--   cuando un turno saliente se encola por el canal. NO hay UNIQUE en CLIENTE_ID:
--   un cliente acumula varios turnos. CREATED_AT es el instante del turno.
--
-- MSP_RX_DECISION:
--   Log auditable: una fila por decisión del copiloto. PK = UUID CHAR(36) ASCII
--   generado en Go. TURNO_REF enlaza (opcional) al turno entrante que originó la
--   decisión. INTENCION es la intención clasificada por la IA; CONFIANZA es el
--   porcentaje [0,100] (SMALLINT); SENALES es un arreglo JSON de señales
--   detectadas. ACCION_PROPUESTA ('responder'|'escalar') es lo que la política
--   determinó. BORRADOR es el texto redactado (si responde). EVIDENCIA es un
--   arreglo JSON de chips de evidencia (el "por qué"). RAZON_ESCALAMIENTO explica
--   por qué escaló (si escaló). RESULTADO codifica el desenlace
--   ('propuesto'|'aprobado'|'editado'|'escalado'), definido en el domain.
--   CREATED_AT es el instante de la decisión.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y hashes se pasan desde Go
--   (CLAUDE.md §1). Sin triggers, procedimientos ni generadores. Los invariantes
--   de negocio (estados válidos, transiciones, enums) viven en el domain de Go.
-- ============================================================================

CREATE TABLE MSP_RX_CONVERSACION (
  ID               CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  CLIENTE_ID       INTEGER                             NOT NULL,
  ESTADO           VARCHAR(16)   CHARACTER SET ASCII   NOT NULL,
  ASIGNADO_A       VARCHAR(64)   CHARACTER SET UTF8,
  RESUMEN_MEMORIA  BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  CONTEXTO_NOTA    BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  BANDERAS         BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  NOTA_HASH        CHAR(64)      CHARACTER SET ASCII,
  CREATED_AT       TIMESTAMP                           NOT NULL,
  UPDATED_AT       TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_RX_CONVERSACION      PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_RX_CONVERSACION_CLI  UNIQUE (CLIENTE_ID)
);

CREATE TABLE MSP_RX_TURNO (
  ID           CHAR(36)      CHARACTER SET ASCII                  NOT NULL,
  CLIENTE_ID   INTEGER                                            NOT NULL,
  DIRECCION    VARCHAR(10)   CHARACTER SET ASCII                  NOT NULL,
  AUTOR        VARCHAR(10)   CHARACTER SET ASCII                  NOT NULL,
  CUERPO       BLOB SUB_TYPE TEXT CHARACTER SET UTF8              NOT NULL,
  MENSAJE_REF  CHAR(36)      CHARACTER SET ASCII,
  CREATED_AT   TIMESTAMP                                          NOT NULL,

  CONSTRAINT PK_MSP_RX_TURNO PRIMARY KEY (ID)
);

CREATE TABLE MSP_RX_DECISION (
  ID                  CHAR(36)      CHARACTER SET ASCII            NOT NULL,
  CLIENTE_ID          INTEGER                                      NOT NULL,
  TURNO_REF           CHAR(36)      CHARACTER SET ASCII,
  INTENCION           VARCHAR(120)  CHARACTER SET UTF8,
  CONFIANZA           SMALLINT,
  SENALES             BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  ACCION_PROPUESTA    VARCHAR(16)   CHARACTER SET ASCII,
  BORRADOR            BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  EVIDENCIA           BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  RAZON_ESCALAMIENTO  VARCHAR(200)  CHARACTER SET UTF8,
  RESULTADO           VARCHAR(16)   CHARACTER SET ASCII,
  CREATED_AT          TIMESTAMP                                    NOT NULL,

  CONSTRAINT PK_MSP_RX_DECISION PRIMARY KEY (ID)
);

CREATE INDEX IDX_MSP_RX_CONVERSACION_ESTADO ON MSP_RX_CONVERSACION (ESTADO);
CREATE INDEX IDX_MSP_RX_TURNO_CLIENTE       ON MSP_RX_TURNO (CLIENTE_ID);
CREATE INDEX IDX_MSP_RX_DECISION_CLIENTE    ON MSP_RX_DECISION (CLIENTE_ID);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (46, '000046_create_msp_rx_conversacion', CURRENT_TIMESTAMP);
COMMIT;
