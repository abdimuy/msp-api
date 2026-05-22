# Crear una venta en Microsip — flujo paso a paso

> Receta de escritura para que **msp-api** dé de alta una venta de Punto de Venta
> directamente en la base Firebird de Microsip (familia `DOCTOS_PV`). Todo lo de
> aquí está **verificado contra ventas reales** capturadas con trace de ejecución
> (`fbtracemgr`): crédito (DOCTO 15239152, FOLIO `Y00002262`) y contado (DOCTO
> 15239168). Para el detalle del trace y los cuerpos de los procedures, ver
> [`microsip-venta-flow.md`](./microsip-venta-flow.md).

## TL;DR — el modelo en una frase

**Solo escribes la familia `DOCTOS_PV*` (+ el contador de folio) con `APLICADO='N'`,
y luego haces `UPDATE … APLICADO='S'`. Ese flip dispara toda la cascada de
Microsip**, que automáticamente:
- baja la existencia del almacén (`DOCTOS_IN` + saldos de inventario),
- genera la cuenta por cobrar (`DOCTOS_CC` + importes),
- registra el movimiento de caja (`MOVTOS_EFVO_CAJA`),
- encola el cobro (`MGGENCOBRO_*`).

⚠️ **No** se puede insertar directo con `APLICADO='S'`: la aplicación cuelga del
trigger AFTER UPDATE de la transición `N→S`. Un INSERT con `'S'` **no** aplica.

## Tablas que escribes vs las que NO

| Escribes tú (manual) | Op | | Las genera Microsip solo (NO tocar) |
|---|---|---|---|
| `DOCTOS_PV` | INSERT + UPDATEs | | `DOCTOS_IN` / `DOCTOS_IN_DET` (inventario) |
| `DOCTOS_PV_DET` | INSERT (1×prod) | | `SALDOS` / existencias, `CAPAS_COSTOS` |
| `DOCTOS_PV_COBROS` | INSERT | | `DOCTOS_CC` / `IMPORTES_DOCTOS_CC` (CxC) |
| `LIBRES_VTA_PV` | INSERT | | `IMPUESTOS_DOCTOS_PV_DET` (impuestos) |
| `FOLIOS_CAJAS` | UPDATE (tx aparte) | | `MOVTOS_EFVO_CAJA`, `MGGENCOBRO_*` |
| | | | `DOCTOS_ENTRE_SIS` (liga PV↔CC) |

## Prerrequisitos

### Datos de catálogo (deben existir en Microsip)
`CAJA_ID`, `SUCURSAL_ID`, `ALMACEN_ID`, `CAJERO_ID`, `CLIENTE_ID` (+ `CLAVE_CLIENTE`),
`MONEDA_ID`, y por renglón `ARTICULO_ID` (+ `CLAVE_ARTICULO`), `FORMA_COBRO_ID`.

### Preferencias de empresa (condicionan la cascada)
La generación de inventario y CxC está **gateada por preferencias** que lee
`APLICA_DOCTO_PV` del *registry* de Microsip:
- **`INTEG_IN_PV='S'`** → genera el documento de inventario (descuenta almacén).
- **`INTEG_CC_PV='S'`** → genera la cuenta por cobrar.
- **`CLIENTE_EVENTUAL_PV_ID`** → si el `CLIENTE_ID` de la venta **es** el cliente
  eventual/genérico, **NO** se genera `DOCTOS_CC`.

En `DESARROLLO` ambas están en `'S'`. **Verificar en la empresa destino real.**

### Otras condiciones
- Un **corte de caja abierto** para esa `CAJA_ID` (el cobro escribe `MOVTOS_EFVO_CAJA`).
- El artículo debe estar marcado **`ES_ALMACENABLE='S'`** para que afecte stock
  (servicios / no almacenables no descuentan inventario).

## El folio

No es un generador; es la tabla contador **`FOLIOS_CAJAS`**:

| Columna | Ejemplo |
|---|---|
| `CAJA_ID` | 22198 |
| `TIPO_DOCTO` | `V` (venta) |
| `SERIE` | `Y` |
| `CONSECUTIVO` | 2262 |

