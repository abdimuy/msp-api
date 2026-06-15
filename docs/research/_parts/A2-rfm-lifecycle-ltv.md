# Inteligencia de Clientes: RFM, Estados de Ciclo de Vida y CLV/LTV

> Sección A2 del documento "State of the Art" para el componente de customer intelligence.
> Contexto: mueblería mexicana, crédito a plazos, base no-bancarizada, ~miles de clientes, ciclo de recompra ~11 meses.

---

## 2. RFM Segmentation

### Recomendación

RFM (Recency, Frequency, Monetary) es el punto de partida correcto para este negocio. Es implementable con SQL puro, no requiere ML, y entrega valor en semanas. La adaptación crítica para crédito a plazos está en la dimensión **M**: usar el **ingreso financiero total por cliente** (precio_crédito − precio_contado + todas las compras acumuladas) en lugar del precio de lista o contado. Esto captura el verdadero valor de un cliente que paga a plazos, donde el diferencial de precio es margen real para el negocio.

Se recomienda usar **quintiles (1–5)** sobre cuartiles (1–4) para un negocio con pocos miles de clientes, ya que cada score representa el 20% de la base y da mayor granularidad de segmentación accionable. Para bases menores de 500 clientes activos, considerar cuartiles (1–4).

---

### Fórmula/Algoritmo

#### Paso 1: Definir la ventana de análisis

La ventana debe ser al menos 2× el ciclo de recompra. Para un ciclo de ~11 meses, usar **24 meses** como ventana rodante.

```sql
-- Fecha de referencia para recency (snapshot date)
DEFINE analysis_date = CURRENT_DATE;
DEFINE window_start  = analysis_date - INTERVAL 24 MONTH;
```

#### Paso 2: Calcular las métricas brutas por cliente

```sql
SELECT
    c.cliente_id,

    -- R: días desde la ÚLTIMA compra (a plazos o contado) dentro de la ventana
    DATEDIFF(DAY, MAX(v.fecha_venta), :analysis_date) AS recency_days,

    -- F: número de ventas (contratos) en la ventana
    COUNT(DISTINCT v.venta_id)                        AS frequency,

    -- M: ingreso financiero total = precio_credito real cobrado,
    --    no precio_contado. Incluye diferencial de financiamiento.
    --    Para crédito: SUM(precio_credito). Para contado: SUM(precio_contado).
    --    El diferencial (precio_credito - precio_contado) es la "prima financiera".
    SUM(
        CASE
            WHEN v.tipo_venta = 'credito'
            THEN v.total_credito          -- precio total a plazos (ya incluye prima)
            ELSE v.total_contado
        END
    )                                                 AS monetary_total,

    -- M_prima: componente de prima financiera pura (para análisis secundario)
    SUM(
        CASE
            WHEN v.tipo_venta = 'credito'
            THEN v.total_credito - v.precio_contado_equivalente
            ELSE 0
        END
    )                                                 AS monetary_prima_financiera

FROM clientes c
JOIN ventas v ON v.cliente_id = c.cliente_id
WHERE v.fecha_venta BETWEEN :window_start AND :analysis_date
GROUP BY c.cliente_id;
```

#### Paso 3: Scoring por quintiles con NTILE

```sql
-- Tabla intermedia con métricas crudas: rfm_raw(cliente_id, recency_days, frequency, monetary_total)

SELECT
    cliente_id,
    recency_days,
    frequency,
    monetary_total,

    -- R: menor recency_days = más reciente = mayor score
    NTILE(5) OVER (ORDER BY recency_days ASC)  AS r_score,

    -- F: mayor frequency = mayor score
    NTILE(5) OVER (ORDER BY frequency DESC)    AS f_score,

    -- M: mayor monetary = mayor score
    NTILE(5) OVER (ORDER BY monetary_total DESC) AS m_score

FROM rfm_raw;
-- Resultado: r_score, f_score, m_score en rango [1,5]
-- Score 5 = top 20% (mejores), Score 1 = bottom 20% (peores)
```

> **Nota sobre ORDER BY en R**: `ORDER BY recency_days ASC` con NTILE asigna score 5 al cliente más reciente (menor número de días). Verificar la dirección en la implementación para no invertir la lógica.

#### Paso 4: RFM combinado y segmentación

