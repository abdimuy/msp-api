# Spike — Aplicar venta local en Microsip

> Investigación de solo-lectura contra la BD Firebird dev (`MUEBLERA.FDB`).
> Fecha: 2026-05-28. Base: Docker `mueblera-firebird` (Firebird 3.0.11).
> Objetivo: confirmar las incógnitas abiertas del diseño y proveer IDs reales para
> el seed de config.

---

## 1. Procedimientos y triggers del flip `APLICADO N→S`

### Triggers AFTER UPDATE en `DOCTOS_PV`

| Trigger | Tipo | Secuencia | Qué hace |
|---|---|---|---|
| `DOCTOS_PV_AFTUPD_0` | AFTER UPDATE | 0 | Detecta `OLD.APLICADO <> NEW.APLICADO` → llama `APLICA_DOCTO_PV NEW.DOCTO_PV_ID` |
| `DOCTOS_PV_AFTUPD_1` | AFTER UPDATE | 1 | Detecta `OLD.ESTATUS <> NEW.ESTATUS AND NEW.ESTATUS='C'` → borra `MOVTOS_EFVO_CAJA` del documento (solo cancelación) |

El trigger `DOCTOS_PV_BEFUPD_0` (BEFORE UPDATE) valida la transición y hace `NEW.APLICADO = GET_SN(...)` para confirmar que el valor fue sanitizado; la aplicación real ocurre en el AFTER.

### Cadena de llamadas para `TIPO_DOCTO='V'`

```
DOCTOS_PV_AFTUPD_0
  └─ APLICA_DOCTO_PV(DOCTO_PV_ID)
       ├─ GET_ELEM_REGISTRY(0, 'PreferenciasEmpresa') → ID_PREFER_EMP
       ├─ GET_ELEM_REGISTRY(ID_PREFER_EMP, 'INTEG_IN_PV')   → INTEG_IN
       ├─ GET_ELEM_REGISTRY(ID_PREFER_EMP, 'METODO_COSTEO') → (si INTEG_IN='S')
       ├─ GET_ELEM_REGISTRY(ID_PREFER_EMP, 'INTEG_CC_PV')   → INTEG_CC
       ├─ GET_ELEM_REGISTRY(ID_PREFER_EMP, 'CLIENTE_EVENTUAL_PV_ID') → (si INTEG_CC='S')
       └─ APLICA_VTA_PV(DOCTO_PV_ID, INTEG_IN, METODO_COSTEO, INTEG_CC, CLIENTE_EVENTUAL_ID)
            ├─ [si INTEG_IN='S'] GENERA_DOCTO_IN_PV(DOCTO_PV_ID, METODO_COSTEO)
            └─ [si INTEG_CC='S' y CLIENTE_ID ≠ CLIENTE_EVENTUAL_ID]
               GENERA_DOCTO_CC_PV(DOCTO_PV_ID)
```

### Columnas de `DOCTOS_PV` leídas por los procedures

| Columna | Leída por | Propósito |
|---|---|---|
| `TIPO_DOCTO` | `APLICA_DOCTO_PV`, `GENERA_DOCTO_IN_PV` | decide el branch del procedure |
| `CLIENTE_ID` | `APLICA_VTA_PV`, `GENERA_DOCTO_CC_PV` | comparar con `CLIENTE_EVENTUAL_ID`; copiar al `DOCTOS_CC` |
| `CAJA_ID` | `GENERA_DOCTO_CC_PV` | obtener `NOMBRE_CAJA` para `DESCRIPCION` |
| `SUCURSAL_ID` | `GENERA_DOCTO_IN_PV`, `GENERA_DOCTO_CC_PV` | copiado al `DOCTOS_IN` y `DOCTOS_CC` |
| `FOLIO` | `GENERA_DOCTO_IN_PV`, `GENERA_DOCTO_CC_PV` | unicidad y copia al IN/CC |
| `FECHA` | `GENERA_DOCTO_IN_PV`, `GENERA_DOCTO_CC_PV` | copiada al IN/CC |
| `ALMACEN_ID` | `GENERA_DOCTO_IN_PV` | destino del documento de inventario |
| `CLAVE_CLIENTE` | `GENERA_DOCTO_CC_PV` | copiada al `DOCTOS_CC` |
| `TIPO_CAMBIO` | `GENERA_DOCTO_CC_PV` | copiado al `DOCTOS_CC` |
| `IMPORTE_NETO + IMPORTE_DONATIVO` | `GENERA_DOCTO_CC_PV` | base del cargo CxC |
| `TOTAL_IMPUESTOS` | `GENERA_DOCTO_CC_PV` | campo `IMPUESTO` del importe CC |
| `TOTAL_RETENCIONES` | `GENERA_DOCTO_CC_PV` | desglose retenciones IVA/ISR |

