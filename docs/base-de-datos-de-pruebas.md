# Base de datos de pruebas — cómo se genera y cómo se usa

- **Fecha:** 2026-07-31
- **Artefacto:** `msp-test-db.fbk.gz` (~16 MB comprimido, 158 MB restaurado)
- **Origen:** `MUEBLERA.FDB` del contenedor `mueblera-firebird` (3.9 GB)

La base de desarrollo pesa 3.9 GB y contiene datos reales de clientes. Ni una cosa ni la otra sirve para repartirla entre desarrolladores. Este documento describe cómo se produce una versión de 16 MB con la que **pasa la suite de integración completa**.

---

## Para usar la base (lo que hace un desarrollador)

```sh
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

> ⚠️ **`go test` a secas no ve `FB_DATABASE`.** El target del Makefile carga `.env` con `include`; una invocación manual de `go test` no. Sin esa variable los tests de integración **se saltan en silencio** y el paquete reporta `ok`. Es un falso verde que cuesta una tarde.

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

**Omite** los movimientos (4.5 millones de filas entre cuentas por cobrar y punto de venta), las bitácoras, el rastreo GPS del sistema legado, las cachés derivadas y **todas las imágenes**.

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

Pesan poco —2,728 y 1,384 páginas— así que quitarlos no es cuestión de tamaño sino de no repartir datos personales entre máquinas de desarrollo. Los tests crean sus propios clientes con identificadores sintéticos (`900000001`, `777001`), aunque al menos uno busca un cliente existente.

**Decisión pendiente.** Si se omiten, hay que calcular su cierre transitivo —es amplio— y sembrar unos clientes sintéticos para el test que los necesita.

---

## Regenerar cuando la base cambie

Este artefacto es una foto del esquema y los catálogos al 2026-07-31. Hay que regenerarlo cuando se agreguen migraciones que cambien el esquema, o cuando cambien catálogos de los que dependan los tests.

El procedimiento completo toma unos **quince minutos**, casi todos de espera.
