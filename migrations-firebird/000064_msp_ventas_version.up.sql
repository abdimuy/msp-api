-- ============================================================================
-- Migración 000064: MSP_VENTAS.VERSION — control de versión optimista
-- ============================================================================
--
-- Por qué:
--   Hoy el escritorio y el teléfono pueden corregir la misma venta a la vez
--   y el último en escribir pisa al otro sin que nadie se entere — lo
--   documenta, con nombre propio, la prueba-trampa
--   TestConcurrency_PATCHHeader_LastWriteWins
--   (internal/ventas/app/concurrency_test.go:47), cuyo comentario señala
--   exactamente esta columna como el arreglo pendiente. Esta migración es la
--   base estructural de ese arreglo: control de concurrencia optimista por
--   compare-and-swap en cada UPDATE de MSP_VENTAS.
--
--   VERSION arranca en 0 para toda venta existente y sube en 1 en cada
--   escritura de cualquier tipo (edición de header/cliente/líneas,
--   transición de situación, cancelación, aplicación en Microsip). El
--   repositorio (internal/ventas/infra/ventfb) condiciona cada UPDATE a
--   `WHERE ID = ? AND VERSION = ?` y escribe `VERSION = ?` (el valor nuevo)
--   en la misma sentencia — la comparación y el incremento van juntos,
--   nunca en dos pasos separados, porque una ventana entre leer y escribir
--   es exactamente el defecto que esta columna cierra.
--
-- Por qué UNA columna y no UPDATED_AT:
--   UPDATED_AT es un TIMESTAMP de Firebird escrito desde Go vía
--   firebird.ToWallClock, con resolución y round-trip que no está medido
--   para servir de comparador de igualdad exacta, y dos escrituras dentro
--   del mismo tick producirían el mismo valor. Un entero monótono no tiene
--   ninguna de las dos dudas.
--
-- Regla dura #1 de CLAUDE.md — sin lógica en la base de datos:
--   Sin DEFAULT, sin trigger, sin generador, sin CHECK de negocio. El valor
--   siempre lo escribe Go: en cada INSERT (domain.Venta nace con version=0,
--   valor cero de Go) y en cada UPDATE (v.version incrementado por el
--   helper markUpdated antes de persistir). El UPDATE masivo de esta
--   migración (VERSION = 0 para las filas existentes) no es lógica de
--   negocio: es el equivalente a un backfill de un valor constante, la
--   misma clase de operación que 000022 hizo con TX_ID DEFAULT 0.
--
-- Aplicación en tres pasos, con COMMIT entre cada uno (patrón de 000007,
-- 000026 y 000034 en este mismo directorio):
--   1. ADD VERSION nullable — cambio de metadatos, no reescribe filas.
--   2. UPDATE ... SET VERSION = 0 — backfill explícito de las filas
--      existentes. MSP_VENTAS no es una tabla de altísimo volumen (miles,
--      no millones de filas), así que un UPDATE sin WHERE es aceptable aquí.
--   3. ALTER COLUMN VERSION SET NOT NULL — ahora que ninguna fila tiene
--      NULL, la restricción se puede aplicar. Medido: `ALTER TABLE ... ALTER
--      COLUMN ... SET NOT NULL` ya tiene tres precedentes funcionando en
--      este Firebird (000007, 000026, 000034), así que no hace falta la
--      alternativa `DEFAULT 0 NOT NULL` que sugería el plan como plan B.
--
-- OJO al número de migración: el plan original pedía 000063, pero la base
-- de dev compartida (mueblera-firebird) ya tiene el ID 63 aplicado como
-- "000063_create_msp_visitas_imagenes" — un archivo que NO existe en este
-- repositorio (migrations-firebird/ llega hasta 000062 en main). Otra rama
-- en curso aplicó su migración directamente contra la base compartida sin
-- fusionar el archivo. Como fb-migrate-up salta por ID ya presente en
-- MSP_MIGRATIONS, usar 000063 aquí habría hecho que esta migración se
-- reportara "aplicada" sin ejecutar un solo ALTER. Se usa 000064, el
-- siguiente ID verdaderamente libre tanto en los archivos de este repo como
-- en la base de dev compartida.
-- ============================================================================

ALTER TABLE MSP_VENTAS ADD VERSION INTEGER;
COMMIT;

UPDATE MSP_VENTAS SET VERSION = 0;
COMMIT;

ALTER TABLE MSP_VENTAS ALTER COLUMN VERSION SET NOT NULL;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (64, '000064_msp_ventas_version', CURRENT_TIMESTAMP);
COMMIT;
