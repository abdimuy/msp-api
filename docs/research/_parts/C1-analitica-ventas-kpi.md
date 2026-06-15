# C1 — Analítica de ventas: KPIs, demanda y cohortes

> **Alcance:** Retailer mexicano de muebles con venta a crédito en abonos, historial de 20 años de POS, productos con costo y categoría, inventario, rutas/zonas. Stack: Go + Firebird, Windows Server, sin nube. Las métricas marcadas **[A MEDIR NOSOTROS]** no tienen benchmarks confiables para este contexto específico y deben calibrarse con datos propios.

---

## 1. Core retail sales KPIs y framework de seguimiento

### Recomendación

Los retailers de clase mundial rastrean un núcleo de siete métricas que cubren rendimiento de ventas, eficiencia de inventario y rentabilidad. Para muebles a crédito, las más críticas son: **Ticket Promedio (ATV)**, **Margen Bruto**, **Sell-Through Rate** (STR) y **Like-for-Like** (ventas comparables). La conversión y UPT son útiles pero requieren conteo de tráfico fiable (dato que muchas muebleras no capturan).

### Fórmulas / definiciones

#### 1.1 Average Transaction Value (ATV) — Ticket promedio

```
ATV = Ingresos totales del período / Número de transacciones del período
```

**Roll-up recomendado:** diario (por zona/ruta), semanal (por categoría), mensual (empresa). En SQL:

```sql
SELECT
  DATE_TRUNC('month', fecha_venta)  AS mes,
  zona_id,
  SUM(total_venta)                  AS ingresos,
  COUNT(*)                          AS num_transacciones,
  SUM(total_venta) / COUNT(*)       AS atv
FROM ventas
GROUP BY 1, 2;
```

**Benchmark general retail:** varía enormemente por categoría. Para muebles medianos-altos: **[A MEDIR NOSOTROS]** — el promedio del sector mueblería en México no tiene publicación confiable en pesos corrientes. Calcular el propio ATV como baseline antes de comparar.

#### 1.2 Units Per Transaction (UPT) — Piezas por ticket

```
UPT = Total de unidades vendidas en el período / Número de transacciones del período
```

En mueblería a crédito, UPT tipicamente es 1.0–2.5 piezas por venta. Un aumento de UPT indica venta cruzada exitosa (colchón + base, sala + comedor). Compartir diariamente con el equipo de ventas.

```sql
SELECT
  DATE_TRUNC('week', fecha_venta) AS semana,
  vendedor_id,
  SUM(cantidad)                   AS unidades,
  COUNT(DISTINCT folio_venta)     AS tickets,
  CAST(SUM(cantidad) AS NUMERIC(10,2)) / COUNT(DISTINCT folio_venta) AS upt
FROM detalle_ventas dv
JOIN ventas v ON v.id = dv.venta_id
GROUP BY 1, 2;
```

#### 1.3 Sell-Through Rate (STR)

```
STR = (Unidades vendidas en el período / Unidades recibidas en el período) × 100
```

Alternativamente, sobre inventario disponible:

```
STR = (Unidades vendidas / (Inventario inicial + Unidades recibidas)) × 100
```

**Benchmarks verificados:** Shopify y Cin7 reportan 70–80% como rango saludable para productos de temporada. Home improvement (categoría más cercana a muebles): ~55% en primeras 8 semanas, ~90% en el año. Fragrancia/cosméticos (no aplica): 23–25% en 8 semanas. **Para muebles: [A MEDIR NOSOTROS]** — la categoría tiene ciclos de reposición muy distintos a moda.

**Pitfall clave:** usar solo unidades recibidas (no el inventario abierto) sobreestima el STR para SKUs de alta rotación y subestima problemas en SKUs estancados.

#### 1.4 Gross Margin % (Margen Bruto)

```
Margen Bruto % = ((Ingresos - COGS) / Ingresos) × 100
```

donde `COGS` = costo de los artículos vendidos (precio de compra al proveedor, flete de entrada).

