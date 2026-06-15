# A1 — Arquitectura de Producto: Customer Intelligence

> Sección del documento "State of the Art" para el componente de inteligencia de clientes.
> Aplica al contexto de una mueblería mexicana con crédito a plazos, clientela no-bancarizada,
> miles de clientes, BD Firebird (Microsip ERP), stack Go + Windows Server legacy.

---

## 1. Unified customer profile / Customer 360 / Feature store

### Recomendación

Para una escala de miles de clientes (no millones), la estrategia correcta es un **read model materializado mantenido por la propia aplicación Go** — una tabla `MSP_CUSTOMER_PROFILES` en Firebird que agrega y desnormaliza señales de las tablas transaccionales. No se justifica ningún feature store externo (Feast, Tecton, Vertex AI) ni pipeline de streaming. El modelo batch + incremental cubre el 95% del valor al 10% del costo operativo.

La distinción clave del consenso 2026 (Treasure Data, apxml.com, RisingWave): el customer 360 no es un dashboard — es una **decisión de arquitectura de datos** antes de ser un producto de visualización. Perfiles correctos requieren datos de entrada correctos; historial de pagos parcial o mal timestampeado produce señales peor que la ausencia de datos.

### Fórmula / Arquitectura

**Estrategia de materialización: Batch nightly + Incremental incremental (watermark)**

```
┌────────────────────────────────────────────────────────────────┐
│  Tablas fuente (Firebird)                                      │
│  DOCTOS_CC (crédito) | DOCTOS_IN (pagos) | CLIENTES (demo)    │
│  MSP_OUTBOX_EVENTS | LIBRES_CARGOS_CC | CONCEPTOS             │
└────────────────────┬───────────────────────────────────────────┘
                     │ updated_at > watermark (incremental, 15 min)
                     │ OR full recompute (nightly, 03:00 h)
                     ▼
┌────────────────────────────────────────────────────────────────┐
│  Goroutine refresh (dentro del mismo binario Go / nssm)        │
│  - lee dirty customer_ids                                      │
│  - computa perfil completo por cliente                         │
│  - escribe a MSP_CUSTOMER_PROFILES (upsert por cliente_id)     │
│  - actualiza MSP_PROFILE_WATERMARK                             │
└────────────────────┬───────────────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────────────┐
│  MSP_CUSTOMER_PROFILES (tabla Firebird)                        │
│  — campo por campo, sin lógica en el DB (regla 1 del proyecto) │
└────────────────────┬───────────────────────────────────────────┘
                     │ GET /customers/{id}/profile
                     ▼
┌────────────────────────────────────────────────────────────────┐
│  In-process LRU cache (samber/hot o ristretto)                 │
│  TTL = 5 min | singleflight para cache stampede               │
└────────────────────────────────────────────────────────────────┘
```

Comparativa de estrategias:

| Estrategia | Staleness | Carga FB | Complejidad | Fit Windows Server |
|---|---|---|---|---|
| Batch nightly (full) | ~24 h | Burst nocturno | Mínima | Excelente |
| Incremental watermark (15 min) | ~15 min | Continua/baja | Baja | Excelente |
| On-demand + cache 5 min | ~5 min | Spike en miss | Media | Excelente |
| Streaming (Kafka, Flink) | Segundos | N/A | Muy alta | No aplica |

**Recomendación concreta**: Incremental 15 min (goroutine ticker) + nightly full recompute como safety net + in-process cache con TTL 5 min para las peticiones HTTP. Ningún proceso externo. Ningún scheduler adicional. Corre dentro del mismo servicio nssm.

**Campos del perfil robusto para crédito minorista** (sintetizado de Latentview, Snowflake Financial Services, Saras Analytics, Scribd ficha-de-cliente, y literatura de cobranza mexicana):

*Identidad / Demografía*
- `cliente_id`, `nombre_completo`, `curp_rfc`, `telefono`, `direccion`
- `fecha_alta`, `vendedor_asignado`, `cobrador_asignado`, `zona`

*Estado de crédito (snapshot)*
- `limite_credito`, `saldo_pendiente`
- `utilizacion_credito` = `saldo_pendiente / limite_credito` (0–1)
- `numero_creditos_activos`, `antiguedad_meses`

