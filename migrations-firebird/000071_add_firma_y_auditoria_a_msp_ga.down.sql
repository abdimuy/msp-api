-- ============================================================================
-- Rollback de la migración 000071: firma, huella y auditoría GPS
-- ============================================================================
-- Orden inverso al up.
-- ============================================================================

ALTER TABLE MSP_GA_EVENTO DROP AUDITORIA_GPS;
COMMIT;

ALTER TABLE MSP_GA_EVENTO DROP DISTANCIA_ENTREGA_M;
COMMIT;

ALTER TABLE MSP_GA_EVENTO DROP HUELLA;
COMMIT;

ALTER TABLE MSP_GA_IMAGEN DROP HUELLA;
COMMIT;

ALTER TABLE MSP_GA_IMAGEN DROP TIPO;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 71;
COMMIT;
