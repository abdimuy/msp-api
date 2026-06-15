# B3 — Productos por cliente, catálogo y margen

> Datos al: 2026-06-11. Motor: Firebird 5.0.4 (ODS 13.1). Snapshot: `MUEBLERA_SNP.fdb`.
> Todas las consultas son SELECT-only sobre el snapshot.

---

## Bloque 5 — Historial de compras por cliente: `DOCTOS_PV` + `DOCTOS_PV_DET`

### 5.1 Tabla `DOCTOS_PV` — encabezado de venta de punto de venta

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_PV_ID` | INTEGER | — | NOT NULL PK | ID interno del documento PV |
| `CAJA_ID` | INTEGER | — | NOT NULL | Caja registradora |
| `TIPO_DOCTO` | CHAR(1) | — | NOT NULL | Tipo: `V`=venta, `D`=devolución, `P`=pedido |
| `SUCURSAL_ID` | INTEGER | — | NOT NULL | Sucursal |
| `FOLIO` | CHAR(9) | — | NOT NULL | Folio impreso en ticket (texto) |
| `FECHA` | DATE | — | NOT NULL | Fecha de la venta |
| `HORA` | TIME | — | NOT NULL | Hora de la venta |
| `CAJERO_ID` | INTEGER | — | nullable | Usuario cajero |
| `CLIENTE_ID` | INTEGER | — | **nullable** | FK → `CLIENTES.CLIENTE_ID` (NULL = mostrador sin cliente) |
| `CLAVE_CLIENTE` | VARCHAR(20) | — | nullable | Clave textual del cliente |
| `CLIENTE_FAC_ID` | INTEGER | — | nullable | Cliente de facturación (puede diferir) |
| `DIR_CLI_ID` | INTEGER | — | nullable | Dirección de entrega del cliente |
| `ALMACEN_ID` | INTEGER | — | nullable | Almacén que surte |
| `VENDEDOR_ID` | INTEGER | — | nullable | Vendedor asignado al documento |
| `ESTATUS` | CHAR(1) | — | NOT NULL | `A`=activo, `C`=cancelado, `P`=pendiente |
| `APLICADO` | CHAR(1) | — | nullable | Flag de aplicación contable |
| `IMPORTE_NETO` | NUMERIC(15,2) | 15,2 | nullable | Subtotal neto (sin impuestos) |
| `TOTAL_IMPUESTOS` | NUMERIC(15,2) | 15,2 | nullable | IVA total del documento |
| `TOTAL_RETENCIONES` | NUMERIC(15,2) | 15,2 | nullable | Retenciones |
| `TOTAL_FPGC` | NUMERIC(15,2) | 15,2 | nullable | Total con impuesto incluido |
| `DSCTO_IMPORTE` | NUMERIC(15,2) | 15,2 | nullable | Descuento en importe |
| `DSCTO_PCTJE` | NUMERIC(9,6) | 9,6 | nullable | Porcentaje de descuento global |
| `FECHA_VIGENCIA` | DATE | — | nullable | Vigencia del pedido (si aplica) |
| `METODO_PAGO_SAT` | CHAR(3) | — | nullable | Clave SAT de método de pago |
| `SISTEMA_ORIGEN` | CHAR(2) | — | NOT NULL | Origen del registro |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Timestamp de creación |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | — | nullable | Timestamp última modificación |
| `FECHA_HORA_CANCELACION` | TIMESTAMP | — | nullable | Timestamp de cancelación |

**Cobertura medida:**
- Total de registros no cancelados (`ESTATUS <> 'C'`): **422,003**
- Rango de fechas: histórico hasta 2026-06-11

**Vínculo a `DOCTOS_CC`:** `DOCTOS_CC` no tiene `DOCTO_PV_ID`. El enlace lógico es por `CLIENTE_ID` + `FECHA` o por `FOLIO` (columna texto, no integridad referencial). No existe FK directa de PV→CC, lo que significa que la **cadena PV → cargo CxC no se puede unir de forma determinista** sin conocer la convención de folio usada en cada caso. Ver Brecha 5-B.

### 5.2 Tabla `DOCTOS_PV_DET` — líneas del documento de venta

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_PV_DET_ID` | INTEGER | — | NOT NULL PK | ID de línea |
| `DOCTO_PV_ID` | INTEGER | — | NOT NULL FK | → `DOCTOS_PV.DOCTO_PV_ID` |
| `ARTICULO_ID` | INTEGER | — | NOT NULL FK | → `ARTICULOS.ARTICULO_ID` |
| `CLAVE_ARTICULO` | VARCHAR(20) | — | nullable | Clave textual del artículo |
| `UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Cantidad vendida |
| `UNIDADES_DEV` | NUMERIC(18,5) | 18,5 | nullable | Cantidad devuelta |
| `UNIDADES_SURT` | NUMERIC(18,5) | 18,5 | nullable | Unidades surtidas |
| `PRECIO_UNITARIO` | NUMERIC(18,6) | 18,6 | nullable | Precio unitario sin IVA |
| `PRECIO_UNITARIO_IMPTO` | NUMERIC(18,6) | 18,6 | nullable | Precio unitario con IVA |
| `IMPUESTO_POR_UNIDAD` | NUMERIC(18,6) | 18,6 | nullable | IVA por unidad |
| `PCTJE_DSCTO` | NUMERIC(9,6) | 9,6 | nullable | % descuento en línea |
| `PRECIO_TOTAL_NETO` | NUMERIC(15,2) | 15,2 | nullable | Total de la línea sin IVA |
| `DSCTO_ART` | NUMERIC(15,2) | 15,2 | nullable | Descuento artículo |
| `DSCTO_EXTRA` | NUMERIC(15,2) | 15,2 | nullable | Descuento extra |
| `VENDEDOR_ID` | INTEGER | — | nullable | Vendedor de la línea (puede diferir del header) |
| `PCTJE_COMIS` | NUMERIC(9,6) | 9,6 | nullable | % comisión vendedor en línea |
| `POSICION` | INTEGER | — | NOT NULL | Orden de la línea en el documento |
| `ROL` | CHAR(1) | — | NOT NULL | Rol de la partida |
| `NOTAS` | BLOB TEXT | — | nullable | Notas de la línea |

**Cobertura medida:** 150,877 líneas en total.

### 5.3 Cadena de join validada

```sql
-- Cadena PV → PV_DET → ARTICULOS: validada con datos reales
SELECT FIRST 5
  pv.DOCTO_PV_ID,
  pv.CLIENTE_ID,
  pv.FECHA,
  det.ARTICULO_ID,
  det.UNIDADES,
  det.PRECIO_UNITARIO,
  det.PRECIO_TOTAL_NETO,
  a.NOMBRE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
