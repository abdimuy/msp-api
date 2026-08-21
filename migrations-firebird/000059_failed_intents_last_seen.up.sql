-- ============================================================================
-- Migración 000059: MSP_FAILED_INTENTS.LAST_SEEN_AT
-- ============================================================================
--
-- Por qué:
--   Hasta ahora `Save` era un INSERT puro: cada reintento del teléfono
--   escribía fila nueva Y copia nueva del cuerpo con sus fotos. Medido en
--   producción sobre 7 días: **609 filas para 130 ventas distintas**, 685
--   archivos y **896 MB**; una sola venta dejó 13 copias de 2.8 MB.
--
--   Con la dedup por (PATH, IDEMPOTENCY_KEY), una fila pasa a representar
--   varios intentos. Eso cambia lo que significa RECEIVED_AT: deja de ser
--   "cuándo pasó esto" para ser **el PRIMER intento**. El último se pierde,
--   y es justo el dato que hace legible la pantalla:
--
--       "13 intentos · desde 13:20 · el último hace 4 minutos"
--
--   Sin esta columna sólo se puede escribir la mitad de esa frase, y la
--   mitad que queda es la que engaña: una venta cuyo último reintento fue
--   hace cuatro minutos se leería como "de hace seis horas".
--
-- Nullable y sin DEFAULT, las dos cosas a propósito:
--
--   - **Nullable** porque NULL tiene un significado honesto: "esta fila se ha
--     visto una sola vez" (o es anterior a la dedup). El código lo lee como
--     "el último intento ES RECEIVED_AT" en vez de inventar un valor. Un
--     backfill que copiara RECEIVED_AT a todas las filas viejas afirmaría que
--     se vieron dos veces, que es falso.
--   - **Sin DEFAULT** por la regla dura #1 de CLAUDE.md: el valor lo escribe
--     Go con time.Now() envuelto en firebird.ToWallClock, igual que el resto
--     de los timestamps de esta tabla.
--
-- Sin índice SOBRE LA COLUMNA NUEVA: nadie filtra ni ordena por ella. Se lee
--   junto con la fila que ya se localizó por PK o por el índice de STATUS.
--
-- El índice que sí hace falta es otro, y es de la dedup: la captura busca
--   ahora por (IDEMPOTENCY_KEY, PATH, STATUS) en cada 4xx/5xx, y el marcado
--   por éxito posterior hace la misma búsqueda en cada 2xx. Sin índice eso es
--   un barrido de la tabla en la ruta caliente de toda venta que entra —hoy
--   son cientos de filas y no se nota, y el día que se note será por un
--   incidente. La clave va primero por ser la columna selectiva; PATH y
--   STATUS completan el predicado para que la lectura no toque la fila.
--
-- ALTER TABLE ADD de una columna nullable no reescribe las filas existentes
--   en Firebird: es un cambio de metadatos. No hace falta ventana.
-- ============================================================================

ALTER TABLE MSP_FAILED_INTENTS ADD LAST_SEEN_AT TIMESTAMP;
COMMIT;

CREATE INDEX IDX_MSP_FAILED_INTENTS_DEDUP
  ON MSP_FAILED_INTENTS (IDEMPOTENCY_KEY, PATH, STATUS);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (59, '000059_failed_intents_last_seen', CURRENT_TIMESTAMP);
COMMIT;
