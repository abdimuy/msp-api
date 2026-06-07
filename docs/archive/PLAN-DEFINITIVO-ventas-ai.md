# PLAN DEFINITIVO — Automatización de ventas con IA

> Documento maestro y definitivo. Consolida TODO: el caso, los números validados, los
> escenarios, las proyecciones, lo que se le presenta al dueño, y la estrategia privada de
> negociación. **La Parte A es para el dueño; la Parte B (🔒) es solo para ti.**
>
> Todos los números **VALIDADO** salen de la copia Microsip (read-only). Corte de datos
> efectivo: **~feb 2026** (anclar antigüedades a 2026-02-28). Complementa
> [`automatizacion-ventas-ai.md`](./automatizacion-ventas-ai.md),
> [`plan-presentacion-dueno.md`](./plan-presentacion-dueno.md),
> [`pilot-winback-dormidos.md`](./pilot-winback-dormidos.md).

## Índice
0. Resumen ejecutivo
1. El negocio (datos validados)
2. Economía unitaria (márgenes, costos, comisión)
3. La oportunidad (Tehuacán + red)
4. Calidad del crédito (riesgo por historial)
5. Conversión esperada
6. El demo
7. Proyecciones a 3 años (3 escenarios × 2 comisiones)
8. Lo que se queda el dueño (neto)
9. Compensación (estructura + comisión)
10. Segunda palanca (cobranza/OXXO)
11. Prueba de atribución (control group)
12. **PARTE A — Lo que le digo al dueño**
13. **PARTE B — 🔒 Estrategia privada (negociación)**
14. Riesgos y caveats
15. Próximos pasos

---

## 0. Resumen ejecutivo

- **El 78% del efectivo del negocio ($81.9M/2025)** entra por el sistema operativo que el dev maneja. El negocio **vende solo a clientes que ya compraron** → reactivar/recomprar ES el negocio.
- **Oportunidad medida (Tehuacán):** 13,014 clientes buenos; 5,617 contactables que cumplen estándar; 586 "prime-listos" para el demo.
- **Las ventas del sistema son las más seguras:** cliente leal cae 2.7% vs 12.9% de uno nuevo.
- **Margen bruto 54.4%; contribución ~38%; neto al dueño ~$2,100-2,400 por venta** (después de todo, IVA dentro).
- **Demo esperado:** ~$300-350k (un cerrador) a ~$1.4M (multi-cerrador).
- **Comisión:** como ningún vendedor cierra (bodeguero de sueldo entrega), el sistema reemplaza a los 2 vendedores → comisión de **$285 (1 vend.)** a **$570 (2 vend.)** por venta.
- **Tu ingreso en Año 3:** de **$687k (conservador, $285)** a **$1.90M (optimista, $570)**. Hoy: $286k.
- **El dueño en Año 3 (muy probable):** **~$4M netos** de ventas que no existían.

---

## 1. El negocio (datos validados, 2025)

| Métrica | Valor |
|---|---|
| Ventas a crédito / año | 16,015 |
| Crédito otorgado (subtotal) | $117.3M |
| Ticket promedio | $7,322 sin IVA / ~$8,400 c/IVA |
| Efectivo cobrado por el sistema (ruta+enganche) | $81.9M = 78% |
| Margen bruto | 54.4% (markup 2.19x) |
| Castigo / pérdida real | ~2.16% |
| Recompra (2+ compras) | ~47% (avg 2.4 compras) |
| Ciclo de recompra | ~331 días (~11 meses) |
| Vendedores (rutas) | ~39 |

El crecimiento del ingreso (~9-16%/año) es **mayormente inercia** (libro +10% + inflación), NO eficiencia. → El valor del dev es el **delta sobre control**.

---

## 2. Economía unitaria (validada)

**Margen bruto 54.4%** ($101.8M venta vs $46.5M costo). Cascada por venta (% del precio):

| Concepto | % |
|---|---|
| Margen bruto | 54.4% |
| − Cobrador (de cada pago) | −10% |
| − Castigo | −2.5% |
| − Entrega/bodeguero | −4% |
| **= Contribución** | **~38%** |
| − Overhead fijo (estimado) | ~−12% |
| **= Neto antes de comisión** | **~26-32%** |

