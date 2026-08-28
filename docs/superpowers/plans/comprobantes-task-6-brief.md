# Comprobantes — Tarea 6: la migración `000049`

> **Rama:** `feat/comprobantes-migracion` — **nueva**, sacada de `main` ya actualizado. Haz `git pull` de `main` primero: tus once puertos ya están ahí (PR #16, mergeado el 28 de agosto).
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md), **§5 completo**
> **Plan:** tarea `0.1` de la tanda 0
> **Plazo:** entrega el **martes 1 de septiembre al final de tu jornada**. Ver el calendario al final.

> **Ojo con la numeración.** Éste es el brief de la tarea **6**, y corresponde a la tarea **0.1 del plan** — la primera de la tanda 0, la única que quedaba sin dueño. Los briefs se numeran por orden de envío y el plan por dependencias; no coinciden, y no tienen por qué. Si algún documento te dice otra cosa, gobierna éste.

## Dónde encaja

Los once puertos están en `main`. `EnvioRepo` describe una cola, `CursorRepo` un avance y `ConfigRepo` una fila de interruptores — y **ninguna de las tres existe en ninguna base de datos**. Esta tarea las crea.

Es lo último de la tanda 0. En cuanto esté, las nueve tareas de la tanda 1 dejan de ser teoría: un repositorio no se escribe contra una tabla que no existe.

Es también la tarea más corta que te ha tocado y la más difícil de deshacer. Un `VARCHAR` corto o un índice que falta se arreglan con otra migración. Un `UNIQUE` que no está no se arregla: se descubre el día que el cursor reprocesa un tramo del changelog y un cliente recibe dos veces el mismo comprobante, con el saldo de dos momentos distintos.

---

## La regla dura, y es una sola

**CLAUDE.md §1 — la base es un almacén tonto.** El esquema es estructural; todo el comportamiento vive en Go.

En estos dos archivos está **prohibido**:

- Cualquier `DEFAULT`, y con más razón `DEFAULT CURRENT_TIMESTAMP`. Los timestamps los pasa Go con `firebird.ToWallClock(t)`.
- Cualquier generador o columna identidad. Los UUID salen de `uuid.New()` en `CrearEnvio`, que ya escribiste.
- `CREATE TRIGGER` y `CREATE PROCEDURE`. Sin excepciones: la excepción de ADR-0006 cubre las tablas espejo de Microsip, no las nuestras.
- `CHECK` que codifique una regla de negocio. La máquina de estados vive en `estado_envio.go` y ahí se queda.

