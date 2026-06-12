# Crear un cliente en Microsip — flujo paso a paso

> Receta de escritura para que **msp-api** dé de alta un cliente directamente en
> la base Firebird de Microsip (familia `CLIENTES` + `DIRS_CLIENTES` +
> `CLAVES_CLIENTES` + `LIBRES_CLIENTES`).
>
> **Verificado tres veces**:
> 1. Captura del flujo del GUI Microsip para URIEL ISAI CORTERO GONZALEZ
>    (`CLIENTE_ID=3083038`) y ALDRICH ABDIEL CORTERO GONZALEZ (`CLIENTE_ID=3083041`)
>    vía `fbtracemgr`.
> 2. **Cliente real creado con éxito desde msp-api**: "PRUEBA AGENTE GO 20260602"
>    (`CLIENTE_ID=15239220`, `CLAVE_CLIENTE=0044523`). Aparece y abre
>    correctamente en el GUI Microsip CXC con zona, cobrador y vendedor visibles.
> 3. Schema validado contra `RDB$RELATION_FIELDS` en `DESARROLLO.FDB`.
>
> Para el procedimiento del trace ver
> [`microsip-trace-runbook.md`](./microsip-trace-runbook.md).

## TL;DR — el modelo en una frase

**Cuatro INSERTs en orden FK dentro de una sola transacción** (`CLIENTES` →
`CLAVES_CLIENTES` → `DIRS_CLIENTES` → `LIBRES_CLIENTES`) y los triggers de
Microsip se encargan del resto. No hay procedure de "alta de cliente" tipo
`APLICA_DOCTO_PV` — es solo escritura directa a 4 tablas.

A diferencia del alta de venta, **no hay cascada compleja**: ningún
`UPDATE … APLICADO='S'` que dispare creación de documentos derivados. El
cliente queda listo apenas se commitea la tx.

## Tablas que escribes vs las que NO

| Escribes tú (4 INSERTs) | Op | | Lo demás (sin tocar) |
|---|---|---|---|
| `CLIENTES` | INSERT | | `MSP_CHANGE_LOG` (lo escribe el trigger `MSP_CLIENTES_LISTEN` automáticamente — legacy del sync Node a Mongo; **sin consumer en este Go**) |
| `CLAVES_CLIENTES` | INSERT (`CLAVE_CLIENTE_ID=-1`) | | |
| `DIRS_CLIENTES` | INSERT | | |
| `LIBRES_CLIENTES` | INSERT | | |

El GUI de Microsip (`Cxc.exe`) emite además un `UPDATE CLIENTES` redundante
inmediatamente después del INSERT (mismos valores) y un `UPDATE … SET
USUARIO_ULT_MODIF = USUARIO_CREADOR, FECHA_HORA_ULT_MODIF =
FECHA_HORA_CREACION` final. Son artefactos del framework Delphi del cliente
GUI; **no son necesarios** si el INSERT manda valores consistentes desde el
inicio.

## Columnas REALMENTE obligatorias (NOT NULL sin default)

Verificado contra `RDB$RELATION_FIELDS` en `DESARROLLO.FDB`. **Mucho menos de
lo que el GUI llena.** Distinción importante:

- **Obligatorio por esquema**: `NULL_FLAG=1` sin `DEFAULT_SOURCE` → si faltan,
  el INSERT explota.
- **Obligatorio por lógica**: la columna es nullable, pero sin ella el cliente
  queda incompleto o no es visible "como cliente real" en el GUI.

**`CLIENTES`** — solo 6 columnas NOT NULL sin default:

```
CLIENTE_ID, NOMBRE, SUJETO_IEPS, DIFERIR_CFDI_COBROS, MONEDA_ID, COND_PAGO_ID
```

El resto (`ESTATUS`, `COBRAR_IMPUESTOS`, `GENERAR_INTERESES`, etc.) son
`NOT NULL CON DEFAULT` (Firebird los rellena) o nullable.

**`DIRS_CLIENTES`** — solo 3 obligatorias:
```
DIR_CLI_ID, CLIENTE_ID, NOMBRE_CONSIG
```

**`CLAVES_CLIENTES`** — 4 obligatorias:
```
CLAVE_CLIENTE_ID, CLAVE_CLIENTE, CLIENTE_ID, ROL_CLAVE_CLI_ID
```

