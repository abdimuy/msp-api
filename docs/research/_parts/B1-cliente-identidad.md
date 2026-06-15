# B1 — Cliente, Identidad y Señales de Fraude

**Corte de datos:** `MAX(FECHA) = 2026-06-11` en `DOCTOS_CC` (snapshot restaurado en `MUEBLERA_SNP.fdb`).

> **Nota metodológica:** algunas consultas de los pasos 6–9 no pudieron completarse
> porque un proceso `isql` quedó colgado dentro del contenedor `mueblera-firebird-snap`
> (PID 739, 99.9 % CPU), bloqueando la BD con un lock exclusivo. Las secciones
> afectadas (`MSP_GARANTIAS`, `CLAVES_CLIENTES` cobertura live, señales de fraude
> compartidas) se documentan con la SQL correcta y se marcan `[PENDIENTE]`.
> Las cifras de cobertura de `CLIENTES`, `DIRS_CLIENTES` y `MSP_LOCAL_SALE` son reales.

---

## 0. Paridad y frescura

```sql
SELECT MAX(FECHA) AS MAX_FECHA FROM DOCTOS_CC;
-- Resultado: 2026-06-11
```

**Tablas MSP presentes** (11 tablas en total):

```sql
SELECT RDB$RELATION_NAME FROM RDB$RELATIONS
WHERE RDB$RELATION_NAME LIKE 'MSP%' AND RDB$SYSTEM_FLAG = 0
ORDER BY RDB$RELATION_NAME;
```

| Tabla MSP | Descripción funcional |
|---|---|
| `MSP_CHANGE_LOG` | Log de cambios legacy (trigger `MSP_CLIENTES_LISTEN`), sin consumer activo en Go |
| `MSP_GARANTIAS` | Garantías de productos vendidos |
| `MSP_GARANTIA_EVENTOS` | Eventos del flujo de garantía |
| `MSP_GARANTIA_IMAGENES` | Imágenes adjuntas a garantías |
| `MSP_LOCAL_SALE` | Ventas de campo capturadas por la app (contiene `TELEFONO`, `AVAL_O_RESPONSABLE`) |
| `MSP_LOCAL_SALE_COMBO` | Combos de productos en ventas de campo |
| `MSP_LOCAL_SALE_IMAGES` | Imágenes de ventas de campo |
| `MSP_LOCAL_SALE_PRODUCT` | Productos en ventas de campo |
| `MSP_LOCAL_SALE_VENDEDOR` | Vendedores asignados a ventas de campo |
| `MSP_PAGOS_RECIBIDOS` | Pagos de cobranza recibidos (454,838 registros) |
| `MSP_VISITAS` | Visitas de cobranza georreferenciadas (295,871 registros) |

**Tablas del mapa documentado que NO existen:** `MSP_VENTAS`, `MSP_SALDOS_VENTAS`.
El equivalente funcional de "ventas de campo" es `MSP_LOCAL_SALE`. No existe tabla
`MSP_SALDOS_VENTAS` en este snapshot.

**Tablas adicionales relevantes Microsip (no MSP):**
- `LIBRES_CLIENTES` — tabla custom Mueblera (1:1 con `CLIENTES`): `CELULAR`, `REFERENCIA`, `IDENTIFICACION_OFICIAL`, `COMPROBANTE_DE_DOMICILIO`, `U_LATITUD`, `U_LONGITUD`
- `CLAVES_CLIENTES` — clave alfanumérica del cliente (tipo `0044523`)
- `DIRS_CLIENTES` — direcciones y teléfonos

---

## 1. CLIENTES — inventario de columnas y cobertura

### Tipos Firebird (referencia de decodificación)
| Código | Tipo SQL |
|---|---|
| 8 | INTEGER |
| 14 | CHAR |
| 16 | BIGINT (NUMERIC escalado) |
| 23 | BOOLEAN |
| 27 | DOUBLE PRECISION |
| 35 | TIMESTAMP |
| 37 | VARCHAR |
| 261 | BLOB |

### Columnas de CLIENTES

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH,
       F.RDB$FIELD_PRECISION, F.RDB$FIELD_SCALE, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'CLIENTES'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Notas |
