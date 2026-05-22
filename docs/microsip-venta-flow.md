# Microsip — flujo de creación de venta (Punto de Venta)

> Cómo Microsip crea una venta a crédito, capturado contra `DESARROLLO.FDB`
> (Firebird 3.0.11) vía trace + lectura de schema/triggers. Base para diseñar
> el sync futuro msp-api → Microsip. **Nada en este doc se infirió; todo se
> verificó contra una venta real (DOCTO_PV_ID 15239068).**

## Hallazgo central: es PUNTO DE VENTA, no facturación

La venta se crea con `PVenta.exe` → tabla **`DOCTOS_PV`**. El módulo de
facturación (`DOCTOS_VE`) es **otra cosa** y no se toca en este flujo. El sync
debe escribir a la familia `DOCTOS_PV`.

| | Punto de Venta | Facturación |
|---|---|---|
| Tabla header | `DOCTOS_PV` | `DOCTOS_VE` |
| Ejecutable | `PVenta.exe` | otro módulo |
| Caja | sí (`CAJA_ID`) | no |

## El save completo (capturado por trace de ejecución — DOCTO 15239152 / FOLIO Y00002262)

> Capturado el 2026-05-21 con un trace **server-side** (`fbtracemgr` lanzado como
> Tarea Programada SYSTEM, escribiendo a `C:\capture.log` vía stdout). Venta a
> crédito de 1 producto (`BOC.KAISERRECMSA2029`, 1 u., total 4 320.00 con IVA
> incluido). **Esto es lo que el cliente `PVenta.exe` hace realmente al guardar**
> — el dato que los traces anteriores nunca lograron capturar. 32 510 líneas,
> 1 532 `EXECUTE_PROCEDURE_FINISH`, todo en la transacción de escritura
> `TRA_7312682`.

### Hallazgo #1 — el save es INSERT con `APLICADO='N'`, luego UPDATE a `'S'`

El documento **no** se inserta ya aplicado. La secuencia real:

1. `INSERT INTO DOCTOS_PV` con **`APLICADO='N'`**, **`IMPORTE_NETO=0`**,
   **`FOLIO` temporal `_01788986`** (empieza con `_`).
2. Se insertan renglones, cobro, libres y se recalculan totales con varios
   `UPDATE DOCTOS_PV` intermedios.