**`LIBRES_CLIENTES`** — 1 obligatoria:
```
CLIENTE_ID
```

→ Para que el cliente sea **funcional y consultable como cliente normal en el
GUI** (no solo "obligatorio por esquema"), sí hay que llenar también ZONA,
COBRADOR, VENDEDOR, TIPO_CLIENTE_ID, dirección, etc. Ver tablas detalladas
abajo. Pero saber que el esquema solo exige 6+3+4+1 = 14 columnas es útil
para minimizar el INSERT si fuera necesario.

## Prerrequisitos — catálogos que deben existir

Los IDs siguientes son **FKs a tablas catálogo** y deben existir antes de
insertar. Valores observados en `DESARROLLO.FDB` (Tehuacán, Puebla):

| FK | Tabla | Valor observado | Significado |
|---|---|---|---|
| `MONEDA_ID` | `MONEDAS` | `1` | MXN |
| `PAIS_ID` | `PAISES` | `336` | México |
| `ESTADO_ID` | `ESTADOS` | `337` | Puebla |
| `CIUDAD_ID` | `CIUDADES` | `338` | Tehuacán |
| `COND_PAGO_ID` | `CONDS_PAGO` | `21497` | Contado (default) |
| `TIPO_CLIENTE_ID` | `TIPOS_CLIENTES` | `21499` | Particular (default) |
| `VIA_EMBARQUE_ID` | `VIAS_EMBARQUE` | `87621` | Default |
| `ZONA_CLIENTE_ID` | `ZONAS_CLIENTES` | `12271`, `21563` | Por ruta del cliente |
| `COBRADOR_ID` | `COBRADORES` | `11294`, `11502` | **No tiene mapeo nativo a zona** — el admin lo asigna manual en el GUI |
| `VENDEDOR_ID` | `VENDEDORES` | `88240`, `88266` | **Microsip vendedor = ruta** (ver sección) |
| `ROL_CLAVE_CLI_ID` | `ROLES_CLAVES_CLIENTES` | `2` | "Principal" |
| `COMPROBANTE_DE_DOMICILIO` | (catálogo Mueblera) | `2992` | Default observado |
| `IDENTIFICACION_OFICIAL` | (catálogo Mueblera) | `6597`, `6598` | INE/pasaporte/etc. |
| `LOCALIDAD` | (catálogo Mueblera) | `-1` (sin) o `793694` | Opcional |

Las celdas con un solo valor tuvieron el **mismo valor** en las dos altas
capturadas. Las otras dependen de la zona del cliente o de elecciones del
usuario.

## Cómo resolver ZONA → COBRADOR + VENDEDOR (la relación no es directa)

**Hallazgo crítico — verificado contra schema**:

- `ZONAS_CLIENTES` **no tiene** `COBRADOR_ID` ni `VENDEDOR_ID`. Solo:
  `ZONA_CLIENTE_ID, NOMBRE, CUENTA_CXC, CUENTA_ANTICIPOS, ES_PREDET, OCULTO`.
- `COBRADORES` **no tiene** liga a zona. Solo: `COBRADOR_ID, NOMBRE,
  POLITICA_COMIS_COB_ID`.
- En Microsip nativo **no hay tabla de mapeo zona → cobrador**.

Pero los nombres siguen una **convención por ruta**:

```
ZONA:      "R/25"
COBRADOR:  "RUTA 25 - NOE CORTERO"   (un cobrador por ruta)
VENDEDOR:  "RUTA25"                   (un vendedor por ruta, Microsip)
```

En la práctica el admin asigna cada cliente a su zona + el cobrador y vendedor
de esa ruta — por convención todos los clientes de la misma zona tienen
mismo cobrador y mismo vendedor. **99.998% de los 44484 clientes activos
tienen cobrador asignado** (`44483/44484`).

### En msp-api ya existe la tabla `MSP_CFG_ZONA_CAJA` que mapea zona →

`CAJA_ID, CAJERO_ID, VENDEDOR_ID` (43/44 zonas pobladas; MAYOREO = `-1`).
**Falta `COBRADOR_ID`** — se agrega con la misma migration pattern. Seed
automático posible:

```sql
UPDATE MSP_CFG_ZONA_CAJA z
SET COBRADOR_ID = (
  SELECT FIRST 1 c.COBRADOR_ID
  FROM CLIENTES c
  WHERE c.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
    AND c.COBRADOR_ID IS NOT NULL
  GROUP BY c.COBRADOR_ID
  ORDER BY COUNT(*) DESC
);
```

(Toma el cobrador más frecuente por zona — refleja la convención operativa.)

## VENDEDORES en Microsip son por ruta, NO por persona

**No confundir** la tabla `VENDEDORES` de Microsip con nuestros vendedores
por venta:

| Cosa | Tabla | Nomenclatura | Cardinalidad |
|---|---|---|---|
| Vendedor-ruta (Microsip) | `VENDEDORES` | `RUTA01`...`RUTA39` | Uno por ruta |
| Nuestros vendedores | `MSP_VENTAS_VENDEDORES` | Personas reales | Varios por venta |

`CLIENTES.VENDEDOR_ID` (FK a Microsip `VENDEDORES`) lleva el **vendedor-ruta**
que corresponde a la zona del cliente. La atribución real de quién hizo la
venta vive en `MSP_VENTAS_VENDEDORES` (msp-api). `MSP_USUARIOS` no tiene liga
directa a `VENDEDOR_ID` de Microsip — son sistemas independientes que
coinciden por nombre de ruta.

## La `CLAVE_CLIENTE`

Es un identificador alterno tipo `0044521` (7 dígitos zero-padded). **Es
incremental** — verificado: las tres altas usaron `0044521 → 0044522 → 0044523`
(GUI, GUI, agente). No hay generator Firebird visible (probablemente el GUI
calcula `MAX(CAST(CLAVE_CLIENTE AS INTEGER)) + 1` en una conexión separada
antes del INSERT).

Para Go: leer máximo, sumar 1, padding a 7 dígitos. Bajo lock para evitar
colisiones bajo concurrencia.

```sql
SELECT LPAD(CAST(MAX(CAST(CLAVE_CLIENTE AS INTEGER)) + 1 AS VARCHAR(20)), 7, '0')
FROM CLAVES_CLIENTES
WHERE ROL_CLAVE_CLI_ID = 2
```

(El `CAST … AS INTEGER` ignora claves no numéricas — Microsip permite claves
alfanuméricas pero las "principales" son siempre numéricas.)

## Los IDs primarios (`CLIENTE_ID`, `DIR_CLI_ID`)

Vienen de un **generador compartido** — `ID_DOCTOS`, el mismo que se usa para
`DOCTOS_PV.DOCTO_PV_ID`, `DOCTOS_CC.DOCTO_CC_ID`, etc. Salto entre dos altas
GUI consecutivas: `3083038 → 3083041`. Salto al alta del agente:
`3083041 → 15239220` (un mes de tráfico de ventas entre medias). **Esperar
gaps de muchos miles** entre altas si hay otra actividad.

```sql
SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE  -- next CLIENTE_ID
SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE  -- next DIR_CLI_ID
```

`CLAVE_CLIENTE_ID` y `LIBRES_CLIENTES` no necesitan generator explícito:
- `CLAVE_CLIENTE_ID = -1` → el trigger `CLAVES_CLIENTES_BEFINS` lo reemplaza.
- `LIBRES_CLIENTES` usa `CLIENTE_ID` como PK; no tiene ID propio.

## Columnas — verificado contra dos altas reales del GUI

### `CLIENTES` (28 columnas en el INSERT del GUI)