|---|---|---|---|---|
| `CLIENTE_ID` | INTEGER | 4 | ✅ | PK; generator `ID_DOCTOS` (compartido) |
| `NOMBRE` | VARCHAR(200) | 200 | ✅ | Win1252; único por convención GUI (no UNIQUE constraint) |
| `CONTACTO1` | VARCHAR(50) | 50 | — | Win1252 |
| `CONTACTO2` | VARCHAR(50) | 50 | — | Win1252 |
| `ESTATUS` | CHAR(1) | 1 | — | Default `'A'`; A=activo, B=bloqueado, V=vendedor-ruta, C=cancelado |
| `CAUSA_SUSP` | VARCHAR(100) | 100 | — | Texto de suspensión |
| `FECHA_SUSP` | DATE | 4 | — | Solo si ESTATUS='S' |
| `COBRAR_IMPUESTOS` | CHAR(1) | 1 | — | Default `'S'` |
| `RETIENE_IMPUESTOS` | CHAR(1) | 1 | — | Default `'N'` |
| `SUJETO_IEPS` | CHAR(1) | 1 | ✅ | Sin default — obligatorio en INSERT |
| `GENERAR_INTERESES` | CHAR(1) | 1 | — | |
| `EMITIR_EDOCTA` | CHAR(1) | 1 | — | |
| `DIFERIR_CFDI_COBROS` | BOOLEAN | 1 | ✅ | Sin default — obligatorio en INSERT |
| `LIMITE_CREDITO` | NUMERIC(15,-2) | 8 | — | Escalado ×100; `0` = contado |
| `MONEDA_ID` | INTEGER | 4 | ✅ | FK `MONEDAS`; `1`=MXN |
| `COND_PAGO_ID` | INTEGER | 4 | ✅ | FK `CONDS_PAGO`; `21497`=Contado |
| `TIPO_CLIENTE_ID` | INTEGER | 4 | — | FK `TIPOS_CLIENTES`; ver distribución abajo |
| `ZONA_CLIENTE_ID` | INTEGER | 4 | — | FK `ZONAS_CLIENTES` |
| `COBRADOR_ID` | INTEGER | 4 | — | FK `COBRADORES` |
| `VENDEDOR_ID` | INTEGER | 4 | — | FK `VENDEDORES`; representa la **ruta**, no la persona |
| `NOTAS` | BLOB SUB_TYPE 1 | — | — | Win1252 |
| `CUENTA_CXC` | VARCHAR(30) | 30 | — | |
| `CUENTA_ANTICIPOS` | VARCHAR(30) | 30 | — | |
| `FORMATOS_EMAIL` | VARCHAR(100) | 100 | — | |
| `RECEPTOR_CFD` | VARCHAR(30) | 30 | — | |
| `NUM_PROV_CLIENTE` | VARCHAR(35) | 35 | — | |
| `CAMPOS_ADDENDA` | BLOB SUB_TYPE 1 | — | — | |
| `USUARIO_CREADOR` | VARCHAR(31) | 31 | — | |
| `FECHA_HORA_CREACION` | TIMESTAMP | 8 | — | Llenado por trigger `CLIENTES_BEFINS` |
| `USUARIO_AUT_CREACION` | VARCHAR(31) | 31 | — | |
| `USUARIO_ULT_MODIF` | VARCHAR(31) | 31 | — | |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | 8 | — | |
| `USUARIO_AUT_MODIF` | VARCHAR(31) | 31 | — | |
| `PRECIO_EMPRESA_ID` | INTEGER | 4 | — | FK lista de precios |

### Cobertura de CLIENTES (45,216 registros totales)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(NOMBRE) AS CON_NOMBRE,
  COUNT(CONTACTO1) AS CON_CONTACTO1,
  COUNT(CONTACTO2) AS CON_CONTACTO2,
  COUNT(ESTATUS) AS CON_ESTATUS,
  COUNT(CAUSA_SUSP) AS CON_CAUSA_SUSP,
  COUNT(FECHA_SUSP) AS CON_FECHA_SUSP,
  COUNT(LIMITE_CREDITO) AS CON_LIMITE_CREDITO,
  COUNT(TIPO_CLIENTE_ID) AS CON_TIPO_CLIENTE,
  COUNT(ZONA_CLIENTE_ID) AS CON_ZONA,
  COUNT(COBRADOR_ID) AS CON_COBRADOR,
  COUNT(VENDEDOR_ID) AS CON_VENDEDOR,
  COUNT(NOTAS) AS CON_NOTAS,
  COUNT(FECHA_HORA_CREACION) AS CON_FECHA_CREACION,
  COUNT(FECHA_HORA_ULT_MODIF) AS CON_FECHA_MODIF
