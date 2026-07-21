# Diseño — Reactivación R7 · Fase 3: Copiloto IA + Canal WhatsApp + Bandeja

> Continúa el diseño aprobado en [`2026-07-20-reactivacion-r7-design.md`](2026-07-20-reactivacion-r7-design.md)
> (spec padre) y en [`2026-07-20-reactivacion-r7-fase2-canal-design.md`](2026-07-20-reactivacion-r7-fase2-canal-design.md).
> Fase 1 (universo + cohorte + atribución) y Fase 2 (canal con enviador falso + gobernador) están mergeadas a
> `main` local. Esta fase construye **el cerebro (copiloto IA), el canal real de WhatsApp, y la bandeja del
> operador**. Diseñada completa a pedido del usuario, pero se **construye por etapas** (§4).

## 1. Contexto y objetivo

Con Fase 1+2 ya tenemos a quién contactar, la medición, y la maquinaria de envío (con enviador falso). Falta
lo que hace que esto **venda de verdad y se pueda operar**: (a) un copiloto de IA que lee las respuestas de
los clientes, decide si contesta o escala, y redacta; (b) el canal real de WhatsApp para enviar/recibir en
vivo; (c) una bandeja para que un humano opere en **modo sombra** (la IA redacta, el humano confirma).

Todo bajo el principio de industria regulada: **"la IA recomienda; el sistema gobierna y decide"**, con log
auditable por decisión. Reactivación y cobranza son canales **estrictamente separados**.

## 2. Decisiones cerradas (con el usuario)

1. **Columna de seguridad:** IA propone (intención+acción+borrador) → capa de política determinista en runtime
   (segmento permitido, no-deuda, frequency cap, gobernador, allowlist, `auto_send`, umbral de escalamiento) →
   envía/propone/escala → **log de decisión auditable**. Contenido **semi-armado** en lo sensible (módulos
   pre-aprobados que la IA combina); charla libre solo en lo no sensible.
2. **Deuda = intocable.** Cualquier mención de deuda/pago → señal sensible → escala a humano; el bot nunca la
   toca (regulación mexicana de cobranza; canales separados).