- **Folio = `SERIE` + `CONSECUTIVO` con padding a 8 dígitos** → `Y` + `00002262`
  = `Y00002262` (`CHAR(9)`).
- Antes de aplicar, el documento usa un folio **temporal** `_01788986` (lo genera
  el procedure `GEN_FOLIO_TEMP`).
- Microsip verifica unicidad: `SELECT FOLIO FROM DOCTOS_PV WHERE TIPO_DOCTO='V' AND FOLIO=?`.
- Al final hace **`UPDATE FOLIOS_CAJAS SET CONSECUTIVO = CONSECUTIVO+1`** — y eso va
  en **una transacción separada** de la del save (para que dos ventas concurrentes
  no repitan folio). Replicar esa atomicidad al reservar el folio.

## Columnas obligatorias (verificado contra el esquema en Docker `MUEBLERA.FDB`)

Distinción importante: **obligatorio por esquema** (`NOT NULL` sin default → si falta,
falla el INSERT) **≠ obligatorio por lógica** (la columna es nullable, pero sin ella
la venta queda mal o no aplica). Lista verificada con `RDB$RELATION_FIELDS`:

**`DOCTOS_PV`**
- *Obligatorias por esquema:* `DOCTO_PV_ID`, `CAJA_ID`, `TIPO_DOCTO`, `SUCURSAL_ID`,
  `FOLIO`, `SISTEMA_ORIGEN`.
- *`NOT NULL` con default* (puedes omitir; Microsip las rellena): `FECHA`, `HORA`,
  `PESO_EMBARQUE`, `PROCESO_ORIGEN`, `ES_FAC_GLOBAL`, `CFDI_CERTIFICADO`.
- *Nullable por esquema pero necesarias por lógica:* **`ALMACEN_ID`** (sin él no
  hay descuento de inventario), `CLIENTE_ID` (+ `CLAVE_CLIENTE`), `MONEDA_ID`,
  `ESTATUS='N'`, `APLICADO='N'` (su default es `N`, pero conviene setearlo explícito).

**`DOCTOS_PV_DET`**
- *Obligatorias por esquema:* `DOCTO_PV_DET_ID`, `DOCTO_PV_ID`, `ARTICULO_ID`,
  **`ROL`** (¡`NOT NULL` sin default! → `'N'`), `PRECIO_MODIFICADO` (default `'N'`),
  `POSICION` (default + trigger).
- *Necesarias por lógica:* `UNIDADES`, `PRECIO_UNITARIO`, `PRECIO_UNITARIO_IMPTO`
  (tienen default 0, pero una venta con 0 no sirve). `CLAVE_ARTICULO` es nullable.

**`DOCTOS_PV_COBROS`**
- *Obligatorias por esquema:* `DOCTO_PV_COBRO_ID`, `DOCTO_PV_ID`, `TIPO`,
  `FORMA_COBRO_ID`. (`IMPORTE`, `IMPORTE_MON_DOC` tienen default pero los provees.)

**`LIBRES_VTA_PV`**: solo `DOCTO_PV_ID`.

### Auto-generación de IDs y POSICION (triggers `BEFINS`)
- `DOCTOS_PV_BEFINS`: si `DOCTO_PV_ID = -1` → `GEN_ID(ID_DOCTOS,1)`.
- `DOCTOS_PV_DET_BEFINS`: si `DOCTO_PV_DET_ID = -1` → `GEN_ID`; si `POSICION = -1`
  → `MAX(POSICION)+1` del documento (arranca en 1).
- **Patrón recomendado:** pasar **`-1`** en los PK y en `POSICION` y dejar que el
  trigger los asigne. Todos los IDs salen del mismo generador `ID_DOCTOS`.

## Paso a paso

Todo dentro de **una transacción** (`READ_COMMITTED`, `READ_WRITE`). Si algo falla,
`ROLLBACK` completo. Los IDs (`DOCTO_PV_ID`, det, cobro) salen del mismo generador
`GEN_ID(ID_DOCTOS, 1)` y son consecutivos; se puede pasar `-1` y el trigger `BEFINS`
lo reemplaza.