FROM CLIENTES;
```

| Campo | No-nulo | % |
|---|---|---|
| Total registros | 45,216 | — |
| `NOMBRE` | 45,216 | 100.0% |
| `ESTATUS` | 45,216 | 100.0% |
| `LIMITE_CREDITO` | 45,216 | 100.0% |
| `FECHA_HORA_CREACION` | 45,216 | 100.0% |
| `FECHA_HORA_ULT_MODIF` | 45,216 | 100.0% |
| `TIPO_CLIENTE_ID` | 45,215 | ~100.0% |
| `ZONA_CLIENTE_ID` | 45,215 | ~100.0% |
| `COBRADOR_ID` | 45,214 | ~100.0% |
| `VENDEDOR_ID` | 45,195 | 99.9% |
| `NOTAS` | 36,687 | 81.1% |
| `CONTACTO1` | 23,563 | 52.1% |
| `CAUSA_SUSP` | 4,468 | 9.9% |
| `FECHA_SUSP` | 40,426 | 89.4% (nulos = activos, valor presente = suspendidos) |
| `CONTACTO2` | 2,174 | 4.8% |

> **Nota:** `FECHA_SUSP` no-nulo (40,426) parece alto — podría ser que el campo
> almacena una fecha de alta provisional o que muchos clientes tienen fecha histórica
> de suspensión aunque ahora estén activos. Requiere validación cruzada con ESTATUS.

### Distribución ESTATUS

```sql
SELECT ESTATUS, COUNT(*) AS CNT FROM CLIENTES GROUP BY ESTATUS ORDER BY CNT DESC;
```

| ESTATUS | Significado | Cantidad | % |
|---|---|---|---|
| `B` | Bloqueado | 28,463 | 63.0% |
| `A` | Activo | 10,207 | 22.6% |
| `V` | Vendedor-ruta (Microsip) | 6,445 | 14.3% |
| `C` | Cancelado | 101 | 0.2% |

> Los 6,445 registros con `ESTATUS='V'` son registros internos de Microsip
> que representan rutas/vendedores, no clientes reales. Los clientes activos
> reales son 10,207. Los "bloqueados" (28,463) incluyen clientes con saldo
> en mora o dados de baja sin eliminar.

### Distribución TIPO_CLIENTE_ID

```sql
SELECT TIPO_CLIENTE_ID, COUNT(*) AS CNT FROM CLIENTES GROUP BY TIPO_CLIENTE_ID ORDER BY CNT DESC;
```

| `TIPO_CLIENTE_ID` | Descripción documentada | Cantidad | % |
|---|---|---|---|
| `21499` | Público General / Particular | 40,852 | 90.3% |
| `27770` | Mal Cliente | 4,283 | 9.5% |
| `11955` | (otro tipo — no documentado) | 36 | 0.1% |
| `179164` | (otro tipo — no documentado) | 30 | 0.1% |
| `67821` | (otro tipo — no documentado) | 14 | 0.0% |
| `NULL` | Sin asignar | 1 | 0.0% |

**Verificación:** `21499`=Público General y `27770`=Mal Cliente confirman el mapa documentado.
Los tres tipos menores (`11955`, `179164`, `67821`) no estaban en el mapa — requieren
consulta a `TIPOS_CLIENTES` para obtener su nombre.

### Rango de fechas

```sql
SELECT MIN(FECHA_HORA_CREACION) AS PRIMERA_ALTA,
       MAX(FECHA_HORA_CREACION) AS ULTIMA_ALTA,
       MIN(FECHA_HORA_ULT_MODIF) AS PRIMERA_MODIF,
       MAX(FECHA_HORA_ULT_MODIF) AS ULTIMA_MODIF
