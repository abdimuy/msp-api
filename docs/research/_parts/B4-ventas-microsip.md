# B4 — Diccionario de datos corregido: VENTAS sobre `DOCTOS_PV` (Microsip)

> **Corrección estructural.** El análisis previo derivó el RFM de clientes desde el ledger de **crédito** (`DOCTOS_CC` concepto 5), que solo ve ventas a crédito y omite ~75 % de los eventos de compra (ventas de contado). Este bloque re-ancla el análisis de ventas sobre la tabla real de ventas de Microsip `DOCTOS_PV`.
>
> **Fuente:** `MUEBLERA_SNP.fdb` (snapshot). Corte de datos `MAX(FECHA) = 2026-06-11`. Solo lecturas (`SELECT`).
>
> **Ground truth heredado (no se re-deriva):** `DOCTOS_VE` está abandonada (59 filas, ene-2018) → ignorada. `DOCTOS_PV` es LA tabla de ventas: 423,284 filas, 2006-03-26 → 2026-06-11, `CLIENTE_ID` 100 % poblado, 44,781 clientes distintos. Ventas reales = `TIPO_DOCTO IN ('V','P')` + `ESTATUS='N'` = 421,575 filas. Contado vs crédito vive en `DOCTOS_PV_COBROS` (`FORMA_COBRO_ID`: 67=Efectivo, 68=Cheque, 71=Crédito, 27773=Traspaso). Cadena de productos: `DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS → LINEAS_ARTICULOS`.

---

## 1. Clasificación de la venta: CONTADO vs CRÉDITO

Una venta PV es **CRÉDITO** si tiene una línea de cobro con `FORMA_COBRO_ID = 71` en `DOCTOS_PV_COBROS`; en caso contrario es **CONTADO**. Validado primero en muestra (las 10 ventas más recientes traían las 10 con línea 71 = crédito).

### CRÉDITO

```sql
SELECT
  COUNT(*) AS N_CREDITO,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS IMPORTE_CREDITO
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
  AND EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c
              WHERE c.DOCTO_PV_ID = pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID = 71);
```

| N_CREDITO | IMPORTE_CREDITO (IMPORTE_NETO) |
|-----------|-------------------------------:|
| 104,013   | $664,649,206.89 |

### CONTADO

```sql
SELECT
  COUNT(*) AS N_CONTADO,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS IMPORTE_CONTADO
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c
                  WHERE c.DOCTO_PV_ID = pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID = 71);
```

| N_CONTADO | IMPORTE_CONTADO (IMPORTE_NETO) |
|-----------|-------------------------------:|
| 317,562   | $123,011,986.21 |

### Split final

| Clase | Ventas (count) | % count | Importe (IMPORTE_NETO) | % valor |
|-------|---------------:|--------:|-----------------------:|--------:|
| **CONTADO** | 317,562 | **75.3 %** | $123.0 M | 15.6 % |
| **CRÉDITO** | 104,013 | **24.7 %** | $664.6 M | 84.4 % |
| **Total** | **421,575** | 100 % | $787.7 M | 100 % |

`104,013 + 317,562 = 421,575` → cuadra exacto con el ground truth. **Confirma la tesis de la corrección:** el contado es el **75 %** de los eventos de compra (lo que el RFM de crédito no veía), aunque solo ~16 % del valor.

### ENGANCHE — corrección de un supuesto del ground truth

El supuesto heredado decía que "el enganche es la línea efectivo (67) en la misma venta a crédito". **Esto es FALSO en `DOCTOS_PV_COBROS`.** Las líneas de cobro de las ventas a crédito son casi exclusivamente forma 71:

```sql
SELECT c.FORMA_COBRO_ID, COUNT(*) AS N, SUM(CAST(c.IMPORTE AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV_COBROS c
WHERE EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c2
              WHERE c2.DOCTO_PV_ID = c.DOCTO_PV_ID AND c2.FORMA_COBRO_ID = 71)
  AND EXISTS (SELECT 1 FROM DOCTOS_PV pv
              WHERE pv.DOCTO_PV_ID = c.DOCTO_PV_ID AND pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N')
GROUP BY c.FORMA_COBRO_ID ORDER BY 2 DESC;
```

