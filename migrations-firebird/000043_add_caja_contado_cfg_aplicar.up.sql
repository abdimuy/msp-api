-- ============================================================================
-- Migración 000043: agrega CAJA_CONTADO_ID y CAJERO_CONTADO_ID a MSP_CFG_APLICAR
-- ============================================================================
--
-- Por qué:
--   Las ventas de contado no tienen zona de cliente, por lo que no pueden
--   resolver su caja a través de MSP_CFG_ZONA_CAJA. En su lugar usan una
--   caja fija de mostrador (CAJA1). Los IDs de esa caja y su cajero se
--   persisten aquí para que la configuración sea editable sin redeployar.
--
--   CAJA_CONTADO_ID:   ID de la caja de mostrador para ventas de contado.
--   CAJERO_CONTADO_ID: ID del cajero de mostrador para ventas de contado.
--   Ambas columnas son nullable: NULL indica que el operador aún no ha
--   configurado los valores (la API rechazará la aplicación si son NULL
--   y el valor lo requiere). La lógica de validación vive en Go (CLAUDE.md §1).
--
-- Restricciones:
--   Sin DEFAULT — los valores se pasan desde Go (CLAUDE.md §1).
--   Sin triggers ni procedimientos. Sin lógica de negocio en la BD.
-- ============================================================================

ALTER TABLE MSP_CFG_APLICAR ADD CAJA_CONTADO_ID INTEGER;
ALTER TABLE MSP_CFG_APLICAR ADD CAJERO_CONTADO_ID INTEGER;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (43, '000043_add_caja_contado_cfg_aplicar', CURRENT_TIMESTAMP);
COMMIT;
