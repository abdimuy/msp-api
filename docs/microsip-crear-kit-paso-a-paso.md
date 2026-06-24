# Crear un kit (juego) en Microsip — flujo paso a paso

> Receta de escritura para que **msp-api** dé de alta un **kit** (en Microsip se
> llama **juego**: artículo con `ES_JUEGO='S'` + receta de componentes) directamente
> en la base Firebird de Microsip (familia `ARTICULOS` / `JUEGOS_DET`). Todo lo de
> aquí está **verificado contra dos kits reales** creados desde el cliente Microsip
> Inventarios 2026 y capturados con trace de ejecución (`fbtracemgr`) sobre
> `DESARROLLO.FDB`: `KIT NUEVO PRUEBA` (ARTICULO_ID 3083057) y `KIT NUEVO PRUEBA 2`
> (ARTICULO_ID 3083058, con clave). Para cómo correr el trace ver
> [`microsip-trace-runbook.md`](./microsip-trace-runbook.md); para cómo se **vende**
> un kit (descarga de inventario por componentes) ver la sección final.

## TL;DR — el modelo en una frase

**Un kit es un artículo normal con `ES_JUEGO='S'` cuya receta vive en `JUEGOS_DET`.**
Crearlo es **catálogo puro**: insertas el artículo, su fila de extensión
`LIBRES_ARTICULOS`, y una fila por componente en `JUEGOS_DET`. **No** lleva precio,
**no** lleva impuestos y **no** tiene existencia propia (`SALDOS_IN`). El inventario
solo se mueve **al vender**, y se descarga de los **componentes**, no del kit.

⚠️ No hay cascada tipo "aplicar" como en ventas: aquí es solo alta de catálogo. El
único disparo automático son los triggers `BEFINS`/`BEFUPD` que asignan ID y validan.

## Tablas que escribes vs las que NO

| Escribes tú (manual) | Op | | Las pone Microsip solo / no aplican |
|---|---|---|---|
| `ARTICULOS` | INSERT + UPDATE | | `SALDOS_IN` (el kit NO tiene existencia) |
| `JUEGOS_DET` | INSERT (1×componente) | | `PRECIOS_ARTICULOS` (kit sin precio) |
| `LIBRES_ARTICULOS` | INSERT | | `IMPUESTOS_ARTICULOS` (kit sin impuesto) |
| `CLAVES_ARTICULOS` | INSERT (opcional, si pones clave/código) | | — |

## TL;DR del trace (qué disparó la DB)

Orden cronológico real capturado al crear `KIT NUEVO PRUEBA 2` (3083058):

1. `EXECUTE PROCEDURE GEN_CATALOGO_ID` → nuevo `ARTICULO_ID`.
2. `INSERT INTO ARTICULOS` (esqueleto, **`ES_JUEGO='N'` todavía**). Triggers
   `ARTICULOS_BEFINS` (asigna ID si es −1) y `ARTICULOS_BEFINSUPD` (valida nombre
   único vía `PKG_CADENAS_COLLATE.EXISTE_NOMBRE_ARTICULO`).
3. *(si pones clave)* `GEN_CATALOGO_ID` → `INSERT INTO CLAVES_ARTICULOS`. Triggers
   `CLAVES_ARTICULOS_BEFINS`, `CLAVES_ARTICULOS_BEFINSUPD_RI`.
4. `INSERT INTO JUEGOS_DET` (uno por componente).
5. Al **Guardar**: `UPDATE ARTICULOS` con todos los campos (**ahora `ES_JUEGO='S'`**)
   → `INSERT INTO LIBRES_ARTICULOS` → `UPDATE ARTICULOS` (usuario/fecha últ. modif).
   Triggers `ARTICULOS_BEFUPD_0/1`, `ARTICULOS_BEFUPDDEL_RI`, `ARTICULOS_BEFINSUPD`.

## Prerrequisitos (deben existir en Microsip)

