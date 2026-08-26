-- ============================================================================
-- Migración 000061: MSP_FAILED_INTENTS.MODULO + MSP_FAILED_INTENTS.RESUMEN
-- ============================================================================
--
-- Por qué:
--   La pantalla de intentos fallidos muestra "Sin nombre capturado" en las
--   19 filas de ventas, y no es un defecto de la UI: **el dato nunca llega**.
--   El frontend deducía el nombre y el monto leyendo el cuerpo capturado, y
--   una venta lleva fotos —o sea multipart—, y en esa ruta BODY se guarda
--   vacío A PROPÓSITO: el cuerpo vive en disco (BODY_BLOB_PATH) y la fila
--   sólo tiene `null`. El deducir-del-cuerpo devolvía nulo siempre.
--
--   La respuesta NO es que el listado abra los blobs. Existe
--   GET /{id}/blob-parts, pero usarlo por renglón convierte la ruta más
--   caliente de la pantalla en un N+1 sobre el disco. El resumen se extrae
--   UNA VEZ, en la captura, y viaja en la fila.
--
-- MODULO — columna real e indexada, no derivada del PATH en memoria:
--   Los chips *Todo / Ventas / Pagos* filtran por módulo. Filtrar en memoria
--   sobre una página paginada da resultados mentirosos: "Ventas" mostraría
--   sólo las ventas que cupieron en los primeros 20 renglones. El filtro
--   tiene que ser SQL, y para eso la columna tiene que existir e indexarse.
--
--   VARCHAR(40) ASCII: es un identificador interno ('ventas', 'cobranza'),
--   no texto de usuario. Mismo tratamiento que STATUS y METHOD.
--
-- RESUMEN — JSON {titulo, monto, referencia}, no tres columnas:
--   Tres columnas obligarían a una migración cada vez que un módulo nuevo
--   necesite un campo distinto. Con el JSON, añadir un módulo es una
--   implementación en Go y una línea de registro; el esquema no se mueve.
--
--   Deja sitio, además, para lo que quedó fuera de alcance a propósito: el
--   detalle del 422 de existencias ("falta 1 base Leos Venecia; hay 16 en
--   Almacén General"). El día que se capture, es una llave más en el JSON.
--
-- Las dos nullable, y eso es lo honesto:
--   Las 34 filas que ya existen no tienen ni módulo ni resumen. NULL
--   significa "no se extrajo" — que es exactamente lo que pasó. Rellenarlas
--   con '' o con un módulo adivinado del PATH afirmaría una extracción que
--   nunca corrió.
--
--   Se iluminan solas: el janitor —que ya recorre filas fuera de la ruta
--   caliente— abre el blob una vez, extrae y guarda. Sin N+1 y sin migración
--   de datos.
--
-- Sin DEFAULT y sin trigger, por la regla dura #1 de CLAUDE.md: el valor lo
--   escribe Go, siempre.
--
-- Índice (MODULO, RECEIVED_AT), con la misma forma que IDX_..._STATUS:
--   el listado siempre ordena por RECEIVED_AT DESC, así que la columna de
--   orden acompaña a la de filtro. MODULO va primero por ser la selectiva.
--
-- ALTER TABLE ADD de columnas nullable no reescribe las filas existentes en
--   Firebird: es un cambio de metadatos. No hace falta ventana.
--
-- ORDEN DE DESPLIEGUE: esta migración va SOLA y PRIMERO, sin binario. No
--   rompe nada —las columnas son nullable y nadie las lee todavía—. El
--   binario con los extractores va después; el escritorio, al final. Al
--   revés el escritorio muestra huecos donde espera resumen, que es el mismo
--   error que ya se cometió con el APK y la migración 000056.
-- ============================================================================

ALTER TABLE MSP_FAILED_INTENTS ADD MODULO VARCHAR(40) CHARACTER SET ASCII;
COMMIT;

ALTER TABLE MSP_FAILED_INTENTS ADD RESUMEN BLOB SUB_TYPE TEXT SEGMENT SIZE 512 CHARACTER SET UTF8;
COMMIT;

CREATE INDEX IDX_MSP_FAILED_INTENTS_MODULO
  ON MSP_FAILED_INTENTS (MODULO, RECEIVED_AT);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (61, '000061_failed_intents_modulo_resumen', CURRENT_TIMESTAMP);
COMMIT;