FROM CLIENTES;
```

| Columna | Valor |
|---|---|
| Primera alta | 2018-01-08 18:01:10 |
| Última alta | 2026-06-11 19:04:51 |
| Primera modificación | 2018-03-23 18:37:19 |
| Última modificación | 2026-06-11 19:41:52 |

Rango de datos: **~8.4 años** de historial de clientes.

---

## 2. DIRS_CLIENTES — direcciones y teléfonos

### Columnas de DIRS_CLIENTES

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'DIRS_CLIENTES'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Notas |
|---|---|---|---|---|
| `DIR_CLI_ID` | INTEGER | 4 | ✅ | PK; generator `ID_DOCTOS` |
| `CLIENTE_ID` | INTEGER | 4 | ✅ | FK `CLIENTES` |
| `NOMBRE_CONSIG` | VARCHAR(200) | 200 | ✅ | Hardcoded `"Dirección principal"` en altas Go |
| `CALLE` | VARCHAR(430) | 430 | — | Composición: `NOMBRE_CALLE` + `NUM_EXT` + `\n` + `COLONIA` + `, ` + `POBLACION` |
| `NOMBRE_CALLE` | VARCHAR(100) | 100 | — | Calle sin número |
| `NUM_EXTERIOR` | VARCHAR(10) | 10 | — | |
| `NUM_INTERIOR` | VARCHAR(10) | 10 | — | |
| `COLONIA` | VARCHAR(100) | 100 | — | |
| `COLONIA_CLAVE_FISCAL` | VARCHAR(4) | 4 | — | |
| `POBLACION` | VARCHAR(100) | 100 | — | |
| `POBLACION_CLAVE_FISCAL` | VARCHAR(3) | 3 | — | |
| `REFERENCIA` | VARCHAR(100) | 100 | — | Referencia de ubicación (campo distinto al de `LIBRES_CLIENTES`) |
| `CIUDAD_ID` | INTEGER | 4 | — | FK `CIUDADES`; `338`=Tehuacán |
| `ESTADO_ID` | INTEGER | 4 | — | FK `ESTADOS`; `337`=Puebla |
| `CODIGO_POSTAL` | VARCHAR(10) | 10 | — | |
| `PAIS_ID` | INTEGER | 4 | — | FK `PAISES`; `336`=México |
| `TELEFONO1` | VARCHAR(35) | 35 | — | **Columna principal de teléfono** |
| `TELEFONO2` | VARCHAR(35) | 35 | — | Rara vez poblada |
| `FAX` | VARCHAR(35) | 35 | — | Prácticamente vacío |
| `EMAIL` | VARCHAR(200) | 200 | — | Casi vacío (2 registros) |
| `RFC_CURP` | VARCHAR(18) | 18 | — | No requerido para "público general" |
| `TIPO_PERSONA` | CHAR(1) | 1 | — | |
| `CLAVE_REGIMEN_FISCAL` | CHAR(3) | 3 | — | |
| `TAX_ID` | VARCHAR(40) | 40 | — | |
| `CONTACTO` | VARCHAR(50) | 50 | — | |
| `VIA_EMBARQUE_ID` | INTEGER | 4 | — | FK catálogo; `87621`=default |
| `ES_DIR_PPAL` | CHAR(1) | 1 | — | `'S'`=principal |
| `USAR_PARA_ENVIOS` | CHAR(1) | 1 | — | |
| `USAR_PARA_FACTURAR` | CHAR(1) | 1 | — | |
| `GLN` | VARCHAR(20) | 20 | — | UTF8 charset |

### Cobertura de DIRS_CLIENTES (45,217 registros totales)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(TELEFONO1) AS CON_TEL1,
  COUNT(CASE WHEN TELEFONO1 IS NOT NULL AND TELEFONO1 <> '' THEN 1 END) AS TEL1_NO_VACIO,
  COUNT(TELEFONO2) AS CON_TEL2,
  COUNT(CASE WHEN TELEFONO2 IS NOT NULL AND TELEFONO2 <> '' THEN 1 END) AS TEL2_NO_VACIO,
  COUNT(EMAIL) AS CON_EMAIL,
  COUNT(CASE WHEN EMAIL IS NOT NULL AND EMAIL <> '' THEN 1 END) AS EMAIL_NO_VACIO,
  COUNT(RFC_CURP) AS CON_RFC,
  COUNT(CASE WHEN RFC_CURP IS NOT NULL AND RFC_CURP <> '' THEN 1 END) AS RFC_NO_VACIO,
  COUNT(COLONIA) AS CON_COLONIA,
  COUNT(CASE WHEN COLONIA IS NOT NULL AND COLONIA <> '' THEN 1 END) AS COLONIA_NO_VACIO,
  COUNT(CIUDAD_ID) AS CON_CIUDAD,
  COUNT(ES_DIR_PPAL) AS CON_PPAL
FROM DIRS_CLIENTES;
```

| Campo | No-nulo o valor | % sobre 45,217 |
|---|---|---|
| Total registros | 45,217 | — |
| `TELEFONO1` no-nulo | 23,823 | 52.7% |
| **`TELEFONO1` no-nulo y no-vacío** | **23,820** | **52.7%** |
| `TELEFONO2` no-nulo y no-vacío | 1,858 | 4.1% |
| `EMAIL` no-vacío | 2 | ~0.0% |
| `RFC_CURP` no-vacío | 1 | ~0.0% |
| `COLONIA` no-vacío | 45,156 | 99.9% |
| `CIUDAD_ID` no-nulo | 45,083 | 99.7% |
| `ES_DIR_PPAL` no-nulo | 45,217 | 100.0% |

