-- ============================================================================
-- Rollback de la migración 000008: tablas de configuración "aplicar venta"
-- ============================================================================
-- Elimina las cinco tablas MSP_CFG_* de configuración del mapeo.
-- ============================================================================

DROP TABLE MSP_CFG_APLICAR;
DROP TABLE MSP_CFG_NUM_VENDEDORES;
DROP TABLE MSP_CFG_PLAZO_CREDITO;
DROP TABLE MSP_CFG_FRECUENCIA_FORMA_PAGO;
DROP TABLE MSP_CFG_ZONA_CAJA;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 8;
COMMIT;