- **Los artículos componente** ya dados de alta en `ARTICULOS` (sus `ARTICULO_ID`,
  y su `CLAVE_ARTICULO_ID` si lo referencias en la receta). Los componentes son los
  que cargan inventario al vender, así que típicamente son `ES_ALMACENABLE='S'`.
- **`LINEA_ARTICULO_ID`** — la línea/categoría del artículo (ej. 11774 en las pruebas).
- Para la clave (opcional): un **`ROL_CLAVE_ART_ID`** válido — **17** es la clave
  principal (el mismo rol que usan nuestros tests e2e).

No requiere preferencias de empresa (a diferencia de ventas): el alta de catálogo no
está gateada por `INTEG_*`.

## El generador de IDs

Todo ID de catálogo (artículo **y** clave) sale del procedimiento `GEN_CATALOGO_ID`,
que es simplemente:

```sql
CATALOGO_ID = GEN_ID(ID_CATALOGOS, 1);
```

Además, el trigger `ARTICULOS_BEFINS` aplica el patrón **`-1` → autogen**:

```sql
IF (NEW.ARTICULO_ID = -1) THEN
   NEW.ARTICULO_ID = GEN_ID(ID_CATALOGOS, 1);
```

**Patrón recomendado:** pasar `-1` en `ARTICULO_ID` y dejar que el trigger lo asigne,
o tomarlo tú con `GEN_ID(ID_CATALOGOS, 1)`. Para `CLAVE_ARTICULO_ID` usa el mismo
generador (`GEN_ID(ID_CATALOGOS, 1)` / `GEN_CATALOGO_ID`).

## Columnas obligatorias (verificado contra el esquema, `RDB$RELATION_FIELDS`)

Distinción: **obligatorio por esquema** (`NOT NULL` sin default → si falta, falla el
INSERT) **≠ obligatorio por lógica**.

**`ARTICULOS`**
- *Obligatorias por esquema:* `ARTICULO_ID`, `NOMBRE`, `APLICAR_FACTOR_VENTA`,
  `FACTOR_VENTA`, `RED_PRECIO_CON_IMPTO`.
- *Necesarias por lógica para que sea kit:* **`ES_JUEGO='S'`**, `ES_ALMACENABLE`
  (`'S'`/`'N'` según si el kit se ensambla a stock o se vende por componentes),
  `ESTATUS='A'` (activo), `LINEA_ARTICULO_ID`.
- *Auto por DEFAULT de columna (no las pases):* `USUARIO_CREADOR` (= `USER`),
  `FECHA_HORA_CREACION` (= timestamp). En el trace quedaron `SYSDBA` / la hora.

**`JUEGOS_DET`**
- *Obligatorias por esquema:* `ARTICULO_ID` (el juego), `COMPONENTE_ID`.
- *Necesarias por lógica:* `UNIDADES` (cantidad del componente por juego),
  `CLAVE_ARTICULO_ID`, `ES_REEMPLAZABLE` (`'N'`), `PERMITIR_MODIF_UNID` (`'N'`).

**`LIBRES_ARTICULOS`**
- *Obligatoria por esquema:* solo `ARTICULO_ID`. (Resto nullable/default.)

**`CLAVES_ARTICULOS`** (opcional)
- *Obligatorias por esquema:* `CLAVE_ARTICULO_ID`, `CLAVE_ARTICULO`, `ARTICULO_ID`,
  `ROL_CLAVE_ART_ID`.

## Paso a paso

Todo dentro de **una transacción** (`READ_COMMITTED`, `READ_WRITE`). Si algo falla,
`ROLLBACK` completo.

### Fase 1 — Insertar el artículo (esqueleto)

`INSERT INTO ARTICULOS (...)`. Microsip inserta primero con **`ES_JUEGO='N'`** y lo
cambia a `'S'` en el guardado (Fase 4); puedes insertarlo directamente con `'S'` si
prefieres. Columnas y valores reales capturados (kit 3083058):