**Comisión de vendedores (a dedo, por tramos, tope $700 total):** venta $8,000 → $600 total → **$300 por vendedor**. Tope: **$700 total / $350 por vendedor** para ventas >$20k.
- Comisión promedio ponderada (con tope, distribución real de tickets): **~$285/vendedor** (1 vend.) / **~$570** (2 vend.).

> **Nota IVA:** los montos crudos en `IMPORTES.IMPORTE` son **subtotal sin IVA**; total real = `IMPORTE+IMPUESTO`. Saldo se calcula por cargo, con IVA, sumando solo balances abiertos.

---

## 3. La oportunidad (Tehuacán)

Público general (TIPO 21499) + sin castigo + con crédito. `CIUDAD_ID=338`.

| Estado | Definición | Clientes | Con tel. |
|---|---|---|---|
| **Buenos (total)** | | **13,014** | 8,083 |
| Reciente | <90 días | 1,137 | |
| **Por liquidar** | debe, ≥70% pagado | 1,027 | 817 |
| Activo medio | debe, <70% (**NO contactar**) | 1,772 | |
| **Dormido** | liquidado, 90-540 días | 1,558 | 1,307 |
| Frío | liquidado, +18 meses | 7,520 | |

**Contactables que cumplen estándar: 5,617.** Excluidos por deber: 2,466.
**Prime-listos para el demo:** 154 (por liq. ≥90%) + 432 (dormido fresco) = **586.**

Tehuacán ≈ 36% del mercado caliente de la red (~40,486 público general; ~39 rutas).

**Distribución de tickets (Tehuacán 2025, c/IVA):** <5k 26.9% · 5-8k 33.5% · 8-10k 18.3% · 10-15k 11.2% · 15-20k 5.5% · 20k+ 4.3%. (60% son <$8k.)

---

## 4. Calidad del crédito — el riesgo por historial (validado)

| Historial | Clientes | Tasa de castigo |
|---|---|---|
| 1 compra | 23,199 | **12.9%** |
| 2-3 compras | 13,185 | 8.4% |
| 4-5 compras | 4,219 | 4.6% |
| 6+ compras | 2,829 | **2.7%** |

**El sistema vende solo a clientes de 4-6+ compras ya probados → riesgo ~2.7-4.6% (5x más seguros que un cliente nuevo).** Las ventas del sistema son las más seguras del negocio.

---

## 5. Conversión esperada (anclada en cadencia real, NO benchmarks)

> La investigación de mercado refutó casi todos los números de conversión de la industria. Se ancla en: ciclo ~11 meses, 46% de recompras dentro de 6 meses, 68% dentro de 12 meses.

| Segmento | Conversión (eventual) | En 8 sem (demo) |
|---|---|---|
| Por liquidar (casi acaban) | 15-25% | ~20% |
| Dormido | 6-12% | ~8% |
| Frío | 2-5% | ~3% |

**Control orgánico (90 días): ~2%** — la línea base que se resta.

---

## 6. El demo

AI hace outreach (~$60 en tokens); humano(s) cierran. Ticket Tehuacán ~$8,406. Ventana ~8 sem.

| Config | Ventas | $ en ventas |
|---|---|---|
| Solo un cerrador (limitado por capacidad) | ~28-53 | ~$235-445k (central ~$335k) |
| 2-3 cerradores | ~70-120 | ~$0.6-1M |
| Multi-cerrador, pool completo (central) | ~165 | **~$1.4M** |

Con un cerrador, el límite es su capacidad (hay ~586 prime). Con varios, el límite pasa a logística/crédito.

**Las ventas del demo auto-hecho son tu "muestra gratis"** (no peleas por cobrarlas) — el trato es por lo que sigue.

---

## 7. Proyecciones a 3 años (3 escenarios × 2 comisiones)

**Ventas por escenario:**

