# Automatización de ventas con IA — análisis completo y estrategia

> Documento maestro. Consolida el análisis de reactivación/winback de clientes por
> WhatsApp con IA: la oportunidad, los números validados en la base, el diseño del
> demo, la economía, la compensación del desarrollador y la estrategia con el dueño.
>
> Todos los números **VALIDADO** salen de la copia Microsip (Firebird en Docker),
> consultada en **solo lectura**. Corte de datos efectivo: **~febrero 2026** (la cola
> del snapshot está incompleta; anclar antigüedades a 2026-02-28). Complementa
> [`business-case-ventas-cobranza-ia.md`](./business-case-ventas-cobranza-ia.md) y
> [`pilot-winback-dormidos.md`](./pilot-winback-dormidos.md).

---

## 1. Resumen ejecutivo

- El **78% del efectivo del negocio ($81.9M en 2025)** pasa por el sistema operativo que
  el desarrollador construye y opera (cobranza en ruta concepto 87327 + enganche).
- El negocio **vende solo a clientes que ya compraron antes** → la reactivación/recompra
  **ES el negocio**, no un extra. Un vendedor de IA por WhatsApp que reactiva clientes
  dormidos y "por liquidar" automatiza el motor central.
- **Margen bruto real validado: 54.4%** (markup 2.19x). Tras costos operativos
  (cobrador, castigo, entrega): **contribución ~38%**.
- **Demo estimado:** con tu papá solo ~$300-350k; con 2-3 cerradores ~$0.6-1M; central
  multi-cerrador ~$1.4M en ventas (8 semanas, solo Tehuacán).
- **Compensación propuesta:** base + **comisión tipo vendedor (~$300/venta)** sobre venta
  nueva medida contra grupo de control. La empresa se queda ~10x lo que el dev gana.
- **Proyección de ingreso del dev (comisión $300/venta + sueldo):** Año 1 ~$390-520k ·
  Año 2 ~$650-780k · Año 3 ~$950k-1.1M.
- **Segunda palanca (Fase 2-3):** automatizar cobranza migrando buenos pagadores a pago
  digital/OXXO elimina la comisión ~10% del cobrador (~$7.6M/año en total) → ahorro de
  ~$1.5-2M/año y resuelve el dolor #1 del dueño (efectivo en manos de trabajadores). Ver §11.

---

## 2. El negocio (datos validados, 2025)

| Métrica | Valor | Fuente |
|---|---|---|
| Ventas a crédito / año | **16,015** | DB |
| Crédito otorgado (subtotal sin IVA) | $117.3M | DB |
| Ticket promedio (sin IVA / con IVA) | $7,322 / ~$8,406 | DB |
| **Efectivo cobrado por el sistema (ruta+enganche)** | **$81.9M = 78%** | DB |
| **Margen bruto de producto** | **54.4%** (markup 2.19x) | DB ($101.8M venta vs $46.5M costo) |
| Castigo / pérdida real | ~2.16% | DB |
| Recompra (clientes con 2+ compras) | ~47% | DB |
| Ciclo de recompra promedio | **~331 días (~11 meses)** | DB |
| Vendedores (rutas) | ~39 (RUTA01-39) | DB |

**Estructura de costos (cascada por venta, % del precio):**

| Concepto | % |
|---|---|
| Margen bruto | 54.4% |
| − Comisión cobrador (de cada pago) | −10% |
| − Castigo/incobrable | −2.5% |
| − Entrega/logística | ~−4% |
| **= Contribución antes de comisión de venta** | **~38%** |

> Los gastos fijos (bodega, oficina, secretarias) NO aplican a ventas incrementales —
> ya están pagados. Para el winback la métrica correcta es la **contribución (~38%)**.

**Comisión de vendedores (a dedo, por tramos):** venta de $8,000 → comisión total **$600**
→ **$300 por vendedor** (normalmente 2 vendedores se reparten). ≈7.5% de la venta.

---

## 3. La oportunidad medida (Tehuacán)