WHERE pv.CLIENTE_ID IS NOT NULL AND pv.ESTATUS <> 'C'
ORDER BY pv.FECHA DESC;
```

**Resultado real (2026-06-11):**

| DOCTO_PV_ID | CLIENTE_ID | FECHA | ARTICULO_ID | UNIDADES | PRECIO_UNITARIO | PRECIO_TOTAL_NETO | NOMBRE |
|---|---|---|---|---|---|---|---|
| 15542211 | 12387 | 2026-06-11 | 710396 | 1.00000 | 1668.000000 | 1668.00 | BASE DE CAMA INDIVIDUAL ANTONI CAFE PAMBAZO TACTOPIEL DUELA |
| 15542211 | 12387 | 2026-06-11 | 897 | 1.00000 | 7674.000000 | 7674.00 | ROPERO MONARCA RAMAS 3 PIEZAS SAN BERNARDO CHOCOLATE |
| 15542211 | 12387 | 2026-06-11 | 3027845 | 1.00000 | 3586.206897 | 3586.21 | TOCADOR GESSEL MARMOL BLANCO MDF SARAI |
| 15542211 | 12387 | 2026-06-11 | 93259 | 1.00000 | 3051.724138 | 3051.72 | LAVADORA REDONDA ACROS 22 KG. MOD. ALF2253EC |
| 15543397 | 34084 | 2026-06-11 | 483 | 1.00000 | 1560.000000 | 1560.00 | BASE DE MADERA MATRIMONIAL CHOCOLATE |

**Cadena VALIDADA.** Join funciona sin ambigüedades: `DOCTO_PV_ID` es la FK entre ambas tablas; `ARTICULO_ID` liga a catálogo.

### 5.4 Artículos y categorías por cliente

```sql
-- Artículos comprados por cliente con categoría
SELECT
  pv.CLIENTE_ID,
  la.NOMBRE AS LINEA,
  a.ARTICULO_ID,
  a.NOMBRE AS ARTICULO,
  COUNT(*) AS VECES_COMPRADO,
  SUM(CAST(det.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS TOTAL_GASTADO
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.CLIENTE_ID = :cliente_id AND pv.ESTATUS <> 'C'
GROUP BY pv.CLIENTE_ID, la.NOMBRE, a.ARTICULO_ID, a.NOMBRE
ORDER BY TOTAL_GASTADO DESC;
```

### 5.5 Cliente más activo medido

```sql
SELECT FIRST 1 CLIENTE_ID, COUNT(*) AS VENTAS
FROM DOCTOS_PV WHERE ESTATUS <> 'C' AND CLIENTE_ID IS NOT NULL
GROUP BY CLIENTE_ID ORDER BY VENTAS DESC;
```
**Resultado:** `CLIENTE_ID=12387`, **2,383 tickets** de venta no cancelados. Es el cliente con mayor frecuencia de compra en la BD.

---

## Bloque 6 — Catálogo: `ARTICULOS` + `LINEAS_ARTICULOS`

### 6.1 Tabla `ARTICULOS`

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `ARTICULO_ID` | INTEGER | — | NOT NULL PK | ID interno |
| `NOMBRE` | VARCHAR(200) | — | NOT NULL | Nombre completo (Win1252 legacy, nombres en mayúsculas) |
| `LINEA_ARTICULO_ID` | INTEGER | — | nullable FK | → `LINEAS_ARTICULOS.LINEA_ARTICULO_ID` (categoría) |
| `ESTATUS` | CHAR(1) | — | NOT NULL | `A`=activo, `S`=suspendido |
| `CAUSA_SUSP` | VARCHAR(100) | — | nullable | Motivo de suspensión |
| `FECHA_SUSP` | DATE | — | nullable | Fecha de suspensión |
| `ES_ALMACENABLE` | CHAR(1) | — | nullable | Si lleva inventario físico |
| `ES_JUEGO` | CHAR(1) | — | nullable | Si es conjunto/juego de piezas |
| `UNIDAD_VENTA` | VARCHAR(20) | — | nullable | Unidad de venta (pieza, par, etc.) |
| `UNIDAD_COMPRA` | VARCHAR(20) | — | nullable | Unidad de compra |
| `DIAS_GARANTIA` | INTEGER | — | nullable | Días de garantía |
| `ES_IMPORTADO` | CHAR(1) | — | nullable | Flag de artículo importado |
| `PESO_UNITARIO` | NUMERIC(18,5) | 18,5 | nullable | Peso en kg |
| `CUENTA_ALMACEN` | VARCHAR(30) | — | nullable | Cuenta contable de almacén |
| `CUENTA_COSTO_VENTA` | VARCHAR(30) | — | nullable | Cuenta contable de costo de venta |
| `CUENTA_VENTAS` | VARCHAR(30) | — | nullable | Cuenta contable de ventas |
| `FACTOR_VENTA` | NUMERIC(18,5) | 18,5 | NOT NULL | Factor de conversión venta |
| `NOTAS_VENTAS` | BLOB TEXT | — | nullable | Notas de ventas (imprimibles en ticket) |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Creación |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | — | nullable | Última modificación |

**Cobertura medida:** 6,319 artículos en total (activos e inactivos).

### 6.2 Tabla `LINEAS_ARTICULOS` — categorías de producto

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `LINEA_ARTICULO_ID` | INTEGER | — | NOT NULL PK | ID de categoría |
| `NOMBRE` | VARCHAR(50) | — | NOT NULL | Nombre de la línea/categoría |
| `GRUPO_LINEA_ID` | INTEGER | — | nullable | Agrupador superior (subcategorías) |
| `CLAVE` | VARCHAR(20) | — | nullable | Clave textual alternativa |
| `ES_PREDET` | CHAR(1) | — | nullable | Si es la línea predeterminada |
| `OCULTO` | CHAR(1) | — | NOT NULL | Si está oculta en el POS |
| `FACTOR_VENTA` | NUMERIC(18,5) | 18,5 | NOT NULL | Factor de precio heredable |
| `CUENTA_ALMACEN` | VARCHAR(30) | — | nullable | Cuenta contable |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Creación |

**Cobertura medida:** 47 categorías.

### 6.3 Mapa completo de categorías (`LINEA_ARTICULO_ID` → nombre)

```sql
SELECT LINEA_ARTICULO_ID, NOMBRE FROM LINEAS_ARTICULOS ORDER BY NOMBRE;
```

**Resultado real (47 categorías):**

| ID | Nombre |
|---|---|
| 772151 | ALACENAS |
| 379 | ARTICULOS DE 2DA |
| 349 | BICICLETAS |
| 376 | BLANCOS |
| 350 | BLU-RAY |
| 351 | BOCINAS PORTATILES |
| 729401 | CALENTADORES SOLARES |
| 352 | CAMAS Y LITERAS |
| 11574 | CAMPANAS |
| 353 | CAR AUDIO |
| 123054 | CASITAS |
| 146542 | CELULAR |
| 354 | COLCHONES Y BOXES |
| 355 | COMEDORES Y ANTECOMEDORES |
| 772157 | COMODAS Y CAJONERAS |
| 356 | CUNAS |
| 357 | DVD |
| 375 | ENSERES DOMESTICOS |
| 358 | ESTUFAS Y PARRILLAS |
| 359 | HORNOS DE MICROONDAS |
| 11774 | KIT CAMAS COMPLETAS |
| 22580 | KIT COMEDORES COMPLETOS |
| 23522 | KIT MESA INFANTIL |
| 360 | LAPTOPS Y PC'S DE ESCRITORIO |
| 361 | LAVADORAS |
| 772155 | LIBREROS Y CENTROS DE ENTRETENIMIENTO |
| 362 | LICUADORAS |
| 378 | MAQUINAS DE COSER |
| 374 | METALICOS |
| 363 | MINICOMPONENTES Y MODULARES |
| 123058 | MONTABLES |
| 364 | MUEBLES DE OFICINA |
| 365 | MUEBLES EN GENERAL |
| 366 | MUEBLES INFANTILES |
| 367 | PANTALLAS |
| 123056 | PISTAS DE CARROS |
| 368 | PLANCHAS |
| 373 | PLASTICOS |
| 772245 | PORTA MICROONDAS, ESPECIEROS, DISPENSEROS |
| 1796075 | PRODUCTOS ELECTRONICOS |
| 369 | RECAMARAS |
| 370 | REFRIGERADORES Y CONGELADORES |
| 772149 | ROPEROS |
| 371 | SALAS Y ESTANCIAS |
| 772153 | TOCADORES |
| 12456 | VARIOS |
| 12053 | VENTILADORES |

### 6.4 Top 15 categorías por volumen de ventas (importe neto)

```sql
SELECT FIRST 15
  la.LINEA_ARTICULO_ID,
  la.NOMBRE AS LINEA,
  COUNT(DISTINCT pv.DOCTO_PV_ID) AS NUM_VENTAS,
  COUNT(det.DOCTO_PV_DET_ID) AS NUM_PARTIDAS,
  SUM(CAST(det.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS TOTAL_VENTAS
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.ESTATUS <> 'C'
GROUP BY la.LINEA_ARTICULO_ID, la.NOMBRE
ORDER BY TOTAL_VENTAS DESC;
```

**Resultado real:**

| Rank | Categoría | Ventas (docs) | Partidas | Total Ventas (MXN) |
|---|---|---|---|---|
| 1 | ROPEROS | 14,021 | 14,525 | $97,330,671 |
| 2 | KIT CAMAS COMPLETAS | 9,637 | 9,675 | $76,588,012 |
| 3 | LAVADORAS | 11,455 | 11,665 | $72,096,187 |
| 4 | COLCHONES Y BOXES | 24,292 | 25,363 | $69,867,637 |
| 5 | PANTALLAS | 8,242 | 8,354 | $63,134,984 |
| 6 | REFRIGERADORES Y CONGELADORES | 5,364 | 5,472 | $55,707,863 |
| 7 | ESTUFAS Y PARRILLAS | 3,715 | 3,754 | $27,983,390 |
| 8 | ALACENAS | 3,799 | 3,860 | $23,204,563 |
| 9 | BOCINAS PORTATILES | 5,416 | 5,615 | $21,788,221 |
| 10 | SALAS Y ESTANCIAS | 1,559 | 1,592 | $21,310,839 |
| 11 | TOCADORES | 3,353 | 3,498 | $17,186,515 |
| 12 | CELULAR | 2,940 | 3,149 | $17,090,092 |
| 13 | ENSERES DOMESTICOS | 4,599 | 5,450 | $12,347,051 |
| 14 | CAMAS Y LITERAS | 12,967 | 14,278 | $10,855,103 |
| 15 | COMODAS Y CAJONERAS | 2,300 | 2,432 | $10,041,977 |

### 6.5 Mapa de pares complementarios propuesto

Definido sobre los nombres reales de categorías de la BD:

| Categoría comprada | Complemento natural | Lógica de recomendación |
|---|---|---|
| KIT CAMAS COMPLETAS / CAMAS Y LITERAS | COLCHONES Y BOXES | Cama sin colchón → oportunidad inmediata |
| COLCHONES Y BOXES | KIT CAMAS COMPLETAS / ROPEROS | Colchón solo → probable upgrade a recámara |
| REFRIGERADORES Y CONGELADORES | ESTUFAS Y PARRILLAS | Clásico par de electrodomésticos de cocina |
| ESTUFAS Y PARRILLAS | ALACENAS / PORTA MICROONDAS, ESPECIEROS, DISPENSEROS | Equipamiento de cocina completo |
| LAVADORAS | REFRIGERADORES Y CONGELADORES | Segunda compra de blanco más común |
| PANTALLAS | BOCINAS PORTATILES / MINICOMPONENTES Y MODULARES | Upgrade de audio para sala con TV nueva |
| SALAS Y ESTANCIAS | PANTALLAS / LIBREROS Y CENTROS DE ENTRETENIMIENTO | Sala → equipo de entretenimiento |
| COMEDORES Y ANTECOMEDORES | ALACENAS / HORNOS DE MICROONDAS | Comedor → equipamiento de comedor/cocina |
| RECAMARAS | COLCHONES Y BOXES / TOCADORES / ROPEROS / COMODAS Y CAJONERAS | Juego de recámara completo |
| MUEBLES INFANTILES / CUNAS | KIT MESA INFANTIL / PISTAS DE CARROS / CASITAS | Cuarto infantil completo |

> **Nota:** los pares son hipótesis sobre comportamiento de compra. Para validarlos estadísticamente se requiere análisis de co-ocurrencia dentro del mismo `CLIENTE_ID` (ver SQL en Brecha 6-A).

---

## Bloque 9 — Inventario y costo: `SALDOS_IN` + `DOCTOS_IN_DET`

### 9.1 Tabla `SALDOS_IN` — saldos mensuales de inventario

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `ARTICULO_ID` | INTEGER | — | NOT NULL PK† | FK → `ARTICULOS.ARTICULO_ID` |
| `ALMACEN_ID` | INTEGER | — | NOT NULL PK† | FK → almacén |
| `ANO` | SMALLINT | — | NOT NULL PK† | Año del saldo |
| `MES` | SMALLINT | — | NOT NULL PK† | Mes del saldo (1–12) |
| `ULTIMO_DIA` | SMALLINT | — | NOT NULL PK† | Último día hábil del mes |
| `ENTRADAS_UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Unidades que entraron en el mes |
| `SALIDAS_UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Unidades que salieron en el mes |
| `ENTRADAS_COSTO` | NUMERIC(15,2) | 15,2 | nullable | Costo total de entradas del mes |
| `SALIDAS_COSTO` | NUMERIC(15,2) | 15,2 | nullable | Costo total de salidas del mes |

† PK compuesta implícita por combinación de los 5 campos.

> **CAVEAT:** `SALDOS_IN` es un resumen mensual de movimientos, no un saldo actual. No contiene `COSTO_PROMEDIO` ni `ULTIMO_COSTO` directamente; para el costo promedio ponderado hay que calcular `ENTRADAS_COSTO / ENTRADAS_UNIDADES` por período.

### 9.2 Tabla `DOCTOS_IN_DET` — líneas de movimientos de inventario

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_IN_DET_ID` | INTEGER | — | NOT NULL PK | ID de línea |
| `DOCTO_IN_ID` | INTEGER | — | NOT NULL FK | → encabezado del movimiento (`DOCTOS_IN`) |
| `ALMACEN_ID` | INTEGER | — | NOT NULL | Almacén |
| `CONCEPTO_IN_ID` | INTEGER | — | NOT NULL | Concepto de movimiento (compra, ajuste, traspaso…) |
| `ARTICULO_ID` | INTEGER | — | NOT NULL FK | → `ARTICULOS.ARTICULO_ID` |
| `CLAVE_ARTICULO` | VARCHAR(20) | — | nullable | Clave textual |
| `TIPO_MOVTO` | CHAR(1) | — | NOT NULL | `E`=entrada, `S`=salida |
| `UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Cantidad del movimiento |
| `COSTO_UNITARIO` | NUMERIC(18,6) | 18,6 | nullable | **Costo unitario en el momento del movimiento** |
| `COSTO_TOTAL` | NUMERIC(15,2) | 15,2 | nullable | Costo total de la línea |
| `METODO_COSTEO` | CHAR(1) | — | NOT NULL | Método: `P`=promedio, `F`=FIFO, etc. |
| `CANCELADO` | CHAR(1) | — | nullable | Si el movimiento fue cancelado |
| `APLICADO` | CHAR(1) | — | nullable | Si fue aplicado contablemente |
| `FECHA` | DATE | — | NOT NULL | Fecha del movimiento |
| `ROL` | CHAR(1) | — | NOT NULL | Rol del movimiento |
| `CENTRO_COSTO_ID` | INTEGER | — | nullable | Centro de costo |

### 9.3 Dónde vive el costo unitario

El costo unitario real está en **`DOCTOS_IN_DET.COSTO_UNITARIO`** (NUMERIC 18,6). Es el costo **en el momento del movimiento de entrada** — refleja el precio de compra registrado en esa entrada de inventario.

`SALDOS_IN` permite calcular costo promedio mensual pero no tiene columna directa de costo por unidad.

### 9.4 SQL para obtener costo por artículo (más reciente entrada)

```sql
-- Ultimo costo de entrada registrado por artículo
SELECT
  did.ARTICULO_ID,
  a.NOMBRE,
  FIRST 1 did.COSTO_UNITARIO AS ULTIMO_COSTO,
  did.FECHA AS FECHA_ULTIMO_COSTO
FROM DOCTOS_IN_DET did
JOIN ARTICULOS a ON a.ARTICULO_ID = did.ARTICULO_ID
WHERE did.TIPO_MOVTO = 'E'
  AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S')
ORDER BY did.ARTICULO_ID, did.FECHA DESC;
-- NOTA: Firebird no soporta DISTINCT ON; usar subconsulta con MAX(FECHA) por ARTICULO_ID.
```

Versión funcional en Firebird:

```sql
-- Costo más reciente por artículo (subconsulta correlacionada)
SELECT
  did.ARTICULO_ID,
  a.NOMBRE,
  did.COSTO_UNITARIO AS ULTIMO_COSTO_ENTRADA,
  did.FECHA
FROM DOCTOS_IN_DET did
JOIN ARTICULOS a ON a.ARTICULO_ID = did.ARTICULO_ID
WHERE did.TIPO_MOVTO = 'E'
  AND did.FECHA = (
    SELECT MAX(did2.FECHA)
    FROM DOCTOS_IN_DET did2
    WHERE did2.ARTICULO_ID = did.ARTICULO_ID
      AND did2.TIPO_MOVTO = 'E'
      AND (did2.CANCELADO IS NULL OR did2.CANCELADO <> 'S')
  )
  AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S');
```

### 9.5 SQL para margen por venta (precio − costo)

```sql
-- Margen por línea de venta usando último costo de entrada
SELECT
  pv.FECHA,
  pv.CLIENTE_ID,
  a.NOMBRE AS ARTICULO,
  la.NOMBRE AS LINEA,
  det.UNIDADES,
  det.PRECIO_UNITARIO,
  costo.ULTIMO_COSTO,
  det.PRECIO_UNITARIO - costo.ULTIMO_COSTO AS MARGEN_UNITARIO,
  CASE WHEN det.PRECIO_UNITARIO > 0
    THEN (det.PRECIO_UNITARIO - costo.ULTIMO_COSTO) / det.PRECIO_UNITARIO * 100
    ELSE NULL END AS MARGEN_PCT
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
JOIN (
  -- subconsulta de último costo por artículo
  SELECT did.ARTICULO_ID,
         did.COSTO_UNITARIO AS ULTIMO_COSTO
  FROM DOCTOS_IN_DET did
  WHERE did.TIPO_MOVTO = 'E'
    AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S')
    AND did.FECHA = (
      SELECT MAX(did2.FECHA)
      FROM DOCTOS_IN_DET did2
      WHERE did2.ARTICULO_ID = did.ARTICULO_ID
        AND did2.TIPO_MOVTO = 'E'
        AND (did2.CANCELADO IS NULL OR did2.CANCELADO <> 'S')
    )
) costo ON costo.ARTICULO_ID = det.ARTICULO_ID
WHERE pv.CLIENTE_ID = :cliente_id AND pv.ESTATUS <> 'C'
ORDER BY pv.FECHA DESC;
```

> **Advertencia de costo promedio:** `SALDOS_IN` permite calcular costo promedio mensual ponderado pero requiere acumulación:
> ```sql
> SELECT ARTICULO_ID, ALMACEN_ID, ANO, MES,
>        CAST(ENTRADAS_COSTO AS DOUBLE PRECISION) /
>          NULLIF(CAST(ENTRADAS_UNIDADES AS DOUBLE PRECISION), 0) AS COSTO_PROM_MES
> FROM SALDOS_IN
> WHERE ENTRADAS_UNIDADES > 0;
> ```
> Este costo promedio mensual es más representativo para margen histórico que el último costo puntual.

---

## Brechas identificadas

### Brecha 5-A — `CLIENTE_ID` nulo en ventas de mostrador
`DOCTOS_PV.CLIENTE_ID` es nullable. Ventas a "público general" no tienen cliente asignado y no pueden agregarse al perfil. La proporción exacta de ventas sin cliente requiere:
```sql
SELECT COUNT(*) FROM DOCTOS_PV WHERE ESTATUS <> 'C' AND CLIENTE_ID IS NULL;
```

### Brecha 5-B — Sin FK directa PV → DOCTOS_CC (CxC)
`DOCTOS_CC` no tiene `DOCTO_PV_ID`. El vínculo es por `CLIENTE_ID` + fecha/folio en texto, sin integridad referencial. Esto impide construir un join determinista entre "artículo vendido" y "cargo generado en CxC". La cadena PV → cargo CxC → cobranza **no es directamente trazable** para análisis automatizado.

### Brecha 6-A — Análisis de co-ocurrencia de categorías (pares complementarios)
El mapa de pares propuesto en §6.5 es heurístico. Para validarlo estadísticamente se requiere:
```sql
-- Co-ocurrencia de categorías en el historial de un cliente
SELECT a1.LINEA_ARTICULO_ID AS LINEA_A, a2.LINEA_ARTICULO_ID AS LINEA_B, COUNT(*) AS CLIENTES_CON_AMBAS
FROM (SELECT DISTINCT pv.CLIENTE_ID, ar.LINEA_ARTICULO_ID
      FROM DOCTOS_PV pv
      JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
      JOIN ARTICULOS ar ON ar.ARTICULO_ID = det.ARTICULO_ID
      WHERE pv.ESTATUS <> 'C' AND pv.CLIENTE_ID IS NOT NULL) a1
JOIN (SELECT DISTINCT pv.CLIENTE_ID, ar.LINEA_ARTICULO_ID
      FROM DOCTOS_PV pv
      JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
      JOIN ARTICULOS ar ON ar.ARTICULO_ID = det.ARTICULO_ID
      WHERE pv.ESTATUS <> 'C' AND pv.CLIENTE_ID IS NOT NULL) a2
  ON a1.CLIENTE_ID = a2.CLIENTE_ID AND a1.LINEA_ARTICULO_ID < a2.LINEA_ARTICULO_ID
GROUP BY a1.LINEA_ARTICULO_ID, a2.LINEA_ARTICULO_ID
ORDER BY CLIENTES_CON_AMBAS DESC;
```
Esta consulta es costosa sobre 422K docs; se recomienda ejecutar en ventana de mantenimiento o en una proyección materializada.

### Brecha 9-A — Costo es puntual, no histórico por venta
`DOCTOS_IN_DET.COSTO_UNITARIO` registra el costo en el momento del movimiento de **entrada** de inventario, no en el momento de la **venta**. Para ventas antiguas, el "último costo" actual puede diferir significativamente del costo vigente cuando se realizó la venta. El margen calculado con costo actual es una **aproximación**, no el margen real de la transacción original.

### Brecha 9-B — Sin costo_unitario en `DOCTOS_PV_DET`
`DOCTOS_PV_DET` no tiene columna de costo. El costo unitario en el momento de la venta no queda registrado en el documento PV. Microsip guarda el costo en los movimientos de inventario generados por la venta (en `DOCTOS_IN_DET` con `TIPO_MOVTO='S'`), pero ligar la salida de inventario al renglón específico de la venta requiere un join adicional no trivial.

### Brecha 9-C — IVA no distinguible en precio de venta
`PRECIO_UNITARIO` en `DOCTOS_PV_DET` puede incluir o excluir IVA según el flag `IMPUESTO_INCLUIDO` de `DOCTOS_PV`. El margen real requiere normalizar el precio a base (sin IVA) antes de comparar contra el costo (que es siempre sin IVA).

### Brecha 9-D — Artículos sin línea de categoría
`ARTICULOS.LINEA_ARTICULO_ID` es nullable. Artículos sin categoría asignada no aparecen en análisis por `LINEAS_ARTICULOS`. Su proporción debe verificarse:
```sql
SELECT COUNT(*) FROM ARTICULOS WHERE LINEA_ARTICULO_ID IS NULL;
```
