# Estrategia de ventas con IA — Documento maestro privado

> **PRIVADO. Nunca se muestra al dueño.** La versión mostrable al dueño vive en
> `ventas-ai-presentacion-dueno.md`.
>
> Consolida todo el contenido valioso de los cinco documentos anteriores. Datos validados
> en la copia Microsip (solo lectura). Corte efectivo: **junio 2026** (anclar antigüedades
> a 2026-06-01). Diseño técnico completo en
> `docs/superpowers/specs/2026-06-06-ventas-ai-winback-system-design.md`.

---

## Índice

1. Resumen ejecutivo
2. El negocio (datos verificados 2025)
3. Economía unitaria — cascada por venta, neto del dueño
4. La oportunidad medida — Tehuacán y la red
   - 4.1 Tehuacán
   - 4.2 Red completa
   - 4.3 El pool de dormidos
   - 4.4 Huecos complementarios — cross-sell secuenciado
   - 4.5 Motor unificado de next-best-product
5. Calidad del crédito — riesgo por número de compras
   - 5.1 Tabla de castigo por historial
   - 5.2 Modelo de laxitud — tiers de expansión
6. Conversión esperada
7. El demo — diseño y proyección mes a mes del Año 1
8. Proyecciones a 3 años — matriz de dos motores
9. Lo que se queda el dueño (neto) y el reparto
10. Compensación — estructura y decisión actual
11. PARTE B — Estrategia privada de negociación
    - 11.1 Lectura honesta del dueño
    - 11.2 Perfil del dueño
    - 11.3 El analgésico — el centro del caso
    - 11.4 La estructura de la fuerza de venta — por qué no habrá guerra
    - 11.5 Fraude de prestanombres — argumento adicional
    - 11.6 Los dos motores de ingreso — cómo presentarlos
    - 11.7 Qué pedir y en qué orden
    - 11.8 El "sí, pero te pago menos" — manual de respuesta
    - 11.9 Escalera de respaldo
    - 11.10 Hasta dónde ceder
    - 11.11 Las 6 tácticas
    - 11.12 Ventaja única
    - 11.13 Lo no negociable
    - 11.14 Lo que el dueño NUNCA debe ver
    - 11.15 Secuencia de convencimiento
12. Segunda palanca — cobranza y OXXO (PAUSADA — Fase 2-3)
13. Prueba de atribución — grupo de control
14. Estado del arte 2026 — técnicas a replicar
15. Precedentes probados — por qué esto funciona
16. Cómo re-consultar la base de datos — receta y mapa de tablas
17. Riesgos y caveats
18. Próximos pasos
19. Costos operativos — WhatsApp y tokens de IA

---

## 1. Resumen ejecutivo

- El **78% del efectivo del negocio ($81.9M en 2025)** entra por el sistema operativo que
  el desarrollador construye y mantiene (cobranza en ruta concepto 87327 + enganche). Ese
  sistema fue la replicación de una app que el dueño pagaba más de $100k/año externamente;
  es decir, es sustitución de costo — justifica el sueldo base, no la comisión por ventas.
- **El negocio ya vive del winback, de forma pasiva.** El 67.2% de las ventas (69.6% del
  dinero) son clientes repetidos. ~6,000+ ventas/año son reactivaciones orgánicas — los
  clientes vuelven solos. El ciclo de recompra promedio es ~331 días. El sistema no inventa
  el motor: lo acelera y captura el segmento que no volvería por sí solo.
- **El analgésico, no la vitamina.** El dolor #1 del dueño no es el dinero — son los
  trabajadores de campo (vendedores y cobradores que manejan efectivo, reportan después,
  pueden defraudar). Le está costando la salud. El sistema entrega control desde la oficina,
  origen verificado, cero fraude de prestanombres. Es analgésico, no vitamina.
- **Oportunidad medida:** 7,603 buenos elegibles en Tehuacán que no compraron en 2025 (de
  los cuales 3,485 son contactables); en la red completa, 26,651 elegibles (11,114
  contactables). El proceso manual de "hojas" ya existe y captura aproximadamente 1 de cada
  5 por año — esa es la línea base de control.
- **Motor unificado next-best-product.** El cross-sell no es "2º artículo mismo día"
  (attach mismo-día: solo 4.4% en tickets <$8k). El cross-sell real es secuenciado —
  ofrecer el complemento cuando el cliente libera abono. Winback y cross-sell colapsan en
  un solo motor que trabaja al cliente por estado (dormido / por-liquidar) ofreciéndole su
  producto faltante. Segmento NUEVO exclusivo: "por liquidar activos con hueco" (~2,982
  clientes que jamás aparecerían en un winback estándar).
- **Las ventas del sistema son las más seguras:** perfil winback (4+ compras, récord limpio)
  proyecta ~1.1% de castigo anual versus ~6% de un cliente nuevo.
- **Margen bruto real 52.8%** (no 54.4% como decían documentos anteriores). Contribución
  después de cobrador, castigo y entrega: ~38%. Neto del dueño por venta winback con
  comisión $285: ~$3,070.
- **Comisión acordada: $285/venta** (equivale a 1 vendedor). Ancla de negociación: $570
  (2 vendedores). Base propuesta: $7,000/sem.
- **Dos motores de ingreso.** Motor 1: winback incremental ($285/venta sobre ventas que no
  existían). Motor 2: canal/desplazamiento — el sistema procesa ventas existentes a media
  comisión ($285), lo que equivale a AHORRO para el dueño vs los ~$570 actuales. Atribución
  distinta; no mezclar ante el hermano DBA.
- **Proyecciones (base $364k + $285/venta):** piso seguro solo con winback ~$400-575k/año;
  con canal (winback + desplazamiento 15-35%) el rango escala a $1.05M-$2.40M en Año 3
  según laxitud del segmento. Conversión sigue siendo hipótesis hasta el demo.
- **Neto del dueño en Año 3 (escenario normal, solo winback ≥2 compras):** ~$2.85M de
  ventas que no existían; con canal 35% sube a ~$7.11M.
- Solo 51% del pool total tiene teléfono (20,828 de ~40,489). Enriquecer con
  `MSP_LOCAL_SALE.TELEFONO` antes del piloto casi duplica el pool contactable — más palanca
  que cualquier ajuste de laxitud.
- El grupo de control es más importante de lo habitual: con tanta reactivación orgánica,
  el sistema cobra solo el LIFT incremental, no las 6,000 ventas que volvían solas.

---

## 2. El negocio (datos verificados 2025)

| Métrica | Valor |
|---|---|
| Ventas a crédito / año | 16,015 |
| — Clientes nuevos (primera compra) | ~4,800 (~32.8%) |
| — Clientes repetidos (2+ compras) | ~11,215 (~67.2%) |
| — Ratio recompra / nuevo | 2.1 : 1 (~846 recompras/mes vs ~400 nuevos/mes) |
| Crédito otorgado (subtotal sin IVA) | $117.3M |
| Ticket promedio sin IVA | $7,322 |
| Ticket promedio con IVA | $8,061 (IVA efectivo 10.09%) |
| Ticket cliente repetido (con IVA) | **$8,034** |
| Ticket cliente nuevo (con IVA) | **$7,207** (+11.5% ventaja repetido) |
| Efectivo cobrado por el sistema (ruta + enganche) | $81.9M = 78% |
| Margen bruto real | **52.8%** (markup 2.12x; COGS $55.3M) |
| Premio de financiamiento (spread crédito vs contado) | 28% (~$26.6M/año) |
| Castigo / pérdida (vintage, público general) | ~2.5% solo castigos; ~3.0% incluyendo cancelaciones |
| Recompra (clientes con 2+ compras) | ~67.2% de las ventas; 69.6% del dinero |
| Reactivaciones orgánicas / año (sin sistema) | ~6,000+ (60.9% de recompras de clientes 6+ meses ausentes) |
| Ciclo de recompra promedio | ~331 días (~11 meses) |
| Pool total con teléfono válido | 20,828 de ~40,489 (**solo 51%**) |
| Vendedores (rutas) | ~39 (RUTA01–39) |
| Crecimiento del crédito otorgado | 2021 $71.0M → 2025 $117.3M (~10%/año) |