### Columnas de `DOCTOS_PV_DET` leídas

| Columna | Leída por | Propósito |
|---|---|---|
| `ARTICULO_ID` | `GENERA_DOCTO_IN_PV` | verificar `ARTICULOS.ES_ALMACENABLE='S'` |
| `ROL` | `GENERA_DOCTO_IN_PV` | ignorar renglones `ROL='C'`; tratar `ROL='J'` como juego |
| `POSICION` | `GENERA_DOCTO_IN_PV` | `ORDER BY POSICION` |
| `DOCTO_PV_DET_ID` | `GENERA_DOCTO_IN_PV` | pasado a `GENERA_DOCTO_IN_PV_MOVTO` |

**Conclusión:** el adapter debe poblar `CAJA_ID`, `SUCURSAL_ID`, `FOLIO`, `FECHA`,
`ALMACEN_ID`, `CLAVE_CLIENTE`, `CLIENTE_ID`, `TIPO_CAMBIO`, `IMPORTE_NETO`,
`TOTAL_IMPUESTOS`, y en detalle `ARTICULO_ID`, `ROL`, `POSICION`.
Todo lo que ya dicta la receta. No hay columna sorpresa.

---

## 2. Preferencias de empresa

Las preferencias las lee `APLICA_DOCTO_PV` vía `GET_ELEM_REGISTRY` del nodo
`PreferenciasEmpresa` (ELEMENTO_ID 236, tabla `REGISTRY`).

| Clave (REGISTRY.NOMBRE) | ELEMENTO_ID | Valor actual | Efecto |
|---|---|---|---|
| `INTEG_IN_PV` | 10966 | **`S`** | genera inventario en la aplicación |
| `INTEG_CC_PV` | 10967 | **`S`** | genera CxC en la aplicación |
| `CLIENTE_EVENTUAL_PV_ID` | 10970 | **`0`** | ningún cliente tiene ID 0 → todos generan CxC |

Ambas integraciones están habilitadas. El valor `0` de `CLIENTE_EVENTUAL_PV_ID`
significa que no hay cliente genérico/eventual configurado: toda venta genera
`DOCTOS_CC`, sin excepción.

---

## 3. Triggers MSP_*_LISTEN

Existen los siguientes triggers custom (prefijo `MSP`):

| Trigger | Tabla | Tipo |
|---|---|---|
| `MSP_CLIENTES_LISTEN` | `CLIENTES` | ALL (insert+update) |
| `MSP_DOCTOS_CC_LISTEN` | `DOCTOS_CC` | ALL |
| `MSP_FORMAS_COBRO_DOCTOS_LISTEN` | `FORMAS_COBRO_DOCTOS` | ALL |
| `MSP_IMPORTES_DOCTOS_CC_LISTEN` | `IMPORTES_DOCTOS_CC` | ALL |
| `MSP_PAGOS_RECIBIDOS_LISTEN` | `MSP_PAGOS_RECIBIDOS` | ALL |

Estos cinco triggers ya están instalados. Al aplicar la venta y que la cascada
cree `DOCTOS_CC` e `IMPORTES_DOCTOS_CC`, los triggers `MSP_DOCTOS_CC_LISTEN` y
`MSP_IMPORTES_DOCTOS_CC_LISTEN` dispararán automáticamente. El adapter no necesita
llamarlos; es un efecto secundario deseable (notificación hacia msp-api).

No existe un trigger `MSP_DOCTOS_PV_LISTEN`; si se necesita escuchar la creación de
ventas, habría que añadirlo.