```sql
SELECT
  categoria_id,
  DATE_TRUNC('month', fecha_venta) AS mes,
  SUM(total_venta)                 AS ingresos,
  SUM(costo_total)                 AS cogs,
  (SUM(total_venta) - SUM(costo_total)) / SUM(total_venta) * 100 AS margen_bruto_pct
FROM detalle_ventas dv
JOIN productos p ON p.id = dv.producto_id
JOIN ventas v    ON v.id = dv.venta_id
GROUP BY 1, 2;
```

**Benchmarks verificados:**
- Retail general: 40–60% (qoblex, 2025).
- Wholesale furniture USA 2022: 34.7% (Statista).
- Boutique furniture retailers: >45%.
- Mueblería mediana México **[A MEDIR NOSOTROS]** — nuestros datos reales (ver `verified_unit_economics.md`) señalan ~52.8% de margen, por encima del benchmark gringo de wholesale porque vendemos directo al consumidor con crédito propio.

**Pitfall:** el margen bruto no captura el costo del crédito (costo financiero de cartera vencida, costo de fondeo). En mueblería a crédito, el margen operativo real es sensiblemente menor al bruto.

#### 1.5 Contribution Margin (Margen de Contribución por SKU/Categoría)

```
Contribution Margin % = ((Ingresos - Costos Variables) / Ingresos) × 100
```

Los costos variables incluyen: COGS + comisiones de venta + flete de entrega + costos de cobranza variables. Excluye costos fijos (renta, nómina fija, depreciación).

**Diferencia clave con margen bruto:** el contribution margin es siempre mayor o igual al margen bruto porque excluye costos fijos. Útil para decidir si mantener o discontinuar una línea de producto.

```sql
-- Contribution margin por categoria (simplified: solo COGS y comision)
SELECT
  p.categoria_id,
  SUM(dv.total_venta)                                          AS ingresos,
  SUM(dv.costo_total)                                          AS cogs,
  SUM(dv.total_venta * 0.04)                                   AS comision_est,  -- ajustar pct real
  SUM(dv.total_venta - dv.costo_total - dv.total_venta * 0.04)
    / SUM(dv.total_venta) * 100                                AS contrib_margin_pct
FROM detalle_ventas dv
JOIN productos p ON p.id = dv.producto_id
GROUP BY 1;
```

#### 1.6 Same-Store / Like-for-Like (LFL) Sales — Ventas comparables

```
LFL % = ((Ventas período actual - Ventas mismo período año anterior) / Ventas mismo período año anterior) × 100
```

**Criterio de inclusión:** solo sucursales/rutas que operaron en ambos períodos. Las que abrieron o cerraron se excluyen.

```sql
-- LFL mensual por zona (solo zonas activas en ambos años)
WITH zonas_validas AS (
  SELECT zona_id
  FROM ventas
  WHERE EXTRACT(YEAR FROM fecha_venta) IN (2024, 2025)
  GROUP BY zona_id
  HAVING COUNT(DISTINCT EXTRACT(YEAR FROM fecha_venta)) = 2
)
SELECT
  v.zona_id,
  EXTRACT(MONTH FROM v.fecha_venta)                          AS mes,
  SUM(CASE WHEN EXTRACT(YEAR FROM fecha_venta) = 2025 THEN total_venta ELSE 0 END) AS ventas_actual,
  SUM(CASE WHEN EXTRACT(YEAR FROM fecha_venta) = 2024 THEN total_venta ELSE 0 END) AS ventas_anterior,
  (SUM(CASE WHEN EXTRACT(YEAR FROM fecha_venta) = 2025 THEN total_venta ELSE 0 END)
   - SUM(CASE WHEN EXTRACT(YEAR FROM fecha_venta) = 2024 THEN total_venta ELSE 0 END))
   / NULLIF(SUM(CASE WHEN EXTRACT(YEAR FROM fecha_venta) = 2024 THEN total_venta ELSE 0 END), 0) * 100 AS lfl_pct
FROM ventas v
JOIN zonas_validas z ON z.zona_id = v.zona_id
GROUP BY 1, 2;
```

**Pitfall crítico:** LFL positivo no implica empresa sana. Una zona puede crecer en ventas mientras su cartera vencida se deteriora. Siempre cruzar LFL con margen bruto y tasa de cobranza.

#### 1.7 Gross Margin Return on Inventory Investment (GMROI)