**Nota sobre el IVA:** los montos crudos en `IMPORTES_DOCTOS_CC.IMPORTE` son subtotal sin
IVA; el total real incluye `IMPUESTO`. El IVA efectivo (10.09%) casi no se factura y se
trata como margen del dueño por instrucción explícita. El real fiscal es ~14% menor.

**Nota sobre la pérdida:** la métrica "2.16%" de documentos anteriores era un cálculo
same-year que subestima porque el castigo llega con 1-3 años de retraso sobre un libro que
crece ~10%/año. El dato correcto traza el castigo al año de la venta original (vintage):
~2.5-3%. No se usa el 2.16% en ningún cálculo de esta versión.

**Nota sobre las condonaciones:** ~$14M/año (~12% de la venta) parecen una fuga, pero es
el modelo de precios por diseño — aplica precio de contado a quien liquida anticipado
(0-1 mes: ~40% off; 3-6 meses: ~26%; +12 meses: ~4%). La oficina las revisa y autoriza
antes de aplicarlas. No es dinero recuperable ni se presenta como ahorro.

**Nota sobre la reactivación orgánica:** el 60.9% de las recompras proviene de clientes
ausentes 6+ meses, la mayoría sin contacto activo — el negocio reacciva pasivamente ~6,000
clientes/año. Esto hace al grupo de control aún más crítico: el sistema solo cobra el
diferencial sobre esa base orgánica.

**Crecimiento:** el aumento del ~10%/año es mayormente inercia (libro + inflación), no
eficiencia. El valor del desarrollador es el delta sobre ese control.

---

## 3. Economía unitaria — cascada por venta, neto del dueño

**Base de cálculo: ticket con IVA $8,061 (winback, perfil 4+ compras)**

| Concepto | Monto | % |
|---|---|---|
| Ticket (c/IVA) | $8,061 | 100% |
| − COGS | −$3,456 | 42.9% |
| = Margen bruto | $4,605 | 57.1% |
| − Cobrador 10% | −$806 | 10.0% |
| − Castigo perfil winback ~1.5% | −$121 | 1.5% |
| − Entrega 4% | −$322 | 4.0% |
| = Contribución | $3,356 | 41.6% |
| − Comisión desarrollador $285 | −$285 | 3.5% |
| **= Neto del dueño** | **$3,071** | **38.1%** |

*Con castigo del público general (~2.5%): −$202 → contribución $3,275 → neto dueño ~$2,990.*

**Comisión de vendedores (estructura del dueño, a dedo, por tramos, tope $700 total):**
- Venta $8,000 → comisión total ~$600 → $300 por vendedor si son 2.
- Comisión promedio ponderada (distribución real de tickets, con tope): **~$285/vendedor**.
- El tope es $700 total ($350/vendedor) para ventas >$20k.

**Por qué el winback es el canal más rentable:**
- Comisión $285 vs $570 en el modelo de 2 vendedores — la mitad.
- Castigo ~1.5% vs ~2.5% del público general — un tercio menos de pérdida.
- Sin costo de prospección: estos clientes ya conocen la empresa.

**ROI del trato para el dueño:** por cada $1 de comisión que paga al desarrollador,
la empresa retiene ~$10.78 de contribución neta. La razón desarrollador / dueño es 9% / 91%.

**Distribución de tickets en Tehuacán 2025 (con IVA):**
- <$5k: 26.9% · $5-8k: 33.5% · $8-10k: 18.3% · $10-15k: 11.2% · $15-20k: 5.5% · $20k+: 4.3%
- El 60.4% de los tickets son menores a $8k.

---

## 4. La oportunidad medida — Tehuacán y la red

### 4.1 Tehuacán (CIUDAD_ID = 338, 36% del mercado caliente de la red)

Universo filtrado: Público General (TIPO_CLIENTE_ID = 21499), sin castigo, con crédito.

| Estado | Definición | Clientes | Contactables |
|---|---|---|---|
| Buenos (total) | público gral., sin castigo, con crédito | 9,719 | — |
| — compraron en 2025 | activos recientes | 2,116 | — |
| **Hueco (no compraron 2025)** | buenos pero sin venta en 2025 | **7,603** | **3,485** |
| Dormido-fresco (3-9 meses) | liquidado, listo para recompra | — | **1,684** |
| Por liquidar (≥70% pagado) | debe pero casi termina | 1,027 | 817 |
| Activo medio (<70% pagado) | NO contactar todavía | 1,772 | — |
| Dormido medio (9-18 meses) | liquidado, ciclo cumplido | — | — |

**Listos para el demo (prime, contactables):**
- Por liquidar ≥90% pagado: 154
- Dormido-fresco (3-9 meses): 432 en Tehuacán; 1,684 en el segmento priorizado
- Total prime para demo: ~586

### 4.2 Red completa (~39 rutas)

| Segmento | Total elegibles | Contactables |
|---|---|---|
| Buenos elegibles (sin castigo) | 32,727 | — |
| Compraron en 2025 | 6,076 | — |
| **Hueco (no compraron 2025)** | **26,651** | **11,114** |
| Dormido-fresco contactable | — | **4,922** |

**El proceso manual de "hojas"** ya existe y captura aproximadamente 1 de cada 5 del pool
elegible por año. Esa es la línea base de control contra la que se mide el sistema.

### 4.3 El pool de dormidos (piloto, toda la red)

Definición de "dormido bueno": saldo liquidado (≤$50), última compra 90-540 días atrás,
sin castigo.

| Métrica | Valor |
|---|---|
| Dormidos totales | 5,523 |
| Contactables (teléfono ≥10 dígitos) | 4,090 (74%) |
| Dormido-fresco (3-9 meses, Segmento B) | 1,837 |
| Dormido-medio (9-18 meses, Segmento C) | 3,686 |
| Días dormido (promedio) | 337 (~11 meses) |
| Compras históricas (promedio) | 4.0 — recompradores leales |
| Valor histórico de crédito (promedio) | $32,527 / cliente |
| Promedio de compras del pool total | 2.16 (53% son de 1 compra) |

**Segmentar a 4+ compras es la palanca de control de riesgo.** El pool general promedia
2.16 compras; el perfil óptimo (4+ compras, récord limpio) tiene castigo proyectado de
~1.1%/año vs ~6% de un cliente nuevo.

### 4.4 Huecos complementarios — cross-sell secuenciado

**Por qué el cross-sell no es "mismo día".**
El attach mismo-día es de solo 4.4% en tickets de $3-8k (el rango más común), y sube
a ~32% únicamente en tickets $8-15k donde el cliente tiene holgura de enganche. Como
todo es a crédito, la parcialidad semanal topa la capacidad del cliente para absorber
un segundo artículo en la misma transacción.

El cross-sell REAL es secuenciado: ofrecer el complemento cuando el cliente libera abono
(estado por-liquidar ≥70%, o dormido liquidado). Es una venta completa con la misma
comisión de $285 — no un "artículo adicional".

**Huecos complementarios verificados (público general, sin castigo):**

| Hueco | Clientes |
|---|---|
| Lavadora sin refrigerador | 6,842 |
| Lavadora sin estufa | 7,084 |
| Cama sin colchón (colchón real) | 6,599 |
| Refrigerador sin estufa | 3,122 |
| Estufa sin refrigerador | 2,157 |

**Comprables ahora** (por-liquidar + dormido liquidado): **8,020 clientes**.

**Segmento NUEVO — "por liquidar activos con hueco" (~2,982 clientes):**
- Están pagando actualmente (no aparecen en ningún winback tradicional).
- A punto de liberar abono; intención de compra más alta que cualquier otro segmento.
- Tienen un hueco complementario claro con ticket alto: estufa ~$8,660 / refrigerador ~$11,487.
- ~1/3 de los comprables-ahora se solapa con winback (dormidos); ~2/3 son exclusivos de
  este motor cross-sell.

