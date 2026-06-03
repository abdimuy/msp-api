-- ============================================================================
-- Rollback de la migración 000022: eliminar changelog tables + TX_ID columns
-- ============================================================================
-- Deshace en orden inverso todo lo que creó 000022_changelog_and_tx_id.up.sql.
-- ============================================================================

DELETE FROM MSP_MIGRATIONS WHERE ID = 22;
COMMIT;

ALTER TABLE MSP_PAGOS_VENTAS DROP TX_ID;
COMMIT;

ALTER TABLE MSP_SALDOS_VENTAS DROP TX_ID;
COMMIT;

DROP INDEX IDX_MSP_PAGOS_CHANGELOG_TX_ID;
COMMIT;

DROP INDEX IDX_MSP_SALDOS_CHANGELOG_TX_ID;
COMMIT;

DROP TABLE MSP_PAGOS_CHANGELOG;
COMMIT;

DROP TABLE MSP_SALDOS_CHANGELOG;
COMMIT;

DROP GENERATOR GEN_MSP_PAGOS_CHANGELOG_SEQ;
COMMIT;

DROP GENERATOR GEN_MSP_SALDOS_CHANGELOG_SEQ;
COMMIT;
