# Base de datos de pruebas — cómo se genera y cómo se usa

- **Fecha:** 2026-07-31
- **Artefacto:** `msp-test-db.fbk.gz` (~15 MB comprimido, 139 MB restaurado)
- **Origen:** `MUEBLERA.FDB` del contenedor `mueblera-firebird` (3.9 GB)

La base de desarrollo pesa 3.9 GB y contiene datos reales de clientes. Ni una cosa ni la otra sirve para repartirla entre desarrolladores. Este documento describe cómo se produce una versión de 15 MB con la que **pasa la suite de integración completa**.

---

## Para usar la base (lo que hace un desarrollador)

```sh
# 0. Levantar el servidor de Firebird (una sola vez por máquina)
docker network create firebird_default
docker compose --profile firebird up -d firebird

# 1. Descomprimir
gunzip msp-test-db.fbk.gz

# 2. Restaurar dentro del contenedor de Firebird
docker cp msp-test-db.fbk mueblera-firebird:/tmp/
docker exec -i mueblera-firebird /usr/local/firebird/bin/gbak -c \
  -user sysdba -password masterkey \
  /tmp/msp-test-db.fbk /firebird/data/MUEBLERA.FDB

# 3. Apuntar el .env
#    FB_DATABASE=/firebird/data/MUEBLERA.FDB

# 4. Verificar
make test-firebird-all
```

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

**Conserva** todos los catálogos, que es de lo que dependen los tests:

| Tabla | Filas |
|---|---|
| `ARTICULOS` | 6,113 |
| `CAJAS` | 61 |
| `ALMACENES` | 50 |
| `ZONAS_CLIENTES` | 46 |
| `MSP_CFG_ZONA_CAJA` | 44 |
| `MSP_CFG_PLAZO_CREDITO` · `MSP_CFG_VENDEDOR_MICROSIP` · `MSP_CFG_APLICAR` | 8 |
| `SALDOS_IN` | existencias de inventario |

**Omite** los movimientos (4.5 millones de filas entre cuentas por cobrar y punto de venta), **la lista de precios** (`PRECIOS_ARTICULOS`, 97,071 renglones), las bitácoras, el rastreo GPS del sistema legado, las cachés derivadas y **todas las imágenes**.

> La lista de precios se omite y la suite pasa igual: **la API no consulta `PRECIOS_ARTICULOS` al aplicar una venta.** El precio viaja en la petición desde la app del cobrador. Es la pieza de información comercial más sensible que había y no hace falta para probar.

`ARTICULOS` se conserva porque `SALDOS_IN` la referencia. No trae costos ni márgenes — solo claves, descripciones y cuentas contables.

---

## Cómo se genera

### Paso 1 — Respaldo consistente de la base viva

`gbak` es seguro sobre una base en uso; no hay que detener nada.

```sh
docker exec -i mueblera-firebird /usr/local/firebird/bin/gbak -b \
  -user sysdba -password masterkey \
  /firebird/data/MUEBLERA.FDB /firebird/data/full.fbk
```

### Paso 2 — Restaurar omitiendo datos

`gbak -skip_data` acepta **una sola expresión regular** (no varios nombres) y omite los datos de las tablas que coincidan, sin borrar nada. La lista completa está en [`scripts/db-test-skip-tables.txt`](../scripts/db-test-skip-tables.txt).

```sh
docker exec -i mueblera-firebird /usr/local/firebird/bin/gbak -c \
  -skip_data "$(cat scripts/db-test-skip-tables.txt)" \
  -user sysdba -password masterkey \
  /firebird/data/full.fbk /firebird/data/CAT.FDB
```

### Paso 3 — Respaldar y comprimir

```sh
docker exec -i mueblera-firebird /usr/local/firebird/bin/gbak -b \
  -user sysdba -password masterkey \
  /firebird/data/CAT.FDB /firebird/data/msp-test-db.fbk
docker exec -i mueblera-firebird gzip -9 /firebird/data/msp-test-db.fbk
docker cp mueblera-firebird:/firebird/data/msp-test-db.fbk.gz .
```

### Paso 4 — Verificar el artefacto, no la base de la que salió

Restaurar el `.gz` a una base nueva y correr la suite **contra esa**. Un archivo que nunca se restauró no está verificado.

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

## Sobre los datos de clientes

La base conserva `CLIENTES` (43,834 filas) y `DIRS_CLIENTES` con **nombres, domicilios y teléfonos reales**. Las fotografías sí se omiten.

Pesan poco —2,728 y 1,384 páginas— así que quitarlos no es cuestión de tamaño sino de no repartir datos personales entre máquinas de desarrollo.

**Esto es también lo que impide publicar el artefacto.** El repositorio es público; subir el `.fbk.gz` a un release, a un artefacto de Actions o al propio repositorio sería divulgar nombres, domicilios y teléfonos de 43,834 personas. Mientras `CLIENTES` siga dentro, el artefacto sólo puede viajar por canales privados.

**Ya no queda ningún test que dependa de un cliente real** (2026-08-16): los que buscaban a `12387` o `12440` ahora siembran el suyo con `microsipseed.Cliente`. Lo que falta para omitir `CLIENTES` y `DIRS_CLIENTES` es calcular su cierre transitivo de llaves foráneas —es amplio— y volver a correr la suite.

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

La base conserva los catálogos y **omite los movimientos**. Conteos reales del artefacto:

