-- ============================================================================
-- Migración 000044: MSP_RX_COHORTE
-- ============================================================================
--
-- Por qué:
--   El módulo de reactivación (R7) materializa un snapshot de cohorte del
--   piloto de Tehuacán: una fila por cliente del experimento, fijada al
--   construir la cohorte con su COHORTE_FECHA. El snapshot permite medir el
--   enganche (recompra) *después* de esa fecha sin que el universo se mueva,
--   comparando tratamiento (contactado) vs control.
--   Los cálculos y la generación de IDs/timestamps viven en Go (CLAUDE.md §1).
--
-- MSP_RX_COHORTE:
--   PK = UUID CHAR(36) ASCII generado en Go. UNIQUE (CLIENTE_ID) garantiza
--   idempotencia: reconstruir la cohorte hace UPDATE-luego-INSERT por cliente.
--   SEGMENTO codifica el tramo del universo: 'recien_liquidado' (SALDO=0) o
--   'por_liquidar_hueco' (SALDO>0 y < 20% del PRECIO_TOTAL); los valores
--   canónicos están definidos como constantes en el domain VO de Go (segmento.go).
--   EN_CONTROL / FUE_CONTACTADO son SMALLINT (0/1); ambos se fijan en el primer
--   INSERT y NUNCA se sobrescriben en UPDATE (el flag de A/B y el de contacto
--   deben sobrevivir a las reconstrucciones — el canal prende FUE_CONTACTADO en
--   la Fase 3). COHORTE_FECHA se fija igual una sola vez.
--   FECHA_ULTIMA_COMPRA_BASE es la última compra al construir la cohorte
--   (línea base para medir enganche). SALDO (NUMERIC 18,2) y POR_LIQUIDAR_PCT
--   (NUMERIC 5,2) son los totales del cliente en el corte.
--   CREATED_AT y UPDATED_AT siguen la convención de auditoría estándar de MSP.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y valores se pasan
--   desde Go (CLAUDE.md §1). Sin triggers, procedimientos ni generadores.
--   Los invariantes de negocio viven en el domain entity de Go.
-- ============================================================================

CREATE TABLE MSP_RX_COHORTE (
  ID                       CHAR(36)      CHARACTER SET ASCII  NOT NULL,
  CLIENTE_ID               INTEGER                            NOT NULL,
  NOMBRE                   VARCHAR(200),
  TELEFONO                 VARCHAR(40),
  SEGMENTO                 VARCHAR(24)   CHARACTER SET ASCII  NOT NULL,
  EN_CONTROL               SMALLINT                           NOT NULL,
  FUE_CONTACTADO           SMALLINT                           NOT NULL,
  COHORTE_FECHA            TIMESTAMP                          NOT NULL,
  FECHA_ULTIMA_COMPRA_BASE TIMESTAMP,
  SALDO                    NUMERIC(18,2)                      NOT NULL,
  POR_LIQUIDAR_PCT         NUMERIC(5,2),
  CREATED_AT               TIMESTAMP                          NOT NULL,
  UPDATED_AT               TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_RX_COHORTE      PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_RX_COHORTE_CLI  UNIQUE (CLIENTE_ID)
);

CREATE INDEX IDX_MSP_RX_COHORTE_CONTROL   ON MSP_RX_COHORTE (EN_CONTROL);
CREATE INDEX IDX_MSP_RX_COHORTE_CONTACTADO ON MSP_RX_COHORTE (FUE_CONTACTADO);
CREATE INDEX IDX_MSP_RX_COHORTE_SEGMENTO   ON MSP_RX_COHORTE (SEGMENTO);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (44, '000044_create_msp_rx_cohorte', CURRENT_TIMESTAMP);
COMMIT;