### Fase 1 — Cabecera, SIN aplicar

`INSERT INTO DOCTOS_PV (...)` con `APLICADO='N'`, `IMPORTE_NETO=0` (se recalcula),
FOLIO temporal. Columnas/valores reales capturados:

| Columna | Valor | Nota |
|---|---|---|
| `DOCTO_PV_ID` | 15239152 | PK (`-1` → autogen) |
| `CAJA_ID` | 22198 | NOT NULL |
| `TIPO_DOCTO` | `V` | venta |
| `SUCURSAL_ID` | 225490 | NOT NULL |
| `FOLIO` | `_01788986` | temporal al insertar |
| `FECHA` / `HORA` | 2026-05-21 / 18:54:05 | |
| `CAJERO_ID` | 22392 | |
| `CLAVE_CLIENTE` / `CLIENTE_ID` | `0004546` / 47913 | |
| `ALMACEN_ID` | 11058 | **de dónde sale la mercancía** |
| `MONEDA_ID` | 1 | |
| `IMPUESTO_INCLUIDO` | `S` | precios con IVA incluido |
| `TIPO_CAMBIO` | 1.0 | |
| `TIPO_DSCTO` / `DSCTO_PCTJE` / `DSCTO_IMPORTE` | `P` / 0 / 0 | |
| `ESTATUS` | `N` | normal |
| **`APLICADO`** | **`N`** | **clave: sin aplicar** |
| `IMPORTE_NETO` / `TOTAL_IMPUESTOS` | 0 / 0 | se recalculan en fase 3 |
| `SISTEMA_ORIGEN` | `PV` | NOT NULL |
| `CONTABILIZADO` / `TICKET_EMITIDO` | `N` / `N` | |
| `CARGAR_SUN` | `S` | |
| `UNID_COMPROM` | `N` | |
| `USUARIO_CREADOR` | `RUTA25` | |
| `FECHA_HORA_CREACION` | 2026-05-21T18:54:05 | |

(Resto de columnas nullables: `CLIENTE_FAC_ID`, `DIR_CLI_ID`, `VENDEDOR_ID`,
`DESCRIPCION`, `FECHA_VIGENCIA`, etc. → `NULL`.)

### Fase 2 — Renglones (uno por producto)

`INSERT INTO DOCTOS_PV_DET (...)`. El trigger `DOCTOS_PV_DET_BEFINS` asigna
`POSICION` = `MAX(POSICION)+1` si se pasa el patrón correcto.

| Columna | Valor | Nota |
|---|---|---|
| `DOCTO_PV_DET_ID` | 15239153 | PK |
| `DOCTO_PV_ID` | 15239152 | FK a la cabecera |
| `CLAVE_ARTICULO` / `ARTICULO_ID` | `BOC.KAISERRECMSA2029` / 2532100 | |
| `UNIDADES` / `UNIDADES_DEV` | 1 / 0 | |
| `PRECIO_UNITARIO` | 3724.137931 | **neto** (sin IVA) |
| `PRECIO_UNITARIO_IMPTO` | 4320.000000 | con IVA |
| `PCTJE_DSCTO` | 0 | |
| `PRECIO_TOTAL_NETO` | 0 | se calcula |
| `PRECIO_MODIFICADO` | `N` | |
| `PCTJE_COMIS` | 0 | |
| `ROL` | `N` | |
| `POSICION` | 1 | lo asigna el trigger |
| `TIPO_CONTAB_UNID` | `0` | |
| `ES_TRAN_ELECT` | `N` | |
| `IMPUESTO_POR_UNIDAD` | 0 | |

### Fase 3 — Recalcular totales

`UPDATE DOCTOS_PV SET IMPORTE_NETO=?, TOTAL_IMPUESTOS=? …`. Microsip los calcula con
el procedure `CALC_TOTALES_DOCTO_PV`; msp-api puede calcularlos en Go y setearlos.
(El cliente real hace varios UPDATE intermedios mientras el usuario edita el form;
para nosotros basta dejar la cabecera con los totales correctos antes de aplicar.)