Tehuacán = mercado más fuerte (~36% del mercado caliente de la red). Ubicación vía
`DIRS_CLIENTES.CIUDAD_ID=338`. Tipo cliente `PUBLICO EN GENERAL` (21499) = retail real;
mayoreo/medio mayoreo están aparte (~49 clientes). Castigados (fugas 27967 / mal cliente
27968) excluidos.

**Universo (lógica corregida: saldo por cargo, con IVA, solo balances abiertos):**

| Estado | Definición | Clientes | Con teléfono |
|---|---|---|---|
| **Clientes buenos (total)** | público gral, sin castigo, con crédito | **13,014** | 8,083 |
| Reciente | compró <3 meses | 1,137 | |
| **Por liquidar** | debe, ≥70% pagado (casi termina) | **1,027** | 817 |
| Activo medio | debe, <70% pagado (**NO contactar**) | 1,772 | |
| **Dormido** | liquidado, 3-18 meses sin volver | **1,558** | 1,307 |
| Frío | liquidado, +18 meses | 7,520 | |

**A quiénes contactaríamos** (cumplen estándar + con teléfono + no deben o casi acaban):
**5,617** — por liquidar 817, dormido 1,307, reciente liquidado 144, frío 3,349.
**Excluidos por deber todavía: 2,466.**

**"Listos para comprar YA" (prime para el demo):**

| Segmento | Clientes |
|---|---|
| Por liquidar ≥90% pagado (a punto de terminar) | **154** |
| Dormido fresco (3-9 meses) | **432** |
| Por liquidar 70-90% | 662 |
| Dormido medio (9-18 meses) | 875 |

---

## 4. Conversión creíble (anclada en la data, NO en benchmarks)

> La investigación de mercado (estado del arte 2026) **refutó casi todos los números de
> conversión de la industria**. La conversión se ancla en la cadencia real de recompra:
> ~11 meses, 46% de recompras dentro de 6 meses, 68% dentro de 12 meses.

| Segmento | Conversión creíble | Por qué |
|---|---|---|
| Por liquidar (casi acaban) | **15-25%** | En su punto de rebuy + liberan abono |
| Dormido | **6-12%** | Pasaron su ciclo; el outreach los reactiva |
| Frío | **2-5%** | Cola larga |

**Conversión del grupo de control (orgánica, 90 días): ~2%** — la línea base que se resta.

---

## 5. El demo — estimaciones

Demo = piloto de winback en Tehuacán, AI hace outreach + humano(s) cierra(n). Ventana ~8
semanas. Ticket Tehuacán ~$8,406.

| Configuración | Ventas | $ en ventas |
|---|---|---|
| Solo tu papá (limitado por su capacidad de cierre) | ~28-53 | **~$235-445k** (central ~$335k) |
| 2-3 cerradores (recomendado) | ~70-120 | **~$0.6-1M** |
| Multi-cerrador, pool completo (central) | ~165 | **~$1.4M** (rango $0.9-1.9M) |

**Cuello de botella:** con un cerrador, el límite es su capacidad (no la demanda — hay
~586 prime-calientes). Con varios cerradores, el límite pasa a **logística/entrega y
aprobación de crédito** (no la IA, que aguanta el volumen por ~$60 total).

**Recomendación:** demo **limpio de $600k-$1M con 2-3 cerradores** > demo grande y caótico.
La luz verde la da la **atribución clara de que fuiste tú**, no el tamaño.

### Economía del demo central (~165 ventas, ~$1.39M, sin impuestos)

| Concepto | Monto |
|---|---|
| Ventas | $1,390,000 |
| − Mercancía (~40%) | −$556,000 |
| − Cobrador (10%) | −$139,000 |
| − Castigo (2.5%) | −$35,000 |
| − Entrega (4%) | −$56,000 |
| − Comisiones (tú + cerrador, $600/venta) | −$99,000 |
| **= La empresa se queda** | **~$505,000 (~36%)** |
| **Tú (comisión)** | **~$50,000** ($300/v) a ~$99,000 ($600/v) |