| FORMA_COBRO_ID | N | IMPORTE |
|---------------:|--:|--------:|
| 71 (crédito) | 104,013 | $733,118,777.50 |
| 67 (efectivo) | **1** | $400.00 |

- El **enganche NO se registra como línea efectivo (67) separada** en el PV. La línea 71 lleva el **monto financiado completo** ($733.1 M, incluye enganche + saldo a plazos), mientras `SUM(IMPORTE_NETO)` de las mismas ventas es $664.6 M (precio de lista).
- No existe columna `ENGANCHE` en `DOCTOS_PV` (solo `DSCTO_IMPORTE`, `IMPORTE_DONATIVO`, `IMPORTE_NETO`).
- La columna `TIPO` de `DOCTOS_PV_COBROS` es C (cobro) / A (abono), **no** distingue enganche.

**Total efectivo de contado vs financiado:** todas las líneas efectivo (67) caen en ventas de **contado**, no en crédito:

```sql
SELECT c.FORMA_COBRO_ID, COUNT(*) AS N_LINES, SUM(CAST(c.IMPORTE AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV_COBROS c
JOIN DOCTOS_PV pv ON pv.DOCTO_PV_ID = c.DOCTO_PV_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
GROUP BY c.FORMA_COBRO_ID ORDER BY 2 DESC;
```

| FORMA_COBRO_ID | N_LINES | IMPORTE |
|---------------:|--------:|--------:|
| 67 (efectivo) | 317,735 | $124,720,531.00 |
| 71 (crédito)  | 104,013 | $733,118,777.50 |
| 68 (cheque)   | 13      | $2,150.00 |

> El detalle de enganche por crédito (cuánto se dio de inicial en cada venta a plazos) **no vive en el PV**; vive en el ledger de crédito `DOCTOS_CC` (concepto enganche/abono), aplicable solo a la cohorte de crédito (ver §4).

---

## 2. RFM sobre la base COMPLETA de ventas (contado + crédito), por cliente

Definiciones:
- **Recency** = días de `MAX(FECHA)` del cliente hasta `2026-06-11`.
- **Frequency** = número de ventas (`COUNT(*)`) del cliente.
- **Monetary** = `SUM(IMPORTE_NETO)` (precio de lista; para crédito es el precio a crédito).

```sql
SELECT pv.CLIENTE_ID,
  DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS RECENCY_DAYS,
  COUNT(*) AS FREQUENCY,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS MONETARY
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
GROUP BY pv.CLIENTE_ID;
```

Muestra (top frecuencia): `12387 → R=0 F=2381 M=$32.7M`; `12440 → R=0 F=1075 M=$25.1M`; `106559 → R=1 F=520`; `38327 → R=7 F=494`; `54601 → R=6 F=474`. Los líderes de frecuencia son revendedores/cuentas de ruta (ver §6).

### Distribución de Frequency (44,738 clientes)

```sql
SELECT CASE WHEN FREQUENCY=1 THEN '1'
            WHEN FREQUENCY BETWEEN 2 AND 3 THEN '2-3'
            WHEN FREQUENCY BETWEEN 4 AND 9 THEN '4-9'
            WHEN FREQUENCY BETWEEN 10 AND 49 THEN '10-49'
            ELSE '50+' END AS FREQ_BUCKET, COUNT(*) AS N_CLIENTES
FROM ( SELECT pv.CLIENTE_ID, COUNT(*) AS FREQUENCY FROM DOCTOS_PV pv
       WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY 1;
```

| Frequency | N clientes | % |
|-----------|-----------:|--:|
| 1 (one-shot) | 18,023 | 40.3 % |
| 2-3 | 10,117 | 22.6 % |
| 4-9 | 5,987 | 13.4 % |
| 10-49 | 8,848 | 19.8 % |
| 50+ | 1,763 | 3.9 % |

### Distribución de Recency (44,738 clientes)

