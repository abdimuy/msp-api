# Diseño — Sistema de Inteligencia (Cliente + Negocio) y Motor de Winback

> **Estado:** diseño aprobado en brainstorming (2026-06-13). Pendiente: plan de implementación.
> **Insumos:** `docs/research/inteligencia-cliente-estado-del-arte.md`,
> `docs/research/inteligencia-cliente-diccionario-datos.md`,
> `docs/research/inteligencia-negocio-estado-del-arte.md`,
> `docs/ventas-ai-estrategia.md` (§16 corregido).

## 1. Resumen ejecutivo

Construir un sistema de inteligencia de datos para la mueblería (msp-api) con **dos pilares sobre
una sola fundación de datos analítica**, más un **motor de winback** que convierte la inteligencia
en acción vía WhatsApp con IA:

- **Pilar 1 — Customer Intelligence:** un perfil derivado, rico y explicable, **por cliente**
  (segmentación RFMS, riesgo, ciclo de vida, next-best-product, propensión, LTV, banderas de
  fraude). Sirve tanto al motor como a la "ficha Cliente 360" que la oficina consulta para control.
- **Pilar 2 — Business Intelligence:** analítica **agregada** del negocio (ventas/KPIs, "core
  products" ABC, inventario/GMROI, desempeño por ruta/zona/cobrador, salud de cartera, y
  control/antifraude — el dolor #1 del dueño).
- **Fundación compartida:** un modelo dimensional (`FACT_VENTAS`, `FACT_COBROS` + dimensiones)
  materializado, del que **ambos pilares y el motor leen lo mismo**. Un hecho, muchas vistas.
- **Motor de Winback:** cola priorizada por el motor + conversación con IA donde **el usuario casi
  nunca escribe directo al cliente** (la IA redacta; autonomía por niveles de sensibilidad), con
  **grupo de control** para atribución honesta.

Principio rector: **engine = UI**. La UI no inventa nada; proyecta exactamente lo que el motor
materializa, y cada score viaja con su *driver* (el "por qué"). Construir el motor "UI-ready"
significa exponer cada output con su explicación desde el inicio.

## 2. Objetivo y alcance

**Objetivo:** dar a la mueblería (a) un motor de reactivación/venta accionable y (b) control desde
la oficina, ambos alimentados por un perfil de datos rico — lo más rico posible **con la data que
ya existe**, a nivel "científico de datos, estado del arte 2026".

**En alcance (este diseño):**
- La fundación dimensional materializada y su estrategia de refresh.
- El perfil de cliente (features + scores + explicabilidad).
- La analítica de negocio (ventas, producto, inventario, operación, cartera, control).
- El motor de winback (priorización, NBA, conversación con IA, autonomía por niveles, control group).
- El contrato de datos "UI-ready" que consumirá el frontend.

**Fuera de alcance (otros specs/fases):**
- La implementación del **frontend** (app Tauri/web separada). Aquí se define qué expone la API y la
  *idea general* de la UI (mockups en `.superpowers/brainstorm/`), no el código del cliente.
- El **canal WhatsApp** (verificación Business API, whatsmeow) y el runtime del bot de conversación
  — diseño propio posterior.
- Verificación de identidad documental (INE/CURP) — **no soportable** con la data actual (gap duro).
- Cualquier escritura/cambio a las tablas Microsip nativas.

## 3. Decisión de arquitectura: dos pilares sobre fundación compartida

Confirmado por el estado del arte (Walmart Retail Link, Mercado Libre, Target, Shopify) y por las
restricciones de stack: el patrón universal es **OLTP → transformación batch → modelo dimensional →
capa de métricas → muchas superficies de consumo**. No se construyen dos sistemas; se construye una
capa analítica de la que cuelgan: dashboards de negocio, ficha de cliente y motor de automatización.

```
Firebird (Microsip, OLTP)  ──worker Go (batch + incremental, watermark)──►  Tablas MSP_AN_* (OLAP-lite)
   DOCTOS_PV, DOCTOS_CC,                                                     FACT_VENTAS, FACT_COBROS,
   IMPORTES_*, ARTICULOS,                                                    DIM_FECHA/CLIENTE/PRODUCTO/RUTA,
   LINEAS, SALDOS_IN, ...                                                    PERFIL_CLIENTE (+ scores)
                                                                                      │
                                          ┌───────────────────────────────┬──────────┴───────────┐
                                   Business Intelligence            Customer 360            Motor Winback
                                   (dashboards agregados)        (ficha por cliente)     (cola + conversación IA)
                                          └──────────── todos vía capa de métricas (Go) ───────────┘
```

## 4. Fundación de datos

### 4.1 Hechos verificados (resumen; detalle en los docs de research)
- **La venta vive en `DOCTOS_PV`** (contado + crédito), `CLIENTE_ID` 100% poblado; `DOCTOS_CC` es la
  **capa de crédito** encima. RFM/recencia se anclan en `DOCTOS_PV`, no en `DOCTOS_CC`.
- Split: crédito 104,013 ventas / $664.6M (84% valor); contado 317,562 / $123M. Enganche (~$35M) en
  `DOCTOS_CC` concepto 24533 / `LIBRES_CARGOS_CC`.
- Cobertura de teléfono 52.7% (`DIRS_CLIENTES`) + `MSP_LOCAL_SALE` (móviles frescos de campo).
- Catálogo de conceptos CC verificado; `LIBRES_CARGOS_CC` desde 2018; prima de financiamiento +45–64%.
- Gaps duros: identidad documental (INE/CURP ~0%), aval (3.1%), margen exacto histórico (costo puntual),
  FK directa PV→CC (enlace por `FOLIO`+`CLIENTE_ID`+`SISTEMA_ORIGEN`).

### 4.2 Modelo dimensional (Kimball mínimo)
- **`FACT_VENTAS`** — grano: línea de venta (`DOCTOS_PV_DET`). Medidas: importe, costo aprox., margen,
  tipo (contado/crédito), prima de financiamiento. FKs a DIM_FECHA/CLIENTE/PRODUCTO/RUTA/VENDEDOR.
- **`FACT_COBROS`** — grano: movimiento CC (cargo/abono/enganche/castigo). Medidas: importe, IVA,
  días-a-pagar, atraso. FKs a DIM_FECHA/CLIENTE/RUTA/COBRADOR + link al cargo (`DOCTO_CC_ACR_ID`).
- **Dimensiones:** `DIM_FECHA`, `DIM_CLIENTE`, `DIM_PRODUCTO` (con LINEA/categoría), `DIM_RUTA`/zona,
  `DIM_COBRADOR`/vendedor.
- **`PERFIL_CLIENTE`** — la fila rica por cliente (feature store): features descriptivas + scores +
  drivers + predicciones. Es un rollup materializado sobre los facts.

### 4.3 Materialización y refresh
- Firebird **no tiene vistas materializadas** (bug CORE822). Se usan **tablas ordinarias `MSP_AN_*`**
  pobladas por un **worker en Go** (paquete `internal/analytics`), con **watermark** (incremental por
  `FECHA`/id) + reconstrucción nocturna completa.
- Regla del proyecto (CLAUDE.md): toda la lógica (IDs, timestamps, scoring) vive en Go; las `MSP_AN_*`
  son estructurales. Ojo con el bug del driver (`SUM` sin escalar → `CAST(... AS NUMERIC)`).

#### Arquitectura de escala (validada en R1, jun-2026)

Tiempos medidos sobre 43k filas de cliente en el snapshot:
- **Full refresh:** lectura Microsip ~11 s + escritura `MSP_AN_*` ~39 s → total ~50 s.
- El refresh **corre en el worker async**, no síncrono por HTTP (`POST /winback/refresh` devuelve
  202 inmediatamente; el worker procesa en background).
- **Saldo:** se lee de `MSP_SALDOS_VENTAS` (una fila por cargo); un join de `IMPORTES_DOCTOS_CC`
  sobre `DOCTOS_PV` a nivel de fila explota (~450 M combinaciones) e infla `MONETARY` — bug real
  hallado y corregido.
- **Upsert por lotes:** el driver `firebirdsql` v0.9.19 falla con error -804 al enlazar parámetros
  `?` en `MERGE USING (SELECT ?)`. Solución: `EXECUTE BLOCK` (batch) o UPDATE-then-INSERT. No usar
  `MERGE` con parámetros en queries de upsert masivo.
- **DuckDB** como substrato OLAP embebido (binario Windows, sin servidor, proceso batch): candidato
  para R3 cuando lleguen queries DS pesadas (BG/NBD sobre historial completo, Gamma-Gamma,
  uplift). Se incorpora como paso batch previo al refresh Go, no como servidor permanente.

## 5. Pilar 1 — Customer Intelligence (perfil unificado)

Un `PERFIL_CLIENTE` que es a la vez features del motor y la ficha 360. Familias:

- **A. Identidad & contactabilidad:** datos base, tenure, mejor teléfono (cruce DIRS + MSP_LOCAL_SALE),
  score de contactabilidad, de-dup heurístico, banderas de identidad/fraude.
- **B. Comportamiento de compra (sobre `DOCTOS_PV`):** RFM + S (solvencia), segmento nombrado, mezcla
  contado/crédito, ticket y tendencia, ciclo de recompra personalizado → próxima compra estimada,
  ownership de categorías, next-best-product (market basket).
- **C. Pago & crédito (`DOCTOS_CC` + `MSP_PAGOS_RECIBIDOS`):** saldo, % pagado, "por liquidar",
  puntualidad/atrasos, tier de riesgo conductual, perfil de financiamiento, prima generada, castigos.
- **D. Valor (LTV):** LTV histórico (con prima) y predictivo simple, decil de valor, contribución/año.
- **E. Ciclo de vida & propensión:** estado (nuevo/activo/por-liquidar/dormido/frío/castigado/perdido)
  anclado al ciclo ~11 meses; propensión a recompra (cold-start heurístico → modelo); score de
  priorización de winback; Next Best Action.
- **F. Atribución/contexto operativo:** cobrador/ruta/zona, vendedor (caveat IDs opacos), canal.
- **G. Capa de explicación:** timeline unificado (compras+pagos+visitas), drivers de cada score,
  alertas accionables.

### 5.1 Enfoque "científico de datos 2026"
- **Descriptivo → diagnóstico → predictivo → prescriptivo**, no solo números actuales.
- **Predicciones con incertidumbre:** próxima compra (survival/hazard), churn, LTV predicho — con
  intervalo/confianza explícitos.
- **Atribución tipo SHAP:** cada score se desglosa en contribuciones por feature (transparencia, no
  caja negra).
- **Benchmark vs. cohorte:** percentiles del cliente contra clientes similares.
- **Cold-start sin etiquetas:** arranca con heurísticas RFM/reglas; evoluciona a modelo cuando hay
  historial etiquetado (bootstrapping desde eventos de recompra reales).
- **Tendencias en sparklines + detección de cambio de comportamiento.**

### 5.2 Cliente 360 (una pantalla, varios tabs)
Tabs: **Resumen** (default limpio: NBA + scores + timeline + crédito + next-best-product + contacto),
**Análisis & predicción** (predicciones+IC, SHAP, peer benchmark, tendencias), **Pagos & solvencia**,
**Productos**, **Riesgo & fraude**. La profundidad se reparte en tabs para no saturar.

## 6. Pilar 2 — Business Intelligence

Familias y pantallas:

- **H. Analítica de ventas:** KPIs (ventas, ATV, UPT, # ventas, margen de contribución, mezcla
  crédito) con drill-down (día/semana/mes × zona/categoría); comp/same-store/zone (LFL); YoY/MoM;
  estacionalidad + pronóstico **Holt-Winters** (supera a redes neuronales a nuestra escala); cohortes.
- **I. "Core products" / producto:** **ABC/Pareto por margen de contribución** (clase A/B/C, %
  acumulado, curva de Pareto), desempeño por categoría (47 reales), ciclo de vida del producto,
  prima de financiamiento por categoría/plazo, pares complementarios.
- **J. Inventario & costo:** rotación (turns), DIO, GMROI por categoría (margen ⚠️ aproximado), stock
  muerto / sobre / sub.
- **K. Operativo (control):** desempeño por ruta/zona/cobrador/vendedor (ventas, cobranza, conversión
  visita→venta, productividad), penetración y comp por zona, rankings.
- **L. Salud de cartera (agregado):** PAR30/60/90, CEI, DSO, aging quincenal, **roll rates** (punto
  crítico M1→M2), cosechas (vintage), salud por zona; puente a winback ("por liquidar").
- **M. Control / antifraude (dolor #1):** **gap de captura app vs ERP** (los ~17k pagos huérfanos),
  float (días cobro→depósito), **Z-score de recovery rate** por ruta (outliers), ratio de
  ajustes/cancelaciones; alertas con "por qué" + drill-down al pago/cliente.

Pantallas: **Ventas & Productos**, **Cartera & Cobranza**, **Control & Fugas** (cada una con tabs).

## 7. Motor de Winback (la inteligencia → acción)

- **Cola priorizada:** la lista *es* la salida del score (propensión × valor × contactabilidad ×
  "por liquidar"), filtrable por segmento/ola; estados ENVIADO/RESPONDIÓ/HANDOFF/CONTROL.
- **Conversación con IA + handoff:** el bot conversa; cuando hay intención de compra/precio, sugiere
  handoff al humano con el contexto del 360 cargado.
- **Redacción asistida por IA (decisión clave):** el usuario **casi nunca escribe directo al cliente**.
  Da una *indicación interna* (nota/clic/dictado) y la **IA redacta el mensaje final** (personalizado
  con nombre/producto/historial, voz de marca). El humano **Aprueba / Regenera / Ajusta tono**;
  "editar manual" es **excepción**.
- **Autonomía por niveles de sensibilidad (modelo B):**
  - 🟢 **Auto-enviar (IA sola):** saludos, "ya casi liquidas", recordatorios suaves, reactivación,
    FAQ, confirmaciones.
  - 🟡 **Borrador + aprobar (1 clic):** cotización/precio, cierre/visita, objeciones, cambios de plan,
    promesas.
  - 🔴 **Handoff dirigido:** quejas, temas delicados/legales, montos altos, cliente molesto, datos
    sensibles.
  - **Salvaguardas:** umbral de confianza (si la IA duda, sube 🟢→🟡), kill switch, respeto a
    STOP/opt-out, horario + límite diario (anti-baneo), todo auditable, humano en el loop en 🟡/🔴.
- **Grupo de control + atribución:** holdout (~15%) y uplift vs. control — no-negociable para medir
  el incremental real sobre la reactivación orgánica.

## 8. UI / experiencia (north-star)

- **Cockpit action-first** (decisión aprobada): el inicio es "qué hacer hoy" (winback priorizado +
  alertas de control + pulso), no una pared de gráficas.
- **6 pantallas hero:** Inicio·Cockpit, Cliente 360, Ventas & Productos, Cartera & Cobranza,
  Control & Fugas, Winback (+ Segmentos & Reglas).
- **Principios de diseño 2026:** action-first; **explicabilidad por diseño** (cada score con su
  driver); **fuente única** (UI/dashboards/motor leen el mismo perfil/fact); **command-K + drill-down**
  de cualquier agregado al cliente/folio; **divulgación progresiva** (tabs + sparklines) para ser rico
  sin saturar.
- **Aclaración de stack:** la UI **no es una app nueva** — las pantallas entran como **sección/módulo
  nuevo dentro de la app Tauri/web de oficina/admin que ya existe** (reúsa su shell, navegación, auth y
  el CORS Tauri/Windows ya configurado). "Separado" solo significa codebase distinto del Go (frontend
  vs backend). El diseño asegura que **msp-api exponga datos "UI-ready"** — cada endpoint entrega el
  valor + sus drivers + el contexto que la pantalla necesita, para que motor y UI no sean ajenos.
- Mockups de referencia: `.superpowers/brainstorm/61816-1781391947/content/` (north-star, cliente-360
  v1/v2, ventas-productos, control-fugas, cartera, winback, winback-ai-redaccion, autonomy-tiers).

## 9. Construible hoy vs. gaps

**Construible ya con la data:** RFM/RFMS, comportamiento/solvencia de pago, ciclo de vida,
next-best-product, riesgo conductual, LTV histórico (con prima), todos los KPIs de negocio, ABC/core
products, cartera (PAR/CEI/roll/vintage), control/antifraude (gap de captura, Z-score, float).

**Gaps (marcar explícitos, no inventar):** verificación de identidad documental (INE/CURP ~0%);
antifraude por aval (3.1%); margen **aproximado** (costo puntual, no histórico); atribución
producto→CxC no automática; vendedor real desde `LIBRES_CARGOS_CC` (IDs opacos); prima de
financiamiento solo desde 2018. Señales de fraude por atributo compartido limitadas por cobertura de
teléfono (52.7%).

### Hallazgos validados con datos reales (jun-2026)

Resultado de correr los algoritmos contra los datos reales de la BD de desarrollo durante R1/R1+:

**BG/NBD + Gamma-Gamma validado.**
Ajusta y scorea **43,399 clientes** en ~1 s. **65 % "vivos"** (P\_alive > 0.5). CLV a 12 meses
promedio de repetidores: **~$1,873**. El modelo **reordena el ranking** respecto al heurístico RFM y
atrapa falsos positivos: compradores de una sola vez con ticket alto que el heurístico trata como
candidatos valiosos pero que tienen probabilidad de recompra baja. → **Es el score de R2.**

**Dimensión de score objetivo: RFM + S(olvencia) + CLV ajustado por riesgo.**
- Solvencia = `EstadoPago` (al-corriente/atrasado/moroso) + puntualidad (stretch/cadencia inferida).
- Riesgo-ajustado = ingreso futuro esperado (BG/NBD) − pérdida esperada (PD) − costo de cobranza.
- Uplift modeling: gateado a acumulación de suficiente grupo de control (calendario, no estimación).

**Calidad de datos Microsip** (ver diccionario §"Calidad de datos de crédito/pagos"):
la fecha de `DOCTOS_CC` es la de validación en oficina (97 % mismo día que el cobro real; ruido
despreciable para features semanales/mensuales). El plazo del crédito se infiere del plan de pagos;
las cadencias son mixtas (71 % semanal, 23 % quincenal, 6 % mensual) y se infieren por crédito.

## 10. Estructura de módulos (vertical slices)

Conforme a CLAUDE.md (módulos por slice, cruces solo vía contracts):

- **`internal/analytics/`** — fundación dimensional + BI + feature store.
  - `domain/` (facts, dims, perfil, scores como VOs), `app/` (cálculo de features/scores/KPIs),
    `metrics/` (capa de métricas: definiciones únicas), `infra/analyticsfb/` (lectura Microsip +
    lectura/escritura `MSP_AN_*`), `infra/worker/` (refresh batch+incremental con watermark),
    `infra/http/` (endpoints UI-ready de dashboards y perfil), `analytics_contracts.go`.
- **`internal/reactivacion/`** — motor de winback.
  - `domain/` (campaña, cola, mensaje, política de autonomía, grupo de control), `app/`
    (priorización, NBA, orquestación de conversación, redacción IA, atribución), `ports/outbound/`
    (perfil de cliente vía contrato de analytics; canal de mensajería; proveedor IA),
    `infra/` (adaptadores), `reactivacion_contracts.go`.
- Reglas duras del repo aplican (sin lógica en DB, UTF-8, fechas UTC + `firebird.ToWallClock`,
  inglés en código / español al usuario).

## 11. Ruta de implementación — rebanadas verticales, demo-first (enfoque elegido)

**Enfoque elegido:** NO "API completa primero". Se construye en **rebanadas verticales de punta a
punta** (dato → endpoint → pantalla mínima), API y UI **a la par**, empezando por la rebanada que da
el **primer número** (winback). Razones: el riesgo #1 es no shippear el demo; el contrato "UI-ready"
se valida construyendo la UI encima; y aprovechamos la fuerza de frontend del dev.

**Reglas del enfoque:**
- Cada rebanada termina en **algo que el dueño puede ver/usar**.
- La **fundación dimensional se endurece incrementalmente** (no big-bang): la rebanada 1 usa un
  `FACT_VENTAS` mínimo + una tabla `MSP_AN_*` simple; rebanadas siguientes agregan facts/dims/scores a
  medida que los piden.
- La UI vive en la **app Tauri/web de oficina/admin existente** (sección nueva); el plan de msp-api
  cubre el lado API de cada rebanada, asumiendo una rebanada de UI en paralelo (en esa app) que la
  consume.

**Rebanadas (cada una con su plan propio):**
1. **Rebanada 1 — Demo de winback (el primer número):** ✅ **ENTREGADA (R1 + R1+, jun-2026).**

   Lo que se entregó en `internal/analytics`:
   - **Materialización:** tablas `MSP_AN_WINBACK_CANDIDATOS` + `MSP_AN_REFRESH_STATE` (anclas
     materializadas; scores/segmento se computan al leer) + worker de refresh en background con
     single-flight.
   - **Endpoints:** `GET /v2/analytics/winback` (lista priorizada), `GET /v2/analytics/winback/attribution`
     (tratamiento vs control + uplift), `POST /v2/analytics/winback/refresh` (async 202).
   - **R1+ — enriquecimiento post-demo:**
     - Señal de **solvencia**: `EstadoPago` (al-corriente/atrasado/moroso) + `FECHA_ULTIMO_PAGO`.
     - **Calibración del score**: recencia modelada como ventana-winback con pico (lineal por tramos)
       + multiplicador de solvencia; el filtro por defecto excluye clientes ACTIVO y NUEVO.
     - **Capa de frases** (etiqueta/resumen/tier) para lectura directa en oficina sin interpretar
       números crudos.
   - Nota: scores son **heurísticos** en R1+. BG/NBD (probabilístico) entra en R2.

2. **Rebanada 2 — Cliente 360 (Resumen):** completar el perfil (segmento, tier de riesgo, crédito,
   timeline, contacto) + endpoints + tab Resumen.
3. **Rebanada 3 — Cliente 360 (Análisis & predicción):** scores DS (propensión, churn, LTV
   predicho) con incertidumbre + atribución (drivers) + peer benchmark.
4. **Rebanada 4 — Business Intelligence: Ventas & Productos** (KPIs + core products/ABC + estacionalidad).
5. **Rebanada 5 — Cartera & Cobranza** (PAR/CEI/aging/roll/vintage).
6. **Rebanada 6 — Control & Fugas** (gap de captura, Z-score, float, alertas).
7. **Rebanada 7 — Conversación IA** (redacción asistida + autonomía por niveles + handoff) +
   integración del **canal WhatsApp** (spec propio).

En cada rebanada, la fundación dimensional y la capa de métricas se amplían lo necesario; al final de
la rebanada 6 la fundación está completa de forma natural, sin una fase de "plomería" aislada.

## 12. Preguntas abiertas / [A MEDIR NOSOTROS] antes de definir tablas

- Volumen real y grano óptimo de `FACT_VENTAS`/`FACT_COBROS` (filas, índices).
- Clientes activos con historial suficiente para scores predictivos confiables.
- Frecuencia de refresh aceptable (¿nocturno basta, o incremental cada N min?) según uso del dashboard.
- # de SKUs distintos y cardinalidad de `DIM_PRODUCTO`.
- Calibración local de umbrales marcados `[A MEDIR NOSOTROS]` en los docs de research (RFM cuantiles,
  recencia por ciclo, tasas de churn/castigo, GMROI, conversión).

## 13. Referencias
- Estado del arte cliente: `docs/research/inteligencia-cliente-estado-del-arte.md`
- Diccionario de datos (cliente/venta/pago): `docs/research/inteligencia-cliente-diccionario-datos.md`
- Estado del arte negocio: `docs/research/inteligencia-negocio-estado-del-arte.md`
- Estrategia y mapa de tablas: `docs/ventas-ai-estrategia.md` (§16)
- Memoria: `reference_microsip_sales_structure`, `project_winback_pilot`.