Permitido y esperado: `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, índices, tipos de columna.

Si algo de esto te parece que hace falta para que la tabla "se defienda sola", ése es exactamente el impulso que la regla existe para frenar. Dilo antes de escribirlo.

---

## Lo que más fácil se equivoca en esta tarea

**El número `000049` está libre y se usa tal cual, aunque ya existan la `000050` y hasta la `000061`.**

Suena mal y no lo es. `golang-migrate` no soporta Firebird, así que el repositorio aplica los `.sql` con un target propio (`fb-migrate-up` en el `Makefile`, línea 129). Ese target **no lleva un número de versión**: lee los `ID` que ya están en `MSP_MIGRATIONS`, y aplica cualquier archivo cuyo id no esté en esa lista, sin importar el orden. Una migración fuera de orden entra sin problema.

`000049` estaba apartada para comprobantes desde el plan del 29 de julio, igual que la `000048` para asistencia. **No la renumeres a `000062`.** Si la renumeras, el número queda huérfano para siempre y el plan y el repositorio dejan de decir lo mismo.

Nombre exacto de los dos archivos, sin inventar variantes:

```
migrations-firebird/000049_create_msp_cm_comprobantes.up.sql
migrations-firebird/000049_create_msp_cm_comprobantes.down.sql
```

---

## Leer antes de escribir

1. **El spec, §5 completo** — las tres tablas, sus columnas y los cuatro índices de §5.4. Es corto y es todo lo que describe el modelo.
2. **`migrations-firebird/000055_create_msp_cfg_sync_epoch.up.sql`** — el molde más cercano: tabla de configuración, filas semilla escritas en el propio `.sql`, y el encabezado que explica *por qué* existe cada columna. Ese encabezado no es adorno: es lo que va a leer quien toque esto dentro de un año.
3. **`migrations-firebird/000050_create_msp_ga_garantias.up.sql` y su `.down.sql`** — el molde de varias tablas en una migración, los nombres de constraints e índices, y sobre todo la forma del `down`.
4. **El `Makefile`, líneas 113 a 165** — `fb-migrate-up`, `fb-migrate-down`, `fb-migrate-status`. Son los comandos con los que vas a verificar, y conviene que entiendas qué hacen antes de correrlos.

---

## Las decisiones ya están tomadas. No adivines ninguna

El spec da las columnas y los tipos. Lo que no da —qué es nulo, cómo se llama cada restricción, qué se siembra— queda cerrado aquí. **El DDL lo escribes tú**: es traducción mecánica de una tabla a `CREATE TABLE` y tienes dos moldes al lado. Lo que no puedes hacer es decidir por tu cuenta cualquiera de estos quince puntos.

### 1. `MSP_CM_ENVIO` — nulabilidad, columna por columna

Sale del dominio que ya escribiste (`domain/envio.go`): lo que allá es puntero, aquí es nulo.

| Columna | Tipo y charset | |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | `NOT NULL`, PK |
| `TIPO` | `VARCHAR(12)` ASCII | `NOT NULL` |
| `REFERENCIA` | `VARCHAR(40)` ASCII | `NOT NULL` |
| `CLIENTE_ID` | `INTEGER` | `NOT NULL` |
| `TELEFONO` | `VARCHAR(20)` ASCII | **nulo** — `sin_telefono` es un caso normal |
| `ESTADO` | `VARCHAR(12)` ASCII | `NOT NULL` |
| `PROGRAMADO_PARA` | `TIMESTAMP` | `NOT NULL` |
| `DOCUMENTO_RUTA` | `VARCHAR(500)` UTF8 | **nulo** — el PDF puede no estar rendereado al encolar |
| `CANAL` | `VARCHAR(20)` ASCII | `NOT NULL` |
| `MENSAJE_EXTERNO_ID` | `VARCHAR(64)` ASCII | **nulo** — sólo existe después de un envío aceptado |
| `INTENTOS` | `SMALLINT` | `NOT NULL` |
| `ULTIMO_ERROR` | `VARCHAR(500)` UTF8 | **nulo** |
| `DETENIDO_POR` | `VARCHAR(64)` UTF8 | **nulo** |
| `ENVIADO_EN` | `TIMESTAMP` | **nulo** |
| `CREATED_AT` | `TIMESTAMP` | `NOT NULL` |
| `UPDATED_AT` | `TIMESTAMP` | `NOT NULL` |

El charset no es decorativo: ASCII para lo que es identificador del contrato (estados, tipos, canales, teléfonos, el id que devuelve WhatsApp) y UTF8 para lo que escribe o lee una persona (rutas, mensajes de error, nombres). Es la regla de `ENCODING_HANDLING.md` y ya la aplicaste en la 000050.

### 2. `MSP_CM_CURSOR`

`CLAVE` `VARCHAR(32)` ASCII `NOT NULL` (PK) · `SEQ_ID` `BIGINT NOT NULL` · `UPDATED_AT` `TIMESTAMP NOT NULL`.

### 3. `MSP_CM_CONFIG`

`ID` `CHAR(36)` ASCII `NOT NULL` (PK), y las seis restantes `NOT NULL`: `VENTANA_VENTA_MIN`, `VENTANA_PAGO_MIN`, `HABILITADO_VENTA`, `HABILITADO_PAGO`, `MAX_INTENTOS` `SMALLINT`, más `CREATED_AT` y `UPDATED_AT` `TIMESTAMP`.

### 4. Nombres de las restricciones y los índices

La convención del repositorio, que ya viste en la 000050:

```
CONSTRAINT PK_MSP_CM_ENVIO  PRIMARY KEY (ID)
CONSTRAINT UQ_MSP_CM_ENVIO_TIPO_REF  UNIQUE (TIPO, REFERENCIA)
CONSTRAINT PK_MSP_CM_CURSOR PRIMARY KEY (CLAVE)
CONSTRAINT PK_MSP_CM_CONFIG PRIMARY KEY (ID)