| Columna | Tipo | URIEL | ALDRICH | NOT NULL sin default? | Notas |
|---|---|---|---|---|---|
| `CLIENTE_ID` | integer | `3083038` | `3083041` | ✅ | Generator `ID_DOCTOS` |
| `NOMBRE` | varchar(200) | "URIEL …" | "ALDRICH …" | ✅ | Único por convención del GUI (warning, no UNIQUE) |
| `CONTACTO1`, `CONTACTO2` | varchar(50) | NULL | NULL | | No usados en showroom |
| `ESTATUS` | varchar(1) | `'A'` | `'A'` | (default `'A'`) | A=activo, S=suspendido |
| `FECHA_SUSP` | date | NULL | NULL | | Solo si ESTATUS='S' |
| `CAUSA_SUSP` | varchar(100) | NULL | NULL | | Idem |
| `COBRAR_IMPUESTOS` | varchar(1) | `'S'` | `'S'` | (default) | |
| `GENERAR_INTERESES` | varchar(1) | `'S'` | `'S'` | (default) | |
| `EMITIR_EDOCTA` | varchar(1) | `'S'` | `'S'` | (default) | |
| `DIFERIR_CFDI_COBROS` | boolean | false | false | ✅ | Sin default — hay que pasarlo |
| `LIMITE_CREDITO` | bigint(*,−2) | `0` | `10000.00` | (default 0) | Decimal escalado ×100; opcional |
| `MONEDA_ID` | integer | `1` | `1` | ✅ | MXN |
| `COND_PAGO_ID` | integer | `21497` | `21497` | ✅ | Catálogo |
| `TIPO_CLIENTE_ID` | integer | `21499` | `21499` | (nullable) | Catálogo |
| `ZONA_CLIENTE_ID` | integer | `12271` | `21563` | (nullable) | De `venta.direccion.zona_cliente_id` |
| `COBRADOR_ID` | integer | `11294` | `11502` | (nullable) | Derivado de la zona (ver sección) |
| `VENDEDOR_ID` | integer | `88240` | `88266` | (nullable) | Vendedor-ruta de la zona (Microsip) |
| `NOTAS` | blob | NULL | NULL | | No usado |
| `CUENTA_CXC`, `CUENTA_ANTICIPOS` | varchar(30) | NULL | NULL | | No usados |
| `RETIENE_IMPUESTOS` | varchar(1) | `'N'` | `'N'` | (default `'N'`) | |
| `NUM_PROV_CLIENTE` | varchar(35) | NULL | NULL | | No usado |
| `RECEPTOR_CFD` | varchar(30) | NULL | NULL | | No usado |
| `CAMPOS_ADDENDA` | blob TEXT | (hex BLOB_ID) | (hex BLOB_ID) | (nullable) | **Ver gotcha** |
| `SUJETO_IEPS` | varchar(1) | `'N'` | `'N'` | ✅ | Sin default — hay que pasarlo |
| `USUARIO_AUT_CREACION/MODIF` | varchar(31) | NULL | NULL | | No usados |

### `DIRS_CLIENTES` (27 columnas)

| Columna | Tipo | URIEL | ALDRICH | Notas |
|---|---|---|---|---|
| `DIR_CLI_ID` | integer | `3083040` | `3083043` | Generator `ID_DOCTOS` |
| `CLIENTE_ID` | integer | `3083038` | `3083041` | FK al cliente |
| `NOMBRE_CONSIG` | varchar(200) | "Dirección principal" | "Dirección principal" | Hardcoded |
| `CALLE` | varchar(430) | "VICENTE GUERRERO 12\nSAN PEDRO …" | idem | Composición: `NOMBRE_CALLE` + " " + `NUM_EXT` + "\n" + `COLONIA` + ", " + `POBLACION` |
| `CIUDAD_ID` | integer | `338` | `338` | Catálogo |
| `ESTADO_ID` | integer | `337` | `337` | Catálogo |
| `CODIGO_POSTAL` | varchar(10) | NULL | `"74750"` | Opcional |
| `PAIS_ID` | integer | `336` | `336` | México |
| `TELEFONO1` | varchar(35) | NULL | `"2381863330"` | Opcional, del snapshot del cliente |
| `TELEFONO2`, `FAX` | varchar(35) | NULL | NULL | No usados |
| `EMAIL` | varchar(200) | NULL | NULL | No usado en showroom |
| `RFC_CURP` | varchar(18) | NULL | NULL | **No requerido** — venta a "público en general" |
| `TIPO_PERSONA` | varchar(1) | NULL | NULL | No requerido |
| `CLAVE_REGIMEN_FISCAL` | varchar(3) | NULL | NULL | No requerido |
| `TAX_ID`, `CONTACTO` | varchar | NULL | NULL | No usados |
| `VIA_EMBARQUE_ID` | integer | `87621` | `87621` | Catálogo |
| `ES_DIR_PPAL` | varchar(1) | `'S'` | `'S'` | Hardcode S (es la única dir) |
| `USAR_PARA_ENVIOS` | varchar(1) | `'S'` | `'S'` | Hardcode S |
| `USAR_PARA_FACTURAR` | varchar(1) | `'S'` | `'S'` | Hardcode S |
| `NOMBRE_CALLE` | varchar(100) | "VICENTE GUERRERO" | idem | Calle sin número |
| `NUM_EXTERIOR` | varchar(10) | `"12"` | `"12"` | |
| `NUM_INTERIOR` | varchar(10) | NULL | NULL | Opcional |
| `COLONIA` | varchar(100) | "SAN PEDRO ACOQUIACO" | idem | |
| `POBLACION` | varchar(100) | "TEHUACAN" | idem | |
| `REFERENCIA` | varchar(100) | NULL | NULL | Opcional — campo distinto al `REFERENCIA` de `LIBRES_CLIENTES` |

