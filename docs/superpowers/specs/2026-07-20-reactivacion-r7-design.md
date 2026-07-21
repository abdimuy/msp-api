# Diseño — Módulo `reactivacion` (R7): reactivación de clientes con IA por WhatsApp

> Spec de diseño. Alimentado por la investigación en
> [`docs/ventas-ai-chatbot-research-2026.md`](../../ventas-ai-chatbot-research-2026.md). Fecha: 2026-07-20.
> Estado: **en revisión del usuario** (pre-implementación).

## 1. Contexto y objetivo

La capa de inteligencia del proyecto "vender con IA" está construida (winback, Cliente 360, 3 scores,
cartera, narrativa). **Lo que no existe es la capa que genera dinero: el contacto y la venta.** Este módulo
la construye para una mueblería a crédito.

**Doble propuesta de valor** (ambas son entregables, no solo la primera):
1. **Ingreso incremental** — reactivar clientes dormidos/en-ciclo con mensajes personalizados por IA y **medir
   el enganche recuperado** contra un grupo control. Es el número para la junta con el dueño.
2. **Independencia del buen trato humano** — la calidad de atención deja de depender de qué empleado esté o de
   la rotación. La IA da trato **consistente** siempre; el sistema se vuelve menos dependiente de cualquier
   empleado. Para el dueño esto resuelve un dolor cotidiano (empleados que maltratan al cliente + rotación).

**Métrica de éxito del demo:** **enganche** en Microsip (no venta cerrada — cae en días, no en 90 días),
atribuido treatment vs. control, ventana 1-2 meses.

## 2. Decisiones cerradas (con el usuario)

- **Segmento de arranque:** recién-liquidado y por-liquidar-con-hueco (tibios: te conocen, responden →
  anti-baneo + convierten rápido). NO dormidos profundos primero.
- **Geografía del piloto: SOLO Tehuacán.** El universo se filtra a clientes de Tehuacán (zona/ciudad). Alinea
  con el chip local (LADA 238) y acota el demo.
- **Habla en la parcialidad real, no en "mensualidad":** el pago puede ser **semanal, quincenal o mensual** —
  el enganche y la parcialidad **ya viven en la DB (Microsip)**. La IA lee los términos reales del cliente y le
  habla en *su* cadencia y montos, no en rangos inventados.
- **Canal:** `whatsmeow` (WhatsApp no oficial) con **chip prepago dedicado — nunca el número del negocio** —
  para el demo → API oficial (BSP) después. La API oficial requiere documentos fiscales del dueño (Constancia
  de Situación Fiscal, acta constitutiva, comprobante de domicilio) → es conversación con él, no tarea técnica.
- **Autonomía:** se construye completa; en el demo `auto_send = off` (human-gate) + **triaje en la sombra**
  (la IA decide y registra lo que *habría* hecho autónomo, pero el humano confirma el envío). Se prende sobre
  API oficial.
- **La IA es siempre la pluma:** el humano nunca teclea el mensaje final crudo — ni en el primer contacto ni
  en el cierre. Aporta **decisión/autoridad** (aprobar precio, confirmar existencia); **dicta la intención y la
  IA redacta**. Así el trato consistente se mantiene incluso cuando cierra un empleado malo en trato.
- **Cadencia:** máx **2 toques** en frío para el demo (configurable; el circuit-breaker manda). 3+ al migrar a
  API oficial con opt-in.
- **Escalamiento invertido (ventas ≠ soporte):** se escala en el momento BUENO (señal de compra → humano
  cierra same-day) y en lo sensible (deuda), no solo en el malo.

## 3. Arquitectura — 3 piezas

Módulo nuevo `internal/reactivacion/` (Go, hexagonal, como los demás). Se apoya en lo ya construido
(`internal/analytics/` para candidatos/scores/atribución; `internal/clientes/` para la ficha; el catálogo
Microsip para productos/ventas).

| Pieza | Qué es | Parte de |
|---|---|---|
| **A · Universo + medición** | Segmentos (por-liquidar/recién-liquidado), teléfonos desde `MSP_LOCAL_SALE`, asignación control por hash, atribución por enganche | Reusa/extiende `analytics` (R1 ya tiene lista+control+atribución) |
| **B · Canal WhatsApp** | Adaptador `whatsmeow`: pareo QR, enviar/recibir, **gobernador de envío** (jitter/tope/horario/breaker), flag `auto_send`, resiliencia a baneo | Nuevo, infra aislada; riesgo técnico → spike temprano |
| **C · Copiloto IA + bandeja** | Opener con next-best-product, triaje por mensaje, redactado, modo dictado, capa de gobierno+auditoría, UI de aprobación/conversación | Nuevo (el módulo `reactivacion` que los specs nombran pero nunca se creó) |

**Frontend:** módulo nuevo en `sistema-cobro-web` (bandeja de conversaciones + aprobación + modo dictado),
patrón hexagonal como los demás.

## 4. Máquina de estados del cliente