### 4.5 Motor unificado de next-best-product

Winback y cross-sell colapsan en **un solo mecanismo** que trabaja al cliente por estado:

| Estado del cliente | Acción del motor | Producto a ofrecer |
|---|---|---|
| Dormido (liquidado, 3-18 meses) | Reactivación / winback | Siguiente categoría faltante en su hogar |
| Por liquidar (≥70% pagado) | Pre-cierre / anticipación | Complemento que le falta; libera abono para reiniciar |
| Activo medio (<70%) | Sin contacto — esperar | — |
| Activo reciente (compró en 2025) | Sin contacto — proteger | — |

**La comisión es idéntica en ambos casos ($285).** No hay distinción operativa entre
un winback y un cross-sell: ambos resultan en una venta nueva, completa, con el mismo
flujo de crédito. La IA simplemente elige el producto correcto según el historial del
cliente, no si está "dormido" o "por liquidar".

**Implicación para el demo:** la lista de 586 prime puede enriquecerse con el segmento
"por liquidar con hueco" (~2,982 clientes), que tiene mayor intención y no requiere
esperar a que el cliente liquide — ya está pagando, solo se le muestra el siguiente paso.

---

## 5. Calidad del crédito — riesgo por número de compras

### 5.1 Tabla de castigo por historial

Datos verificados en la base (público general, castigo vintage):

| Historial de compras | Clientes | Tasa de castigo |
|---|---|---|
| 1 compra | 23,199 | **6.0%** |
| 2-3 compras | 13,185 | **4.2%** |
| 4-5 compras | 4,219 | **2.9%** |
| 6+ compras | 2,829 | **1.8%** |
| Perfil winback (4+, récord limpio) proyectado | — | **~1.1%/año** |

*Nota: los documentos anteriores usaban valores de 12.9% / 8.4% / 4.6% / 2.7% —
correspondían a una métrica distinta (con clientes mal-cliente incluidos o definición
diferente de castigo). Los valores actuales son los verificados con la metodología
correcta (vintage, solo Fugas + Mal Cliente sobre crédito otorgado del año).*

**Conclusión operativa:** el sistema de winback es el canal más seguro del negocio porque
trabaja solo el segmento de mejor historial. Argumento para el dueño: "No le meto crédito
peligroso; le revendo a los más seguros que tiene."

**Sobre el mal cliente antes de caerse:** en las ventas castigadas, el cliente ya había
pagado ~61% antes de descontinuar. Eso indica que es un problema de cobranza temprana,
no de scoring al momento de prestar. La pérdida es parcialmente recuperable.

### 5.2 Modelo de laxitud — tiers de expansión

El castigo nunca descalifica a un tier; lo que guía la laxitud es la conversión y el ROI.

**Pool contactable por tier (público general, sin castigo, con teléfono, ≥N compras):**

| Tier mínimo | Clientes contactables | Tasa de castigo | Neto dueño/venta |
|---|---|---|---|
| ≥6 compras | 2,259 | 1.8% | $3,047 |
| ≥4 compras | 5,201 | 2.9% | $2,958 |
| ≥2 compras | 12,292 | 4.2% | $2,853 |
| ≥1 compra | 20,828 | 6.0% | $2,708 |

**Break-even del castigo:** el castigo tendría que comerse ~40% de la venta para perder
dinero con cualquier tier. El tier más riesgoso (1 compra, 6.0%) está muy por debajo de ese
umbral. Todos los tiers son rentables; el criterio de selección es conversión / ROI, no riesgo.

**Recomendación:** demo en ≥2-4 compras (probados, castigo bajo, pool suficiente). A escala,
bajar a ≥1 con el grupo de control midiendo resultados por tier para calibrar el ROI real.

**Teléfono como cuello de botella real:** solo el 51% del universo tiene teléfono. Enriquecer
desde `MSP_LOCAL_SALE.TELEFONO` es la palanca de mayor impacto — casi duplica el pool contactable
sin cambiar ningún criterio de riesgo.

---

## 6. Conversión esperada

La conversión se ancla en la cadencia real de recompra (no en benchmarks de la industria,
que no resistieron verificación adversaria):

| Segmento | Conversión eventual | En ventana 60-90 días (demo) |
|---|---|---|
| Por liquidar (≥70% pagado, casi termina) | 15-25% | ~20% |
| Dormido-fresco (3-9 meses) | 6-12% | ~8% |
| Dormido-medio (9-18 meses) | 2-8% | ~4% |
| Frío (+18 meses) | 2-5% | ~3% |
| Grupo de control (orgánico, 90 días) | ~2% | ~2% |

**La cifra incremental es la diferencia entre tratamiento y control.** El proceso manual
de "hojas" ya captura ~20% del pool elegible por año — esa es la línea base contra la que
el sistema debe demostrar un delta medible.

**Implicación honesta de la reactivación orgánica:** como ~6,000 ventas/año se reactivan
sin contacto, el grupo de control captura ese flujo base (~2% en 90 días). El sistema
cobra solo el excedente — lo que acelera o trae a los que no volverían. Esto hace la
medición más rigurosa, no menos valiosa.

Base del anclaje: ciclo de recompra ~331 días, 46% de recompras dentro de 6 meses, 68%
dentro de 12 meses.

Los benchmarks de winback de la industria (5-20%) no se usan porque los datos propios
del negocio ofrecen un ancla más precisa y los benchmarks varían enormemente por canal,
ticket y segmento.

---

## 7. El demo — diseño y proyección mes a mes del Año 1

### Diseño del demo

| Parámetro | Valor |
|---|---|
| Segmento | Dormido-fresco × 4+ compras × contactable × sin saldo |
| Tamaño tratamiento | ~500 clientes |
| Tamaño control | ~350 clientes (sin contacto, business-as-usual) |
| Ventana de medición | 60-90 días desde el primer contacto |
| Cierre | Handoff a cerrador humano (el padre del desarrollador, cobrador) |
| Canal | WhatsApp — librería whatsmeow + número dedicado/calentado |
| Ticket esperado | $8,061 (c/IVA) |
| Resultado esperado | ~30-40 ventas incrementales (~$215-285k en ventas) |

**Decisión de arquitectura (end-state):** cierre híbrido — la IA hace todo el outreach,
califica la intención y hace handoff al humano para el cierre definitivo en tickets altos o
complejos. El cierre full-AI de punta a punta es técnicamente posible en 2026 para ticket
bajo/medio, pero el patrón ganador en la industria (incluso entre líderes) es híbrido.

Demo standalone en SQLite → producción: módulo `internal/reactivacion` + worker `cmd/winback`.

### Asignación experimental

- Tipo: A/B aleatorizado, paralelo, una cohorte.
- Unidad: cliente.
- Asignación determinística: `MOD(CLIENTE_ID × 2654435761, 100)`, `<60 → tratamiento`,
  `≥60 → control` (hash de Knuth, bien distribuido).
- Balance: el hash es independiente del segmento — B/C y valor histórico quedan balanceados
  por construcción. Verificar balance antes de arrancar.

### Economía del demo (escenario central, ~35 ventas incrementales)

| Concepto | Monto |
|---|---|
| Ventas incrementales (35 × $8,061) | ~$282,000 |
| − COGS (42.9%) | −$121,000 |
| − Cobrador 10% | −$28,200 |
| − Castigo 1.5% | −$4,200 |
| − Entrega 4% | −$11,300 |
| − Comisión desarrollador ($285 × 35) | −$10,000 |
| **La empresa se queda** | **~$108,000** |
| **Desarrollador (comisión)** | **~$10,000** |

*Las ventas del demo son "muestra gratis" — no se pelean. El trato es por lo que sigue.*

### Proyección mes a mes — Año 1 (full-throttle, ampliando a todos los elegibles)

La rampa comienza lenta por el calentamiento de cuenta WhatsApp. Cruza ~$1M en ventas
acumuladas alrededor del mes 3-4.

