-- ============================================================================
-- Rollback de la migración 000007: modelo de estados de 3 dimensiones
-- ============================================================================
-- Colapsa las 3 dimensiones de vuelta al enum único STATUS
-- (borrador/aprobada/cancelada) y elimina las columnas nuevas.
--
-- Mapeo inverso de SITUACION → STATUS: 'revisada' no existía en el enum viejo,
-- así que cae a 'borrador'; 'aprobada' y 'cancelada' se conservan. Cualquier
-- venta ya 'aplicada' pierde sus artefactos Microsip en este rollback.
-- ============================================================================

-- ─── 1) Índices nuevos ───────────────────────────────────────────────────────
DROP INDEX IDX_MSP_VENTAS_SINCRONIZACION;
DROP INDEX IDX_MSP_VENTAS_SITUACION;
COMMIT;

-- ─── 2) CHECKs nuevos (incluye el STATUS active/deleted) ─────────────────────
ALTER TABLE MSP_VENTAS DROP CONSTRAINT CK_VTA_APLICADA_COHERENTE;
ALTER TABLE MSP_VENTAS DROP CONSTRAINT CK_MSP_VENTAS_SINCRONIZACION;
ALTER TABLE MSP_VENTAS DROP CONSTRAINT CK_MSP_VENTAS_SITUACION;
ALTER TABLE MSP_VENTAS DROP CONSTRAINT CK_MSP_VENTAS_STATUS;
COMMIT;

-- ─── 3) Restituir STATUS desde SITUACION ─────────────────────────────────────
UPDATE MSP_VENTAS
   SET STATUS = CASE
       WHEN SITUACION IN ('aprobada', 'cancelada') THEN SITUACION
       ELSE 'borrador'
   END;
COMMIT;

ALTER TABLE MSP_VENTAS
  ADD CONSTRAINT CK_MSP_VENTAS_STATUS
  CHECK (STATUS IN ('borrador', 'aprobada', 'cancelada'));
COMMIT;

-- ─── 4) Dropear columnas nuevas ──────────────────────────────────────────────
ALTER TABLE MSP_VENTAS DROP MICROSIP_APLICADA_AT;
ALTER TABLE MSP_VENTAS DROP MICROSIP_FOLIO;
ALTER TABLE MSP_VENTAS DROP MICROSIP_DOCTO_PV_ID;
ALTER TABLE MSP_VENTAS DROP SINCRONIZACION;
ALTER TABLE MSP_VENTAS DROP SITUACION;
COMMIT;

-- ─── 5) Quitar la migración del registro ─────────────────────────────────────
DELETE FROM MSP_MIGRATIONS WHERE ID = 7;

COMMIT;
