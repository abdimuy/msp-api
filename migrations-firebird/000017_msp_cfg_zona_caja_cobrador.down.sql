-- ============================================================================
-- Rollback de la migración 000017: quitar COBRADOR_ID de MSP_CFG_ZONA_CAJA
-- ============================================================================

ALTER TABLE MSP_CFG_ZONA_CAJA DROP COBRADOR_ID;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 17;
COMMIT;
