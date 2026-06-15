# C2 — Inteligencia de Producto e Inventario

> **Contexto:** Mueblería mexicana en crédito a plazos, miles de clientes, 20 años de historial POS por línea de producto, ~47 categorías, costo unitario disponible, saldos de inventario en Firebird. Las recomendaciones están calibradas para artículos de ticket alto, rotación lenta, y margen por unidad elevado.

---

## 1. Identificación de SKUs Core / Hero SKU y Análisis ABC

### Recomendación

El análisis ABC es el punto de partida estándar en *world-class* merchandising: clasifica cada SKU según su contribución a los resultados de negocio, concentra el esfuerzo de gestión en los artículos que más importan, y hace explícita la "long tail" que consume capital sin retorno proporcional.

**Dimensión correcta para clasificar:** margen de contribución (ingreso − COGS), NO ingresos brutos ni unidades vendidas. Los ingresos miden actividad, no valor; las unidades malclasifican artículos de alto volumen y margen bajo. Referencia directa de MCP Analytics: *"Revenue measures activity, not value."*

Para una mueblería con costo unitario disponible, la métrica correcta es:

```
Contribución_anual_SKU = SUM(precio_venta − costo_unitario) × unidades_vendidas  [período rolling 12 meses]
```

**Cortes ABC estándar** (verificados en tres fuentes independientes):

| Clase | % de SKUs | % del valor acumulado | Gestión |
|-------|-----------|----------------------|---------|
| A | 10-20% | 70-80% | Revisión semanal, nivel de servicio 98-99%, múltiples proveedores |
| B | 20-30% | 15-20% | Revisión mensual, nivel de servicio 95% |
| C | 50-60% | 5-10% | Reglas automáticas min/max, nivel de servicio 90%, reorden trimestral |

> **Pitfall importante:** Los cortes son *punto de partida*, no ley. MCP Analytics recomienda ajustarlos a la capacidad operativa real (ej. si solo puedes gestionar 30 SKUs de forma diferenciada, esos son tus A items, independientemente de que representen 8% del catálogo). En mueblería con catálogo de 300-500 SKUs activos, los A items probablemente son 30-60 SKUs.

**Diferencia entre top-sellers por unidades vs. margen vs. tráfico:**

- **Por unidades:** Identifica volumen, útil para logística y espacios de piso. Riesgo: puede sobreponderar artículos baratos de baja contribución.
- **Por margen:** Identifica los verdaderos generadores de utilidad. Un sofá con 52% de margen que se vende 20 veces/año > una lámpara que se vende 200 veces con 8% de margen.
- **Por tráfico (*traffic drivers*):** Artículos que atraen visitas aunque no sean los más rentables. En crédito a plazos, el producto de *enganche* bajo actúa como tráfico driver aunque su margen unitario sea menor.

### Fórmula (SQL implementable en Firebird)

```sql
-- Paso 1: Calcular contribución anual por SKU (rolling 12 meses)
WITH contribucion AS (
  SELECT
    d.CLAVE_ARTICULO,
    SUM((d.PRECIO - d.COSTO) * d.CANTIDAD) AS margen_total,
    SUM(d.CANTIDAD)                          AS unidades_vendidas,
    SUM(d.PRECIO * d.CANTIDAD)               AS ingresos_totales
  FROM DOCTOS_VE_DET d
  JOIN DOCTOS_VE h ON h.ID_DOCTO = d.ID_DOCTO
  WHERE h.FECHA >= DATEADD(-365 DAY TO CURRENT_DATE)
    AND h.STATUS NOT IN ('C')  -- excluir canceladas
  GROUP BY d.CLAVE_ARTICULO
),
-- Paso 2: Ranking y acumulado
ranking AS (
  SELECT
    CLAVE_ARTICULO,
    margen_total,
    unidades_vendidas,
    ingresos_totales,
    SUM(margen_total) OVER (ORDER BY margen_total DESC
                            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
      AS margen_acumulado,
    SUM(margen_total) OVER () AS margen_gran_total
  FROM contribucion
)
-- Paso 3: Asignar clase ABC
SELECT
  CLAVE_ARTICULO,
  margen_total,
  unidades_vendidas,
  ROUND(100.0 * margen_acumulado / margen_gran_total, 2) AS pct_acumulado,
  CASE
    WHEN 100.0 * margen_acumulado / margen_gran_total <= 80 THEN 'A'
    WHEN 100.0 * margen_acumulado / margen_gran_total <= 95 THEN 'B'
    ELSE 'C'
  END AS clase_abc
FROM ranking
ORDER BY margen_total DESC;
```

