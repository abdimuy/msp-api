-- ============================================================================
-- Migración 000039: agrega rejilla mensual V y ticket promedio V a MSP_AN_WINBACK_CANDIDATOS
-- ============================================================================
--
-- Por qué:
--   El motor de recompra/CLV (BG/BB y NBD-Pareto) necesita una cuadrícula de
--   compras mensual V-puro y el ticket promedio V. Las columnas FRECUENCIA y
--   MONETARY existentes agregan TIPO_DOCTO IN ('V','P'), con lo que las filas
--   de cobro en punto de venta (tipo 'P') contaminan ambas métricas y no pueden
--   reutilizarse como insumos del modelo de recompra.
--
-- Columnas:
--   FECHA_PRIMER_VENTA      — MIN(FECHA) de DOCTOS_PV WHERE TIPO_DOCTO='V' AND
--                             ESTATUS='N'. NULL si el cliente no tiene ventas V.
--   FECHA_ULTIMA_VENTA      — MAX(FECHA) de DOCTOS_PV WHERE TIPO_DOCTO='V' AND
--                             ESTATUS='N'. NULL si el cliente no tiene ventas V.
--   VENTAS_MESES_DISTINTOS  — COUNT(DISTINCT mes calendario) sobre ventas V del
--                             cliente. NULL si no hay ventas V.
--   MONETARY_V_PROM         — AVG(IMPORTE_NETO) de ventas V. Proxy del ticket
--                             promedio. NULL si no hay ventas V. NUMERIC(18,2).
--
-- Restricciones:
--   Todas nullable — un cliente nuevo puede no tener historial de ventas V.
--   Sin DEFAULT ni trigger (CLAUDE.md §1). Los valores los pasa Go explícitamente.
--   Sin CHECK de rango — el rango canónico vive en el domain entity de Go.
-- ============================================================================

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS ADD FECHA_PRIMER_VENTA     TIMESTAMP;
ALTER TABLE MSP_AN_WINBACK_CANDIDATOS ADD FECHA_ULTIMA_VENTA     TIMESTAMP;
ALTER TABLE MSP_AN_WINBACK_CANDIDATOS ADD VENTAS_MESES_DISTINTOS INTEGER;
ALTER TABLE MSP_AN_WINBACK_CANDIDATOS ADD MONETARY_V_PROM        NUMERIC(18,2);

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (39, '000039_add_recompra_grid', CURRENT_TIMESTAMP);
COMMIT;