### Fase 4 — Forma de cobro (decide contado vs crédito)

`INSERT INTO DOCTOS_PV_COBROS (...)`:

| Columna | Crédito | Contado |
|---|---|---|
| `DOCTO_PV_COBRO_ID` | 15239154 | (autogen) |
| `DOCTO_PV_ID` | 15239152 | |
| `TIPO` | `C` | `C` |
| **`FORMA_COBRO_ID`** | **71** (Crédito, TIPO=`R`) | **67** (Efectivo, TIPO=`E`) |
| `IMPORTE` / `IMPORTE_MON_DOC` | 4320.00 | total |
| `TIPO_CAMBIO` | 1.0 | |

**El tipo de venta NO es un campo de la cabecera** — se deriva del `FORMA_COBRO_ID`.

### Fase 5 — Campos libres

`INSERT INTO LIBRES_VTA_PV (DOCTO_PV_ID) VALUES (?)` — única columna obligatoria;
el resto queda NULL/default. Los `IMPUESTOS_DOCTOS_PV_DET` se generan solos en el
recálculo de impuestos (no es insert manual).

### Fase 6 — APLICAR (el paso que dispara todo)

```sql
UPDATE DOCTOS_PV
SET APLICADO = 'S',
    FOLIO    = 'Y00002262',   -- folio definitivo (fase del folio)
    IMPORTE_NETO = ?, TOTAL_IMPUESTOS = ?, ...
WHERE DOCTO_PV_ID = 15239152;
```

La transición **`APLICADO` `N→S`** dispara `DOCTOS_PV_AFTUPD_0/_1` →
`APLICA_DOCTO_PV` → `APLICA_VTA_PV`, que automáticamente:
- **Inventario** (si `INTEG_IN_PV='S'`): `GENERA_DOCTO_IN_PV` crea `DOCTOS_IN`
  (salida del `ALMACEN_ID` de la cabecera) y baja existencias (`AFECTA_SALDOS_IN`).
- **CxC** (si `INTEG_CC_PV='S'` y el cliente ≠ eventual): `GENERA_DOCTO_CC_PV`.

Luego `UPDATE FOLIOS_CAJAS` (incremento del folio, tx aparte) y `COMMIT`.

## Contado vs crédito — la única diferencia real

Ambos corren **la misma cascada**. Lo único que cambia es cuántos `DOCTOS_CC`
genera `GENERA_DOCTO_CC_PV`, según el importe pagado a crédito (forma con `TIPO='R'`):

| | Crédito (forma 71) | Contado (forma 67) |
|---|---|---|
| `DOCTOS_CC` | 1 (solo el **cargo**) | 2 (**cargo + pago**) |
| Saldo del cliente | queda abierto | liquidado (neto cero) |
| Inventario | igual | igual |

Para contado **no** generas el pago a mano: lo crea la cascada al detectar que la
forma de cobro no es de crédito.

## Fase 7 — Datos particulares + enganche (customización MSP)

Microsip de fábrica **no** llena estos campos; los pone un sistema externo de MSP.
Para replicar una venta MSP completa hay que hacerlo nosotros, **después** de que
la venta aplique y genere el cargo (`DOCTOS_CC` concepto 5 "Venta en mostrador").

### 7a. Obtener el cargo generado
```sql
SELECT D.DOCTO_CC_ID
FROM DOCTOS_ENTRE_SIS E JOIN DOCTOS_CC D ON D.DOCTO_CC_ID = E.DOCTO_DEST_ID
WHERE E.CLAVE_SIS_FTE='PV' AND E.CLAVE_SIS_DEST='CC' AND E.DOCTO_FTE_ID = :docto_pv;
```

### 7b. Datos particulares → `LIBRES_CARGOS_CC`
`INSERT INTO LIBRES_CARGOS_CC` (keyed por el `DOCTO_CC_ID` del cargo). Columnas
(las que son ID apuntan a catálogos de listas custom — valores reales observados):

