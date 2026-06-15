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