---

## 4. Charset de columnas de texto

### `DOCTOS_PV`

| Columna | Tipo FB | Charset declarado |
|---|---|---|
| `TIPO_DOCTO`, `FOLIO`, `APLICADO`, `ESTATUS`, `SISTEMA_ORIGEN`, flags (`CHAR(1)`) | CHAR | **NONE** |
| `CLAVE_CLIENTE` | VARCHAR | **ISO8859_1** |
| `CLAVE_CLIENTE_FAC` | VARCHAR | **ISO8859_1** |
| `PERSONA`, `DESCRIPCION`, `REFER_RETING`, `EMAIL_ENVIO`, `USUARIO_*`, `FECHA_INICIO`, `FECHA_FIN` | VARCHAR | **NONE** |

### `DOCTOS_CC`

| Columna | Tipo FB | Charset declarado |
|---|---|---|
| `CLAVE_CLIENTE` | VARCHAR | **ISO8859_1** |
| Resto de VARCHAR (`DESCRIPCION`, `CUENTA_CONCEPTO`, `EMAIL_ENVIO`, `USUARIO_*`, `LAT`, `LON`) | VARCHAR | **NONE** |
| CHAR flags (`FOLIO`, `APLICADO`, `ESTATUS_ANT`, etc.) | CHAR | **NONE** |

### `LIBRES_CARGOS_CC`

| Columna | Tipo | Charset |
|---|---|---|
| `AVAL_O_RESPONSABLE` | VARCHAR | **NONE** |
| `OBSERVACIONES` | VARCHAR | **NONE** |

### Veredito de charset

`CHARSET=NONE` en Firebird significa que la columna hereda el charset de la
**conexión**. La BD se creó con `DEFAULT CHARACTER SET NONE`; la conexión del
adapter usa `-ch UTF8` (o en Go, `charset=UTF8` en el DSN). Por tanto:

- Las columnas `NONE` aceptarán bytes UTF-8 tal como lleguen desde Go.
- La columna `CLAVE_CLIENTE` (`ISO8859_1`) en `DOCTOS_PV` y `DOCTOS_CC` sí
  necesita encoding ISO-8859-1. Afortunadamente `CLAVE_CLIENTE` solo contiene
  el código alfanumérico del cliente (p. ej. `0004546`), que es ASCII puro:
  no hay caracteres especiales en la práctica.

**Conclusión práctica:** el adapter puede escribir UTF-8 en todas las columnas de
texto. La afirmación de la receta de que "las tablas legacy son ISO8859_1" es
**parcialmente correcta**: solo dos columnas `CLAVE_CLIENTE` en las tablas de
cabecera son `ISO8859_1`; el resto es `NONE` (acepta lo que manda la conexión).
No se necesita `firebird.Win1252` ni `EncodeWin1252` para escribir en estas tablas,
salvo si algún día se escribe un `CLAVE_CLIENTE` con tildes (no aplica en la
práctica).

---

## 5. IDs de catálogo reales

### Formas de cobro (`FORMAS_COBRO`)

| FORMA_COBRO_ID | NOMBRE | TIPO |
|---|---|---|
| **67** | Efectivo | `E` (contado) |
| 68 | Cheque | `C` |
| **71** | Crédito | `R` (crédito) |
| 27773 | TRASPASO CTA ANTERIOR | `O` |

Confirmado: 67 = Efectivo (`TIPO='E'`), 71 = Crédito (`TIPO='R'`).

### Sucursales (`SUCURSALES`)

| SUCURSAL_ID | NOMBRE |
|---|---|
| **225490** | Matriz |
| 3036477 | Mercancía en tránsito |

Una sola sucursal operativa. Todas las ventas existentes usan `SUCURSAL_ID=225490`.
Es el único valor a usar en la config.

### Cajas (`CAJAS`)

La tabla `CAJAS` **no tiene columna `SUCURSAL_ID`**. Todas las ventas recientes
observadas en `DOCTOS_PV` usan `SUCURSAL_ID=225490` independientemente de la caja.

Cajas RUTA (con `ALMACEN_ID=11058`) — las relevantes para ventas en campo:

| CAJA_ID | NOMBRE | Serie (TIPO_DOCTO='V') | Consecutivo actual |
|---|---|---|---|
| 21770 | RUTA01 | AA | 2611 |
| 21787 | RUTA02 | BB | 2204 |
| 21804 | RUTA03 | CC | 2123 |
| 21821 | RUTA04 | DD | 2560 |
| 21838 | RUTA05 | EE | 2055 |
| 21855 | RUTA06 | F | 2194 |
| 21872 | RUTA07 | G | 2383 |
| 21908 | RUTA08 | H | 2660 |
| 21925 | RUTA09 | I | 2224 |
| 21942 | RUTA10 | J | 2225 |
| 21959 | RUTA11 | K | 3046 |
| 21976 | RUTA12 | L | 2492 |
| 21993 | RUTA13 | M | 2203 |
| 22010 | RUTA14 | N | 2538 |
| 22027 | RUTA15 | O | 1849 |
| 22044 | RUTA16 | P | 3614 |
| 22061 | RUTA17 | Q | 2561 |
| 22078 | RUTA18 | R | 3124 |
| 22095 | RUTA19 | S | 2347 |
| 22112 | RUTA20 | T | 2129 |
| 22129 | RUTA21 | U | 2437 |
| 2998592 | RUTA22 | TEX | 227 |
| 22164 | RUTA23 | W | 2165 |
| 22181 | RUTA24 | X | 2328 |
| 22198 | RUTA25 | Y | 2208 |
| 22215 | RUTA26 | Z | 2448 |
| 22232 | RUTA27 | AB | 2067 |
| 3034589 | RUTA28 | ATZ | 72 |
| 22266 | RUTA29 | AD | 2595 |
| 22283 | RUTA30 | AE | 2118 |
| 22300 | RUTA31 | AF | 2233 |
| 22317 | RUTA32 | AG | 2251 |
| 22334 | RUTA33 | AH | 2380 |
| 22351 | RUTA34 | AI | 2300 |
| 50188 | RUTA35 | AN | 2430 |
| 78170 | RUTA36 | AO | 2139 |
| 79620 | RUTA37 | AP | 1857 |
| 320864 | RUTA38 | AJ | 2049 |
| 841539 | RUTA39 | AK | 1519 |
| 22147 | RUTA_MSP_MATRIZ | MSP | 2412 |
| 179199 | RUTA_MSP_TEHUACAN | TEH | 2296 |
| 22249 | RUTA_MSP_ZOQUITLAN | ZOQ | 730 |
| 32119 | TRABAJADORES MSP | AM | 394 |

### Cajeros (`CAJEROS`)

Patrón: cada CAJA RUTA_N tiene su CAJERO del mismo nombre. Referencia completa:

| CAJERO_ID | NOMBRE/USUARIO |
|---|---|
| 22368 | RUTA01 |
| 22369 | RUTA02 |
| 22370 | RUTA03 |
| 22371 | RUTA04 |
| 22372 | RUTA05 |
| 22373 | RUTA06 |
| 22374 | RUTA07 |
| 22375 | RUTA08 |
| 22376 | RUTA09 |
| 22377 | RUTA10 |
| 22378 | RUTA11 |
| 22379 | RUTA12 |
| 22380 | RUTA13 |
| 22381 | RUTA14 |
| 22382 | RUTA15 |
| 22383 | RUTA16 |
| 22384 | RUTA17 |
| 22385 | RUTA18 |
| 22386 | RUTA19 |
| 22387 | RUTA20 |
| 22388 | RUTA21 |
| 22389 | RUTA22 |
| 22390 | RUTA23 |
| 22391 | RUTA24 |
| 22392 | RUTA25 |
| 22393 | RUTA26 |
| 22394 | RUTA27 |
| 2998590 | RUTAMSP |
| 22395 | RUTAZOQ |
| 22396 | RUTA29 |
| 22397 | RUTA30 |
| 22398 | RUTA31 |
| 22399 | RUTA32 |
| 22400 | RUTA33 |
| 22401 | RUTA34 |
| 50187 | RUTA35 |
| 78189 | RUTA36 |
| 79641 | RUTA37 |
| 320881 | RUTA38 |
| 841633 | RUTA39 |
| 1162794 | TRABAJADORES_MSP |
| 221644 | RUTATEH |
| 3034611 | RUTA28 |

