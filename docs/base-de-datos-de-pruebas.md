# Base de datos de pruebas — cómo se genera y cómo se usa

- **Fecha:** 2026-08-17
- **Artefacto:** `msp-test-db.fbk.gz` (4.4 MB comprimido, 88 MB restaurado)
- **Origen:** `MUEBLERA.FDB` del contenedor `mueblera-firebird` (3.9 GB)
- **Generador:** [`scripts/make-test-db.sh`](../scripts/make-test-db.sh)

La base de desarrollo pesa 3.9 GB y contiene datos personales reales. Ni una cosa ni la otra sirve para repartirla entre desarrolladores. Este documento describe cómo se produce una versión de 4.4 MB, **sin datos personales**, con la que **pasa la suite de integración completa**.

> **Estado (2026-08-17): el artefacto ya no contiene datos personales.** El padrón sale por `-skip_data` y lo que sobrevive se sustituye por datos sintéticos. La sección [Cómo se demuestra que no quedan datos personales](#cómo-se-demuestra-que-no-quedan-datos-personales) explica con qué se mide. Publicarlo o no sigue siendo decisión de quien sea dueño del repositorio; este documento sólo sostiene que el contenido ya no lo impide.

---

## Para usar la base (lo que hace un desarrollador)

```sh
# 0. Levantar el servidor de Firebird (una sola vez por máquina)
docker network create firebird_default
docker compose --profile firebird up -d firebird

# 1. Descomprimir
gunzip msp-test-db.fbk.gz

# 2. Restaurar dentro del contenedor de Firebird, EN SU PROPIO ARCHIVO
docker cp msp-test-db.fbk mueblera-firebird:/tmp/
docker exec -i mueblera-firebird /usr/local/firebird/bin/gbak -c \
  -user sysdba -password masterkey \
  /tmp/msp-test-db.fbk /firebird/data/MSPTEST.FDB

# 3. Apuntar el .env
#    FB_DATABASE=/firebird/data/MSPTEST.FDB

# 4. Verificar
make test-firebird-all
```

> ⚠ **No restaures encima de `/firebird/data/MUEBLERA.FDB`.** En una máquina que ya tiene la base de desarrollo, ese archivo es la base de desarrollo: restaurar sobre él la destruye. Dale su propio nombre y apunta el `.env` ahí. En una máquina nueva, donde `MUEBLERA.FDB` no existe, da lo mismo — pero el nombre aparte no cuesta nada y quita el filo.

> El paso 0 está detrás de un profile a propósito: `docker compose up` no lo arranca. Quien ya tenga un contenedor `mueblera-firebird` levantado a mano **no debe correrlo** — el servicio existe para que una máquina nueva reproduzca el mismo servidor sin adivinar la imagen ni la configuración.

### No uses cualquier imagen de Firebird

**Usa la del `compose.yml`: `jacobalberty/firebird:v4.0`.** No es un capricho de versión.

Las imágenes de **Firebird 5 traen `WireCrypt = Required`**, y el driver `firebirdsql` v0.9.19 no negocia ese nivel. La conexión muere en el saludo inicial, antes de tocar la base. El síntoma es inconfundible:

```
fbtestutil: begin tx: Incompatible wire encryption levels requested on client and server
fbtestutil: ping localhost: firebird: ping: Error op_response:<número>
```

Y engaña, porque **no falla un test: fallan cientos, todos a la vez**. Da la impresión de que la base llegó incompleta o que faltan tablas. No es eso: ningún test alcanzó a preguntarle nada a la base. La señal para distinguirlo es que **no aparece ni un solo `Table unknown`** en toda la salida. Si los errores son de `ping` o de `wire encryption`, el problema es el servidor, no los datos.

Si por lo que sea hay que quedarse con otra imagen, dos salidas:

- **Del lado del cliente:** `FB_WIRE_CRYPT=false` en el `.env`.
- **Del lado del servidor:** dejar `WireCrypt = Enabled` y `AuthServer = Srp256, Srp` en el `firebird.conf`. Es el mismo arreglo que se aplicó en el servidor Windows cuando pasó a Firebird 5.

> ⚠️ **La configuración se escribe una sola vez, en el primer arranque.** El entrypoint de la imagen genera `/firebird/etc/firebird.conf` únicamente cuando el volumen está vacío. Si quedó mal, **cambiar variables de entorno no la corrige**: hay que borrar el volumen (`docker volume rm msp-firebird-data`) y volver a restaurar, o editar el archivo a mano dentro del contenedor. Es la trampa que hace perder más tiempo aquí.

### Meilisearch es aparte

Los tests de `ventas/infra/ventsearch` necesitan Meilisearch corriendo y fallan con `dial tcp 127.0.0.1:7700: connect: connection refused` si no está. No tiene relación con Firebird:

```sh
docker compose up -d meilisearch
```

> ⚠️ **`go test` a secas no ve `FB_DATABASE`.** El target del Makefile carga `.env` con `include`; una invocación manual de `go test` no. Sin esa variable los tests de integración **se saltan en silencio** y el paquete reporta `ok`. Es un falso verde que cuesta una tarde.

> ⚠️ **Para correr contra otra base, `export` no basta.** El `include .env` del Makefile pisa la variable del entorno, así que `export FB_DATABASE=... && make test-firebird-all` corre contra la base del `.env`, no contra la que se pidió — en silencio. La forma que sí funciona es pasarla como argumento, que gana sobre todo lo demás:
>
> ```sh
> make FB_DATABASE=/firebird/data/DEVTEST.FDB test-firebird-all
> ```

> ⚠️ **`go test ./...` necesita `-timeout` amplio.** El paquete `internal/analytics/infra/analyticsfb` tarda **~11 minutos** contra la base completa (`TestRepo_LeerAnclasDesde_Regression` solo ya son ~1m20s). El default de Go son 10 minutos, así que la suite completa **truena por timeout, no por una aserción**. Usar `-timeout 1800s`. Contra la base reducida el problema no aparece porque esos tests no tienen datos que recorrer.

---

## Qué contiene y qué no

**Conserva** los catálogos, que es de lo que dependen los tests. Conteos reales del artefacto restaurado:

| Tabla | Filas |
|---|---|
| `SALDOS_IN` (inventario) | 94,073 |
| `ARTICULOS` | 6,113 |
| `MSP_AN_CARTERA_SNAPSHOT` | 2,645 |
| `LISTAS_ATRIBUTOS` | 1,246 |
| `CAJEROS` | 66 |
| `CAJAS` | 61 |
| `COBRADORES` | 52 |
| `ALMACENES` | 50 |
| `ZONAS_CLIENTES` | 46 · `VENDEDORES` 46 |
| `MSP_CFG_ZONA_CAJA` | 44 |
| `MSP_USUARIOS` | 9 · `MSP_ROLES` 4 |

**Omite los datos** de 179 tablas: los movimientos (4.5 millones de filas entre cuentas por cobrar y punto de venta), **el padrón de clientes completo**, la lista de precios (`PRECIOS_ARTICULOS`, 97,071 renglones), las compras y los proveedores, las bitácoras, el rastreo GPS del sistema legado, las cachés derivadas y **todas las imágenes**. Quedan en cero:

| Tabla | Filas |
|---|---|
| `CLIENTES` · `DIRS_CLIENTES` · `LIBRES_CLIENTES` | 0 |
| `DOCTOS_PV` (ventas) · `DOCTOS_CC` (crédito) | 0 |
| `MSP_LOCAL_SALE` · `MSP_AN_WINBACK_CANDIDATOS` | 0 |
| `MSP_OUTBOX_EVENTS` · `MSP_FAILED_INTENTS` | 0 |
| `PROVEEDORES` · `BITACORA` · `MOTIVOS_CANCELACION` | 0 |

> La lista de precios se omite y la suite pasa igual: **la API no consulta `PRECIOS_ARTICULOS` al aplicar una venta.** El precio viaja en la petición desde la app del cobrador. Es la pieza de información comercial más sensible que había y no hace falta para probar.

`ARTICULOS` se conserva porque `SALDOS_IN` la referencia. No trae costos ni márgenes — solo claves, descripciones y cuentas contables.

---

## Cómo se genera

Con un comando:

```sh
./scripts/make-test-db.sh
```

Tarda unos quince minutos, casi todos de espera. Deja tres archivos en el directorio actual: el artefacto `msp-test-db.fbk.gz`, el volcado `pii-corpus.txt` con todo el texto de la base ya restaurada, y `pii-padron.txt` con los nombres reales de la base viva **para poder cruzarlos**.

> ⚠ `pii-padron.txt` **sí** contiene datos personales. Es local, sirve sólo para la auditoría y hay que borrarlo al terminar.

El script hace cinco pasos, y ninguno toca la base de desarrollo después del primero:

| Paso | Qué hace |
|---|---|
| 1 | `gbak -b` de la base viva. Es seguro sobre una base en uso; no hay que detener nada |
| 2 | `gbak -c -skip_data` con la expresión de [`scripts/db-test-skip-tables.txt`](../scripts/db-test-skip-tables.txt) |
| 3 | `isql` con [`scripts/db-test-anonymize.sql`](../scripts/db-test-anonymize.sql), que sustituye lo que sobrevivió |
| 4 | Respaldo y `gzip -9` del resultado |
| 5 | Restaura el `.gz` a una base **aparte** y vuelca de ahí todo el texto |

Dos variables ayudan cuando hay que iterar:

```sh
REUSE_BACKUP=1 ./scripts/make-test-db.sh                  # salta el paso 1
VERIFY_DB=/firebird/data/CATVERIF2.FDB ./scripts/make-test-db.sh
```

La segunda existe por una trampa real: si una corrida de `go test` quedó a medias, el servidor conserva la conexión contra la base de verificación y `gbak` responde `database already exists` **aunque el archivo ya no esté**. Usar otro nombre sale más barato que reiniciar el contenedor.

El paso 5 es deliberado: **se verifica el artefacto, no la base de la que salió.** Un archivo que nunca se restauró no está verificado.

---

## Cómo se demuestra que no quedan datos personales

Dos herramientas, y ninguna se fía de los nombres de las columnas.

**[`scripts/db-test-dump-text.sh`](../scripts/db-test-dump-text.sh)** vuelca a `TABLA.COLUMNA::valor` el contenido de **todas** las columnas `CHAR`, `VARCHAR` y `BLOB SUB_TYPE TEXT` de **todas** las tablas con filas. Es a propósito exhaustivo: buscar sólo en columnas llamadas `NOMBRE` o `TELEFONO` es exactamente cómo se pasan por alto `LISTAS_ATRIBUTOS.VALOR_DESPLEGADO` y `MOTIVOS_CANCELACION.MOTIVO`, que traían nombres de clientes reales y no se llaman así.

**[`scripts/db-test-pii-scan.py`](../scripts/db-test-pii-scan.py)** cruza ese volcado contra el padrón real y reporta tres cosas:

1. **Nombres reales encontrados**, por columna. Busca cada nombre del padrón como **subcadena**, no como valor completo, para atrapar los que van embebidos en un texto libre ("el pago era para el cliente FULANO DE TAL"). Si encuentra alguno que no esté en la lista de excepciones, el script **sale con código 1**.
2. Patrones de correo, teléfono, RFC y CURP, agrupados por columna, para revisión humana.
3. Todas las columnas con frases de 2 a 5 palabras, para revisión humana.

```sh
python3 scripts/db-test-pii-scan.py pii-corpus.txt pii-padron.txt
```

Resultado sobre el artefacto del 2026-08-17: **cero aciertos, código de salida 0.**

Hay **una** excepción declarada en el propio script: `TIPOS_CLIENTES.NOMBRE = 'PUBLICO EN GENERAL'`, un valor del catálogo de tipos de cliente de Microsip que coincide con el nombre del cliente genérico de mostrador. No es el dato de nadie. La lista de excepciones vive en el código, con su justificación, y se revisa como se revisa un `nolint`: es la única forma de que este escaneo mienta.

El escaneo tarda unos segundos y **hay que correrlo cada vez que se regenera el artefacto.**

### Lo que este método NO garantiza

Con esto dicho, y sin adornarlo:

- **El cruce sólo conoce a los clientes que hoy están en `CLIENTES`.** Un nombre de empleado, o el de un cliente dado de baja, no lo detecta. Por eso la lista de columnas con frases se imprime completa y hay que leerla: así aparecieron `MOTIVOS_CANCELACION` (307 renglones de texto libre con nombres de clientes y de personal) y `BITACORA` (usuarios de Microsip), que ninguna consulta automática habría señalado.
- **Los apellidos sintéticos se eligieron ausentes del padrón a propósito.** Con apellidos comunes, 47 de las 132 combinaciones generadas coincidían con clientes reales; no eran fugas, pero volvían ruidosa la verificación. Los que están en el script —ZUBIETA, ARRIETA, GOROSTIZA, ELIZONDO…— se comprobaron ausentes de las 43,834 filas del padrón.
- **Quedan identificadores de la EMPRESA**, que no son datos personales pero conviene saber que están: el nombre comercial en `REGISTRY` y `MG_EXSIM_LICENCIAS`, los nombres y domicilios de sus almacenes y sucursales, y el catálogo público de colonias de `LISTAS_ATRIBUTOS` (atributo 787502). El RFC del titular y el registro patronal del IMSS **sí** se sustituyen: el primero es de persona física y codifica su fecha de nacimiento.
- **`SALDOS_EDOFIN.NOMBRE` trae `JUAN PEREZ`.** Es la etiqueta de una línea de estado financiero, no está en el padrón, y todo apunta a que alguien la escribió como relleno. Se deja como está.

---

## Cuatro cosas que cuestan horas si no se saben

### `DELETE` no encoge el archivo, y además es inviable

Borrar las filas y esperar que el `.fdb` adelgace no funciona: Firebird marca el espacio como libre pero no lo devuelve. Hace falta un ciclo de respaldo y restauración.

Peor: **la base de Microsip es trigger-driven por construcción** ([ADR-0006](adr/0006-firebird-adapter-trigger-rule-exemption.md)). Desactivar nuestros triggers `MSP_*` no basta — los de Microsip recalculan saldos en cada borrado. Un intento de purgar 4.4 millones de filas corrió **más de hora y media sin terminar**. `-skip_data` hace lo mismo en 25 segundos porque no borra nada.

### Omitir una tabla exige omitir todos sus descendientes

Si se omiten los datos de `DOCTOS_CC` pero se conservan los de una tabla que la referencia, la restauración falla con violación de llave foránea y **la base queda sin poder activar índices**, o sea inservible.

No sirve ir agregando tablas conforme `gbak` las reclama — cada intento cuesta minutos y hay 59. El cierre transitivo se calcula de una vez:

```sql
WITH RECURSIVE fk AS (
  SELECT TRIM(rc_child.RDB$RELATION_NAME) AS HIJA,
         TRIM(rc_par.RDB$RELATION_NAME)   AS PADRE
    FROM RDB$REF_CONSTRAINTS r
    JOIN RDB$RELATION_CONSTRAINTS rc_child ON rc_child.RDB$CONSTRAINT_NAME = r.RDB$CONSTRAINT_NAME
    JOIN RDB$RELATION_CONSTRAINTS rc_par   ON rc_par.RDB$CONSTRAINT_NAME   = r.RDB$CONST_NAME_UQ
),
cierre AS (
  SELECT CAST('DOCTOS_CC' AS VARCHAR(64)) AS TABLA FROM RDB$DATABASE
  UNION ALL SELECT CAST('DOCTOS_PV' AS VARCHAR(64)) FROM RDB$DATABASE
  -- ... una línea por cada semilla
  UNION ALL
  SELECT CAST(fk.HIJA AS VARCHAR(64)) FROM fk JOIN cierre c ON fk.PADRE = c.TABLA
)
SELECT DISTINCT TABLA FROM cierre ORDER BY 1;
```

### `gstat` no cuenta los BLOBs

`gstat -d` reporta páginas de datos por tabla, y con eso se identifica qué pesa. Pero **no cuenta las páginas de BLOB**.

Después de quitar los movimientos, la base seguía en 448 MB con apenas 10 mil páginas de datos — unos 80 MB. Los 370 MB restantes eran las **fotos de producto** en `IMAGENES_ARTICULOS`, invisibles para ese conteo. Si los números no cuadran, la diferencia son BLOBs.

Y de paso: `IMAGENES_CLIENTES` guarda fotos de identificación. Omitirlas no es solo tamaño.

### `SALDOS_IN` parece derivada y no lo es

Son saldos de inventario, recalculables en principio. Al omitirla fallan seis tests de combos y juegos —`TestE2E_AplicarComboJuego_MatchDescargaInventario` entre ellos— porque verifican que aplicar una venta descargue existencias, y sin saldos no hay nada que descargar.

Cuesta 8 MB. Se queda.

---

## Qué se hizo con los datos personales

Se probaron los dos caminos obvios y ninguno bastaba solo.

**Omitir el padrón** (`-skip_data` sobre `CLIENTES` y su cierre transitivo) saca 43,834 clientes, 43,835 domicilios y 45,287 formas de cobro de un plumazo. Pero deja intactos a los **empleados**: `COBRADORES` traía nombres de personas dentro de la cadena "RUTA 01 - …", `AGENTES` nombre y celular, `EMPLEADOS` CURP, RFC y hasta los nombres del padre y de la madre. Esas tablas los tests las necesitan pobladas —`MSP_CFG_ZONA_CAJA` referencia cobradores, vendedores y cajeros— así que omitirlas no era opción.

**Sustituir todo por datos sintéticos** habría obligado a reescribir 43,834 filas de `CLIENTES` más sus tablas hijas, con los triggers de Microsip disparando en cada `UPDATE`. Caro y frágil.

Lo que se hizo es la combinación:

| Qué | Cómo | Cuántas filas |
|---|---|---|
| Padrón de clientes y todo lo que cuelga de él | `-skip_data` | 0 filas en el artefacto |
| `MSP_LOCAL_SALE` (nombre, teléfono, domicilio y GPS de clientes) | `-skip_data` | 0 |
| `MSP_OUTBOX_EVENTS`, `MSP_FAILED_INTENTS` (cargas de petición) | `-skip_data` | 0 |
| `MOTIVOS_CANCELACION`, `BITACORA`, proveedores y compras | `-skip_data` | 0 |
| Catálogos de personas que los tests necesitan | `db-test-anonymize.sql` | ~950 |

### Las tablas con datos personales que no eran obvias

Este es el hallazgo que más tiempo costó, y la razón de que el volcado sea exhaustivo. Ninguna de estas se llama como para sospechar:

| Tabla | Qué traía | Filas |
|---|---|---|
| `LISTAS_ATRIBUTOS.VALOR_DESPLEGADO` | El padrón de vendedores del campo libre `LIBRES_CARGOS_CC.VENDEDOR_1/2/3`: nombres de personas, 16 de ellos también clientes | 622 |
| `MOTIVOS_CANCELACION.MOTIVO` | Texto libre del capturista, con nombres de clientes dentro de la frase | 307 |
| `MSP_LOCAL_SALE` | Nombre, teléfono, domicilio, colonia y GPS del cliente de cada venta local | 2,231 |
| `LIBRES_CLIENTES` | Celular, comprobante de domicilio y coordenadas del cliente | 43,834 |
| `BITACORA` | Usuarios de Microsip y valores de los cambios | 1,187 |
| `MSP_AN_CLIENTE_NARRATIVA` | Texto generado por IA **sobre clientes reales** | 11 |
| `REGISTRY` | El RFC de persona física del titular y el domicilio fiscal | 384 |
| `CLAVES_PROVEEDORES` / `DOCTOS_CM` / `DOCTOS_CP` | RFC de 13 caracteres —persona física— de un proveedor | ~30 |

`LISTAS_ATRIBUTOS` no se puede omitir: el módulo `config` la lee para resolver los vendedores de crédito, y `internal/clientes` compara `UPPER(VALOR_DESPLEGADO) = 'CONTADO'`. Se sustituyen **sólo** los seis atributos que son listas de personas (11350, 11351, 11702, 19985, 19986, 19987), y el nombre sintético se deriva de `HASH()` del valor original, no del identificador de la fila: la misma persona aparece en los tres atributos y `ListarIdentidadesMicrosip` **agrupa por `VALOR_DESPLEGADO`**. Un reemplazo por fila rompería esa agrupación y el test de `config` con ella.

---

## Levantar la API contra esta base

Pasar los tests y servir como entorno de desarrollo son dos cosas distintas. Esto es lo segundo.

```sh
export FB_DATABASE=/firebird/data/DEVTEST.FDB
export FIREBASE_DEV_MODE=true
export APP_PORT=3099          # o el que se prefiera
go run ./cmd/api serve
```

Arranca en unos 6 segundos. Verificado: `/healthz` responde 200, `/v2/zonas-cliente` responde 200.

### El primer arranque deja al usuario sin permisos

Con `FIREBASE_DEV_MODE=true` el token es literal, sin Firebase de por medio:

```
Authorization: Bearer dev:<firebase_uid>:<email>
```

El primer `GET /v2/me` **da de alta al usuario automáticamente**, pero **sin rol**. La respuesta trae `"permisos":[]` y a partir de ahí casi todo contesta 403. No es una falla: falta asignarle el rol.

La base ya trae el rol `super_admin` con sus 40 permisos, así que no hay que crearlo — solo asignarlo. **`make fb-seed-admin` no sirve para esto**: ese seed crea un rol `ADMIN` aparte, pensado para una base vacía, y aquí duplicaría la configuración.

Después del primer `/v2/me`, con el correo que se usó en el token:

```sql
INSERT INTO MSP_USUARIOS_ROLES (USUARIO_ID, ROL_ID, CREATED_AT, CREATED_BY)
SELECT u.ID, r.ID, CURRENT_TIMESTAMP, u.ID
  FROM MSP_USUARIOS u CROSS JOIN MSP_ROLES r
 WHERE u.EMAIL = '<tu-correo>' AND r.NOMBRE = 'super_admin';
COMMIT;
```

Volver a llamar `/v2/me`: debe devolver los 40 permisos.

### Qué se puede desarrollar y qué no

La base conserva los catálogos y **omite los movimientos y el padrón**.

**Ventas funciona.** Hay catálogos e inventario, y la suite prueba el flujo de aplicar una venta de principio a fin. Quien trabaje ahí crea sus propios datos y avanza — incluido el cliente, que antes venía dado.

**Cobranza, reportes, analítica, el Cliente 360 y el directorio van a ver pantallas vacías**, porque leen movimientos y clientes que ya no están. Para trabajar ahí hay que sembrar: `internal/platform/microsipseed` hace exactamente eso y es lo que usan sus tests.

---

## Limitaciones conocidas

### ~~Diez tests leen filas de producción por identificador fijo~~ — RESUELTO (2026-08-16)

> Se conserva el enunciado porque explica de dónde viene el paquete `microsipseed`.

Había tests que traían filas reales por identificadores escritos a mano —`clienteID = 24037`, `doctoPVID = 4070523`, `pago 4070588`— y verificaban cantidades exactas. Contra la base reducida fallaban, porque esas filas viven en los movimientos que `-skip_data` omite.

**El conteo que decía este documento estaba mal por partida doble.** Corriendo la suite COMPLETA (`go test ./...`, no los nueve paquetes del target) contra la base reducida salieron **trece**, repartidos en **cuatro** módulos — no diez en dos:

| Paquete | Tests | Qué leían de producción |
|---|---|---|
| `clientes/infra/clientesfb` | 8 | clientes 24037/12387/12440/202468, venta 4070523, pagos 4070588 y 4172481, zona 21566 |
| `clientes/infra/clienteshttp` | 1 | el reporte de la clienta 24037 (4 ventas, 3 liquidadas) |
| `analytics/infra/analyticsfb` | 2 | clientes 3074781 y 114397 con sus fechas de pago; "algún cliente que pagó en ene–feb 2026" |
| `rutas/infra/rutasfb` | 1 | la zona 12271 con ventas en los últimos 30 días |
| `reactivacion/infra/reactivacionfb` | 1 | el universo de Tehuacán: clientes reales CON TELÉFONO |

Ahora siembran lo que verifican, vía [`internal/platform/microsipseed`](../internal/platform/microsipseed/seed.go). El sembrador no inserta el cargo de cuentas por cobrar a mano: inserta el documento de punto de venta y voltea `APLICADO` de `'N'` a `'S'`, dejando que la cascada de Microsip genere el cargo, el puente `DOCTOS_ENTRE_SIS`, los importes y las cachés `MSP_SALDOS_VENTAS` / `MSP_PAGOS_VENTAS` — el mismo camino que el writer de producción.

### ~~Otros veinte tests seguían atados al padrón~~ — RESUELTO (2026-08-17)

Vaciar `CLIENTES` sacó a la luz una segunda tanda que el trabajo anterior no había tocado, porque contra una base **con** padrón pasaban sin quejarse: veinte tests en cuatro paquetes.

| Paquete | De qué dependía | Cómo quedó |
|---|---|---|
| `cobranza/infra/microsip` | `testClienteID = 11486`, una fila del padrón | `seedCliente` siembra cliente + `CLAVES_CLIENTES` en transacción confirmada |
| `ventas/infra/ventfb` | la misma constante `11486` | `seedClienteFixture`, **idempotente**: contra la base de desarrollo completa no hace nada |
| `ventas/infra/venthttp` | `SELECT FIRST 1 … WHERE ESTATUS='A'` y `11486` escrito a mano | `seedClienteConClave`, dentro de la transacción con rollback |
| `clientes/app` | `n > 0` y "el índice tiene ≥1000 documentos" | siembra 3 clientes y afirma contra ese número, no contra el tamaño del padrón |

El umbral de 1000 documentos merece una nota: era el tamaño del padrón real disfrazado de aserción. Es la misma bomba de tiempo que describe la sección de abajo, sólo que apuntando hacia el otro lado — no fallaba cuando la base crecía, fallaba cuando dejaba de tenerla.

### Sembrar un cliente confirmado exige borrar `SALDOS_CC` primero

Un trigger de Microsip crea una fila en `SALDOS_CC` al dar de alta un cliente. La llave foránea `CLI_A_SALDOS` impide entonces borrar la fila de `CLIENTES`, así que un `DELETE FROM CLIENTES` de limpieza **falla**. Si el error se descarta —`_, _ = q.ExecContext(...)`— la limpieza parece funcionar y el cliente sembrado se queda para siempre en la base compartida.

Dos reglas para cualquier siembra que confirme:

1. Borrar hijos antes que padre: `SALDOS_CC` → `CLAVES_CLIENTES` → `CLIENTES`.
2. **No descartar el error de la limpieza.** Reportarlo con `t.Errorf`. Una limpieza que falla en silencio es indistinguible de una que funciona.

Y una tercera, que costó una corrida entera: **`defer pool.Close()` corre ANTES que los `t.Cleanup`.** Un test que abre su propio pool con `defer pool.Close()` deja a toda limpieza registrada con `t.Cleanup` hablándole a un pool cerrado (`sql: database is closed`). Se arregla registrando el cierre también con `t.Cleanup`, antes de sembrar: el LIFO lo pone al final.

### `CLIENTES_AK1` es único sobre `NOMBRE`

Dos tests del mismo paquete que siembran el mismo nombre chocan contra el índice, no contra el código. Cualquier siembra que confirme debe llevar un sufijo único (`uuid.NewString()[:8]`).

### El target de integración cubre 9 de 25 paquetes

`make test-firebird-all` corre nueve paquetes. Veinticinco dependen de `FB_DATABASE`.

El bloqueo que impedía ampliarlo —los tests atados a filas de producción— ya no existe.

Queda una advertencia, con la evidencia que hay y no más: al correr `clientes` y `cobranza` juntos, **`TestSync_GoldenSnapshot` falló una vez**. No se reprodujo en 3 corridas paralelas más, 2 con `-p 1`, 2 del paquete solo y 1 del test aislado — todas en verde. La salida de la falla se perdió (el filtro de la corrida sólo conservó el nombre), así que **la causa no está probada**.

Lo que sí es un hecho es la condición de fondo: `go test ./...` corre los paquetes en PARALELO y esta suite comparte una sola base Firebird; el propio comentario de ese test dice que su marca de agua sale del `MinActiveTransactionID` vivo, que es estado global del servidor. Por eso el job de CI usa `-p 1`: cuesta tiempo de reloj y compra determinismo, que es lo único que hace útil una señal remota. Si alguien amplía `make test-firebird-all`, conviene que haga lo mismo.

### `cobranza/infra/microsip` escribe filas confirmadas

Ese paquete está **excluido a propósito** de `make test-firebird-all` porque confirma escrituras en Microsip y depende de `t.Cleanup` para borrarlas. `go test ./...` **sí** lo corre. Contra una base desechable —la de CI, o la restaurada del artefacto— eso es correcto; contra la base de desarrollo compartida, no.

### Cómo no volver a plantar una bomba de tiempo

Un tercer caso ya se corrigió. Dos tests de `analyticsfb` insertaban instantáneas con fecha del día y las buscaban en `ListRecentSnapshots(100)`, que ordena por `FECHA_CORTE DESC`. Pasaron hasta que el job diario acumuló más de cien instantáneas más recientes; ese día empezaron a fallar solos, sin que nadie tocara código.

La regla que dejan: **un test afirma sobre el código, no sobre el estado de la base compartida.** Si un fixture tiene que caer dentro de una ventana ordenada, su clave debe garantizarlo por construcción —una fecha que ningún corte real alcanza— y no por el tamaño que la tabla tenga hoy.

---

## Regenerar cuando la base cambie

Este artefacto es una foto del esquema y los catálogos al 2026-08-17. Hay que regenerarlo cuando se agreguen migraciones que cambien el esquema, o cuando cambien catálogos de los que dependan los tests.

```sh
./scripts/make-test-db.sh
python3 scripts/db-test-pii-scan.py pii-corpus.txt pii-padron.txt
make FB_DATABASE=/firebird/data/CATVERIF.FDB test-firebird-all
rm pii-padron.txt          # contiene datos personales reales
```

**Al regenerar, vuelve a correr el escaneo.** Una tabla nueva con datos personales entra al artefacto sin avisar; la lista de omisión no se entera sola.

---

## Qué se probó y qué no

El artefacto se verificó **descomprimiéndolo y restaurándolo a una base nueva**, no sobre la base desde la que se generó, y contra esa base se corrió `go test ./... -p 1 -race` completo.

Dos recortes se intentaron y uno se revirtió:

| Tabla | Qué pasó |
|---|---|
| `SALDOS_IN` | Sin ella fallan seis tests de combos y juegos que verifican la descarga de inventario. Cuesta 8 MB; se queda |
| `PRECIOS_ARTICULOS` | Se probó quitarla y **la suite pasa completa**. Se queda fuera |

**Lo que NO se hizo, y es a propósito:** el artefacto no se subió a ningún lado. No hay release, ni artefacto de CI, ni commit del `.fbk.gz`. El trabajo termina con el archivo generado en local y el procedimiento escrito. Publicarlo es decisión de quien sea dueño del repositorio.

**Lo que queda fuera del alcance de esto:** el índice de Meilisearch de la máquina de desarrollo sigue teniendo 43,729 documentos con nombres, domicilios y teléfonos reales. No viaja en el artefacto ni en el repositorio, pero está ahí y se repuebla solo con cada reconciliación contra la base viva.