```
SELECCIONADO ──(hash estable)──► CONTROL   (nunca se contacta; solo se mide el enganche)
     │
     └─► ENCOLADO ──[gobernador: horario+jitter+tope diario]──► CONTACTADO
                                                                    │
                     ┌──────────────────────────────────────────────┤
                     ▼                                               ▼
              SIN_RESPUESTA (48h)                                RESPONDIÓ
                     │                                               │
             1 toque suave (día ~3)                                 ▼
                     │                                        CONVERSANDO ◄────┐
              sigue sin responder                                   │          │
                     ▼                             ┌─────────────────┤          │
                 DESCARTADO                        ▼                 ▼          │
                                         IA responde (sombra:   ESCALADO a     │
                                         propone, humano OK)    humano ────────┘
                                                   │            (briefing estructurado)
                                                   ▼
                                         SEÑAL DE COMPRA
                                                   ▼
                                     INTERESADO/CITA ─► ENGANCHE ✅ ─► CERRADO
```

- Control existe desde el estado 1 y jamás se contacta.
- Máx 2 toques salientes en frío. Sin insistir → protege el chip.
- Reentry de primera clase: al escalar/retomar, la conversación conserva estado y memoria del cliente.

## 5. Motor de decisión por mensaje (triaje)

Por cada mensaje entrante, la IA evalúa y **escala si CUALQUIER señal se dispara** (umbral **alto**; en duda,
escala — lección Klarna). Meta de calibración: escalar **10-20%**.

| Señal | Ejemplo | Acción |
|---|---|---|
| **Señal de compra** ★ | precio, disponibilidad, "me interesa", "cuándo llega" | → Humano (cerrar, same-day) |
| **Deuda existente** (sensible) | "aún debo la sala", reclamo de pago | → Humano; sale del flujo de venta (cobranza es otro canal) |
| Confianza baja (<~65%) | no entiende la intención | → Humano |
| Pide humano explícito | "quiero hablar con una persona" | → Humano (inmediato) |
| Loop / enojo | repite 3×, tono negativo | → Humano |
| Fuera del allowlist (§8) | pide algo no permitido | → Humano (no improvisa) |
| Todo lo demás | saludo, "¿quién habla?", "ahorita no", reagendar | IA responde |

En **modo sombra** (demo): la IA marca su decisión + redacta; el envío final lo confirma el humano. Se registra
"la IA habría acertado el X%" — segundo número para la junta.

## 6. Los dos modos del humano

1. **Aprobar / editar** — la IA ya redactó; el operador da OK, ajusta o reescribe.
2. **Dictar intención → IA redacta** — el operador dice *"ofrécele 12 mensualidades sin enganche"* y la IA lo
   convierte al WhatsApp con el tono correcto.

En ambos, y también en el **cierre escalado**, la IA es la pluma. El humano aporta criterio/autoridad, no
las palabras. **Operador del demo: el usuario mismo desde hoy**, diseñado para que después lo opere cualquiera
sin rehacer nada (nada de línea de comandos; UI simple pero usable).

## 7. Capa de gobierno + auditoría ("IA recomienda, sistema decide")

Todo mensaje saliente pasa por una compuerta de política antes de enviarse:

```
IA propone (mensaje + acción) ─► [POLÍTICA runtime] ─► envía / bloquea / escala
                                   · auto_send on/off (off en chip no-oficial)
                                   · gobernador (§9)
                                   · segmento permitido / no-deuda
                                   · frequency cap por cliente
                                   · allowlist de contenido (§8)
         └──────────────► LOG de decisión (auditable): cliente, mensaje, por qué, señales, resultado
```

Requisito: responder *"¿por qué este cliente recibió este mensaje?"* en minutos. Cubre el riesgo regulatorio
del segmento que aún debe. Reactivación y cobranza son canales separados; el bot **nunca** toca la deuda.

## 8. Allowlist de contenido — el "qué puede hacer" (BORRADOR, requiere tus números)

Rieles explícitos. Todo lo que se salga → **escala, no improvisa** (Klarna: alucinar un precio/fecha es peor
que pedir ayuda). **Los `[POR CONFIRMAR]` los llenas tú con datos reales del negocio.**

### ✅ Puede OFRECER
- Productos/categorías del catálogo Microsip que **tienen existencia confirmada** (la IA consulta stock; sin
  stock → no ofrece ese ítem).
- Next-best-product derivado del historial del cliente + canasta de mercado.
- Planes de pago **leídos de la DB (Microsip)**: la IA usa el **enganche** y la **parcialidad real** del
  cliente (**semanal / quincenal / mensual**) — nunca rangos inventados. Habla en su cadencia y montos reales.
- Promos: **ninguna por ahora.** No ofrece promociones hasta que el usuario cargue una lista de promos
  vigentes. (Mientras tanto, "promo" pedida por el cliente → escala.)

### ✅ Puede AFIRMAR
- "Manejamos crédito / pagos en parcialidades (semanal, quincenal o mensual)."
- **Entrega: 1 a 2 días** (piloto). *(Más adelante: mismo día según la hora — se activa cuando se defina la regla.)*
- "Por tu buen historial de pago tienes [beneficio]" (solo si el dato de historial lo respalda).
- Ubicación/horario de la tienda: `[POR CONFIRMAR: dirección + horario de la tienda de Tehuacán]`.