```sql
SELECT CASE WHEN RECENCY_DAYS<=30 THEN '0-30d'
            WHEN RECENCY_DAYS<=90 THEN '31-90d'
            WHEN RECENCY_DAYS<=180 THEN '91-180d'
            WHEN RECENCY_DAYS<=365 THEN '181-365d'
            WHEN RECENCY_DAYS<=730 THEN '1-2a'
            ELSE '2a+' END AS REC_BUCKET, COUNT(*) AS N_CLIENTES
FROM ( SELECT pv.CLIENTE_ID, DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS RECENCY_DAYS
       FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY MIN(RECENCY_DAYS);
```

| Recency | N clientes | % |
|---------|-----------:|--:|
| 0-30d | 1,209 | 2.7 % |
| 31-90d | 2,110 | 4.7 % |
| 91-180d | 3,039 | 6.8 % |
| 181-365d | 4,799 | 10.7 % |
| 1-2a | 6,491 | 14.5 % |
| **2a+** | **27,090** | **60.6 %** |

> El 60 % de la base no compra hace 2+ años → enorme universo de winback (consistente con la estrategia de ventas-AI).

### Distribución de Monetary (44,738 clientes)

```sql
SELECT CASE WHEN MONETARY<2000 THEN '<2k'
            WHEN MONETARY<5000 THEN '2k-5k'
            WHEN MONETARY<15000 THEN '5k-15k'
            WHEN MONETARY<50000 THEN '15k-50k'
            WHEN MONETARY<150000 THEN '50k-150k'
            ELSE '150k+' END AS MON_BUCKET, COUNT(*) AS N_CLIENTES,
       SUM(MONETARY) AS IMPORTE_BUCKET
FROM ( SELECT pv.CLIENTE_ID, SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS MONETARY
       FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY MIN(MONETARY);
```

| Monetary | N clientes | Importe del bucket |
|----------|-----------:|-------------------:|
| <2k | 1,677 | $2.2 M |
| 2k-5k | 6,229 | $22.1 M |
| 5k-15k | 22,796 | $195.5 M |
| 15k-50k | 12,164 | $313.0 M |
| 50k-150k | 1,763 | $126.5 M |
| 150k+ | 109 | $128.3 M |

> Cola larga: 109 clientes (revendedores) concentran $128 M; 1,872 clientes (50k+) suman $254.8 M. La masa está en 5k–50k (34,960 clientes, $508 M).

---

## 3. Cohort split — el titular que el RFM de crédito perdía

¿Cuántos clientes compran **solo contado**, **solo crédito**, o **ambos**?

```sql
SELECT CASE WHEN N_CRED>0 AND N_CONT>0 THEN 'AMBOS'
            WHEN N_CRED>0 THEN 'SOLO_CREDITO'
            ELSE 'SOLO_CONTADO' END AS COHORTE, COUNT(*) AS N_CLIENTES
FROM (
  SELECT pv.CLIENTE_ID,
    SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71) THEN 1 ELSE 0 END) AS N_CRED,
    SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71) THEN 0 ELSE 1 END) AS N_CONT
  FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  GROUP BY pv.CLIENTE_ID
) t
GROUP BY 1 ORDER BY 2 DESC;
```

| Cohorte | N clientes | % |
|---------|-----------:|--:|
| **SOLO_CREDITO** | 30,257 | 67.6 % |
| **AMBOS** | 13,677 | 30.6 % |
| **SOLO_CONTADO** | 804 | 1.8 % |
| **Total** | **44,738** | 100 % |

**Lo que el RFM de crédito perdía:**
- **804 clientes** (solo contado) eran **completamente invisibles** al análisis de crédito.
- **13,677 clientes "ambos"** tenían su Frequency/Recency/Monetary **subcontados**: el RFM de crédito solo veía sus compras a plazos, ignorando sus compras de contado.
- En total, **14,481 clientes (32.4 %)** estaban mal medidos por el RFM de crédito, y los **317,562 eventos de contado** (75 % de todas las ventas) no entraban en ninguna métrica de recencia/frecuencia.

---

## 4. Reconciliación del overlay de crédito (`DOCTOS_CC` concepto 5)

`DOCTOS_CC` **no tiene FK a PV** (no hay `DOCTO_PV_ID`). El enlace es por **`FOLIO + CLIENTE_ID + SISTEMA_ORIGEN`**. Validado en muestra (cada venta a crédito empata 1:1 con su cargo concepto 5):