**Cobertura telefónica: 52.7% sobre todos los registros de dirección.**

La relación es prácticamente 1:1 entre `CLIENTES` y `DIRS_CLIENTES` (45,216 vs 45,217
filas). La consulta en direcciones principales (`ES_DIR_PPAL = 'S'`) también devolvió
45,216 filas con las mismas cifras de teléfono — confirma que es un registro por cliente.

**Muestra de formato de TELEFONO1:**
```
238 162 3585
238 236 7726
236 113 3320
238 208 6384
236 120 7376
```
Formato predominante: `NNN NNN NNNN` (10 dígitos separados por espacios). Lada 238 = Tehuacán.

### Distribución CIUDAD_ID (top 10)

```sql
SELECT CIUDAD_ID, COUNT(*) AS CNT FROM DIRS_CLIENTES GROUP BY CIUDAD_ID ORDER BY CNT DESC ROWS 10;
```

| CIUDAD_ID | Cantidad | Notas |
|---|---|---|
| 338 | 14,120 | Tehuacán (ciudad principal) |
| 11115 | 5,586 | |
| 11371 | 3,263 | |
| 11599 | 2,119 | |
| 22939 | 2,050 | |
| 11605 | 2,047 | |
| 11373 | 1,950 | |
| 11640 | 1,534 | |
| 11608 | 1,485 | |
| 26220 | 1,432 | |

`CIUDAD_ID=338` (Tehuacán) es la ciudad dominante (31.2% de registros). Los IDs
restantes corresponden a poblaciones de la zona de cobertura (requieren join a
`CIUDADES` para nombres).

---

## 3. Tablas MSP propias — inventario y cobertura

### MSP_LOCAL_SALE (reemplaza al documentado MSP_VENTAS)

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'MSP_LOCAL_SALE'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Descripción |
|---|---|---|---|---|
| `LOCAL_SALE_ID` | CHAR(36) | 36 | ✅ | UUID PK |
| `NOMBRE_CLIENTE` | VARCHAR(200) | 200 | ✅ | Nombre capturado en el momento de la venta |
| `FECHA_VENTA` | TIMESTAMP | 8 | ✅ | |
| `LATITUD` / `LONGITUD` | DOUBLE PRECISION | 8 | ✅ | GPS del vendedor en el momento de la venta |
| `DIRECCION` | VARCHAR(300) | 300 | ✅ | Dirección de entrega libre |
| `PARCIALIDAD` | NUMERIC(12,0) | 8 | ✅ | Monto de pago periódico |
| `ENGANCHE` | NUMERIC(12,0) | 8 | — | |
| **`TELEFONO`** | **VARCHAR(30)** | **30** | **✅** | **Teléfono capturado por el vendedor en campo** |
| `FREC_PAGO` | VARCHAR(50) | 50 | ✅ | Frecuencia de pago |
| **`AVAL_O_RESPONSABLE`** | **VARCHAR(200)** | **200** | **—** | **Nombre del aval o responsable de la deuda** |
| `NOTA` | VARCHAR(500) | 500 | — | |
| `DIA_COBRANZA` | VARCHAR(20) | 20 | ✅ | Día de cobro |
| `PRECIO_TOTAL` | NUMERIC(12,0) | 8 | ✅ | |
| `TIEMPO_A_CORTO_PLAZO_MESES` | INTEGER | 4 | ✅ | |
| `MONTO_A_CORTO_PLAZO` | NUMERIC(12,0) | 8 | ✅ | |
| `ENVIADO` | BOOLEAN | 1 | — | Si fue procesada/enviada a Microsip |
| `USER_EMAIL` | VARCHAR(255) | 255 | — | Usuario vendedor |
| `ALMACEN_ID` | INTEGER | 4 | — | |
| `NUMERO` | VARCHAR(20) | 20 | — | Folio de venta |
| `COLONIA` | VARCHAR(120) | 120 | — | |
| `POBLACION` | VARCHAR(120) | 120 | — | |
| `CIUDAD` | VARCHAR(120) | 120 | — | |
| `TIPO_VENTA` | VARCHAR(20) | 20 | — | |
| `ZONA_CLIENTE_ID` | INTEGER | 4 | — | Zona de la venta |
| `ALMACEN_DESTINO_ID` | INTEGER | 4 | — | |
| `MONTO_CONTADO` | NUMERIC(0,0) | 8 | — | |

