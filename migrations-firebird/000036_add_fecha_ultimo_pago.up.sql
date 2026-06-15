-- ============================================================================
-- Migración 000036: agrega FECHA_ULTIMO_PAGO a MSP_AN_WINBACK_CANDIDATOS
-- ============================================================================
--
-- Por qué:
--   La señal de solvencia EstadoPago necesita la fecha del último pago del
--   cliente para distinguir deudores morosos de buenos pagadores dormidos.
--   El valor viene de MAX(MSP_SALDOS_VENTAS.FECHA_ULT_PAGO) por cliente
--   (cargos CARGO_CANCELADO='N') y es mutable (cambia en cada refresh).
--
-- Restricciones:
--   NULLABLE — un cliente nuevo puede no tener historial de pagos.
--   Sin DEFAULT ni trigger (CLAUDE.md §1). El valor lo pasa Go explícitamente.
-- ============================================================================

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD FECHA_ULTIMO_PAGO TIMESTAMP;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (36, '000036_add_fecha_ultimo_pago', CURRENT_TIMESTAMP);
COMMIT;
