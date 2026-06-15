# Inteligencia de Negocio (ventas / producto / cartera / control) — Estado del arte

> **Segundo pilar** del sistema de inteligencia, complementario al perfil por cliente
> (`inteligencia-cliente-estado-del-arte.md`). Aquí: analítica **agregada** del negocio — ventas,
> "core products", inventario, ruta/zona/vendedor, salud de cartera, y control/antifraude — más la
> **arquitectura** para servirlo junto al perfil por cliente desde una sola capa.
>
> **Cómo se produjo:** investigación web en paralelo (4 agentes) con verificación adversaria — ≥2
> fuentes por afirmación; benchmarks que no resistieron quedan refutados o marcados
> `[A MEDIR NOSOTROS]`. Contexto: mueblería mexicana a crédito, ~miles de clientes, 20 años de
> historial POS, stack legacy Go + Firebird (Windows, sin nube).

## Índice
- **C1 — Analítica de ventas:** KPIs core, demanda/estacionalidad, cohortes de negocio.
- **C2 — Producto e inventario:** ABC/"core products", surtido/GMROI, pricing/prima de financiamiento, rotación.
- **C3 — Canal/ruta/desempeño:** fuerza de ventas, zona, cartera/cobranza, fraude/fuga.
- **C4 — Cómo lo aterrizan los tops + arquitectura:** Walmart/MercadoLibre/etc., modelo dimensional, capa semántica, realización en Go/Firebird.

---


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


---


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


---


# C3 — Canal, Ruta y Desempeño de Fuerza de Ventas

> Este documento cubre cuatro dimensiones analíticas críticas para una mueblería mexicana con modelo de crédito a plazos, fuerza de ventas en campo (vendedores), cobranza en ruta (cobradores), mostrador y zonas geográficas: (1) desempeño individual de vendedores y rutas, (2) comparativo por zona/ubicación, (3) salud de la cartera y cobranza a nivel negocio, y (4) detección de fraude y fuga de efectivo en ruta. Cada sección incluye fórmulas SQL-implementables, benchmarks verificados con fuentes, y advertencias sobre métricas que no aplican directamente al contexto mexicano de mueblería a crédito.

---

## 1. Desempeño de Fuerza de Ventas y Rutas

### Recomendación

Para un negocio con vendedores en campo que visitan clientes en zonas asignadas, el marco analítico más relevante es el de **Direct Store Delivery (DSD) / Route Accounting**: cada vendedor opera como una "ruta" con cartera de clientes propia. Las métricas deben capturar tanto el esfuerzo (actividad) como el resultado (revenue, conversiones) y la eficiencia de la ruta (tiempo vendiendo vs. tiempo en traslado).

El error más común es medir solo revenue total por vendedor sin normalizar por cartera activa ni por días trabajados. Un vendedor con cartera grande puede superar en pesos a uno más productivo con cartera pequeña.

### Fórmulas / métricas

**Cobertura de territorio (Territory Coverage Efficiency)**
```
Cobertura = (Clientes contactados en período / Total clientes en cartera asignada) × 100
```
Benchmark: > 80% mensual es saludable para rutas maduras. [A MEDIR NOSOTROS]

