-- ============================================================================
-- Migración 000037: agrega señales de puntualidad de cobranza a MSP_AN_WINBACK_CANDIDATOS
-- ============================================================================
--
-- Por qué:
--   El motor de inteligencia de ventas necesita señales de cadencia y puntualidad
--   de pago para clasificar el riesgo de cobranza (TierRiesgo) a tiempo de lectura.
--   Todos los valores se materializan desde MSP_PAGOS_VENTAS (filtro CANCELADO='N'
--   AND APLICADO='S') usando una ventana LAG(FECHA) por CLIENTE_ID.
--
-- Decisión de filtro (verificado en BD live):
--   CANCELADO='N' AND APLICADO='S' captura 2,170,485 de 2,173,381 filas.
--   Los 2,896 excluidos son cancelaciones (CANCELADO='S'). No se filtra por
--   CONCEPTO_CC_ID — todos los conceptos encontrados representan abonos reales.
--
-- Columnas:
--   NUM_PAGOS         — total de pagos aplicados (gaps computados = NUM_PAGOS - 1).
--   CADENCIA_DIAS     — promedio de días entre pagos consecutivos; NULL si <2 pagos.
--   DIAS_ATRASO_PROM  — promedio de max(0, gap − cadencia) sobre todos los gaps;
--                       NULL si no hay suficientes datos.
--   PCT_PAGOS_A_TIEMPO — % de gaps dentro de cadencia + 7 días de tolerancia;
--                        NULL si no hay suficientes datos. NUMERIC(5,2).
--   FECHA_PROX_PAGO   — último pago + cadencia; NULL si sin cadencia.
--   MONTO_PROX_PAGO   — promedio del IMPORTE de abonos como proxy de cuota;
--                        NULL si sin datos. NUMERIC(18,2).
--
-- Restricciones:
--   Todas nullable — un cliente nuevo puede no tener historial.
--   Sin DEFAULT ni trigger (CLAUDE.md §1). Los valores los pasa Go explícitamente.
--   Sin CHECK de rango — el rango canónico vive en el domain entity de Go.
-- ============================================================================

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD NUM_PAGOS          INTEGER;

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD CADENCIA_DIAS      INTEGER;

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD DIAS_ATRASO_PROM   INTEGER;

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD PCT_PAGOS_A_TIEMPO NUMERIC(5,2);

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD FECHA_PROX_PAGO    TIMESTAMP;

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD MONTO_PROX_PAGO    NUMERIC(18,2);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (37, '000037_add_cobranza_signals', CURRENT_TIMESTAMP);
COMMIT;
