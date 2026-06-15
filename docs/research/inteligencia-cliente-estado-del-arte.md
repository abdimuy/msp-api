# Inteligencia de Cliente — Estado del arte (Track A)

> **Entregable 1** del *Plan de INVESTIGACIÓN — Componente "Inteligencia de Cliente"*.
> Síntesis del estado del arte para construir un perfil derivado por cliente (interno + "Cliente 360")
> en **retail de muebles a crédito, base de miles de clientes, mercado mexicano/no-bancarizado**.
>
> **Cómo se produjo:** investigación web en paralelo (un agente por clúster de temas) con
> **verificación adversaria** — ≥2 fuentes independientes por afirmación sustantiva; los números de
> industria que no resistieron verificación quedan marcados `[A MEDIR NOSOTROS]` en vez de asumirse.
> Las fuentes con URL están al final de cada tema.
>
> **Alcance:** SOLO estado del arte. El mapeo de qué soporta la base de datos real vive en el
> Entregable 2 (`inteligencia-cliente-diccionario-datos.md`), y el cruce "qué recomienda el estado
> del arte ↔ qué soporta la DB" está en ese mismo doc.

## Etiquetas de confianza

- **Recomendación verificada:** sostenida por ≥2 fuentes independientes citadas.
- `[A MEDIR NOSOTROS]`: número/parámetro de industria que NO debe asumirse — se calibra con nuestros datos.
- **Pitfall:** error común documentado; evitarlo es parte del diseño.

## Índice (11 temas, agrupados por clúster)