```sql
SELECT
    cliente_id,
    r_score,
    f_score,
    m_score,
    CAST(r_score AS VARCHAR) || CAST(f_score AS VARCHAR) || CAST(m_score AS VARCHAR) AS rfm_code,
    (r_score + f_score + m_score) AS rfm_sum,
    CASE
        -- Champions: alta R, alta F, alta M
        WHEN r_score >= 4 AND f_score >= 4 AND m_score >= 4 THEN 'Champions'

        -- Leales: alta F y M, recency aceptable
        WHEN r_score >= 3 AND f_score >= 4 AND m_score >= 3 THEN 'Leales'

        -- Potenciales: recientes pero baja frecuencia (compraron 1-2 veces)
        WHEN r_score >= 4 AND f_score <= 2                  THEN 'Potenciales'

        -- En riesgo: antes leales, ahora con baja recency
        WHEN r_score <= 2 AND f_score >= 3 AND m_score >= 3 THEN 'En Riesgo'

        -- No perderlos: alta F y M histórica pero sin comprar recientemente
        WHEN r_score = 1 AND f_score >= 4 AND m_score >= 4  THEN 'No Perderlos'

        -- Hibernando: poca actividad, bajo valor
        WHEN r_score <= 2 AND f_score <= 2 AND m_score <= 2 THEN 'Hibernando'

        -- Perdidos: todo en mínimos
        WHEN r_score = 1 AND f_score = 1 AND m_score = 1    THEN 'Perdidos'

        ELSE 'Medio'
    END AS segmento
FROM rfm_scored;
```

#### Cómputo de fronteras de quintiles (alternativa manual, sin NTILE)

Si la base de datos no soporta `NTILE` (Firebird < 3.0 no lo tiene; Firebird 3.0+ sí lo soporta con `ROW_NUMBER()`):

```sql
-- Alternativa usando PERCENT_RANK o ROW_NUMBER / COUNT(*)
-- Firebird 3.0+ tiene ROW_NUMBER() OVER
SELECT
    cliente_id,
    recency_days,
    CEILING(
        5.0 * ROW_NUMBER() OVER (ORDER BY recency_days ASC)
            / COUNT(*) OVER ()
    ) AS r_score
FROM rfm_raw;
```

La frontera del quintil k es el percentil `(k/5) * 100`. Ejemplo para 1,000 clientes:
- Quintil 5 (top 20%): clientes del rank 1 al 200
- Quintil 4: ranks 201–400
- Quintil 3: ranks 401–600
- Quintil 2: ranks 601–800
- Quintil 1: ranks 801–1000

**Firebird 3.0+ tiene soporte de window functions**; verificar versión del servidor antes de elegir método.

---

### Ajuste de M para Crédito a Plazos (tema central)

La literatura estándar define M como "total gastado". En crédito a plazos, esto subestima el valor real del cliente porque:

1. **El precio de crédito > precio de contado** en un diferencial que suele ser 30–80% según el plazo. Este diferencial es margen del negocio, no de un banco externo.
2. **Un cliente de crédito que repite** genera prima financiera acumulada en cada contrato nuevo, no solo en el primero.
3. **El riesgo crediticio** (clientes con morosidad alta) podría justificar un descuento al M, pero esto es función del estado de cartera, no del RFM de compra.

**Fórmula M ajustada para crédito a plazos:**

```
M_ajustado(i) = SUM(precio_credito_contrato_j)  para cada contrato j del cliente i
              = SUM(precio_contado_j + prima_financiera_j)

Prima financiera neta (cliente i) = M_ajustado(i) - SUM(precio_contado_j)
```

Se recomienda calcular **dos variantes de M** y almacenarlas ambas:
- `M_total_credito`: valor bruto del crédito otorgado (ingreso potencial máximo)
- `M_prima_neta`: solo el diferencial precio_crédito − precio_contado (margen financiero puro)

Usar `M_total_credito` para el score RFM, y `M_prima_neta` para análisis de rentabilidad por segmento.

> **Verificación adversarial**: El paper de ResearchGate sobre RFM en crédito personal define M como "total amount of expenses" (gasto total) en el contexto de tarjetas de crédito. Ninguna fuente encontrada propone explícitamente la prima financiera como componente de M en crédito minorista. Esta adaptación es **razonada desde primeros principios** para el modelo de negocio específico de este retailer. **[A MEDIR NOSOTROS]**: El diferencial precio_crédito − precio_contado real por plazo y categoría de producto.