→ La empresa se queda **~10x** lo que gana el dev. Imposible que se sienta robo.

---

## 6. Estado del arte 2026 — técnicas a copiar (validadas)

> Confirmadas por verificación adversaria. Los **números** de conversión de la industria
> se refutaron — copiar las **técnicas**, medir los números uno mismo.

1. **No outbound frío** (colapsó: 11x perdió 70-80% de clientes). Tu lista leal/opt-in está
   del lado correcto.
2. **Propensity scoring** para priorizar a quién contactar (reglas simples bastan).
3. **Next-best-product** del historial real (no genérico) — ej. estufa/pantalla a quien ya
   tiene refri+lavadora.
4. **Clasificar la respuesta por intención → guion por objeción.**
5. **Decodificar la objeción latente** ("está caro" = "¿me cabe en la quincena?").
6. **Human-in-the-loop en 3 niveles:** saludo=auto · negociación=aprobación · crédito=handoff.
7. **Handoff con disparadores** (intención de compra, frustración, confianza <50%) y
   **contexto completo en el mismo hilo de WhatsApp** (el cliente nunca repite nada).
8. **Medir por resultado** (Sierra: $100M ARR cobrando por resolución).

**Costos (junio 2026):** modelo barato (Gemini Flash ~1¢/conv, Claude Haiku ~3¢, Sonnet
~8.5¢). Demo completa <$25 USD. WhatsApp Business México: marketing $0.0436/msg. **El modelo
NO es el costo alto.** Recomendación: demo con Sonnet (calidad de conversión), escalar con
Haiku/Flash + prompt caching.

**Canal:** demo con cuenta WhatsApp normal (rápido, gratis, riesgo de baneo bajo si es
personalizado/ritmo humano); escalar con WhatsApp Business Platform (plantillas aprobadas).
**Iniciar verificación de WhatsApp Business YA** — es el cuello de botella de tiempo.

---

## 7. Proyección a 3 años

Supuestos: base $8,000/sem ($416k/año, corrección); comisión $300/venta; el negocio crece
~10%/año; el sistema escala de Tehuacán a la red (flywheel: vendedores alimentan la base +
ciclo anual la renueva + IA mejora con datos).

| Año | Ventas del sistema AI | % del negocio | Comisión ($300/v) | Ingreso total dev¹ |
|---|---|---|---|---|
| **1** | ~235-350 / $2-3M | ~1.3-2% | $70-105k | **~$390-520k** |
| **2** | ~765-1,200 / $6.5-10M | ~4-6% | $230-360k | **~$650-780k** |
| **3** | ~1,650-2,200 / $14-18M | ~7.7-10% | $500-660k | **~$950k-1.1M** |

¹ Base $416k + comisión. Hoy: $286k ($5,500/sem). Con $600/venta los montos ~duplican.

> **Aspiracional (pulido, maduro):** el sistema podría llegar a $12-20M/año = ~10-15% del
> negocio → agregaría tanto crecimiento nuevo como lo que el negocio crece solo (~duplicar
> el ritmo). Es el techo, no el piso.

---

## 8. Compensación — estructura justa

**Modelo:** base (por operar/construir los sistemas) + **comisión por venta nueva medida
contra control** (como un vendedor, +un poco por construir la máquina).

- **Base:** corrección de $5,500 → $7,500-9,000/sem. Justificada por ser dueño del cerebro
  operativo (78% del efectivo) + resolver el control de efectivo (ver §10).
- **Comisión:** **$300/venta** (= lo que gana 1 vendedor; el dueño se ahorra al 2º porque
  la IA hace su trabajo). Opción agresiva: $600 (reemplazó a los 2).
- **Sobre venta MEDIDA CONTRA CONTROL** — no negociable.

**Por qué es justo (no robo):** por cada $1 de comisión, la empresa retiene ~$2.8 de
contribución real (ya restados cobrador/castigo/entrega) y ~92% de la venta. El bono del dev,
aun al 10%, es **<1% de la venta total del negocio**.