| Columna | Tipo | Ejemplo real | Nota |
|---|---|---|---|
| `DOCTO_CC_ID` | id | (cargo) | FK al cargo |
| `FORMA_DE_PAGO` | id | 33824 | = "Semanal" (ID de lista) |
| `PARCIALIDAD` | num | 250 | importe del abono |
| `CREDITO_EN_MESES` | id | 33828 | ID de lista |
| `TIEMPO_A_CORTO_PLAZOMESES` | num | 4 | |
| `MONTO_A_CORTO_PLAZO` | num | 10600 | |
| `VENDEDOR_1/2/3` | id | 46279 | empleado/vendedor |
| `NUMERO_DE_VENDEDORES` | id | 47558 | ID de lista |
| `ENGANCHE` | money | 500.00 | **= el del pago de enganche** |
| `PRECIO_DE_CONTADO` | money | 9100.00 | |
| `AVAL_O_RESPONSABLE` | texto | | |
| `OBSERVACIONES` | texto | | |

### 7c. Movimiento de pago de enganche → `DOCTOS_CC` (concepto Enganche)
Es un documento de CxC **aparte**, naturaleza abono, que se liga al cargo y baja
el saldo. Mismo patrón insert-`N`→update-`S`.

1. **Folio** de `FOLIOS_CONCEPTOS` por `(SISTEMA='CC', CONCEPTO_ID=24533, SUCURSAL_ID)`:
   serie `@` (sin prefijo) → folio = `LPAD(CONSECUTIVO, 9, '0')`. Incrementar al final.
2. **`INSERT INTO DOCTOS_CC`**: `CONCEPTO_CC_ID=24533` ("Enganche"),
   `NATURALEZA_CONCEPTO='R'`, `SISTEMA_ORIGEN='CC'`, `SUCURSAL_ID`, `CLIENTE_ID`,
   `CLAVE_CLIENTE`, `FECHA`, `TIPO_CAMBIO=1`, `DESCRIPCION='Enganche'`,
   `ESTATUS='N'`, `ESTATUS_ANT='N'`, `APLICADO='N'`, `COBRADOR_ID` y `COND_PAGO_ID`
   del cliente. (Obligatorias por esquema: `DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO,
   NATURALEZA_CONCEPTO, SUCURSAL_ID, FECHA, CLIENTE_ID, SISTEMA_ORIGEN, ESTATUS_ANT`.)
3. **`INSERT INTO IMPORTES_DOCTOS_CC`**: `DOCTO_CC_ID`=enganche,
   **`DOCTO_CC_ACR_ID`=cargo**, `TIPO_IMPTE='R'`, `IMPORTE`=enganche, `FECHA`,
   `IMPUESTO=0`, etc.
4. **`UPDATE DOCTOS_CC SET APLICADO='S'`** → dispara `AFECTA_SALDOS_CC` (baja el
   saldo del cliente en el monto del enganche).
5. `UPDATE FOLIOS_CONCEPTOS SET CONSECUTIVO=CONSECUTIVO+1` para ese concepto/sucursal.

**Regla:** `LIBRES_CARGOS_CC.ENGANCHE` y el `IMPORTE` del pago de enganche deben
ser **el mismo valor**.

> Catálogos pendientes de mapear: los IDs de lista (`FORMA_DE_PAGO`,
> `CREDITO_EN_MESES`, `NUMERO_DE_VENDEDORES`) — reutilizar los observados o ubicar
> la tabla de valores de listas custom.

## Reglas clave / errores a evitar

1. **Nunca insertar con `APLICADO='S'`.** Debe ser INSERT `N` → UPDATE `S`.
2. **`DOCTOS_CC` no tiene `DOCTO_PV_ID`** — se liga a la venta vía la tabla
   `DOCTOS_ENTRE_SIS` (`CLAVE_SIS_FTE='PV'`, `CLAVE_SIS_DEST='CC'`). (Por eso el
   footprint original no lo detectó.)
3. **Las preferencias `INTEG_IN_PV` / `INTEG_CC_PV` mandan.** Si están en `'N'`,
   no se genera inventario / CxC aunque insertes todo bien.
