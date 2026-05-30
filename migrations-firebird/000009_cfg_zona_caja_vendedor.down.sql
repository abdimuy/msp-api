-- ============================================================================
-- Rollback de la migración 000009: quitar VENDEDOR_ID de MSP_CFG_ZONA_CAJA
-- ============================================================================

ALTER TABLE MSP_CFG_ZONA_CAJA DROP VENDEDOR_ID;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 9;
COMMIT;
