-- ============================================================================
-- Migración 000006 (DOWN): remover UQ_MSP_ROLES_NOMBRE
-- ============================================================================

ALTER TABLE MSP_ROLES
  DROP CONSTRAINT UQ_MSP_ROLES_NOMBRE;

DELETE FROM MSP_MIGRATIONS WHERE ID = 6;

COMMIT;