CREATE INDEX IDX_MSP_CM_ENVIO_ESTADO_PROG ON MSP_CM_ENVIO (ESTADO, PROGRAMADO_PARA);
CREATE INDEX IDX_MSP_CM_ENVIO_CLIENTE     ON MSP_CM_ENVIO (CLIENTE_ID);
CREATE INDEX IDX_MSP_CM_ENVIO_CREATED     ON MSP_CM_ENVIO (CREATED_AT);
```

Son los cuatro de §5.4 contando el `UNIQUE`. `(ESTADO, PROGRAMADO_PARA)` es el índice con el que `ReclamarLote` va a buscar trabajo — el orden de las dos columnas importa y es ése.

### 5. Ninguna llave foránea sale del módulo

`CLIENTE_ID` es un entero opaco y `REFERENCIA` un texto opaco. **No** hay `REFERENCES CLIENTES`, ni hacia `DOCTOS_CC`, ni hacia `DOCTOS_PV`. Mismo criterio que en garantías: nuestras tablas no cuelgan del esquema de Microsip, porque el día que Microsip cambie no queremos que se lleve el módulo por delante.

### 6. El `UNIQUE (TIPO, REFERENCIA)` no es opcional y no es un índice más

Es la idempotencia del módulo entero. El cursor puede reprocesar un tramo del changelog tras un reinicio; cuando lo hace, el segundo intento choca con esta restricción y se descarta. Eso es justo lo que documentaste la semana pasada en `Guardar` y en `ErrEnvioDuplicado`. Sin la restricción, ese comentario miente y el descarte nunca ocurre.

### 7. El `INSERT` a `MSP_MIGRATIONS` sí lleva `CURRENT_TIMESTAMP`

```sql
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (49, '000049_create_msp_cm_comprobantes', CURRENT_TIMESTAMP);
```

Y **no** es una violación de la regla §1. `MSP_MIGRATIONS` es la bitácora del migrador, no una entidad del dominio: nadie la lee desde Go para tomar una decisión de negocio, y el instante que interesa es literalmente "cuándo corrió este archivo". Todas las migraciones del repositorio lo hacen así. No lo "corrijas".

### 8. `MSP_CM_CONFIG` se siembra en la migración, con estos valores exactos

Una fila, y estos valores:

- `ID` = `'68ce8df1-ce8b-48d2-8449-bb2dbf386464'` — te lo doy ya generado a propósito, para que sea el mismo en dev, en pruebas y en producción. No generes otro.
- `VENTANA_VENTA_MIN` = `5`, `VENTANA_PAGO_MIN` = `5` — los cinco minutos de §5.3.
- `HABILITADO_VENTA` = `0`, `HABILITADO_PAGO` = `0`.
- `MAX_INTENTOS` = `3`.
- `CREATED_AT` y `UPDATED_AT` = un **literal explícito**, nunca `CURRENT_TIMESTAMP`: `CAST('2026-09-01 00:00:00' AS TIMESTAMP)`. Igual que la 000055, y por la misma razón: el valor sembrado tiene que ser idéntico en todos los entornos donde se aplique.

Sembrar aquí un UUID escrito a mano no contradice la regla §1: lo que la regla prohíbe es que la **base** genere identidad. Un dato semilla escrito en el `.sql` es tan determinista como una constante en Go.

### 9. Los dos interruptores nacen **apagados**

`HABILITADO_VENTA = 0` y `HABILITADO_PAGO = 0`. Es deliberado y es lo contrario de lo que uno pondría por costumbre.

La tabla se va a crear en producción semanas antes de que exista el worker que la lee. Si nacieran encendidas, el día que se despliegue el primer worker el módulo empezaría a mandar comprobantes solo, sin que nadie haya decidido que ese día era el día. Encender es un `UPDATE` de dos segundos; desencender después de haberle escrito a trescientos clientes no se puede.

### 10. `MSP_CM_CURSOR` **no** se siembra. La tabla se crea vacía

Es la decisión menos evidente del brief, así que va con su razón.

Si sembraras `('pagos_changelog', 0, ...)`, el primer arranque del worker leería el cursor en 0 y recorrería **todo** `MSP_PAGOS_CHANGELOG` desde el principio de los tiempos, encolando un comprobante por cada pago histórico. Cientos de miles. Con los interruptores apagados no saldría ninguno, pero la cola quedaría llena de basura que alguien tendría que limpiar a mano antes de encender.

De dónde arranca el cursor es una decisión del **momento del despliegue**, no del momento de la migración: es el `SEQ_ID` máximo que exista cuando el worker arranque por primera vez. Eso le toca a la tarea 1.2. Aquí sólo se crea la tabla.

### 11. Sin `GRANT` a nadie

Las migraciones `000051`, `000052` y `000053` otorgan permisos a los usuarios de Microsip sobre tablas, generadores y procedimientos nuestros. **Aquí no aplica**, y conviene que sepas por qué existen esas tres: cuando un trigger de Microsip toca un objeto nuestro y el usuario de Microsip no tiene permiso, la aplicación de escritorio **truena al cobrar**. Fue un incidente real.

Ningún trigger de Microsip toca `MSP_CM_*`: estas tablas las escribe únicamente nuestro API, que se conecta como `SYSDBA`. Así que sin `GRANT`, y dilo en el encabezado con esa razón — como lo dice la 000055.

### 12. `COMMIT` entre sentencias

Firebird ejecuta el DDL en transacción; `isql` necesita el `COMMIT` explícito entre bloques o la siguiente sentencia no ve el objeto recién creado. Sigue el patrón de la 000050: `COMMIT` después de cada `CREATE TABLE`, después del bloque de índices, después de los `INSERT` y al final.

### 13. El `.down.sql`: `DROP TABLE` y nada más

Tres `DROP TABLE` con su `COMMIT`, más el `DELETE FROM MSP_MIGRATIONS WHERE ID = 49;`.

**Sin `DROP INDEX` y sin `ALTER TABLE ... DROP CONSTRAINT`.** `DROP TABLE` ya se lleva sus índices y sus restricciones, y la razón de fondo está escrita en el `down` de la 000050: si el `up` falló a la mitad, intentar borrar un objeto que nunca se creó **aborta el rollback** y deja la base peor que antes de empezar.

Aquí no hay llaves foráneas entre las tres tablas, así que el orden no cambia nada. Ponlo igual en orden inverso al `up`, por costumbre: es lo que salva el día que sí las haya.

### 14. Verifica el ancho de `ESTADO` antes de escribirlo

`VARCHAR(12)`, y el estado más largo de tu propia máquina es `sin_telefono`. Cuéntalo. Si te da 12, entra justo y está bien. Si te diera 13, **no cambies el ancho por tu cuenta**: avísame, porque entonces el que está mal es el spec y hay que decidirlo, no parcharlo.

Lo mismo con `CANAL` `VARCHAR(20)` contra `whatsapp_business`, y con `TIPO` `VARCHAR(12)` contra `venta` y `pago`.

### 15. El encabezado del archivo es parte de la entrega

Los dos `.sql` abren con el bloque de comentarios del molde: qué crea la migración, **por qué existe cada tabla**, qué significa cada columna que no sea obvia, y el párrafo de restricciones que deja escrito que no hay `DEFAULT`, ni trigger, ni procedimiento, ni `GRANT`, con su razón.

No es burocracia. La 000055 y la 000050 se entienden hoy sin abrir el spec precisamente por eso, y son el motivo de que este brief se haya podido escribir en una tarde.

---

## La prueba que sostiene esta tarea

Una sola, y es la del `UNIQUE`. Contra tu base de desarrollo, después de aplicar la migración:

1. Inserta una fila cualquiera en `MSP_CM_ENVIO`, con todas sus columnas `NOT NULL`.
2. Inserta una segunda con **el mismo `TIPO` y la misma `REFERENCIA`** y distinto `ID`.
3. La segunda tiene que fallar con una violación de restricción. **Pega la salida literal de Firebird en el reporte** — el mensaje completo, con el nombre `UQ_MSP_CM_ENVIO_TIPO_REF` dentro.
4. Deja la tabla como la encontraste: envuelve las dos inserciones en una transacción y haz `ROLLBACK`, o borra las filas por `ID`. **No dejes datos de prueba en la base compartida.**

Y verifica que los índices existen de verdad, leyéndolos de la base y no de tu archivo:

```sql
SELECT RDB$INDEX_NAME FROM RDB$INDICES WHERE RDB$RELATION_NAME = 'MSP_CM_ENVIO';
```

Tienen que salir los cuatro. Un índice que se te olvidó en el `.sql` no lo detecta ningún comando: la migración aplica perfecto y la tabla queda lenta para siempre.

---

## Antes de aplicar nada: un respaldo

```sh
make fb-snapshot NAME=antes-000049
```

Tarda medio minuto y te deja volver atrás si algo sale mal. Aplicar una migración a medias contra la base de desarrollo sin respaldo es la única forma de arruinarle el día a alguien más desde esta tarea.

**Sólo tu contenedor local.** Nada de esto se corre contra la base de producción; eso lo hago yo, con autorización, y es otro procedimiento.

---

## Las compuertas de esta entrega

```sh
make fb-snapshot NAME=antes-000049
make fb-migrate-up
make fb-migrate-status
make fb-migrate-down N=000049
make fb-migrate-up
```

El ciclo completo `up → down → up` es la compuerta que de verdad importa aquí: demuestra que el `down` funciona, y un `down` roto no se descubre hasta el día en que hace falta, que es siempre el peor día posible.

Y, por el arreglo de un minuto de abajo, las cuatro de siempre:

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
```

