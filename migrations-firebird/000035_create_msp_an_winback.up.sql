-- ============================================================================
-- Migración 000035: MSP_AN_WINBACK_CANDIDATOS + MSP_AN_REFRESH_STATE
-- ============================================================================
--
-- Por qué existen estas tablas:
--   Materializan la lista de candidatos winback (clientes con alta RFM que
--   no han comprado en los últimos N días) para el motor de inteligencia de
--   ventas. MSP_AN_REFRESH_STATE controla el watermark del último cálculo
--   para evitar recalcular el universo completo en cada refresh incremental.
--
-- Propiedad:
--   Tablas propias de MSP; ningún trigger Microsip las toca. ID, CREATED_AT,
--   UPDATED_AT, COHORTE_FECHA y LAST_RUN_AT se pasan explícitamente desde Go
--   (CLAUDE.md §1).
--
-- MSP_AN_WINBACK_CANDIDATOS:
--   Un row por cliente candidato activo en la cohorte más reciente.
--   UNIQUE(CLIENTE_ID) fuerza una sola entrada por cliente — el proceso de
--   refresh usa UPSERT (UPDATE si existe, INSERT si no). EN_CONTROL=1 indica
--   que el cliente ya tiene una gestión activa y no debe mostrarse en el
--   listado principal del vendedor.
--
-- MSP_AN_REFRESH_STATE:
--   Tabla de estado del scheduler. Un row por job (e.g. 'winback'). No tiene
--   ID propio — el JOB es PK. LAST_WATERMARK guarda el último DOCTOS_PV
--   procesado; NULL en la primera corrida fuerza recálculo completo.
--
-- Restricciones CHECK:
--   - CK_CANDIDATOS_EN_CONTROL  guardarraíl: flag binario 0/1 (no BOOLEAN,
--                                convención Microsip + tablas MSP_*).
--   - CK_CANDIDATOS_MONETARY    guardarraíl defensivo contra monetary negativo.
--   - CK_CANDIDATOS_SALDO       guardarraíl defensivo contra saldo negativo.
--   La regla canónica de cada invariante vive en el domain entity de Go.
--
-- Índices:
--   - IDX_CANDIDATOS_EN_CONTROL  filtro más común del listado (EN_CONTROL=0).
--   - IDX_CANDIDATOS_MONETARY    orden DESCENDING para ranking de mayor valor.
-- ============================================================================

CREATE TABLE MSP_AN_WINBACK_CANDIDATOS (
  ID                   CHAR(36)      CHARACTER SET ASCII NOT NULL,
  CLIENTE_ID           INTEGER                           NOT NULL,
  NOMBRE               VARCHAR(200)  CHARACTER SET UTF8,
  ZONA                 VARCHAR(120)  CHARACTER SET UTF8,
  TELEFONO             VARCHAR(40)   CHARACTER SET ASCII,
  FECHA_ULTIMA_COMPRA  TIMESTAMP,
  FRECUENCIA           INTEGER                           NOT NULL,
  MONETARY             NUMERIC(18,2)                     NOT NULL,
  SALDO                NUMERIC(18,2)                     NOT NULL,
  POR_LIQUIDAR_PCT     NUMERIC(5,2),
  NEXT_BEST_PRODUCT    VARCHAR(120)  CHARACTER SET UTF8,
  EN_CONTROL           SMALLINT                          NOT NULL,
  COHORTE_FECHA        TIMESTAMP                         NOT NULL,
  CREATED_AT           TIMESTAMP                         NOT NULL,
  UPDATED_AT           TIMESTAMP                         NOT NULL,

  CONSTRAINT PK_MSP_AN_WINBACK_CANDIDATOS PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AN_WINBACK_CAND_CLIENTE UNIQUE (CLIENTE_ID),
  CONSTRAINT CK_MSP_AN_WINBACK_CAND_EN_CTRL CHECK (EN_CONTROL IN (0, 1)),
  CONSTRAINT CK_MSP_AN_WINBACK_CAND_MONETARY CHECK (MONETARY >= 0),
  CONSTRAINT CK_MSP_AN_WINBACK_CAND_SALDO CHECK (SALDO >= 0)
);

CREATE INDEX            IDX_MSP_AN_WINBACK_CAND_EN_CTRL ON MSP_AN_WINBACK_CANDIDATOS (EN_CONTROL);
CREATE DESCENDING INDEX IDX_MSP_AN_WINBACK_CAND_MONETARY ON MSP_AN_WINBACK_CANDIDATOS (MONETARY);

CREATE TABLE MSP_AN_REFRESH_STATE (
  JOB             VARCHAR(60)  CHARACTER SET ASCII NOT NULL,
  LAST_WATERMARK  TIMESTAMP,
  LAST_RUN_AT     TIMESTAMP                        NOT NULL,

  CONSTRAINT PK_MSP_AN_REFRESH_STATE PRIMARY KEY (JOB)
);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (35, '000035_create_msp_an_winback', CURRENT_TIMESTAMP);
COMMIT;
