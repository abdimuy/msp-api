-- ============================================================================
-- Migración 000033: ensanchar MSP_VENTAS.NUMERO_EXTERIOR a VARCHAR(50)
-- ============================================================================
--
-- Por qué:
--   El número exterior del domicilio (p. ej. "MZ 5 LT 23 LOCAL 4 INT B") supera
--   con frecuencia los 20 caracteres. En Firebird 3 el INSERT truncaba en
--   silencio; en Firebird 5 lanza error -303 (string right truncation). En el
--   msp-api el dominio rechaza el valor con `numero_exterior_too_long` antes de
--   llegar a la BD, lo que bloquea direcciones legítimas. Se ensancha la columna
--   a VARCHAR(50) y se sube `maxNumeroExteriorLength` en el dominio en paralelo
--   para mantenerlos en sync (la constante espeja el ancho de la columna).
--
--   Ensanchar un VARCHAR es metadata-only en Firebird: no reescribe filas y no
--   rompe a ningún lector. Mantiene CHARACTER SET UTF8 (fijado en 000005).
-- ============================================================================

ALTER TABLE MSP_VENTAS ALTER COLUMN NUMERO_EXTERIOR TYPE VARCHAR(50) CHARACTER SET UTF8;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (33, '000033_widen_numero_exterior', CURRENT_TIMESTAMP);
COMMIT;