---

## Si no tienes base de datos, avísame hoy

Esta tarea no se puede entregar sin aplicarla contra un Firebird real. Si no tienes el contenedor `mueblera-firebird` corriendo, o no te pasaron la base, **dímelo hoy viernes y no el lunes a mediodía**: se resuelve en una hora si me entero a tiempo y te quema media entrega si me entero tarde.

Vale para cualquier otra cosa: la regla de avisar al atorarte más de dos horas sigue en pie, y en esta tarea aplica con más razón porque la mitad del trabajo es contra una base que se puede portar raro.

---

## Un arreglo de un minuto, ya que estás

En `internal/comprobantes/ports/outbound/envio_repo.go`, el comentario de `Guardar` que escribiste en la ronda de revisión parte el identificador en dos renglones:

```go
// A write that hits UNIQUE(TIPO, REFERENCIA) returns domain.
// ErrEnvioDuplicado: the receipt for that fact already exists.
```

`go doc` lo renderiza así, cortado, y rompe el enlace al identificador. Muévelo para que `domain.ErrEnvioDuplicado` quede junto en el mismo renglón. Es sólo eso; el texto no cambia.

---

## Archivos que puedes tocar

```
migrations-firebird/000049_create_msp_cm_comprobantes.up.sql
migrations-firebird/000049_create_msp_cm_comprobantes.down.sql
internal/comprobantes/ports/outbound/envio_repo.go     (sólo el salto de línea del comentario)
docs/superpowers/plans/comprobantes-task-6-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** No toques ninguna otra migración, ni `.golangci.yml`, ni el dominio, ni los otros diez puertos.

---

## El reporte

`docs/superpowers/plans/comprobantes-task-6-report.md`.

La regla de siempre, que ya cumpliste bien en la tarea 5: **se escribe al final, después del último commit, abriendo los archivos y contando lo que hay.**

Y esta vez con una exigencia más, porque es una tarea de base de datos: **el reporte lleva el esquema tal como quedó en la base, no como lo escribiste.** Desde `isql`:

```
SHOW TABLE MSP_CM_ENVIO;
SHOW TABLE MSP_CM_CURSOR;
SHOW TABLE MSP_CM_CONFIG;
```

Pega esa salida. Es la diferencia entre afirmar que la columna quedó `VARCHAR(12) ASCII` y demostrarlo: si escribiste el charset mal, ahí se ve, y en tu `.sql` no.

Más lo de siempre: la salida literal de las compuertas, el error del `UNIQUE`, los cuatro índices leídos de `RDB$INDICES`, y qué decidiste donde el brief te haya dejado elegir. Si no te dejó elegir en nada, dilo — también es información.

---

## Calendario y puntos de control

| Cuándo | Qué |
|---|---|
| **Viernes 28 (hoy)** | **Sólo leer**: §5 del spec, las dos migraciones molde y los targets del `Makefile`. Y contestarme si tienes la base de desarrollo corriendo. |
| **Lunes 31** | El `.up.sql` completo, aplicado contra tu base, con la prueba del `UNIQUE` corrida. |
| **Lunes 31, fin de jornada** | **Punto de control: el `.up.sql` aplicado.** Mándamelo junto con la salida de `make fb-migrate-status`. Es el archivo del que van a colgar las nueve tareas siguientes. |
| **Martes 1 de septiembre** | El `.down.sql`, el ciclo `up → down → up`, el arreglo de un minuto y el reporte. |
| **Martes 1, fin de jornada** | **Entrega.** |

Son dos medias jornadas, unas 8 h, para una tarea que aquí toma entre 1 y 2. Con el ritmo de tus cuatro entregas anteriores eso da unas 4 o 5 h de trabajo real, así que **esta vez hay holgura y es a propósito**: la mitad del trabajo no es escribir SQL, es verificarlo contra una base, y eso nunca sale a la primera.

El punto de control va el lunes y no el martes por lo mismo de siempre: si el esquema sale con correcciones, prefiero que las apliques el martes en la mañana y no descubrirlas cuando ya no hay día.