### Cobertura MSP_LOCAL_SALE (6,438 registros)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(TELEFONO) AS CON_TEL,
  COUNT(CASE WHEN TELEFONO IS NOT NULL AND TELEFONO <> '' THEN 1 END) AS TEL_NO_VACIO,
  COUNT(AVAL_O_RESPONSABLE) AS CON_AVAL,
  COUNT(CASE WHEN AVAL_O_RESPONSABLE IS NOT NULL AND AVAL_O_RESPONSABLE <> '' THEN 1 END) AS AVAL_NO_VACIO,
  MIN(FECHA_VENTA) AS PRIMERA_VENTA,
  MAX(FECHA_VENTA) AS ULTIMA_VENTA
FROM MSP_LOCAL_SALE;
```

| Campo | Valor | % |
|---|---|---|
| Total ventas de campo | 6,438 | — |
| `TELEFONO` no-nulo | 6,438 | 100.0% |
| `TELEFONO` no-vacío | 5,756 | 89.4% |
| `AVAL_O_RESPONSABLE` no-nulo | 6,438 | 100.0% |
| `AVAL_O_RESPONSABLE` no-vacío | 198 | 3.1% |
| Rango de fechas | 2025-09-13 → 2026-06-11 | ~9 meses |

**`MSP_LOCAL_SALE.TELEFONO` es la fuente más fresca de móviles** (89.4% cobertura efectiva
sobre ventas de campo capturadas desde sep 2025). La cobertura baja de `AVAL_O_RESPONSABLE`
(3.1%) indica que el campo está disponible en la app pero no se captura rutinariamente.

**Enlace a CLIENTES:** `MSP_LOCAL_SALE` **no tiene `CLIENTE_ID`** — las ventas de campo
representan clientes nuevos no-capturados en Microsip aún. El enlace es por `NOMBRE_CLIENTE`
(texto libre) o por la conversión posterior cuando se registra en Microsip.

### MSP_PAGOS_RECIBIDOS (454,838 registros)

Columnas: `ID` (CHAR/UUID), `DOCTO_CC_ID` (INTEGER, FK a `DOCTOS_CC`), `FECHA` (TIMESTAMP).
Tabla de log de pagos. Enlace a `CLIENTES` via `DOCTOS_CC.CLIENTE_ID`.

### MSP_VISITAS (295,871 registros)

| Columna | Tipo | NOT NULL | Descripción |
|---|---|---|---|
| `ID` | CHAR(36) | ✅ | UUID PK |
| `COBRADOR` | VARCHAR(150) | ✅ | Nombre del cobrador |
| `COBRADOR_ID` | INTEGER | ✅ | FK `COBRADORES` |
| `FECHA` | TIMESTAMP | ✅ | |
| `FORMA_COBRO_ID` | INTEGER | ✅ | FK forma de cobro |
| `LAT` / `LNG` | DOUBLE PRECISION | ✅ | GPS de la visita |
| `NOTA` | VARCHAR(10,000) | — | |
| `TIPO_VISITA` | VARCHAR(100) | ✅ | |
| `ZONA_CLIENTE_ID` | INTEGER | ✅ | |
| `CLIENTE_ID` | INTEGER | ✅ | FK `CLIENTES` |
| `IMPTE_DOCTO_CC_ID` | INTEGER | — | FK pago específico |

Permite derivar frecuencia de visitas, patrones de pago, y correlación cobrador-cliente.

### MSP_CHANGE_LOG

Columnas: `LOG_ID` (INTEGER), `TABLE_NAME` (VARCHAR 50), `RECORD_ID` (VARCHAR 50),
`OPERATION` (VARCHAR 10), `CHANGE_TIMESTAMP` (TIMESTAMP), `PROCESSED` (INTEGER).
Producido por el trigger legacy `MSP_CLIENTES_LISTEN`. Sin consumer activo en el Go actual.

### MSP_GARANTIAS — [PENDIENTE]

Esquema no obtenido por lock de BD. Columnas esperadas según código Go: relaciona
productos con garantías post-venta. Tabla presente, filas por confirmar.

---

## 10. Identidad y señales de fraude

### CLAVES_CLIENTES (tabla Microsip)

Documentado contra código Go (`cliente_writer.go`) y doc `microsip-crear-cliente-paso-a-paso.md`.
No fue posible ejecutar consultas live por lock de BD.

| Columna | Tipo SQL | NOT NULL | Notas |
|---|---|---|---|
| `CLAVE_CLIENTE_ID` | INTEGER | ✅ | PK; trigger `CLAVES_CLIENTES_BEFINS` asigna el valor real cuando se inserta con -1 |
| `CLAVE_CLIENTE` | VARCHAR(20) | ✅ | Código alfanumérico tipo `"0044523"` (7 dígitos zero-padded para "principal") |
| `CLIENTE_ID` | INTEGER | ✅ | FK `CLIENTES` |
| `ROL_CLAVE_CLI_ID` | INTEGER | ✅ | FK `ROLES_CLAVES_CLIENTES`; `2`="Principal" |

**Nota arquitectural crítica:** La tabla tiene al menos una página corrupta que
causa error de motor en full-table scans. El código Go usa únicamente acceso
por `CLIENTE_ID` con índice (`CLAVES_CLIENTES_AK1`), evitando el scan completo.
Cualquier consulta de cobertura debe filtrar por `CLIENTE_ID` o usar un índice.

```sql
-- Cobertura estimada (NO ejecutar como full scan):
SELECT FIRST 1 CLAVE_CLIENTE FROM CLAVES_CLIENTES WHERE CLIENTE_ID = 3083038 ORDER BY CLAVE_CLIENTE_ID;
-- [PENDIENTE: confirmar con lock liberado]
```

### LIBRES_CLIENTES — tabla custom Mueblera

Columnas relevantes para identidad (documentado en `microsip-crear-cliente-paso-a-paso.md`):

| Columna | Tipo | Notas |
|---|---|---|
| `CLIENTE_ID` | INTEGER | PK + FK `CLIENTES` |
| `CELULAR` | VARCHAR(99) | Alternativo — en práctica vacío; el teléfono va en `DIRS_CLIENTES.TELEFONO1` |
| `COMPROBANTE_DE_DOMICILIO` | INTEGER | FK catálogo Mueblera; `2992`=default observado |
| `IDENTIFICACION_OFICIAL` | INTEGER | FK catálogo Mueblera; `6597`/`6598`=INE/pasaporte |
| `REFERENCIA` | VARCHAR(99) | Descripción de ubicación: "CASA COLOR AZUL, 2 PISOS" |
| `U_LATITUD` / `U_LONGITUD` | VARCHAR(99) | GPS opcional capturado en el alta |
| `LOCALIDAD` | INTEGER | FK catálogo; `-1`=sin localidad |

**Señal de identidad:** `IDENTIFICACION_OFICIAL` indica el tipo de ID presentada
pero no almacena el número de documento. No hay número de INE, CURP, ni número
de credencial en ninguna tabla Microsip — la cobertura real de identidad formal
es prácticamente nula salvo el `RFC_CURP` de `DIRS_CLIENTES` (0.002% poblado).

### Señales de fraude compartido — EJECUTADO (2026-06-13)

**Resultados agregados:**

| Señal | Casos | Clientes afectados |
|---|---|---|
| Teléfonos compartidos por >1 cliente | **1,615** números | **3,391** clientes (~14% de los 23,820 con teléfono) |
| Caso extremo | 1 teléfono → **7 clientes distintos** | (`238 380 8758`) |
| `CALLE` compartida por >1 cliente | 4,390 calles | señal **débil** — sin número exterior no discrimina hogar real |
| Aval repetido en `MSP_LOCAL_SALE` | **8** avales | señal casi inservible (cobertura aval 3.1%) |

> **Lectura:** el teléfono compartido es la única señal de atributo-compartido con volumen
> accionable, pero está limitada por la cobertura del 52.7% de teléfono. La dirección por `CALLE`
> sin número exterior es ruidosa. El aval es inservible por falta de captura.


#### Teléfono compartido entre distintos clientes

```sql
-- Clientes distintos con el mismo TELEFONO1 (señal de fraude/familia):
SELECT TELEFONO1, COUNT(DISTINCT CLIENTE_ID) AS N_CLIENTES
FROM DIRS_CLIENTES
WHERE TELEFONO1 IS NOT NULL AND TELEFONO1 <> ''
GROUP BY TELEFONO1
HAVING COUNT(DISTINCT CLIENTE_ID) > 1
ORDER BY N_CLIENTES DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