```
GMROI = Margen Bruto ($) / Costo Promedio de Inventario
```

**Benchmark:** GMROI > 3.0 es el objetivo común citado (qoblex, 2025). Significa que por cada peso invertido en inventario se generan 3+ pesos de margen bruto. **[A MEDIR NOSOTROS]** — para muebles con rotación lenta el GMROI puede ser menor.

### Pitfalls generales

1. **Canibalización de zonas:** cuando se abre una ruta nueva cerca de otra existente, las ventas LFL de la ruta vieja bajan aunque la empresa crezca. Separar siempre ventas totales vs. LFL.
2. **Crédito distorsiona ATV:** un cliente que compra $15,000 a 18 meses parece igual que uno que paga de contado en el ticket, pero el riesgo y el valor presente neto son distintos. Considerar ATV con/sin enganche y ATV por modalidad de pago.
3. **Benchmarks gringos no aplican directo:** los márgenes de furniture retail en USA incluyen costos laborales y de real estate muy distintos a México. Usar como referencia de dirección, no de número exacto.
4. **Margen bruto ≠ rentabilidad real en crédito:** la pérdida por cartera incobrable puede comerse varios puntos de margen. Reportar siempre margen bruto + tasa de mora juntos.

### Fuentes

- [Retail Dogma — Sales Metrics Formulas](https://www.retaildogma.com/sales-metrics/)
- [Cin7 — 27 Essential Retail KPIs](https://www.cin7.com/blog/retail-kpis/)
- [Qoblex — Essential Retail KPIs 2025](https://qoblex.com/blog/essential-retail-kpis-complete-guide-to-measuring-store-performance-in-2025/)
- [Retail Dogma — Same Store Sales](https://www.retaildogma.com/same-store-sales/)
- [Corporate Finance Institute — Same-Store Sales](https://corporatefinanceinstitute.com/resources/accounting/same-store-sales/)
- [Wall Street Prep — Same Store Sales Formula](https://www.wallstreetprep.com/knowledge/same-store-sales/)
- [Shopify — Sell-Through Rate](https://www.shopify.com/blog/sell-through-rate)
- [STORIS — Furniture Retail Metrics & KPIs](https://www.storis.com/educational-content/furniture-retail-metrics-and-kpis/)
- [HFA — KPIs Furniture Retailers Should Focus On](https://myhfa.org/kpis-furniture-retailers-should-focus-on-to-drive-success/)
- [Shopify — Contribution Margin vs Gross Margin](https://www.shopify.com/blog/contribution-margin-vs-gross-margin)
- [Statista — Gross Margin Furniture USA 2022](https://www.statista.com/statistics/199673/share-of-gross-margin-of-furniture-sales-in-us-wholesale-since-1993/)
- [Financial Models Lab — Furniture Store KPIs](https://financialmodelslab.com/blogs/kpi-metrics/furniture-retail)

---

## 2. Análisis de demanda agregada: tendencias y estacionalidad

### Recomendación

Para un retailer con 20 años de historial y sin infraestructura de ML, el stack óptimo es: **Media Móvil** para visualización de tendencia, **Holt-Winters multiplicativo** como modelo de pronóstico operacional, y **Seasonal Naive** como benchmark de comparación. La evidencia empírica de la M4 Competition (100,000 series de tiempo) confirma que los métodos estadísticos clásicos igualan o superan al ML puro en la mayoría de los casos de retail.

### Fórmulas / definiciones

#### 2.1 Media Móvil Simple (MMS)

```
MMA_t = (y_{t} + y_{t-1} + ... + y_{t-n+1}) / n
```

donde `n` es la ventana (e.g., 4 semanas, 12 meses). Elimina ruido aleatorio pero da el mismo peso a datos recientes y antiguos. Adecuada para visualización de tendencia, no para pronóstico.

**En SQL (media móvil de 3 meses):**

```sql
SELECT
  mes,
  zona_id,
  ingresos,
  AVG(ingresos) OVER (
    PARTITION BY zona_id
    ORDER BY mes
    ROWS BETWEEN 2 PRECEDING AND CURRENT ROW
  ) AS mma_3m
FROM ventas_mensuales_por_zona;
```

#### 2.2 Crecimiento YoY y MoM

```
Crecimiento YoY (%) = ((Ventas mes actual - Ventas mismo mes año anterior) / Ventas mismo mes año anterior) × 100
Crecimiento MoM (%) = ((Ventas mes actual - Ventas mes anterior) / Ventas mes anterior) × 100
```

**Pitfall:** MoM en retail es ruidoso por estacionalidad. Siempre reportar YoY como métrica primaria y MoM como secundaria.

#### 2.3 Seasonal Naive — Baseline de referencia

El modelo más simple con estacionalidad: el pronóstico de `t+h` es igual al valor observado en el mismo período del año anterior.

```
ŷ_{t+h} = y_{t+h-m}
```

donde `m` = período estacional (12 para datos mensuales, 52 para semanales). Funciona sorprendentemente bien y sirve como **benchmark obligatorio**: si un modelo más complejo no supera al Seasonal Naive, no vale la pena usarlo.

**En SQL (proyección del mes siguiente usando año anterior):**

```sql
SELECT
  zona_id,
  ADD_MONTHS(mes, 12) AS mes_proyectado,  -- Firebird: DATEADD(MONTH, 12, mes)
  ingresos            AS pronostico_seasonal_naive
FROM ventas_mensuales_por_zona
WHERE mes = DATEADD(MONTH, -12, DATE_TRUNC('month', CURRENT_DATE));
```

#### 2.4 Holt-Winters (Triple Exponential Smoothing) — Modelo operacional recomendado

Maneja simultáneamente nivel, tendencia y estacionalidad. Existen dos variantes:

- **Aditivo:** cuando las fluctuaciones estacionales son de magnitud constante en el tiempo.
- **Multiplicativo:** cuando las fluctuaciones estacionales crecen proporcionalmente con la tendencia. **Recomendado para mueblería** donde las ventas de Buen Fin/Navidad son porcentualmente similares año a año pero nominalmente mayores conforme crece el negocio.

**Ecuaciones del modelo multiplicativo (Holt-Winters):**

Pronóstico:
```
ŷ_{t+h|t} = (ℓ_t + h·b_t) · s_{t+h-m(k+1)}
```

Nivel:
```
ℓ_t = α · (y_t / s_{t-m}) + (1 - α) · (ℓ_{t-1} + b_{t-1})
```

Tendencia:
```
b_t = β* · (ℓ_t - ℓ_{t-1}) + (1 - β*) · b_{t-1}
```

Estacionalidad:
```
s_t = γ · (y_t / (ℓ_{t-1} + b_{t-1})) + (1 - γ) · s_{t-m}
```

Donde:
- `α ∈ (0,1)` — suavizamiento del nivel (valores altos = más reactivo a cambios recientes)
- `β* ∈ (0,1)` — suavizamiento de la tendencia
- `γ ∈ (0,1)` — suavizamiento estacional
- `m` = período estacional (12 para series mensuales)
- `k = ⌊(h-1)/m⌋`

**Implementación práctica:** Holt-Winters no se implementa bien en SQL puro — requiere iteración. Se puede calcular en Go usando una librería como `gonum` o un script Python/R separado que corra como proceso batch y escriba sus salidas a Firebird. Los índices estacionales resultantes (`s_t`) son la parte más valiosa: permiten ajustar YoY para comparar meses sin el efecto de temporada.

#### 2.5 Descomposición STL (Seasonal-Trend Decomposition)

Para exploración y diagnóstico (no para pronóstico operacional), STL descompone una serie en:

```
y_t = T_t + S_t + R_t   (aditivo)
y_t = T_t · S_t · R_t   (multiplicativo)
```

donde `T_t` = tendencia, `S_t` = componente estacional, `R_t` = residual/ruido.

**Cuándo usar STL:** para identificar si la estacionalidad cambió de forma (e.g., ¿el Buen Fin ahora pesa más que antes?), detectar outliers (años con cierres por pandemia, etc.) y validar si el modelo multiplicativo o aditivo es más apropiado. Herramienta exploratoria, no de producción.

#### 2.6 Cuándo lo simple gana al ML

La **M4 Competition** (Makridakis et al., 2019) evaluó 100,000 series de tiempo con 61 métodos. Resultado clave: los métodos de ML puros rindieron **peor** que los estadísticos clásicos (Theta, Exponential Smoothing, promedios de ETS). El ganador fue un híbrido ES-RNN, pero requería infraestructura significativa.

Un estudio en dataset de retail (Lawrence May, 2024) comparó ETS, ARIMA y redes neuronales: **ETS/Holt-Winters ganó** con RMSE 11,610 vs. 14,856 de ARIMA vs. redes neuronales que quedaron por debajo de ambos. El Seasonal Naive superó a las redes neuronales.

**Conclusión:** con 20 años de historia, estacionalidad clara (Buen Fin, Día de Madres, Quincenas de enero) y sin equipo de data science, Holt-Winters multiplicativo es el techo práctico razonable. Superar ese techo requiere datos de panel (múltiples SKUs simultáneos), no solo más complejidad de modelo.

### Pitfalls

1. **Estacionalidad de crédito vs. de demanda:** en mueblería con crédito, las ventas de enero pueden dispararse por quincenas o por programas de pago de contado. Esto distorsiona la estacionalidad "real" de demanda. Separar análisis por modalidad de pago cuando sea posible.
2. **Outliers estructurales:** los años 2020–2021 (COVID) distorsionan los índices estacionales si se incluyen naively. Identificar y "winsorizar" o excluir antes de ajustar el modelo.
3. **Media móvil introduce lag:** una MMA-12 le da peso igual al mes de hace 11 meses que al mes pasado. Para detectar cambios de tendencia, preferir Exponential Smoothing (SES o Holt) que da más peso a lo reciente.
4. **No confundir forecast de unidades con forecast de pesos:** en un entorno inflacionario (México), el pronóstico en pesos corrientes mezcla inflación con demanda real. Deflactar o trabajar en unidades cuando el objetivo es analizar demanda física.

### Fuentes

- [Holt-Winters Seasonal Method — OTexts Forecasting P&P](https://otexts.com/fpp2/holt-winters.html)
- [M4 Competition — ScienceDirect](https://www.sciencedirect.com/science/article/pii/S0169207019301128)
- [Comparing ARIMA, ETS, Neural Networks on Retail — Lawrence May, Medium](https://medium.com/@lawrence.may/comparing-advanced-forecasting-techniques-arima-ets-and-neural-networks-on-a-retail-sales-75fdc6a8baa6)
- [Exponential Smoothing Overview — ScienceDirect](https://www.sciencedirect.com/topics/social-sciences/exponential-smoothing)
- [Assessing Sales Forecasting Methods 2026 — Wiley](https://onlinelibrary.wiley.com/doi/10.1155/acis/6686245)
- [Mathematics of Demand Forecasting — Pardovicki, Medium](https://medium.com/@j.pardovicki_49040/module-ii-the-mathematics-of-demand-forecasting-00550dd661db)
- [Absence of Evidence for Pure ML in Time Series — magsv.se](https://magsv.se/2020-04-06-M4Competition-results/)

---

## 3. Análisis de cohortes y retención a nivel negocio

### Recomendación

El análisis de cohortes por mes de primera compra es la herramienta más poderosa para entender la calidad de los clientes adquiridos en cada período. Para mueblería a crédito con ciclo de recompra largo (18–30 meses), los intervalos de análisis deben ser **trimestrales o semestrales** — no mensuales como en DTC de consumibles. La métrica clave no es la tasa de retención mensual sino la **tasa de recompra a 12/24/36 meses**.

### Fórmulas / definiciones

#### 3.1 Definición de cohorte de adquisición

Una cohorte es el grupo de clientes cuya **primera compra** ocurrió en un mes/trimestre determinado.

```sql
-- Tabla de cohortes: cliente → mes de primera compra
CREATE VIEW cohortes_clientes AS
SELECT
  cliente_id,
  DATE_TRUNC('month', MIN(fecha_venta)) AS mes_adquisicion
FROM ventas
GROUP BY cliente_id;
```

#### 3.2 Retention Rate (Tasa de Retención) por cohorte

```
Retention Rate_{cohort, t} = (Clientes de esa cohorte que compraron en el período t) / (Tamaño original de la cohorte) × 100
```

```sql
-- Matriz de retención: cohorte × intervalos de N meses desde primera compra
WITH cohortes AS (
  SELECT
    cliente_id,
    DATE_TRUNC('month', MIN(fecha_venta)) AS mes_adquisicion
  FROM ventas GROUP BY cliente_id
),
actividad AS (
  SELECT
    c.cliente_id,
    c.mes_adquisicion,
    DATE_TRUNC('month', v.fecha_venta)                                   AS mes_venta,
    DATEDIFF(MONTH, c.mes_adquisicion, DATE_TRUNC('month', v.fecha_venta)) AS meses_desde_adq
    -- Firebird: usa DATEDIFF(MONTH, ...) o expresion equivalente
  FROM cohortes c
  JOIN ventas v ON v.cliente_id = c.cliente_id
)
SELECT
  mes_adquisicion,
  COUNT(DISTINCT CASE WHEN meses_desde_adq = 0  THEN cliente_id END) AS m0,  -- tamaño cohorte
  COUNT(DISTINCT CASE WHEN meses_desde_adq = 6  THEN cliente_id END) AS m6,
  COUNT(DISTINCT CASE WHEN meses_desde_adq = 12 THEN cliente_id END) AS m12,
  COUNT(DISTINCT CASE WHEN meses_desde_adq = 24 THEN cliente_id END) AS m24,
  COUNT(DISTINCT CASE WHEN meses_desde_adq = 36 THEN cliente_id END) AS m36
FROM actividad
GROUP BY mes_adquisicion
ORDER BY mes_adquisicion;
```

Para obtener el porcentaje: dividir cada columna `m6`/`m12`/`m24` entre `m0`.

#### 3.3 Curva de recompra (Repurchase Curve)

La curva de recompra muestra, para cada cohorte, el porcentaje acumulado de clientes que realizaron al menos una segunda compra dentro de N meses.

```
Repurchase Rate a N meses = (Clientes de la cohorte con ≥2 compras en primeros N meses) / Tamaño cohorte × 100
```

**Benchmark verificado:** La tasa de repeat purchase anual en furniture hovers bajo 15% (mobiloud/byradiant, 2026). Una mueblería con 20% de recompra anual está por encima del promedio. El ciclo esperado de recompra es 18–30 meses para muebles de sala/recámara principales.

**[A MEDIR NOSOTROS]:** calcular la curva de recompra real con los 20 años de historia para determinar:
- ¿Cuál es nuestro M12, M24, M36 repurchase rate histórico?
- ¿Qué cohortes (años de adquisición) son más valiosas?
- ¿El ciclo de recompra ha cambiado en los últimos 5 años?

#### 3.4 Revenue Cohort (Ingresos por cohorte)

Extiende la tabla de retención a pesos en lugar de clientes:

```sql
SELECT
  c.mes_adquisicion,
  SUM(CASE WHEN meses_desde_adq = 0  THEN v.total_venta ELSE 0 END) AS ingresos_m0,
  SUM(CASE WHEN meses_desde_adq = 12 THEN v.total_venta ELSE 0 END) AS ingresos_m12,
  SUM(CASE WHEN meses_desde_adq = 24 THEN v.total_venta ELSE 0 END) AS ingresos_m24,
  SUM(CASE WHEN meses_desde_adq = 36 THEN v.total_venta ELSE 0 END) AS ingresos_m36
FROM actividad a
JOIN ventas v ON v.cliente_id = a.cliente_id
  AND DATE_TRUNC('month', v.fecha_venta) = DATEADD(MONTH, a.meses_desde_adq, a.mes_adquisicion)
JOIN cohortes c ON c.cliente_id = a.cliente_id
GROUP BY c.mes_adquisicion;
```

**Lectura de la tabla revenue cohort:**
- **Horizontal (fila):** cómo evoluciona el valor de una cohorte a lo largo del tiempo. Una cohorte que genera más ingresos en M24 que en M12 indica que los clientes están comprando más conforme el crédito se liquida.
- **Vertical (columna M12):** comparar el valor a 12 meses entre cohortes de diferentes años — ¿estamos adquiriendo clientes mejores o peores con el tiempo?
- **Diagonal:** mismo período calendario comparando cohortes distintas.

#### 3.5 ARPU por cohorte (Average Revenue Per User)

```
ARPU_{cohort, t} = Ingresos totales de la cohorte en el período t / Tamaño original de la cohorte
```

Permite detectar si el ticket promedio de recompra creció o bajó respecto a la primera compra.

#### 3.6 Ajuste para crédito a abonos (específico mueblería)

En mueblería con crédito propio, una "compra" puede extenderse 12–24 meses en abonos. Definir claramente qué cuenta como evento de compra para cohortes:

- **Opción A — Fecha de la venta (recomendada):** se registra cuando el contrato se firma, independientemente de los pagos.
- **Opción B — Fecha del último abono:** distorsiona el análisis porque mezcla pagos de contratos viejos con actividad nueva.

**Recomendación:** usar fecha de la venta (folio) para cohortes de adquisición. Los abonos son eventos de cobranza, no de demanda.

**Segundo efecto crítico:** un cliente puede estar en medio de un plan de 18 meses y ser clasificado como "no recompró" cuando en realidad sigue activo como deudor. Distinguir:
- **Clientes activos con crédito vigente** (no son churned, solo no han terminado de pagar la primera compra)
- **Clientes con crédito liquidado** (elegibles para recompra real)

Para análisis de retención real en crédito, filtrar cohortes que hayan completado su primer ciclo de pago.

### Pitfalls

1. **Confundir cobranza con demanda:** si un cliente paga su última cuota en mes 18 y compra nuevamente en mes 20, el gap "real" de inactividad es solo 2 meses, no 20.
2. **Cohortes pequeñas dan ruido estadístico:** si una cohorte mensual tiene <30 clientes, el análisis es inestable. Agrupar a nivel trimestral o semestral para cohortes antiguas.
3. **El benchmark del 15% annual repeat furniture** viene de retailers en mercados sin crédito propio. En mueblería con crédito, la relación es continua por la duración del crédito — la recompra real solo puede medirse después de que el crédito original se liquida.
4. **No usar retention rate mensual estándar (SaaS):** en muebles, un cliente que no compró en el mes 7 no está churned — simplemente no necesita otro mueble todavía. Usar ventanas de 12/24/36 meses, no 30 días.
5. **Descuentos distorsionan el ingreso de la cohorte:** las cohortes adquiridas en períodos de promoción agresiva (Buen Fin) pueden mostrar ingresos M0 altos pero márgenes bajos. Trackear margen bruto por cohorte, no solo ingresos.

### Fuentes

- [Peel Insights — Guide to Cohort Analysis](https://www.peelinsights.com/post/your-guide-to-cohort-analysis)
- [Saras Analytics — Cohort Retention Analysis](https://www.sarasanalytics.com/blog/cohort-retention-analysis)
- [Count.co — Cohort Retention Analysis](https://count.co/metric/cohort-retention-analysis)
- [Medium — Cohort Analysis in SQL (Lateefat Oyebola)](https://medium.com/@meetlateefaah/an-in-depth-guide-to-cohort-analysis-in-sql-4c1d110efa5a)
- [Medium — Cohort Retention Analysis in SQL (Tyran Christian)](https://medium.com/@beananalytics_/cohort-retention-analysis-c058faddd764)
- [Amplitude — Cohorts to Improve Retention](https://amplitude.com/blog/cohorts-to-improve-your-retention)
- [mobiloud — Repeat Customer Rate Benchmarks 2026](https://www.mobiloud.com/blog/repeat-customer-rate-ecommerce)
- [byradiant — Repeat Purchase Rate](https://byradiant.com/blog/repeat-purchase-rate)
- [Porch Group Media — Furniture Customer Acquisition Strategies](https://porchgroupmedia.com/blog/top-furniture-customer-acquisition-strategies-to-boost-sales-and-brand-loyalty/)
- [O'Reilly — Cohort Analysis SQL for Data Analysis](https://www.oreilly.com/library/view/sql-for-data/9781492088776/ch04.html)

---

*Generado: 2026-06-13. Documento de investigación interna — no publicar.*
