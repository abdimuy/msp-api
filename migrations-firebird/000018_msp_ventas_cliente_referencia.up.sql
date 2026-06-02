-- ============================================================================
-- Migración 000018: agregar CLIENTE_REFERENCIA a MSP_VENTAS
-- ============================================================================
-- Optional referencia de ubicación capturada en el showroom (p. ej. "casa
-- azul esquina"). Se persiste en el snapshot de la venta y, al aplicar,
-- se copia a LIBRES_CLIENTES.REFERENCIA cuando el flujo auto-crea el
-- cliente en Microsip. Nullable porque ventas existentes no la tenían.
-- ============================================================================

ALTER TABLE MSP_VENTAS ADD CLIENTE_REFERENCIA VARCHAR(99) CHARACTER SET UTF8;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (18, '000018_msp_ventas_cliente_referencia', CURRENT_TIMESTAMP);
COMMIT;
