# Investigación — Chatbot de ventas a crédito "state of the art 2026"

> Documento de consulta. Recopila la investigación de mercado (jul 2026) para el diseño del sistema de
> reactivación con IA (Pieza R7 / módulo `reactivacion`). Cada afirmación lleva su fuente al final.
> **No es un spec** — alimenta el spec. Contexto: mueblería a crédito, canal WhatsApp vía `whatsmeow`
> (no oficial) para el demo → API oficial después. Métrica de éxito del demo: **enganche** (no venta cerrada),
> treatment vs. grupo control.

---

## 0. TL;DR — lo que reordena el diseño

1. **El benchmark real es el BDC automotriz**, no Klarna/Sephora (esos son *servicio*). Bien durable + financiado
   + leads dormidos + recompra por ciclo = nuestro caso. Reactivación con IA **15-35%** vs 5-8% manual.
2. **A quién le escribes > cuánto.** Mensajear a no-contactos/fríos es la mayor bandera de baneo. Nuestro
   segmento (recién-liquidado / por-liquidar) responde → nos protege. La elección de segmento es anti-baneo,
   no solo pro-conversión.
3. **En ventas se escala en el momento BUENO** (señal de compra → humano cierra same-day), al revés que en
   soporte (que escala en el momento malo).
4. **"La IA recomienda; el sistema gobernado decide."** Crédito es industria regulada → capa de política en
   runtime + log auditable + contenido semi-armado para lo sensible.
5. **Lección Klarna:** 67% automatizado *bien* > 90% *mal*. Umbral de escalamiento **alto**, no bajo.
6. **Se vende la mensualidad, no el precio.** Enganche + pago mensual, nunca el total.

---

## 1. Evitar el baneo (whatsmeow / canal no oficial)

**Realidad base:** clientes no oficiales (Baileys, whatsmeow, WAHA, Evolution) tienen **ventanas de baneo de
2-8 semanas**. Meta empuja updates de protocolo varias veces al año y caza conexiones de protocolo viejo.
Diseñar con premisa **"el chip se muere, es cuándo no si"** (nunca el número del negocio; respaldo de
conversaciones; re-pareo rápido).

**Señales de detección, por peso:**

| Señal | Detalle |
|---|---|
| **A quién escribes** | Contactos que no te tienen guardado / desconocidos = "la mayor bandera roja", sola. No hay tope diario publicado en la app normal: el signal es *quién*, no *cuántos*. 200 a contactos que responden puede estar bien; 30 fríos a extraños te marca. |
| **Tasa de respuesta** | <~10% de respuestas = spam de una vía. Mensajes sin respuesta en 48h se acumulan hacia restricción. |
| **Intervalos robóticos** | "Exactamente cada 10s" se detecta. Hay que **variar (jitter)**, no pausar-fijo. |
| **Texto idéntico** | En reloj fijo = lo más fácil de marcar. La personalización de la IA (cada mensaje distinto) es una **ventaja anti-detección**. |
| **Bloqueos/reportes** | Disparan degradación rápida. |

**Números concretos de calentamiento / operación:**
- **Semana 1:** 20-50 mensajes/día (o solo 10-20 rondas de ida y vuelta). Máx ~2/min.
- Actividad **<6h/día**, no 3 días seguidos la primera semana.
- Calentar **2-4 semanas**; el número se estabiliza a los ~20-30 días. Empezar en frío con volumen = muerte.
- Escalar volumen semanal **solo si la calidad se mantiene**.

**Traducción a diseño → "gobernador de envío"** (no un `sleep` fijo): jitter (ej. 90s-8min), tope diario,
solo horas hábiles, y **circuit-breaker por tasa de respuesta** (si cae del umbral, auto-pausa + alerta).
El flag `auto_send` en "pausado-gobernado" por defecto; "instantáneo" jamás en chip no-oficial.

---

## 2. Handoff IA↔humano (patrones HITL 2026)

**Definición:** el handoff es la frontera diseñada entre lo que la IA hace sola y lo que requiere humano, +
la arquitectura para pausar, entregar contexto, y **retomar limpio** ("reentry es concepto de primera clase").

**Disparadores de escalamiento (multi-señal, no solo confianza):**
- **Confianza calibrada por dominio:** consumo 60-70%, soporte empresa 80-85%, financiero 90-95%. Los scores
  crudos necesitan calibración (temperature scaling, conformal prediction).