### ⚠️ Debe ESCALAR (no decide sola)
- Precios/mensualidades fuera del rango estándar; cualquier **descuento**.
- Disponibilidad que no puede confirmar contra stock.
- Compromisos concretos de **fecha o monto** de entrega.
- Cualquier mención de la **deuda existente** del cliente.
- Reclamos, garantías, devoluciones, quejas de servicio.
- Cualquier petición fuera de este allowlist.

### 🚫 NUNCA dice
- Montos, plazos o promos inventados (no pre-cargados).
- Promesas de entrega que no puede cumplir.
- Nada sobre la deuda existente ni presión de cobranza.
- Datos de otros clientes.
- Afirmaciones sobre calidad/garantía no aprobadas.

## 9. Gobernador de envío (anti-baneo)

- **Calentar** el chip 2-4 semanas antes de campaña (se estabiliza ~20-30 días).
- **20-40 msgs/día** semana 1; sube semanal solo si la calidad aguanta; máx ~2/min.
- **Jitter** aleatorio (ej. 90s–8min), nunca fijo. Solo horas hábiles, <6h/día.
- **Circuit-breaker por tasa de respuesta:** si cae del umbral → auto-pausa + alerta.
- Premisa "el chip se muere (2-8 sem)": respaldo de conversaciones + re-pareo rápido; nunca el número del negocio.

## 10. Contenido / prompt del copiloto

- **Vende la parcialidad, no el precio** — enganche + pago en la cadencia real del cliente (semanal /
  quincenal / mensual), leídos de la DB. Ej. "con $X de enganche y $Y a la semana".
- **Opener por segmento:** recién-liquidado = felicitar + siguiente compra con beneficio; por-liquidar =
  completar el juego con pago que cabe, con tacto (no lidera con "compra más").
- **Objeciones (5 tipos):** precio→reframe a la parcialidad (semanal/quincenal/mensual); autoridad ("le
  pregunto a mi esposo/a")→incluir al que decide; tiempo→siguiente paso chico. Respuestas **cortas**, tono cercano.
- **Memoria por cliente:** nada de "¿quién eres?" en el segundo mensaje.

## 11. Medición

- Asignación treatment/control por **hash estable** del cliente (reproducible, sin sesgo). El control **nunca**
  se contacta.
- Éxito = **enganche** en Microsip, atribuido treatment vs. control en la ventana (1-2 meses).
- Secundarias (para la junta): tasa de respuesta, % de turnos que la IA resolvió sola en sombra, citas,
  interesados, y el "trato consistente" como narrativa de valor.

## 12. Fases de implementación (para el plan)

1. **Fase 1 — Pieza A:** segmentos + teléfonos (`MSP_LOCAL_SALE`) + control + atribución por enganche. Reusa
   `analytics`. Barata, desbloquea todo. **Empezar aquí.**
2. **Fase 2 — Spike de B:** validar `whatsmeow` (pareo QR, enviar/recibir) en el Mac con un chip de prueba +
   el gobernador. Quita el riesgo técnico antes de invertir en C.
3. **Fase 3 — Pieza B completa + Pieza C:** canal productivo + copiloto (opener/NBP, triaje, dictado,
   gobierno+auditoría) + UI de bandeja. Modo sombra.

Cada fase = su propio plan de implementación.

## 13. Fuera de alcance (YAGNI del demo)

- Envío autónomo real (queda para API oficial).
- Multi-canal (SMS/email/voz): solo WhatsApp.
- 3er+ toque de cadencia (hasta API oficial + opt-in).
- Palomita verde / cuenta oficial de WhatsApp.
- Cobranza (canal separado; el bot nunca la toca).
- Materialización completa del "cierre" automatizado (el humano cierra en el demo).

## 14. Riesgos

- **Baneo del chip** (2-8 sem inevitable): mitigado por §9 + nunca-el-número-del-negocio + re-pareo.
- **Calidad de teléfonos:** `MSP_LOCAL_SALE.TELEFONO` sube cobertura de ~51% a ~85%, pero hay que validar formato.
- **Regulación de cobranza:** mitigado por separación estricta reactivación/deuda (§7, §8).
- **Privacidad LLM:** si se usa un LLM hosted, no mandar datos sensibles del cliente sin revisar términos
  (el free tier de Gemini entrena con lo enviado — ver runbook §7). Definir proveedor antes de Fase 3.
- **Dependencia de un operador:** mitigado porque la IA es la pluma (cualquiera puede operar).

## 15. Referencias

- Investigación: [`docs/ventas-ai-chatbot-research-2026.md`](../../ventas-ai-chatbot-research-2026.md)
- Estrategia: [`docs/ventas-ai-estrategia.md`](../../ventas-ai-estrategia.md)
- Specs previos (a actualizar — algunos citan Postgres/SQLite pre-ADR-0008):
  `2026-06-06-ventas-ai-winback-system-design.md`, `2026-06-13-sistema-inteligencia-cliente-negocio-design.md`