```sql
SELECT FIRST 5 pv.FOLIO, pv.CLIENTE_ID, pv.IMPORTE_NETO,
  (SELECT COUNT(*) FROM DOCTOS_CC cc
   WHERE cc.FOLIO=pv.FOLIO AND cc.CLIENTE_ID=pv.CLIENTE_ID
     AND cc.SISTEMA_ORIGEN='PV' AND cc.CONCEPTO_CC_ID=5) AS CC_MATCH
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO='V' AND pv.ESTATUS='N'
  AND EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
ORDER BY pv.DOCTO_PV_ID DESC;
```

| FOLIO | CLIENTE_ID | IMPORTE_NETO | CC_MATCH |
|-------|-----------:|-------------:|---------:|
| AJ0002163 | 2782515 | 7,500.00 | 1 |
| AJ0002162 | 1267580 | 1,465.52 | 1 |
| AJ0002161 | 2629511 | 8,362.07 | 1 |
| AD0002717 | 1464283 | 9,900.00 | 1 |
| AD0002716 | 3092126 | 7,413.79 | 1 |

### Reconciliación 105,054 vs 108,077 vs 104,013

```sql
-- Cargos concepto 5 por estatus
SELECT cc.ESTATUS, COUNT(*) FROM DOCTOS_CC cc WHERE cc.CONCEPTO_CC_ID=5 GROUP BY cc.ESTATUS;
-- → ESTATUS 'N' : 108,077 (todos)

-- Cargos concepto 5 por sistema origen
SELECT cc.SISTEMA_ORIGEN, COUNT(*) FROM DOCTOS_CC cc WHERE cc.CONCEPTO_CC_ID=5 GROUP BY cc.SISTEMA_ORIGEN;
-- → PV : 108,076 ; CC : 1

-- Ventas PV con línea forma 71, SIN filtro de estatus, por TIPO/ESTATUS
SELECT pv.TIPO_DOCTO, pv.ESTATUS, COUNT(*) FROM DOCTOS_PV pv
WHERE EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
GROUP BY pv.TIPO_DOCTO, pv.ESTATUS;
```

Líneas de cobro forma-71 en PV (sin filtrar estatus):

| TIPO_DOCTO | ESTATUS | N | Interpretación |
|-----------|--------|--:|----------------|
| V | N | 104,013 | venta a crédito **viva** |
| V | C | 822 | venta a crédito **cancelada** |
| V | D | 203 | devolución sobre venta a crédito |
| D | N | 16 | docto devolución |
| | | **105,054** | = total cobros forma 71 (ground truth) |

**Cadena de reconciliación:**
- `104,013` (ventas crédito vivas, V+N) — la base limpia para RFM.
- `+822 +203 +16 = 105,054` cobros forma-71 totales → **cuadra con el ground truth de 105,054**.
- `108,077` cargos concepto-5 (`108,076` origen PV `+1` origen CC) > `105,054`. **Diff ≈ 3,022.** Origen probable: cargos CC de ventas a crédito cuya línea de cobro PV fue editada/borrada tras generar el cargo, planes de cargo múltiple, y 1 cargo de origen `CC` (manual). El rango de fechas de los cargos concepto-5 (2006-03-26 → 2026-06-11) coincide con PV → no son del módulo `DOCTOS_VE` abandonado.

### Dimensión de comportamiento de pago (saldo / % pagado)

`DOCTOS_CC` **no tiene columna `SALDO`** (su `IMPORTE_COBRO` está en 0 en la cabecera; el monto vive en la tabla de detalle de importes CC). El saldo se computa como cargos (concepto 5) menos abonos. Conceptos más frecuentes en `DOCTOS_CC` (`ESTATUS='N'`):

| CONCEPTO_CC_ID | N | Rol |
|---------------:|--:|-----|
| 87327 | 1,810,839 | abono/pago de cobranza (mayoritario) |
| 155 | 314,891 | abono |
| **5** | **108,077** | **cargo (venta a crédito)** |
| 24533 | 69,515 | abono/movimiento |
| 27969 | 60,500 | abono/movimiento |