**Validación de mercado:** comisión de ventas (el dueño ya la paga), revenue sharing,
**gainsharing/Plan Scanlon** (desde 1935, comparte ahorros/ganancias), y **PTU** (México:
reparto legal del 10% de utilidades). No es un invento — el dueño ya practica estos modelos.

**Estrategia de escalonamiento:** cerrar base + comisión AHORA (montos chicos, nadie se
espanta) con cláusula de *"revisión al llegar a $5M"*. El flywheel infla el bono después,
como hecho consumado.

---

## 9. La prueba de atribución (lo más importante)

**Grupo de control (holdout / RCT)** — la única forma irrefutable:

1. Tomar los elegibles, partirlos **al azar**: tratamiento (el sistema los trabaja) vs
   control (no se tocan).
2. A 60-90 días, contar compras en cada grupo.
3. **El control dice cuántos comprarían solos. El excedente del tratamiento es del sistema.**

Ejemplo: control 300 → 6 compran (2%); tratamiento 300 → 24 (8%); **incremental = 18 ventas
que no existirían sin el sistema.** El dev cobra solo el excedente (las 18), no las 6 que
hubieran comprado solas → desarma el "hubieran comprado igual".

La lista de tu papá (`.pilot-data/`) ya trae columna `brazo` (split al azar por hash). La
medición: ventas nuevas (concepto 5) de IDs tratamiento vs control tras la fecha de contacto,
verificadas en la base. **El dueño debe aceptar no tocar el control durante la prueba.**

---

## 10. Estrategia con el dueño

**Perfil:** dueño de pueblo (cerca de Tehuacán), no técnico, paga **solo si es negocio**
("a otro de bodega que pidió aumento haciendo lo mismo le dio +$200/sem"). Se está
modernizando (habla de inflación, socios/proveedores). Su hermano (ex-admin DBA) es aliado de
credibilidad. **Su dolor #1 (afecta su salud): trabajadores que manejan efectivo y reportan
después → conflictos.**

**Reglas de oro:**
1. **Mostrar dinero tangible, no proyecciones.** Correr el demo callado, generar ventas
   reales visibles en SU Microsip, luego pedir.
2. **Framing de NEGOCIO, nunca "aumento por mi trabajo"** (esa es la categoría del de bodega
   = $200). Entrar como "le traje dinero nuevo, quiero mi parte".
3. **Lenguaje de dueño:** "clientes que volvieron + pesos que entraron + yo lo hice". Cero
   jerga (nada de flywheel, conversión, contribución).
4. **Gancho de inflación:** "usted dice que hay que ganarle a la inflación; yo le agrego
   crecimiento NUEVO arriba de eso, medible".
5. **Ganar al hermano primero** con el rigor (control group) → avalista interno.

**Palanca extra — control de efectivo (su dolor #1):** sobre la app de cobranza (que ya
captura GPS `LAT`/`LON` + hora), construir captura en tiempo real + conciliación automática +
geo-verificación + pago digital gradual. Le quita dinero directo a trabajadores y le da
control desde la oficina. **Abrir con esto (defensa/su salud) genera confianza antes de pedir
el bono de ventas (ataque/dinero nuevo).**

---

## 11. Palanca 2 — Automatización de cobranza (ahorro del 10% del cobrador)

> La primera palanca (§3-§10) es **ofensa**: vender más con IA. Esta es **defensa**:
> automatizar la cobranza para eliminar la comisión del cobrador y dar control a la oficina.
> Se construye sobre el mismo motor (la app de cobranza que ya captura GPS+hora). Va en
> **Fase 2-3**, después de probar las ventas.

**El lever:** cada cliente que paga por cobrador le cuesta a la empresa **~10% de comisión**
en cada cobro. Migrar a los buenos pagadores a **pago digital** (depósito/transferencia/CoDi
o **OXXO** con referencia → cuenta de la empresa) **elimina ese 10%** en sus pagos.

