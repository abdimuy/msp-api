-- ============================================================================
-- Rollback de la migración 000010: eliminar MSP_SALDOS_VENTAS y sus objetos
-- ============================================================================
-- Se deshacen en orden inverso a la creación:
--   triggers → procedimiento → generador → índices → tablas → registro.
-- ============================================================================

-- ─── Triggers ────────────────────────────────────────────────────────────────

DROP TRIGGER MSP_SALDOS_CLIENTES_AU;
COMMIT;

DROP TRIGGER MSP_SALDOS_IMPORTES_CC_AIUD;
COMMIT;

DROP TRIGGER MSP_SALDOS_DOCTOS_CC_AD;
COMMIT;

DROP TRIGGER MSP_SALDOS_DOCTOS_CC_AIU;
COMMIT;

-- ─── Procedimiento ───────────────────────────────────────────────────────────

DROP PROCEDURE MSP_RECOMPUTE_SALDO_VENTA;
COMMIT;

-- ─── Generador ───────────────────────────────────────────────────────────────

DROP GENERATOR GEN_MSP_SALDOS_ERRORS_ID;
COMMIT;

-- ─── Índices (Firebird elimina índices implícitamente con la tabla,
--     pero se listan explícitamente por claridad y en caso de que la tabla
--     ya no exista pero los índices sí, p. ej. tras un fallo parcial). ────────

DROP INDEX IDX_MSP_SALDOS_FECHA_CARGO;
COMMIT;

DROP INDEX IDX_MSP_SALDOS_CLIENTE;
COMMIT;

DROP INDEX IDX_MSP_SALDOS_ZONA_FUP;
COMMIT;

DROP INDEX IDX_MSP_SALDOS_ZONA_SALDO;
COMMIT;

DROP INDEX IDX_MSP_SALDOS_PV;
COMMIT;

-- ─── Tablas ──────────────────────────────────────────────────────────────────

DROP TABLE MSP_SALDOS_ERRORS;
COMMIT;

DROP TABLE MSP_SALDOS_VENTAS;
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────

DELETE FROM MSP_MIGRATIONS WHERE ID = 10;
COMMIT;
