-- Down para 000059_failed_intents_last_seen.
--
-- Elimina la columna. Es destructivo por definición —se pierde la fecha del
-- último intento de cada fila deduplicada— pero no hay nada que preservar:
-- el dato sólo existe para que la consola pueda escribir "el último hace 4
-- minutos", y sin la columna esa frase tampoco se escribe.
--
-- OJO al orden de reversión: el binario con la dedup escribe esta columna en
-- cada UPDATE. Correr este down con ese binario vivo hace que toda captura
-- repetida falle con "column unknown" y la evidencia se pierda, que es
-- exactamente el fallo que el módulo existe para evitar. **Primero se
-- revierte el binario, después la columna.**

DROP INDEX IDX_MSP_FAILED_INTENTS_DEDUP;
COMMIT;

ALTER TABLE MSP_FAILED_INTENTS DROP LAST_SEEN_AT;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 59;
COMMIT;