- **Señales de comportamiento:** loop (repite lo mismo 3+ veces), degradación de sentimiento, **petición
  explícita de humano** (inmediato, cero fricción), complejidad, **stake-based** (cuenta VIP, montos altos).
- **Meta de calibración: escalar 10-20%.** >20% = sobre-escala (cola de revisión inútil); >60% = cuello de
  botella que anula el propósito.

**Briefing estructurado (NO el chat crudo):** "un handoff no es transferir la conversación, es transferir el
*estado de trabajo*". Campos: `intent, confidence, sentiment, entities, actions_taken, constraints,
suggested_next_step, transcript_reference` (referencia, no lo embebes).

**Modos de transferencia:** *warm* (IA informa al humano antes de entregar; para casos delicados/VIP) vs.
*cold* (empaqueta contexto → cola; más rápido, falla si el briefing no es autosuficiente).

**Compuerta de aprobación (draft-and-approve):** para acciones irreversibles, la acción **no se pre-ejecuta**;
corre solo con aprobación humana. Para decisiones corregibles → auditoría asíncrona con rollback.

**Cadena de handoffs ≤4** (más falla desproporcionadamente). Cuidado con *context bleed* (historia
irrelevante confunde al que recibe) y *reviewer fatigue* (escalar >20% → sellos de goma).

---

## 3. Reactivación de clientes por WhatsApp (México / crédito)

**Estructura del opener:** saludo personalizado + referencia a comportamiento pasado específico (producto,
compra, tiempo transcurrido) + un solo objetivo + CTA suave. Tono **conversacional y cercano**, breve; lo
corporativo se lee como notificación automática y baja la intención de respuesta.

**Segmentación por situación (mapea a nuestros segmentos):**
- **Recién liquidado / cliente leal** → reconocimiento + beneficio exclusivo. *"Por haber sido cliente tienes
  acceso a un beneficio especial."*
- **Inactividad media (60-90d)** → reconectar, mostrar novedades.
- **Deudor al corriente (por liquidar con hueco)** → **cuidado**: no lideras con "compra más"; completar el
  juego / complemento con pago que le cabe.

**Cadencia recomendada por la práctica de reactivación: 3 mensajes** (inmediato → +3-5d con incentivo →
+7-10d con oferta concreta). *"Un solo mensaje falla."*

> ⚠️ **TENSIÓN con §1 (baneo):** los blogs de reactivación asumen **canal oficial + opt-in**. En chip prepago
> con fríos, insistir sube el riesgo. **Resolución para el demo: máx 2 toques** (opener + 1 seguimiento suave a
> ~día 3 solo si no hubo respuesta; luego descartar). El circuit-breaker manda sobre el best-practice. Los 3
> toques se abren al migrar a API oficial con opt-in.

**Vender la mensualidad, no el precio:** en crédito el cliente piensa en enganche + pago mensual, no en el
total. Todo el opener y las respuestas hablan en esos términos.

---

## 4. Manejo de objeciones (voz/chat de ventas)

**Framework:** detectar (por lenguaje, directo o indirecto) → clasificar → rebatir corto → **volver al
objetivo**. Brevedad: respuestas cortas suenan más seguras que un párrafo.

**Cinco tipos universales:**

| Objeción | Ejemplo | Estrategia |
|---|---|---|
| **Precio** | "está caro" | reframe a valor / **opciones de pago (mensualidad)** / comparación |
| **Tiempo** | "déjame pensarlo" | siguiente paso de bajo compromiso, urgencia suave, seguimiento agendado |
| **Confianza** | "¿cómo sé que sirve?" | prueba social, garantía, referencias |
| **Competencia** | "ya le compro a otro" | diferenciación, facilidad de cambio |
| **Autoridad** | "tengo que preguntarle a mi esposo/a" | incluir al que decide, mandar info compartible, callback conjunto |

En muebles la de **autoridad** es enorme. Objeciones compuestas: reconocer ambas en un turno y volver al
objetivo. La IA convierte 40-60% más que scripts estáticos que se colapsan a la primera resistencia.

---

## 5. Personalización gobernable (industria regulada — crédito lo es)

**Principio 2026:** en regulado la ventaja no es personalización agresiva sino **gobernable** — relevancia
*demostrablemente permitida, proporcional, explicable y confiable*. Cada interacción responde: ¿por qué lo
hicimos? ¿qué dato usamos? ¿estábamos permitidos? ¿podemos probarlo?

**"La IA recomienda; los sistemas gobernados deciden":** el modelo propone acción+contenido+timing → pasa por
capa de política en runtime (consentimiento, propósito, frequency caps, elegibilidad) → **solo si pasa, se
ejecuta.** El agente es rápido; la gobernanza atrapa violaciones antes de desplegar.

**Guardrails concretos:**
- **Consentimiento/propósito en runtime** (no asumir permiso por datos históricos).
- **Minimización:** señales derivadas sobre datos crudos; retención por propósito.
- **Ensamblado de contenido restringido:** módulos pre-aprobados (ofertas, montos, disclosures) que la IA
  combina, en vez de generación 100% libre para lo sensible. Preserva auditabilidad.
- **Auditabilidad por diseño:** responder *"¿por qué este cliente vio este mensaje?"* en minutos. Loguear en
  cada punto: identidad, permisos, propósito, módulos usados, razonamiento de decisión, resultado.

**Lo que la IA NO debe hacer:** inferencias sensibles sin consentimiento (p. ej. estrés financiero), acción
autónoma sin sistema gobernado que la haga cumplir, decisiones inexplicables, uso de datos fuera de propósito.

> **Aplicación directa a nosotros:** el flag `auto_send` + gobernador + **log de decisiones** ES esta capa.
> Y con el segmento que aún debe, el bot **nunca** toca la deuda existente (caso sensible → humano). Regulación
> mexicana de cobranza penaliza prácticas abusivas: reactivación y cobranza son canales separados, no se cruzan.

---

## 6. Memoria del agente (2026)

Memoria por cliente = **componente de primera clase** (ya no "stateless con cero personalización"). El agente
recuerda entre turnos y sesiones → nada de "¿quién eres?" en el segundo mensaje. Algoritmos token-eficientes
(extracción jerárquica de una pasada + recuperación multi-señal). Para nosotros: cada conversación guarda
estado; el copiloto entra al handoff con memoria completa del cliente.

---

## 7. Benchmark: cómo lo hacen los tops

### 7.1 El espejo correcto — BDC automotriz (bien durable + financiado)

**Segmentación (5 tipos):** orphan leads (vendedor que se fue), leads viejos de internet (30-120d),
clientes de servicio con equity (trade-in), tráfico de piso no cerrado, y **clientes de hace 2-4 años en
ciclo de reemplazo = la mayor confianza** ← = nuestro recién-liquidado.

**Resultados:** reactivación **15-35%** con IA (vs 5-8% manual); **+53% lead-to-close** (CDK Global);
tiempo a cita 3-7d; show 55-65%; close 8-15%. Caso: un dealer pasó de 205 a 448 citas/mes, ~$83k impacto en 30d.

**Handoff:** *"si la conversación revela intención real, el lead SALE de la cadencia automática y entra a
pipeline humano"* — **el mismo día** (esperar 24h+ convierte mal). Confirma nuestra inversión: escalar en la
señal de compra, con urgencia same-day.

**Cadencia automotriz (30d, multi-canal):** día 1 SMS re-intro, 2 email valor, 5 llamada/voz-IA calificar, 8
SMS seguimiento, 14 email oferta, 21 SMS "¿sigues interesado?", 30 archivar. **OJO:** operan bajo compliance
(FCC DNC / permiso previo para marketing) — por eso pueden 7 toques; nosotros no (ver §3).

### 7.2 Servicio (Klarna/Sephora/H&M) — referencia de escala + la lección

- **Klarna:** IA manejó 2/3 de los chats en 30 días (2.3M chats), resolución 11min→<2min, $60M ahorro. **Pero
  automatizó de más y echó humanos para atrás.**
- **Sephora:** 75% de consultas resueltas sin humano; abandono de carrito -18%.
- **H&M:** 7M contactos/mes, 65% end-to-end, first-contact 88%.

**Lección Klarna (failure modes a evitar):**
1. Optimizar por **tasa de automatización** sobre calidad — *"67% bien > 90% mal"*.
2. Saltarse fases de revisión humana en el rollout.
3. **Umbrales de confianza muy bajos** → atrapan respuestas malas en el tier automático. (→ umbral **alto**.)
4. Alucinaciones en casos raros; casos sensibles/emocionales mal manejados; riesgo de cumplimiento por
   manejar autónomamente cosas delicadas.
5. Vender "IA reemplaza agentes" en vez de "IA da apalancamiento al agente".

**Balance óptimo:** IA en tier-1 (rutina bien definida); humano en tier-2+ (complejo, ambiguo, sensible).

---

## 8. Síntesis → decisiones de diseño para nuestro sistema

| Hallazgo | Decisión de diseño |
|---|---|
| A quién > cuánto; fríos = baneo | Segmento recién-liquidado/por-liquidar (responden) + solo clientes propios |
| Escalar en señal de compra, same-day | Motor de triaje invertido: señal de compra → humano cierra ya |
| IA recomienda, sistema gobierna + audita | Flag `auto_send` + gobernador + **log de decisiones** por mensaje |
| Contenido semi-armado en lo sensible | Opener/ofertas desde plantillas de mensualidad/enganche aprobadas; charla libre solo en no-sensible |
| Umbral de escalamiento alto (Klarna) | En duda, escala. Nunca autónomo en casos sensibles/deuda |
| Vender mensualidad | Prompt del copiloto habla en enganche + pago mensual |
| Objeciones: 5 tipos, rebatir corto | Prompt de manejo de objeciones (precio→mensualidad; autoridad→incluir pareja) |
| Memoria de primera clase | Estado por conversación; handoff con memoria completa |
| Chip se muere (2-8 sem) | Nunca número del negocio; respaldo; re-pareo rápido |
| Cadencia: tensión 3-toque vs baneo | Demo = **2 toques**; 3 al migrar a API oficial + opt-in |
| Deuda existente = sensible | Reactivación y cobranza separadas; deuda → humano, nunca el bot |

---

## Fuentes

**Baneo / canal no oficial**
- checkleaked — Avoid WhatsApp Ban: Dev Guide — https://whatsapp.checkleaked.cc/blog/avoid-whatsapp-ban
- kraya.ai — WhatsApp Automation Ban Risk 2026 — https://blog.kraya-ai.com/whatsapp-automation-ban-risk
- wadesk — WhatsApp Warm-Up 2026 — https://warmer.wadesk.io/blog/whatsapp-account-warm-up

**Handoff HITL / arquitectura de agente**
- Zylos — Agent-to-Human Handoff Patterns — https://zylos.ai/research/2026-04-03-agent-to-human-handoff-patterns/
- digitalapplied — HITL Escalation Design 2026 — https://www.digitalapplied.com/blog/human-in-the-loop-escalation-design-ai-agents-2026
- buildmvpfast — Confidence-Threshold Handoff — https://www.buildmvpfast.com/blog/agent-handoff-patterns-ai-human-escalation-confidence-threshold-2026
- mem0 — State of AI Agent Memory 2026 — https://mem0.ai/blog/state-of-ai-agent-memory-2026

**Reactivación crédito / WhatsApp México**
- beexcc — Recuperar clientes por WhatsApp — https://blog.beexcc.com/recuperar-clientes-por-whatsapp

**Objeciones / cierre**
- Trillet — Voice Agent Objection Handling — https://www.trillet.ai/blogs/voice-agent-objection-handling

**Personalización gobernable (regulado)**
- CIO — Hyper-personalization with guardrails in regulated industries — https://www.cio.com/article/4177299/hyper-personalization-in-the-age-of-agentic-ai-how-regulated-industries-can-do-it-with-guardrails.html

**Benchmark automotriz (el espejo)**
- Clearline — Dealership Lead Reactivation Playbook 2026 — https://www.useclearline.com/blog/dealership-lead-reactivation-ai-tools-2026

**Benchmark servicio + lección Klarna**
- Twig — Klarna AI Walk-Back — https://www.twig.so/blog/klarna-ai-customer-support-efficiency
- Klarna press — AI handles two-thirds of chats — https://www.klarna.com/international/press/klarna-ai-assistant-handles-two-thirds-of-customer-service-chats-in-its-first-month/
- DigitalDefynd — Sephora / H&M AI case studies — https://digitaldefynd.com/IQ/sephora-using-ai-case-study/

---

_Recopilado jul 2026 para el diseño del módulo `reactivacion` (R7). Actualizar si Meta cambia políticas de
API/detección o si se migra a un BSP oficial._