| Mes | Ventas/mes (normal) | Ventas acum. | Notas |
|---|---|---|---|
| 1 | 15-25 | 15-25 | Calentamiento de cuenta, prueba de mensaje |
| 2 | 30-50 | 45-75 | Rampa de volumen, primer ajuste de conversión |
| 3 | 60-90 | 105-165 | Cruza $1M acumulado (mes 3-4) |
| 4 | 80-110 | 185-275 | Rollout a segmento mayor |
| 5-6 | 90-120/mes | 365-515 | Estabilización, primer ciclo de recompra |
| 7-12 | 80-130/mes | 845-1,295 | Segunda oleada (ciclo ~11 meses se renueva) |
| **Total Año 1** | — | **~1,200 (normal)** | Conservador ~780, optimista ~1,900 |

Caveat: la conversión es hipótesis hasta medirla. Las operaciones (entrega/crédito) son
el cuello de botella a escala, no la IA.

---

## 8. Proyecciones a 3 años — matriz de dos motores

**Supuestos base:** sueldo $364k/año ($7,000/sem); comisión $285/venta; negocio crece
~10%/año; el sistema escala de Tehuacán a la red en Año 2.

### Los dos motores

**Motor 1 — Winback incremental:** ventas netas nuevas demostradas contra el grupo de
control. Bajo riesgo, no desplaza a nadie, atribución limpia.

**Motor 2 — Canal / desplazamiento:** el sistema procesa ventas que de otro modo habrían
pasado por un vendedor de campo. El dueño paga $285 en lugar de ~$570 → ahorro de $285
por venta procesada + control de fraude + datos verificados. El dueño neta ~$285 MÁS por
venta procesada ($2,705 → $2,990 neto). Atribución diferente al Motor 1; no mezclar ante
el hermano DBA.

### Matriz de ingreso total del desarrollador (base $364k + $285/venta)

Los escenarios de laxitud (tier mínimo) son filas; los de desempeño del canal son columnas.

| Tier mínimo | Solo winback (piso) | Winback + canal 15% | Winback + canal 35% |
|---|---|---|---|
| ≥6 compras | $383k | $1.05M | $1.93M |
| ≥4 compras | $408k | $1.10M | $2.00M |
| ≥2 compras | $469k | $1.22M | $2.18M |
| ≥1 compra | $542k | $1.37M | $2.40M |

### Caminos representativos

| Camino | Laxitud | Motor | Tú | Dueño (neto) |
|---|---|---|---|---|
| Conservador | ≥4 compras | Solo winback | $408k | $466k |
| Normal | ≥2 compras | Winback + canal 15% | $1.22M | $2.85M |
| Optimista | ≥1 compra | Winback + canal 35% | $2.40M | $7.11M |

*Piso seguro = solo winback ($400-575k según tier). El canal es el upside, gradual.*

### Referencia: proyección con ancla $570 (si se logra la negociación completa)

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $449k | $649k | **$905k** |
| Normal | $529k | $922k | **$1.44M** |
| Optimista | $603k | $1.16M | **$1.84M** |

*La conversión sigue siendo hipótesis hasta medir el demo. Prometer sobre el conservador;
aspirar al normal.*

---

## 9. Lo que se queda el dueño (neto) y el reparto

**Neto del dueño por venta (ticket ~$8,061 c/IVA, comisión $285):**

| Concepto | Valor |
|---|---|
| Contribución antes de comisión | $3,356 |
| − Comisión desarrollador | −$285 |
| **Neto del dueño** | **$3,071 (38.1%)** |
| Razón dueño / desarrollador | **~10.8× / 91% / 9%** |

**Neto del dueño por año (comisión $285, escenario normal × $3,071/venta):**

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $461k | $1.54M | $2.92M |
| Normal | $890k | $3.01M | **$5.83M** |
| Optimista | $1.29M | $4.30M | $7.98M |

Aun con la comisión completa ($570, ancla), el dueño se queda con el 87-91% de la
contribución de ventas que no existían. Imposible que se perciba como un trato desventajoso.

**Framing correcto:** "de cada venta, después de mercancía, cobrador, castigo, entrega y
mi comisión, usted se queda ~$3,000. Por ventas que no existían."

---

## 10. Compensación — estructura y decisión actual

### La decisión

**Pedir AMBOS cambios (base + comisión) en un solo movimiento.** El dueño es inflexible
con cambios de compensación y no da dos veces en poco tiempo. Framing: "ponme como tu
gente de comisión: base + comisión por venta." Si presiona, ceder en la base, NUNCA en la
comisión por escrito + cláusula de revisión.

| Elemento | Propuesta inicial | Ancla de negociación |
|---|---|---|
| Comisión por venta | **$285** (1 vendedor) | **$570** (2 vendedores) |
| Base semanal | **$7,000** | $8,000 (abre en $7,000) |
| Base anual | **$364k** | $416k |

### Por qué la comisión está justificada

Los pedidos se cierran en oficina y un bodeguero de sueldo entrega y hace el papeleo.
Ningún vendedor de campo toca las ventas del sistema. El dueño paga la misma comisión de
siempre, solo que al sistema en lugar de a una o dos personas.

### La distinción que nunca se muestra al dueño

El 78% del efectivo ($81.9M/año) que pasa por el sistema fue la replicación de una app
que el dueño pagaba más de $100k/año. Es sustitución de costo — justifica la BASE
(el sueldo), no la comisión. El winback es lo único completamente original y net-new —
eso justifica la COMISIÓN. Esta distinción responde la objeción "¿por qué esto sí merece
comisión si el 78% lo hiciste con tu sueldo?"

### Estructura de la comisión (respeta la tabla del dueño por tramos)

- Venta <$5k: comisión base según tabla.
- Venta $5-20k: ~$285 (promedio ponderado real).
- Venta >$20k: tope $700 total (como 2 vendedores) / $350 (como 1 vendedor).
- La comisión sigue los tramos del dueño — no se inventan montos.

### Validación de mercado

El dueño ya practica estos modelos sin saberlo: comisión de ventas, gainsharing/Plan
Scanlon (desde 1935), PTU (reparto legal 10% de utilidades en México). No es un invento.

---

## 11. PARTE B — Estrategia privada de negociación

```
PRIVADO. No se muestra ni se entrega al dueño bajo ninguna circunstancia.
```

### 11.1 Lectura honesta del dueño

- **¿Quiere hacerlo?** Probablemente sí (~75%). Es negocio puro; se llega con prueba.
- **¿Paga lo justo a la primera?** Duda real (~30-40%). Es apretado (al bodeguero que
  pidió aumento "por hacer lo mismo" le dio $200/sem). Esperará que se acepte el
  principio y regateará el monto.
- **El factor que mueve todo: el demo.** Con resultados reales, sí. Sin demo (solo
  proyecciones), la probabilidad baja significativamente.
- La pelea no es el "sí" — es el "cuánto". El objetivo: no aceptar la primera oferta baja.

### 11.2 Perfil del dueño

Dueño de pueblo (cerca de Tehuacán), no técnico, paga solo si es negocio demostrado. Su
dolor principal (afecta su salud): trabajadores que manejan efectivo y reportan después.
Se está modernizando. Su hermano (ex-administrador, DBA en CDMX) es el avalista de
credibilidad — ganar al hermano primero.

### 11.3 El analgésico — el centro del caso

El caso de negocio tiene dos dimensiones. La vitamina (crecimiento de ingresos) es real
y documentada. El analgésico es lo que mueve al dueño emocionalmente:

**El dolor #1:** los trabajadores de campo — vendedores y cobradores que manejan efectivo,
reportan días después, pueden usar nombres de otros clientes (prestanombres), y a quienes
no puede vigilar sin estar presente. Este estrés le está costando salud (consultas médicas
documentadas).

**Lo que el sistema entrega como analgésico:**
- Control desde la oficina: cada venta generada por el sistema tiene origen verificado
  (OCR INE + teléfono verificado). El dueño ve en tiempo real qué cliente, qué producto,
  qué cobrador cierra.