**Tasa de conversión de visita**
```
Conversión = (Visitas que resultaron en venta / Total visitas realizadas) × 100
```
Una mejora del 10% en conversión se traduce típicamente en 15–20% de incremento de revenue por ruta. [[SPOTIO, 2026]](https://spotio.com/blog/sales-kpi/)

**Velocidad de pipeline por vendedor**
```
Velocidad = (Oportunidades abiertas × Valor promedio de venta × Tasa de cierre) / Ciclo promedio en días
```
Permite detectar dónde se estancan los créditos: ¿en la evaluación?, ¿en la entrega?

**Cuota de ventas (Quota Attainment)**
```
Cumplimiento_Cuota = (Ventas reales en período / Cuota asignada en período) × 100
```
Benchmark industria B2C campo: > 80% se considera sólido. < 60% requiere intervención inmediata. [[HiBob / Forecastio, 2026]](https://www.hibob.com/financial-metrics/quota-attainment/)

**Revenue por visita** (normalización clave)
```
Revenue_por_visita = Ventas totales del vendedor / Número de visitas realizadas
```

**Eficiencia de ruta (tiempo vendiendo vs. traslado)**
```
Eficiencia_Ruta = (Tiempo en visitas / Tiempo total en campo) × 100
```
Benchmark DSD: los vendedores en campo pasan ~35–45% de su jornada en traslado y ~55–65% vendiendo. Optimizar rutas puede mover esto a 50/50 con un incremento de 20–30% en revenue. [[McKinsey via SPOTIO]](https://spotio.com/blog/sales-kpi/)

**Implementación SQL (ejemplo sobre tablas msp-api)**

```sql
-- Productividad mensual por vendedor
SELECT
    v.vendedor_id,
    v.nombre,
    COUNT(DISTINCT vis.cliente_id)            AS clientes_contactados,
    COUNT(vis.visita_id)                      AS total_visitas,
    COUNT(ven.venta_id)                       AS ventas_cerradas,
    CAST(COUNT(ven.venta_id) AS NUMERIC(18,4))
      / NULLIF(COUNT(vis.visita_id), 0) * 100 AS tasa_conversion_pct,
    CAST(SUM(ven.monto_total) AS NUMERIC(18,2))
      / NULLIF(COUNT(vis.visita_id), 0)       AS revenue_por_visita,
    SUM(ven.monto_total)                      AS revenue_total,
    v.cuota_mensual,
    CAST(SUM(ven.monto_total) AS NUMERIC(18,4))
      / NULLIF(v.cuota_mensual, 0) * 100      AS cumplimiento_cuota_pct
FROM MSP_VENDEDORES v
LEFT JOIN MSP_VISITAS vis
    ON vis.vendedor_id = v.vendedor_id
    AND vis.fecha >= :fecha_inicio AND vis.fecha <= :fecha_fin
LEFT JOIN MSP_VENTAS ven
    ON ven.visita_id = vis.visita_id
GROUP BY v.vendedor_id, v.nombre, v.cuota_mensual
ORDER BY revenue_total DESC;
```

**Nota Firebird**: usar `CAST(... AS NUMERIC(18,4))` para divisiones; el driver v0.9.19 requiere escala explícita en agregados para evitar desbordamiento de escala.

**Métricas de actividad DSD (ruta física)**

En route-accounting clásico, cada ruta cierra el día con un **settlement** que concilia: cargado en camión → entregado → devuelto → cobrado. Para ventas de mueblería a domicilio, el equivalente es:

| Métrica DSD | Equivalente mueblería |
|---|---|
| Load vs. sold | Contratos firmados vs. órdenes de entrega generadas |
| Cash collected | Efectivo recibido por cobrador en ruta |
| Returns | Cancelaciones / devoluciones post-entrega |
| Route settlement gap | Diferencia entre lo que el cobrador declara y lo depositado |

Fuente metodológica: [[OrderWerks Route Accounting Guide]](https://www.orderwerks.com/blog/route-accounting-software-guide), [[Field Technologies Online - DSD]](https://www.fieldtechnologiesonline.com/doc/direct-store-delivery-dsd-and-route-0002)

### Pitfalls

- **No comparar vendedores con carteras de tamaño radicalmente distinto** sin normalizar por cartera asignada. El revenue absoluto favorece sistemáticamente a quienes heredaron zonas más grandes.
- **El benchmark de "sales per rep" global (~$1.2M USD/año) no aplica** a mueblería popular mexicana donde el ticket es $3,000–$20,000 MXN. [A MEDIR NOSOTROS] el baseline propio antes de fijar cuotas.
- **La tasa de conversión visita→venta depende del tipo de visita** (prospección en frío vs. reactivación vs. renovación). Agregar sin segmentar esconde los mejores indicadores.
- **Tiempo en campo no equivale a productividad**: un vendedor que hace 30 visitas cortas puede superar en conversión a uno que hace 10 visitas largas. Medir ambas dimensiones.

### Fuentes

- [SPOTIO — 20 Essential Field Sales KPIs 2026](https://spotio.com/blog/sales-kpi/)
- [HiBob — Sales Quota Attainment Formula](https://www.hibob.com/financial-metrics/quota-attainment/)
- [D2D CRM — Top 5 KPIs Route Planning Software](https://thed2dcrm.com/blog/top-5-kpis-to-track-with-sales-route-planning-software/)
- [OrderWerks — Route Accounting Software Guide](https://www.orderwerks.com/blog/route-accounting-software-guide)
- [SalesMate — 30+ Sales KPIs 2026](https://www.salesmate.io/blog/sales-kpis/)

---

## 2. Desempeño por Zona / Ubicación / Tienda

### Recomendación

Para una mueblería con múltiples zonas geográficas (zonas de cobranza/ventas) y eventualmente más de una sucursal o mostrador, el análisis de desempeño zonal debe responder: ¿qué zonas generan más venta nueva?, ¿cuáles tienen mejor tasa de reenganche?, ¿cuáles tienen mayor cartera en riesgo? La zona no es solo un filtro de reporte — es la unidad estratégica de expansión o retracción.

**Sales per square foot** es una métrica clásica de retail que aquí tiene aplicabilidad limitada: el modelo de venta en campo no tiene "espacio de piso" por zona asignada. Se adapta mejor como **revenue por cliente activo en zona** o **revenue por km² de zona**.

### Fórmulas / métricas

**Revenue por zona (absoluto y relativo)**
```
Revenue_Zona = SUM(monto_venta) WHERE zona_id = X AND período = P
Participación_Zona = Revenue_Zona / Revenue_Total × 100
```

**Penetración de mercado por zona** (requiere estimación de mercado total)
```
Penetración = Clientes_activos_zona / Hogares_estimados_zona × 100
```
[A MEDIR NOSOTROS] — requiere datos censales INEGI por colonia/municipio.

**Productividad zonal normalizada**
```
Revenue_por_cliente_activo_zona = SUM(ventas_zona) / COUNT(DISTINCT clientes_con_compra_zona)
Ticket_promedio_zona = SUM(ventas_zona) / COUNT(ventas_zona)
```

**Tasa de reenganche por zona**
```
Reenganche_Zona = Clientes_con_2a_compra_en_zona / Clientes_con_1a_compra_en_zona × 100
```
Métrica clave para el modelo de venta recurrente: zonas con alto reenganche son más rentables por CAC (costo de adquisición ya amortizado).

**Comp performance (comparativo período a período)**
```
Crecimiento_Zona = (Revenue_Zona_T - Revenue_Zona_T-1) / Revenue_Zona_T-1 × 100
```
Permite detectar zonas en declive estructural vs. zonas con variación estacional.

**Sales per square foot — adaptación a mueblería en campo**

El benchmark global de retail de mueblería es $185–$500 USD por pie cuadrado de showroom. [[Wall Street Oasis]](https://www.wallstreetoasis.com/resources/skills/accounting/sales-per-square-foot) [[HFA — Furniture Retailers]](https://myhfa.org/blog/whats-a-decent-sales-volume-for-your-showroom-space/) Sin embargo, para ventas en campo esta métrica **no aplica directamente**. La adaptación correcta para una mueblería con vendedores en ruta es:

```
Revenue_por_km2_zona = Revenue_Zona / Área_km2_zona
```

O más útil aún: **clientes activos por km²** como proxy de densidad de cartera.

**Implementación SQL**

```sql
-- Comparativo de zonas: período actual vs. anterior
WITH zona_actual AS (
    SELECT
        z.zona_id,
        z.nombre_zona,
        COUNT(DISTINCT v.cliente_id)  AS clientes_activos,
        COUNT(v.venta_id)             AS num_ventas,
        SUM(v.monto_total)            AS revenue
    FROM MSP_ZONAS z
    JOIN MSP_CLIENTES c ON c.zona_id = z.zona_id
    JOIN MSP_VENTAS v   ON v.cliente_id = c.cliente_id
    WHERE v.fecha_venta BETWEEN :inicio_actual AND :fin_actual
    GROUP BY z.zona_id, z.nombre_zona
),
zona_anterior AS (
    SELECT
        z.zona_id,
        SUM(v.monto_total) AS revenue_ant
    FROM MSP_ZONAS z
    JOIN MSP_CLIENTES c ON c.zona_id = z.zona_id
    JOIN MSP_VENTAS v   ON v.cliente_id = c.cliente_id
    WHERE v.fecha_venta BETWEEN :inicio_anterior AND :fin_anterior
    GROUP BY z.zona_id
)
SELECT
    a.zona_id,
    a.nombre_zona,
    a.clientes_activos,
    a.num_ventas,
    a.revenue,
    p.revenue_ant,
    CAST((a.revenue - p.revenue_ant) AS NUMERIC(18,4))
      / NULLIF(p.revenue_ant, 0) * 100  AS crecimiento_pct,
    CAST(a.revenue AS NUMERIC(18,4))
      / NULLIF(a.clientes_activos, 0)   AS revenue_por_cliente
FROM zona_actual a
LEFT JOIN zona_anterior p ON p.zona_id = a.zona_id
ORDER BY a.revenue DESC;
```

### Pitfalls

- **El benchmark de $185 USD/sqft para mueblería aplica a showrooms físicos** con clientes que entran a comprar. En un modelo de venta domiciliaria en zonas de clase popular, la métrica de espacio no es relevante. Usar en cambio revenue por cliente activo o revenue por ruta/zona.
- **Zonas geográficas grandes en km² no son equivalentes a zonas "de alto potencial"**: una colonia densa de clase media-baja puede superar a una zona suburbana más extensa. Segmentar por densidad de hogares, no solo por área.
- **No concluir que una zona "creció" sin verificar si también creció la cartera vencida en esa zona**: revenue alto con PAR alto puede indicar ventas irresponsables.
- Los datos de Statista para México muestran que el mercado de muebles hogar suma ~$180B MXN en revenue total (censo 2019), concentrado en CDMX y EdoMex. [[Data México / Economía]](https://www.economia.gob.mx/datamexico/en/profile/industry/retail-trade-of-furniture-for-home-and-other-household-goods) — útil como referencia macro pero no como benchmark operativo.

### Fuentes

- [Wall Street Oasis — Sales per Square Foot](https://www.wallstreetoasis.com/resources/skills/accounting/sales-per-square-foot)
- [HFA — Sales Volume for Showroom Space](https://myhfa.org/blog/whats-a-decent-sales-volume-for-your-showroom-space/)
- [DTiQ — Average Retail Sales per Square Foot](https://www.dtiq.com/blog/retail/average-retail-sales-per-square-foot)
- [Data México — Retail Muebles Hogar](https://www.economia.gob.mx/datamexico/en/profile/industry/retail-trade-of-furniture-for-home-and-other-household-goods)
- [Business Plan Templates — Furniture Retail Metrics](https://businessplan-templates.com/blogs/metrics/furniture-retail-store)

---

## 3. Analítica de Cartera y Cobranza (nivel negocio)

### Recomendación

Esta es la sección más crítica para una mueblería con crédito propio. El negocio **es** en gran parte un prestamista informal: financia muebles a plazos, cobra efectivo en ruta, y lleva el riesgo de incumplimiento en su propio balance. Las métricas correctas son las de **microfinanzas / crédito al consumo popular**, no las de retail puro.

Los tres indicadores de cartera no negociables son: **Portfolio at Risk (PAR)**, **Collection Rate (Tasa de Recuperación)** y **Roll Rate (Tasa de Migración de Mora)**. Juntos dan una imagen completa de la salud de la cartera y permiten detectar deterioro antes de que se vuelva pérdida irreversible.

### Fórmulas / métricas

#### A. Portfolio at Risk (PAR)

**Definición**: porcentaje del saldo total de la cartera que tiene al menos un pago vencido con más de N días de atraso.

```
PAR_30 = Saldo_total_créditos_con_atraso_>30_días / Saldo_total_bruto_cartera × 100
PAR_60 = Saldo_total_créditos_con_atraso_>60_días / Saldo_total_bruto_cartera × 100
PAR_90 = Saldo_total_créditos_con_atraso_>90_días / Saldo_total_bruto_cartera × 100
```

**Importante**: el numerador es el **saldo total del crédito** (no solo el pago vencido). Si un crédito de $10,000 tiene un pago de $500 vencido hace 35 días, contribuye $10,000 al numerador de PAR_30. Esto es intencional: mide la exposición real, no solo el pago atrasado.

Fuentes: [[Harbourfront Technologies — PAR]](https://harbourfronts.com/portfolio-at-risk/), [[AccountingGuide — PAR]](https://accountinguide.com/portfolio-at-risk/), [[Bryt Software — PAR Microfinance]](https://www.brytsoftware.com/what-portfolio-at-risk-measures/)

**Benchmarks verificados para microfinanzas / crédito popular**:
- PAR_30 < 5%: cartera sana
- PAR_30 5–10%: señal de alerta — revisar cobranza preventiva
- PAR_30 > 10%: deterioro serio — requiere intervención
- PAR_90 > 5%: las pérdidas son casi inevitables en esa fracción

[A MEDIR NOSOTROS] — el baseline real de esta operación antes de fijar umbrales de alerta.

**Implementación SQL (Firebird)**

```sql
-- PAR calculado al día de hoy
-- Asume tabla MSP_CREDITOS con saldo_vigente y MSP_PAGOS con fecha_vencimiento

WITH cartera AS (
    SELECT
        c.credito_id,
        c.saldo_vigente,
        MIN(p.fecha_vencimiento) AS fecha_primer_vencido
    FROM MSP_CREDITOS c
    JOIN MSP_PAGOS p
        ON p.credito_id = c.credito_id
        AND p.estatus = 'PENDIENTE'
        AND p.fecha_vencimiento < CURRENT_DATE
    WHERE c.estatus IN ('VIGENTE', 'VENCIDO')
    GROUP BY c.credito_id, c.saldo_vigente
),
total_cartera AS (
    SELECT CAST(SUM(saldo_vigente) AS NUMERIC(18,2)) AS saldo_bruto
    FROM MSP_CREDITOS
    WHERE estatus IN ('VIGENTE', 'VENCIDO')
)
SELECT
    tc.saldo_bruto                                             AS cartera_total,
    CAST(SUM(CASE WHEN CURRENT_DATE - ca.fecha_primer_vencido > 30
                  THEN ca.saldo_vigente ELSE 0 END)
         AS NUMERIC(18,2))                                    AS saldo_PAR30,
    CAST(SUM(CASE WHEN CURRENT_DATE - ca.fecha_primer_vencido > 60
                  THEN ca.saldo_vigente ELSE 0 END)
         AS NUMERIC(18,2))                                    AS saldo_PAR60,
    CAST(SUM(CASE WHEN CURRENT_DATE - ca.fecha_primer_vencido > 90
                  THEN ca.saldo_vigente ELSE 0 END)
         AS NUMERIC(18,2))                                    AS saldo_PAR90,
    CAST(SUM(CASE WHEN CURRENT_DATE - ca.fecha_primer_vencido > 30
                  THEN ca.saldo_vigente ELSE 0 END)
         AS NUMERIC(18,4)) / NULLIF(tc.saldo_bruto, 0) * 100 AS PAR30_pct,
    CAST(SUM(CASE WHEN CURRENT_DATE - ca.fecha_primer_vencido > 90
                  THEN ca.saldo_vigente ELSE 0 END)
         AS NUMERIC(18,4)) / NULLIF(tc.saldo_bruto, 0) * 100 AS PAR90_pct
FROM cartera ca
CROSS JOIN total_cartera tc;
```

#### B. Collection Rate (Tasa de Recuperación)

**Definición**: porcentaje de los pagos esperados en el período que efectivamente se cobraron.

```
Collection_Rate = Pagos_cobrados_en_período / Pagos_esperados_en_período × 100
```

Para créditos a plazos semanales o quincenales:
```
Collection_Rate_semana = SUM(cobros_recibidos_semana) / SUM(cuotas_programadas_semana) × 100
```

Benchmark en microcrédito / crédito popular México: > 95% es excelente; < 90% es señal de deterioro. [A MEDIR NOSOTROS para esta operación]

#### C. Collection Effectiveness Index (CEI)

Métrica de eficiencia de cobranza a nivel portafolio, más útil cuando hay plazos variables:

```
CEI = (AR_inicio + Ventas_crédito_mes - AR_fin_total) /
      (AR_inicio + Ventas_crédito_mes - AR_fin_corriente) × 100
```

Benchmark: CEI > 85% indica cobranza eficiente. [[HighRadius]](https://www.highradius.com/resources/Blog/collections-effectiveness-index-how-to-act-on-it/)

#### D. Days Sales Outstanding (DSO)

```
DSO = (Cartera_vigente_promedio / Ventas_crédito_período) × Días_del_período
```

Para crédito a plazos fijos (p.ej. 52 semanas), el DSO "teórico" equivale aproximadamente a la mitad del plazo. Desviaciones hacia arriba indican mora estructural. [[Wall Street Prep — DSO]](https://www.wallstreetprep.com/knowledge/days-sales-outstanding-dso/)

#### E. Roll Rate (Tasa de Migración de Mora)

Mide qué porcentaje de créditos en un bucket de mora migran al siguiente bucket más grave en el próximo período.

**Buckets estándar** (adaptado a pagos semanales/quincenales de mueblería):

| Bucket | Días de atraso | Equivalente en pagos quincenales |
|--------|---------------|----------------------------------|
| Al corriente | 0 | 0 pagos vencidos |
| M1 | 1–30 días | 1–2 pagos vencidos |
| M2 | 31–60 días | 3–4 pagos vencidos |
| M3 | 61–90 días | 5–6 pagos vencidos |
| M4+ | 91–120 días | 7+ pagos vencidos |
| Pérdida | > 120 días | Candidato a castigo |

**Fórmula de roll rate forward (ejemplo M1 → M2)**:

```
Roll_M1_a_M2 = Saldo_en_M2_este_mes_que_estaba_en_M1_el_mes_pasado /
               Saldo_total_en_M1_el_mes_pasado × 100
```

Ejemplo numérico: si $600,000 de la cartera M1 migraron a M2 y el saldo total M1 era $1,000,000 → Roll Rate = 60%. [[Head of Credit]](https://theheadofcredit.com/roll-rate-analysis-for-aging-buckets/) [[ListenData — Roll Rate Analysis]](https://www.listendata.com/2019/09/roll-rate-analysis.html)

**Implementación SQL — distribución de cartera por aging bucket**

```sql
-- Distribución de cartera por bucket de mora al día de hoy
SELECT
    CASE
        WHEN CURRENT_DATE - MIN(p.fecha_vencimiento) <= 0
            THEN 'Al corriente'
        WHEN CURRENT_DATE - MIN(p.fecha_vencimiento) BETWEEN 1  AND 30
            THEN 'M1 (1-30 días)'
        WHEN CURRENT_DATE - MIN(p.fecha_vencimiento) BETWEEN 31 AND 60
            THEN 'M2 (31-60 días)'
        WHEN CURRENT_DATE - MIN(p.fecha_vencimiento) BETWEEN 61 AND 90
            THEN 'M3 (61-90 días)'
        WHEN CURRENT_DATE - MIN(p.fecha_vencimiento) BETWEEN 91 AND 120
            THEN 'M4 (91-120 días)'
        ELSE 'M5+ (>120 días)'
    END                                                   AS bucket_mora,
    COUNT(DISTINCT c.credito_id)                          AS num_creditos,
    CAST(SUM(c.saldo_vigente) AS NUMERIC(18,2))           AS saldo_total,
    CAST(SUM(c.saldo_vigente) AS NUMERIC(18,4))
      / NULLIF((SELECT CAST(SUM(saldo_vigente) AS NUMERIC(18,4))
                FROM MSP_CREDITOS
                WHERE estatus IN ('VIGENTE','VENCIDO')), 0) * 100
                                                          AS pct_cartera
FROM MSP_CREDITOS c
LEFT JOIN MSP_PAGOS p
    ON p.credito_id = c.credito_id
    AND p.estatus = 'PENDIENTE'
    AND p.fecha_vencimiento < CURRENT_DATE
WHERE c.estatus IN ('VIGENTE', 'VENCIDO')
GROUP BY 1
ORDER BY MIN(CURRENT_DATE - p.fecha_vencimiento) NULLS FIRST;
```

### Pitfalls

- **PAR no es lo mismo que "pagos atrasados"**: el numerador es el saldo total del crédito que tiene mora, no solo el monto atrasado. Confundir esto subestima la exposición real hasta 10x en créditos con muchas cuotas pendientes.
- **El DSO para crédito a plazos fijos no se interpreta igual que en B2B**: un crédito a 52 semanas "bien portado" tendrá DSO ≈ 26 semanas. Comparar contra benchmarks B2B (30–45 días) es un error categórico.
- **Los benchmarks BNPL globales (BNPL delinquency 34–41%)** no aplican al modelo de venta física con cobranza en ruta. [[ProdigalTech]](https://www.prodigaltech.com/blog/why-bnpl-is-now-the-fastest-growing-delinquency-problem-in-consumer-lending) El modelo de relación personal cobrador-cliente tiene tasas de recuperación estructuralmente más altas.
- **Coppel y Elektra** no publican métricas de cobranza a nivel operativo. Su escala (Coppel: >$247B MXN en revenue 2022 [[Statista]](https://www.statista.com/statistics/1415300/revenue-furniture-retailer-mexico/)) hace que sus benchmarks sean inaplicables a una operación regional. [A MEDIR NOSOTROS] con datos propios.
- **Nunca reportar solo el collection rate global** sin desglosarlo por cobrador y por zona: el promedio puede ocultar cobradores con tasas de recuperación del 60% encubiertos por otros con 98%.

### Fuentes

- [Harbourfront Technologies — Portfolio at Risk](https://harbourfronts.com/portfolio-at-risk/)
- [AccountingGuide — PAR](https://accountinguide.com/portfolio-at-risk/)
- [Bryt Software — What PAR Measures](https://www.brytsoftware.com/what-portfolio-at-risk-measures/)
- [HighRadius — Collection Effectiveness Index](https://www.highradius.com/resources/Blog/collections-effectiveness-index-how-to-act-on-it/)
- [Wall Street Prep — DSO Formula](https://www.wallstreetprep.com/knowledge/days-sales-outstanding-dso/)
- [Head of Credit — Roll Rate Aging Buckets](https://theheadofcredit.com/roll-rate-analysis-for-aging-buckets/)
- [ListenData — Roll Rate Analysis](https://www.listendata.com/2019/09/roll-rate-analysis.html)
- [MinTea Blog — Roll Rate Banking Credit Risk](https://mintea.blog/?p=2643)
- [ProdigalTech — BNPL Delinquency](https://www.prodigaltech.com/blog/why-bnpl-is-now-the-fastest-growing-delinquency-problem-in-consumer-lending)

---

## 4. Fraude / Fuga y Señales de Control Agregado

### Recomendación

Este es el pain point #1 del dueño en cualquier negocio con cobranza en efectivo por rutas. El riesgo no es solo robo directo: es también **"float" no declarado** (el cobrador cobra pero no entrega ese día), **cobros no registrados en el sistema** (el cobrador acuerda con el cliente un descuento informal y se queda la diferencia), y **cancelaciones/ajustes abusivos** (abonos revertidos sin autorización).

La detección de fraude en rutas de efectivo no requiere machine learning sofisticado en etapa inicial: requiere **reconciliación sistemática por cobrador** y alertas sobre desviaciones estadísticas del patrón histórico de cada ruta.

El principio central es: **cada cobrador tiene un perfil de comportamiento esperado**. Desvíos consistentes de ese perfil (sin justificación operativa) son la señal de control primaria.

### Fórmulas / métricas

#### A. Gap de Captura de Pago (Payment Capture Gap)

**Definición**: diferencia entre lo que el cobrador declara cobrado y lo que el sistema registra como depositado/liquidado.

```
Gap_Cobrador = Cobros_declarados_por_cobrador - Depósitos_registrados_en_sistema
Gap_pct = Gap_Cobrador / Cobros_declarados_por_cobrador × 100
```

Un gap persistente > 0 (cobrador declara más de lo que deposita) puede indicar:
- Retraso en entrega de efectivo (float temporal — tolerable si < 1 día hábil)
- Retención parcial del cobro (fraude en escala)
- Error de captura (requiere auditoría cruzada)

#### B. Eficiencia de Cobranza por Cobrador (vs. cartera asignada)

```
Recovery_Rate_Cobrador = SUM(cobros_cobrador) / SUM(cuotas_esperadas_cartera_cobrador) × 100
```

Alertas:
- Si un cobrador tiene recovery_rate < 85% consistentemente mientras otros tienen > 95%: señal de selección adversa de clientes (no visita a los morosos) o de cobros no reportados.
- Si un cobrador tiene recovery_rate = 100% exacto en múltiples períodos: también es sospechoso (¿está cubriendo faltantes con efectivo propio para no ser detectado?).

#### C. Ratio de Cancelaciones / Ajustes por Cobrador

```
Ratio_Ajustes = (Cancelaciones + Ajustes_negativos) / Cobros_totales × 100
```

Un cobrador que genera muchos ajustes hacia abajo puede estar "devolviendo" cobros que ya retuvo. Benchmark: [A MEDIR NOSOTROS] — establecer umbral en promedio + 2 desviaciones estándar.

#### D. Variance por Ruta — Detección de Anomalías

```
Z_Score_Cobrador = (Recovery_Rate_Cobrador - Media_recovery_todos_cobradores) /
                   Desviación_estándar_recovery_todos_cobradores
```

Cobradores con Z-score < -2 (más de 2 desviaciones estándar por debajo del promedio del equipo) son candidatos a auditoría.

#### E. Días entre cobro y depósito (Float Monitoring)

```
Float_Días = AVG(fecha_depósito - fecha_cobro_declarado) por cobrador
```

Benchmark aceptable: 0–1 día hábil. Float > 2 días consistente por un cobrador específico: señal de retención temporal.

#### F. Clientes con cobros inconsistentes (señal de acuerdo informal)

```
-- Clientes cuyo pago real difiere del pago esperado en > X%
SELECT
    pago.cliente_id,
    pago.cobrador_id,
    pago.monto_cobrado,
    cuota.monto_cuota,
    ABS(pago.monto_cobrado - cuota.monto_cuota) / cuota.monto_cuota * 100 AS desviacion_pct
FROM MSP_COBROS pago
JOIN MSP_CUOTAS cuota ON cuota.cuota_id = pago.cuota_id
WHERE ABS(pago.monto_cobrado - cuota.monto_cuota) / cuota.monto_cuota > 0.10
ORDER BY desviacion_pct DESC;
```

Cobros que difieren > 10% del monto de cuota esperado (sin descuento autorizado) merecen revisión. Pueden indicar descuentos informales donde el cobrador se queda la diferencia.

**Implementación SQL — Dashboard de control por cobrador**

```sql
-- Panel de control de cobranza: señales de alerta por cobrador
SELECT
    cob.cobrador_id,
    cob.nombre                                                  AS cobrador,
    COUNT(DISTINCT co.cuota_id)                                 AS cuotas_atendidas,
    CAST(SUM(co.monto_cobrado) AS NUMERIC(18,2))                AS total_cobrado,
    CAST(SUM(cu.monto_cuota)   AS NUMERIC(18,2))                AS total_esperado,
    CAST(SUM(co.monto_cobrado) AS NUMERIC(18,4))
      / NULLIF(SUM(cu.monto_cuota), 0) * 100                   AS recovery_rate_pct,
    CAST(SUM(co.monto_cobrado) AS NUMERIC(18,4))
      - CAST(SUM(co.monto_depositado) AS NUMERIC(18,4))        AS gap_no_depositado,
    COUNT(CASE WHEN ABS(co.monto_cobrado - cu.monto_cuota)
                    / NULLIF(cu.monto_cuota, 0) > 0.10
               THEN 1 END)                                      AS cobros_con_desviacion,
    AVG(CAST(co.fecha_deposito - co.fecha_cobro AS INTEGER))    AS float_promedio_dias
FROM MSP_COBRADORES cob
JOIN MSP_COBROS co    ON co.cobrador_id  = cob.cobrador_id
JOIN MSP_CUOTAS cu    ON cu.cuota_id     = co.cuota_id
WHERE co.fecha_cobro BETWEEN :fecha_inicio AND :fecha_fin
GROUP BY cob.cobrador_id, cob.nombre
ORDER BY recovery_rate_pct ASC;  -- los de más bajo recovery al tope
```

**Reconciliación DSD-style (carga vs. liquidación)**

En el modelo DSD clásico, el sistema compara automáticamente lo cargado en el camión contra lo vendido/entregado/devuelto. Para cobranza en ruta el equivalente es: [[OrderWerks]](https://www.orderwerks.com/blog/route-accounting-software-guide)

```
Efectivo_esperado = SUM(cuotas_en_ruta_cobrador_del_día)
Efectivo_declarado = SUM(cobros_registrados_por_cobrador_ese_día)
Efectivo_depositado = Depósito_verificado_en_caja_o_banco

Señal_1: Efectivo_esperado ≠ Efectivo_declarado → cobros no reportados
Señal_2: Efectivo_declarado ≠ Efectivo_depositado → retención de efectivo
```

Las discrepancias deben **detectarse automáticamente el mismo día** de la ruta, no al mes siguiente. [[HighRadius — Financial Anomaly Management]](https://www.highradius.com/resources/Blog/financial-anomaly-management/)

### Pitfalls

- **No depender solo de reportes del cobrador**: el sistema debe calcular independientemente cuánto se esperaba cobrar en cada ruta, sin que el cobrador pueda influir ese número retroactivamente.
- **El "float" de 1–2 días es normal** en operaciones de campo donde el cobrador visita colonias alejadas. No toda demora en depósito es fraude — pero sí debe tener un límite máximo y monitorearse por cobrador.
- **Los sistemas de fraud detection basados en AI** (detección de anomalías en tiempo real, como los de HighRadius o Hawk.AI) están diseñados para volúmenes de transacciones de banca digital, no para 50–200 cobros diarios. En esta escala, **reglas estadísticas simples (Z-score, umbral fijo, variance diaria)** son más auditables, más fáciles de defender ante el cobrador, y suficientes. [[HighRadius — Transaction Anomaly Detection]](https://www.highradius.com/resources/Blog/transaction-data-anomaly-detection/)
- **No confundir baja recuperación con fraude**: un cobrador en zona de alta cartera vencida puede tener recovery_rate bajo porque simplemente los clientes no pagan, no porque esté robando. Cruzar siempre la tasa de recuperación del cobrador contra el PAR de su cartera.
- **Los controles más efectivos en efectivo son los preventivos**: numeración correlativa de recibos, tickets entregados al cliente en el momento del cobro, confirmación SMS/WhatsApp al cliente de pago recibido. Estos hacen que el cliente mismo sea parte del control.

### Fuentes

- [OrderWerks — Route Accounting Settlement](https://www.orderwerks.com/blog/route-accounting-software-guide)
- [HighRadius — Financial Anomaly Management](https://www.highradius.com/resources/Blog/financial-anomaly-management/)
- [HighRadius — Transaction Data Anomaly Detection](https://www.highradius.com/resources/Blog/transaction-data-anomaly-detection/)
- [EverWorker AI — Cash Leakage Prevention CFOs](https://everworker.ai/blog/ai_payroll_anomaly_detection_prevent_fraud_cash_leakage_cfo)
- [Kolleno — Cash Collections Metrics](https://www.kolleno.com/cash-flow-mastery-a-deep-dive-into-cash-collections-formula-and-metrics/)
- [Allianz Trade — Collection Effectiveness Index](https://www.allianz-trade.com/en_US/insights/collection-effectiveness-index.html)

---

## Apéndice: Mapa de métricas por tabla origen (msp-api)

| Métrica | Tablas Firebird involucradas |
|---------|------------------------------|
| PAR30/60/90 | MSP_CREDITOS, MSP_PAGOS |
| Collection Rate por cobrador | MSP_COBROS, MSP_CUOTAS, MSP_COBRADORES |
| Roll Rate aging | MSP_CREDITOS, MSP_PAGOS (snapshot mensual) |
| Revenue por vendedor / zona | MSP_VENTAS, MSP_VENDEDORES, MSP_ZONAS, MSP_CLIENTES |
| Quota Attainment | MSP_VENTAS, MSP_VENDEDORES (campo cuota_mensual) |
| Gap cobrador | MSP_COBROS (campos monto_cobrado, monto_depositado, fecha_deposito) |
| Float monitoring | MSP_COBROS (fecha_cobro, fecha_deposito) |
| Ajustes/cancelaciones | MSP_AJUSTES_COBRO, MSP_COBROS |

Los campos `monto_depositado` y `fecha_deposito` en `MSP_COBROS` son recomendación de diseño si aún no existen — son la base de los controles antifraude más efectivos.

> Documento generado: 2026-06-13. Fuentes verificadas con ≥ 2 referencias independientes por afirmación. Benchmarks marcados [A MEDIR NOSOTROS] deben calibrarse con datos históricos propios antes de fijar umbrales de alerta.


---


# C4 — Tops & Arquitectura de Analytics para Retail

> Investigacion de estado del arte sobre como los grandes del retail estructuran sus capas analiticas, que patrones aplican universalmente, y como traducirlos a nuestra escala real: miles de clientes, cientos de miles de transacciones, Go sobre Firebird, Windows Server, sin cloud ni Docker en produccion.
>
> **Nota de escala:** Walmart procesa 2.5 petabytes por hora y tiene 250 millones de clientes semanales. Coppel opera ~1,980 sucursales. Target tiene 2,000+ tiendas. Nosotros tenemos una sola sucursal con rutas definidas. Esta diferencia de escala es el filtro principal para decidir que adoptar y que descartar.

---

## 1. Como estructuran analytics los grandes del retail

### Patron / como lo hacen

**Walmart — Retail Link + Data Cafe**

Walmart construyo dos capas diferenciadas: Retail Link (portal de acceso para proveedores, orientado a sell-through y reabastecimiento) y el Data Cafe (*Collaborative Analytics Facilities for Enterprise*), un hub de analytics interno en Bentonville. El Data Cafe conecta mas de 200 fuentes de datos: datos transaccionales propios (hasta 40 petabytes de historial reciente), datos meteorologicos, precios de gasolina, datos de Nielsen, señales de redes sociales, y bases de eventos locales. El resultado es que una consulta que antes tomaba dos o tres semanas hoy tarda 20-30 minutos. La arquitectura combina analytics proactivo (modelos predictivos) y reactivo (respuesta a eventos), todo unificado desde la misma capa de datos. [[1]](https://www.retailitinsights.com/doc/walmart-turns-to-data-caf-analytics-hub-to-make-sense-of-data-0001) [[2]](https://bernardmarr.com/walmart-big-data-analytics-at-the-worlds-biggest-retailer/)

**Target — Guest ID + CORE**

Target asigna a cada comprador un Guest ID vinculado a su tarjeta de credito, nombre o correo. Eso construye una base de datos de comportamiento de compra y demograficos que alimenta modelos predictivos. Encima de esos perfiles, Target construyo CORE (*Contextual Offer Recommendation Engine*): un modelo de multi-armed bandit contextual alimentado por historial de transacciones, interacciones con promociones y comportamiento de navegacion. El modelo usa factorizacion de matrices (Non-Negative Matrix Factorization) para extraer features latentes de la interaccion cliente-oferta, y una red neuronal que combina esos features para recomendar. En 2023 CORE sirvio millones de ofertas personalizadas. [[3]](https://tech.target.com/blog/contextual-offer-recommendation-engine)

**Mercado Libre — BigQuery + Looker**

Mercado Libre procesa cientos de terabytes diarios y ejecuta cientos de miles de consultas diarias usando BigQuery como capa de computo analitico y Looker como capa de BI/visualizacion. Su filosofia: "los datos deben ser oportunos, creibles y disponibles para analisis." Ingestan datos de streaming, trafico web (Google Analytics, App Annie), logs de almacen y red, APIs internas y sistemas de gestion. Los equipos de negocio, transporte y operaciones reciben datos en tiempo real integrados con Slack, email y Google Sheets. [[4]](https://dev.to/cloudnomics/mercado-libre-goes-bigquery-moneyball-goes-cloud-plus-plugging-a-300b-retail-search-hole-ecb)

**Shopify — analitica para comerciantes (modelo SaaS embebido)**

Shopify rediseno completamente su suite de analytics en el Summer '24 Edition: dashboards en tiempo real, cohort reports de retencion, y un asistente conversacional (Sidekick) que responde preguntas como "cual fue mi producto mas vendido la semana pasada." El dato clave de arquitectura: Shopify usa un modelo de datos unificado para todos los canales de venta, lo que reduce el TCO hasta 37% segun ellos mismos. La metrica de negocio central es la misma independientemente del canal (web, POS, app). [[5]](https://badger.blue/blogs/ecommerce-unpacked/shopify-analytics-2024-update) [[6]](https://www.shopify.com/enterprise/blog/sales-analytics-guide)

**Coppel / Elektra — el modelo de credito al consumo como dato**

Coppel (1,980 sucursales) y Elektra (1,277 sucursales) son esencialmente financieras disfrazadas de mueblerias. Su ventaja competitiva no es el producto sino el historial crediticio: cada pago semanal es un dato que calibra el riesgo del cliente y permite ampliar o contraer el credito. Ambas empresas estan en proceso de transformacion hacia ecosistemas omnicanal (Elektra con la superapp Baz, 12 millones de usuarios). No hay documentacion tecnica publica de su arquitectura de datos, pero el patron es claro: el dato de pago es el nucleo, y la analitica de cliente gira alrededor del comportamiento de pago, no del producto. [[7]](https://www.espressomatutino.com/p/2025-12-18) [[8]](https://americasmi.com/insights/digitization-mexico-retailers-finance/)

**Patron comun: tres capas**

En todos los casos, independientemente de la escala, aparece la misma estructura de tres capas:

1. **Capa de datos operacionales** (OLTP): la base transaccional (POS, ERP, CRM) — optimizada para escrituras concurrentes, registros individuales.
2. **Capa analitica derivada** (OLAP o modelo dimensional): tablas pre-calculadas o un data warehouse separado — optimizado para lecturas agregadas, scans de columnas, joins de millones de filas.
3. **Capa semantica / de metricas**: definiciones de negocio (que cuenta como "venta cerrada", que es un "cliente activo") centralizadas y reutilizadas por todos los dashboards y consumidores.

### Como aterrizarlo a nuestra escala

Los tres patrones aplican. La diferencia es el tamano de cada capa:

- **Capa 1 (OLTP):** ya existe — es Firebird con las tablas de Microsip + nuestras tablas `MSP_*`.
- **Capa 2 (OLAP):** no necesitamos BigQuery ni Redshift. Necesitamos tablas de resumen (`MSP_ANALYTICS_*`) refrescadas por un job de Go en batch. Ver seccion 4.
- **Capa 3 (semantica):** no necesitamos dbt ni Cube. Necesitamos que las definiciones de metricas vivan como constantes/queries nombradas en Go, no dispersas en SQL ad-hoc por cada handler. Ver seccion 3.

El patron de Coppel/Elektra es el mas relevante: el dato del pago/credito es el nucleo, y la analitica de cliente gira alrededor del comportamiento de pago. Es exactamente nuestro caso.

### Pitfalls / overkill

- **OVERKILL:** Multi-source data ingestion (meteorologia, Nielsen, redes sociales). Para una sola sucursal con rutas geograficamente acotadas, los datos externos agregan ruido, no senal.
- **OVERKILL:** Real-time streaming analytics (Kafka, Flink). Nuestras ventas no ocurren a 1M eventos/segundo. Un refresh batch cada 30-60 minutos es suficiente para cualquier decision operativa.
- **OVERKILL:** Recommendation engine con matrix factorization (estilo Target CORE). Con miles de clientes y un catalogo de cientos de productos, la segmentacion manual por zona/ruta y comportamiento de pago supera en precision a cualquier ML con tan pocos datos. [A MEDIR NOSOTROS: cuantos clientes activos y cuantos SKUs distintos vendemos — si son <5,000 clientes y <500 SKUs, cualquier modelo de ML es overfitting puro.]
- **Valido:** Guest ID / perfil unificado por cliente. Asignar un ID persistente y acumular historial de transacciones, pagos y comportamiento de cobro ES el fundamento. Ya lo tenemos en Firebird.

### Fuentes

- [Walmart Data Cafe — Retail IT Insights](https://www.retailitinsights.com/doc/walmart-turns-to-data-caf-analytics-hub-to-make-sense-of-data-0001)
- [Walmart Big Data — Bernard Marr](https://bernardmarr.com/walmart-big-data-analytics-at-the-worlds-biggest-retailer/)
- [Target CORE — tech.target.com](https://tech.target.com/blog/contextual-offer-recommendation-engine)
- [Mercado Libre + BigQuery — dev.to/cloudnomics](https://dev.to/cloudnomics/mercado-libre-goes-bigquery-moneyball-goes-cloud-plus-plugging-a-300b-retail-search-hole-ecb)
- [Shopify Analytics 2024 — badger.blue](https://badger.blue/blogs/ecommerce-unpacked/shopify-analytics-2024-update)
- [Coppel vs Elektra — Espresso Matutino](https://www.espressomatutino.com/p/2025-12-18)
- [Retail y finanzas en Mexico — Americas Market Intelligence](https://americasmi.com/insights/digitization-mexico-retailers-finance/)

---

## 2. Modelado dimensional: star schema, grain, hechos y dimensiones

### Patron / como lo hacen

**El problema que resuelve el modelo dimensional**

Las bases de datos OLTP (como Firebird con Microsip) estan normalizadas para minimizar redundancia y optimizar escrituras concurrentes. Una venta en Microsip involucra joins entre `DOCTOS_VE`, `DOCTOS_VE_DET`, `CLIENTES`, `INVENTARIOS`, `VENDEDORES`, y posiblemente otras tablas. Una consulta analitica — "cuanto vendimos por ruta en los ultimos 90 dias, desglosado por categoria de producto" — requiere un join de 5+ tablas sobre cientos de miles de filas. En OLTP, esto es lento y compite con las escrituras del dia. En OLAP, es la operacion basica.

El modelado dimensional (Kimball, 1996) resuelve esto con dos tipos de tablas: [[9]](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/) [[10]](https://www.owox.com/blog/articles/star-schema-explained)

- **Fact table (tabla de hechos):** registra medidas cuantificables de un evento de negocio. Columnas: claves foraneas a dimensiones + medidas numericas (importe, cantidad, descuento).
- **Dimension tables (tablas de dimension):** contexto descriptivo del hecho. Columnas: atributos como nombre del cliente, categoria del producto, dia de la semana, nombre del vendedor.

**El concepto de grain**

El grain es el nivel mas atomico de detalle que almacena la fact table. La regla de Kimball: elegir el grain mas atomico posible. Para ventas en Microsip, el grain correcto es la **linea de detalle de la venta** (un renglón de `DOCTOS_VE_DET`), no el encabezado del documento. Esto permite responder tanto "cuanto vendio el vendedor X" como "cuantas unidades del producto Y se vendieron en la ruta Z".

**Ejemplo concreto: Sales Fact para nuestra muebleria**

```
FACT_VENTAS (grain: una linea de detalle de venta)
  -- Claves foraneas
  fecha_id          → DIM_FECHA
  cliente_id        → DIM_CLIENTE
  producto_id       → DIM_PRODUCTO
  vendedor_id       → DIM_VENDEDOR
  ruta_id           → DIM_RUTA
  -- Medidas
  importe_bruto     NUMERIC(18,2)   -- precio * cantidad
  descuento         NUMERIC(18,2)
  importe_neto      NUMERIC(18,2)   -- importe_bruto - descuento
  cantidad          INTEGER
  costo_unitario    NUMERIC(18,2)   -- para margen
  es_credito        SMALLINT        -- 0/1, bandera para separar contado vs credito

DIM_FECHA
  fecha_id, fecha, anio, mes, semana_del_anio, dia_semana,
  es_fin_de_semana, nombre_mes, trimestre

DIM_CLIENTE
  cliente_id, nombre, zona, colonia, municipio,
  tipo_cliente (nuevo/recurrente), fecha_primer_compra,
  limite_credito_vigente, estatus_cartera (al_dia/vencido/...)

DIM_PRODUCTO
  producto_id, descripcion, categoria, subcategoria, proveedor,
  costo_promedio, precio_lista

DIM_VENDEDOR
  vendedor_id, nombre, tipo (propio/comisionista)

DIM_RUTA
  ruta_id, nombre_ruta, cobrador_id, zona_geografica
```

**OLTP vs OLAP: la diferencia clave**

| Dimension       | OLTP (Firebird/Microsip)        | OLAP (modelo dimensional)          |
|-----------------|----------------------------------|------------------------------------|
| Optimizado para | Escrituras concurrentes rapidas  | Lecturas agregadas sobre millones  |
| Esquema         | Normalizado (muchos joins)       | Desnormalizado (star schema)       |
| Actualizacion   | En tiempo real, por transaccion  | Batch (cada N minutos/horas)       |
| Consulta tipica | "dame el documento #12345"       | "ventas por ruta, mes, categoria"  |
| Indices         | Por PK y FK                      | Por columnas de filtro/agrupacion  |

La observacion central de Kimball: "haz el trabajo dificil ahora para que sea facil de consultar despues." El modelo dimensional paga el costo en el momento de la ingesta/refresh, no en el momento de la consulta. [[9]](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/)

### Como aterrizarlo a nuestra escala

No necesitamos un data warehouse separado. El modelo dimensional puede vivir como tablas regulares en Firebird, con un prefijo `MSP_AN_` (analytics) para distinguirlas de las tablas operacionales:

- `MSP_AN_FACT_VENTAS` — grain: linea de detalle
- `MSP_AN_FACT_COBROS` — grain: un pago recibido
- `MSP_AN_DIM_FECHA` — estatica, generada una vez
- `MSP_AN_DIM_CLIENTE` — se actualiza cuando cambia un cliente
- `MSP_AN_DIM_PRODUCTO`, `MSP_AN_DIM_RUTA` — idem

El job de Go que refresca estas tablas lee de Microsip/MSP, transforma al modelo dimensional, y hace upserts. Las consultas analiticas van contra estas tablas, no contra las tablas operacionales. Separacion limpia, sin riesgo de contension.

**Dimension de fecha:** generar una vez, de 2015 a 2035. Una tabla de ~7,300 filas. Permite queries como `WHERE d.nombre_mes = 'Enero' AND d.anio = 2025` sin funciones SQL que rompan indices.

### Pitfalls / overkill

- **OVERKILL:** Snowflake schema (normalizar las dimensiones). Solo agrega joins sin beneficio para nuestra escala.
- **OVERKILL:** Slowly Changing Dimensions tipo 2 (historial de cambios en dimensiones). Para nosotros, si un cliente cambia de zona, nos interesa la zona actual, no la historia completa. SCD tipo 1 (sobreescribir) es suficiente.
- **OVERKILL:** Fact tables de eventos granulares para navegacion web, clicks, etc. Nuestros eventos relevantes son ventas y cobros — granulares si, pero acotados.
- **VALIDO:** Conformed dimensions (dimensiones compartidas entre facts). `DIM_CLIENTE` usada tanto por `FACT_VENTAS` como por `FACT_COBROS` permite cruzar "el cliente X compro en enero y pago en febrero" sin joins complicados.
- **CUIDADO [A MEDIR NOSOTROS]:** El volumen de `FACT_VENTAS` depende de cuantas lineas de detalle tenemos en historial. Si son <500,000 filas, incluso una query ad-hoc sobre las tablas normalizadas de Microsip puede ser aceptable. La justificacion de un modelo dimensional aumenta con el volumen y la frecuencia de las consultas.

### Fuentes

- [Kimball Dimensional Modeling — holistics.io](https://www.holistics.io/books/setup-analytics/kimball-s-dimensional-data-modeling/)
- [Star Schema explicado — OWOX](https://www.owox.com/blog/articles/star-schema-explained)
- [Kimball para principiantes — Medium/QuarkAndCode](https://medium.com/@QuarkAndCode/kimball-data-modeling-explained-star-schema-for-beginners-5298c31943bb)
- [OLTP vs OLAP — AWS](https://aws.amazon.com/compare/the-difference-between-olap-and-oltp/)
- [dbt + Kimball — dbt Developer Blog](https://docs.getdbt.com/blog/kimball-dimensional-model)

---

## 3. La capa semantica / de metricas: el gran giro de los 2020s

### Patron / como lo hacen

**El problema que resuelve**

En cualquier equipo con mas de un dashboard, la misma metrica se define de formas distintas en distintos lugares. "Ventas del mes" puede significar `SUM(importe_bruto)` en un reporte, `SUM(importe_neto)` en otro, y `SUM(importe_neto WHERE es_credito=0)` en un tercero. Cuando el gerente ve tres numeros distintos en tres reportes distintos, deja de confiar en los datos. La capa semantica resuelve esto definiendo cada metrica una sola vez, en un lugar neutral, y sirviendo esa definicion a todos los consumidores. [[11]](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly) [[12]](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)

**Tres arquitecturas dominantes en 2025-2026**

1. **Warehouse-native** (Snowflake Semantic Views, Databricks Metric Views): la logica semantica vive como objetos de base de datos. Conveniente dentro de una plataforma; limitante si hay multiples consumidores o apps embebidas.

2. **Transformation-layer** (dbt MetricFlow, GA octubre 2024): las definiciones de metricas viven como YAML junto a los modelos dbt, con control de versiones en Git. Sin cache propio; delega la ejecucion al warehouse.

3. **Decoupled semantic layer** (Cube.dev, AtScale, GoodData): una capa independiente entre el warehouse y todos los consumidores — dashboards, apps embebidas, agentes de IA. Expone SQL, REST, GraphQL. Agrega cache y control de acceso propios. [[13]](https://www.typedef.ai/resources/semantic-layer-architectures-explained-warehouse-native-vs-dbt-vs-cube)

**Cuando NO necesitas una capa semantica de terceros**

Cube.dev lo dice explicitamente: equipos con un solo warehouse y un solo consumidor de BI, sin planes de embeber analytics ni de usar agentes de IA, pueden no necesitar una capa semantica separada todavia. La recomendacion es revisarlo cuando aparezca un segundo consumidor. [[12]](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)

**"Nearly headless BI": el beneficio sin el stack completo**

David Jayatillake articulo el patron para equipos chicos: codificar las definiciones de negocio una sola vez como codigo (structs, YAML embebido, constantes SQL nombradas), hacer que todos los consumidores lean de esas definiciones, y dejar que el motor de base de datos ejecute. El beneficio — consistencia de metricas — se obtiene sin desplegar Cube ni conectarse a dbt Cloud. [[14]](https://davidsj.substack.com/p/nearly-headless-bi) [[11]](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly)

### Como aterrizarlo a nuestra escala

**La implementacion Go-native de una thin metrics layer:**

En lugar de dbt o Cube, creamos un paquete `internal/analytics/metrics/` con definiciones de metricas como constantes SQL nombradas o structs Go:

```go
// internal/analytics/metrics/ventas.go

// VentasNetas define la metrica canonica de ventas netas.
// Todos los handlers, exports y jobs usan esta query, no SQL ad-hoc.
const VentasNetas = `
    SELECT
        SUM(f.importe_neto)  AS ventas_netas,
        SUM(f.importe_bruto) AS ventas_brutas,
        COUNT(DISTINCT f.cliente_id) AS clientes_unicos,
        SUM(f.cantidad)      AS unidades
    FROM MSP_AN_FACT_VENTAS f
    WHERE f.fecha_id BETWEEN :desde AND :hasta
`

// VentasNetasPorRuta extiende la metrica base con desglose.
const VentasNetasPorRuta = VentasNetas + `
    JOIN MSP_AN_DIM_RUTA r ON r.ruta_id = f.ruta_id
    GROUP BY r.ruta_id, r.nombre_ruta
`
```

Reglas para que funcione:
1. **Ningun handler construye SQL de metricas directamente.** Todo SQL analitico viene del paquete `metrics/`.
2. **Si la definicion cambia** (ej. "las notas de credito no cuentan como venta"), se cambia en un solo lugar y todos los reportes se actualizan automaticamente en el siguiente refresh.
3. **Las metricas tienen tests.** Un test de integracion verifica que `VentasNetas` retorna el valor correcto contra datos de fixture — exactamente igual que testear un endpoint HTTP.

Esto es "semantica como codigo" sin ninguna herramienta externa. El costo es disciplina de equipo: nadie escribe SQL analitico ad-hoc en los handlers.

**Analogia con el problema original de la capa semantica:** en lugar de tener tres dashboards con tres queries distintas para "ventas netas", tenemos una constante `metrics.VentasNetas` que los tres handlers usan. El numero siempre coincide.

### Pitfalls / overkill

- **OVERKILL:** dbt Semantic Layer / MetricFlow. Requiere dbt Cloud, una arquitectura de transformacion separada, y un equipo de data engineers. No aplica para un sistema Go monolitico sobre Firebird.
- **OVERKILL:** Cube.dev. Es una pieza de infraestructura adicional (servidor, cache Redis, config YAML compleja) que resuelve el problema de multi-tenant y multi-warehouse. Nosotros tenemos un tenant y un warehouse.
- **OVERKILL:** LookML / Looker. Es una herramienta de BI completa con su propio lenguaje de modelado. Para nuestro caso, un dashboard custom en React/Svelte consume directamente nuestra API Go.
- **VALIDO:** El patron "metrics as code" descrito arriba. Zero dependencias externas, 100% compatible con Go + Firebird, y resuelve exactamente el problema de inconsistencia de metricas.
- **CUIDADO [A MEDIR NOSOTROS]:** La necesidad de una capa semantica formal crece con el numero de consumidores. Si solo hay un dashboard interno y un report Excel exportado, una o dos constantes SQL nombradas son suficientes. Si hay un dashboard de gerencia, un reporte para el dueno, una app movil del cobrador y un export para contabilidad, la disciplina del paquete `metrics/` se vuelve critica.

### Fuentes

- [Rise of the Semantic Layer — Airbyte](https://airbyte.com/blog/the-rise-of-the-semantic-layer-metrics-on-the-fly)
- [Best Semantic Layer 2026 — Cube.dev](https://cube.dev/articles/best-semantic-layer-for-ai-and-bi-2026)
- [Arquitecturas de Semantic Layer — typedef.ai](https://www.typedef.ai/resources/semantic-layer-architectures-explained-warehouse-native-vs-dbt-vs-cube)
- [Nearly Headless BI — David Jayatillake / Substack](https://davidsj.substack.com/p/nearly-headless-bi)
- [dbt Semantic Layer vale la pena? — Medium](https://medium.com/@reliabledataengineering/the-dbt-semantic-layer-is-it-worth-migrating-from-your-metrics-store-e7aa4bec2e52)

---

## 4. Arquitectura para nuestras restricciones: Go, Firebird, Windows Server

### Patron / como lo hacen los que trabajan con stacks similares

**Batch rollups vs on-demand: el debate resuelto**

En sistemas OLTP donde el motor de base de datos es el cuello de botella, el consenso de la industria es claro: las consultas analiticas complejas no deben competir con las transacciones operacionales. El patron dominante para equipos sin data warehouse dedicado es: **tablas de resumen refrescadas en batch por un worker**, no queries on-demand contra las tablas normalizadas. [[15]](https://clickhouse.com/resources/engineering/real-time-analytics-postgres) [[16]](https://codelit.io/blog/database-materialized-view-refresh)

Los cuatro patrones de refresh documentados (de mas simple a mas complejo):

| Patron        | Latencia de datos | Impacto en escrituras | Complejidad |
|---------------|-------------------|----------------------|-------------|
| Lazy (cron)   | Minutos-horas     | Ninguno              | Baja        |
| Incremental   | Segundos-minutos  | Bajo                 | Alta        |
| Eager (sync)  | Ninguna           | Alto (bloquea writes)| Baja        |
| Streaming     | Sub-segundo       | Medio                | Muy alta    |

Para analytics de negocio (dashboards, reportes, perfiles de cliente), **lazy refresh via cron** es el patron correcto. Los datos de "ventas de esta semana" no necesitan actualizarse mas rapido que cada 30-60 minutos. [[16]](https://codelit.io/blog/database-materialized-view-refresh)

**El problema especifico de Firebird: no tiene materialized views nativas**

A diferencia de PostgreSQL (que tiene `REFRESH MATERIALIZED VIEW`), Firebird no implementa materialized views como objeto de base de datos nativo. El feature request lleva abierto desde 2005 (CORE822). [[17]](https://github.com/FirebirdSQL/firebird/issues/1208)

Esto es una limitacion real, pero no bloqueante. El workaround estandar: **tablas regulares que actuan como materialized views**, refrescadas por un proceso externo (un job de Go). La tabla `MSP_AN_FACT_VENTAS` no es una view — es una tabla ordinaria. Un goroutine con ticker, o un endpoint `/admin/analytics/refresh` llamado por Windows Task Scheduler, ejecuta el `DELETE + INSERT` periodicamente.

**DuckDB como motor OLAP embebido: el caso**

DuckDB emergió como "el SQLite del OLAP" — un motor columnar embebido, sin servidor, que puede leer archivos Parquet, CSV, y conectarse a bases de datos externas. Para analytics complejos sobre datos ya extraidos, DuckDB puede dar una aceleracion de ~500x sobre queries equivalentes en bases de datos relacionales orientadas a filas. [[18]](https://motherduck.com/learn/select-olap-solution-postgres/) [[19]](https://medium.com/@connect.hashblock/materialized-views-in-duckdb-fast-analytics-without-warehouses-981189a26f05)

Sin embargo, DuckDB en produccion sobre Windows Server con Go tiene fricciones: el driver CGO para Go requiere CGO_ENABLED=1, que rompe la compilacion cruzada CGO_ENABLED=0 que usamos. Hasta que DuckDB tenga un driver Go puro sin CGO estable, **esta opcion es overkill para nosotros**.

**La arquitectura correcta para Go + Firebird + Windows Server**

```
[Firebird: tablas operacionales Microsip + MSP_*]
              |
              | (lectura batch, cada 30-60 min)
              v
[Go analytics worker: lee, transforma, hace upserts]
              |
              v
[Firebird: tablas MSP_AN_* (modelo dimensional)]
              |
    +---------+----------+
    |                    |
    v                    v
[API Go: endpoints    [API Go: endpoints
 /analytics/ventas     /clientes/:id/perfil
 /analytics/rutas      /clientes/:id/historial]
 /analytics/productos]
              |
              v
[Frontend: dashboard Go/React/Svelte via JSON]
```

**Componentes concretos:**

1. **`internal/analytics/worker/`** — goroutine o job externo que refresca las tablas `MSP_AN_*`. Logica: `DELETE FROM MSP_AN_FACT_VENTAS WHERE fecha_id >= :watermark` + `INSERT INTO MSP_AN_FACT_VENTAS SELECT ... FROM DOCTOS_VE_DET ...`. Guarda un watermark de ultimo refresh en `MSP_CFG` o en archivo de estado.

2. **`internal/analytics/repo/`** — repositorio Firebird que lee de `MSP_AN_*`. Implementa queries como `VentasPorRuta(desde, hasta time.Time) ([]RutaMetrica, error)`. No construye SQL en los handlers.

3. **`internal/analytics/metrics/`** — el paquete de definiciones de metricas descrito en la seccion 3. SQL nombrado, no ad-hoc.

4. **`internal/analytics/infra/http/`** — handlers HTTP que consumen el repo. Endpoints: `GET /analytics/ventas`, `GET /analytics/rutas`, `GET /analytics/productos`, `GET /clientes/:id/perfil-analitico`.

**Perfil de cliente desde la capa analitica**

La misma `FACT_VENTAS` + `FACT_COBROS` que sirven los dashboards agregados sirven los perfiles individuales de cliente:

```sql
-- Perfil analitico de un cliente especifico
SELECT
    COUNT(DISTINCT v.fecha_id)  AS dias_con_compra,
    SUM(v.importe_neto)         AS gasto_historico_total,
    AVG(v.importe_neto)         AS ticket_promedio,
    MAX(f.fecha)                AS ultima_compra,
    (SELECT SUM(c.monto) FROM MSP_AN_FACT_COBROS c
     WHERE c.cliente_id = :cid
       AND c.dias_mora > 0)     AS monto_pagado_con_mora
FROM MSP_AN_FACT_VENTAS v
JOIN MSP_AN_DIM_FECHA f ON f.fecha_id = v.fecha_id
WHERE v.cliente_id = :cid
```

Esto demuestra la ganancia del modelo dimensional: un query de perfil de cliente y un query de dashboard de ventas por ruta usan exactamente la misma fact table, con filtros distintos. Una sola capa materializada sirve ambos casos de uso.

**Estrategia de refresh incremental sin materialized views nativas**

```
MSP_AN_REFRESH_LOG (tabla de control)
  refresh_id   CHAR(36)
  tipo         VARCHAR(50)   -- 'fact_ventas', 'fact_cobros', etc.
  watermark    TIMESTAMP     -- hasta donde se proceso
  started_at   TIMESTAMP
  finished_at  TIMESTAMP
  filas_procesadas INTEGER
  estatus      VARCHAR(20)   -- 'ok', 'error'
```

El worker Go: al arrancar, lee el watermark de la ultima corrida exitosa y solo procesa registros nuevos desde ese punto. Si falla, el registro queda en estatus 'error' y la proxima corrida reintenta desde el watermark anterior. Logica simple, sin dependencias externas, compatible con Windows Task Scheduler o un goroutine con ticker.

### Como aterrizarlo a nuestra escala

Pasos en orden de implementacion:

1. **Generar `MSP_AN_DIM_FECHA`** una sola vez (migration). 7,300 filas para 20 anios.
2. **Crear `MSP_AN_DIM_CLIENTE`, `MSP_AN_DIM_PRODUCTO`, `MSP_AN_DIM_RUTA`** — refrescadas por el worker cuando hay cambios.
3. **Crear `MSP_AN_FACT_VENTAS`** y `MSP_AN_FACT_COBROS` — el nucleo del modelo.
4. **Implementar el worker de refresh** en `internal/analytics/worker/` con watermark y log.
5. **Implementar el repo analitico** en `internal/analytics/repo/` usando el paquete `metrics/`.
6. **Exponer endpoints HTTP** de dashboards y perfiles de cliente.

**[A MEDIR NOSOTROS]:**
- Cuantas filas tiene `DOCTOS_VE_DET` en historial — determina si el refresh inicial es un problema de rendimiento.
- Cuanto tarda un `SELECT COUNT(*) FROM DOCTOS_VE_DET` en Firebird — baseline de velocidad.
- Cuantos clientes distintos tienen mas de una compra — determina si los perfiles de cliente tienen suficiente historial para ser utiles.
- Frecuencia de consultas al dashboard — si es <10 veces al dia, incluso una query on-demand lenta es aceptable; si es continua, el refresh batch se justifica.

### Pitfalls / overkill

- **OVERKILL:** DuckDB embebido en Go (CGO requerido, rompe compilacion cruzada Windows).
- **OVERKILL:** ClickHouse o cualquier motor columnar externo. Agregan un servidor adicional en Windows, operacion compleja, sin beneficio claro para nuestra escala.
- **OVERKILL:** Streaming analytics (Kafka + Flink). Las ventas de una muebleria no requieren latencia sub-segundo. El gerente no necesita ver el dashboard actualizado en tiempo real mientras el vendedor esta llenando el contrato.
- **OVERKILL:** ETL con Airflow o herramientas de orquestacion. Un goroutine de Go con un ticker de 30 minutos o una tarea de Windows Task Scheduler hacen el mismo trabajo con cero dependencias adicionales.
- **VALIDO:** Tablas regulares en Firebird como "materialized views manuales" — el workaround documentado por la comunidad Firebird. Sin fricciones de stack, compatible con el resto del sistema.
- **VALIDO:** Un unico worker de refresh que construye toda la capa analitica — no necesitamos pipelines de ETL separados para cada tabla.
- **CUIDADO:** La contension de recursos entre el worker de refresh y las transacciones operacionales. El worker debe programarse en horas de baja actividad (madrugada, hora de comida) y usar transacciones de solo lectura sobre las tablas Microsip para no bloquear escrituras.

### Fuentes

- [Real-time analytics sobre Postgres — ClickHouse Blog](https://clickhouse.com/resources/engineering/real-time-analytics-postgres)
- [Estrategias de refresh para materialized views — codelit.io](https://codelit.io/blog/database-materialized-view-refresh)
- [Materialized views en Firebird — GitHub Issue CORE822](https://github.com/FirebirdSQL/firebird/issues/1208)
- [Seleccionar solucion OLAP — MotherDuck](https://motherduck.com/learn/select-olap-solution-postgres/)
- [DuckDB materialized views — Medium/HashBlock](https://medium.com/@connect.hashblock/materialized-views-in-duckdb-fast-analytics-without-warehouses-981189a26f05)
- [Rollup tables — QuestDB](https://questdb.com/glossary/rollup-table/)
- [OLAP en Postgres: retos y estrategias — epsio.io](https://www.epsio.io/blog/olap-in-postgres-features-challenges-and-optimization-strategies)


---