| Columna | Valor | Nota |
|---|---|---|
| `ARTICULO_ID` | 3083058 | PK (`-1` → autogen `ID_CATALOGOS`) |
| `NOMBRE` | `KIT NUEVO PRUEBA 2` | NOT NULL, único (validado por trigger) |
| `ES_ALMACENABLE` | `S` | |
| `ES_JUEGO` | `N` → luego `S` | **marca de kit** (se vuelve `S` al guardar) |
| `ESTATUS` | `A` | activo |
| `FECHA_SUSP` / `CAUSA_SUSP` | NULL | |
| `IMPRIMIR_COMP` | `N` | imprimir componentes en ticket |
| `PERMITIR_AGREGAR_COMP` | NULL | |
| `LINEA_ARTICULO_ID` | 11774 | línea/categoría |
| `UNIDAD_VENTA` / `UNIDAD_COMPRA` | NULL | |
| `CONTENIDO_UNIDAD_COMPRA` | 1 | |
| `PESO_UNITARIO` | 0 | |
| `ES_PESO_VARIABLE` | `N` | |
| `SEGUIMIENTO` | `N` | |
| `DIAS_GARANTIA` | 0 | |
| `ES_IMPORTADO` | `N` | |
| `ES_SIEMPRE_IMPORTADO` | `S` | |
| `PCTJE_ARANCEL` | 0 | |
| `NOTAS_COMPRAS` / `NOTAS_VENTAS` | NULL (blob) | |
| `IMPRIMIR_NOTAS_COMPRAS` / `IMPRIMIR_NOTAS_VENTAS` | `N` / `N` | |
| `CUENTA_ALMACEN` … `CUENTA_DEVOL_COMPRAS` (6 cuentas) | NULL | contables |
| `APLICAR_FACTOR_VENTA` | `N` | NOT NULL |
| `FACTOR_VENTA` | 0 | NOT NULL |
| `RED_PRECIO_CON_IMPTO` | `N` | NOT NULL |
| `FACTOR_RED_PRECIO_CON_IMPTO` | 0.01 | |
| `USUARIO_AUT_CREACION` / `USUARIO_AUT_MODIF` | NULL | |
| `USUARIO_CREADOR` / `FECHA_HORA_CREACION` | (DEFAULT) | los pone la DB |

### Fase 2 — Clave / código (opcional)

Solo si el kit lleva código de barras / clave. Toma `CLAVE_ARTICULO_ID` de
`GEN_CATALOGO_ID` y:

```sql
INSERT INTO CLAVES_ARTICULOS
  (CLAVE_ARTICULO_ID, CLAVE_ARTICULO, ARTICULO_ID, ROL_CLAVE_ART_ID, CONTENIDO_EMPAQUE)
VALUES (?, 'CLAVE12345', :articulo_id, 17, 1.0);
```

`ROL_CLAVE_ART_ID = 17` = clave principal.

### Fase 3 — Receta: componentes (uno por fila)

`INSERT INTO JUEGOS_DET (...)` por cada componente del kit:

```sql
INSERT INTO JUEGOS_DET
  (ARTICULO_ID, COMPONENTE_ID, CLAVE_ARTICULO_ID, UNIDADES, ES_REEMPLAZABLE, PERMITIR_MODIF_UNID)
VALUES (:articulo_id, :componente_id, :clave_componente_id, :unidades, 'N', 'N');
```

Ejemplo real (`KIT NUEVO PRUEBA`, 3083057): componente 483 *BASE DE MADERA
MATRIMONIAL CHOCOLATE* (clave 484) ×1, y componente 582 *COLCHON LESTER QUADRANT
MATRIMONIAL* (clave 583) ×1.

> **`UNIDADES` = cantidad del componente por UN juego.** Al vender N juegos, Microsip
> multiplica (`ALTA_COMPONENTES_PV`: `UNIDADES_SUBMOVTO = UNIDADES_VENDIDAS × UNIDADES`).

### Fase 4 — Guardar: completar el artículo + extensión

