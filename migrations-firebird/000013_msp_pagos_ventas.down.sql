-- ============================================================================
-- Rollback de la migración 000013: eliminar MSP_PAGOS_VENTAS y sus objetos
-- ============================================================================
-- Se deshacen en orden inverso a la creación:
--   triggers → procedimiento → índices → tabla → registro.
-- MSP_SALDOS_ERRORS y GEN_MSP_SALDOS_ERRORS_ID NO se tocan: son compartidos
-- con 000010.
-- ============================================================================

-- ─── Triggers ────────────────────────────────────────────────────────────────

DROP TRIGGER MSP_PAGOS_RECIBIDOS_AIUD;
COMMIT;

DROP TRIGGER MSP_PAGOS_CLIENTES_AU;
COMMIT;

DROP TRIGGER MSP_PAGOS_DOCTOS_CC_AU;
COMMIT;

DROP TRIGGER MSP_PAGOS_IMPORTES_AIUD;
COMMIT;

-- ─── Procedimiento ───────────────────────────────────────────────────────────

DROP PROCEDURE MSP_RECOMPUTE_PAGO;
COMMIT;

-- ─── Índices (Firebird los elimina implícitamente con la tabla, pero los
--     listamos explícitamente por simetría y para tolerar fallos parciales) ──

DROP INDEX IDX_MSP_PAGOS_CLIENTE_FECHA;
COMMIT;

DROP INDEX IDX_MSP_PAGOS_CARGO;
COMMIT;

DROP INDEX IDX_MSP_PAGOS_ZONA_UPDATED;
COMMIT;

DROP INDEX IDX_MSP_PAGOS_ZONA_FECHA;
COMMIT;

-- ─── Tabla ───────────────────────────────────────────────────────────────────

DROP TABLE MSP_PAGOS_VENTAS;
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────

DELETE FROM MSP_MIGRATIONS WHERE ID = 13;
COMMIT;