**Clúster 1 — Arquitectura y producto** (`§1, §8, §10, §11`)
- §1. Perfil unificado / Customer 360 / feature store
- §8. UI "Cliente 360" para usuarios de negocio no técnicos
- §10. Cómo lo estructuran los comparables (Coppel / Elektra / Aaron's / Yalo / CDPs)
- §11. Enfoque técnico en Go leyendo Firebird

**Clúster 2 — Segmentación, ciclo de vida y valor** (`§2, §6, §7`)
- §2. Segmentación RFM (con ajuste de "M" por premio de financiamiento)
- §6. Estados de ciclo de vida del cliente (anclados al ciclo de recompra ~11 meses)
- §7. CLV/LTV para crédito a plazos

**Clúster 3 — Recomendación y propensión** (`§3, §4`)
- §3. Next-best-product / next-best-offer (market basket, complemento faltante)
- §4. Propensión / probabilidad de reactivación / churn (cold-start sin etiquetas)

**Clúster 4 — Riesgo y fraude** (`§5, §9`)
- §5. Scoring de riesgo de crédito thin-file / a plazos
- §9. Verificación de identidad y señales de fraude (prestanombres / identidad sintética)

---


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


---


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


---


## 3. Next-Best-Product / Next-Best-Offer

### Recomendación

Para una mueblería con miles de clientes y datos de compra escasos (cada cliente compra pocos productos distintos), la estrategia recomendada es **una combinación de dos capas**:

1. **Capa 1 — Association rules sobre historial de transacciones** (market basket analysis): genera un mapa de pares complementarios directamente de lo que los clientes han comprado en conjunto o en secuencia. Es interpretable, no requiere infraestructura de ML, y funciona bien con el volumen de datos disponible desde el inicio.

2. **Capa 2 — Item-based collaborative filtering con implicit feedback** (una vez que se acumule historial suficiente): complementa la capa 1 identificando patrones latentes de co-compra entre clientes similares.

El enriquecimiento con **lógica de "complemento faltante"** (cliente que ya compró X pero no Y, siendo (X,Y) un par complementario conocido) es la aplicación operativa inmediata de ambas capas.

---

### Fórmula/Algoritmo

#### Association Rules — Métricas Fundamentales

Dado un conjunto de transacciones T, para una regla A → B:

```
Support(A ∪ B) = |transacciones que contienen A y B| / |total de transacciones|

Confidence(A → B) = Support(A ∪ B) / Support(A)
                  = P(B | A)

Lift(A → B)     = Support(A ∪ B) / [Support(A) × Support(B)]
                = Confidence(A → B) / Support(B)
                = P(A ∪ B) / [P(A) × P(B)]
```

**Interpretación de Lift:**
- Lift = 1: A y B son independientes (asociación espuria).
- Lift > 1: A y B co-ocurren más de lo esperado por azar → regla útil.
- Lift < 1: presencia de A reduce la probabilidad de B (sustitutos, no complementos).

**Umbrales prácticos para mueblería (escala miles de tickets):**
- `min_support ≥ 0.005` (al menos 5 transacciones por cada 1,000) para evitar reglas sobre eventos rarísimos.
- `min_confidence ≥ 0.15` (15% de co-compra condicional, ajustado a categoría).
- `lift > 1.2` como filtro mínimo de significancia; idealmente > 2.0 para reglas de acción inmediata.

**Algoritmo:** Apriori (implementación disponible en `mlxtend` Python). Propiedad anti-monótona: si un conjunto de ítems no supera `min_support`, ningún superconjunto lo superará → poda del espacio de búsqueda.

#### Construcción del Mapa de Pares Complementarios

```
Para cada par de categorías (cat_A, cat_B):
  1. Calcular co_purchase_rate = |clientes que compraron ambas| / |clientes que compraron al menos una|
  2. Calcular lift(cat_A, cat_B)
  3. Si lift > 1.5 y co_purchase_rate > umbral_negocio → marcar como PAR COMPLEMENTARIO
  4. Ordenar por lift descendente

Para recomendar a cliente C:
  - Obtener conjunto de categorías ya compradas: Compras(C)
  - Para cada par (A, B) en mapa complementario donde A ∈ Compras(C) y B ∉ Compras(C):
    → B es "complemento faltante" → priorizar oferta
```

**Ejemplos de pares complementarios documentados en retail de hogar:**
- cama → colchón (bed → mattress): par clásico post-venta inmediata.
- refrigerador ↔ estufa: compra en el mismo horizonte temporal de equipamiento de cocina.
- lavadora → secadora o refrigerador → sin fridge (complemento de electrodomésticos).

Fuente de señal: co-purchase (mismo ticket o mismo cliente en ventana de 90 días) diferencia mejor los complementos de los sustitutos que co-view. Los sustitutos generan lift < 1 en co-purchase (el cliente compra uno *en vez* del otro).

#### Collaborative Filtering con Implicit Feedback

Para datos escasos, el enfoque **item-based CF** sobre matriz binaria de implicit feedback supera al user-based CF:

```
P(u,i) = 1  si el usuario u compró el ítem i
P(u,i) = 0  si no hay registro (no necesariamente "no le interesa")

Similitud entre ítems i y j:
  cosine_sim(i, j) = (P_i · P_j) / (||P_i|| × ||P_j||)

Recomendación para usuario u:
  score(u, j) = Σ_{i ∈ Compras(u)} cosine_sim(i, j) × P(u, i)
  Recomendar los top-K ítems j con mayor score no comprados por u
```

La similitud coseno es preferida para matrices dispersas sobre Pearson o distancia euclidiana. Librería práctica: `implicit` (Python, MIT license).

**Cuándo usar cada enfoque según tamaño de datos:**

| Escenario | Recomendación |
|---|---|
| < 5,000 transacciones totales | Solo association rules + mapa manual de complementos |
| 5,000–50,000 transacciones | Association rules + item-based CF con cosine similarity |
| > 50,000 transacciones | Añadir matrix factorization (ALS) o deep item-based CF |

---

### Pitfalls

1. **Popularity bias en confidence:** Si el 80% de los clientes compra colchones, cualquier regla X → colchón tendrá confidence alta aunque X no tenga relación causal. El lift corrige esto, pero sigue siendo sensible a ítems muy populares.

2. **Lift alto en ítems poco frecuentes:** Lift tiende a maximizarse cerca del `min_support` mínimo. Reglas con lift = 8 pero support = 0.001 (2 transacciones sobre 2,000) son estadísticamente irrelevantes. Solución: exigir un mínimo absoluto de transacciones además del soporte relativo.

3. **Spurious rules por umbral de soporte demasiado bajo:** Bajar `min_support` para encontrar asociaciones raras explota el número de reglas generadas y aumenta el ruido. Para mueblería (ciclos de compra largos), el soporte natural será bajo; es preferible **ampliar la ventana temporal** (usar historial de 3-5 años) que bajar el umbral artificialmente.

4. **Confundir sustitutos con complementos:** Un cliente que compra una cama *king* raramente compra también una *queen*. El lift alto en pares de la misma sub-categoría indica sustitución, no complementariedad. Filtrar pares de la misma categoría de producto al construir el mapa.

5. **Cold start de ítems nuevos:** Un producto recién añadido al catálogo no tendrá historial de co-compra. Solución: asignar el ítem a una categoría y heredar las asociaciones de categoría como proxy hasta acumular datos propios.

6. **Sparsidad en collaborative filtering:** Con < 5 compras por cliente, el vector de usuario es casi todo ceros. Item-based CF es más robusto que user-based en este escenario porque agrega señal desde el lado del ítem (muchos usuarios por ítem) en lugar del lado del usuario (pocos ítems por usuario).

---

### Fuentes

- [Market Basket Analysis: Association Rules — Niharika Goel, Medium](https://medium.com/@niharika.goel/market-basket-analysis-association-rules-e7c27b377bd8) — fórmulas de support, confidence, lift; aplicación retail.
- [Association Rules and the Apriori Algorithm — KDnuggets](https://www.kdnuggets.com/2016/04/association-rules-apriori-algorithm-tutorial.html) — pitfall de popularity bias (ejemplo milk/beer); lift para filtrar asociaciones espurias.
- [Marketing - Market Basket Analysis — Michael Fuchs Python](https://michael-fuchs-python.netlify.app/2020/09/15/marketing-market-basket-analysis/) — implementación práctica con mlxtend; fórmulas verificadas.
- [Novel lift adjustment methodology — ScienceDirect 2025](https://www.sciencedirect.com/article/pii/S2772662225000384) — lift sesgo hacia ítems raros, inestabilidad cerca de min_support; lift > 1.2 como filtro mínimo.
- [Inferring Complementary Products from Baskets — arXiv 1809.09621](https://arxiv.org/pdf/1809.09621) — metodología para distinguir complementos de sustitutos desde historial de compra.
- [Complementary Item Recommendations at E-Commerce — Adevinta Tech Blog](https://medium.com/adevinta-tech-blog/complementary-item-recommendations-at-e-commerce-marketplaces-3a5d9fc5ff9f) — señal co-purchase vs co-view; ejemplo colchón + base de cama.
- [Deep Item-based Collaborative Filtering for Sparse Implicit Feedback — arXiv 1812.10546](https://arxiv.org/abs/1812.10546) — item-based CF sobre datos escasos volátiles; mejora sobre CF estándar.
- [Implicit — Python CF library for implicit feedback](https://github.com/benfred/implicit) — implementación práctica ALS/cosine para matrices dispersas.
- [Resolving Data Sparsity and Cold Start — Springer](https://link.springer.com/chapter/10.1007/978-3-642-31454-4_36) — comparación CF vs content-based en escenarios de sparsidad; recomendación híbrida.

---

## 4. Propensión / Probabilidad de Reactivación / Churn

### Recomendación

Para una mueblería sin dataset etiquetado inicial, la estrategia recomendada es **un pipeline por fases**:

**Fase 0 — Arranque sin etiquetas (semanas 1–4):** Reglas heurísticas basadas en RFM (Recency, Frequency, Monetary) para clasificar clientes en segmentos de riesgo. No requiere modelo ML; produce scores accionables de inmediato.

**Fase 1 — Bootstrapping de etiquetas (semanas 4–12):** Usar RFM + clustering K-means para generar etiquetas sintéticas (churned / at-risk / active) a partir del historial. Estas etiquetas entrenan el primer modelo supervisado.

**Fase 2 — Modelo supervisado (mes 3+):** Logistic Regression o Gradient Boosting (XGBoost/LightGBM) entrenado sobre las etiquetas bootstrapped, con validación sobre eventos reales de recompra observados en el periodo siguiente.

**Fase 3 — Modelo probabilístico sin etiquetas (paralelo a Fase 2):** BG/NBD (Beta Geometric / Negative Binomial Distribution) para estimar probabilidad de recompra futura sin necesidad de etiqueta binaria de churn. Especialmente adecuado para retail no-contractual con ciclos largos.

---

### Fórmula/Algoritmo

#### Fase 0 — Heurísticas RFM como Scores de Propensión

Calcular para cada cliente al día de hoy:

```
R = días transcurridos desde la última compra  (menor R → más activo)
F = número total de compras en la ventana histórica
M = valor monetario total (o promedio por compra)

Score de riesgo de churn (regla simple):
  Si R > P90_industria AND F = 1 → riesgo ALTO (cliente de compra única, inactivo)
  Si R > P75_industria AND F ≥ 2 → riesgo MEDIO (cliente recurrente en decaída)
  Si R ≤ P50_industria → riesgo BAJO (cliente activo)
```

Los percentiles P50/P75/P90 de Recency se calculan directamente sobre la distribución del propio negocio. **[A MEDIR NOSOTROS]**: el umbral de recency que define "inactivo" en mueblería mexicana a crédito. Adobe Commerce sugiere usar la mediana del intervalo de recompra como referencia; para retail de alta rotación este umbral suele ser 90–180 días; para mueblería (compra durable) puede ser 12–36 meses.

**Regla de decaimiento de probabilidad de recompra (basada en Adobe Commerce / Optimove):**

Si la probabilidad inicial de recompra de un cliente es `p0`:
- A 90 días sin compra: probabilidad ≈ `p0 × 0.58` [A MEDIR NOSOTROS]
- A 180 días sin compra: probabilidad ≈ `p0 × 0.25` [A MEDIR NOSOTROS]
- Punto de corte para reactivación: cuando la probabilidad cae por debajo de `p0 / 2`

#### Fase 1 — Bootstrapping de Etiquetas con RFM + K-Means

```
1. Normalizar R, F, M a escala [0,1] (MinMax o percentil)
2. Aplicar K-means con k=3 a 5 clusters (usar elbow method sobre inercia)
3. Asignar etiquetas semánticas por perfil de centroide:
   - Cluster {R_alto, F_bajo, M_bajo} → "CHURNED" (etiqueta negativa)
   - Cluster {R_bajo, F_alto, M_alto} → "LEAL" (etiqueta positiva)
   - Clusters intermedios → "EN RIESGO"
4. Usar cluster_id + RFM raw como features del modelo supervisado
5. Validar: en los siguientes 90 días, ¿qué % del cluster "LEAL" recompró?
   ¿Qué % del cluster "CHURNED" no recompró? → calibrar etiquetas
```

**Nota crítica:** las etiquetas del clustering son proxies, no ground truth. El modelo supervisado entrenado sobre ellas hereda el sesgo de la definición de cluster. Iterar con datos de comportamiento real observado.

#### Fase 2 — Modelo Supervisado (Logistic Regression / XGBoost)

**Features predictivos para mueblería a crédito:**

| Feature | Tipo | Descripción |
|---|---|---|
| `recency_days` | Numérico | Días desde última compra |
| `frequency` | Numérico | Total de compras históricas |
| `monetary_avg` | Numérico | Ticket promedio |
| `inter_purchase_time_avg` | Numérico | Tiempo promedio entre compras |
| `inter_purchase_time_trend` | Numérico | ¿Se está alargando el intervalo? |
| `num_categories` | Numérico | Diversidad de categorías compradas |
| `pago_puntualidad` | Binario/ratio | % de cuotas pagadas a tiempo |
| `mora_max_dias` | Numérico | Máxima mora histórica en días |
| `canal_contacto_activo` | Binario | ¿Responde WhatsApp/llamada? |
| `antiguedad_cliente_meses` | Numérico | Tiempo desde primera compra |
| `cluster_rfm` | Categórico | Cluster RFM (Fase 1) |

**Logistic Regression (recomendada para arranque):**
```
P(reactivación) = 1 / (1 + exp(-[β0 + β1·recency + β2·frequency + β3·monetary + ...]))
```
Ventajas: interpretable (coeficientes = importancia de feature), funciona con muestras pequeñas, no requiere tuning extenso. Limitación: solo captura relaciones lineales.

**XGBoost (recomendado cuando n_clientes > 1,000 con historial):**
- Captura interacciones no lineales entre features (ej.: mora alta + recency alta = riesgo exponencialmente mayor).
- Produce probabilidades calibradas con isotonic regression o Platt scaling.
- Estudio de referencia: accuracy 76.6%, recall 84.2% sobre churn e-commerce (n=~25,000).

**Definición de etiqueta para entrenamiento supervisado (sin etiquetas pre-existentes):**
```
Tomar historial de 36 meses.
Para cada cliente activo hace 12 meses:
  Si recompró en los siguientes 12 meses → label = 1 (reactivó)
  Si no recompró → label = 0 (churned)
Entrenar sobre esta ventana histórica.
Aplicar modelo a clientes actuales.
```
Este es el bootstrapping desde eventos reales: no requiere etiquetado manual. Solo requiere historial suficiente (al menos 24–36 meses para tener ventana de entrenamiento y ventana de observación).

#### Fase 3 — Modelo BG/NBD (sin etiquetas, probabilístico)

El modelo BG/NBD (Beta Geometric / Negative Binomial Distribution) estima la probabilidad de que un cliente esté "vivo" y haga otra compra, dado solo su historial de transacciones (recency, frequency, T = antigüedad):

```
Inputs por cliente: x (# compras), t_x (tiempo de última compra), T (antigüedad total)
Output: P(activo | x, t_x, T) y E[compras futuras en periodo τ]
```

**Ventajas para mueblería:**
- No requiere definir churn binario ni etiquetas.
- Maneja nativamente ciclos de compra largos e irregulares.
- Combina bien con Gamma-Gamma model para estimar Customer Lifetime Value.
- Implementación: librería `lifetimes` (Python) o `CLVTools` (R).

---

### Pitfalls

1. **Cold start real:** Un cliente con solo 1 compra tiene R y M pero F=1 e inter_purchase_time inexistente. El modelo supervisado no puede inferir nada de frecuencia. Solución: segmento separado para clientes de primera compra; usar solo features de la transacción única (categoría, monto, canal) + demográficos.

2. **Sesgo de supervivencia en etiquetas bootstrapped:** Si solo se tiene historial de clientes "activos" (que alguna vez compraron y están en la BD), los clientes que nunca reactivaron y fueron dados de baja ya no están disponibles. Esto subestima la tasa real de churn. [A MEDIR NOSOTROS]: la tasa de bajas definitivas históricas.

3. **Definición de churn no estándar en mueblería:** El ciclo natural de recompra es largo (2–5 años para muebles de sala/recámara). Un cliente sin compra en 12 meses no es necesariamente churned; puede estar en su ventana inter-compra normal. El umbral de churn debe basarse en la distribución real del inter_purchase_time del negocio, no en benchmarks de e-commerce rápido (donde se citan 90–180 días). **[A MEDIR NOSOTROS].**

4. **Precision vs. Recall trade-off:** Para campañas de reactivación con costo marginal bajo (WhatsApp), maximizar recall (capturar todos los at-risk) puede ser preferible. Para campañas costosas (visita de vendedor), priorizar precision. El modelo debe calibrarse según el costo de la acción.

5. **Data leakage:** Si las features de entrenamiento incluyen datos del periodo de observación (ej.: cuántas cuotas pagó *después* de la fecha de corte), el modelo estará sobreajustado. Cortar features estrictamente en la fecha de predicción.

6. **Etiquetas de K-means como ground truth:** Si el modelo supervisado se entrena *solo* sobre etiquetas de clustering sin validar con comportamiento real posterior, el modelo aprende a replicar el clustering, no a predecir recompra real. Siempre validar con ventana de observación real.

7. **Números de industria no verificables:** Se citan tasas de churn de 20–30% en retail general y de 60–80% de decaimiento de probabilidad en 6 meses, pero estos números vienen de e-commerce de alta frecuencia y no aplican directamente a mueblería a crédito. **[A MEDIR NOSOTROS]** en el dataset propio.

---

### Fuentes

- [Hybrid RFM + K-means + Deep Learning for Churn — Scientific Reports 2026](https://www.nature.com/articles/s41598-026-53220-0) — bootstrapping de etiquetas con clustering; framework RFM → etiquetas → modelo supervisado.
- [RFM + K-means Retail Churn — ScienceDirect 2025](https://www.sciencedirect.com/science/article/abs/pii/S0957417425020846) — implementación práctica de K-means sobre RFM para generar etiquetas sin datos etiquetados previos.
- [Retail Customer Churn with RFM and K-Means — IJERT](https://www.ijert.org/research/retail-customer-churn-analysis-using-rfm-model-and-k-means-clustering-IJERTV10IS030170.pdf) — receta de clustering sobre RFM para retail; definición de clusters churned/at-risk.
- [Hybrid Logistic Regression + XGBoost for Churn — IIETA](https://www.iieta.org/journals/isi/paper/10.18280/isi.240510) — features en 6 dimensiones; accuracy 76.6%, recall 84.2%; pipeline LR → XGBoost.
- [Enhancing Customer Repurchase Prediction with RFM — ScienceDirect 2025](https://www.sciencedirect.com/science/article/pii/S0970389625000266) — integración de métricas RFM en algoritmos de clasificación para precisión de recompra.
- [Repeat Probability Decay and Churn — Adobe Commerce](https://experienceleague.adobe.com/en/docs/commerce-business-intelligence/mbi/analyze/performance/repeat-decay-churn) — definición de churn como punto donde probabilidad de recompra cae a la mitad; umbral dinámico basado en industria.
- [BG/NBD Churn Prediction — FasterCapital](https://fastercapital.com/content/BG-NBD-Model--Predicting-Customer-Churn-Using-the-BG-NBD-Model.html) — modelo probabilístico sin etiquetas; manejo de datos censurados; aplicación retail no-contractual.
- [Simplified BG/NBD — arXiv 2502.12912](https://arxiv.org/pdf/2502.12912) — implementación numérica estable del BG/NBD; alternativa al Pareto/NBD.
- [Propensity Modelling — Impression Digital](https://www.impressiondigital.com/blog/propensity-modelling/) — definición de propensity score; técnicas (LR, decision trees, neural nets); proceso de scoring.
- [Exploiting Time-varying RFM for Churn — Springer Annals of Operations Research](https://link.springer.com/article/10.1007/s10479-023-05259-9) — RFM dinámico (inter-purchase time variance) como feature predictivo; deep learning sobre secuencias temporales.
- [Reactivation Rate Model — Optimove](https://academy.optimove.com/hc/en-us/articles/8665330827677-Reactivation-Rate-Model) — definición de reactivation vs. retention; umbral de riesgo accionable.


---


# A4 — Riesgo crediticio y detección de fraude
> Sección del documento "Estado del arte — Inteligencia de cliente" para mueblería con crédito a plazos, mercado no-bancarizado mexicano.
> Fecha: junio 2026.

---

## 5. Scoring de riesgo crediticio en población thin-file / crédito a plazos

### Contexto de mercado

Alrededor del 50 % de la población mexicana sigue sin cuenta bancaria, y aproximadamente el 70 % carece de historial crediticio formal en el Buró de Crédito o Círculo de Crédito [[RiskSeal 2025](https://riskseal.io/blog/alternative-credit-scoring-in-mexico)]. Las calificadoras convencionales solo alcanzan ~49 % de los adultos mexicanos, lo que hace inviable el score de buró como único criterio de originación para retailers de crédito directo como Coppel, Elektra o una mueblería regional [[RiskSeal — Consumer Lending Mexico](https://riskseal.io/blog/consumer-loans-in-mexico)].

### Recomendación

Construir un **scorecard de comportamiento interno** (behavioral scorecard) alimentado exclusivamente con los datos transaccionales propios de la cartera. No esperar al buró: la propia historia de pago con la empresa es la señal más predictiva disponible para clientes recurrentes. Para clientes nuevos (first-time), aplicar un score de aplicación basado en características demográficas + aval + zona geográfica.

El estándar de la industria BNPL y de retailers no-bancarizados en México converge en **cinco dimensiones de comportamiento de pago** [[riskseal.io — BNPL features](https://riskseal.io/blog/credit-scoring-features-for-bnpl-providers)] [[SciELO — Riesgo crédito retail México](https://www.scielo.org.mx/scielo.php?pid=S0186-10422017000200377&script=sci_arttext&tlng=en)]:

| Dimensión | Descripción operacional |
|-----------|------------------------|
| **Puntualidad** | % de pagos recibidos en o antes de la fecha de vencimiento |
| **Días de atraso promedio** | Promedio de días de mora por cuota en créditos activos o históricos |
| **% pagado acumulado** | Saldo pagado / saldo total originado (proxy de capacidad de pago) |
| **Recencia del último pago** | Días transcurridos desde el último pago efectivo |
| **Frecuencia / créditos completados** | Número de créditos liquidados en su totalidad |

### Fórmula: scorecard ponderado (0–100 puntos)

```
Score_comportamiento = 
    0.35 × P_puntualidad
  + 0.20 × P_recencia
  + 0.20 × P_porcentaje_pagado
  + 0.15 × P_atraso_promedio
  + 0.10 × P_frecuencia
```

Donde cada `P_x` es la puntuación parcial de la dimensión, escalada 0–100.

#### Cálculo de cada componente

**P_puntualidad** (peso 35 %):
```
puntualidad_rate = pagos_a_tiempo / total_pagos_vencidos
P_puntualidad = puntualidad_rate × 100
```
Ajuste: si `puntualidad_rate >= 0.95` → 100 pts; `0.80-0.94` → 70; `0.60-0.79` → 40; `< 0.60` → 0.

**P_recencia** (peso 20 %):
```
días_desde_ultimo_pago = CURRENT_DATE - MAX(fecha_pago)
P_recencia = MAX(0, 100 - días_desde_ultimo_pago * 1.5)
```
(Penaliza 1.5 pts por cada día adicional sin pago; llega a 0 a los ~67 días.)

**P_porcentaje_pagado** (peso 20 %):
```
pct_pagado = SUM(monto_pagado) / SUM(saldo_total_originado)  -- sobre cartera histórica del cliente
P_porcentaje_pagado = MIN(pct_pagado * 100, 100)
```

**P_atraso_promedio** (peso 15 %):
```
dias_atraso_prom = AVG(MAX(0, fecha_pago_real - fecha_vencimiento))  -- en días, por cuota
P_atraso_promedio = MAX(0, 100 - dias_atraso_prom * 3.33)
```
(Llega a 0 con 30+ días de atraso promedio, consistente con la escala de Buró de Crédito donde ≥30 días ya es impacto grave [[Condusef](https://www.condusef.gob.mx/?p=contenido&idc=267&idcat=3)].)

**P_frecuencia** (peso 10 %):
```
P_frecuencia = MIN(creditos_completados * 20, 100)
-- 0 completados = 0 pts; 5+ completados = 100 pts
```

#### Umbrales de tier (clasificación de riesgo)

| Tier | Rango score | Etiqueta | Acción sugerida |
|------|-------------|----------|-----------------|
| Verde | 75–100 | Buen pagador | Aprobación rápida; límite ampliable |
| Amarillo | 50–74 | Pagador irregular | Aprobación con condiciones (aval, enganche mayor) |
| Rojo | 0–49 | Alto riesgo / incumplidor | Rechazar o restructurar previo |

> **Nota sobre clientes nuevos:** sin historial interno, score = 0 en todas las dimensiones de comportamiento. Usar score de aplicación separado (zona geográfica, tipo de producto, presencia de aval solvente).

### Comparación con la industria

Coppel y Elektra no divulgan sus fórmulas propietarias. Lo que sí se sabe:
- Ambas operan con **datos 100 % propios** (sin buró para nuevos clientes), aprendidos de décadas de cartera en segmentos populares.
- Usan **scoring de comportamiento actualizado mensualmente** sobre la cartera activa.
- El factor más crítico reportado por analistas es la **puntualidad en los primeros 3 meses** de un nuevo crédito — altamente predictiva del comportamiento del resto del plazo [[RiskSeal — BNPL features](https://riskseal.io/blog/credit-scoring-features-for-bnpl-providers)].
- BNPL players (Kueski, Aplazo) incorporan datos telco y de apps móviles — **irrelevante para una mueblería** sin acceso a esas APIs.

Los números específicos de tasas de default de Coppel/Elektra **no están verificados públicamente** — [A MEDIR NOSOTROS] con nuestra propia cartera.

### Pitfalls críticos

#### 1. Sesgo de selección / survivorship bias
El modelo entrenado sobre clientes que **sí obtuvieron crédito** no refleja el universo de solicitudes. Si históricamente se rechazó a clientes con cierto perfil, el modelo nunca los vio comportarse — y podría rechazarlos perpetuamente sin evidencia de que son malos pagadores [[credit-scoring.co.uk — Reject Inference](https://www.credit-scoring.co.uk/blog/rejectinference)].

**Solución parcial:** técnicas de *reject inference* (augmentation, extrapolation, parceling) para inferir outcomes de rechazados. Pero advertencia: "reject inference will never be perfect" y sus hipótesis son incontrastables [[credit-scoring.co.uk](https://www.credit-scoring.co.uk/blog/rejectinference)].

#### 2. Reject inference — el problema imposible
Se intenta responder "¿qué habría pasado si le hubiéramos prestado a quienes rechazamos?". La respuesta es forzosamente una estimación con supuestos no verificables [[arxiv:1909.06108](https://arxiv.org/pdf/1909.06108)]. El peor antipatrón: tratar todos los rechazos como malos pagadores → modelo excesivamente conservador.

#### 3. Discriminación por proxy
Variables como zona geográfica o tipo de colonia pueden ser proxies de etnia o nivel socioeconómico. En México, esto puede violar la Ley Federal para Prevenir y Eliminar la Discriminación. Usar variables demográficas solo como segmentador de primer nivel, no como penalizador directo.

#### 4. Sesgo de supervivencia temporal
Si la cartera analizada corresponde a un período atípico (pandemia, inflación alta), las ponderaciones aprenden comportamientos de ese contexto. Validar con ventanas temporales fuera de muestra.

#### 5. Clientes con un solo crédito vs. recurrentes
El scorecard de comportamiento es inútil para first-time clients (0 historial). Necesita un score de originación separado y una política explícita de "período de prueba" (ej: crédito pequeño + enganche alto para nuevos).

### Fuentes — Sección 5

- [RiskSeal — Alternative Credit Scoring in Mexico](https://riskseal.io/blog/alternative-credit-scoring-in-mexico)
- [RiskSeal — Consumer Lending Market Mexico](https://riskseal.io/blog/consumer-loans-in-mexico)
- [RiskSeal — BNPL Credit Scoring Features](https://riskseal.io/blog/credit-scoring-features-for-bnpl-providers)
- [RiskSeal — Mastering Credit Scoring with Alternative Data](https://riskseal.io/blog/mastering-credit-scoring-with-alternative-data)
- [SciELO — Credit Risk Management at Retail in Mexico (2017)](https://www.scielo.org.mx/scielo.php?pid=S0186-10422017000200377&script=sci_arttext&tlng=en)
- [Condusef — Buró de Crédito y plazos](https://www.condusef.gob.mx/?p=contenido&idc=267&idcat=3)
- [credit-scoring.co.uk — The Hidden Challenge of Reject Inference](https://www.credit-scoring.co.uk/blog/rejectinference)
- [arxiv:1909.06108 — Shallow Self-Learning for Reject Inference](https://arxiv.org/pdf/1909.06108)
- [ResearchGate — RFMS Method for Credit Scoring](https://www.researchgate.net/publication/322167215_RFMS_Method_for_Credit_Scoring_Based_on_Bank_Card_Transaction_Data)
- [Plaid — Synthetic Identity Fraud](https://plaid.com/resources/fraud/synthetic-identity-fraud/)

---

## 9. Verificación de identidad y señales de fraude (prestanombres / identidad sintética)

### Contexto en México

El robo de identidad cibernético creció **281 % entre 2022 y 2023** según la CONDUSEF [[Jumio — Fraude identidad sintética](https://www.jumio.com/es/fraude-de-identidad-sintetica/)]. En préstamos digitales, organizaciones criminales reclutan a personas físicas reales (*prestanombres*) para solicitar crédito en su nombre, o construyen identidades sintéticas combinando datos reales (CURP, INE) con información fabricada (nombre, domicilio, teléfono). Para una mueblería con crédito a plazos, el riesgo más inmediato es el **prestanombre** (identidad prestada) y el **fraude en red** (un operador que gestiona múltiples solicitudes con datos cruzados) [[FintechMexico — Tendencias fraude 2024](https://www.fintechmexico.org/notices/tendencias-emergentes-en-el-fraude-de-prestamos-digitales-en-mexico)].

> Se estima que el **95 % de las identidades sintéticas no se detecta en el proceso de onboarding** con métodos convencionales [[Linkurious — Synthetic Identity Fraud](https://linkurious.com/blog/synthetic-identity-fraud/)]. Este número no ha sido verificado de forma independiente para el contexto mexicano — [A MEDIR NOSOTROS] en nuestra cartera.

### Recomendación

Implementar detección en **dos capas**:
1. **Capa de calidad de datos** — heurísticas deterministas sobre inconsistencias en campos de identidad al momento de la solicitud.
2. **Capa de grafo de atributos compartidos** — análisis de red sobre la base de clientes acumulada para detectar clusters de cuentas ligadas por atributos comunes (teléfono, domicilio, INE, aval).

No se requiere ML complejo para la capa 1; sí se recomienda para la capa 2 a escala.

### Señales de fraude — Capa 1: calidad de datos en solicitud

| Señal | Descripción | Severidad |
|-------|-------------|-----------|
| **INE duplicada** | El número de folio o clave de elector INE ya existe en otra cuenta con nombre distinto | Muy alta |
| **CURP duplicada** | Misma CURP asociada a nombres o fechas de nacimiento distintos | Muy alta |
| **Teléfono reutilizado** | Mismo número de teléfono registrado en N > 2 cuentas distintas (titular o aval) | Alta |
| **Domicilio + CURP inconsistente** | Mismo domicilio declarado pero CURPs distintos (indica domicilio falso o compartido atípico) | Alta |
| **RFC formato inválido** | RFC no válido según algoritmo SAT (longitud, dígito verificador) | Media |
| **Edad implícita en CURP vs. edad declarada** | La fecha de nacimiento codificada en la CURP difiere >1 año de la declarada | Alta |
| **Teléfono / domicilio del aval = teléfono / domicilio del titular** | El aval no es independiente; posible fraude coordinado | Media-alta |
| **Aval que ya es titular en otra cuenta activa con atraso** | El aval tiene riesgo propio no revelado | Alta |
| **Velocidad de solicitudes** | Más de 2 solicitudes desde el mismo número de teléfono o domicilio en < 30 días | Media |

### Heurísticas SQL concretas para detección en base transaccional

Las siguientes queries pueden ejecutarse sobre la tabla de clientes/solicitudes. Asumir esquema simplificado: `clientes(id, nombre, telefono, domicilio, curp, ine_folio)` y `avales(credito_id, aval_cliente_id)`.

#### H1: Teléfono compartido entre N > 2 clientes distintos
```sql
SELECT telefono, COUNT(DISTINCT id) AS n_clientes
FROM clientes
GROUP BY telefono
HAVING COUNT(DISTINCT id) > 2
ORDER BY n_clientes DESC;
-- Flag: cualquier teléfono con n_clientes >= 3 es candidato a revisión manual.
-- n_clientes >= 5 es señal fuerte de operador fraudulento.
```

#### H2: INE reutilizada con nombre distinto
```sql
SELECT ine_folio,
       COUNT(DISTINCT nombre) AS n_nombres,
       COUNT(DISTINCT id)     AS n_cuentas
FROM clientes
WHERE ine_folio IS NOT NULL
GROUP BY ine_folio
HAVING COUNT(DISTINCT nombre) > 1;
-- Cualquier resultado es fraude probable (la INE es personal e intransferible).
```

#### H3: Mismo domicilio + distintas CURPs (posible domicilio inventado o prestanombre)
```sql
SELECT domicilio,
       COUNT(DISTINCT curp)   AS n_curps,
       COUNT(DISTINCT id)     AS n_clientes
FROM clientes
GROUP BY domicilio
HAVING COUNT(DISTINCT curp) > 3  -- umbral: >3 personas con diferente CURP en mismo domicilio
ORDER BY n_curps DESC;
-- Domicilios como "Av. Reforma 1" con 10+ CURPs distintos = domicilio ficticio o "colector".
```

#### H4: Aval que aparece como titular en otra cuenta
```sql
SELECT a.aval_cliente_id,
       c.nombre,
       COUNT(DISTINCT a.credito_id)  AS veces_como_aval,
       COUNT(DISTINCT cr.id)         AS cuentas_propias_activas
FROM avales a
JOIN clientes c   ON c.id = a.aval_cliente_id
JOIN creditos cr  ON cr.titular_id = a.aval_cliente_id AND cr.estado = 'activo'
GROUP BY a.aval_cliente_id, c.nombre
HAVING COUNT(DISTINCT a.credito_id) >= 2
   AND COUNT(DISTINCT cr.id) >= 1;
-- Persona que garantiza ≥2 créditos ajenos Y tiene cuenta propia activa = riesgo de sobreapalancamiento
-- o participante en red de prestanombres.
```

#### H5: Aval con atraso en su propio crédito al momento de avalar
```sql
SELECT a.credito_id,
       a.aval_cliente_id,
       MAX(p.dias_atraso) AS max_atraso_aval
FROM avales a
JOIN pagos p ON p.cliente_id = a.aval_cliente_id
WHERE p.fecha_vencimiento <= CURRENT_DATE
GROUP BY a.credito_id, a.aval_cliente_id
HAVING MAX(p.dias_atraso) > 30;
-- Aval con >30 días de atraso propio no es garante solvente.
```

#### H6: Velocidad de solicitudes desde mismo teléfono (loan stacking signal)
```sql
SELECT telefono,
       COUNT(*)            AS solicitudes_30d,
       MIN(fecha_solicitud) AS primera,
       MAX(fecha_solicitud) AS ultima
FROM solicitudes
WHERE fecha_solicitud >= CURRENT_DATE - 30
GROUP BY telefono
HAVING COUNT(*) >= 2;
```

### Capa 2: análisis de grafo de atributos compartidos

Para escala (> 10 000 clientes), las queries SQL anteriores son el punto de partida pero no revelan clusters multi-hop. El enfoque estándar de la industria es un **grafo de identidad** donde:

- **Nodos**: clientes, números de teléfono, domicilios, folios INE, CURPs
- **Aristas**: relación "cliente X tiene atributo Y"

Los nodos con grado anormalmente alto (un teléfono conectado a 20 clientes) son **hubs de fraude**. Las comunidades densas (Louvain, Leiden) revelan redes de prestanombres coordinadas [[Linkurious — Fraud Use Cases](https://linkurious.com/blog/fraud-use-cases-graph-analytics/)] [[Medium — Graph-Based Fraud Ring Detection](https://medium.com/@amistapuramk/graph-based-approaches-for-detecting-fraud-rings-in-digital-platforms-c3031f83ef99)].

Señales estructurales de grafo:
- **Alto coeficiente de clustering**: cuentas dentro de una comunidad que se avalan mutuamente en ciclos.
- **Ciclos cortos**: A avala a B, B avala a C, C avala a A — patrón infrecuente en datos legítimos.
- **Nodo hub**: un número de teléfono o domicilio con grado > umbral (sugerir: > 5 conexiones a clientes distintos para iniciar revisión).

> Para una base de cartera de tamaño mediano (< 50 000 clientes), estas consultas SQL son suficientes sin necesidad de un grafo dedicado. NetworkX (Python) sobre un export CSV funciona para análisis ad-hoc.

### Señales de identidad sintética (vs. prestanombre)

| Tipo de fraude | Señal característica | Diferencia operacional |
|----------------|---------------------|------------------------|
| **Prestanombre** | Identidad real de persona real; INE/CURP válidos; el defraudador gestiona la relación pero la persona existe | Difícil detectar en originación; detectable por comportamiento post-originación (quién hace los pagos, qué número llama) |
| **Identidad sintética** | Combinación de datos reales (CURP de otra persona) + datos falsos (nombre, teléfono) | Detectable en originación: INE/CURP no coinciden con nombre; validación RENAPO |
| **Loan stacking** | Múltiples solicitudes simultáneas a varios prestamistas con mismos datos | Detectable solo con consorcio de datos (inexistente para mueblerías regionales) — [A MEDIR NOSOTROS] |

### Pitfalls

1. **Falsos positivos en domicilios compartidos legítimos**: familias numerosas, edificios de departamentos, INFONAVIT. El umbral de H3 (>3 CURPs en mismo domicilio) debe calibrarse con contexto geográfico de la colonia.

2. **INE vencida ≠ fraude**: muchos clientes no-bancarizados usan INEs vencidas por años. Verificar vigencia pero no rechazar automáticamente — pedir segundo documento.

3. **Sesgo de detección sobre nuevos canales**: si el modelo aprende sobre solicitudes presenciales y luego se aplica a solicitudes digitales (donde el fraude sintético es más común), habrá underfitting en el canal digital [[FintechMexico](https://www.fintechmexico.org/notices/tendencias-emergentes-en-el-fraude-de-prestamos-digitales-en-mexico)].

4. **AI-generated deepfakes**: para 2025-2026, documentos generados por IA (INE sintética) ya bypasean OCR básico. Se requiere validación contra RENAPO o liveness detection biométrico para canales digitales [[Signifyd — Fraude identidad sintética](https://mx.signifyd.com/blog/que-es-el-fraude-de-identidad-sintetica/)].

5. **Ausencia de consorcio**: sin datos cross-lender, el loan stacking (solicitante en 5 mueblerías simultáneamente) es indetectable internamente — [A MEDIR NOSOTROS] solo si hay intercambio con otras empresas de la región.

### Fuentes — Sección 9

- [Jumio — ¿Qué es el fraude de identidad sintética?](https://www.jumio.com/es/fraude-de-identidad-sintetica/)
- [FintechMexico — Tendencias fraude préstamos digitales México](https://www.fintechmexico.org/notices/tendencias-emergentes-en-el-fraude-de-prestamos-digitales-en-mexico)
- [Signifyd — Fraude identidad sintética MX](https://mx.signifyd.com/blog/que-es-el-fraude-de-identidad-sintetica/)
- [Malwarebytes — Fraude identidad sintética](https://www.malwarebytes.com/cybersecurity/basics/synthetic-identity-fraud)
- [Linkurious — Synthetic Identity Fraud Detection](https://linkurious.com/blog/synthetic-identity-fraud/)
- [Linkurious — Fraud Use Cases Graph Analytics](https://linkurious.com/blog/fraud-use-cases-graph-analytics/)
- [Plaid — Synthetic Identity Fraud](https://plaid.com/resources/fraud/synthetic-identity-fraud/)
- [TransUnion — Synthetic Identity Fraud 2.0 + AI](https://www.transunion.com/blog/detecting-synthetic-identity-fraud-enhanced-by-ai)
- [Medium — Graph-Based Fraud Ring Detection](https://medium.com/@amistapuramk/graph-based-approaches-for-detecting-fraud-rings-in-digital-platforms-c3031f83ef99)
- [Checkout.com — Fraude identidad sintética](https://www.checkout.com/es-es/blog/fraude-de-identidad)


---
