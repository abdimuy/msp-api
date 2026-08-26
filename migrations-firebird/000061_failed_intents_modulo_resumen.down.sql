-- Down para 000061_failed_intents_modulo_resumen.
--
-- Elimina el índice y las dos columnas. Es destructivo por definición —se
-- pierde el módulo y el resumen de cada fila— pero no hay nada que preservar:
-- ambos son datos DERIVADOS del cuerpo capturado, que sigue en su sitio
-- (BODY o BODY_BLOB_PATH). Volver a aplicar el up y dejar pasar al janitor
-- los reconstruye.
--
-- OJO al orden de reversión: el binario con los extractores escribe estas dos
-- columnas en cada captura y las lee en cada listado. Correr este down con
-- ese binario vivo hace que TODA captura de un 4xx/5xx falle con
-- "column unknown" — y la captura es justo lo que evita perder la venta.
-- **Primero se revierte el binario, después las columnas.**

DROP INDEX IDX_MSP_FAILED_INTENTS_MODULO;
COMMIT;

ALTER TABLE MSP_FAILED_INTENTS DROP RESUMEN;
COMMIT;

ALTER TABLE MSP_FAILED_INTENTS DROP MODULO;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 61;
COMMIT;
