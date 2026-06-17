-- ============================================================================
-- Migración 000038: agrega señales de crédito v1 a MSP_AN_WINBACK_CANDIDATOS
-- ============================================================================
--
-- Por qué:
--   El scorecard de crédito v1 (v1-pit-20260617) incorpora PAGOS_90D
--   (pagos recientes en los últimos 90 días) y ANTIGUEDAD_DIAS (días desde
--   el primer cargo). Estas dos columnas materializan los insumos necesarios
--   para el feature DIAS_SIN_PAGAR cuando FechaUltimoPago es cero, y para
--   la señal de frescura de pago reciente.
--
-- Columnas:
--   PAGOS_90D        — conteo de abonos reales (conceptos 87327/155/11) en los
--                      últimos 90 días. NULL si no hay historia de pagos.
--   FECHA_PRIMER_CARGO — fecha más antigua en MSP_SALDOS_VENTAS.FECHA_CARGO
--                        para el cliente. NULL si no tiene cargos.
--
-- Restricciones:
--   Todas nullable — un cliente nuevo puede no tener historial.
--   Sin DEFAULT ni trigger (CLAUDE.md §1). Los valores los pasa Go explícitamente.
--   Sin CHECK de rango — el rango canónico vive en el domain entity de Go.
-- ============================================================================

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD PAGOS_90D          INTEGER;

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD FECHA_PRIMER_CARGO TIMESTAMP;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (38, '000038_add_credito_features', CURRENT_TIMESTAMP);
COMMIT;