### Listas custom de `LIBRES_CARGOS_CC` (tabla `LISTAS_ATRIBUTOS`)

Las columnas `FORMA_DE_PAGO`, `CREDITO_EN_MESES` y `NUMERO_DE_VENDEDORES` son
campos de tipo lista (`TIPO='L'`) definidos en `ATRIBUTOS` y sus opciones en
`LISTAS_ATRIBUTOS`. El PK de `LISTAS_ATRIBUTOS` es `LISTA_ATRIB_ID` — ese es
el entero que se guarda en `LIBRES_CARGOS_CC`.

#### `FORMA_DE_PAGO` (ATRIBUTO_ID 19979, NOMBRE_COLUMNA `FORMA_DE_PAGO`)

| LISTA_ATRIB_ID | VALOR_DESPLEGADO | POSICION |
|---|---|---|
| **33824** | **Semanal** | 1 |
| **33825** | **Quincenal** | 2 |
| **33826** | **Mensual** | 3 |
| **840308** | **Contado** | 4 |

#### `CREDITO_EN_MESES` (ATRIBUTO_ID 19982, NOMBRE_COLUMNA `CREDITO_EN_MESES`)

| LISTA_ATRIB_ID | VALOR_DESPLEGADO (plazo) | POSICION |
|---|---|---|
| **33827** | **18** meses | 1 |
| **33828** | **12** meses | 2 |
| **33829** | **9** meses | 3 |
| **33830** | **6** meses | 4 |
| **840309** | **0** (contado) | 5 |

#### `NUMERO_DE_VENDEDORES` (ATRIBUTO_ID 21608, NOMBRE_COLUMNA `NUMERO_DE_VENDEDORES`)

| LISTA_ATRIB_ID | VALOR_DESPLEGADO | POSICION |
|---|---|---|
| **47558** | **1** vendedor | 1 |
| **47559** | **2** vendedores | 2 |
| **47560** | **3** vendedores | 3 |

**Nota sobre el valor -1:** aparece en `LIBRES_CARGOS_CC` para registros donde el
campo no fue seleccionado (valor centinela "ninguno"). No es un `LISTA_ATRIB_ID`
válido en `LISTAS_ATRIBUTOS`.

### Zonas de cliente (`ZONAS_CLIENTES`)

Las zonas coinciden 1:1 con cajas RUTA por nombre. Selección relevante:

| ZONA_CLIENTE_ID | NOMBRE | CAJA correspondiente | CAJA_ID |
|---|---|---|---|
| 12271 | R/01 | RUTA01 | 21770 |
| 12272 | R/02 | RUTA02 | 21787 |
| 12273 | R/03 | RUTA03 | 21804 |
| 12274 | R/04 | RUTA04 | 21821 |
| 21500 | R/05 | RUTA05 | 21838 |
| 21501 | R/06 | RUTA06 | 21855 |
| 21502 | R/07 | RUTA07 | 21872 |
| 21503 | R/08 | RUTA08 | 21908 |
| 21504 | R/09 | RUTA09 | 21925 |
| 21505 | R/10 | RUTA10 | 21942 |
| 21549 | R/11 | RUTA11 | 21959 |
| 21550 | R/12 | RUTA12 | 21976 |
| 21551 | R/13 | RUTA13 | 21993 |
| 21552 | R/14 | RUTA14 | 22010 |
| 21553 | R/15 | RUTA15 | 22027 |
| 21554 | R/16 | RUTA16 | 22044 |
| 21555 | R/17 | RUTA17 | 22061 |
| 21556 | R/18 | RUTA18 | 22078 |
| 21557 | R/19 | RUTA19 | 22095 |
| 21558 | R/20 | RUTA20 | 22112 |
| 21559 | R/21 | RUTA21 | 22129 |
| 21560 | R/MSP_MATRIZ | RUTA_MSP_MATRIZ | 22147 |
| 21561 | R/23 | RUTA23 | 22164 |
| 21562 | R/24 | RUTA24 | 22181 |
| 21563 | R/25 | RUTA25 | 22198 |
| 21564 | R/26 | RUTA26 | 22215 |
| 21565 | R/27 | RUTA27 | 22232 |
| 21566 | R/MSP_ZOQUITLAN | RUTA_MSP_ZOQUITLAN | 22249 |
| 21567 | R/29 | RUTA29 | 22266 |
| 21568 | R/30 | RUTA30 | 22283 |
| 21569 | R/31 | RUTA31 | 22300 |
| 21570 | R/32 | RUTA32 | 22317 |
| 21571 | R/33 | RUTA33 | 22334 |
| 21572 | R/34 | RUTA34 | 22351 |
| 50137 | R/35 | RUTA35 | 50188 |
| 77487 | R/36 | RUTA36 | 78170 |
| 79618 | R/37 | RUTA37 | 79620 |
| 320085 | R/38 | RUTA38 | 320864 |
| 843629 | R/39 | RUTA39 | 841539 |
| 179174 | R/MSP_TEHUACAN | RUTA_MSP_TEHUACAN | 179199 |
| 2990693 | R/22 | RUTA22 | 2998592 |
| 3034588 | R/28 | RUTA28 | 3034589 |
| 23301 | MAYOREO | CAJA MAYOREO | 64109 |
| 634157 | TRABAJADORES MSP | TRABAJADORES MSP | 32119 |

