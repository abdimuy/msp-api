-- Recalcula la selectividad de los índices de NUESTRAS tablas (MSP_*).
--
-- Procedimiento completo, cuándo correrlo y por qué no es un worker:
-- docs/ops/estadisticas-indices.md. Esto NO es lógica en la base (CLAUDE.md
-- §1): no crea procedimientos, triggers ni bloques guardados; sólo le pide a
-- Firebird que vuelva a medir un dato que únicamente calcula al crear el
-- índice.
--
-- NO es de solo lectura: SET STATISTICS es una sentencia de metadatos. No
-- cambia ninguna fila ni estructura, pero sí puede cambiar el PLAN que el
-- optimizador escoge desde ese momento. Por eso se corre con el antes/después
-- de scripts/medicion/desempeno-indices-produccion.sql.
--
-- Dos pasos, sin bloques ni EXECUTE STATEMENT: el primero GENERA las
-- sentencias en un archivo y el segundo lo ejecuta.
--
-- Uso (servidor Windows, isql de Firebird 5; ver despliegue-produccion.md §11):
--   isql.exe -user <USUARIO> -password <CONTRASENA> ^
--     localhost:<RUTA_FDB> -i C:\recalcular-estadisticas-indices.sql
-- El archivo intermedio queda en C:\msp-set-statistics.sql y se puede revisar.

SET HEADING OFF;
SET LIST OFF;
SET COUNT OFF;

-- Paso 0: cómo estaban (para el registro del antes).
SET HEADING ON;
SELECT CAST(TRIM(i.RDB$RELATION_NAME) AS VARCHAR(40)) AS TABLA,
       CAST(TRIM(i.RDB$INDEX_NAME) AS VARCHAR(40))    AS INDICE,
       i.RDB$STATISTICS                                AS SELECTIVIDAD
FROM RDB$INDICES i
WHERE i.RDB$RELATION_NAME STARTING WITH 'MSP_'
  AND COALESCE(i.RDB$INDEX_INACTIVE, 0) = 0
ORDER BY 1, 2;
SET HEADING OFF;

-- Paso 1: generar.
OUTPUT C:\msp-set-statistics.sql;
SELECT 'SET STATISTICS INDEX ' || TRIM(i.RDB$INDEX_NAME) || ';'
FROM RDB$INDICES i
WHERE i.RDB$RELATION_NAME STARTING WITH 'MSP_'
  AND COALESCE(i.RDB$INDEX_INACTIVE, 0) = 0
ORDER BY i.RDB$RELATION_NAME, i.RDB$INDEX_NAME;
SELECT 'COMMIT;' FROM RDB$DATABASE;
OUTPUT;

-- Paso 2: ejecutar.
INPUT C:\msp-set-statistics.sql;

-- Paso 3: cómo quedaron.
SET HEADING ON;
SELECT CAST(TRIM(i.RDB$RELATION_NAME) AS VARCHAR(40)) AS TABLA,
       CAST(TRIM(i.RDB$INDEX_NAME) AS VARCHAR(40))    AS INDICE,
       i.RDB$STATISTICS                                AS SELECTIVIDAD
FROM RDB$INDICES i
WHERE i.RDB$RELATION_NAME STARTING WITH 'MSP_'
  AND COALESCE(i.RDB$INDEX_INACTIVE, 0) = 0
ORDER BY 1, 2;
