# Diseño — Tres scores de cliente para la mueblería de crédito

> Fecha: 2026-06-17 · Estado: **diseño aprobado para implementar** (R1 ya existe)
> Negocio: mueblería con crédito propio + cobranza en ruta (semanal), ~43k clientes, ticket ~$6,000, margen ~53%.
> Fundado en: investigación citada (Fader/Hardie BTYD/CLV; FDIC scoring taxonomy; RALTV) + EDA de nuestros datos.

## 0. Principio rector (regla de oro, respaldada por reguladores)

**Un score = un resultado = un propósito.** La FDIC (Credit Card Activities Manual, cap. VIII) y FICO lo establecen: cada modelo predice **un solo outcome** y se usa solo para eso. Mezclar señales no relacionadas en un número (el "customer health score" / RFM compuesto) es un anti-patrón documentado — y es exactamente el defecto del "Score" winback actual (recencia+valor+contacto+por-liquidar), cuyo tope artificial en 85 para liquidados es el síntoma de manual.

Por eso construimos **tres scores especializados**, no uno compuesto, y los operamos juntos en una **matriz riesgo × valor** que mapea a las dos decisiones del dueño: *¿a quién le vendo/extiendo crédito?* y *¿a quién priorizo en cobranza?*

Los tres comparten el **mismo sustrato transaccional** (DOCTOS_PV ventas · MSP_PAGOS_VENTAS abonos · MSP_SALDOS_VENTAS cargos/saldos) y la **misma disciplina point-in-time** (features a una fecha de observación T, label en ventana posterior, validación out-of-time, entrenar offline / aplicar en Go) → consistencia entre scores.

---

## 1. Score 1 — RIESGO DE CRÉDITO (default) · YA CONSTRUIDO

**Outcome:** probabilidad de incumplimiento (roll-to-non-performing) de una cuenta de crédito viva, ~12 meses adelante. Es el **número primario** para el dueño (decisión de cobranza y de extender crédito).

Detalle completo en [project_credito_scorecard_pit] y `analysis/creditscorecard/README.md`. Resumen:
- Behavioral PD point-in-time; label = castigo formal **o** silencio-mientras-debe ≥90d; cohortes maduras; validación OOT → **Gini 0.86, KS 0.73**.
- 6 features conductuales (días sin pagar dominante; pagos_90d; % a tiempo; cadencia; num_pagos; antigüedad). NO usa saldo actual (leakage: el castigo zerea el saldo).
- Servido en Go (`buildCreditoFeatures` + `scorecard.json` embebido v1); aplica solo a saldo>0 + performing (pago dentro de 180d). Banda BAJO/MEDIO/ALTO/CRITICO.

**Convención del score:** 0–100, **alto = mejor pagador / menor riesgo** (FICO-style). Esta convención se mantiene en los tres.

---

## 2. Score 2 — PROPENSIÓN A RECOMPRA (next-purchase)

**Outcome (un solo target):** `P(el cliente hace una nueva compra a crédito dentro de N meses de la fecha de observación T)`. Forward-looking, misma disciplina de ventana que el score de crédito.

### 2.1 Por qué importa y datos que lo aterrizan (EDA propia)
- **Tasa de recompra: 46.6%** de clientes tienen ≥2 compras (26.6% ≥3, 10.2% ≥5). Es un negocio de recompra fuerte → el score vale mucho (alimenta las "hojas" que el dueño ya reparte a vendedores, para que **conviertan**).
- **Intervalo entre compras:** mediana ~6 meses; dentro de **12 meses ocurre el 69%** de las recompras (18m: 81%, 24m: 88%).
- **Los clientes que aún deben SÍ recompran:** **50% de las recompras ocurren <6 meses** del cargo previo → muchos compran un segundo mueble **mientras siguen pagando el primero**.
- Estacionalidad leve (dic/ene un poco más altos por aguinaldo).