> **Nota Firebird:** `SUM(...) OVER (ORDER BY ...)` requiere Firebird 3.0+. Verificar versión del servidor antes de ejecutar. Para FB 2.5, materializar en tabla temporal.

### Long-tail management

La "long tail" (clase C) en mueblería acumula capital muerto. La estrategia estándar:
1. SKUs C sin venta en 90 días → candidatos a liquidación o devolución a proveedor.
2. SKUs C con 1-2 ventas/año pero margen alto → conservar si el inventario físico es 0 (bajo riesgo de capital ocioso).
3. SKUs C con inventario físico > 3 meses de venta → trigger de markdown o bundle con artículo A.

### Pitfalls

- **No reclasificar basado en un solo período pico.** Una venta grande de diciembre puede promover un SKU de C a A artificialmente. Usar rolling 12 meses y excluir pedidos atípicos.
- **Actualizar la clasificación al menos trimestral** en retail estacional; mensual pre-temporada. En mueblería, el ciclo de vida de un SKU puede ser de 2-5 años, por lo que la frecuencia puede ser trimestral.
- **No usar ingresos brutos** si el catálogo mezcla categorías de precio muy diferente (sillones $8,000 vs. cojines $300).

### Fuentes

- [MCP Analytics — ABC Pareto Analysis Practical Guide](https://mcpanalytics.ai/articles/abc-pareto-analysis-practical-guide-for-data-driven-decisions)
- [Finale Inventory — ABC Analysis Guide](https://www.finaleinventory.com/guides/abc-analysis/)
- [NetSuite — ABC Inventory Analysis](https://www.netsuite.com/portal/resource/articles/inventory-management/abc-inventory-analysis.shtml)
- [MRPeasy — ABC Analysis 80/20 Rule](https://www.mrpeasy.com/blog/abc-analysis/)
- [Netstock — Advantages of ABC Analysis](https://www.netstock.com/blog/the-advantages-of-an-effective-abc-analysis/)

---

## 2. Gestión de Surtido y Categorías (Assortment & Category Management)

### Recomendación

**GMROI** (Gross Margin Return on Inventory Investment) es la métrica central para category management en retail de ticket alto: integra margen y rotación en un solo número, eliminando la ambigüedad de evaluar cada uno por separado.

**Dimensiones de surtido:**
- **Amplitud (*breadth*):** número de categorías ofrecidas (ej. salas, recámaras, comedores, etc.).
- **Profundidad (*depth*):** número de SKUs dentro de cada categoría (ej. 15 modelos de sofá vs. 3).

En mueblería con crédito a plazos, la profundidad excesiva fragmenta el inventario y eleva el capital ocioso. La práctica *world-class* (Blue Yonder, Intuendi) recomienda racionalizacion de SKU basada en GMROI por categoría antes de expandir amplitud.

**Etapas del ciclo de vida del producto** y señales de POS:

| Etapa | Señal en datos POS | Acción de surtido |
|-------|-------------------|------------------|
| Introducción | Ventas <5% del promedio de categoría, margen alto para captar mercado | Asignar espacio de piso limitado, monitoreo semanal |
| Crecimiento | Ventas creciendo >20% MoM por 2-3 meses | Ampliar inventario, negociar volumen con proveedor |
| Madurez | Ventas estables, crecimiento <5% MoM | Optimizar margen, gestionar markdown proactivo |
| Declive | Ventas cayendo >15% MoM por 2 meses consecutivos | Liquidar inventario, no reordenar |

### Fórmula

**GMROI:**
```
GMROI = Margen Bruto ($) / Inventario Promedio a Costo

Donde:
  Margen Bruto ($)          = Ingresos totales − COGS
  Inventario Promedio Costo = (Inventario_inicio + Inventario_fin) / 2

Equivalente:
  GMROI = Rotación de Inventario × Margen Bruto %
```

**Benchmarks de mueblería verificados (2 fuentes independientes):**

| Fuente | Benchmark mueblería |
|--------|---------------------|
| Shopify (datos 2021) | $2.50 GMROI |
| Eightx (benchmarks 2026) | 1.3 – 2.5 GMROI |
| Retalon | $2.00 – $3.00 |
| Toolio (Home Goods) | $2.10 – $4.42 |

> Interpretación estándar: GMROI < $1.00 = el inventario destruye margen bruto. GMROI < $2.00 en mueblería = problema de exceso de inventario en slow movers, no de margen.

**Sell-Through Rate (STR):**
```
STR (%) = (Unidades vendidas / Unidades recibidas) × 100
```

Benchmarks para home goods y mueblería:
- Target saludable: **55-75%** [A MEDIR NOSOTROS — validar contra historial propio]
- Zona de riesgo: **40-55%** — revisar pricing y posicionamiento
- Dead stock: **< 40%** — markdown inevitable

**SQL para ciclo de vida (clasificación por tendencia de ventas):**

```sql
-- Clasificar SKUs por etapa del ciclo de vida usando ventanas de 60 días
WITH ventas_periodos AS (
  SELECT
    d.CLAVE_ARTICULO,
    SUM(CASE WHEN h.FECHA >= DATEADD(-30 DAY TO CURRENT_DATE)
             THEN d.CANTIDAD ELSE 0 END) AS unidades_30d,
    SUM(CASE WHEN h.FECHA BETWEEN DATEADD(-60 DAY TO CURRENT_DATE)
                              AND DATEADD(-31 DAY TO CURRENT_DATE)
             THEN d.CANTIDAD ELSE 0 END) AS unidades_30d_prev,
    SUM(CASE WHEN h.FECHA >= DATEADD(-365 DAY TO CURRENT_DATE)
             THEN d.CANTIDAD ELSE 0 END) AS unidades_anio
  FROM DOCTOS_VE_DET d
  JOIN DOCTOS_VE h ON h.ID_DOCTO = d.ID_DOCTO
  WHERE h.STATUS NOT IN ('C')
  GROUP BY d.CLAVE_ARTICULO
),
tendencia AS (
  SELECT
    CLAVE_ARTICULO,
    unidades_30d,
    unidades_30d_prev,
    unidades_anio,
    CASE
      WHEN unidades_30d_prev = 0 AND unidades_30d > 0 THEN 100.0
      WHEN unidades_30d_prev = 0                      THEN 0.0
      ELSE ROUND(100.0 * (unidades_30d - unidades_30d_prev) / unidades_30d_prev, 1)
    END AS cambio_pct_mom
  FROM ventas_periodos
)
SELECT
  CLAVE_ARTICULO,
  unidades_30d,
  cambio_pct_mom,
  CASE
    WHEN unidades_30d = 0 AND unidades_anio = 0           THEN 'sin_movimiento'
    WHEN unidades_anio > 0 AND cambio_pct_mom < -15       THEN 'declive'
    WHEN cambio_pct_mom BETWEEN -15 AND 5                 THEN 'madurez'
    WHEN cambio_pct_mom > 20                              THEN 'crecimiento'
    ELSE 'introduccion'
  END AS etapa_ciclo_vida
FROM tendencia
ORDER BY unidades_anio DESC;
```

### Pitfalls

- **GMROI agrega y oculta.** Un GMROI de categoría de $2.00 puede esconder 5 SKUs con GMROI de $4.50 y 10 SKUs con GMROI de $0.40. Siempre desagregar a nivel SKU. (Retalon documenta este problema explícitamente.)
- **STR en mueblería es más bajo que en apparel por diseño.** No comparar contra benchmarks de fast fashion (60-80% mensual). El ciclo de consideración en mueblería es de semanas a meses.
- **La etapa del ciclo de vida basada en 30 días es ruidosa** para artículos de baja rotación. Usar ventanas de 90 días o ajustar umbrales para SKUs con <5 ventas/mes.

### Fuentes

- [Shopify — GMROI for Retail: Formula & Benchmarks](https://www.shopify.com/blog/gmroi)
- [Eightx — What is GMROI (2026 benchmarks)](https://eightx.co/blog/what-is-gmroi)
- [Retalon — What is GMROI in Retail](https://retalon.com/blog/what-is-gmroi)
- [Toolio — Complete Guide to GMROI](https://www.toolio.com/post/the-complete-guide-to-gmroi-for-retail-brands)
- [Toolio — Sell-Through Rate Benchmarks](https://www.toolio.com/post/sell-through-rate-how-to-calculate-and-5-strategies-to-optimize)
- [Shopify — Sell-Through Rate 2026](https://www.shopify.com/blog/sell-through-rate)
- [Datatas — Managing Product Lifecycle with SQL](https://datatas.com/managing-product-lifecycle-with-sql/)

---

## 3. Analítica de Precios y Margen

### Recomendación

Para mueblería en crédito a plazos hay **tres dimensiones de margen** que la mayoría de los sistemas de retail ignoran:

1. **Margen de producto** (precio − COSTO): el margen "tradicional" del producto.
2. **Margen mix de categoría** (*margin mix*): cómo la mezcla de ventas entre categorías afecta el margen global — vender más recámaras premium vs. mesas de centro baratas.
3. **Prima de financiamiento** (*financing premium*): la diferencia entre el precio de crédito y el precio de contado, que en mueblería mexicana puede representar 20-80% adicional sobre el precio base dependiendo del plazo.

**Prima de financiamiento — fórmula:**

```
Prima_financiamiento_SKU = Precio_crédito_total − Precio_contado

Tasa_implícita_anual (TIR mensual) = [(Precio_crédito / Precio_contado)^(1/n) − 1] × 12

Donde n = número de meses del plan de pagos
```

Ejemplo: producto con precio contado $5,000, precio crédito (24 meses) $8,400:
- Prima = $3,400 (68% sobre el contado)
- Tasa implícita mensual = (8,400/5,000)^(1/24) − 1 ≈ 2.17% mensual = ~26% anual

Esta prima es **margen adicional real** si la cartera es propia (in-house financing). Si el crédito es otorgado por tercero (banco o financiera), el retailer recibe el precio contado y cede la prima. En el caso de mueblería con cartera propia, el precio de crédito define el ingreso realizado en cuotas.

> **Aplicación analítica:** calcular la TIR implícita por plan de pago y por SKU revela qué productos son más rentables *en términos de flujo de efectivo real*, especialmente cuando los planes de pago varían (6, 12, 18, 24 meses).

**Elasticidad de precio (nivel básico):**

```
Elasticidad_precio = % cambio en cantidad demandada / % cambio en precio

E = (ΔQ / Q₀) / (ΔP / P₀)

|E| > 1 → elástico (demanda sensible al precio; markdown puede aumentar ingreso total)
|E| < 1 → inelástico (demanda poco sensible; markdown reduce ingresos sin compensar con volumen)
```

En mueblería de ticket alto, los artículos de lujo tienden a elasticidad baja (|E| < 1); artículos de entrada de rango (sofás básicos, colchones económicos) pueden ser más elásticos.

> **[A MEDIR NOSOTROS]** La elasticidad real por categoría y por rango de precio solo puede calcularse con historial de cambios de precio y volúmenes resultantes. Con 20 años de datos POS, es factible estimarla con regresión simple en períodos de cambio de precio conocido.

**Markdown optimization (nivel pragmático sin ML):**

Trigger de markdown basado en STR y tiempo en inventario:

| Condición | Acción recomendada |
|-----------|-------------------|
| STR < 40% AND días_inventario > 90 | Markdown Fase 1: −10 a −15% |
| STR < 30% AND días_inventario > 180 | Markdown Fase 2: −20 a −30% |
| STR < 20% AND días_inventario > 270 | Markdown Fase 3: clearance al costo + % mínimo |

**Margin mix — fórmula de seguimiento:**

```
Margen_mix_período = SUM(ventas_SKU × margen_pct_SKU) / SUM(ventas_SKU)

Comparar vs. período anterior para detectar si el mix se está deteriorando
(ej. crecen ventas de categorías de margen bajo)
```

### Pitfalls

- **No confundir precio de crédito con ingreso.** En cartera propia, el ingreso se realiza conforme se cobran las cuotas, no al momento de la venta. Para analítica de margen de producto, usar precio pactado total; para flujo de caja, usar cuotas cobradas.
- **El markdown en mueblería tiene costo de oportunidad de espacio de piso**, no solo de inventario. Un sofá liquidado en 30% de descuento libera espacio para un artículo A. Este costo rara vez se modela en herramientas básicas.
- **La mejora de 400-800 bps en margen por markdown optimization** citada por McKinsey/RELEX aplica a retailers con sistemas de ML sobre catálogos de cientos de miles de SKUs. En mueblería de tamaño medio, una regla de decisión basada en STR + días de inventario ya es un avance significativo. [Refutado como benchmark aplicable directamente — contexto muy diferente.]

### Fuentes

- [RELEX Solutions — The Retailer's Guide to Markdown Optimization](https://www.relexsolutions.com/resources/markdown-optimization/)
- [McKinsey — Why Markdown Pricing Matters More Than Ever](https://www.mckinsey.com/industries/retail/our-insights/hitting-the-mark-why-markdowns-matter-more-than-ever)
- [o9 Solutions — Effective Markdown Optimization](https://o9solutions.com/articles/effective-markdown-optimization)
- [eFM — Implicit Interest Rate Calculation](https://efinancemanagement.com/investment-decisions/implicit-interest-rate)
- [Retail Town — Retail Margin Analysis Formulas](https://retail.town/store-operation/enhancing-retail-profitability-margin-analysis/)
- [Infobae — Precio contado vs. financiado](https://www.infobae.com/economia/2017/01/24/precio-contado-vs-financiado-por-que-uno-sera-mas-barato-y-que-pasara-con-las-cuotas-sin-interes/)

---

## 4. Analítica de Inventario

### Recomendación

Los cuatro KPIs fundamentales de inventario para mueblería son: **rotación**, **DIO/días de suministro**, **GMROI** (ya cubierto en sección 2), y **punto de reorden**. En artículos de ticket alto y rotación lenta, el costo del capital ocioso es el principal riesgo financiero.

### Fórmulas

**Rotación de inventario (Inventory Turns):**
```
Rotación = COGS / Inventario Promedio a Costo

Inventario Promedio = (Inventario_inicio_período + Inventario_fin_período) / 2
```

Benchmarks verificados para mueblería (retail, no manufactura):

| Fuente | Benchmark mueblería |
|--------|---------------------|
| Lightspeed (2024) | 3-4 rotaciones/año |
| Retalon / Shopify | 2-4 rotaciones/año (retailers) |
| DIO equivalente | 91-182 días |

> **[A MEDIR NOSOTROS]** La rotación varía enormemente por categoría dentro de la tienda. Salas y comedores pueden rotar 2x/año; colchones y accesorios 4-6x. El benchmark global de "2-4x" es solo orientativo.

**DIO (Days Inventory Outstanding / Días de Inventario):**
```
DIO = (Inventario a Costo / COGS) × 365

Equivalente:
DIO = 365 / Rotación_de_inventario
```

**Punto de reorden (Reorder Point):**
```
ROP = Demanda_durante_tiempo_de_entrega + Stock_de_seguridad

Demanda_durante_entrega = Ventas_promedio_diarias × Lead_time_días

Stock_de_seguridad (método conservador para slow movers):
SS = (Ventas_máx_diarias × Lead_time_máximo) − (Ventas_prom_diarias × Lead_time_promedio)
```

**Detección de sobrestock vs. desabasto (SQL implementable):**

```sql
-- Clasificar inventario actual por riesgo
WITH rotacion_sku AS (
  SELECT
    d.CLAVE_ARTICULO,
    SUM(d.COSTO * d.CANTIDAD) / 365.0 AS costo_diario_promedio,
    -- Inventario actual: requiere join con tabla de existencias
    i.EXISTENCIA * i.COSTO_PROMEDIO   AS inventario_costo
  FROM DOCTOS_VE_DET d
  JOIN DOCTOS_VE h     ON h.ID_DOCTO = d.ID_DOCTO
  JOIN INVCE_ARTICULOS i ON i.CLAVE_ARTICULO = d.CLAVE_ARTICULO  -- ajustar tabla
  WHERE h.FECHA >= DATEADD(-365 DAY TO CURRENT_DATE)
    AND h.STATUS NOT IN ('C')
  GROUP BY d.CLAVE_ARTICULO, i.EXISTENCIA, i.COSTO_PROMEDIO
),
dio_sku AS (
  SELECT
    CLAVE_ARTICULO,
    inventario_costo,
    costo_diario_promedio,
    CASE
      WHEN costo_diario_promedio > 0
      THEN ROUND(inventario_costo / costo_diario_promedio, 0)
      ELSE 9999
    END AS dias_inventario_actual
  FROM rotacion_sku
)
SELECT
  CLAVE_ARTICULO,
  dias_inventario_actual,
  inventario_costo,
  CASE
    WHEN dias_inventario_actual = 9999 OR dias_inventario_actual > 365 THEN 'sobrestock_critico'
    WHEN dias_inventario_actual > 180                                   THEN 'sobrestock'
    WHEN dias_inventario_actual BETWEEN 30 AND 180                      THEN 'normal'
    WHEN dias_inventario_actual BETWEEN 0 AND 29                        THEN 'bajo_stock'
    ELSE 'sin_inventario'
  END AS clasificacion_inventario
FROM dio_sku
ORDER BY dias_inventario_actual DESC;
```

**GMROI desde el lado del inventario** (complementa sección 2):
```sql
-- GMROI por categoría
SELECT
  a.CLAVE_FAMILIA,
  SUM((d.PRECIO - d.COSTO) * d.CANTIDAD) AS margen_bruto_anual,
  AVG(i.EXISTENCIA * i.COSTO_PROMEDIO)    AS inventario_promedio_costo,
  CASE
    WHEN AVG(i.EXISTENCIA * i.COSTO_PROMEDIO) > 0
    THEN ROUND(
      SUM((d.PRECIO - d.COSTO) * d.CANTIDAD) /
      AVG(i.EXISTENCIA * i.COSTO_PROMEDIO), 2)
    ELSE NULL
  END AS gmroi
FROM DOCTOS_VE_DET d
JOIN DOCTOS_VE h        ON h.ID_DOCTO = d.ID_DOCTO
JOIN INVCE_ARTICULOS a  ON a.CLAVE_ARTICULO = d.CLAVE_ARTICULO
JOIN INVCE_ARTICULOS i  ON i.CLAVE_ARTICULO = d.CLAVE_ARTICULO
WHERE h.FECHA >= DATEADD(-365 DAY TO CURRENT_DATE)
  AND h.STATUS NOT IN ('C')
GROUP BY a.CLAVE_FAMILIA
ORDER BY gmroi DESC NULLS LAST;
```

> **Nota:** `AVG(inventario)` sobre un año requiere snapshots periódicos de existencias para ser exacto. Si solo hay saldo actual, usar saldo actual como aproximación conservadora del promedio.

### Pitfalls

- **La rotación de 2-4x en mueblería no es inherentemente mala.** Compararla contra supermercados o electrónica (10-20x) es un error de categoría. El benchmark relevante es el sector: mueblería mexicana en crédito a plazos. [A MEDIR NOSOTROS — con 20 años de historia propia.]
- **DIO > 120 días en artículos de alto costo de almacenamiento es una alerta financiera**, pero en mueblería de bajo costo de bodega y alta prima de financiamiento, puede ser aceptable si el GMROI compensa.
- **El punto de reorden para slow movers (1-2 ventas/mes) necesita ajustarse.** La fórmula estándar con ventas promedio diarias da ROP muy bajo. Usar lead time en semanas y demanda en unidades/semana, con stock de seguridad de 1 unidad mínima para SKUs A/B.
- **"Stockout" en mueblería puede ser invisible.** Si el artículo no está en piso, el cliente puede no preguntar por él. Monitorear "días sin existencia" como proxy de stockout en lugar de solo ventas perdidas.

### Fuentes

- [Lightspeed — Inventory Turnover Ratio Retail](https://www.lightspeedhq.com/blog/inventory-turnover-ratio/)
- [Shopify — Days Inventory Outstanding](https://www.shopify.com/blog/days-inventory-outstanding)
- [Fishbowl — Days Inventory Outstanding Complete Guide](https://www.fishbowlinventory.com/blog/days-inventory-outstanding)
- [ShipBob — Reorder Point Formula Guide](https://www.shipbob.com/blog/reorder-point-formula/)
- [Netstock — Reorder Point Formula](https://www.netstock.com/blog/reorder-point-formula/)
- [Extensiv — Reorder Point & Safety Stock](https://www.extensiv.com/blog/reorder-point-safety-stock)
- [Wair — Key Inventory Performance Indicators for Retail](https://wair.ai/key-inventory-performance-indicators-for-strategic-retail-management/)

---

## Resumen de benchmarks clave

| Métrica | Benchmark mueblería | Estatus |
|---------|---------------------|---------|
| GMROI | 1.3 – 2.5 (saludable ~$2.20) | Verificado en 3 fuentes |
| Rotación inventario | 2 – 4x / año | Verificado en 2 fuentes |
| DIO | 91 – 182 días | Derivado de rotación |
| Sell-through rate | 55 – 75% target; <40% = dead stock | 1 fuente primaria — [A MEDIR NOSOTROS] |
| ABC cortes (valor) | 80/15/5 o 70/20/10 | Verificado en 4+ fuentes |
| Margen bruto mueblería | 40 – 52% retail | [A MEDIR NOSOTROS — datos propios disponibles] |

---

*Investigación sintetizada con verificación adversarial de fuentes independientes. Junio 2026.*
