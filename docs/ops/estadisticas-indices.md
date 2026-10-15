# Estadísticas de índices en Firebird: quién las recalcula y cuándo

Firebird guarda en `RDB$INDICES.RDB$STATISTICS` la **selectividad** de cada
índice (≈ 1 / número de claves distintas) y el optimizador decide el plan con
ese número. **La calcula sólo en tres momentos**: al crear el índice, al
reactivarlo (`ALTER INDEX ... ACTIVE`) y al restaurar la base con `gbak -c`.
Nunca la vuelve a medir sola mientras la tabla crece. (Esto viene de la
documentación de Firebird; lo que sí se midió es lo de abajo: valores de
creación que siguen ahí con la tabla ya llena.)

## Por qué importa (medido en dev, 2026-10-14)

En la base de dev (Firebird 4.0.2), antes de la migración 000065:

| Índice | `RDB$STATISTICS` guardado | Origen probable (inferido del valor) |
|---|---|---|
| `IDX_MSP_PAGOS_RECIB_IMPTE` (000054) | 0.333 | 3 claves distintas al crearlo |
| `IDX_MSP_PAGOS_REC_ESTADO` | 1.0 | tabla vacía al crearlo |
| `IDX_MSP_PAGOS_REC_PEND` | 1.0 | tabla vacía al crearlo |
| `IDX_MSP_PAGOS_REC_CARGO` | 1.0 | tabla vacía al crearlo |

Lo que esas cifras cambian, medido (detalle en
[`../evidencia/infra.md`](../evidencia/infra.md), **E-INF-015**):

- **Pendientes** (`ESTADO = 'P'`): con 1.0 el optimizador ignoraba el índice de
  estado y leía 84,873 capturas por fecha; recalculado, combina fecha y estado
  y lee 2. De 0.53 s a 0.01 s.
- **Las subconsultas `MIN(ID)`** del tablero y del detalle de pago **no** se
  arreglaron recalculando: siguieron recorriendo la PK. Eso lo arreglan los
  índices compuestos de la misma migración. Para pendientes basta recalcular;
  para los `MIN` hacen falta los índices.

`scripts/medicion/desempeno-indices-produccion.sql` repite la medición en
producción.

## Decisión: procedimiento operativo, no worker en Go

Se eligió un **procedimiento manual con script** (abajo) en lugar de un worker
nocturno dentro del API. Razones:

1. **El valor que hace daño es el de un índice creado sobre una tabla vacía o
   casi vacía.** Una vez medido con la tabla poblada, la selectividad cambia
   en proporción al crecimiento y el orden relativo entre índices —que es lo
   que decide el plan— se mueve despacio. El caso agudo se corrige una vez
   (la migración 000065 lo hace para los índices de `MSP_PAGOS_RECIBIDOS` y
   `MSP_VISITAS` que usa el tablero); después basta una revisión periódica.
2. **`SET STATISTICS` es una sentencia de metadatos.** Un worker obligaría a
   que el usuario de base del servicio tenga permiso de alterar índices, y a
   meter DDL en el mismo pool que atiende a los cobradores. El API hoy sólo
   lee y escribe filas; conviene que siga así.
3. **Un cambio de estadísticas puede cambiar planes en caliente**, para bien o
   para mal. Correrlo a mano, con la medición de antes y después, deja a una
   persona mirando el resultado; un worker lo haría a las 3 a.m. sin nadie
   que compare.
4. **Frecuencia baja.** No justifica código, flags, pruebas y despliegue
   nuevos (CLAUDE.md §5: la producción es un Windows Server sin
   orquestador; cada pieza que corre sola es una pieza más que vigilar).

Si la medición periódica muestra que la deriva sí cambia planes entre
revisiones, se reabre la decisión y se implementa el worker con flag
apagado por defecto. Hasta entonces, no.

## Cuándo correrlo

- **Después de cada migración que cree un índice sobre una tabla `MSP_*` que
  todavía no tiene datos** (o casi): el índice nace con selectividad de tabla
  vacía. Correrlo cuando la tabla ya tenga volumen real (días o semanas
  después).
- **Después de cualquier carga masiva o purga** en tablas `MSP_*` (backfills,
  reprocesos, limpieza del changelog).
- **Una vez al mes** como rutina, con la revisión de la deriva (abajo).
- No hace falta después de restaurar con `gbak -c`: la restauración ya las
  recalcula.

## Cómo

1. Medir **antes**: `scripts/medicion/desempeno-indices-produccion.sql`
   (sólo lectura). Guardar la salida con fecha.
2. Recalcular: `scripts/mantenimiento/recalcular-estadisticas-indices.sql`.
   Genera un `SET STATISTICS INDEX` por cada índice activo de una tabla
   `MSP_*`, lo deja en `C:\msp-set-statistics.sql` y lo ejecuta. Imprime la
   selectividad antes y después.
3. Medir **después** con el mismo script del paso 1 y comparar planes,
   lecturas y tiempos.

Subida y ejecución, como cualquier `.sql` en producción
([`despliegue-produccion.md`](despliegue-produccion.md) §11): `scp` y luego
`isql -i`; nunca por stdin.

```bash
scp ${=KEY} -P $PUERTO scripts/mantenimiento/recalcular-estadisticas-indices.sql \
  Administrador@$HOST:C:/recalcular-estadisticas-indices.sql
```

```bat
C:\PROGRA~1\Firebird\Firebird_5_0\isql.exe -user <USUARIO> -password <CONTRASEÑA> ^
  localhost:<RUTA_FDB> -i C:\recalcular-estadisticas-indices.sql -o C:\estadisticas-%DATE:~-4%.txt
```

Fuera del horario de cobranza. El recálculo lee cada índice completo. En dev
(2026-10-15) el script entero —236 índices de tablas `MSP_*`— tardó 5.7 s, y
cada `SET STATISTICS` de `MSP_PAGOS_RECIBIDOS` (348 mil filas) entre 0.05 y
0.09 s. **En producción no se ha medido.**

## Revisión de la deriva

La sección P2 de `scripts/medicion/desempeno-indices-produccion.sql` compara,
para los índices del tablero, la selectividad guardada contra la real
(`1 / COUNT(DISTINCT clave)`). Una diferencia de un orden de magnitud o más es
la señal para recalcular sin esperar al mes.
