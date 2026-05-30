-- ============================================================================
-- Migración 000007: modelo de estados de 3 dimensiones en MSP_VENTAS
-- ============================================================================
-- Reemplaza el enum único STATUS (borrador/aprobada/cancelada) por tres
-- dimensiones independientes, alineado con el diseño aprobado
-- (docs/superpowers/specs/2026-05-22-aplicar-venta-local-microsip-design.md):
--
--   STATUS         → existencia técnica: 'active' | 'deleted' (soft-delete).
--   SITUACION      → ciclo de negocio:   'borrador' | 'revisada' | 'aprobada' | 'cancelada'.
--   SINCRONIZACION → frente a Microsip:  'pendiente' | 'aplicada'.
--
-- Más los artefactos de materialización (respaldan SINCRONIZACION='aplicada'):
--   MICROSIP_DOCTO_PV_ID, MICROSIP_FOLIO, MICROSIP_APLICADA_AT.
--
-- Backfill: el STATUS actual (borrador/aprobada/cancelada) se mueve a SITUACION;
-- STATUS pasa a 'active' para todas las filas (todavía no hay soft-delete);
-- SINCRONIZACION arranca en 'pendiente' (ninguna venta aplicada aún).
--
-- Requiere: migración 000003 aplicada (creó STATUS + CK_MSP_VENTAS_STATUS).
-- COMMITs intermedios para evitar conflictos DDL/DML en Firebird al redefinir
-- el CHECK de STATUS dentro de la misma operación.
-- ============================================================================

-- ─── 1) Columnas nuevas ──────────────────────────────────────────────────────
ALTER TABLE MSP_VENTAS ADD SITUACION            VARCHAR(30) CHARACTER SET ASCII;
ALTER TABLE MSP_VENTAS ADD SINCRONIZACION       VARCHAR(15) CHARACTER SET ASCII;
ALTER TABLE MSP_VENTAS ADD MICROSIP_DOCTO_PV_ID INTEGER;
ALTER TABLE MSP_VENTAS ADD MICROSIP_FOLIO       CHAR(9) CHARACTER SET ASCII;
ALTER TABLE MSP_VENTAS ADD MICROSIP_APLICADA_AT TIMESTAMP;
COMMIT;

-- ─── 2) Backfill desde el STATUS actual ──────────────────────────────────────
UPDATE MSP_VENTAS SET SITUACION = STATUS;
UPDATE MSP_VENTAS SET SINCRONIZACION = 'pendiente';
COMMIT;

-- ─── 3) Repurpose de STATUS → existencia técnica ─────────────────────────────
ALTER TABLE MSP_VENTAS DROP CONSTRAINT CK_MSP_VENTAS_STATUS;
COMMIT;
UPDATE MSP_VENTAS SET STATUS = 'active';
COMMIT;

-- ─── 4) NOT NULL + CHECKs ────────────────────────────────────────────────────
ALTER TABLE MSP_VENTAS ALTER COLUMN SITUACION      SET NOT NULL;
ALTER TABLE MSP_VENTAS ALTER COLUMN SINCRONIZACION SET NOT NULL;

ALTER TABLE MSP_VENTAS
  ADD CONSTRAINT CK_MSP_VENTAS_STATUS
  CHECK (STATUS IN ('active', 'deleted'));

ALTER TABLE MSP_VENTAS
  ADD CONSTRAINT CK_MSP_VENTAS_SITUACION
  CHECK (SITUACION IN ('borrador', 'revisada', 'aprobada', 'cancelada'));

ALTER TABLE MSP_VENTAS
  ADD CONSTRAINT CK_MSP_VENTAS_SINCRONIZACION
  CHECK (SINCRONIZACION IN ('pendiente', 'aplicada'));

-- Invariante artefactos ↔ aplicada: aplicada ⟺ docto y folio presentes.
ALTER TABLE MSP_VENTAS
  ADD CONSTRAINT CK_MSP_VENTAS_APLICADA_COHERENTE CHECK (
    (SINCRONIZACION = 'aplicada'
      AND MICROSIP_DOCTO_PV_ID IS NOT NULL
      AND MICROSIP_FOLIO       IS NOT NULL)
    OR (SINCRONIZACION = 'pendiente'
      AND MICROSIP_DOCTO_PV_ID IS NULL
      AND MICROSIP_FOLIO       IS NULL)
  );
COMMIT;

-- ─── 5) Índices ──────────────────────────────────────────────────────────────
CREATE INDEX IDX_MSP_VENTAS_SITUACION      ON MSP_VENTAS (SITUACION);
CREATE INDEX IDX_MSP_VENTAS_SINCRONIZACION ON MSP_VENTAS (SINCRONIZACION);
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (7, '000007_ventas_estado_3d', CURRENT_TIMESTAMP);

COMMIT;
