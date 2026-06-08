-- ============================================================================
-- Migración 000027: MSP_VENTAS_TRASPASOS
-- ============================================================================
--
-- Por qué existe esta tabla:
--   Cuando una venta es aprobada, el módulo de inventario genera uno o más
--   traspasos en Microsip (DOCTOS_IN). Esta tabla registra la relación entre
--   MSP_VENTAS.ID y cada DOCTO_IN_ID generado, de modo que los endpoints de
--   administración puedan encontrar los traspasos asociados a una venta sin
--   tener que escanear DOCTOS_IN.
--
-- Propiedad:
--   Tabla propia de MSP; ningún trigger de Microsip la toca. Los valores de
--   ID, CREATED_AT, CREATED_BY y FOLIO se pasan explícitamente desde Go en
--   cada INSERT.
--
-- Restricciones CHECK:
--   CK_MSP_VTA_TRASPASOS_TIPO y CK_MSP_VTA_TRASPASOS_ALMACENES_DIF son
--   guardarraíles defensivos. La regla canónica vive en la entidad de dominio
--   en Go; las CHECK sólo previenen inconsistencias por escrituras directas
--   a la BD.
--
-- Requiere: migración 000002 aplicada (MSP_VENTAS debe existir).
-- ============================================================================

CREATE TABLE MSP_VENTAS_TRASPASOS (
  ID               CHAR(36)    CHARACTER SET ASCII NOT NULL,
  VENTA_ID         CHAR(36)    CHARACTER SET ASCII NOT NULL,
  DOCTO_IN_ID      INTEGER     NOT NULL,
  TIPO             VARCHAR(10) CHARACTER SET ASCII NOT NULL,
  FOLIO            CHAR(9)     CHARACTER SET ASCII NOT NULL,
  ALMACEN_ORIGEN   INTEGER     NOT NULL,
  ALMACEN_DESTINO  INTEGER     NOT NULL,
  CREATED_AT       TIMESTAMP   NOT NULL,
  CREATED_BY       CHAR(36)    CHARACTER SET ASCII NOT NULL,

  CONSTRAINT PK_MSP_VENTAS_TRASPASOS           PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_VTA_TRASPASOS_VENTA        FOREIGN KEY (VENTA_ID) REFERENCES MSP_VENTAS (ID),
  CONSTRAINT CK_MSP_VTA_TRASPASOS_TIPO         CHECK (TIPO IN ('directo', 'reverso')),
  CONSTRAINT CK_MSP_VTA_TRASPASOS_ALMACENES_DIF CHECK (ALMACEN_ORIGEN <> ALMACEN_DESTINO)
);

CREATE INDEX IDX_MSP_VTA_TRASPASOS_VENTA    ON MSP_VENTAS_TRASPASOS (VENTA_ID);
CREATE INDEX IDX_MSP_VTA_TRASPASOS_DOCTO_IN ON MSP_VENTAS_TRASPASOS (DOCTO_IN_ID);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (27, '000027_create_msp_ventas_traspasos', CURRENT_TIMESTAMP);
COMMIT;