3. **`UPDATE DOCTOS_PV SET … APLICADO='S', FOLIO='Y00002262', …`** ← este UPDATE
   es el que **dispara toda la cascada de aplicación**. Sus triggers AFTER UPDATE
   (`DOCTOS_PV_AFTUPD_0/_1`) llaman a `APLICA_DOCTO_PV`, que genera inventario y
   cuenta por cobrar (ver hallazgo #2).

**Implicación crítica para el sync:** insertar directamente con `APLICADO='S'`
**no** aplica el documento (no hay trigger AFTER INSERT que aplique; la aplicación
está colgada del AFTER UPDATE de `APLICADO N→S`). El sync **debe** replicar el
patrón: insertar con `APLICADO='N'` y luego hacer el `UPDATE … APLICADO='S'`.
El `FOLIO` definitivo también se asigna en ese UPDATE final (el `_…` temporal es
un placeholder mientras el documento está sin aplicar).

### Hallazgo #2 — al aplicar, Microsip genera inventario Y cuenta por cobrar

El `UPDATE APLICADO='S'` dispara, **dentro de la misma transacción y vía
procedures/triggers** (no son INSERTs del cliente — el cliente nunca los emite):

- **Inventario** — `GENERA_DOCTO_IN_PV` → crea `DOCTOS_IN` + `DOCTOS_IN_DET`,
  `AFECTA_CAPA_PROMEDIO`, `GET_CAPAS_COSTEO`, `CALC_COSTO_UNIT`,
  `COSTEA_SALIDA_IN`, `COSTEA_MOVTO_SALIDA`, `AFECTA_SALDOS_IN`,
  `APLICA_SALIDA_IN`, `APLICA_DOCTO_IN`. (triggers `DOCTOS_IN_*`, `CAPAS_COSTOS_*`)
- **Cuenta por cobrar (crédito)** — `GENERA_DOCTO_CC_PV` → crea `DOCTOS_CC` +
  `IMPORTES_DOCTOS_CC`, `AFECTA_SALDOS_CC`, `REGISTRA_IMPORTE_CC`. (triggers
  `DOCTOS_CC_BEFINS*`, `IMPTES_DOCTOS_CC_*`)
- **Maestras de aplicación** — `APLICA_VTA_PV` y `APLICA_DOCTO_PV` (orquestan todo
  lo anterior).

> Aparecen además triggers **`MSP_DOCTOS_CC_LISTEN`** y
> **`MSP_IMPORTES_DOCTOS_CC_LISTEN`** (AFTER INSERT/UPDATE sobre `DOCTOS_CC` /
> `IMPORTES_DOCTOS_CC`): son **triggers custom nuestros** ya instalados en
> Microsip (prefijo `MSP_`), no de Microsip de fábrica.

### Hallazgo #3 — contado vs crédito: la diferencia es un 2º `DOCTOS_CC` (el pago)

Se capturó también una venta de **contado/efectivo** (DOCTO 15239168,
`FORMA_COBRO_ID=67`, importe 5 700) para comparar contra la de crédito (DOCTO
15239152, `FORMA_COBRO_ID=71`). **Ambas corren exactamente la misma cascada de
aplicación** (`APLICA_DOCTO_PV`, `GENERA_DOCTO_IN_PV`, `GENERA_DOCTO_CC_PV`,
inventario idéntico). La única diferencia está en la cuenta por cobrar:

| | Crédito (71) | Efectivo (67) |
|---|---|---|
| `DOCTOS_CC` generados | **1** (el cargo) | **2** (cargo + pago) |
| `IMPORTES_DOCTOS_CC` | 1 | 2 |
| `AFECTA_SALDOS_CC` | ×1 | ×2 |
| `REGISTRA_IMPORTE_CC` | ×1 | ×2 |
| `DOCTOS_IN` / `AFECTA_SALDOS_IN` | ×1 | ×1 (idéntico) |

- **Crédito:** se genera solo el **cargo** en `DOCTOS_CC` → queda saldo abierto.
- **Contado:** se genera el cargo **y un segundo `DOCTOS_CC` (el pago/abono)** que
  liquida el saldo en el mismo guardado → saldo neto cero.

> Esto **refina** el "footprint idéntico" del análisis previo: a nivel de las
> tablas `DOCTOS_PV*` el footprint es igual, pero en `DOCTOS_CC` /
> `IMPORTES_DOCTOS_CC` el contado escribe el doble (cargo + pago). El tipo de
> venta sigue derivándose del `FORMA_COBRO_ID` insertado en `DOCTOS_PV_COBROS`
> (67 efectivo TIPO=`E`, 71 crédito TIPO=`R`), y eso es lo que hace que la
> cascada genere uno o dos documentos CC.

**Implicación para el sync:** si replicamos una venta de contado, basta insertar
la forma de cobro `67` y dejar que la cascada genere el pago; no hay que crear el
abono a mano. El inventario se afecta igual en ambos casos.

### Secuencia cronológica de escrituras del cliente (TRA_7312682)

Lo que `PVenta.exe` emite explícitamente (INSERT/UPDATE; los SELECT de carga se
omiten):

| # | Statement | Notas |
|---|---|---|
| 1 | `INSERT INTO DOCTOS_PV` | `APLICADO='N'`, `IMPORTE_NETO=0`, FOLIO `_01788986` |
| 2 | `INSERT INTO DOCTOS_PV_DET` | precedido por `GEN_POS_RENG_PV` (asigna `POSICION`) |
| 3 | `UPDATE DOCTOS_PV` ×N | recalcula totales (`CALC_TOTALES_DOCTO_PV`, `TOT_DOCTO_PV_CALC_PESO`) |
| 4 | `INSERT INTO DOCTOS_PV_COBROS` | `TIPO='C'`, `FORMA_COBRO_ID=71` (Crédito), importe 4320 |
| 5 | `UPDATE DOCTOS_PV_DET` | impuestos por unidad; trigger `…DET_BEFUPD_0` |
| 6 | (trigger) `INSERT IMPUESTOS_DOCTOS_PV_DET` | dentro del cálculo de impuestos (`LISTA_IMPTOS_ART`, `TOT_DOCTO_PV_CALC_IMPTOS_ART`) |
| 7 | `INSERT INTO LIBRES_VTA_PV` | **solo** `(DOCTO_PV_ID)` — el resto queda NULL/default |
| 8 | **`UPDATE DOCTOS_PV … APLICADO='S', FOLIO='Y00002262'`** | **dispara la cascada de aplicación** (hallazgo #2) |
| 9 | `UPDATE DOCTOS_PV` (final) | cierre |

### Procedures que el cliente llama directamente (vía `EXECUTE PROCEDURE` / `SELECT FROM`)

Útiles para el sync solo si queremos replicar cálculos; la aplicación **no**
necesita llamarse a mano (la dispara el UPDATE a `'S'`):

- Totales/impuestos: `CALC_TOTALES_DOCTO_PV`, `TOT_DOCTO_PV_CALC_PESO`,
  `TOT_DOCTO_PV_CALC_IMPTOS_ART`, `LISTA_IMPTOS_ART`, `EQUIVIMP_GET_TABLA_ID`,
  `EQUIVIMP_GET_PRIORIDAD_REEMP`, `EQUIVIMP_GET_PARAMS_DOCTO_PV`.
- Renglón/posición: `GEN_POS_RENG_PV`, `GET_FACTOR_VENTA_ART`,
  `VALIDA_FACTOR_VENTA`.
- Descuentos: `GEN_DOCTO_DET_POLS_DSCTOS`, `GET_POLITICAS_DSCTO_APLICADAS`,
  `GET_DSCTO_MAX_ART`, `GET_POLS_DSCTO_*`.
- Series/lotes/discretos: `SERIES_DUP_DOCTO_PV`, `EXIS_LOTES_DOCTO_PV`,
  `PKG_DISP_SERIES_DOCTO_PV.DISP_SERIES_DOCTO_PV`, `CUADRA_DISCRETOS_PV`.

### Parámetros reales capturados (referencia para el mapeo de columnas)

`INSERT DOCTOS_PV_DET` (20 columnas):
`DOCTO_PV_DET_ID=15239153, DOCTO_PV_ID=15239152, CLAVE_ARTICULO='BOC.KAISERRECMSA2029',
ARTICULO_ID=2532100, UNIDADES=1, UNIDADES_DEV=0, PRECIO_UNITARIO=3724.137931,
PRECIO_UNITARIO_IMPTO=4320.000000, PCTJE_DSCTO=0, PRECIO_TOTAL_NETO=0 (se calcula),
PRECIO_MODIFICADO='N', VENDEDOR_ID=NULL, PCTJE_COMIS=0, ROL='N', NOTAS=NULL,
POSICION=1, TIPO_CONTAB_UNID='0', ES_TRAN_ELECT='N', ESTATUS_TRAN_ELECT=NULL,
IMPUESTO_POR_UNIDAD=0`

`INSERT DOCTOS_PV_COBROS` (7 columnas):
`DOCTO_PV_COBRO_ID=15239154, DOCTO_PV_ID=15239152, TIPO='C', FORMA_COBRO_ID=71,
IMPORTE=4320.00, TIPO_CAMBIO=1.0, IMPORTE_MON_DOC=4320.00`

`INSERT LIBRES_VTA_PV`: `(DOCTO_PV_ID=15239152)` — única columna.

Nota: los IDs (`15239152` header, `…153` det, `…154` cobro) son consecutivos del
mismo generador `GEN_ID(ID_DOCTOS,1)` — confirma el patrón ya documentado.

## Footprint exacto (idéntico en contado y crédito — 2 productos)

```
DOCTOS_PV                       +1   header
├─ DOCTOS_PV_DET                +2   un row por producto (con POSICION)
├─ DOCTOS_PV_COBROS             +1   forma de cobro (crédito)
│   └─ MOVTOS_EFVO_CAJA         +1   ← trigger COBROS_AFTINS_0, NO la app
├─ IMPUESTOS_DOCTOS_PV          +1   resumen de impuesto a nivel doc
├─ IMPUESTOS_DOCTOS_PV_DET      +2   impuesto por renglón
├─ LIBRES_VTA_PV                +1   campos libres del documento
└─ MGGENCOBRO_VEMOST_PROCESADAS +1   ← trigger MGGENCOBRO_DOCTOS_PV_AI0 (cola cobro)
```

Total: **8 filas en 7 tablas**. Dos de ellas (`MOVTOS_EFVO_CAJA`,
`MGGENCOBRO_VEMOST_PROCESADAS`) las escriben triggers — el cliente solo inserta
en las otras 5.

## IDs y generadores

- Todos los PK salen de **un solo generador**: `GEN_ID(ID_DOCTOS, 1)`.
- Header, det, cobro e impuestos consumen IDs consecutivos del mismo generador
  (15239068 → 69 → 71 → 72…).
- Convención de inserción: pasar **`-1`** en el PK y el trigger `BEFINS` lo
  reemplaza con el siguiente del generador.

## Header `DOCTOS_PV` — columnas que importan

| Columna | Valor en la venta | Nota |
|---|---|---|
| `DOCTO_PV_ID` | 15239068 | PK, pasar -1 → autogen |
| `TIPO_DOCTO` | `V` | V = venta |
| `ESTATUS` | `N` | N = normal (no cancelada) |
| `APLICADO` | `S` | afecta existencias/saldos |
| `SISTEMA_ORIGEN` | `PV` | NOT NULL |
| `CAJA_ID` | 22198 | NOT NULL |
| `SUCURSAL_ID` | — | NOT NULL, validado contra `SUCURSALES` |
| `CLIENTE_ID` | 1743795 | |
| `ALMACEN_ID` | 11058 | |
| `MONEDA_ID` | — | si NULL en VE se defaultea del cliente |
| `IMPORTE_NETO` | 15000.00 | |
| `TOTAL_IMPUESTOS` | 0.00 | |
| `FOLIO` | Y00002253 | CHAR(9), folio temporal `_00…` se sincroniza distinto |
| `USUARIO_CREADOR` | RUTA25 | |
| `FECHA_HORA_CREACION` | 2026-05-21 13:55:55 | |

No existe `COND_PAGO_ID` en `DOCTOS_PV` (sí en `DOCTOS_VE`). El crédito se
expresa vía la forma de cobro, no un campo de condiciones.

## Renglones `DOCTOS_PV_DET`

Columnas clave: `ARTICULO_ID`, `CLAVE_ARTICULO`, `UNIDADES`,
`PRECIO_UNITARIO`, `PRECIO_TOTAL_NETO`, `PCTJE_DSCTO`, **`POSICION`**.

`DOCTOS_PV_DET_BEFINS` auto-asigna `POSICION` = `MAX(POSICION)+1` del documento
cuando se pasa `-1`. **Mismo patrón que nuestra migración 000004** (orden
estable de hijos vía columna POSICION).

## Contado vs crédito — solo cambia la forma de cobro

Se capturaron dos ventas: crédito (DOCTO 15239068) y contado (DOCTO 15239079).
**El footprint es estructuralmente idéntico** — mismas 8 filas en las mismas 7
tablas, ambas con `DOCTOS_PV_COBROS.TIPO='C'`, ambas generan `MOVTOS_EFVO_CAJA`.
La única diferencia es el `FORMA_COBRO_ID`:

| `FORMAS_COBRO` | Contado (67) | Crédito (71) |
|---|---|---|
| `NOMBRE` | Efectivo | Crédito |
| `TIPO` | `E` (efectivo) | `R` |
| `CLAVE_FISCAL` | `01` (SAT efectivo) | NULL |
| `CONTAR_EN_CORTE` | `S` | `N` |
| `DAR_CAMBIO` | `S` | `N` |

Implicación para el sync: **el tipo de venta no es un campo del header** — se
deriva de qué `FORMA_COBRO_ID` se inserta en `DOCTOS_PV_COBROS`. El crédito
igual escribe en `MOVTOS_EFVO_CAJA`, pero como su forma tiene
`CONTAR_EN_CORTE='N'` no afecta el corte de caja real (la tabla es un ledger de
cobros, no estrictamente efectivo).

> ⚠️ **CORREGIDO por el trace del save (DOCTO 15239152, ver sección "El save
> completo" abajo).** La afirmación previa de que "el crédito no crea `DOCTOS_CC`
> al guardar" es **falsa**. El crédito **sí** genera un `DOCTOS_CC` (cargo a la
> cuenta del cliente) + `IMPORTES_DOCTOS_CC` **durante el guardado**, dentro de la
> cascada de aplicación que dispara el `UPDATE … APLICADO='S'`. Los procedures de
> cobranza (`COBRANZA_CLIENTE`, `VENCIMIENTOS_CLIENTE`, `LISTA_CARGOS_POR_COBRAR`)
> que se ven al cargar el formulario son solo el **chequeo de crédito previo**, no
> el origen del saldo.

Nota: la venta de contado capturada llevaba IVA 16% (neto 14 655.17 + 2 344.83);
la de crédito iba a 0%. Eso depende de los productos/cliente, **no** del tipo de
pago — las tablas `IMPUESTOS_DOCTOS_PV*` se llenan igual en ambos casos.

## Inventario

> ⚠️ **CORREGIDO por el trace del save.** La conclusión previa ("computa
> existencias en vivo, sin ledger") es **falsa**. El save **sí** genera un
> documento de inventario `DOCTOS_IN` + `DOCTOS_IN_DET` con su movimiento, capas
> de costo (`CAPAS_COSTOS`, `AFECTA_CAPA_PROMEDIO`, `COSTEA_SALIDA_IN`) y afecta
> saldos de inventario (`AFECTA_SALDOS_IN`). Todo ocurre dentro de la cascada que
> dispara el `UPDATE … APLICADO='S'` (procedures `GENERA_DOCTO_IN_PV` →
> `APLICA_DOCTO_IN`). Ver "El save completo" abajo.
>
> El motivo por el que el footprint inicial no lo detectó: `DOCTOS_IN` no tiene
> columna `DOCTO_PV_ID` (se enlaza por otra vía), así que el `COUNT(*) WHERE
> DOCTO_PV_ID=…` no lo veía.

## Cascade de triggers (determinístico)

**INSERT `DOCTOS_PV`:**
1. `DOCTOS_PV_BEFINS` — genera `DOCTO_PV_ID` si es -1.
2. `DOCTOS_PV_BEFINSUPD_RI` — valida FK manual: `SUCURSAL_ID`,
   `LUGAR_EXPEDICION_ID` existen (sin constraint formal → error custom).
3. `DOCTOS_PV_BEFINSUPD_1` — validaciones SAT de factura global; **se saltan
   para venta normal** (solo aplican si `ES_FAC_GLOBAL='S' AND TIPO_DOCTO='F'`).
4. `MGGENCOBRO_DOCTOS_PV_AI0` (AI) — si `TIPO='V' AND ESTATUS='N'` → inserta en
   `MGGENCOBRO_VEMOST_PROCESADAS` (cola).
5. `DOCTOS_PV_AFTALL_SNUBE` — solo en modo "PV Desconectado" (sync sucursales) y
   solo en UPDATE `APLICADO` N→S. En insert directo **no corre**.

**INSERT `DOCTOS_PV_DET`:** `DET_BEFINS` — genera ID + asigna `POSICION`.

**INSERT `DOCTOS_PV_COBROS`:**
- `COBROS_BEFINS` — genera ID.
- `COBROS_BEFINSUPD_0` — valida no duplicar `FORMA_COBRO_ID` (salvo tarjetas
  Banorte/Banregio).
- `COBROS_AFTINS_0` — inserta en `MOVTOS_EFVO_CAJA` (signo según TIPO; C/I =
  entrada positiva).

**Cancelación** (`ESTATUS` N→C, no es parte del alta): `DOCTOS_PV_AFTUPD_1`
borra los `MOVTOS_EFVO_CAJA` del documento.

## Cómo se capturó

- **Conexión**: Firebird remoto vía túnel TCP (`bore` desde el Windows server
  exponiendo `localhost:3050`). Cliente `isql`/`fbtracemgr` desde Mac.
- **Trace del save (el que funcionó)**: `fbtracemgr -start` lanzado como
  **Tarea Programada de Windows corriendo como SYSTEM** (`schtasks /Create … /RU
  SYSTEM`), con la salida redirigida a `C:\capture.log` vía un `.cmd` wrapper.
  Esto lo desprende del árbol de la sesión SSH — la causa de que los intentos
  previos murieran a los ~2 seg (`TRACE_FINI` prematuro) era que el proceso
  quedaba atado a la sesión que lo lanzó. Se detiene con `fbtracemgr -stop -id N`.
  - Config FB 3.x usa **`option = value`** (con `=`) y matcher **SIMILAR TO**
    (`%DESARROLLO.FDB`, no regex POSIX).
  - El config de sesión **no** lleva `log_filename` (la salida va por stdout).
- **Callejón sin salida — audit trace a archivo**: el `AuditTraceConfigFile` a
  nivel motor **no sirve en Firebird 3.0.11 en Windows**: `log_filename` con
  cualquier ruta absoluta lanza `"pattern is invalid"`. El valor se procesa con
  sintaxis sed y `fixupSeparators` colapsa los separadores a un único `\` **antes**
  de compilar el patrón, dejando `\a` (de `\audit…`) como escape inválido. No hay
  forma de escaparlo (probamos `\\`, `\\\\`, `/`, `//`, comillas). Es el bug del
  PR FirebirdSQL/firebird#8238, cuyo fix no está en 3.0.11. Por eso se usó
  `fbtracemgr` (stdout), que esquiva `log_filename`.
- **Footprint**: por cada tabla con columna `DOCTO_PV_ID`, `COUNT(*)` filtrado
  al docto + lectura de filas reales.
- **Cascade**: lectura de `RDB$TRIGGER_SOURCE` de los triggers de la familia.

## Pendientes / próximos pasos

1. ~~Capturar el save completo con trace local en Windows~~ ✅ **HECHO**
   (2026-05-21, DOCTO 15239152 — ver "El save completo"). Reveló: patrón
   insert-`N`-luego-update-`S`, generación de `DOCTOS_IN` y `DOCTOS_CC` en la
   aplicación. Corrigió las secciones Inventario y DOCTOS_CC.
2. Leer el cuerpo de `APLICA_DOCTO_PV` / `GENERA_DOCTO_CC_PV` / `GENERA_DOCTO_IN_PV`
   (`RDB$PROCEDURE_SOURCE`) para saber **qué columnas del header/det leen** al
   generar inventario y CxC → qué debe llenar el sync antes del `UPDATE 'S'`.
3. Leer triggers `BEFINS`/`BEFINSUPD_1` de DET para saber qué columnas son
   obligatorias vs auto-defaulteadas (qué puede omitir el sync).
4. Mapear una **devolución** y una **cancelación** (cambian `ESTATUS`/`TIPO`).