### 2.2 Decisiones de diseño
- **Ventana N = 12 meses** (captura ~69% de recompras; balancea recall vs accionabilidad). Configurable; revisar con out-of-time.
- **Población elegible = TODO cliente con ≥1 compra previa.** NO se excluye a los que deben (saldo>0); el comportamiento de pago entra como **feature** y se segmenta buen/mal pagador (la investigación confirma que la relación predictor→recompra difiere entre buenos y malos pagadores). Esto es lo opuesto al score de riesgo (que sí exige performing) — porque aquí el outcome es "comprar", no "pagar".
- **Censura (pitfall crítico):** un cliente que "todavía no recompra" **no es** un no-recomprador — es dato censurado a la derecha. Se maneja con la ventana de desempeño fija (igual que el crédito) y/o framing de supervivencia; nunca etiquetar como negativo a quien simplemente no ha tenido tiempo.

### 2.3 Método (world-class pero realista)
Patrón recomendado por la literatura para **durables, escasos, no-contractuales**: **híbrido BTYD + ML**, no elegir uno.
1. **BTYD discreto BG/BB** (análogo discreto de Pareto/NBD; Fader/Hardie/Shang 2010) — apropiado cuando no se distingue churn de hiato largo y las compras caen en una rejilla discreta. Usa solo estadísticos RFM por cliente: frecuencia (x), recencia (t_x), longitud de observación (T). Produce `P(activo)` y `E[compras futuras]`.
2. **La probabilidad BG/BB se inyecta como feature** dentro de un clasificador ML (logística/GBM) junto con: recencia, frecuencia, antigüedad, monetary, comportamiento de pago (% a tiempo, días sin pagar, saldo_frac, flag liquidado), estacionalidad. Resultados publicados (EJOR 2022): el híbrido supera a BG/BB solo y a ML solo, y la predicción BTYD es el feature más influyente.

**Arranque realista dado nuestro tamaño/escasez** (mediana 1–2 compras/cliente): empezar con **BG/BB + regresión logística** (pocos features robustos); subir a GBM solo si el lift lo justifica. Validar con la misma maquinaria PIT que ya tenemos.

### 2.4 Serving y calibración
- 0–100, **alto = más propenso a recomprar**. Bandas por **cuantiles** (data-driven, evita topes artificiales) — ALTA/MEDIA/BAJA propensión.
- Evaluación: AUC/lift + **calibración** (que P predicha ≈ tasa observada) + tabla de deciles.

---

## 3. Score 3 — CLV (valor de vida, ajustado por riesgo)

**Outcome:** valor económico esperado del cliente a un horizonte (24–36 meses), **ajustado por riesgo de default**.

### 3.1 Definición (Fader/Hardie)
`CLV = margen × (valor por transacción) × DET` donde **DET = discounted expected transactions** (flujo descontado de compras futuras), pronosticado por **el MISMO modelo BTYD** que alimenta la propensión → **consistencia garantizada** entre score 2 y score 3.

- **Componente monetario:** debiased con el sub-modelo **Gamma-Gamma** (encoge el promedio observado del cliente hacia la media poblacional; crítico porque nuestros clientes tienen pocas compras — el promedio crudo no es confiable hasta ~7–8 transacciones, y casi nadie llega). Por la escasez, considerar de inicio un **CLV simplificado** = (margen × ticket promedio poblacional encogido) × E[compras futuras BTYD], y refinar con Gamma-Gamma después.
- Margen ~53% (ver [project_verified_unit_economics]).

### 3.2 Ajuste por riesgo (no negociable en crédito)
El CLV plano **sobreestima** en crédito porque los clientes más rentables suelen ser los más riesgosos, e ignora la pérdida por default. Se corrige integrando el **score de riesgo (Score 1)**. Opciones (la investigación presenta varias sin head-to-head; elegimos por interpretabilidad):
- **Recomendado — valor esperado:** `CLV_ajustado = E[margen futuro] × P(paga) − E[pérdida por default]`, donde `P(paga)` viene del Score 1. Simple, interpretable, accionable.
- Alternativas: RALTV (Risk-Adjusted Lifetime Value, supera a CLV plano en datos de tarjeta) o incrustar la retención `r_i` directo en el NPV: `CLV_i = (cm_i·12·r_i)/(1+d−r_i)`.

### 3.3 Serving
0–100 por **terciles/cuantiles** (Bajo/Medio/Alto valor), o como monto en pesos en la ficha. Migración de terciles para next-best-action.

---

## 4. Integración — la matriz Riesgo × Valor (+ propensión como lente de acción)

Los líderes (retail, banca, fintech, telcos) **no fusionan**; corren los scores juntos en una matriz y derivan **next-best-action**. Para nosotros:

| | **Alta propensión recompra** | **Baja propensión** |
|---|---|---|
| **Bajo riesgo** | 🟢 Vender más / subir línea de crédito (hojas que SÍ convierten) | 🔵 Reactivar (winback rinde: pagan) |
| **Alto riesgo** | 🟡 Vender con más enganche / vigilar cobranza | 🔴 No extender crédito; enfocar cobranza/recuperación |

- **Valor (CLV)** prioriza *dentro* de cada celda (a quién dedicar el esfuerzo primero).
- UI: tres badges/columnas separadas + (futuro) vista de matriz 2×2. Nunca un solo número compuesto.

---

## 5. Sustrato de datos y consistencia entre scores

- **Estadísticos RFM compartidos** por cliente desde DOCTOS_PV (ventas: recencia/frecuencia/monetary) + MSP_PAGOS_VENTAS (comportamiento de pago) + MSP_SALDOS_VENTAS (cargos/saldo, antigüedad).
- **El mismo motor BTYD** alimenta propensión (P recompra) y CLV (DET) → no se contradicen.
- **Disciplina point-in-time** en los tres: features ≤ T, label en (T, T+ventana], validación out-of-time + vintage, entrenar offline en `analysis/` y **aplicar en Go** con artefacto versionado embebido (patrón `scorecard.json`).
- **Todo a nivel CLIENTE_ID** (los abonos no enlazan a cargos por DOCTO_CC_ID).

## 6. Pitfalls a diseñar en contra (todos los scores)
- **Leakage** (no usar estado posterior al evento; el saldo actual filtró el castigo en el score de riesgo).
- **Ventanas desalineadas** observación/desempeño (espejar la disciplina del score de crédito).
- **Censura de supervivencia** (no etiquetar "aún no compra/aún no cae" como negativo).
- **Desbalance** (cost-sensitive / class_weight; profit-oriented learning).
- **Escasez** (pocas compras/cliente → preferir BTYD + shrinkage Gamma-Gamma sobre ML pesado; empezar simple).

## 7. Plan de implementación (fases, realista)
1. **Inmediato (UI, barato):** promover el **Score de riesgo (1)** a columna/insignia primaria del directorio; relabelar el "Score" winback actual honestamente (no es crédito) o retirarlo de la vista hasta el rediseño. (Opción A de la discusión.)
2. **Score 2 — propensión a recompra:** harness offline (BG/BB + logística, PIT, ventana 12m, incluye owers) → `propensity.json` embebido → `buildPropensityFeatures` en Go → exponer en pulso + directorio (badge/filtro/orden). Espeja toda la maquinaria del score de crédito.
3. **Score 3 — CLV ajustado por riesgo:** reusa el BTYD del paso 2 + Gamma-Gamma + ajuste por Score 1 → `clv.json`/cómputo → exponer.
4. **Matriz 2×2** en la ficha/directorio (next-best-action).

## 8. Decisiones abiertas (confirmar antes/durante impl)
- **N de la ventana de recompra:** 12m por defecto (EDA: 69%); validar OOT (¿18m para más recall?).
- **Combinación CLV×riesgo:** valor esperado (recomendado) vs RALTV vs NPV-con-retención.
- **BG/BB vs arranque más simple** dado pocas transacciones/cliente — decidir con un spike de ajuste (igual que se hizo con el score de crédito).
- **Horizonte CLV:** 24 vs 36 meses.

## Fuentes clave
- Fader, Hardie & Lee (2005) — BG/NBD ("Counting Your Customers the Easy Way"). Fader, Hardie & Shang (2010) — BG/BB discreto. Fader & Hardie — Gamma-Gamma + DET para CLV.
- FDIC Credit Card Activities Manual, cap. VIII — taxonomía de scoring por propósito (application/behavior/collection/recovery/response/revenue).
- RALTV (Risk-Adjusted Lifetime Value) — supera CLV plano en datos de crédito.
- EJOR 2022 (Chou et al.) — híbrido BG/BB + Lasso supera standalone; BTYD como feature dominante.
- EDA propia (`analysis/creditscorecard/recompra_eda.py`): recompra 46.6%, ventana 12m=69%, owers recompran (<6m=50%).