El CAJERO_ID sigue el mismo patrón de nombre. Para la migración seed, la tabla
`MSP_CFG_ZONA_CAJA` puede incluir directamente la tripla
`(ZONA_CLIENTE_ID, CAJA_ID, CAJERO_ID)` construida con estos datos.

### Folio de enganche (`FOLIOS_CONCEPTOS`)

| FOLIO_CONCEPTO_ID | SISTEMA | CONCEPTO_ID | SUCURSAL_ID | SERIE | CONSECUTIVO actual |
|---|---|---|---|---|---|
| 475145 | CC | 24533 | 225490 | `@` | 66275 |

El folio del enganche es `LPAD(CONSECUTIVO, 9, '0')`, p. ej. `000066275`.

---

## 6. Corte de caja abierto

La tabla `MOVTOS_EFVO_CAJA` tiene solo cuatro columnas:
`DOCTO_PV_COBRO_ID`, `CAJA_ID`, `FORMA_COBRO_ID`, `IMPORTE`.
**No existe columna `CORTE_CAJA_ID`** ni ninguna referencia a un corte.

El trigger `DOCTOS_PV_COBROS_AFTINS_0` que escribe en `MOVTOS_EFVO_CAJA` solo
obtiene el `CAJA_ID` del `DOCTOS_PV` y hace un `INSERT` directo sin verificar
ningún corte:

```sql
INSERT INTO MOVTOS_EFVO_CAJA (DOCTO_PV_COBRO_ID, CAJA_ID, FORMA_COBRO_ID, IMPORTE)
VALUES (NEW.DOCTO_PV_COBRO_ID, :CAJA_ID, NEW.FORMA_COBRO_ID, :IMPORTE);
```

No existen tablas `CORTES_CAJA` ni `CORTES_X_CAJA` en la BD. Las únicas tablas
con "corte" son de cajas chicas (`APERTURAS_CAJAS_CHICAS`) y rutas
(`CTI_CIERRE_RUTAS`), que son módulos distintos.

**Conclusión:** **no hay prerrequisito de corte de caja abierto** para escribir en
`MOVTOS_EFVO_CAJA`. La afirmación de la receta sobre "un corte de caja abierto"
es incorrecta para esta BD (ver sección de contradicciones).

---

## Contradicciones con la receta