### `CLAVES_CLIENTES` (4 columnas)

| Columna | Tipo | URIEL | ALDRICH | Notas |
|---|---|---|---|---|
| `CLAVE_CLIENTE_ID` | integer | `-1` | `-1` | **Siempre −1** — trigger `CLAVES_CLIENTES_BEFINS` asigna el valor real |
| `CLAVE_CLIENTE` | varchar(20) | `"0044521"` | `"0044522"` | Incremental, 7 dígitos padded |
| `CLIENTE_ID` | integer | `3083038` | `3083041` | FK |
| `ROL_CLAVE_CLI_ID` | integer | `2` | `2` | Rol "principal" |

### `LIBRES_CLIENTES` (15 columnas — tabla custom Mueblera)

| Columna | Tipo | URIEL | ALDRICH | Notas |
|---|---|---|---|---|
| `CLIENTE_ID` | integer | `3083038` | `3083041` | PK + FK |
| `PASSPED` | varchar(99) | NULL | NULL | No usado |
| `CELULAR` | varchar(99) | NULL | NULL | El teléfono va en `DIRS_CLIENTES.TELEFONO1` |
| `SENDPED`, `DIAS_VISITA`, `DIAS_COBRANZA` | varchar(99) | NULL | NULL | No usados |
| `ORDEN_VISITA` | integer | NULL | NULL | No usado |
| `QR`, `SUSPENSION` | varchar(99) | NULL | NULL | No usados |
| `COMPROBANTE_DE_DOMICILIO` | integer | `2992` | `2992` | Catálogo Mueblera, default observado |
| `IDENTIFICACION_OFICIAL` | integer | `6597` | `6598` | Catálogo Mueblera (INE/pasaporte/etc.) |
| `REFERENCIA` | varchar(99) | NULL | `"CASA COLOR AZUL, 2 PISOS"` | Texto libre — referencia de ubicación |
| `U_LATITUD`, `U_LONGITUD` | varchar(99) | NULL | NULL | Opcionales — del GPS de la venta |
| `LOCALIDAD` | integer | `-1` | `793694` | Catálogo; `-1` = sin localidad |

## Triggers que se disparan (no los invocas, son automáticos)

| Trigger | Tabla | Cuándo | Efecto |
|---|---|---|---|
| `CLIENTES_BEFINS` | `CLIENTES` | BEFORE INSERT | Llena `FECHA_HORA_CREACION`, `USUARIO_CREADOR` |
| `MSP_CLIENTES_LISTEN` | `CLIENTES` | AFTER INSERT/UPDATE/DELETE | Escribe a `MSP_CHANGE_LOG` (legacy sync Node→Mongo; **sin consumer en msp-api Go**) |
| `MSP_SALDOS_CLIENTES_AU` | `CLIENTES` | AFTER UPDATE | Si cambia `ZONA_CLIENTE_ID`, propaga a `MSP_SALDOS_VENTAS` |
| `MSP_PAGOS_CLIENTES_AU` | `CLIENTES` | AFTER UPDATE | Idem para `MSP_PAGOS_VENTAS` |
| `CLAVES_CLIENTES_BEFINS` | `CLAVES_CLIENTES` | BEFORE INSERT | Cuando `CLAVE_CLIENTE_ID=-1`, asigna el siguiente del generador |
| `CLAVES_CLIENTES_BEFINSUPD_RI` | `CLAVES_CLIENTES` | BEFORE INS/UPD | RI: verifica que el `CLIENTE_ID` existe |
| `DIRS_CLIENTES_BEFINS` | `DIRS_CLIENTES` | BEFORE INSERT | Auto-fields |
| `DIRS_CLIENTES_BEFINSUPD_RI` | `DIRS_CLIENTES` | BEFORE INS/UPD | RI |