> **Confirmado:** la dimensión saldo / % pagado / morosidad **sigue siendo válida**, pero aplica **solo a la cohorte de crédito** (43,934 clientes con al menos una venta a plazos = 30,257 solo-crédito + 13,677 ambos). **No se puede aplicar a los 804 clientes solo-contado** (no tienen cuenta CC) ni a la porción de contado de los 13,677 "ambos".

---

## 5. Productos sobre TODAS las ventas (no solo crédito)

Cadena validada end-to-end (`DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS → LINEAS_ARTICULOS`). `DOCTOS_PV_DET` cubre **ambos** tipos de venta (validado: venta crédito `15545178 → LAVADORAS` y crédito `15545161 → LICUADORAS`; el detalle existe igual en contado). Importe de línea = `PRECIO_TOTAL_NETO`.

### Top 15 categorías por importe — TODAS las ventas

```sql
SELECT la.LINEA_ARTICULO_ID, la.NOMBRE,
  COUNT(*) AS N_LINEAS,
  SUM(CAST(d.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET d ON d.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = d.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
GROUP BY la.LINEA_ARTICULO_ID, la.NOMBRE
ORDER BY 4 DESC ROWS 15;
```

| # | Categoría | N líneas | Importe |
|--:|-----------|---------:|--------:|
| 1 | ROPEROS | 14,490 | $97.1 M |
| 2 | KIT CAMAS COMPLETAS | 9,643 | $76.4 M |
| 3 | LAVADORAS | 11,618 | $71.8 M |
| 4 | COLCHONES Y BOXES | 25,289 | $69.7 M |
| 5 | PANTALLAS | 8,300 | $62.8 M |
| 6 | REFRIGERADORES Y CONGELADORES | 5,455 | $55.6 M |
| 7 | ESTUFAS Y PARRILLAS | 3,741 | $27.9 M |
| 8 | ALACENAS | 3,842 | $23.1 M |
| 9 | BOCINAS PORTATILES | 5,558 | $21.6 M |
| 10 | SALAS Y ESTANCIAS | 1,589 | $21.3 M |
| 11 | TOCADORES | 3,478 | $17.1 M |
| 12 | CELULAR | 3,147 | $17.1 M |
| 13 | ENSERES DOMESTICOS | 5,427 | $12.3 M |
| 14 | CAMAS Y LITERAS | 14,232 | $10.8 M |
| 15 | COMODAS Y CAJONERAS | 2,421 | $10.0 M |

### Shift vs solo-contado (top 10 contado)

```sql
SELECT la.NOMBRE, COUNT(*) AS N_LINEAS,
  SUM(CAST(d.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET d ON d.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = d.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
GROUP BY la.NOMBRE ORDER BY 3 DESC ROWS 10;
```

| Categoría | N líneas (contado) | Importe (contado) |
|-----------|-------------------:|------------------:|
| COLCHONES Y BOXES | 1,126 | $2.98 M |
| ROPEROS | 790 | $2.65 M |
| REFRIGERADORES Y CONGELADORES | 226 | $1.36 M |
| LAVADORAS | 384 | $1.35 M |
| PANTALLAS | 168 | $0.98 M |
| KIT CAMAS COMPLETAS | 200 | $0.97 M |
| CAMAS Y LITERAS | 658 | $0.80 M |
| ALACENAS | 238 | $0.68 M |
| BOCINAS PORTATILES | 212 | $0.67 M |
| ESTUFAS Y PARRILLAS | 153 | $0.60 M |

> **El mix de categorías casi no cambia** (mismas líderes), porque por valor el contado es ~16 % del total. El cambio relevante: **COLCHONES Y BOXES** sube a #1 en contado (compra de impulso/efectivo) frente a #4 en la base total, y hay mayor presencia relativa de **CAMAS Y LITERAS** y electrónica chica. La corrección de la base **no reordena materialmente el ranking de productos**; sí corrige el **conteo de eventos y de clientes** (§2-3).

### Manejo de enganche / devolución

- **Enganche:** no aparece como línea de producto en `DOCTOS_PV_DET`; es un movimiento de cobro/CC, no un artículo. No contamina las categorías.
- **Devolución:** se excluye por `TIPO_DOCTO IN ('V','P')` (excluye `D`) y por `ESTATUS='N'` (excluye `C`). El detalle tiene `UNIDADES_DEV` para devoluciones parciales sobre la misma línea; no se descuenta en este conteo (caveat menor).