3. **Info de crédito:** el bot **afirma el estado de completado en positivo** ("ya terminó de pagar" / "ya casi
   termina" — es el gancho de venta), pero **NUNCA da cifras** de saldo/pagos pendientes. Si el cliente
   pregunta cuánto debe → escala a humano (evita cifra equivocada por caché viejo, info sensible por canal no
   seguro, y difuminar la frontera). Los únicos montos que maneja son los de la **nueva** compra (enganche +
   parcialidad reales, leídos de la DB, nunca inventados).
4. **Triaje:** escala si CUALQUIER señal se dispara (umbral **alto**; en duda, escala). Meta 10–20%. Señal de
   compra → humano cierra same-day. Confianza binaria (Alta/Baja) al operador; el % vive en detalle.
5. **Modo sombra** (arranque/demo): la IA decide+redacta, el **humano confirma el envío final**. Se registra
   "la IA habría acertado el N%".
6. **Briefing estructurado** al escalar (no chat crudo): intención · confianza · sentimiento · entidades ·
   qué hizo la IA · restricciones · **siguiente paso sugerido** · referencia al historial.
7. **Memoria de primera clase:** hilo + estado en el flujo + hechos conocidos + resumen para el LLM; viaja al
   escalar/retomar.
8. **La nota del cobrador (`CLIENTES.NOTAS`) se lee antes de vender.** 94% del universo (6,323/6,721) tiene
   nota; 43% con 200+ chars. Se **destila en contexto privado + banderas** para el humano (reusando la infra
   de narrativa); **informa el tono, nunca se cita al cliente** (mucho es negativo/sensible: "maleta", divorcio,
   cuentas compartidas).
9. **LLM intercambiable en caliente:** el proveedor de IA vive detrás de un puerto (arquitectura hexagonal),
   se cambia por config (env). Reusa el cliente OpenAI-compat de `internal/platform/llm` (el de narrativa).
   Proveedor concreto + revisión de privacidad = decisión de despliegue.
10. **Canal real:** whatsmeow; **el chip se muere en 2–8 semanas** → diseñar para eso (nunca el número del
    negocio; conversaciones en NUESTRA DB; re-pareo rápido). La **tasa de respuesta** es el salvavidas
    (circuit-breaker). La mayor bandera de baneo es *a quién* escribes (extraños), no cuántos.
11. **Bandeja:** dirección visual **state-of-the-art 2026** (grises reales en capas, Inter, acento **violeta
    exclusivo para IA**, azul para la acción humana, 3 semánticos). Layout: **inbox de 3 paneles con lógica de
    cola de revisión** (las que te necesitan flotan arriba; anti "sello de goma"). Aprobar = un clic.
    Confianza binaria. "Por qué" + chips de evidencia. Solo backend+web; sin app móvil.

## 3. Descomposición de la fase

| Pieza | Qué | Depende de |
|---|---|---|
| **3a · Copiloto (backend)** | Recibir (simulado o real), triaje, redactar, memoria/estado de conversación, log de decisión, allowlist, LLM tras puerto | — (constrúible sin número, con inbound simulado) |
| **3b · Canal real WhatsApp** | Adaptador whatsmeow: pareo QR, enviar (reusa `MessageSender` de Fase 2) + **recibir**, sesión persistente, re-pareo | El número/chip |
| **3c · Bandeja (frontend)** | Módulo nuevo en `sistema-cobro-web`: inbox 3 paneles, borrador IA, aprobar/editar/dictar, briefing de escalada, ficha con nota | La API de 3a |

**Orden de construcción recomendado (aunque el diseño es completo):** **3a → 3c → 3b.** 3a es el cerebro y
todo depende de él; se prueba con inbound simulado (un endpoint que inyecta un mensaje entrante). 3c lo hace
operable/demostrable con el canal falso de Fase 2. 3b se enchufa cuando llegue el chip (el `MessageSender` ya
existe; solo se agrega la recepción). Cada pieza puede ser su propio plan de implementación.

## 4. Arquitectura de seguridad (el cerebro gobernado)

```
mensaje entrante ─► COPILOTO (LLM tras puerto) propone: intención + acción + borrador
                         │
                         ▼
                 CAPA DE POLÍTICA (runtime, determinista)
                   · segmento permitido · NO-deuda · frequency cap
                   · gobernador (Fase 2) · allowlist (§8) · auto_send · umbral escalamiento
                         │
              ┌──────────┴───────────┐
              ▼                       ▼
       pasa → propone al         escala a HUMANO
       humano (modo sombra)      (briefing estructurado §6)
                         │
                         ▼
              LOG DE DECISIÓN (auditable: cliente, mensaje, acción, señales, módulos, resultado)
```

Requisito regulatorio: responder *"¿por qué este cliente recibió este mensaje?"* en minutos, con identidad,
permiso, propósito, módulos usados y razonamiento. El log es la evidencia.

## 5. Motor de triaje (cuándo escala)

Por cada mensaje entrante, el copiloto clasifica y **escala si CUALQUIER señal se dispara** (umbral alto; meta
10–20% de turnos escalados — <10% arriesgado, >20% cuello de botella / "sello de goma").

| Señal | Ejemplo | Acción |
|---|---|---|
| 🟢 Señal de compra ★ | "me interesa", "cuánto", "cuándo llega" | → Humano (cerrar, same-day) |
| 🔴 Deuda / pago | "aún debo", reclamo, "¿cuánto debo?" | → Humano; sale del flujo de venta |
| 🟡 Confianza baja (<~65%) | no entiende la intención | → Humano |
| 🟡 Pide humano | "quiero hablar con una persona" | → Humano (inmediato) |
| 🟡 Enojo / loop (3×) | tono negativo, repite | → Humano |
| 🟡 Fuera del allowlist | pide algo no permitido | → Humano (no improvisa) |
| ⚪ Todo lo demás | saludo, "ahorita no", reagendar | IA responde |

**Modo sombra:** la IA decide + redacta; el envío final lo confirma el humano. Se registra "habría acertado
N%" (tablero de confianza, no compuerta). Confianza **binaria** al operador (Alta/Baja; sólido vs. punteado),
% en el detalle. Ruteo interno: >90% alta · 70–90% zona de revisión · <70% escala.

**Briefing estructurado** al escalar (transferir el *estado de trabajo*, no la conversación): `intención,
confianza, sentimiento, entidades, qué_hizo_la_IA, restricciones, siguiente_paso_sugerido,
referencia_al_historial`. Modos: *warm* (delicado/VIP) vs *cold* (rápido). Cadena de handoffs ≤4.

## 6. Memoria y estado de conversación

- **Hilo** completo (entrantes + salientes, ordenados), en la DB.
- **Estado en el flujo:** `contactado → respondió → conversando → escalado → interesado → enganche` (o
  `descartado`). Reentry de primera clase: al escalar/retomar, estado y memoria viajan.
- **Hechos conocidos** (nombre, segmento, última compra, next-best-product, oferta hecha) — evita "¿quién
  eres?" y contradicciones.
- **Resumen para el LLM:** no el chat crudo (confunde + cuesta tokens); resumen estructurado + últimos turnos.
  El historial completo queda para auditoría.

## 7. Contenido del copiloto + uso de la nota

**Cómo vende:**
- **Vende la parcialidad, no el precio:** "con $X de enganche y $Y a la semana" — enganche + cadencia reales
  del cliente (semanal/quincenal/mensual) leídos de la DB, nunca inventados.
- **Opener por segmento:** recién-liquidado = felicita + siguiente compra con beneficio; por-liquidar =
  completar el juego con un pago que cabe, con tacto.
- **Objeciones (5 tipos), respuestas cortas:** precio → reencuadra a la parcialidad; autoridad ("le pregunto a
  mi esposo/a") → incluir a quien decide; tiempo → siguiente paso chico.
- **Semi-armado:** ofertas/montos/disclosures salen de módulos pre-aprobados que la IA combina; charla libre
  solo en lo no sensible.

**Uso de la nota del cobrador (`CLIENTES.NOTAS`):**
1. Se lee como **contexto PRIVADO** (infra de narrativa: decodifica Win1252→UTF-8, NFC, cap ~800 runas).
2. Se **destila en señales seguras + banderas** (cadencia/situación de pago, "trabaja fuera", "cuenta
   compartida", riesgo). Se guarda el resumen gobernado, no el texto crudo negativo.
3. **Informa el tono, no las palabras** — nunca se cita al cliente.
4. **Levanta banderas para el humano** (cuenta compartida, situación delicada, riesgo de pago) en la ficha y el
   briefing → revisión antes de ofrecer crédito nuevo.

## 8. Allowlist de contenido (los rieles)

Todo lo que se salga → **escala, no improvisa.** Los valores concretos (`[POR CONFIRMAR]`) los define el
usuario con datos reales del negocio.

- **✅ Puede OFRECER:** productos/categorías del catálogo Microsip **con existencia confirmada** (consulta
  stock; sin stock no ofrece); next-best-product del historial; planes de pago leídos de la DB (enganche +
  parcialidad real). Precio/plan de la **nueva** compra según reglas `[POR CONFIRMAR]` (múltiplos de $50, piso
  de precio, ancla en el pago del cliente — ver spec padre §8 / commits de reglas de precio).
- **✅ Puede AFIRMAR:** estado de completado en positivo ("ya terminó de pagar"); disponibilidad confirmada.
- **⚠️ Debe ESCALAR:** señal de compra; **cualquier cifra de deuda/saldo/pagos pendientes**; petición de humano;
  fuera del allowlist; baja confianza; enojo/loop.
- **🚫 NUNCA dice:** cifras de deuda existente; precios/fechas inventados; nada de cobranza; inferencias
  sensibles (estrés financiero) sin base; citar la nota.

## 9. Canal real de WhatsApp (3b)

- **whatsmeow:** pareo por QR (cuenta real), **enviar** (implementa el `MessageSender` de Fase 2 — cero
  reescritura) **y recibir** (nuevo: inbound alimenta al copiloto). Sesión persistente para sobrevivir
  reinicios.
- **El chip se muere (2–8 sem), es cuándo no si:** número **dedicado desechable, nunca el del negocio**. Las
  **conversaciones viven en nuestra DB** (no en el chip) → re-parear un chip nuevo retoma todo.
- **Anti-baneo:** la mayor señal es *a quién* escribes (extraños que no te tienen guardado). La **tasa de
  respuesta** es reina: el **circuit-breaker** del gobernador auto-pausa si cae del umbral. La personalización
  de la IA (cada mensaje distinto) es ventaja anti-detección. Calentar el chip 2–4 semanas. `auto_send` arranca
  **pausado-gobernado**; "instantáneo" jamás en chip no-oficial.
- **Alertas:** breaker disparado o chip caído → alerta para actuar rápido (re-pareo).

## 10. LLM tras puerto (intercambiable)

El copiloto llama a un puerto `outbound` (p. ej. `CopilotoLLM`), implementado sobre el cliente OpenAI-compat de
`internal/platform/llm` (el que ya usa narrativa). Proveedor y modelo por env (`LLM_BASE_URL`, `LLM_MODEL`,
`LLM_API_KEY`). Cambiar de proveedor = variable de entorno, cero reescritura. **Privacidad:** minimización
(señales derivadas, no datos crudos sensibles); revisar términos del proveedor antes de mandar datos del
cliente (el free tier de algunos entrena con lo enviado). Proveedor concreto = decisión de despliegue.

## 11. Modelo de datos (tablas nuevas `MSP_RX_*`)

Reusa Fase 1 (`MSP_RX_COHORTE`) y Fase 2 (`MSP_RX_MENSAJES`, salientes). Agrega (columnas exactas en el plan;
IDs/timestamps desde Go, UTF-8, sin lógica en DB):

- **`MSP_RX_CONVERSACION`** — una por cliente: `estado` (flujo §6), `asignado_a` (humano al escalar),
  `resumen_memoria` (para el LLM), `contexto_nota` + `banderas` (destilado gobernado de la nota, reusa/enlaza
  narrativa), timestamps de actividad.
- **Turnos entrantes** — extiende el registro de mensajes para incluir **inbound** (dirección, autor
  cliente/ia/humano, cuerpo, hora), enlazados a la conversación. (Decisión de tabla — extender `MSP_RX_MENSAJES`
  con `DIRECCION/AUTOR/CONVERSACION_ID` vs. tabla `MSP_RX_TURNO` aparte — se cierra en el plan; preferencia:
  tabla de turnos unificada por conversación, con los salientes enlazando al registro de envío de Fase 2.)
- **`MSP_RX_DECISION`** — log auditable por decisión: `mensaje_entrante_ref`, `intencion`, `confianza`,
  `senales` (JSON), `accion_propuesta` (responder/escalar/ofrecer), `modulos_allowlist`, `resultado`
  (enviado/aprobado/editado/escalado), y en modo sombra `ia_habria` vs `humano_hizo`. Es el "por qué".

## 12. La bandeja (3c) — dirección visual

Módulo nuevo en `sistema-cobro-web` (hexagonal, como los demás). **Inbox de 3 paneles con lógica de cola de
revisión:**

- **Izquierda — cola:** conversaciones; **"Te necesitan"** arriba (señal de compra, escalada, confianza baja),
  "Al día" abajo. Anti sello-de-goma. Chips de segmento; banderas semánticas.
- **Centro — conversación + compositor:** hilo (cliente=neutro izq., humano=azul der.). El **borrador de la IA**
  vive en el compositor: barra/brillo **violeta**, "✦ Borrador de la IA", **confianza binaria** (Alta sólido /
  Baja punteado; % en hover), tira **"Por qué" + chips de evidencia**. Acciones: **Aprobar y enviar (1 clic /
  Tab, el botón más grande)** · Editar (inline) · 🎙 Dictar · Escalar. Al aprobar, el violeta "se drena" → burbuja
  enviada normal (un humano ya es el dueño).
- **Derecha — ficha:** identidad + chips de estado + **nota destilada** (contexto privado, banderas) + banda
  "La IA recomienda" (acción, confianza, banda, allowlist ✓).
- **Escalada:** en vez del borrador, el **briefing estructurado** (ámbar, no violeta): intención/sentimiento/
  por qué escaló/confianza/entidades/qué hizo la IA + siguiente paso sugerido + SLA; acciones ricas (tomar,
  dictar, enviar a cobranza, ver conversación ↗).

**Design system (tokens, dark + light):** grises reales en capas (`--bg #0B0D12 … --surface-3 #232834`,
bordes `#272B36`), texto `#E8EAED`/`#9BA1AD`, acento acción `#3B82F6`, **acento IA `#A78BFA/#7C3AED`
(exclusivo)**, semánticos `#34D399/#FBBF24/#F87171`. Tipografía **Inter** (números tabulares en métricas).
Radios 6/10/14. Elevación por capas de luminosidad, sombras solo en overlays. Modo claro definido en paralelo.
Mockods de referencia: `.superpowers/brainstorm/.../bandeja-sota.html` y `bandeja-escalada.html`.

## 13. Medición

Igual que Fase 1: treatment (contactado) vs control (nunca contactado), enganche en Microsip. Secundarias:
tasa de respuesta, % de turnos que la IA resolvió sola en sombra ("habría acertado N%"), interesados, citas.

## 14. Arquitectura técnica

Todo en `internal/reactivacion/` (backend, hexagonal, como Fase 1/2) + módulo nuevo en `sistema-cobro-web`
(frontend). Puertos nuevos: `CopilotoLLM` (LLM), `ConversacionRepo`, `DecisionRepo`, y para 3b un
`InboundReceiver` (whatsmeow) que empuja mensajes al copiloto; el `MessageSender` de Fase 2 gana la
implementación `WhatsmeowSender` real. La capa de política (triaje + allowlist + gobernador) es app, lógica
pura y testeable. LLM y canal tras puertos → intercambiables por config.

## 15. Fases de implementación (para los planes)

1. **3a — Copiloto backend** (sin número): puertos LLM/conversación/decisión, motor de triaje + allowlist +
   política, memoria/estado, log de decisión, endpoint de inbound simulado, modo sombra. Reusa narrativa para
   la nota. **Empezar aquí.**
2. **3c — Bandeja** (frontend): consume la API de 3a; opera el modo sombra con el canal falso de Fase 2.
3. **3b — Canal real:** `WhatsmeowSender` + recepción + pareo/re-pareo + sesión persistente. Gated por el chip.

Cada una = su propio spec/plan de implementación.

## 16. Fuera de alcance

- Envío autónomo real sin humano (queda para API oficial + más confianza).
- Multi-canal (SMS/email/voz): solo WhatsApp.
- 3er+ toque de cadencia (hasta API oficial + opt-in).
- Cuenta oficial / palomita verde de WhatsApp.
- Cobranza (canal separado; el bot nunca la toca).
- App móvil de la bandeja (solo web).

## 17. Riesgos

- **Baneo del chip** (2–8 sem): §9 (nunca el número del negocio, conversaciones en DB, re-pareo, breaker por
  respuesta).
- **Privacidad LLM:** revisar términos del proveedor antes de mandar datos; minimización; definir proveedor
  antes de producción.
- **Alucinación de cifras:** semi-armado + montos solo de DB + "cifras de deuda nunca"; en duda, escala.
- **Sobre/sub-escalamiento:** calibrar a 10–20%; modo sombra mide antes de soltar la rienda.
- **Fatiga del revisor:** cola anti sello-de-goma (ordena lo incierto arriba, tag de razón al editar/rechazar,
  agreement por ítem, log de decisiones).
- **Calidad de la nota:** texto libre, fechado, mezcla info sensible → destilar a señales gobernadas, nunca
  crudo al LLM ni al cliente.

## 18. Referencias

- Spec padre: [`2026-07-20-reactivacion-r7-design.md`](2026-07-20-reactivacion-r7-design.md)
- Fase 2: [`2026-07-20-reactivacion-r7-fase2-canal-design.md`](2026-07-20-reactivacion-r7-fase2-canal-design.md)
- Investigación del canal/copiloto: [`../../ventas-ai-chatbot-research-2026.md`](../../ventas-ai-chatbot-research-2026.md)
- UI/UX SOTA 2026 (investigado esta sesión): Intercom Fin Copilot; Sierra/Decagon/Cresta agent-assist;
  Fuselab (confianza binaria vs numérica); Mavik Labs / Velt / Edilec (colas HITL, 5 elementos, anti
  sello-de-goma, logging de decisión); AY Design / Updivision (dark mode "true-grey" en capas, acento violeta
  para IA). Mockups: `.superpowers/brainstorm/*/bandeja-sota.html`, `bandeja-escalada.html`.
- Datos verificados en dev: notas del cobrador 6,323/6,721 (94%) con nota, 2,900 (43%) con 200+ chars.