#### Dirección compartida entre distintos clientes

```sql
-- Clientes distintos con misma CALLE (señal de hogar/fraude):
SELECT CALLE, COUNT(DISTINCT CLIENTE_ID) AS N_CLIENTES
FROM DIRS_CLIENTES
WHERE CALLE IS NOT NULL AND CALLE <> ''
GROUP BY CALLE
HAVING COUNT(DISTINCT CLIENTE_ID) > 1
ORDER BY N_CLIENTES DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

#### Aval compartido en MSP_LOCAL_SALE

```sql
-- Mismo nombre de aval aparece en múltiples ventas de campo:
SELECT AVAL_O_RESPONSABLE, COUNT(*) AS N_VENTAS,
       COUNT(DISTINCT NOMBRE_CLIENTE) AS N_CLIENTES_DISTINTOS
FROM MSP_LOCAL_SALE
WHERE AVAL_O_RESPONSABLE IS NOT NULL AND AVAL_O_RESPONSABLE <> ''
GROUP BY AVAL_O_RESPONSABLE
HAVING COUNT(*) > 1
ORDER BY N_VENTAS DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

---

## 11. Calidad de datos y brechas para perfilado

### Resumen de cobertura de señales clave

| Señal | Tabla | Cobertura | Calidad |
|---|---|---|---|
| Teléfono (cliente registrado) | `DIRS_CLIENTES.TELEFONO1` | **52.7%** de 45,217 | Formato heterogéneo (espacios, sin lada uniforme) |
| Teléfono (ventas de campo) | `MSP_LOCAL_SALE.TELEFONO` | 89.4% de 6,438 | Datos frescos sep 2025–jun 2026 |
| Email | `DIRS_CLIENTES.EMAIL` | ~0.0% | No usable |
| RFC/CURP | `DIRS_CLIENTES.RFC_CURP` | ~0.0% | No usable |
| Número de INE | Ninguna tabla | — | **GAP TOTAL** |
| CURP numérico | Ninguna tabla | — | **GAP TOTAL** |
| Tipo de ID presentada | `LIBRES_CLIENTES.IDENTIFICACION_OFICIAL` | Presente (tipo, no número) | FK a catálogo, no el número real |
| Dirección (colonia) | `DIRS_CLIENTES.COLONIA` | 99.9% | Texto libre, no normalizado |
| GPS de alta | `LIBRES_CLIENTES.U_LATITUD/U_LONGITUD` | Bajo — solo altas desde app | Solo desde msp-api go |
| GPS de visita | `MSP_VISITAS.LAT/LNG` | 100% en registros MSP | 295,871 visitas de cobranza |
| Aval/referente | `MSP_LOCAL_SALE.AVAL_O_RESPONSABLE` | 3.1% de ventas campo | Muy baja captura |
| Fecha de alta | `CLIENTES.FECHA_HORA_CREACION` | 100% | |
| Historial de estatus | Solo `ESTATUS` actual | Sin historial temporal | **GAP**: no hay tabla de log de cambios de estado |