| # | Afirmación en la receta | Hallazgo real | Impacto |
|---|---|---|---|
| 1 | "Un corte de caja abierto para esa `CAJA_ID`" es prerrequisito para `MOVTOS_EFVO_CAJA` | `MOVTOS_EFVO_CAJA` no tiene columna de corte; el trigger inserta sin verificar ningún corte | **Eliminar ese prerrequisito** del checklist; no hay nada que validar |
| 2 | "Encoding: tablas legacy Microsip son `ISO8859_1`" | Solo `CLAVE_CLIENTE` en `DOCTOS_PV` y `DOCTOS_CC` es `ISO8859_1`; el resto de VARCHAR son `NONE` | El adapter puede usar UTF-8 directamente; no necesita conversión para campos de texto libre (`DESCRIPCION`, `USUARIO_CREADOR`, `AVAL_O_RESPONSABLE`, etc.) |
| 3 | La receta menciona `NUMERO_DE_VENDEDORES=47558` como "1 vendedor" | En la BD, `47558` = 1 vendedor (`POSICION=1`), pero se observa `47559` (2 vendedores) más frecuentemente en los datos reales | Usar el ID correcto según el número de vendedores reales de la venta |

Nada más contradice la receta. La mecánica INSERT-`N`→UPDATE-`S`, los IDs de
`FORMAS_COBRO`, el `SUCURSAL_ID`, el generador `ID_DOCTOS`, el folio de
`FOLIOS_CAJAS`, y las preferencias `INTEG_*='S'` están todos confirmados.

---

## IDs reales para seed de config

### Valores fijos (tabla o fila de config única)

| Parámetro | ID |
|---|---|
| `SUCURSAL_ID` | **225490** (Matriz — única sucursal operativa) |
| `FORMA_COBRO_ID` contado | **67** (Efectivo, TIPO='E') |
| `FORMA_COBRO_ID` crédito | **71** (Crédito, TIPO='R') |
| `CLIENTE_EVENTUAL_PV_ID` | **0** (ninguno configurado → todos generan CxC) |
| Concepto CC cargo (venta) | **5** (Venta en mostrador, ID_INTERNO='f', NATURALEZA='C') |
| Concepto CC pago (contado) | **11** (Cobro, ID_INTERNO='P', NATURALEZA='R') |
| Concepto CC enganche | **24533** (Enganche, NATURALEZA='R') |
| `FOLIO_CONCEPTO_ID` enganche | **475145** |

### Mapa frecuencia → `FORMA_DE_PAGO` (LISTA_ATRIB_ID)

| Frecuencia | LISTA_ATRIB_ID |
|---|---|
| Semanal | **33824** |
| Quincenal | **33825** |
| Mensual | **33826** |
| Contado | **840308** |

### Mapa plazo meses → `CREDITO_EN_MESES` (LISTA_ATRIB_ID)

| Plazo | LISTA_ATRIB_ID |
|---|---|
| 18 meses | **33827** |
| 12 meses | **33828** |
| 9 meses | **33829** |
| 6 meses | **33830** |
| 0 (contado) | **840309** |

### Mapa número de vendedores → `NUMERO_DE_VENDEDORES` (LISTA_ATRIB_ID)

| Cantidad | LISTA_ATRIB_ID |
|---|---|
| 1 vendedor | **47558** |
| 2 vendedores | **47559** |
| 3 vendedores | **47560** |

### Mapa zona → caja → cajero (candidatos para `MSP_CFG_ZONA_CAJA`)

El patrón es: `ZONA_CLIENTE_ID` de nombre `R/NN` → `CAJA_ID` de nombre `RUTANN` →
`CAJERO_ID` mismo nombre. Ejemplos de seed (completar con la tabla completa de
arriba para las ~40 rutas):

| ZONA_CLIENTE_ID | ZONA_NOMBRE | CAJA_ID | CAJERO_ID |
|---|---|---|---|
| 12271 | R/01 | 21770 | 22368 |
| 12272 | R/02 | 21787 | 22369 |
| 21563 | R/25 | 22198 | 22392 |
| 21560 | R/MSP_MATRIZ | 22147 | 2998590 |
| 21566 | R/MSP_ZOQUITLAN | 22249 | 22395 |
| 179174 | R/MSP_TEHUACAN | 179199 | 221644 |
| 2990693 | R/22 | 2998592 | 22389 |
| 3034588 | R/28 | 3034589 | 3034611 |
| 634157 | TRABAJADORES MSP | 32119 | 1162794 |
| 23301 | MAYOREO | 64109 | 64127 |
