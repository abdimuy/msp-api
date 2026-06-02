-- ============================================================================
-- Migración 000017: agregar COBRADOR_ID a MSP_CFG_ZONA_CAJA
-- ============================================================================
-- Para auto-crear clientes desde el flujo AplicarVenta necesitamos saber el
-- COBRADOR_ID por zona. El cobrador asignado es el más frecuente entre los
-- clientes existentes de cada zona. Las zonas sin clientes (p. ej. MAYOREO)
-- quedan en -1 (centinela "ninguno"). Espejo de la migración 000009.
-- ============================================================================

ALTER TABLE MSP_CFG_ZONA_CAJA ADD COBRADOR_ID INTEGER;
COMMIT;

-- Backfill: cobrador más frecuente por zona.
UPDATE MSP_CFG_ZONA_CAJA zc
   SET COBRADOR_ID = (
       SELECT FIRST 1 c.COBRADOR_ID
       FROM CLIENTES c
       WHERE c.ZONA_CLIENTE_ID = zc.ZONA_CLIENTE_ID
         AND c.COBRADOR_ID IS NOT NULL
       GROUP BY c.COBRADOR_ID
       ORDER BY COUNT(*) DESC
   );

-- Zonas sin clientes (MAYOREO, etc.) → -1 (ninguno).
UPDATE MSP_CFG_ZONA_CAJA SET COBRADOR_ID = -1 WHERE COBRADOR_ID IS NULL;
COMMIT;

ALTER TABLE MSP_CFG_ZONA_CAJA ALTER COLUMN COBRADOR_ID SET NOT NULL;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (17, '000017_msp_cfg_zona_caja_cobrador', CURRENT_TIMESTAMP);
COMMIT;