4. **Cliente eventual = sin CxC.** Si el `CLIENTE_ID` es el cliente genérico, no
   hay `DOCTOS_CC`.
5. **Folio en transacción aparte** para evitar colisiones.
6. **Encoding:** las tablas legacy de Microsip son `ISO8859_1` (no UTF-8). El
   adapter Firebird debe aislar la conversión (ver `ENCODING_HANDLING.md`).
7. **Siempre incrementar el contador de folio JUNTO con crear el documento.**
   Tanto `FOLIOS_CAJAS` (venta) como `FOLIOS_CONCEPTOS` (enganche/CC) deben subir
   su `CONSECUTIVO` en la misma operación. Si creas el doc pero olvidas el
   `+1`, el contador queda desfasado y el **siguiente** documento reusa el folio →
   colisión. (Nos pasó: el enganche se creó con `000067820` pero el contador quedó
   en `67820`; hubo que subirlo a mano.) Hacerlo idealmente en la misma transacción.
8. **Los campos ID de `LIBRES_CARGOS_CC` (`FORMA_DE_PAGO`, `CREDITO_EN_MESES`,
   `NUMERO_DE_VENDEDORES`, `VENDEDOR_*`) NO tienen FK** — son `LONG` libres que
   apuntan a catálogos de listas custom. No fallan por integridad, pero usa IDs que
   existan como opción en Microsip (reutiliza los observados) o el dropdown saldrá
   vacío en la UI.
9. **El `DOCTOS_CC` (cargo) hereda el FOLIO del `DOCTOS_PV`** (ej. ambos
   `Y00002267`). El doc de enganche, en cambio, lleva su propio folio de
   `FOLIOS_CONCEPTOS`.

### Patrón de creación atómica (probado)
Todo el alta (fases 1–6, e incluso 7) se puede hacer en **un solo `EXECUTE BLOCK`**
de Firebird: tomar los IDs con `GEN_ID(ID_DOCTOS,1)` (o pasar `-1` y dejar que el
trigger `BEFINS` los asigne), insertar/actualizar, y `COMMIT` al final (o `ROLLBACK`
para validar sin persistir). Así toda la venta —con su cascada de aplicación— es
atómica: si algo falla, no queda nada a medias.

## Verificación end-to-end (Nivel 2) — ✅ HECHO

Validado contra `DESARROLLO.FDB` (Firebird 3.0.11) el 2026-05-21 ejecutando la
receta completa vía un `EXECUTE BLOCK` (fases 1–6, IDs reales con `GEN_ID`):

1. ~~Lista exacta de columnas `NOT NULL` + triggers `BEFINS`~~ ✅ (ver "Columnas
   obligatorias" arriba).
2. ~~Ejecutar el flujo completo para contado y crédito y verificar la cascada~~ ✅:

   | Prueba | DOCTO | FOLIO | `APLICADO` | Inventario (`IN`) | CxC (`CC`) | `SALIDAS_UNIDADES` |
   |---|---|---|---|---|---|---|
   | Efectivo (forma 67) | 15239188 | Y00002265 | S | 1 doc | **2** (cargo+pago) | +1 ✅ |
   | Crédito (forma 71) | 15239197 | Y00002266 | S | 1 doc | **1** (cargo) | +1 ✅ |

   La transición `APLICADO N→S` disparó la cascada y generó automáticamente el
   documento de inventario (bajó stock) y los `DOCTOS_CC`, ligados vía
   `DOCTOS_ENTRE_SIS` (`CLAVE_SIS_DEST` = `IN` / `CC`). `FOLIOS_CAJAS` se
   incrementó por venta.

3. La prueba de humo previa se hizo con `ROLLBACK` (sin persistir); la final con
   `COMMIT` (ventas reales en DESARROLLO).

**Conclusión: la receta es correcta y suficiente para crear ventas.** Único
requisito de entorno a confirmar en cada empresa destino: preferencias
`INTEG_IN_PV` / `INTEG_CC_PV` en `'S'` y que el `CLIENTE_ID` no sea el eventual.
