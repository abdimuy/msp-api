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