**Tamaño (validado):** cobranza en ruta (concepto 87327) cobró **$75.8M en 2025** → la
comisión de cobrador (~10%) es **~$7.6M/año**.

| Migración a digital | Ahorro de comisión/año |
|---|---|
| 10% de los cobros | ~$760k |
| **25%** | **~$1.9M** |
| 40% | ~$3.0M |

**El triple win (por esto al dueño le encanta):**
1. **Ahorra el 10%** de comisión en cada cuenta migrada → utilidad directa.
2. **Quita el efectivo de las manos del trabajador** → su dolor #1 (afecta su salud).
3. **Control desde la oficina** → el pago cae directo a la cuenta, en tiempo real.

**Rail clave para este mercado: OXXO.** Mucha gente cerca de Tehuacán no tiene cuenta ni
smartphone → transferencia/CoDi no aplica. **OXXO (efectivo con referencia → cuenta empresa)
es el puente para los no bancarizados** y lo que hace esto viable.

**Qué se construye:** sobre la app de cobranza (que ya captura `LAT`/`LON`+hora) →
captura en tiempo real + **conciliación automática** + **geo-verificación** + generación de
referencias OXXO / links de pago. La **segmentación** (mismo músculo del winback): el sistema
identifica al segmento "digital-seguro" (paga puntual + tiene medios) y deja al cobrador solo
donde se necesita.

**Cómo se cobra (segundo bono — gainsharing / Plan Scanlon):** un **% del ahorro medido**
(ej. 15-20% de la comisión que ya no se paga). Medible: pagos digitales × 10% no pagado.
25% de migración = ~$1.9M ahorrado → parte del dev **~$285-380k/año**, aparte de la comisión
de ventas. Dos fuentes de bono: **venta nueva (ofensa)** + **ahorro de cobranza (defensa)**.

**Riesgos propios de esta palanca:**
- **El cobrador también es disciplina de pago** (la visita semanal mantiene al cliente
  pagando). Migrar mal → **más mora**. Solo migrar **buenos pagadores con medios**, no a quien
  necesita el empujón semanal.
- **Resistencia del cobrador** (pierde comisión) → puede irse o sabotear. El dueño lo quiere,
  pero hay que tener su respaldo.
- **Gradual**, no de golpe — migración del segmento correcto.

**Secuencia:** (1) demo de ventas → luz verde + confianza · (2) control de efectivo → resuelve
su dolor · (3) migración a digital → el ahorro del 10%. No soltar las tres el primer día.

---

## 12. Riesgos honestos

- **No shippear el piloto** es el riesgo #1. Todo el techo vale cero sin el primer número.
- Conversión es **hipótesis hasta medirla** (los % son anclados en cadencia real, no probados).
- **WhatsApp Business** puede atorarse en verificación → empezar ya.
- **Logística/entrega** se satura si vendes rápido con varios cerradores.
- **Atribución** se ensucia si vendedores tocan el control o si hay muchos cerradores.
- Proyecciones Año 2-3 asumen escalar a la red + pulir — solo el Año 1/demo es lo que el
  piloto prueba.
- Margen 54% es **bruto**; con todo el overhead absorbido la utilidad neta del negocio es
  menor (~15-25% típico en mueblería). Para ventas incrementales la métrica es contribución.

---

## 13. Próximos pasos

1. **Iniciar verificación de WhatsApp Business** (cuello de botella de tiempo).
2. **Acordar con el dueño** la fórmula (base + comisión/venta) — semi-escrito — y el "no
   tocar el control".
3. **Generar la lista priorizada** de prime (154 por-liquidar ≥90% + 432 dormidos frescos)
   con su canasta y next-best-product por cliente.
4. **Armar el playbook del agente AI** (system prompt + personalización + buckets de objeción
   + reglas de handoff).
5. **Correr el demo** (8 semanas, 2-3 cerradores, grupo de control).
6. **Medir el delta** vs control → el número para la luz verde y la conversación de comp.
</content>