---

### Pitfalls

1. **M es el menos predictivo de los tres scores.** Múltiples fuentes coinciden en que Recency es el predictor más fuerte de comportamiento futuro, seguido de Frequency. No compensar R bajo con M alto: un cliente que gastó mucho pero no compra en 18 meses es un cliente perdido, no un "Champion".

2. **NTILE con empates produce resultados inconsistentes.** Si muchos clientes tienen la misma frecuencia (p.ej. 50% tiene frequency=1), NTILE puede asignar scores distintos a clientes idénticos. Mitigación: usar `RANK()` y luego mapear a quintiles por rango percentual.

3. **Clientes de crédito activo vs. clientes que compraron.** Un cliente con un crédito vigente pero sin nueva compra puede tener recency alta (no compra nueva) pero sigue siendo un cliente activo en cartera. El RFM de compra y el estado de cartera son dimensiones ortogonales; deben calcularse por separado y combinarse en la vista de cliente.

4. **Ventana temporal fija ignora clientes estacionales.** Si hay estacionalidad (quincenas de enero/julio, Buen Fin), un cliente que compra cada diciembre tiene recency alta en febrero pero no es un cliente en riesgo real. Mitigación: comparar con el mismo mes del año anterior.

5. **Inflation de M por renegociaciones o descuentos.** Si un contrato fue renegociado y el precio_crédito bajó retroactivamente, M puede inflarse o deflarse. Usar el precio del contrato original, no el estado actual de la deuda.

6. **Quintiles en bases pequeñas.** Con menos de 200 clientes activos, los quintiles tienen solo ~40 clientes cada uno y el análisis pierde estabilidad. Usar cuartiles (1–4) o incluso tercios (1–3) y validar que los segmentos sean accionablemente distintos.

---

### Fuentes