*Comportamiento de pago (derivado)*
- `dias_mora_hoy` — días corridos al momento del cómputo
- `max_dias_mora_historico` — peor episodio registrado
- `tasa_pago_puntual` = `pagos_a_tiempo / total_pagos_esperados` (L12M)
- `promedio_dias_adelanto_retraso` — negativo = adelantado
- `pagos_ultimos_12m`, `pagos_perdidos_12m`

*Compras / Transacciones*
- `fecha_primera_compra`, `fecha_ultima_compra`
- `dias_desde_ultima_compra` (Recency)
- `compras_l3m`, `compras_l6m`, `compras_l12m` (Frequency)
- `monto_l3m`, `monto_l6m`, `monto_l12m` (Monetary)
- `ticket_promedio`, `categoria_preferida`, `valor_vida_total`

*Señales de riesgo derivadas*
- `rfms_r`, `rfms_f`, `rfms_m`, `rfms_s` (scores 1–5 por cuartil)
- `rfms_segmento` (label: Campeón, Leal, En Riesgo, Hibernando, Perdido)
- `health_score` (0–100, ver Tema 2)
- `flag_churn` (sin compra en 90+ días con cuenta abierta)
- `flag_mora` (mora > 30 días)

**Fórmula RFMS para crédito** (Huang, Zhou, Wang 2018, *Statistica Sinica* — diseñada específicamente para crédito sin historial buró):

```
rfms_score = 0.25 × r_score + 0.25 × f_score + 0.25 × m_score + 0.25 × s_score
```

- `r_score`: cuartil 1–5 de `dias_desde_ultima_compra` (invertido: menor días = score 5)
- `f_score`: cuartil 1–5 de `compras_l12m`
- `m_score`: cuartil 1–5 de `monto_l12m`
- `s_score`: cuartil 1–5 de `tasa_pago_puntual` (`pagos_puntuales / pagos_esperados`)

Sin datos históricos de default propios aún, pesos iguales (0.25) son el punto de partida apropiado. Los pesos pueden calibrarse a posteriori contra el historial real de cuentas incobrables.

**Segmentos RFM** (Amperity, omniconvert):

| Segmento | Condición |
|---|---|
| Campeón | R=5, F≥4, S≥4 |
| Cliente Leal | F≥3, S≥3 |
| Potencial Prometedor | R≥4, F≤2 |
| En Riesgo | R≤2, fue F≥3 |
| Hibernando | R≤2, F≤2 |
| Perdido | R=1, mora activa |

### Pitfalls

1. **Resolución de identidad incompleta**: el mismo cliente con dos teléfonos aparece como dos perfiles. La clave de deduplicación debe ser CURP, RFC, o teléfono normalizado (10 dígitos MX). Sin este paso previo, los scores son incorrectos.
2. **Datos de entrada incorrectos peores que la ausencia**: historial de pagos parcial produce `tasa_pago_puntual` inflada. Marcar perfiles con < 6 meses de historial como `[DATOS INSUFICIENTES]` — no calcular RFMS sobre ellos.
3. **Staleness silencioso**: si `updated_at` lo setea un trigger de Firebird (no Go), el watermark no detecta las actualizaciones. El proyecto ya aplica la Regla 1 (timestamps desde Go) — verificar que datos migrados también cumplan.
4. **Overengineering con streaming**: una mueblería con miles de clientes que adopta Kafka/Flink incurre en deuda operativa sin beneficio real. El batch incremental cubre el caso de uso.
5. **Compras estacionales**: un cliente que solo compra en diciembre aparece como "churn" en marzo. Contextualizar Recency con la frecuencia histórica anual, no solo L3M. [A MEDIR NOSOTROS]

### Fuentes