| Escenario | Año 1 | Año 2 | Año 3 | Año 3 en $ | % del negocio |
|---|---|---|---|---|---|
| Conservador | 150 | 500 | 950 | ~$8M | ~4% |
| Muy probable | 290 | 980 | 1,900 | ~$16M | ~9% |
| Optimista | 420 | 1,400 | 2,600 | ~$22M | ~12% |

*(Conservador = conversión a la mitad + rollout lento. Muy probable = conversión aguanta + rollout normal. Optimista = pulido desde Año 1 + rollout rápido.)*

**TU INGRESO TOTAL (base $416k + comisión $285 / 1 vendedor):**

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $459k | $559k | **$687k** |
| Muy probable | $499k | $695k | **$958k** |
| Optimista | $536k | $815k | **$1.16M** |

**TU INGRESO TOTAL (base $416k + comisión $570 / 2 vendedores):**

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $502k | $701k | **$958k** |
| Muy probable | $581k | $975k | **$1.50M** |
| Optimista | $655k | $1.21M | **$1.90M** |

**Rango Año 3:** de **$687k** (conservador, $285) a **$1.90M** (optimista, $570). Hoy: $286k.

> Base asumida $416k ($8k/sem). **Con sueldo actual ($286k), resta ~$130k/año** de cada celda. Promete sobre el conservador; aspira al muy probable.

---

## 8. Lo que se queda el dueño (neto, sin impuestos, IVA dentro)

**Neto del dueño por venta (ticket ~$8,400):**

| | Tu comisión $285 | Tu comisión $570 |
|---|---|---|
| **Le queda neto al dueño** | **$2,420 (28.8%)** | **$2,135 (25.4%)** |
| **Él gana (vs tú)** | **~8.5x** | **~3.7x** |

**Neto del dueño por año (con tu comisión $285):**

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $363k | $1.21M | $2.30M |
| Muy probable | $702k | $2.37M | **$4.60M** |
| Optimista | $1.02M | $3.39M | $6.29M |

**Neto del dueño por año (con tu comisión $570):**

| Escenario | Año 1 | Año 2 | Año 3 |
|---|---|---|---|
| Conservador | $320k | $1.07M | $2.03M |
| Muy probable | $619k | $2.09M | **$4.06M** |
| Optimista | $897k | $2.99M | $5.55M |

→ Aun con la comisión completa, **el dueño se queda con 3.7-8.5x lo que tú ganas, limpio de todo, de ventas que no existían.** Imposible que se sienta robo.

> **Caveat IVA:** aquí el IVA cuenta como margen del dueño (instrucción explícita). En la realidad fiscal lo remite al SAT → su neto verdadero es ~14% menor. El overhead (12%) es estimado.

---

## 9. Compensación (estructura + comisión)

**Por qué la comisión completa ($570) está justificada:** los pedidos se cierran en oficina y un **bodeguero de sueldo** entrega + hace papeleo. **Ningún vendedor toca las ventas del sistema** → el sistema reemplaza a los 2 vendedores. El dueño paga la misma comisión de siempre, solo que al sistema en vez de a 2 personas.

- **Comisión:** pides la de **1 vendedor (~$285)** como ancla justa, hasta la **completa (~$570)** con el argumento "mismo costo de siempre, reemplacé a los dos". Sigue **su tabla por tramos** (tope $700) — no inventas montos.
- **Base:** corrección a **$8,000/sem** ($7,000 si hay resistencia), justificada como **mercado/alcance** (opera el 78% + control de efectivo), NO "más por lo mismo". **Va DESPUÉS del demo, no el mismo día.**
- **Validación de mercado:** comisión de ventas (ya la paga), revenue share, gainsharing/Scanlon, PTU (reparto legal 10% en México).

---

## 10. Segunda palanca — automatización de cobranza (Fase 2-3)

Migrar buenos pagadores a OXXO/digital elimina la comisión ~10% del cobrador. Cobranza ruta $75.8M × 10% = **~$7.6M/año**.

| Migración | Ahorro/año |
|---|---|
| 10% | ~$760k |
| 25% | ~$1.9M |
| 40% | ~$3.0M |