## Lo que el GUI hace y NOSOTROS NO necesitamos

El trace muestra que `Cxc.exe` (cliente Microsip Delphi) emite además:

| Statement | Por qué lo emite el GUI | ¿Lo replicamos? |
|---|---|---|
| `SELECT * FROM CLIENTES WHERE NOMBRE = ?` (2 veces) | Warning visual si el nombre ya existe — UX | No (validar en app layer si quieres la regla) |
| `SELECT COUNT(CLAVE_CLIENTE) … WHERE ROL principal` | Verifica si ya hay clave principal antes de insertar | No (invariante: si ya existe el cliente no estamos en este path) |
| `UPDATE CLIENTES SET (28 cols) WHERE CLIENTE_ID = ?` | Artefacto Delphi: re-escribe lo recién insertado | No |
| `UPDATE CLIENTES SET USUARIO_ULT_MODIF = USUARIO_CREADOR, FECHA_HORA_ULT_MODIF = FECHA_HORA_CREACION` | Nivela los timestamps "creación" vs "última modificación" | No (si el INSERT pasa valores consistentes desde el inicio) |

## Atomicidad y orden FK

Los 4 INSERTs van en **una sola transacción** Firebird. Orden obligatorio:

1. `CLIENTES` (sin FKs salientes)
2. `CLAVES_CLIENTES` (FK → `CLIENTES`, `ROLES_CLAVES_CLIENTES`)
3. `DIRS_CLIENTES` (FK → `CLIENTES`, `PAISES`, `ESTADOS`, `CIUDADES`)
4. `LIBRES_CLIENTES` (FK → `CLIENTES`)

Si cualquier INSERT falla, la tx se revierte y **nada queda persistido** —
incluido el cliente "padre". El generator `ID_DOCTOS` sí avanza (los IDs
quemados no se devuelven), pero ese hueco numérico es inocuo.

## SQL que ya probamos (y funcionó)

Validado contra `DESARROLLO.FDB` el 2026-06-02. Creó al cliente
`CLIENTE_ID=15239220, CLAVE_CLIENTE=0044523` que aparece correctamente en
Microsip GUI:

```sql
SET TERM ^ ;
EXECUTE BLOCK
  RETURNS (out_cliente_id INTEGER, out_dir_cli_id INTEGER, out_clave VARCHAR(20))
AS
BEGIN
  out_cliente_id = GEN_ID(ID_DOCTOS, 1);
  out_dir_cli_id = GEN_ID(ID_DOCTOS, 1);
  SELECT LPAD(CAST(MAX(CAST(CLAVE_CLIENTE AS INTEGER)) + 1 AS VARCHAR(20)), 7, '0')
    FROM CLAVES_CLIENTES
    WHERE ROL_CLAVE_CLI_ID = 2
    INTO :out_clave;

  INSERT INTO CLIENTES
    (CLIENTE_ID, NOMBRE, ESTATUS,
     COBRAR_IMPUESTOS, GENERAR_INTERESES, EMITIR_EDOCTA, DIFERIR_CFDI_COBROS,
     LIMITE_CREDITO, MONEDA_ID, COND_PAGO_ID, TIPO_CLIENTE_ID,
     ZONA_CLIENTE_ID, COBRADOR_ID, VENDEDOR_ID,
     RETIENE_IMPUESTOS, SUJETO_IEPS)
  VALUES
    (:out_cliente_id, 'PRUEBA AGENTE GO 20260602', 'A',
     'S', 'S', 'S', FALSE,
     0, 1, 21497, 21499,
     21563, 11502, 88266,
     'N', 'N');

  INSERT INTO CLAVES_CLIENTES
    (CLAVE_CLIENTE_ID, CLAVE_CLIENTE, CLIENTE_ID, ROL_CLAVE_CLI_ID)
  VALUES (-1, :out_clave, :out_cliente_id, 2);

  INSERT INTO DIRS_CLIENTES
    (DIR_CLI_ID, CLIENTE_ID, NOMBRE_CONSIG, CALLE,
     CIUDAD_ID, ESTADO_ID, PAIS_ID,
     TELEFONO1, VIA_EMBARQUE_ID,
     ES_DIR_PPAL, USAR_PARA_ENVIOS, USAR_PARA_FACTURAR,
     NOMBRE_CALLE, NUM_EXTERIOR, COLONIA, POBLACION)
  VALUES
    (:out_dir_cli_id, :out_cliente_id, 'Dirección principal',
     'CALLE PRUEBA 99' || ASCII_CHAR(10) || 'COL PRUEBA, TEHUACAN',
     338, 337, 336,
     '2381111111', 87621,
     'S', 'S', 'S',
     'CALLE PRUEBA', '99', 'COL PRUEBA', 'TEHUACAN');

  INSERT INTO LIBRES_CLIENTES
    (CLIENTE_ID, COMPROBANTE_DE_DOMICILIO, IDENTIFICACION_OFICIAL, LOCALIDAD)
  VALUES (:out_cliente_id, 2992, 6597, -1);

  SUSPEND;
END^
SET TERM ; ^
COMMIT;
```