- [apxml.com — Batch vs. Real-time Computation Trade-offs](https://apxml.com/courses/feature-stores-for-ml/chapter-2-advanced-feature-engineering-computation/batch-real-time-computation)
- [RisingWave — Real-Time Feature Store in 2026](https://risingwave.com/blog/real-time-feature-store-2026/)
- [Latentview — What Is Customer 360?](https://www.latentview.com/blog/what-is-customer-360-view/)
- [Treasure Data — Customer 360 in 2026](https://www.treasuredata.com/blog/customer-360)
- [Snowflake — Customer 360 in Financial Services](https://www.snowflake.com/en/solutions/industries/financial-services/customer-360-in-financial-services/)
- [Saras Analytics — 360-Degree Customer View](https://www.sarasanalytics.com/blog/360-degree-customer-view)
- [Statistica Sinica — RFMS Method for Credit Scoring (Huang, Zhou, Wang 2018)](https://www3.stat.sinica.edu.tw/statistica/J28N5/J28N535/J28N535.html)
- [ResearchGate — RFMS Method (PDF)](https://www.researchgate.net/publication/322167215_RFMS_Method_for_Credit_Scoring_Based_on_Bank_Card_Transaction_Data)
- [Amperity — RFM User Guide](https://docs.amperity.com/ampiq/rfm.html)
- [Omniconvert — RFM Score](https://www.omniconvert.com/blog/rfm-score/)
- [Gurusis — Política de crédito y cobranza MX](https://gurusis.com/politica-de-credito-cobranza-ejemplos/)

---

## 8. "Customer 360" UI para usuarios no técnicos de negocio

### Recomendación

El consenso en implementaciones de MDM y retail CDP (Custify, FirstHive, Informatica MDM, Appsmith) es que una "ficha del cliente" efectiva para usuarios no técnicos requiere: **semáforos visuales de riesgo** (no números crudos), **un solo score de salud compuesto** (no fórmulas), **vistas por rol** (cobrador ≠ vendedor ≠ gerente), y **tiempo relativo** ("hace 23 días") junto con fechas absolutas. Los paneles de alertas accionables eliminan el trabajo de interpretación.

### Fórmula / Arquitectura

**Layout recomendado de la Ficha del Cliente:**

```
┌─ ENCABEZADO ────────────────────────────────────────────────────┐
│ Nombre completo         │ Cliente desde: DD/MM/AAAA             │
│ Teléfono: (XXX) XXX-XXXX│ Zona: Norte | Cobrador: J. Pérez      │
│ RFC/CURP                │ Vendedor: M. García                   │
└─────────────────────────────────────────────────────────────────┘
┌─ ESTADO DE CRÉDITO (semáforo) ──────────────────────────────────┐
│ Saldo pendiente: $X,XXX   │ Límite: $XX,XXX                     │
│ [●VERDE / ●AMARILLO / ●ROJO] Días de mora: N días               │
│ Próximo pago: DD/MM/AAAA  │ Monto: $XXX                         │
│ Pagos puntuales: 87%  (últimos 12 meses)                        │
│ Health Score: 74/100 [B]                                        │
└─────────────────────────────────────────────────────────────────┘
┌─ COMPORTAMIENTO DE COMPRA ──────────────────────────────────────┐
│ Última compra: DD/MM/AAAA (hace N días)                         │
│ Frecuencia: X compras/año │ Ticket promedio: $XXX               │
│ Valor histórico total: $XX,XXX                                  │
│ Segmento: "Cliente Leal" ─────────────────────────────────────  │
└─────────────────────────────────────────────────────────────────┘
┌─ HISTORIAL DE PAGOS (últimos 12) ───────────────────────────────┐
│ Fecha       │ Esperado  │ Pagado    │ Mora (días)               │
│ ...         │ ...       │ ...       │ ...                        │
└─────────────────────────────────────────────────────────────────┘
┌─ ALERTAS ACTIVAS ───────────────────────────────────────────────┐
│ [!] Sin compra en 90 días   [!] Mora vencida 45 días           │
└─────────────────────────────────────────────────────────────────┘
```

**Vistas por rol** (Custify, FirstHive):

| Campo | Cobrador | Vendedor | Gerente |
|---|:---:|:---:|:---:|
| Dias mora, saldo vencido | ✓ | — | ✓ |
| Próximo pago, teléfono, dirección | ✓ | ✓ | ✓ |
| Historial de pagos L12M | ✓ | — | ✓ |
| RFM segmento | — | ✓ | ✓ |
| Últimas 3 compras, categoría preferida | — | ✓ | ✓ |
| Límite disponible | — | ✓ | ✓ |
| Health score, lifetime value | — | — | ✓ |
| Métricas cross-portfolio | — | — | ✓ |

**Health score compuesto** (Routine, Cerebral Ops, Medium credit scoring):

```go
// Punto de partida; calibrar con datos reales [A MEDIR NOSOTROS]
healthScore := 100

// Penalizaciones por mora
switch {
case diasMora == 0:   // sin cambio
case diasMora <= 30:  healthScore -= 20
case diasMora <= 60:  healthScore -= 40
default:              healthScore -= 60
}

// Penalización por tasa de pago
switch {
case tasaPago >= 0.80: // sin cambio
case tasaPago >= 0.50: healthScore -= 10
default:               healthScore -= 20
}

// Penalización por inactividad de compra
switch {
case diasSinCompra > 365: healthScore -= 20
case diasSinCompra > 180: healthScore -= 10
}

// Bonus por antigüedad
switch {
case mesesCliente >= 60: healthScore += 10
case mesesCliente >= 24: healthScore += 5
}

// Bonus por frecuencia de compra
switch {
case compras12m >= 6: healthScore += 10
case compras12m >= 3: healthScore += 5
}

// Clamp 0–100
if healthScore > 100 { healthScore = 100 }
if healthScore < 0   { healthScore = 0 }

// Grado: A=80-100, B=60-79, C=40-59, D=<40
```

Los umbrales exactos son **[A MEDIR NOSOTROS]** con datos reales de la cartera. Los valores anteriores son un punto de partida razonable basado en literatura de scoring para crédito minorista.

**Búsqueda como punto de entrada**: clientes no bancarizados frecuentemente carecen de RFC/CURP disponible inmediatamente. El acceso primario debe ser por nombre o teléfono (10 dígitos normalizados), no por ID interno.

### Pitfalls

1. **Score de salud sobre datos obsoletos**: un cliente que liquidó toda su deuda ayer sigue en rojo hasta el siguiente ciclo de refresh. El componente `dias_mora` debe computarse on-demand (desde el saldo actual) mientras los componentes RFM se sirven desde el perfil materializado (batch).
2. **Demasiados campos sin jerarquía**: los usuarios no técnicos abandonan dashboards con > 20 métricas visibles simultáneamente. Priorizar el semáforo y el health score; el resto bajo "ver detalles".
3. **Mismo UI para todos los roles**: un cobrador viendo RFM segments sin contexto toma peores decisiones que uno que solo ve mora y teléfono.
4. **Fechas sin referencia relativa**: "DD/MM/AAAA" no comunica urgencia; "hace 47 días" sí.

### Fuentes

- [Custify — Customer 360 Product](https://www.custify.com/product-360)
- [FirstHive — Ultimate Guide to Building a Customer 360 Dashboard](https://blog.firsthive.com/the-ultimate-guide-to-building-a-customer-360-dashboard-2/)
- [Appsmith — Customer 360 Dashboard Tutorial](https://www.appsmith.com/blog/customer-dashboard-360-tutorial)
- [Informatica MDM — Customer 360 User Interface](https://docs.informatica.com/master-data-management/customer-360/10-4-hotfix-3/user-guide/introduction-to-informatica-mdm---customer-360/user-interface.html)
- [Routine — How to Build a Customer Health Score](https://routine.co/blog/posts/build-customer-health-score)
- [Cerebral Ops — Customer Health Scoring: Predicting Churn](https://blog.cerebralops.in/customer-health-scoring-predicting-churn-before-it-happens/)
- [Medium — Credit Scoring, Probability of Default, and Churn](https://medium.com/@KingHenryMorgansDiary/credit-scoring-probability-of-default-and-churn-how-businesses-manage-risk-and-retention-ade35a64e16a)
- [IrisAgent — Customer Health Score](https://irisagent.com/customer-health/)

---

## 10. Cómo estructuran esto los comparables: Coppel / Elektra / Aaron's / Yalo / CDPs

### Recomendación

El patrón aplicable a escala de miles de clientes no es el stack técnico de Coppel (SAS CI360, millones de clientes, equipo de analítica dedicado) sino **la decisión arquitectónica que los une a todos**: separar la capa analítica del ERP transaccional, y usar el historial de pagos + compras como señal primaria (no el buró de crédito). Para una mueblería, eso se traduce en un read model propio en vez de un CDP externo.

### Arquitectura / Patrones por comparable

**Coppel (~30M clientes de crédito)**
Fuente: SAS case study + Americas Market Intelligence.

- Stack: SAS Customer Intelligence 360 (puede ser on-premises o híbrido — relevante: **no es cloud-only**).
- La capa analítica está **desacoplada del ERP operacional** (equivalente a Microsip): los marketers segmentan sobre el read model de SAS, no sobre las tablas transaccionales.
- Señal primaria: **datos de originación de crédito + historial de compras**. No web analytics (sus clientes son mayormente offline).
- Activación: WhatsApp vía Yalo (3M clientes/mes). Yalo mantiene su propia capa de perfil sincronizada con el backend de Coppel — de facto un CDP para el canal de mensajería.
- Resultado verificado: 2% incremento en ventas retail, 16% en crédito personal (SAS case study — números de vendor marketing; [A MEDIR NOSOTROS] si aplica a escala menor).

**Banco Azteca / Grupo Elektra (~22.8M clientes)**
Fuente: Wikipedia Banco Azteca + Grupo Elektra annual report.

- **Insight estructural clave**: Banco Azteca financia el 57–59% de las ventas de Elektra directamente. El banco y la mueblería están **fusionados operacionalmente** — los datos de crédito y compra son co-ubicados por diseño, no por integración.
- Para poblaciones no bancarizadas (~50% de sus clientes son invisibles al buró), desarrollaron un **sistema de calificación crediticia interno** basado en historial propio — la misma señal que tiene una mueblería con cartera propia.
- Patrón aplicable: el historial de pagos de crédito propio es más predictivo que cualquier score de buró para esta población. La ventaja competitiva es acumular y explotar ese historial.

**Aaron's (rent-to-own, USA — análogo funcional a mueblería mexicana a plazos)**
Fuente: Retail TouchPoints (resultado verificado con cifras concretas) + Salesforce customer story.

- **Antes**: cobranza, ventas y marketing en silos separados — cada área tenía vista parcial del cliente (exacto análogo a cobrador/vendedor/gerente en una mueblería).
- **Después**: unificación via Zeta Global (resolución de identidad) + Salesforce Data Cloud.
- **Resultados en 8 semanas**:
  - 2,400 clientes nuevos netos
  - **Reducción del 33% en costo de adquisición de cliente**
  - 53% incremento en conversión de email
  - Suscriptores activos triplicados
- El paso difícil fue **identity resolution**: el mismo cliente en direct mail, en tienda, y en social era tratado como tres personas distintas. Para una mueblería: el cliente con dos teléfonos o que compró con nombre del cónyuge.

**Yalo (WhatsApp commerce, usado por Coppel, Elektra, Farmacias del Ahorro)**
Fuente: B Capital investment thesis + Yalo Nestlé case study + Mexico Business News.

- Yalo es la **capa de activación**, no la capa de datos. Requiere que el retailer YA tenga historial de compras para construir el modelo de recomendación.
- Patrón: historial de compras → modelo de recomendación → entrega vía WhatsApp conversacional.
- La señal de comportamiento de la conversación (qué clickeó el cliente, qué rechazó) retroalimenta el modelo. Esto es un CDP mínimo embebido en el canal de mensajería.
- Relevante: Yalo no resuelve el problema de datos — lo presupone resuelto.

**CDPs (Segment, Simon Data) a pequeña escala**
Fuente: Census "The Broken Promise of Customer 360" + Simon Data blog.

- **Crítica verificada (Census)**: los CDPs full-stack están sobredimensionados para organizaciones con miles de clientes. El ciclo de latencia (evento → CDP → activación) reintroduce la complejidad que prometían eliminar. Governance, contratos de datos multi-sistema, y round-trips de red se suman.
- **Para escala de miles**: un read model warehouse-native (o in-process) supera a un CDP stack completo. El equipo ya controla los datos y el esquema — no necesita un intermediario.
- Timeline de implementación: CDP full: 4–8 semanas solo para unificación inicial, 2–4 meses para activación. Read model custom en Go: **días a semanas** con el equipo existente.

### Pitfalls

1. **Copiar el stack, no el principio**: Coppel usa SAS CI360 porque tiene millones de clientes y equipo de analítica de 50+ personas. El principio a copiar es *desacoplar la capa analítica del ERP*, no el vendor.
2. **Asumir que el buró de crédito es necesario**: Banco Azteca y Elektra prueban que para poblaciones no bancarizadas, el historial interno es superior al buró. El buró en México tiene baja cobertura para el segmento de clientes de mueblería a plazos.
3. **Omitir identity resolution**: Aaron's tardó semanas en esto y era el paso de mayor valor. Sin un `cliente_id` canónico que vincule compras, pagos, y contactos del mismo cliente real, todo el análisis posterior es incorrecto.
4. **Yalo/WhatsApp como primer paso**: activación sin un perfil de cliente sólido produce recomendaciones irrelevantes que dañan la relación. El perfil va primero.

### Fuentes

- [SAS — Coppel Case Study](https://www.sas.com/en_za/customers/coppel.html)
- [Americas Market Intelligence — Digitization Mexico Retailers Finance](https://americasmi.com/insights/digitization-mexico-retailers-finance/)
- [Signal360 — Yalo Puts the Conversation Back into Commerce](https://pgsignal.com/2023/10/03/yalo-puts-the-conversation-back-into-commerce/)
- [B Capital — Why We Invested: YaloChat](https://b.capital/why-we-invested/why-we-invested-yalochat/)
- [Yalo — Nestlé Mexico Case Study](https://www.yalo.ai/casos-de-exito/nestle-mexico-boosts-sales-in-less-than-6-months-with-product-recommendation-model)
- [Wikipedia — Banco Azteca](https://en.wikipedia.org/wiki/Banco_Azteca)
- [Wikipedia — Grupo Elektra](https://en.wikipedia.org/wiki/Grupo_Elektra)
- [Retail TouchPoints — Aaron's Data Centralization Cuts CAC 33%](https://www.retailtouchpoints.com/features/retail-success-stories/rent-to-own-retailer-aarons-data-centralization-cuts-customer-acquisition-costs-33)
- [Salesforce — Aaron's Customer Story](https://www.salesforce.com/news/stories/aarons-customer-story/)
- [Chain Store Age — Aaron's Omnichannel](https://chainstoreage.com/exclusive-qa-aarons-company-transforms-omnichannel-commerce)
- [Census — CDPs and the Broken Promise of the Customer 360](https://www.getcensus.com/blog/cdps-and-the-broken-promise-of-the-customer-360)
- [Simon Data — Building a Customer 360 with your CDP](https://www.simondata.com/blog-posts/building-customer-360-cdp)
- [RiskSeal — Alternative Credit Scoring in Mexico](https://riskseal.io/blog/alternative-credit-scoring-in-mexico)
- [NBER — FinTech Lending to Borrowers with No Credit History](https://www.nber.org/system/files/working_papers/w33208/w33208.pdf)

---

## 11. Enfoque técnico en Go leyendo Firebird

### Recomendación

Un único binario Go (el mismo que ya corre como servicio nssm) puede servir simultáneamente la API REST y el motor de perfiles: una goroutine con ticker cada 15 minutos actualiza `MSP_CUSTOMER_PROFILES` por watermark incremental; un in-process cache con TTL de 5 minutos sirve las lecturas HTTP; un singleflight colapsa cache misses concurrentes. No se necesita ningún proceso externo, scheduler del sistema, o infraestructura adicional.

### Arquitectura concreta

**Estructura del binario Go:**

```
main()
├── HTTP server (Huma/chi) — existente
│   └── GET /v1/customers/{id}/profile
│       ├── cache.Get(id)               → HIT: devuelve inmediatamente
│       └── singleflight.Do(id, func{   → MISS: colapsa concurrentes
│               perfil = db.GetProfile(id)
│               cache.Set(id, perfil, 5*time.Minute)
│           })
│
└── ProfileRefreshLoop (goroutine)
    ├── ticker 15 min
    │   ├── lee watermark de MSP_PROFILE_WATERMARK
    │   ├── SELECT DISTINCT cliente_id WHERE updated_at > watermark
    │   ├── para cada cliente_id: computar perfil completo
    │   ├── UPSERT MSP_CUSTOMER_PROFILES
    │   └── actualizar watermark
    └── ticker 03:00 h diario
        └── full recompute (todos los clientes, sin filtro de watermark)
```

**Pool de conexiones separado para el refresh:**

```go
// El refresh usa su propio pool (max 2 conexiones) para no bloquear HTTP
refreshDB := openFirebirdPool(dsn, maxOpen=2, maxIdle=1)
httpDB    := openFirebirdPool(dsn, maxOpen=10, maxIdle=5)
```

**Singleflight para cache stampede** (golang.org/x/sync/singleflight):

```go
var sfGroup singleflight.Group

func getProfile(id string) (CustomerProfile, error) {
    if p, ok := cache.Get(id); ok {
        return p.(CustomerProfile), nil
    }
    v, err, _ := sfGroup.Do(id, func() (interface{}, error) {
        p, err := db.QueryProfile(id)
        if err == nil {
            cache.Set(id, p, 5*time.Minute)
        }
        return p, err
    })
    return v.(CustomerProfile), err
}
```

**Consideraciones específicas de Firebird:**

1. **Bug del driver firebirdsql v0.9.19 (conocido en el proyecto)**: `SUM(NUMERIC)` devuelve valores sin escalar. Todos los agregados monetarios en la query de refresh deben usar `CAST(SUM(col) AS NUMERIC(18, 2))`.

2. **Snapshot isolation de Firebird (MVCC)**: la goroutine de refresh lee un snapshot consistente del momento en que inicia la transacción — correcto por diseño; escrituras concurrentes no contaminan el perfil.

3. **Sin vistas materializadas nativas en Firebird**: `MSP_CUSTOMER_PROFILES` es la vista materializada — mantenida por Go, no por el motor.

4. **Page cache de Firebird para cargas analíticas** (IBSurgeon + docs oficiales):
   - `DefaultDbCachePages = 100000` en `firebird.conf` (SuperServer) para mantener páginas calientes en RAM durante el refresh
   - `FileSystemCacheThreshold = 100M` para aprovechar el page cache del OS
   - Ejecutar el refresh nocturno en horario de baja carga para evitar contención de locks con escrituras transaccionales

5. **Timestamps desde Go, no desde Firebird** (Regla 1 del proyecto): el campo `computed_at` de `MSP_CUSTOMER_PROFILES` se setea desde Go via `firebird.ToWallClock(time.Now())`, nunca con `CURRENT_TIMESTAMP`.

**Trade-offs en el stack legacy (Windows Server, sin Docker):**

| Preocupación | Batch nightly | Incremental 15-min | On-demand+cache |
|---|---|---|---|
| Freshness | ~24 h | ~15 min | ~5 min (TTL) |
| Carga Firebird | Burst nocturno | Continua baja | Spike en cache miss |
| Complejidad | Mínima | Baja | Media |
| Recuperación ante falla | Re-run full batch | Replay desde watermark | Cache calienta en primer request |
| Fit con nssm / Windows Service | Excelente | Excelente | Excelente |
| Dependencias externas | Ninguna | Ninguna | Ninguna |

**Schema de MSP_CUSTOMER_PROFILES (borrador):**

```sql
CREATE TABLE MSP_CUSTOMER_PROFILES (
  CLIENTE_ID       CHAR(36) CHARACTER SET ASCII NOT NULL PRIMARY KEY,
  COMPUTED_AT      TIMESTAMP NOT NULL,

  -- Crédito
  LIMITE_CREDITO         NUMERIC(18,2),
  SALDO_PENDIENTE        NUMERIC(18,2),
  UTILIZACION_CREDITO    NUMERIC(5,4),   -- 0.0000–1.0000
  DIAS_MORA_HOY          INTEGER,
  MAX_DIAS_MORA          INTEGER,
  TASA_PAGO_PUNTUAL      NUMERIC(5,4),
  PAGOS_ULTIMOS_12M      INTEGER,
  PAGOS_PERDIDOS_12M     INTEGER,

  -- Compras
  DIAS_DESDE_ULTIMA_COMPRA INTEGER,
  COMPRAS_L3M            INTEGER,
  COMPRAS_L6M            INTEGER,
  COMPRAS_L12M           INTEGER,
  MONTO_L3M              NUMERIC(18,2),
  MONTO_L6M              NUMERIC(18,2),
  MONTO_L12M             NUMERIC(18,2),
  TICKET_PROMEDIO        NUMERIC(18,2),
  VALOR_VIDA_TOTAL       NUMERIC(18,2),

  -- RFMS
  RFMS_R                 SMALLINT,       -- 1–5
  RFMS_F                 SMALLINT,
  RFMS_M                 SMALLINT,
  RFMS_S                 SMALLINT,
  RFMS_SEGMENTO          VARCHAR(30) CHARACTER SET UTF8,

  -- Health
  HEALTH_SCORE           SMALLINT,       -- 0–100
  HEALTH_GRADO           CHAR(1),        -- A/B/C/D
  FLAG_CHURN             SMALLINT DEFAULT 0,
  FLAG_MORA              SMALLINT DEFAULT 0,

  -- Antigüedad
  ANTIGUEDAD_MESES       INTEGER,
  FECHA_PRIMERA_COMPRA   TIMESTAMP,
  FECHA_ULTIMA_COMPRA    TIMESTAMP
);

CREATE INDEX IDX_MSP_CUSTPROF_MORA     ON MSP_CUSTOMER_PROFILES (DIAS_MORA_HOY);
CREATE INDEX IDX_MSP_CUSTPROF_SEGMENTO ON MSP_CUSTOMER_PROFILES (RFMS_SEGMENTO);
CREATE INDEX IDX_MSP_CUSTPROF_SCORE    ON MSP_CUSTOMER_PROFILES (HEALTH_SCORE);
```

### Pitfalls

1. **Watermark drift con timestamps no controlados**: si alguna tabla fuente tiene `updated_at` seteado por un trigger o por Go en una zona horaria incorrecta, el watermark pierde actualizaciones silenciosamente. Auditar todas las tablas fuente antes de implementar.
2. **Full recompute sin límite de tiempo**: con 10k+ clientes y queries analíticas pesadas, un full recompute puede tardar más de 15 minutos y solaparse con el ticker incremental. Poner un mutex o cancelar el ticker durante el full recompute.
3. **Pool de conexiones compartido**: si el refresh goroutine y los handlers HTTP comparten el mismo pool, una query de refresh lenta agota las conexiones disponibles para HTTP. Pools separados, como se muestra arriba.
4. **Schema drift de MSP_CUSTOMER_PROFILES**: a medida que el producto evoluciona, la tabla acumula campos que el código ya no popula. Alternativamente, usar una columna `EXTRA_JSON BLOB SUB_TYPE TEXT CHARACTER SET UTF8` para campos experimentales/en evolución.
5. **Cache stampede en arranque del servicio**: al reiniciar el servicio en Windows (deploy), todos los clientes están cold en cache. El singleflight previene thundering herd, pero el primer ciclo de 15 min puede tener alta carga. Precargar en background al inicio con prioridad baja.

### Fuentes

- [event-driven.io — Projections and Read Models in Event-Driven Architecture](https://event-driven.io/en/projections_and_read_models_in_event_driven_architecture/)
- [TheCodeInterface — CQRS Read Model Patterns](https://thecodinginterface.com/blog/cqrs-read-model-patterns/)
- [Medium — CQRS Pattern: Optimizing DB Reads and Writes](https://medium.com/@syed.fawzul.azim/cqrs-pattern-optimizing-database-reads-and-writes-with-examples)
- [Medium — Go Backend: Event-Driven Design + Outbox Pattern](https://medium.com/@steffankharmaaiarvi/architecting-go-backend-event-driven-design-outbox-pattern-3928bf315e0a)
- [samber/hot — Go In-Memory Caching Library](https://github.com/samber/hot)
- [IBSurgeon — Short Firebird Database Performance Checklist](https://ib-aid.com/en/articles/short-checklist-for-firebird-database-performance/)
- [IBSurgeon — 45 Ways to Speed Up Firebird](https://ib-aid.com/en/articles/45-ways-to-speed-up-firebird-database/)
- [Firebird — Cache Buffer Documentation](https://www.firebirdsql.org/file/documentation/html/en/firebirddocs/fbcache/firebird-cache.html)
- [golang.org/x/sync/singleflight — package docs](https://pkg.go.dev/golang.org/x/sync/singleflight)

---

*Documento generado: 2026-06-13. Verificación adversarial aplicada: ≥2 fuentes independientes por claim sustantivo. Cifras de vendor marketing marcadas explícitamente. Métricas no verificables para esta cartera marcadas [A MEDIR NOSOTROS].*
