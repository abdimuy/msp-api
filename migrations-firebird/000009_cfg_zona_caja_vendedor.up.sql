-- ============================================================================
-- Migración 000009: agregar VENDEDOR_ID a MSP_CFG_ZONA_CAJA
-- ============================================================================
-- El adapter de materialización (POST /ventas/{id}/aplicar) llena
-- LIBRES_CARGOS_CC.VENDEDOR_1 con el VENDEDOR_ID de Microsip que corresponde a
-- la zona/ruta del cliente — mismo patrón que CAJA_ID/CAJERO_ID. En Microsip
-- los vendedores están nombrados por ruta (RUTA01…RUTA39), así que el mapeo se
-- resuelve uniendo por nombre de caja/ruta. Las zonas sin vendedor-ruta (p. ej.
-- MAYOREO) quedan en -1 (centinela "ninguno"). Estos campos en Microsip son de
-- consulta rápida; la atribución real de vendedores vive en MSP_VENTAS_VENDEDORES.
-- ============================================================================

ALTER TABLE MSP_CFG_ZONA_CAJA ADD VENDEDOR_ID INTEGER;
COMMIT;

-- Backfill: VENDEDOR_ID por nombre de ruta (CAJAS.NOMBRE = VENDEDORES.NOMBRE).
UPDATE MSP_CFG_ZONA_CAJA zc
   SET VENDEDOR_ID = (
       SELECT v.VENDEDOR_ID
       FROM CAJAS c
       JOIN VENDEDORES v ON v.NOMBRE = c.NOMBRE
       WHERE c.CAJA_ID = zc.CAJA_ID
   );

-- Zonas sin match (MAYOREO, etc.) → -1 (ninguno).
UPDATE MSP_CFG_ZONA_CAJA SET VENDEDOR_ID = -1 WHERE VENDEDOR_ID IS NULL;
COMMIT;

ALTER TABLE MSP_CFG_ZONA_CAJA ALTER COLUMN VENDEDOR_ID SET NOT NULL;
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (9, '000009_cfg_zona_caja_vendedor', CURRENT_TIMESTAMP);
COMMIT;