---

## 6. Gaps corregidos y caveats reales

### Retractación explícita

- **RETRACTADO: el "gap de `CLIENTE_ID` nulo".** Es **FALSO**. `CLIENTE_ID` está 100 % poblado en `DOCTOS_PV` (423,284/423,284, 44,781 distintos). No hay ventas anónimas; toda venta (contado o crédito) es atribuible a un cliente.
- **CORREGIDO: el enganche NO es una línea efectivo (67) sobre la venta a crédito** en `DOCTOS_PV_COBROS`. La línea 71 lleva el monto financiado completo; el detalle de enganche vive en el ledger CC, no en el PV (§1).

### Caveats reales que quedan

1. **Cuentas de revendedor / ruta inflan la frecuencia.** Los líderes son personas reales pero operan como mayoristas/ruta (`12387` VICTORINO ENRIQUEZ: F=2,381, M=$32.7 M; `12440`: F=1,075). 109 clientes 150k+ concentran $128 M. Para RFM de cliente final conviene marcarlos/excluirlos (umbral p.ej. F>50 o M>150k).
2. **Separación enganche vs contado puro.** "Contado" aquí = venta sin línea 71. El monto financiado del crédito (forma 71, $733 M) ≠ `SUM(IMPORTE_NETO)` del crédito ($664.6 M); la diferencia incluye intereses/recargos del plan, no es comparable peso a peso con el contado.
3. **No hay FK PV → CC.** El enlace al ledger de crédito es heurístico (`FOLIO + CLIENTE_ID + SISTEMA_ORIGEN`), validado 1:1 en muestra pero con ~3,022 cargos CC sin venta-crédito viva exacta (cancelaciones/ediciones).
4. **Exclusión de cancelados/devoluciones.** Todo el análisis filtra `ESTATUS='N'` y `TIPO_DOCTO IN ('V','P')`. Cancelados (`C`) y devoluciones (`D`) se excluyen; las devoluciones parciales (`UNIDADES_DEV`) no se restan línea a línea (impacto menor en §5).
5. **Dimensión de pago solo para crédito.** Saldo / % pagado / morosidad (`DOCTOS_CC`) aplica solo a los 43,934 clientes con crédito; no a los 804 solo-contado ni a la porción contado de los 13,677 "ambos".

### Validación end-to-end del RFM (FULL vs CRÉDITO-ONLY)

```sql
SELECT pv.CLIENTE_ID,
  DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS R_FULL,
  COUNT(*) AS F_FULL,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS M_FULL,
  SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS x
       WHERE x.DOCTO_PV_ID=pv.DOCTO_PV_ID AND x.FORMA_COBRO_ID=71) THEN 1 ELSE 0 END) AS F_CRED_ONLY
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  AND pv.CLIENTE_ID IN (11515, 11749, 23649, 12387, 38327)
GROUP BY pv.CLIENTE_ID ORDER BY pv.CLIENTE_ID;
```

| CLIENTE_ID | Cohorte | R_FULL (d) | **F_FULL** | M_FULL | **F_CRED_ONLY** | Eventos perdidos por RFM-crédito |
|-----------:|---------|-----------:|-----------:|-------:|----------------:|---------------------------------:|
| 11515 (LAURA A. SANCHEZ) | ambos | 2,520 | **12** | $16,255 | 3 | **9 (75 %)** |
| 11749 (EMARIEL S. ANDRADE) | ambos | 2,380 | **12** | $12,293 | 2 | **10 (83 %)** |
| 23649 | solo contado | 2,973 | **2** | $15,103 | 0 | **2 (100 %) — invisible** |
| 12387 (VICTORINO ENRIQUEZ) | ambos/reseller | 0 | **2,381** | $32.7 M | 1,921 | 460 (19 %) |
| 38327 | ambos | 7 | **494** | $4.0 M | 259 | 235 (48 %) |

> El RFM de crédito subcontaba la frecuencia entre 19 % y 100 % por cliente y volvía invisibles a los solo-contado. La base corregida (`DOCTOS_PV`, contado+crédito) es la única atribución completa.