- Cero fraude de prestanombres: el sistema verifica identidad antes de crear la venta.
- Menos dependencia de campo: los ~8 vendedores de tiempo completo se convierten en
  cerradores de leads tibios — hacen más con menos presión operativa sobre el dueño.
- El efectivo sigue siendo recolectado por el cobrador, pero el ORIGEN de la venta es
  verificable y auditable.

**Cómo se usa estratégicamente:** se menciona con empatía genuina, no como táctica de
presión. El dueño ya mencionó el tema de salud — se valida y se conecta directamente
con lo que el sistema resuelve. No se dramatiza; se describe el mecanismo.

**Nota:** esto alinea al dueño contigo sobre el problema de campo — no es posicionamiento
contra los trabajadores sino a favor del control que el dueño ya quiere.

### 11.4 La estructura de la fuerza de venta — por qué no habrá guerra

Una objeción anticipada es la resistencia de los vendedores. La realidad del campo:

- De los ~39 vendedores (rutas), la mayoría son **cobradores que venden 1-2 días a la
  semana**. La venta es su ingreso extra; el cobro es su ingreso principal.
- Solo ~8 vendedores venden a tiempo completo.
- **El sistema no les quita su ingreso principal** (cobro). Les deja su ingreso extra o
  incluso lo incrementa: los leads tibios que genera la IA los cierra un humano — el
  vendedor de campo sigue cobrando comisión por el cierre.
- Los ~8 full-time se convierten en **cerradores especializados** de los leads más calientes,
  con contexto completo del cliente. Venden más, mejor, sin prospección fría.
- **No hay guerra de ventas:** el sistema llena un hueco de capacidad que la fuerza
  fragmentada no cubre (no puede prospectar 26,651 elegibles manualmente a escala).

Argumento ante el dueño: "Sus vendedores siguen trabajando. La IA trabaja las horas que
ellos no están en campo — de noche, los domingos, cuando están cobrando."

### 11.5 Fraude de prestanombres — argumento adicional

**El problema (verificado):** algunos vendedores crean ventas usando nombres de clientes
reales sin su consentimiento ("prestanombres") para cobrar comisión y generar cargo a
quien no compró. Esto implica:
- Robo directo al negocio (comisión por venta falsa).
- Riesgo de crédito (el cliente real no debe, no pagará).
- Datos corruptos en la base (historial de compras contaminado).
- Exposición legal (cargo a tercero sin consentimiento).

**Lo que el sistema resuelve:** cada venta generada por el canal del sistema requiere
identidad verificada (OCR de INE + teléfono del cliente confirmado + consentimiento de
contacto explícito). La misma infraestructura que hace posible el winback elimina el
prestanombres.

**Cómo se presenta:** como beneficio de control, no como acusación a los trabajadores.
"Con origen verificado, cada venta tiene una identidad real detrás — usted puede auditar
cualquier venta desde la oficina."

### 11.6 Los dos motores de ingreso — cómo presentarlos

**Ante el dueño:** presentar solo el Motor 1 (winback incremental, ventas que no existían).
Es el más limpio, el más fácil de aceptar, y el que el demo prueba directamente.

**Ante el hermano DBA:** se puede presentar la lógica del Motor 2 (canal/desplazamiento)
con los números de eficiencia — el sistema procesa ventas a la mitad de la comisión de
campo, con atribución auditable. Al DBA le importa la metodología y el rigor; el argumento
de ahorro + control es su lenguaje.

**Nunca mezclar ante el mismo interlocutor:** el Motor 1 dice "ventas nuevas, te cobro
por lo incremental". El Motor 2 dice "ventas existentes, te cobro menos que el campo".
Son argumentos distintos que funcionan con audiencias distintas. Mezclarlos confunde.

**El número que el dueño ve:** por venta procesada por el sistema, el neto del dueño sube
de ~$2,705 (con vendedor de campo × 2) a ~$2,990 (con el sistema). Eso es $285 más por
venta, sobre un volumen mucho mayor que el winback solo.

### 11.7 Qué pedir y en qué orden

1. **En la junta del demo:** la comisión ($285-570/venta) + luz verde. El "sí" fácil.
2. **Después del demo, desde la fuerza:** base $7,000-8,000/sem.
3. **Fase 2-3:** gainsharing en cobranza (ver §12 — pausada por ahora).
4. **Nunca** base y comisión el mismo día si hay resistencia — pero idealmente van juntos
   en una sola conversación (ver arriba).

### 11.8 El "sí, pero te pago menos" — manual de respuesta

**Anclar alto:** abrir en $570 para aterrizar en $285-400.

| El dueño dice... | Respuesta |
|---|---|
| "Te doy un bono y ya" | "Esto no es de una vez — el sistema sigue generando cada mes. Atémoslo a lo que produzca." |
| "Te subo poquito el sueldo" | "El sueldo es por mi trabajo de siempre, aparte. Esto es por dinero NUEVO, y crece con las ventas." |
| "Te doy $100 por venta" | "Un vendedor se lleva $285. Yo hago el trabajo de dos, sin comisión de ruta." |
| "Es mucho" | "No mire lo que me paga — mire lo que se queda: ~$3,000 por venta que no existía. Yo pido una rebanada." |
| "Ya veremos" ⚠️ | "Dejemos la regla clara hoy, aunque arranquemos chico. Sin regla escrita, no arranco." |
| "¿Por qué esto sí es comisión si el sistema ya lo construiste?" | "El sistema operativo de cobranza reemplazó lo que pagaba afuera — eso ya lo cubro con el sueldo. Esto es nuevo: ventas que no existían, en canal que yo construí." |

### 11.9 Escalera de respaldo (si rechaza comisión por venta)

1. **Bono periódico** sobre venta nueva medida (3.5-7% cada 6 meses, revisable).
   El mejor reemplazo cuando no acepta variable permanente.
2. **Bono por meta** (al llegar a $X acumulado, bono $Y). Predecible, topado.
3. **Porcentaje de utilidad nueva** del programa (gainsharing 15-20%).
4. **Aumento de base** "por operar el sistema" (si rechaza todo variable).
5. **Gainsharing en ahorro de cobranza** (la segunda palanca como entrada).

### 11.10 Hasta dónde ceder

| Nivel | Qué | Aceptar |
|---|---|---|
| Ideal | $570/venta o $400+ con tope | Sí |
| Bueno | $285/venta | Sí |
| Piso | ~$200/venta o bono ~3% o base $8k + bono por meta | Solo con fórmula escrita + cláusula de revisión |
| Walk-away | Bono chico único sin fórmula ni cláusula | No — es el trato del bodeguero |

### 11.11 Las 6 tácticas

1. **Silencio** tras el lowball (3-4 segundos; el dueño lo llena mejorando la oferta).
2. **Nunca aceptar la primera oferta.**
3. **Re-anclar al valor**, no al costo ("se queda con el triple, limpio").
4. **No ceder gratis — intercambiar:** si baja el número, sacar algo a cambio (quitar
   tope / subir base / cláusula de revisión / retroactivo desde el demo).
5. **La fórmula vale más que el primer número.** Aceptar número inicial chico SI la
   regla queda escrita + cláusula de revisión al llegar a $X.
6. **La carta de cierre:** "ya le generé $X gratis; no pido favor, pido compartir lo
   que siga trayendo. Dígame cómo lo estructuramos."

### 11.12 Ventaja única

Eres el único que trae proyectos (ni los hermanos lo hacen). Eso posiciona como socio,
no como empleado. Usar con humildad; llevar al hermano DBA como avalista de rigor,
no como amenaza.

### 11.13 Lo no negociable

