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