- [RFM Segmentation: Practical Guide — MCP Analytics](https://mcpanalytics.ai/articles/rfm-segmentation-practical-guide-for-data-driven-decisions)
- [RFM Analysis for Customer Segmentation — Omniconvert](https://www.omniconvert.com/blog/rfm-analysis/)
- [RFM Analysis: The Ultimate Guide — Rejoiner](https://www.rejoiner.com/resources/rfm-analysis)
- [RFM Segmentation Guide — Count.co](https://count.co/metric/rfm-segmentation)
- [Customer Segmentation of Personal Credit using RFM — ResearchGate](https://www.researchgate.net/publication/370486268_Customer_Segmentation_of_Personal_Credit_using_Recency_Frequency_Monetary_RFM_and_K-means_on_Financial_Industry)
- [RFM en Retail Español — Unica360](https://www.unica360.com/analisis-rfm-en-retail-empezando-a-segmentar-clientes-i)
- [RFM Analysis Comprehensive Guide — CleverTap](https://clevertap.com/blog/rfm-analysis/)
- [How to Use RFM in Banking — SouthState](https://southstatecorrespondent.com/banker-to-banker/bank-marketing/how-to-use-rfm-customer-segmentation-analysis-in-banking/)
- [RFM Scoring Limitations — Dotdigital](https://support.dotdigital.com/en/articles/8199270-rfm-scoring-and-how-to-use-it)

---

## 6. Customer Lifecycle States

### Recomendación

Los estados de ciclo de vida deben estar **anclados al ciclo de recompra del negocio** (~11 meses), no a valores genéricos de la industria. La clave es distinguir:

- **Estados de cartera** (contrato vigente, vencido, castigado): reflejan el estado de la deuda.
- **Estados de comportamiento de compra** (nuevo, activo, dormido, frío, perdido): reflejan la probabilidad de próxima compra.

Ambos son necesarios y complementarios. El sistema de inteligencia de clientes debe computar ambos y combinarlos en una vista unificada.

---

### Definiciones y Umbrales

El ciclo de recompra de ~11 meses define la unidad de referencia `T_rep = 335 días`.

La regla de "attrition trigger personalizado" de la literatura (Towards Data Science) dice:

```
Umbral_churn(cliente_i) = 3 × AVG(días entre compras de cliente_i)
```

Para clientes con solo una compra (sin historial de frecuencia), usar el percentil 75 de la distribución de días-entre-compras de toda la base.

| Estado | Nombre en sistema | Definición operacional | Umbral (baseline ~11 meses) |
|--------|-------------------|------------------------|-----------------------------|
| **Nuevo** | `nuevo` | Primera compra hace ≤ 90 días, sin compra previa | 0–90 días post-primera-compra |
| **Activo** | `activo` | Última compra hace ≤ T_rep × 0.5 | ≤ 168 días (~5.5 meses) |
| **Por liquidar** | `por_liquidar` | Tiene contrato de crédito vigente con saldo > 0 y último pago al corriente | Estado de cartera (no de recency) |
| **Dormido** | `dormido` | Última compra hace > T_rep × 0.5 y ≤ T_rep × 1.5 | 169–502 días (~5.5–16.5 meses) |
| **Frío** | `frio` | Última compra hace > T_rep × 1.5 y ≤ T_rep × 2.5 | 503–838 días (~16.5–28 meses) |
| **Castigado** | `castigado` | Estado de cartera: deuda incobrable, jurídico, o castigo contable | Estado de cartera |
| **Perdido / Churned** | `perdido` | Última compra hace > T_rep × 2.5 | > 838 días (~28 meses) |

> **[A MEDIR NOSOTROS]**: Los múltiplos exactos (0.5×, 1.5×, 2.5×) son un punto de partida razonado; la calibración correcta requiere analizar la distribución real de `días_entre_compras` de la base histórica. El umbral de "dormido→frío" debe quedar en el P75 de esa distribución, y "frío→perdido" en el P90.

---

### Lógica de Transición

```
                       ┌─────────────────────────────────────────────────────────────┐
                       │               NUEVA COMPRA en cualquier estado               │
                       └──────────────────────────┬──────────────────────────────────┘
                                                  ▼
         Ingreso           ≤ 90d                activo              > 168d       dormido
         ──────►  nuevo  ───────► activo ◄──────────────────────────────── ◄─────────────
                              │                                         │
                              │  168d sin compra                        │ 503d sin compra
                              ▼                                         ▼
                           dormido ─────────────────────────────────► frío
                                          502d sin compra
                                                                        │ 838d sin compra
                                                                        ▼
                                                                     perdido

         Estado cartera (independiente):
         contrato con saldo → por_liquidar
         deuda incobrable   → castigado
```

**Reglas de precedencia:**
1. Si un cliente tiene estado `castigado` en cartera, ese estado domina sobre cualquier estado de comportamiento de compra.
2. `por_liquidar` es un estado de cartera que **coexiste** con un estado de comportamiento. Un cliente puede ser `activo + por_liquidar` (tiene contrato vigente y compró recientemente).
3. La transición de `perdido` → `activo` se da cuando el cliente realiza una nueva compra (reactivación). Se registra como evento separado para medir tasa de winback.

---

### Derivación desde el Ledger Transaccional

```sql
-- Vista de estado de comportamiento de compra
SELECT
    c.cliente_id,
    MAX(v.fecha_venta)                              AS ultima_compra,
    DATEDIFF(DAY, MAX(v.fecha_venta), CURRENT_DATE) AS dias_sin_compra,
    COUNT(DISTINCT v.venta_id)                      AS total_compras,
    MIN(v.fecha_venta)                              AS primera_compra,
    CASE
        WHEN COUNT(DISTINCT v.venta_id) = 1
         AND DATEDIFF(DAY, MIN(v.fecha_venta), CURRENT_DATE) <= 90
        THEN 'nuevo'

        WHEN DATEDIFF(DAY, MAX(v.fecha_venta), CURRENT_DATE) <= 168
        THEN 'activo'

        WHEN DATEDIFF(DAY, MAX(v.fecha_venta), CURRENT_DATE) <= 502
        THEN 'dormido'

        WHEN DATEDIFF(DAY, MAX(v.fecha_venta), CURRENT_DATE) <= 838
        THEN 'frio'

        ELSE 'perdido'
    END AS estado_comportamiento

FROM clientes c
LEFT JOIN ventas v ON v.cliente_id = c.cliente_id
WHERE v.fecha_venta <= CURRENT_DATE  -- excluir futuras
  AND v.estado_venta <> 'cancelada'  -- excluir canceladas
GROUP BY c.cliente_id;
```

```sql
-- Estado de cartera (complementario)
SELECT
    cr.cliente_id,
    CASE
        WHEN cr.estado_juridico = true        THEN 'castigado'
        WHEN cr.saldo_pendiente > 0
         AND cr.dias_vencido = 0              THEN 'por_liquidar'
        WHEN cr.saldo_pendiente > 0
         AND cr.dias_vencido > 0              THEN 'vencido'
        ELSE 'sin_saldo'
    END AS estado_cartera

FROM creditos cr
WHERE cr.saldo_pendiente > 0;
```

---

### Ajuste al Attrition Trigger Personalizado

Para clientes con historial de múltiples compras, usar el trigger personalizado es más preciso que los umbrales fijos de tabla:

```sql
-- Para cada cliente, calcular el AVG de días entre compras
WITH compras_ordenadas AS (
    SELECT
        cliente_id,
        fecha_venta,
        LAG(fecha_venta) OVER (PARTITION BY cliente_id ORDER BY fecha_venta)
            AS fecha_compra_anterior
    FROM ventas
    WHERE estado_venta <> 'cancelada'
),
intervalos AS (
    SELECT
        cliente_id,
        AVG(DATEDIFF(DAY, fecha_compra_anterior, fecha_venta)) AS avg_dias_entre_compras
    FROM compras_ordenadas
    WHERE fecha_compra_anterior IS NOT NULL
    GROUP BY cliente_id
)
SELECT
    i.cliente_id,
    i.avg_dias_entre_compras,
    i.avg_dias_entre_compras * 3 AS umbral_churn_personalizado
FROM intervalos i;
```

Un cliente con `avg_dias_entre_compras = 200` tiene su umbral de churn en 600 días — diferente al umbral genérico. Este approach es preferible para los clientes con ≥ 3 compras históricas.

---

### Pitfalls

1. **Confundir estado de cartera con estado de comportamiento.** Un cliente `castigado` (deuda incobrable) puede haber hecho compras recientes en otro contrato. El sistema debe manejar ambas dimensiones por separado.

2. **Umbrales fijos para toda la base ignoran heterogeneidad.** La literatura (Customer Science, Towards Data Science) es unánime: los umbrales deben variar por segmento, categoría de producto o historial individual. Los valores de la tabla son puntos de partida, no verdades absolutas.

3. **Reclasificación prematura como perdido.** Si un cliente compra bienes de alta duración (cama, sala, comedor) es normal que no compre por 18–24 meses. El estado `frío` debe activar acciones de nurturing, no de recuperación agresiva. Solo `perdido` (> 28 meses) justifica campañas de winback intensivas.

4. **No registrar el evento de transición.** El valor de los estados no es solo el estado actual, sino cuándo ocurrió la transición. Almacenar `estado_anterior`, `estado_nuevo`, `fecha_transicion` en una tabla de historial permite medir tasas de conversión entre estados.

5. **Clientes sin ninguna compra en la ventana.** Los umbrales de tabla no aplican a clientes que entraron al CRM pero nunca compraron (leads). Deben tener su propio estado: `lead_frio` o equivalente, fuera del modelo de comportamiento post-compra.

---

### Fuentes

- [How Lifecycle Models Work: Entries, Exits and Thresholds — Customer Science](https://customerscience.com.au/customer-experience-2/how-lifecycle-models-work-entries-exits-and-thresholds/)
- [Customer Attrition: How to Define Churn — Towards Data Science](https://towardsdatascience.com/customer-attrition-how-to-define-churn-when-customers-do-not-tell-theyre-leaving-ddde83e24e8e/)
- [A Simple Six-Step Approach to Define Customer Churn in Retail — OCTAVE/Medium](https://medium.com/octave-john-keells-group/a-simple-six-step-approach-to-define-customer-churn-in-retail-f401e31e57c0)
- [5 Early Warning Signs a High-Value Customer is About to Churn — Lexer](https://www.lexer.io/blog/5-early-warning-signs-a-high-value-customer-is-about-to-churn)
- [Customer Lifecycle Segmentation — Amplitude](https://amplitude.com/explore/digital-marketing/customer-lifecycle-segmentation)
- [Customer Lifecycle Stages — Optimove](https://www.optimove.com/blog/customer-lifecycle-stages-insights-and-actions)
- [What is RFM Analysis & Why It Matters — MoEngage](https://www.moengage.com/blog/predicitve-segments-rfm-analysis/)

---

## 7. CLV/LTV for Installment Credit

### Recomendación

Para una base de miles de clientes sin infraestructura de ML, el enfoque recomendado es **CLV histórico por cohorte con retención observada**, enriquecido con la **prima financiera** como componente de revenue. El modelo BG/NBD es el gold standard probabilístico para negocios no contractuales, pero requiere suficiente historial (≥ 24 meses de datos, ≥ 2 compras por cliente promedio) y la librería `lifetimes` de Python. Ambos enfoques se documentan a continuación.

La adaptación clave para crédito a plazos: el revenue por cliente no es solo el precio de venta, sino el **precio total de crédito** que incluye la prima financiera. Esta prima es el equivalente funcional del ingreso por intereses en un banco.

---

### Fórmula/Algoritmo

#### Enfoque A: CLV Histórico Simple (sin ML, implementable en SQL/Excel)

**Fórmula base:**

```
CLV_historico(i) = SUM(margen_bruto_por_venta_j)   para todo j ∈ ventas de cliente i
```

Donde:

```
margen_bruto_venta_j = precio_credito_j × margen_operativo
                     = (precio_contado_j + prima_financiera_j) × margen_operativo
```

**[A MEDIR NOSOTROS]**: el margen operativo real por categoría de producto (mueble, colchón, electrodoméstico) porque varía significativamente.

**Fórmula con proyección a futuro (simple):**

```
CLV_proyectado(i) = CLV_historico(i) × (vida_estimada_en_periodos / vida_observada_en_periodos)
```

Donde `vida_estimada` se obtiene de la tabla de retención por cohorte.

---

#### Enfoque B: CLV por Cohorte con Tasa de Retención Observada

Este método usa la distribución real de retención de la base histórica:

```sql
-- Paso 1: Definir cohortes por mes de primera compra
SELECT
    DATE_TRUNC('month', primera_compra) AS cohorte_mes,
    cliente_id
FROM (
    SELECT cliente_id, MIN(fecha_venta) AS primera_compra
    FROM ventas GROUP BY cliente_id
) sub;

-- Paso 2: Para cada cohorte, calcular % de clientes activos en cada mes subsequente
-- "Activo en mes N" = hizo al menos una compra en mes N de su ciclo de vida
-- (o alternativamente: tiene saldo de crédito vigente en mes N)
```

**Fórmula de retención por cohorte:**

```
retention_rate(cohorte, t) = clientes_activos_en_mes_t / tamaño_cohorte_inicial
```

**Fórmula de CLV por cohorte (con descuento):**

```
CLV_cohorte = SUM[ margen_mes_t × retention_rate(t) / (1 + d)^t ]
              para t = 1 a T_horizonte
```

Donde `d` es la tasa de descuento mensual (típicamente 1–2% mensual para México en 2026, equivalente a ~12–27% anual).

**[A MEDIR NOSOTROS]**: la tasa de retención real por cohorte de este negocio. La literatura de e-commerce reporta 60–80% de lapse en 12 meses, pero un negocio de crédito a plazos tiene dinámica diferente: el cliente con contrato vigente es "retenido" mecánicamente mientras paga.

---

#### Enfoque C: BG/NBD + Gamma-Gamma (modelo probabilístico)

**Cuándo usarlo**: cuando ≥ 40% de la base tiene 2+ compras históricas y se dispone de ≥ 24 meses de datos.

**Inputs requeridos por cliente:**
- `frequency`: número de compras repetidas (compras totales − 1)
- `recency`: semanas entre primera y última compra
- `T`: semanas desde la primera compra hasta el análisis
- `monetary_value`: valor promedio por transacción (usar precio_credito)

**Modelo BG/NBD (Buy Till You Die):**

Parámetros poblacionales estimados por MLE: `r, α` (proceso de compra Gamma-Poisson) y `a, b` (proceso de abandono Beta-Geométrico).

La implementación práctica usa la librería `lifetimes`:

```python
from lifetimes import BetaGeoFitter, GammaGammaFitter

# Preparar datos: un registro por cliente
# frequency, recency, T deben estar en la misma unidad (semanas recomendado)
rfm_data = ventas_df.groupby('cliente_id').agg(
    frequency    = ('venta_id', 'count'),       # total compras
    recency      = ...,                          # semanas primera→última compra
    T            = ...,                          # semanas primera compra → hoy
    monetary_value = ('total_credito', 'mean'),  # promedio precio_crédito por compra
)
rfm_data['frequency'] = rfm_data['frequency'] - 1   # compras REPETIDAS

# Ajustar modelo BG/NBD
bgf = BetaGeoFitter(penalizer_coef=0.001)
bgf.fit(rfm_data['frequency'], rfm_data['recency'], rfm_data['T'])

# Predecir compras esperadas en los próximos 12 meses (52 semanas)
rfm_data['expected_purchases_12m'] = bgf.conditional_expected_number_of_purchases_up_to_time(
    52, rfm_data['frequency'], rfm_data['recency'], rfm_data['T']
)

# Ajustar modelo Gamma-Gamma para valor monetario
# Filtrar clientes con frequency > 0 (al menos 2 compras)
ggf = GammaGammaFitter(penalizer_coef=0.01)
ggf.fit(rfm_data[rfm_data.frequency > 0]['frequency'],
        rfm_data[rfm_data.frequency > 0]['monetary_value'])

# CLV combinado a 12 meses con tasa de descuento mensual 1.5%
rfm_data['clv_12m'] = ggf.customer_lifetime_value(
    bgf,
    rfm_data['frequency'],
    rfm_data['recency'],
    rfm_data['T'],
    rfm_data['monetary_value'],
    time=12,         # meses
    freq='W',        # frecuencia base de datos = semanas
    discount_rate=0.015  # 1.5% mensual ~ 19.6% anual
)
```

**Interpretación del output**: `clv_12m` es el valor presente esperado de las compras futuras de cada cliente en los próximos 12 meses, expresado en la misma moneda que `monetary_value` (precio_crédito).

---

#### Incorporación de la Prima Financiera en el CLV

La prima financiera es **ingreso real del negocio** y debe incluirse en el CLV. El ajuste es directo:

```
revenue_cliente_por_compra = precio_credito
                           = precio_contado + prima_financiera

margen_por_compra = revenue_cliente_por_compra × margen_operativo_neto
```

Por tanto, al usar `monetary_value = precio_credito_promedio` (en lugar de `precio_contado`), el modelo BG/NBD + Gamma-Gamma ya captura la prima financiera automáticamente, siempre que el margen_operativo aplicado sea el margen sobre precio_credito (no sobre precio_contado).

**Ejemplo numérico:**
- Mueble con precio_contado = $10,000 MXN
- Prima financiera (12 meses): $3,500 MXN
- precio_credito = $13,500 MXN
- Margen bruto sobre precio_credito (ejemplo): 40%
- Margen por compra = $13,500 × 0.40 = $5,400 MXN

Si se usara solo precio_contado como M, el margen sería $10,000 × 0.40 = $4,000 MXN — **subestimando el CLV en $1,400 MXN por compra** (35% menos).

---

#### CLV Neto (descontando costo de mora y castigo)

Para clientes en riesgo de cartera, el CLV bruto debe ajustarse por la probabilidad de incumplimiento:

```
CLV_neto(i) = CLV_bruto(i) × (1 − P_default(i)) − Costo_cobranza(i)
```

Donde `P_default(i)` es la probabilidad histórica de castigo del segmento de cliente. **[A MEDIR NOSOTROS]**: la tasa de castigo real por segmento de antigüedad y monto de crédito.

---

### Comparativa: Histórico Simple vs. BG/NBD

| Criterio | CLV Histórico Simple | BG/NBD + Gamma-Gamma |
|----------|---------------------|----------------------|
| **Datos mínimos** | 12+ meses, cualquier frecuencia | 24+ meses, ≥ 40% con 2+ compras |
| **Implementación** | SQL/Excel | Python + librería `lifetimes` |
| **Distingue activos de churned** | No (asume que todos están "vivos") | Sí (probabilidad de estar activo) |
| **Proyección individual** | Solo promedio de cohorte | Por cliente individual |
| **Precisión reportada** | Baja-media | ~75% vs ML (~85%) |
| **Cuándo usar** | Inicio, base pequeña, datos escasos | Cuando hay suficiente historial |

**Recomendación de implementación**: comenzar con el histórico por cohorte (Enfoque B) como baseline operacional. Implementar BG/NBD (Enfoque C) cuando la base tenga ≥ 24 meses de historial y al menos 300 clientes con 2+ compras.

**Advertencia adversarial**: la literatura de SaaS y subscription cita CLV fórmulas basadas en `Churn Rate = 1 − Retention`. Estas fórmulas **no aplican** directamente a negocios no-contractuales como una mueblería de crédito, porque los clientes no "cancelan" explícitamente y el churn debe inferirse. Usar la fórmula `CLV = ARPU / Churn_Rate` con churn calculado como "no compró en 12 meses" produce estimaciones muy diferentes de las de cohorte. **Usar siempre el modelo de cohorte o BG/NBD para contextos no-contractuales**.

---

### Pitfalls

1. **Asumir que "activo en cartera" = "cliente activo".** Un cliente que paga sus cuotas mensualmente pero no vuelve a comprar tiene recency alta (no hay nueva compra). El CLV de compra y el ingreso de cartera son distintos. El modelo BG/NBD predice compras nuevas, no pagos de contratos existentes.

2. **Usar precio_contado como M ignorando la prima.** Como se demostró arriba, esto subestima el CLV entre 20–50% dependiendo del plazo y tasa de financiamiento.

3. **BG/NBD asume procesos de Poisson.** En mueblería, las compras son eventuales (bed, sala, comedor) y el proceso puede no seguir Poisson sino un proceso más irregular. Validar que el modelo ajuste bien con el método de holdout: usar datos hasta mes T−6, predecir los últimos 6 meses, y comparar contra observado.

4. **Clientes con frequency = 0 (solo una compra) dominan la base.** En muchos retails, 60–70% de clientes compran solo una vez. El BG/NBD los trata como "posiblemente muertos" con probabilidad alta. Esto es correcto estadísticamente, pero el CLV de este segmento es casi cero. No promediar este segmento con el resto para no inflar las expectativas.

5. **Tasa de descuento inapropiada para México.** Un descuento de 10% anual (estándar en literatura anglosajona) es muy bajo para México 2026, donde la tasa libre de riesgo es ~10–11% (CETES) y el costo de capital de un negocio minorista es mayor. Usar mínimo 15–20% anual (≈ 1.2–1.5% mensual) para descontar flujos futuros.

6. **Inflación erosiona el CLV proyectado.** En entornos de inflación de 4–6% anual (México 2025–2026), los flujos futuros en pesos nominales no son comparables con los históricos. El CLV calculado en pesos corrientes debe deflactarse para comparaciones intertemporales.

---

### Fuentes

- [BG/NBD and Gamma-Gamma Models in Python — Analytics Vidhya/Medium](https://medium.com/analytics-vidhya/customer-life-time-value-prediction-by-using-bg-nbd-gamma-gamma-models-and-applied-example-in-997a5ee481ad)
- [Customer Lifetime Value via Probabilistic Modeling — Towards Data Science](https://towardsdatascience.com/customer-lifetime-value-estimation-via-probabilistic-modeling-d5111cb52dd/)
- [Buy Till You Die: CLV in Python — Towards Data Science](https://towardsdatascience.com/buy-til-you-die-predict-customer-lifetime-value-in-python-9701bfd4ddc0/)
- [BG/NBD vs Other CLV Models: Pros and Cons — FasterCapital](https://fastercapital.com/content/BG-NBD-Model--BG-NBD-vs--Other-Customer-Lifetime-Models--Pros-and-Cons.html)
- [CLV Formulas — CLV-Calculator.com](https://www.clv-calculator.com/customer-lifetime-value-formulas/clv-formula/)
- [Calculating CLV for Banks (interest income model) — CLV-Calculator.com](https://www.clv-calculator.com/calculating-value-for-banks/)
- [CLV Practical Guide — MCP Analytics](https://mcpanalytics.ai/articles/customer-lifetime-value-ltv-practical-guide-for-data-driven-decisions)
- [CLV: Complete Guide for Marketing Analysts — Improvado](https://improvado.io/blog/clv-guide)
- [Lifetimes Python Package Documentation](https://lifetimes.readthedocs.io/en/latest/lifetimes.html)
- [What is CLV — Wall Street Prep](https://www.wallstreetprep.com/knowledge/lifetime-value-ltv/)
- [Non-contractual CLV — Number Analytics](https://www.numberanalytics.com/blog/what-is-customer-lifetime-value-clv-models-and-applications)

---

*Documento generado: 2026-06-13. Requiere revisión con datos reales del negocio para calibrar umbrales marcados como [A MEDIR NOSOTROS].*
