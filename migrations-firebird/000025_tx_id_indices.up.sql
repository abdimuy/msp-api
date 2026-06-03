-- ============================================================================
-- Migración 000025: índices en TX_ID de MSP_PAGOS_VENTAS y MSP_SALDOS_VENTAS
-- ============================================================================
--
-- El cursor sync (commit 7 del sprint cobranza push-channel) agrega el
-- predicado `AND TX_ID < :watermark` a las queries de pagos y saldos.
-- Sin estos índices, cada poll ejecuta un table scan sobre ~350 k filas.
--
-- MSP_PAGOS_VENTAS.TX_ID  — usado por queryPagoSyncPage
-- MSP_SALDOS_VENTAS.TX_ID — usado por querySyncPage (saldos path)
--
-- Las columnas TX_ID fueron agregadas por la migración 000022 con DEFAULT 0.
-- Las filas existentes tienen TX_ID = 0, que está por debajo de cualquier
-- MIN(MON$TRANSACTION_ID) activo, así que siempre quedan dentro del watermark.
-- ============================================================================

CREATE INDEX IDX_MSP_PAGOS_VENTAS_TX_ID ON MSP_PAGOS_VENTAS (TX_ID);
COMMIT;

CREATE INDEX IDX_MSP_SALDOS_VENTAS_TX_ID ON MSP_SALDOS_VENTAS (TX_ID);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (25, '000025_tx_id_indices', CURRENT_TIMESTAMP);
COMMIT;