Triple win: ahorro + quita efectivo a trabajadores (dolor #1 del dueño) + control. **OXXO** = rail para no bancarizados. Se cobra como **gainsharing (15-20% del ahorro)** = +$285-380k/año a 25% migración. Riesgo: el cobrador es disciplina de pago → solo migrar buenos pagadores.

---

## 11. Prueba de atribución — el grupo de control (RCT)

Partir los elegibles al azar: tratamiento (sistema los trabaja) vs control (no se tocan). A 60-90 días, comparar. Ejemplo: control 2% compran, tratamiento 8% → **incremental = 6 puntos = lo que el sistema hizo.** Se cobra solo el excedente sobre control → desarma "hubieran comprado igual".

**Crítico aun en el demo auto-hecho: deja un grupo de control.** Sin él, las ventas son tangibles pero discutibles.

---

## 12. PARTE A — Lo que le digo al dueño

**Llegas con el demo YA HECHO** (ventas reales con tu papá + grupo de control). Objetivo: mostrar resultados y **cerrar el trato**, NO pedir sueldo.

**Guion (8 pasos):**
1. **Apertura con su data:** "El 78% del dinero del negocio pasa por el sistema que manejo."
2. **La oportunidad:** "5,617 clientes en Tehuacán que ya compraron, ya pagaron, y dejaron de venir. Nadie los trabaja."
3. **La calidad/riesgo:** "Son sus más seguros: 3% de riesgo vs 13% de uno nuevo."
4. **Ejemplo real:** Janneth (6 compras, $87k, siempre pagó).
5. **YA LO PROBÉ:** "Con mi papá vendimos $X a gente que no compraba. A otros iguales no los tocamos, y no compraron. Esa diferencia la hizo el sistema. Se las regalé."
6. **El trato:** "Quiero hacerlo en grande. Solo me paga por venta nueva comprobada, como a un vendedor. Si no entra dinero, no me debe nada. Usted se queda con el triple, limpio."
7. **Cierre:** "¿Hacemos el trato para que lo haga con todos sus clientes?"
8. **(Opcional) 2ª palanca:** control de efectivo.

**El framing cuidado:** "El sistema hace lo del vendedor de campo —encuentra y reactiva—; su bodeguero entrega igual. No quita a nadie de la entrega, solo automatiza la prospección, que es lo que escala." *(Cuidado: tu papá es vendedor — enmárcalo como augmentar, no reemplazar.)*

**Lo que VE el dueño:** el trato por venta (~$285-570, como un vendedor) + que se queda 3.7-8.5x. **NUNCA ve tu ingreso total.**

---

## 13. PARTE B — 🔒 Estrategia privada (negociación)

```
PRIVADO. No se lo enseñes ni se lo des al dueño.
```

### Mi lectura honesta (¿quiere o no?)
- **¿Quiere hacerlo?** Probablemente SÍ (~75%) — es negocio, le sale gratis, llegas con prueba.
- **¿Te paga lo justo a la primera?** Duda real (~30-40%) — es apretado (al de bodega le dio $200). Espera que acepte el principio y **regatee el monto**.
- **El factor que mueve todo: el demo.** Con él, sí. Sin él (solo proyecciones), no apostaría.
- La pelea no es el "sí" — es el "cuánto". Tu trabajo: **no aceptar la primera migaja.**

### Qué pides y en qué orden
1. **Hoy:** la comisión ($285-570/venta) + luz verde. El "sí" fácil.
2. **Después del demo, desde la fuerza:** base $8,000/sem.
3. **Fase 2-3:** gainsharing en cobranza.
4. **Nunca** base y comisión el mismo día.

### El "sí, pero te pago menos" — manual
**Ancla ALTO:** abre en $570 para aterrizar en $285-400.

| Te dice… | Tu respuesta |
|---|---|
| "Te doy un bono y ya" | "Esto no es de una vez, el sistema sigue generando. Atémoslo a lo que produzca." |
| "Te subo poquito el sueldo" | "El sueldo es por mi trabajo de siempre, aparte. Esto es por el dinero NUEVO, y crece." |
| "Te doy comisión de $100" | "Un vendedor se lleva $285. Yo hago el trabajo de dos." |
| "Es mucho" | "No mire lo que me paga — mire lo que se queda: ~$4M netos. Yo pido una rebanada." |
| "Ya veremos" ⚠️ | "Dejemos la REGLA clara hoy, aunque arranquemos chico." |

### Escalera de respaldo (si rechaza comisión por venta)
1. **Bono periódico** sobre venta nueva medida (3.5-7% cada 6 meses, revisable). El mejor reemplazo.
2. **Bono por meta** (al llegar a $X, bono $Y). Predecible, topado.
3. **% de utilidad nueva** del programa (gainsharing 15-20%).
4. **Aumento de base** "por operar el sistema" (si no acepta variable).
5. **Gainsharing en ahorro de cobranza** (la 2ª palanca como entrada).

### Hasta dónde ceder
| Nivel | Qué | ¿Aceptar? |
|---|---|---|
| Ideal | $570/venta o $400+ con tope | 🟢 |
| Bueno | $285/venta | 🟢 |
| Piso | ~$200/venta o bono ~3% o base $8k + bono meta | 🟡 solo con fórmula escrita + cláusula de revisión |
| Walk-away | Bono chico único sin fórmula | 🔴 (trato del de bodega) |

### Las 6 tácticas
1. **Silencio** tras el lowball (3-4 seg; él lo llena mejorando la oferta).
2. **Nunca aceptes la primera oferta.**
3. **Re-ancla al VALOR**, no al costo ("se queda con el triple, limpio").
4. **No cedas gratis — intercambia** (bajas tu número → sacas: quitar tope / subir base / cláusula de revisión).
5. **La fórmula vale más que el primer número.** Acepta inicial chico SI dejas la regla + cláusula de revisión al llegar a $X.
6. **Tu carta:** "ya le generé $X gratis; no pido favor, pido compartir lo que siga trayendo" → pásale la pelota de cómo estructurarlo.

### Tu ventaja única
Eres el **único que le trae proyectos** (ni los hermanos) → socio, no empleado. Úsalo con humildad; **trae al hermano DBA contigo** (avalista de rigor), no por encima.

### Lo no negociable
- **Algo atado a resultados, por escrito** (aunque sea un WhatsApp suyo). Sin eso, es el sueldo de siempre disfrazado.
- Si rechaza TODO sobre un sistema que le hace millones → no es mecanismo, es que no quiere compartir. Ahí tu carta es tu valor de mercado y que **tú decides si construyes/escalas esto.**

---

## 14. Riesgos y caveats

- **No shippear el demo** es el riesgo #1. Todo el techo vale cero sin el primer número.
- Conversión es **hipótesis hasta medirla** (anclada en cadencia real, no probada).
- **Atribución** se cae sin grupo de control.
- **Operaciones (entrega/crédito)** se saturan en Año 2-3 — el cuello deja de ser demanda.
- Proyecciones Año 2-3 asumen rollout a la red + pulir.
- Margen 54% es bruto; overhead 12% estimado; IVA contado como margen (real ~14% menos).
- Comisión "a dedo" con tope $700 → promedio ponderado ~$285/vend.

---

## 15. Próximos pasos

1. **Correr el demo auto-hecho** con tu papá — con **grupo de control** y registro limpio (lista, fechas, tratamiento vs control, ventas). Es lo único que da la luz verde.
2. **Iniciar verificación de WhatsApp Business** (cuello de botella de tiempo) — o arrancar con cuenta normal para el demo.
3. **Armar la lista priorizada** (154 + 432 prime) con next-best-product por cliente.
4. **Playbook del agente AI** (system prompt + objeciones + handoff).
5. **Presentar al dueño** con resultados (Parte A) — ganar al hermano primero.
6. **Negociar** con la Parte B en mano — salir con la **fórmula escrita + cláusula de revisión**, sí o sí.

> **El objetivo no es el monto perfecto en la primera junta — es salir con la regla atada para crecer desde la fuerza. De que sales con progreso, sales: no se vuelve a cobrar $5,500 después de esto.**
</content>
