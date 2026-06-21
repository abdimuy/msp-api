-- ============================================================================
-- Migración 000040: MSP_AN_CLIENTE_NARRATIVA + MSP_AN_NARRATIVA_PENDIENTE
-- ============================================================================
--
-- Por qué:
--   El módulo de analítica incorpora una "narrativa del analista" generada por
--   LLM para cada cliente: un párrafo de lectura en lenguaje natural más un
--   arreglo de rasgos validados (e.g. "buen pagador", "comprador estacional").
--   Para no llamar al LLM en cada petición, la narrativa se materializa en
--   MSP_AN_CLIENTE_NARRATIVA y se invalida cuando cambia el hash sha256 de los
--   hechos de entrada (INPUT_HASH). La lectura (ObtenerPulsoCliente) compara
--   INPUT_HASH contra los hechos actuales y encola regeneración si hay mismatch
--   o stale cache. El worker de generación procesa la cola recomputando el hash
--   desde los hechos más recientes y regenerando la narrativa sin comparación.
--   Una fila cacheada cuyo INPUT_HASH coincida con los hechos actuales se sirve
--   como-está sin regeneración.
--
-- MSP_AN_CLIENTE_NARRATIVA:
--   Una fila por cliente (garantizada por UNIQUE(CLIENTE_ID)). El PK es UUID
--   generado en Go. NARRATIVA y RASGOS son blobs UTF-8 (texto libre / JSON).
--   INPUT_HASH permite al lector detectar stale cache sin consultar las tablas
--   de origen. MODELO registra el model id del LLM para trazabilidad.
--   GENERADA_EN es el instante en que el LLM produjo la respuesta; CREATED_AT
--   y UPDATED_AT siguen la convención de auditoría estándar de MSP.
--
-- MSP_AN_NARRATIVA_PENDIENTE:
--   Cola acotada e idempotente: PK = CLIENTE_ID, por lo que re-encolar a un
--   cliente ya en cola es un no-op (upsert UPDATE-then-INSERT en Go). El worker
--   consume filas de esta tabla, genera la narrativa y borra la fila al terminar.
--   INPUT_HASH viaja en la fila de cola para que el worker valide que el cliente
--   aún necesita regeneración al momento de procesarlo (si el hash cambió de
--   nuevo antes de que el worker llegue, simplemente re-encola). ENCOLADA_EN
--   permite ordenar la cola por antigüedad y detectar filas atascadas.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y hashes se pasan desde Go
--   (CLAUDE.md §1). Sin triggers, procedimientos ni generadores. Los invariantes
--   de negocio viven en el domain entity de Go.
-- ============================================================================

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

CREATE TABLE MSP_AN_NARRATIVA_PENDIENTE (
  CLIENTE_ID   INTEGER                     NOT NULL,
  INPUT_HASH   CHAR(64)                    CHARACTER SET ASCII NOT NULL,
  ENCOLADA_EN  TIMESTAMP                                       NOT NULL,

  CONSTRAINT PK_MSP_AN_NARRATIVA_PEND PRIMARY KEY (CLIENTE_ID)
);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (40, '000040_create_msp_an_narrativa', CURRENT_TIMESTAMP);
COMMIT;
