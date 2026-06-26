-- ============================================================================
-- Migración 000042: MSP_AN_CARTERA_SNAPSHOT
-- ============================================================================
--
-- Por qué:
--   El módulo de analítica necesita almacenar snapshots periódicos de la
--   distribución de aging de cartera (por zona, cobrador y bucket de días
--   de vencimiento) para calcular roll-rate y tendencias históricas.
--   Cada fila representa el saldo total y el conteo de clientes de un
--   tramo de antigüedad en un corte de fecha, zona y cobrador.
--   Los cálculos y la generación de IDs/timestamps viven en Go (CLAUDE.md §1).
--
-- MSP_AN_CARTERA_SNAPSHOT:
--   PK = UUID CHAR(36) ASCII generado en Go. UNIQUE (FECHA_CORTE,
--   ZONA_CLIENTE_ID, COBRADOR_ID, BUCKET) garantiza idempotencia al
--   materializar cortes. COBRADOR_ID es nullable para admitir filas de
--   agregación por zona sin desglose de cobrador. BUCKET codifica el tramo
--   de aging: "0-30" / "31-60" / "61-90" / "90+"; los valores canónicos
--   están definidos como constantes en el domain entity de Go (cartera_snapshot.go).
--   SALDO (NUMERIC 18,2) y CONTEO (INTEGER) son los totales del tramo.
--   CREATED_AT y UPDATED_AT siguen la convención de auditoría estándar de MSP.
--
--   Nota: en Firebird, una restricción UNIQUE sobre columnas que contienen NULL
--   no impide filas duplicadas cuando las columnas nullable son NULL (NULL ≠ NULL
--   per SQL estándar). Si se requiere idempotencia estricta para filas sin
--   cobrador, la lógica de upsert del batch debe manejar ese caso en Go.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y valores se pasan
--   desde Go (CLAUDE.md §1). Sin triggers, procedimientos ni generadores.
--   Los invariantes de negocio viven en el domain entity de Go.
-- ============================================================================

CREATE TABLE MSP_AN_CARTERA_SNAPSHOT (
  ID              CHAR(36)      CHARACTER SET ASCII  NOT NULL,
  FECHA_CORTE     TIMESTAMP                          NOT NULL,
  ZONA_CLIENTE_ID INTEGER                            NOT NULL,
  COBRADOR_ID     INTEGER,
  BUCKET          VARCHAR(16)   CHARACTER SET ASCII  NOT NULL,
  SALDO           NUMERIC(18,2)                      NOT NULL,
  CONTEO          INTEGER                            NOT NULL,
  CREATED_AT      TIMESTAMP                          NOT NULL,
  UPDATED_AT      TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_AN_CARTERA_SNAPSHOT    PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AN_CARTERA_SNAP_KEY    UNIQUE (FECHA_CORTE, ZONA_CLIENTE_ID, COBRADOR_ID, BUCKET)
);

CREATE INDEX IDX_MSP_AN_CARTERA_SNAP_FECHA ON MSP_AN_CARTERA_SNAPSHOT (FECHA_CORTE);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (42, '000042_create_msp_an_cartera_snapshot', CURRENT_TIMESTAMP);
COMMIT;
