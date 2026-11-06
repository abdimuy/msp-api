-- ============================================================================
-- Migración 000071: firma, huella y auditoría GPS del módulo de garantías
-- ============================================================================
--
-- Agrega los datos necesarios para:
--   - distinguir imágenes de evidencia y firma;
--   - guardar la huella SHA-256 usada por la idempotencia;
--   - registrar la distancia y resultado de la auditoría GPS al cerrar.
--
-- Todas las columnas son nullable y no tienen DEFAULT, CHECK ni trigger.
-- Las filas anteriores a esta migración quedan con NULL; en Go, una imagen
-- con TIPO NULL se interpreta como evidencia.
-- ============================================================================

ALTER TABLE MSP_GA_IMAGEN ADD TIPO VARCHAR(12) CHARACTER SET ASCII;
COMMIT;

ALTER TABLE MSP_GA_IMAGEN ADD HUELLA CHAR(64) CHARACTER SET ASCII;
COMMIT;

ALTER TABLE MSP_GA_EVENTO ADD HUELLA CHAR(64) CHARACTER SET ASCII;
COMMIT;

ALTER TABLE MSP_GA_EVENTO ADD DISTANCIA_ENTREGA_M DOUBLE PRECISION;
COMMIT;

ALTER TABLE MSP_GA_EVENTO ADD AUDITORIA_GPS VARCHAR(20) CHARACTER SET ASCII;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (71, '000071_add_firma_y_auditoria_a_msp_ga', CURRENT_TIMESTAMP);

COMMIT;