`UPDATE ARTICULOS` con todos los campos finales (**aquí `ES_JUEGO='S'`**), luego la
fila de extensión obligatoria:

```sql
INSERT INTO LIBRES_ARTICULOS
  (ARTICULO_ID, MULTIPLOVENTA, VOLUMEN, OBSERVACION, LINK_IMG, ENVIADA, DESCRIPCION_CORTA)
VALUES (:articulo_id, 0, 0, NULL, NULL, NULL, NULL);
```

Microsip cierra con un `UPDATE ARTICULOS SET USUARIO_ULT_MODIF=USUARIO_CREADOR,
FECHA_HORA_ULT_MODIF=FECHA_HORA_CREACION`. `COMMIT`.

### Patrón de creación atómica (recomendado)

Igual que en ventas: todo el alta se puede hacer en **un solo `EXECUTE BLOCK`** de
Firebird — tomar `ARTICULO_ID` y `CLAVE_ARTICULO_ID` con `GEN_ID(ID_CATALOGOS,1)` (o
pasar `-1` en el artículo y dejar que `BEFINS` lo asigne), insertar ARTICULOS +
LIBRES_ARTICULOS + JUEGOS_DET (+ CLAVES_ARTICULOS), y `COMMIT` (o `ROLLBACK` para
validar sin persistir).

## Cómo se vende un kit (descarga de inventario)

El kit **no tiene existencia**; al venderlo, Microsip descarga sus **componentes**:

1. **Captura de venta** — al meter el renglón del juego (`ROL='J'`, lleva el precio),
   `ALTA_COMPONENTES_PV` explota la receta (`LISTA_COMPONENTES`) e inserta una línea
   `DOCTOS_PV_DET` `ROL='C'` por componente (**precio 0**, `UNIDADES = vendidas ×
   receta`), ligada al padre vía `SUB_MOVTOS_PV`.
2. **Aplicar** — `GENERA_DOCTO_IN_PV` crea un `DOCTOS_IN` (concepto venta) que
   descarga los componentes almacenables (vía `GENERA_DOCTO_IN_PV_MOVTO` +
   `SUB_MOVTOS_IN`), ligado a la venta por `DOCTOS_ENTRE_SIS`.

Invariantes (verificados): toda línea `ROL='J'` tiene precio > 0; toda `ROL='C'`
tiene precio = 0. Ver el flujo de venta en
[`microsip-crear-venta-paso-a-paso.md`](./microsip-crear-venta-paso-a-paso.md).

### Precio del kit cuando lo escribe msp-api (NO requiere `PRECIOS_ARTICULOS`)

**Para la sincronización de ventas locales (Android → Microsip), el juego NO necesita
precio en ninguna lista de precios.** Nuestro `VentaWriter`
(`internal/ventas/infra/microsip/venta_writer.go`, `insertDetalles`) escribe el precio
**explícito en el renglón** `DOCTOS_PV_DET` — `PRECIO_UNITARIO_IMPTO` = el precio de la
venta local y `PRECIO_UNITARIO` (neto) = ese precio ÷ (1 + IVA propio del artículo) —
tomándolo del snapshot de la venta: `producto.Precios().Contado()` (contado) o
`.Anual()` (crédito). **Nunca** llama a `GET_PRECIO_ARTCLI` ni lee `PRECIOS_ARTICULOS`;
solo lee el IVA del artículo (`IMPUESTOS_ARTICULOS`). El precio es 100% nuestro.

Por lo tanto, al vender un kit por código: la línea `ROL='J'` lleva **nuestro** precio
del kit (el de la venta local) y los componentes `ROL='C'` van en 0. Crear el juego es
**solo catálogo** (`ARTICULOS` + `JUEGOS_DET` + `LIBRES_ARTICULOS`) — **sin
`PRECIOS_ARTICULOS`, sin `SALDOS_IN`, sin impuestos**. No hay "Fase 5 — precio".