Notas del experimento:
- El INSERT a `CLIENTES` omitió `CAMPOS_ADDENDA` (lo dejamos NULL) y `NOTAS`
  (NULL). El cliente abre correctamente en Microsip GUI sin esos campos.
- El trigger `CLAVES_CLIENTES_BEFINS` asignó `CLAVE_CLIENTE_ID = 3083044`
  (substituyendo el `-1` que mandamos).
- Verificado en GUI: la zona, cobrador, vendedor, dirección y teléfono se
  ven correctamente. El cliente se puede editar y guardar como cualquier otro.

Para limpiar el cliente de prueba:
```sql
DELETE FROM LIBRES_CLIENTES WHERE CLIENTE_ID = 15239220;
DELETE FROM DIRS_CLIENTES   WHERE CLIENTE_ID = 15239220;
DELETE FROM CLAVES_CLIENTES WHERE CLIENTE_ID = 15239220;
DELETE FROM CLIENTES        WHERE CLIENTE_ID = 15239220;
COMMIT;
```

## Notas, gotchas y campos NO usados en showroom

- **RFC/CURP, email, régimen fiscal, tipo persona** — no se llenan. Mueblería
  factura sus ventas de showroom a "público en general"; no se requiere
  identidad fiscal del cliente.
- **`CAMPOS_ADDENDA`** — es un blob `TEXT` (subtype 1, dominio `MEMO`). El
  hex `0000000000000059` que aparece en el trace **NO es el contenido** sino
  el handle BLOB_ID que Firebird asigna. El contenido real está vacío;
  `35%` de los 44484 clientes lo tienen NULL en producción. **Usar NULL es
  seguro** — se verificó que el cliente abre y funciona sin él.
- **El trigger `MSP_CLIENTES_LISTEN`** escribirá a `MSP_CHANGE_LOG` en cada
  INSERT/UPDATE. Esa tabla no tiene consumer en msp-api Go — es ruido inocuo
  legacy. Si en el futuro se quiere notificar a otros módulos Go cuando se
  crea un cliente, agregar un `OutboxEnqueuer` al servicio (patrón de ventas,
  ver `internal/ventas/infra/ventoutbox/`). YAGNI por ahora.
- **Generators compartidos** — `CLIENTE_ID` y `DIR_CLI_ID` consumen del mismo
  generator que `DOCTOS_PV_ID`/`DOCTOS_CC_ID`. Esperar gaps de varios miles
  entre dos altas consecutivas si hay otra actividad en el sistema.
- **Defaults observados son de DESARROLLO.FDB** — verificar contra producción
  (`MUEBLERA_SNP.FDB`) antes de hardcodear. Los catálogos pueden tener IDs
  distintos por empresa.
- **`COBRADOR_ID` nullable pero "siempre llenar"** — schema permite NULL pero
  44483/44484 clientes activos tienen cobrador. Resolver vía
  `MSP_CFG_ZONA_CAJA` extendida con `COBRADOR_ID`.
- **`VENDEDOR_ID` de Microsip ≠ nuestros vendedores** — Microsip vendedor =
  ruta (`RUTA01...RUTA39`); nuestros vendedores por venta viven en
  `MSP_VENTAS_VENDEDORES` y son personas reales. Una venta puede tener varios
  vendedores, pero el cliente solo lleva el vendedor-ruta de su zona.