### Brechas críticas para perfil de cliente-inteligencia

1. **Identidad formal inexistente:** No se almacena número de INE, CURP, NIP o biometría.
   `LIBRES_CLIENTES.IDENTIFICACION_OFICIAL` guarda solo el _tipo_ de documento, no el número.
   Imposible deduplicar clientes por identidad formal desde la BD.

2. **Email inexistente:** Sólo 2 registros en 45,217 — inutilizable para comunicación digital
   o enriquecimiento.

3. **Teléfono con cobertura parcial:** 47.3% de clientes registrados en Microsip no tiene
   teléfono en `DIRS_CLIENTES`. El 52.7% presente tiene formato heterogéneo (con/sin espacios,
   con/sin lada). Normalización requerida antes de usarlo para matching.

4. **Aval no capturado sistemáticamente:** `MSP_LOCAL_SALE.AVAL_O_RESPONSABLE` existe
   pero solo el 3.1% de ventas de campo lo registra. No hay tabla de avales estructurada.

5. **Sin historial de estados de cliente:** `CLIENTES.ESTATUS` refleja solo el estado
   actual. No existe tabla de auditoría que permita reconstruir cuándo un cliente
   pasó de activo a bloqueado o viceversa.

6. **MSP_VENTAS y MSP_SALDOS_VENTAS no existen** en este snapshot — el mapa documentado
   estaba desactualizado. La funcionalidad está en `MSP_LOCAL_SALE` (ventas campo) y
   el saldo en `DOCTOS_CC`/`IMPTS_DOCTOS_CC`.

7. **CLAVES_CLIENTES tiene corrupción de datos:** Al menos una página corrupta impide
   full-table scans. Las consultas de cobertura de claves deben hacerse por `CLIENTE_ID`
   individual o reconstruirse via backup limpio.

8. **`MSP_LOCAL_SALE` no enlaza directamente a `CLIENTES`:** Las ventas de campo
   capturan `NOMBRE_CLIENTE` como texto libre. El enriquecimiento cruzado requiere
   lógica de matching difuso o el proceso de conversión a Microsip que asigna `CLIENTE_ID`.