⚠️ La regla #1 de abajo (juego sin precio en lista → se vende en 0) aplica **solo** al
cliente de Microsip cuando se captura a mano (ahí el lookup pre-llena el renglón). Para
el writer automático no aplica, porque el precio lo ponemos nosotros en la línea.

## Reglas clave / errores a evitar

1. **El kit es catálogo; para venderlo ≠ 0 _desde el cliente de Microsip_ el juego SÍ
   necesita precio propio en una lista de precios** (para la escritura por código desde
   msp-api **NO** — ver "[Precio del kit cuando lo escribe msp-api](#precio-del-kit-cuando-lo-escribe-msp-api-no-requiere-precios_articulos)" arriba: el precio va en el renglón).
   Para que *exista* no hace falta `SALDOS_IN`/`PRECIOS_ARTICULOS`/
   `IMPUESTOS_ARTICULOS`. **Verificado:** el precio NO está en el form General del
   artículo (ahí "Peso unitario", p. ej. 20,000.00, es el **peso**, no el precio); el
   precio vive en las **listas de precios** (`PRECIOS_ARTICULOS`). El lookup de venta
   `GET_PRECIO_ARTCLI` devuelve el precio propio del artículo en su lista y **NO suma
   componentes** → un juego sin fila en una lista de precios se vende en **0** aunque
   sus componentes tengan precio. Para darle precio al kit: asignarle precio al
   **juego** en una lista de precios (función "Precios", no el form del artículo) o
   capturarlo a mano en el renglón de la venta. "Precio del kit = suma de componentes"
   sería una función/config aparte de Microsip, no es automático en la venta.
2. **`ES_JUEGO='S'` es lo que lo hace kit.** Sin eso es un artículo normal aunque
   tenga filas en `JUEGOS_DET`.
3. **`LIBRES_ARTICULOS` es obligatoria de facto.** Microsip crea siempre la fila de
   extensión; replícala.
4. **`NOMBRE` es único** — lo valida el trigger `ARTICULOS_BEFINSUPD`
   (`EXISTE_NOMBRE_ARTICULO`). Un nombre duplicado falla el alta.
5. **IDs vía `GEN_ID(ID_CATALOGOS,1)`** (artículo y clave), o `-1` + trigger `BEFINS`.
6. **Los componentes deben existir** en `ARTICULOS` antes de referenciarlos en
   `JUEGOS_DET` (FK).
7. **`UNIDADES` en `JUEGOS_DET` es por-juego**, no la cantidad total — Microsip
   multiplica al vender.
8. **Encoding:** las tablas legacy de Microsip son `ISO8859_1` (no UTF-8). El adapter
   Firebird debe aislar la conversión (ver `ENCODING_HANDLING.md`).

## Estado de verificación

- ✅ **Observado** end-to-end: dos kits reales creados desde el cliente Microsip y
  capturados con `fbtracemgr` sobre `DESARROLLO.FDB` (FB 5.0), jun 2026. Tablas,
  columnas, triggers, procedimientos y secuencia documentados arriba.
- ✅ Esquema (NOT NULL, generador, triggers `BEFINS`) verificado con
  `RDB$RELATION_FIELDS` / `RDB$TRIGGERS` / `RDB$PROCEDURES`.
- ✅ **EXECUTE BLOCK verificado:** alta vía insert directo (`EXECUTE BLOCK` +
  `ROLLBACK`) confirmada: catálogo ARTICULOS + LIBRES_ARTICULOS + JUEGOS_DET se
  inserta sin el cliente Microsip (spike documentado en Task 2, jun 2026). El
  writer de msp-api (`JuegoResolver.insertJuego`) usa el mismo camino y produce
  ARTICULOS con `ES_JUEGO='S'` capaces de ser vendidos. El flujo completo
  (combo → juego → ROL='J'+ROL='C' → descarga de inventario vía
  `GENERA_DOCTO_IN_PV`) está cubierto por el E2E
  `TestE2E_ComboJuego_FullCycle_WithResolver` (venthttp) y los tests de Task 4
  (`TestE2E_AplicarComboJuego_*` en ventfb).