| Tabla | Filas |
|---|---|
| `CLIENTES` | 43,834 |
| `SALDOS_IN` (inventario) | 94,073 |
| `ARTICULOS` | 6,113 |
| `ZONAS_CLIENTES` | 46 |
| **`DOCTOS_PV`** (ventas) | **0** |
| **`DOCTOS_CC`** (crédito) | **0** |
| `MSP_SALDOS_VENTAS` | 88 |
| `MSP_PAGOS_VENTAS` | 18 |

**Ventas funciona bien.** Hay catálogos e inventario, y la suite prueba el flujo de aplicar una venta de principio a fin. Quien trabaje ahí crea sus propios datos y avanza.

**Cobranza, reportes, analítica y el Cliente 360 van a ver pantallas vacías**, porque leen los movimientos que se omitieron. No es coincidencia que los diez tests frágiles de abajo sean justamente de `clientes` y `cobranza`.

---

## Limitaciones conocidas

Dos cosas quedan sin resolver a propósito. Están documentadas aquí para que quien las encuentre sepa que ya se conocían y no crea que las rompió.

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

`internal/cobranza`, que este documento señalaba, **pasa completo**: sus pruebas ya sembraban sus propios documentos (`insertCargoDoctosCC` y compañía). Los dos de `analytics`, el de `rutas` y el de `reactivacion` sólo aparecen si se corre `./...`; el target de nueve paquetes ni los toca.

Ahora siembran lo que verifican, vía [`internal/platform/microsipseed`](../internal/platform/microsipseed/seed.go). El sembrador no inserta el cargo de cuentas por cobrar a mano: inserta el documento de punto de venta y voltea `APLICADO` de `'N'` a `'S'`, dejando que la cascada de Microsip genere el cargo, el puente `DOCTOS_ENTRE_SIS`, los importes y las cachés `MSP_SALDOS_VENTAS` / `MSP_PAGOS_VENTAS` — el mismo camino que el writer de producción.

De paso quedaron reforzadas: antes afirmaban `TotalComprado > 0` porque nadie sabía cuánto debía dar; ahora afirman la igualdad exacta, y `ConSaldo`, que contra la base reducida recorría una lista vacía y pasaba **sin verificar nada**, ahora siembra un cliente con saldo y otro sin él.

También se quitó la dependencia de `CLIENTES` reales (`12387`, `12440`, zona `21566`), que no fallaba pero era la misma fragilidad. Eso desbloquea lo de la sección siguiente.

### El target de integración cubre 9 de 25 paquetes

`make test-firebird-all` corre nueve paquetes. Veinticinco dependen de `FB_DATABASE`.

El bloqueo que impedía ampliarlo —los tests atados a filas de producción— ya no existe.

Queda una advertencia, con la evidencia que hay y no más: al correr `clientes` y `cobranza` juntos, **`TestSync_GoldenSnapshot` falló una vez**. No se reprodujo en 3 corridas paralelas más, 2 con `-p 1`, 2 del paquete solo y 1 del test aislado — todas en verde. La salida de la falla se perdió (el filtro de la corrida sólo conservó el nombre), así que **la causa no está probada**.

Lo que sí es un hecho es la condición de fondo: `go test ./...` corre los paquetes en PARALELO y esta suite comparte una sola base Firebird; el propio comentario de ese test dice que su marca de agua sale del `MinActiveTransactionID` vivo, que es estado global del servidor. Por eso el job de CI usa `-p 1`: cuesta tiempo de reloj y compra determinismo, que es lo único que hace útil una señal remota. Si alguien amplía `make test-firebird-all`, conviene que haga lo mismo.

### Cómo no volver a plantar una bomba de tiempo

Un tercer caso ya se corrigió. Dos tests de `analyticsfb` insertaban instantáneas con fecha del día y las buscaban en `ListRecentSnapshots(100)`, que ordena por `FECHA_CORTE DESC`. Pasaron hasta que el job diario acumuló más de cien instantáneas más recientes; ese día empezaron a fallar solos, sin que nadie tocara código.

La regla que dejan: **un test afirma sobre el código, no sobre el estado de la base compartida.** Si un fixture tiene que caer dentro de una ventana ordenada, su clave debe garantizarlo por construcción —una fecha que ningún corte real alcanza— y no por el tamaño que la tabla tenga hoy.

---

## Regenerar cuando la base cambie

Este artefacto es una foto del esquema y los catálogos al 2026-07-31. Hay que regenerarlo cuando se agreguen migraciones que cambien el esquema, o cuando cambien catálogos de los que dependan los tests.

El procedimiento completo toma unos **quince minutos**, casi todos de espera.

---

## Qué se probó y qué no

Cada recorte se validó corriendo los siete paquetes de integración contra la base resultante, y el artefacto final se verificó **descomprimiéndolo y restaurándolo a una base nueva**, no sobre la base desde la que se generó.

Dos recortes se intentaron y se revirtieron porque rompían tests:

| Tabla | Qué pasó |
|---|---|
| `SALDOS_IN` | Sin ella fallan seis tests de combos y juegos que verifican la descarga de inventario. Cuesta 8 MB; se queda |
| `PRECIOS_ARTICULOS` | Se probó quitarla y **la suite pasa completa**. Se queda fuera |

**Piso teórico: 12 MB.** Es el tamaño del respaldo con el esquema vacío. El artefacto pesa 15 MB, así que todos los catálogos juntos cuestan 3 MB comprimidos. No hay margen relevante para seguir recortando por tamaño.