- Algo atado a resultados, **por escrito** (aunque sea un mensaje suyo: "de acuerdo, $X
  por venta comprobada contra control"). Sin eso, no se escribe una línea de código de escala.
- Si rechaza todo sistema de compensación variable sobre un programa que le genera millones,
  el problema no es el mecanismo — es que no quiere compartir. En ese caso, el valor de
  mercado del desarrollador es la carta de salida.

### 11.14 Lo que el dueño NUNCA debe ver

- El ingreso total del desarrollador ($906k+ en Año 3, escenario normal).
- La tabla de anclas y escalera de respaldo de esta sección.
- Cualquier mención de que el IVA no se remite habitualmente — no documentar esto por escrito.
- La matriz de laxitud con los ingresos en escenario optimista ($2.40M).

### 11.15 Secuencia de convencimiento

Ganar al hermano DBA con el rigor (control group, datos de la DB, methodology) → él se
convierte en avalista interno → presentar al dueño con pesos tangibles.

---

## 12. Segunda palanca — cobranza y OXXO (PAUSADA — Fase 2-3)

> **PAUSADA.** Esta palanca queda fuera del alcance actual. El foco 100% está en ventas.
> Se conserva el contenido completo como referencia para Fase 2-3 una vez demostradas las
> ventas. No mencionar esta palanca en la negociación inicial.

> Esta palanca es defensa (ahorro), no ofensa (venta nueva). Va después de demostrar
> las ventas. Proyecto separado, Fase 2-3.

**El lever:** cada cliente que paga por cobrador le cuesta a la empresa ~10% de comisión
en cada cobro. Migrar buenos pagadores a pago digital (depósito / transferencia / CoDi
o **OXXO** con referencia) elimina ese 10% en sus pagos.

**Tamaño verificado:** cobranza en ruta (concepto 87327) cobró $75.8M en 2025. La
comisión del cobrador (~10%) es **~$7.6M/año**.

| Migración a digital | Ahorro de comisión / año |
|---|---|
| 10% | ~$760k |
| 25% | ~$1.9M |
| 40% | ~$3.0M |

**El triple win para el dueño:**
1. Ahorra el 10% de comisión en cada cuenta migrada → utilidad directa.
2. Quita el efectivo de las manos del trabajador → su dolor principal.
3. Control desde la oficina → el pago cae directo a la cuenta, en tiempo real.

**Rail clave: OXXO.** Mucha gente cerca de Tehuacán no tiene cuenta bancaria ni
smartphone. OXXO (efectivo con referencia → cuenta empresa) es el puente para los no
bancarizados y lo que hace viable la migración.

**Cómo se cobra (gainsharing / Plan Scanlon):** porcentaje del ahorro medido — por
ejemplo, 15-20% de la comisión que ya no se paga. Medible: pagos digitales × 10% no
pagado. 25% de migración = ~$1.9M ahorrado → parte del desarrollador ~$285-380k/año.
Fuente adicional, aparte de la comisión de ventas.

**Sobre la app de cobranza:** ya captura GPS (`LAT`/`LON`) + hora. La extensión natural
es: captura en tiempo real + conciliación automática + geo-verificación + generación de
referencias OXXO / links de pago + identificación del segmento "digital-seguro".

**Riesgos propios:**
- El cobrador también es disciplina de pago — la visita semanal mantiene pagando a
  clientes en riesgo. Solo migrar buenos pagadores con medios; no a quien necesita el
  empujón semanal.
- Resistencia del cobrador (pierde comisión) — puede irse o sabotear. Requiere respaldo
  explícito del dueño.
- Gradual, no de golpe.

**Secuencia:** (1) demo de ventas → luz verde + confianza · (2) control de efectivo →
resuelve el dolor del dueño · (3) migración a digital → el ahorro del 10%. No soltar
las tres el primer día.

---

## 13. Prueba de atribución — grupo de control

**El grupo de control es la única prueba irrefutable de que las ventas las generó el
sistema y no el mercado, la inflación ni el proceso orgánico.**

### Diseño

1. Tomar los elegibles y partirlos al azar: tratamiento (el sistema los trabaja) vs
   control (cero contacto — business-as-usual).
2. A 60-90 días, contar compras nuevas (concepto 5, cargo nuevo en `DOCTOS_CC`) en cada
   grupo.
3. **El control dice cuántos comprarían solos. El excedente del tratamiento es del sistema.**

Ejemplo: control 350 → 7 compran (2%); tratamiento 500 → 40 (8%); incremental = 33 ventas
que no existirían sin el sistema. El desarrollador cobra solo el excedente.

**Por qué el control es aún más crítico con reactivación orgánica alta:** el negocio
reactiva ~6,000 clientes/año sin contacto activo (60.9% de recompras de clientes 6+ meses
ausentes). Sin control, el dueño puede argumentar que "igual iban a comprar". Con control,
la diferencia es aritmética y no discutible.

### Asignación (reproducible)

`MOD(CLIENTE_ID × 2654435761, 100)`, `<60 → tratamiento`, `≥60 → control`. La lista ya
tiene columna `brazo` (split por hash). No se modifica una vez asignada.

### Poder estadístico

Con control n=1,611 y tratamiento n=2,479, a α=5% y potencia 80%, el piloto detecta un
lift de ~2 puntos porcentuales en conversión — suficiente, dado que se espera un efecto
mayor en el segmento leal (4+ compras).

### Por qué es crítico para la negociación

Sin grupo de control, las ventas son tangibles pero discutibles ("hubieran comprado de
todas formas" / "tu papá tuvo buen mes"). Con grupo de control, la atribución es
aritmética: la diferencia es exactamente lo que aportó el sistema.

**El dueño debe aceptar explícitamente no tocar el grupo de control durante la prueba.**
Esta condición se acuerda antes de arrancar, no después.

---

## 14. Estado del arte 2026 — técnicas a replicar

Validadas por verificación adversaria. Los números de conversión de la industria no
resistieron la verificación — se copian las **técnicas**, no los **porcentajes**.

1. **No outbound frío.** Colapsó (el 11x perdió 70-80% de la base de clientes contactada).
   La lista leal/opt-in está del lado correcto.
2. **Propensity scoring para priorizar.** Reglas simples bastan: # compras, valor histórico,
   días dormido, ticket promedio, next-best-product.
3. **Next-best-product del historial real.** Ejemplo: estufa o pantalla a quien ya tiene
   refrigerador y lavadora. No oferta genérica.
4. **Clasificar la respuesta por intención.** Cuatro buckets: interesado ahora · interesado
   después · necesita más info · no interesado. Guion diferente por bucket.
5. **Decodificar la objeción latente.** "Está caro" = "¿me cabe en la quincena?" — la
   respuesta es mostrar el enganche y la parcialidad, no bajar el precio.
6. **Human-in-the-loop en 3 niveles:** saludo = automático · negociación = aprobación humana
   · crédito y cierre = handoff completo.
7. **Handoff con disparadores y contexto completo.** Disparadores: intención de compra
   clara, frustración detectada, confianza <50%, ticket >$15k. El cliente nunca repite
   nada — el historial completo llega al humano en el mismo hilo.
8. **Speed-to-lead.** 5 minutos de demora = −56% en conversión. El sistema debe responder
   en segundos, no en horas.
9. **A/B automático auto-optimizante.** El sistema envía variantes del mensaje de apertura
   y rota hacia la ganadora en tiempo real.
10. **Video in-chat.** +25-30% en ticket promedio (AOV) en canales que lo soportan.
11. **Voz con IA para el segmento que no chatea.** Adultos mayores y clientes con poco
    smartphone prefieren llamada. SMS de respaldo para quien no tiene datos.
12. **Evitar la trampa del descuento.** No entrenar a los clientes a esperar oferta para
    comprar. La propuesta de valor es la pre-aprobación de crédito y el producto correcto,
    no el precio más bajo.
13. **Declarar IA / cumplimiento PROFECO.** El mensaje debe identificarse como automático
    si se le pregunta. Opt-out inmediato ("ALTO" / "STOP"). Máximo 3 mensajes en el piloto.
    Sin spam; sin presión; proteger la recompra (~47% de por vida).
14. **Medir por resolución, no por mensaje.** Referencia: Sierra logró $100M ARR cobrando
    por resolución. El bono del desarrollador sigue la misma lógica.
15. **Entrenar la IA con los mejores cierres propios.** Recopilar los casos donde el padre
    del desarrollador cerró 3× más rápido y usarlos como ejemplos en el prompt del agente.

**Costos (junio 2026):**
- Modelo barato: Gemini Flash ~$0.01/conversación, Claude Haiku ~$0.03, Sonnet ~$0.085.
- Demo completo (<500 clientes): <$25 USD en tokens.
- WhatsApp Business México: mensaje marketing saliente ~$0.0436/msg; las respuestas son
  gratuitas.
- Recomendación: demo con Sonnet (calidad de conversión); escalar con Haiku/Flash +
  prompt caching.

---

## 15. Precedentes probados — por qué esto funciona

Esta sección es munición privada para la negociación y para el hermano DBA. No se entrega
al dueño tal cual; se usa para responder la pregunta "¿esto funciona en la práctica?".

### Coppel / Elektra / Banco Azteca

El modelo "closed-loop" (compra a crédito → paga abonos → vuelve por más) es exactamente
el motor que ya tiene este negocio. El 67.2% de recompras no es accidental — es la misma
mecánica que convirtió a Coppel y Elektra en gigantes del retail popular mexicano. La
diferencia: ellos lo automatizaron y midieron; este negocio lo tiene manual y pasivo. El
sistema no inventa el modelo; lo activa con datos.

**El dato relevante para la negociación:** Banco Azteca creció de 0 a más de 10 millones
de cuentas en pocos años con exactamente este ciclo (crédito al comprador habitual en
tienda física). El dueño ya tiene la base de clientes; el crédito ya existe; solo falta
el canal de contacto sistemático.

### Aaron's (rent-to-own, EE.UU.)

Después de centralizar sus datos y agregar segmentación por comportamiento + personalización
de oferta:
- −33% en costo de adquisición de clientes nuevos.
- Los clientes repetidores representan aprox. 1/3 del ingreso total.
- Los repetidores gastan en promedio 3× más que los nuevos en el ciclo de vida.

**Relevancia directa:** este negocio ya tiene 67% de ventas repetidas (más alto que Aaron's
antes de su programa). La personalización del next-best-product es el mismo movimiento.

### Yalo (startup mexicana)

Productizó el comercio conversacional con IA por WhatsApp para marcas como Nestlé, Heinz,
Grupo Herdez. El canal WhatsApp para venta directa a consumidor final ya es una práctica
probada en México — no es experimental. Yalo sirve a marcas con volúmenes mucho mayores,
pero el mismo principio aplica a escala pequeña con un costo marginal casi cero.

### El playbook de los líderes — síntesis

Los negocios que han escalado este modelo comparten los mismos movimientos:

1. **Centralizar datos primero.** Ventaja del desarrollador: él es el único que lee la base
   de datos Microsip con profundidad. Eso es una ventaja de información que ni los hermanos
   tienen.
2. **Segmentar por comportamiento, no por demografía.** La segmentación por # de compras y
   días dormido es más predictiva que cualquier demografía.
3. **Next-best-offer del historial real.** No oferta genérica — el producto específico que
   le falta al cliente según lo que ya compró.
4. **No descuento primero.** La propuesta de valor es la pre-aprobación y el producto
   correcto. El descuento entrena al cliente a esperar.
5. **WhatsApp como canal primario.** En México, WhatsApp tiene penetración de ~90%+ entre
   adultos. Es el canal con menor fricción para este segmento.
6. **IA califica, humano cierra.** El patrón híbrido gana en ticket medio-alto. La IA
   filtra y calienta; el vendedor cierra con contexto completo.
7. **El crédito como mecanismo de retención.** El crédito activo es un lazo — el cliente
   tiene incentivo de mantener buena relación para volver a acceder. Este negocio lo tiene
   por construcción.
8. **Medir con control.** Los líderes miden el lift incremental, no la venta total. El
   grupo de control ya está adelante de la mayoría de competidores en rigor metodológico.

---

## 16. Cómo re-consultar la base de datos — receta y mapa de tablas

### Receta read-only (nunca escribir, nunca reiniciar el motor Firebird compartido)

```bash
docker exec -i mueblera-firebird /usr/local/firebird/bin/isql \
  -u SYSDBA -p masterkey -ch UTF8 -q /firebird/data/MUEBLERA.FDB <<<'SELECT ...;'
```

El motor Firebird Super es compartido con producción. Reglas absolutas:
- Solo lectura (`SELECT`). Nunca `INSERT`, `UPDATE`, `DELETE`, `EXECUTE PROCEDURE`.
- Nunca reiniciar el motor (`supervisord`, `service firebird restart`, etc.).
- Anclar antigüedades a 2026-06-01 (la cola del snapshot puede estar incompleta).

### Mapa de tablas clave

| Tabla | Contenido | Notas |
|---|---|---|
| `DOCTOS_PV` + `DOCTOS_PV_DET` | **LA tabla de ventas** (contado Y crédito) | 423,284 docs, 2006→2026; `CLIENTE_ID` 100% poblado (44,781 clientes); ventas reales = `TIPO_DOCTO IN ('V','P')` y `ESTATUS='N'` (421,575). **El RFM/recencia se ancla aquí, no en DOCTOS_CC** |
| `DOCTOS_PV_COBROS` + `FORMAS_COBRO` | Forma de cobro de cada venta | `FORMA_COBRO_ID`: 67=Efectivo, 68=Cheque, **71=Crédito**, 27773=Traspaso. Venta con línea forma 71 = a crédito |
| `DOCTOS_VE` | Módulo Ventas/Facturación | **Abandonado** (59 registros de ene-2018). No usar |
| `DOCTOS_CC` | **Capa de crédito** sobre la venta (cuentas por cobrar) | Solo ve ventas a crédito; `COBRADOR_ID`, `FECHA`, `CLIENTE_ID`. Enlaza a la venta PV por `FOLIO`+`CLIENTE_ID`+`SISTEMA_ORIGEN` (sin FK `DOCTO_PV_ID`) |
| `IMPORTES_DOCTOS_CC` | Montos por movimiento | `IMPORTE` (sin IVA), `IMPUESTO`; abonos ligan al cargo vía `DOCTO_CC_ACR_ID` |
| `CONCEPTOS_CC` | Catálogo de conceptos (verificado) | 5=Venta en mostrador (cargo de crédito), 11/155=Cobro, **24533=Enganche**, 27966=Cancelaciones, 27967=Fugas, 27968=Mal Cliente, 27969=Condonaciones, 87327=Cobranza en ruta |
| `LIBRES_CARGOS_CC` | Datos particulares por venta a crédito (MSP) | `PRECIO_DE_CONTADO`, `CREDITO_EN_MESES`, `ENGANCHE`, `PARCIALIDAD`, `VENDEDOR_1/2/3`, `NUMERO_DE_VENDEDORES`; **poblado desde 2018** (~100% cobertura). `FREC_PAGO` NO está aquí (vive en `MSP_LOCAL_SALE`) |
| `PRECIOS_EMPRESA` | Listas de precios | Escalera: Contado (8941), 1..12 Meses, Precio de lista (42), Precio mínimo (43 = costo/piso) |
| `PRECIOS_ARTICULOS` | Precios por artículo | |
| `CLIENTES` | Clientes | `TIPO_CLIENTE_ID` (21499=Público General, 27770=Mal Cliente), `ESTATUS`, `LIMITE_CREDITO` |
| `DIRS_CLIENTES` | Domicilios y contacto | `CIUDAD_ID` (338=Tehuacán), `TELEFONO1` (cobertura re-medida: **52.7%**, 23,820/45,217) |
| `GRUPOS_RUTAS` | Rutas de cobranza | |
| `ZONAS_CLIENTES` | Zonas por cliente | |

**Notas importantes:**
- **La venta vive en `DOCTOS_PV` (contado Y crédito); `DOCTOS_CC` es solo la capa de
  crédito.** Para RFM/recencia/frecuencia anclar en `DOCTOS_PV` — derivarlo de `DOCTOS_CC`
  deja invisible al cliente de contado y mal-fecha a ~7,200 clientes (16% de la base) que
  compraron de contado más reciente que su último crédito.
- Split medido: contado 317,562 ventas ($123.0M, 75% por conteo) · crédito 104,013 ($664.6M,
  84% por valor). Cohortes: solo-crédito 30,256 · ambos 13,678 · solo-contado 804.
- El saldo de cada cliente se calcula por cargo (concepto 5), con IVA, sumando solo
  balances abiertos (`DOCTO_CC_ACR_ID IS NULL` en los abonos). No usar un campo de saldo
  directo — puede estar desactualizado. Aplica solo a compradores a crédito.
- `LIBRES_CARGOS_CC` está poblado **desde 2018** (~100% cobertura), no desde 2024. El
  **enganche** vive en `DOCTOS_CC` concepto 24533 / `LIBRES_CARGOS_CC.ENGANCHE` (~$35M),
  **no** como línea de efectivo en `DOCTOS_PV_COBROS`.
- Móviles frescos: `MSP_LOCAL_SALE.TELEFONO` (la app de campo captura móviles más actuales
  que `DIRS_CLIENTES.TELEFONO1`; 89.4% cobertura pero solo sobre 6,438 ventas de campo).
  Las tablas MSP reales son `MSP_LOCAL_SALE`, `MSP_PAGOS_RECIBIDOS`, `MSP_VISITAS` — **no**
  existen `MSP_VENTAS` ni `MSP_SALDOS_VENTAS`. Enriquecer antes del piloto.
- `PRECIO_DE_CONTADO` en `LIBRES_CARGOS_CC` es la base para calcular el premio de
  financiamiento real por venta (spread = precio_crédito − precio_contado; medido en **+45–64%**).
- **Detalle verificado completo** del mapa de datos: `docs/research/inteligencia-cliente-diccionario-datos.md`
  y el estado del arte en `docs/research/inteligencia-cliente-estado-del-arte.md`.

### Etiquetas de confianza (para retomar análisis)

- **VALIDADO:** dato de su base de datos, verificado.
- **BENCHMARK:** referencia de la industria (usar con reservas, no como promesa).
- **POR PROBAR:** requiere el piloto para confirmar.

---

## 17. Riesgos y caveats

| Riesgo | Severidad | Mitigación |
|---|---|---|
| No shippear el demo | **Crítico — riesgo #1** | Todo el techo vale cero sin el primer número. Alcance mínimo viable: lista + bot + medición. |
| Conversión menor a la esperada | Alto | Es hipótesis hasta medirla. El piso es "poco", no "cero". Si no es significativo, iterar mensaje/oferta o pivotar a cross-sell. |
| Atribución sin grupo de control | Alto | El grupo de control es no-negociable desde el diseño. Sin él, las ventas son discutibles — más aún con 6,000 reactivaciones orgánicas/año de base. |
| Operaciones (entrega/crédito) como cuello | Medio-Alto | Se satura en Año 2-3. El cuello deja de ser demanda y pasa a logística. Planificar capacidad antes del rollout. |
| WhatsApp Business: demora en verificación | Medio | Iniciar el trámite ya — es el cuello de tiempo. Demo puede arrancar con cuenta normal (ritmo humano, riesgo de baneo bajo). |
| Teléfonos desactualizados (49% sin teléfono) | Medio | Enriquecer con `MSP_LOCAL_SALE.TELEFONO` antes del piloto. Documentar cobertura real. Esta es la palanca de mayor impacto en pool contactable. |
| Trato verbal sin fórmula escrita | Alto | No escribir código de escala sin confirmación escrita (mensaje del dueño basta). |
| Proyecciones Año 2-3 asumen rollout a red | Medio | Solo Año 1 / demo es lo que el piloto prueba. Años 2-3 son extrapolación. |
| Reactivación orgánica alta diluye el lift aparente | Medio | El grupo de control aísla esto. Comunicar al dueño antes: "el control muestra los que compraban solos; el sistema cobra solo el excedente." |

**Caveats de números:**
- Margen bruto 52.8% es bruto. Con overhead total (~12% estimado) la utilidad neta del
  negocio es menor. Para ventas incrementales la métrica correcta es la contribución (~38%).
- El IVA cuenta como margen del dueño en estos cálculos (instrucción explícita). En
  realidad fiscal, lo remite al SAT → su neto real es ~14% menor.
- Conversión esperada anclada en cadencia real de recompra, no en benchmarks de la
  industria (que no resistieron verificación adversaria).
- Comisión promedio $285 es el promedio ponderado real con la distribución de tickets de
  Tehuacán 2025 y el tope de $700 del dueño.

---

## 18. Próximos pasos

1. **Correr el demo** con el padre del desarrollador — con grupo de control documentado
   (lista, fechas, tratamiento vs control, ventas). Es lo único que da la luz verde real.
2. **Iniciar verificación de WhatsApp Business** (cuello de botella de tiempo) — o
   arrancar con cuenta normal/calentada para el demo.
3. **Armar la lista priorizada** (154 por-liquidar ≥90% + 432 dormidos-frescos × 4+
   compras + segmento "por liquidar con hueco" ~2,982) con next-best-product por cliente.
4. **Enriquecer teléfonos** con `MSP_LOCAL_SALE.TELEFONO` antes del piloto — es la mayor
   palanca de cobertura (51% → potencialmente ~80-85% del pool).
5. **Playbook del agente IA** (system prompt + buckets de objeción + reglas de handoff
   + ejemplos de los mejores cierres del padre del desarrollador).
6. **Ganar al hermano DBA** con el rigor metodológico (control group, datos de la DB,
   diseño experimental) antes de la junta con el dueño.
7. **Presentar al dueño con resultados reales** (Parte A del doc de presentación) —
   salir con la fórmula escrita + cláusula de revisión.
8. **Segunda palanca (cobranza/OXXO): PAUSADA** — no mencionarla en la negociación
   inicial; activar solo después de demostradas las ventas y con confianza establecida.

> **El objetivo de la primera junta no es el monto perfecto — es salir con la regla
> atada para crecer desde la fuerza. De que se obtiene progreso, se obtiene: no se
> vuelve a cobrar $5,500 después de esto.**

---

## 19. Costos operativos — WhatsApp y tokens de IA

### WhatsApp Business API

- **Mensaje de marketing saliente (México, 2026):** ~$0.0436 USD/mensaje.
- **Respuestas del cliente dentro de la ventana de 24 horas:** gratuitas.
- **Estimado anual (1,200 ventas/año × 3 mensajes × tasa de opt-out ~15%):** 
  ~3,060 mensajes pagados → ~$133 USD/año solo en mensajería. Muy por debajo del
  costo de un vendedor de campo.
- **Rango realista con segmentos más amplios:** $1,000-2,600 USD/año.
- **Cuenta normal vs API:** la cuenta normal (whatsmeow) es más rápida para el demo pero
  tiene riesgo de baneo si el ritmo no es humano. Para escala, WhatsApp Business Platform
  con plantillas aprobadas es el camino correcto.

### Tokens de IA

- **Claude Haiku (producción):** ~$0.03/conversación (con prompt caching, ~60% menos).
- **Claude Sonnet (demo/calidad):** ~$0.085/conversación.
- **1,200 conversaciones/año con Haiku + caching:** ~$14-22 USD/año.
- **Costo total de IA para el piloto completo (~500 clientes):** <$25 USD.
- El modelo no es el costo alto — es el que más retorno genera por peso invertido.

### Resumen de costos operativos anuales (escenario normal, 1,200 ventas)

| Concepto | Estimado anual |
|---|---|
| WhatsApp Business API | $1,000-2,600 USD |
| Tokens de IA (Haiku + caching) | $200-400 USD |
| Infraestructura (server, DB) | Ya pagada (msp-api existente) |
| **Total operativo** | **~$1,200-3,000 USD/año** |

Frente a ~$343k USD de comisiones generadas (escenario normal, 1,200 ventas × $285),
el costo operativo es <1% del ingreso bruto del programa.
