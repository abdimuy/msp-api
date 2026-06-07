# Diseño — Sistema de ventas/winback con IA (reactivación de dormidos)

> Spec de diseño. Reactivación de clientes leales dormidos por WhatsApp con IA, con
> grupo de control para atribución. El objetivo es generar **venta incremental medible
> y atribuible al software**, base de la compensación del desarrollador (comisión sobre
> venta nueva). Complementa la estrategia y números en
> [`ventas-ai-estrategia.md`](../../ventas-ai-estrategia.md) (doc maestro privado).
>
> Fecha: 2026-06-06.

## 1. Contexto y objetivo

El negocio revende a clientes que ya compraron (recompra ~47% de por vida, ciclo ~11
meses). Existe un proceso manual ("hojas" de clientes buenos para visitar) que **captura
solo ~1 de cada 5 clientes elegibles al año** — el hueco es la oportunidad.

**Datos verificados (copia Microsip, read-only, 2025):**
- Margen bruto real **52.8%** (no 54.4%); ticket ~$7,100-7,322 s/IVA.
- Neto del dueño por venta winback (IVA contado como su margen, comisión $285):
  **~$2,990-3,070** (con castigo bajo del perfil probado).
- Hueco de clientes buenos sin comprar: Tehuacán 7,603 (3,485 contactables);
  red 26,651 (11,114 contactables). Dormido-fresco contactable: 1,684 / 4,922.
- **Riesgo de impago del perfil objetivo** (público general, 4+ compras, récord limpio):
  **~1.1%/año** — 5-6x más seguro que un cliente nuevo (~6%).

**Objetivo del sistema:** generar venta incremental, cerrarla (handoff en el demo,
híbrido a escala) y **probar la atribución contra un grupo de control**.

## 2. Decisiones de alcance (cerradas)

| Decisión | Resolución |
|---|---|
| **Frontera de cierre** | End-state **C (híbrido)**: full-AI donde se pueda, handoff donde convenga. **Demo = A (handoff)** a un cerrador humano (papá del dev, cobrador). |
| **Canales** | **B**: WhatsApp ahora (whatsmeow), arquitectura multicanal lista (puerto `Canal`) para enchufar **voz y SMS** después sin rehacer el motor. |
| **2ª palanca (cobranza/OXXO)** | **Fuera de alcance** de este sistema (proyecto/spec aparte, Fase 2-3), pero **fundaciones reusables** (read-model, `Canal`, motor de conversación). |
| **Robustez** | **Robusto desde el inicio** en lo que impacta dinero, conversión/alcance y aceptación; **lean** en ops/cosméticos. "Fundación robusta, superficie lean". |

## 3. Arquitectura

### 3.1 Demo (ahora) — servicio standalone

- **Separado de `msp-api`** — cero riesgo al API que mueve el 78% del efectivo; iteración
  a máxima velocidad.
- **Binario Go** con `whatsmeow` (número dedicado y calentado) + motor de conversación IA.
- **Store: SQLite** (embebido, cero infra). Datos propios del winback.
- **Fuente de datos: Firebird (Microsip), solo lectura** — pool de dormidos, historial,
  teléfonos, y ventas nuevas para atribución. **Nunca se escribe en Firebird.**
- Deploy preferido: VPS (siempre arriba, baja latencia a WhatsApp/LLM), con acceso de
  lectura a la copia Microsip.

### 3.2 Producción (post-demo) — híbrido en `msp-api`

- **Datos y lógica → módulo `internal/reactivacion/`** (vertical slice, convenciones
  `CLAUDE.md`): read-model de dormidos derivado de Firebird, tablas de experimento/
  contacto/atribución en **Postgres** (dummy store, IDs/timestamps en Go con
  `uuid.New()`/`time.Now()`, UTC RFC3339, UTF-8/NFC), contracts hacia clientes/ventas.
- **Bot siempre-encendido → binario separado `cmd/winback/`** (monorepo, comparte
  `internal/platform` y contracts), desplegado como **2º servicio nssm**. Aísla la
  conexión WhatsApp/LLM e iteración diaria del API core.

### 3.3 Puertos clave (ports & adapters)

- **`Canal`** — enviar/recibir mensajes. Adaptadores: `whatsmeow` (demo), `cloud-api`
  (escala), `voz`, `sms` (fast-follow). El motor de conversación no conoce el transporte.
- **`ModeloIA`** — generación/clasificación. Adaptador: Anthropic (Sonnet demo, Haiku +
  prompt caching a escala).
- **`FuenteMicrosip`** (read-only) — pool de dormidos, historial, ventas nuevas.
- **`Materializador`** (solo escala/C) — crea la venta en Microsip vía el spec existente
  `2026-05-22-aplicar-venta-local-microsip-design.md`.

## 4. Landscape de features (etiquetado por impacto en paga + demo/escala)

Leyenda paga: **★★★** define la paga (genera/prueba ingreso o te hace irreemplazable) ·
**★★** sube la paga · **★** soporte/higiene. `D` = demo · `S` = escala.

### Etapa 1 — Targeting / Segmentación
| Feature | Paga | D/S |
|---|---|---|
| Read-model de clientes desde Firebird (dormancia, saldo, #compras, historial, teléfono) | ★ | D |
| Filtro de elegibilidad (público gral, sin castigo, contactable) | ★ | D |
| Segmentación por riesgo (4+ compras limpio ≈ 1.1% impago) | ★★ | D |
| Propensity scoring (probabilidad de conversión) | ★★ | S |
| Next-best-product por cliente (de su historial) | ★★★ | D→S |
| Lista priorizada (quién/qué oferta/qué ángulo) | ★★ | D |
| Asignación de grupo de control (holdout aleatorio) | ★★★ | D |

### Etapa 2 — Conversación / Outreach
| Feature | Paga | D/S |
|---|---|---|
| Envío/recepción vía puerto `Canal` (WhatsApp ahora) | ★ | D |
| Apertura hiper-personalizada (relación + historial) | ★★★ | D |
| Motor IA: intención + objeciones + objeción latente | ★★★ | D→S |
| Speed-to-lead: respuesta instantánea 24/7 | ★★★ | D |
| Pre-aprobación de crédito en la conversación | ★★ | D→S |
| Cadencia de seguimiento (día 0/4/10, auto-stop) | ★ | D |
| Control de tono (respetuoso, español profesional) | ★ | D |
| Video en el chat (producto/personalizado) | ★★ | S |
| Catálogo + Flows de WhatsApp | ★★ | S |
| Opt-out / cumplimiento (STOP, declarar IA, PROFECO) | ★ | D |

### Etapa 3 — Handoff / Cierre
| Feature | Paga | D/S |
|---|---|---|
| Disparadores de handoff (intención, frustración, confianza) | ★★ | D |
| Contexto en el mismo hilo (el cliente nunca repite) | ★★ | D |
| Vista del cerrador (qué quiere, historial, términos) | ★★ | D→S |
| Registro de tipo de cierre (full-AI vs handoff) | ★★★ | D |
| Cierre full-AI + materializar venta en Microsip | ★★★ | S |

### Etapa 4 — Aprendizaje / Optimización
| Feature | Paga | D/S |
|---|---|---|
| Infra de variantes de mensaje + medición (iterar a diario) | ★★ | D |
| A/B automático auto-optimizante (ML) | ★★★ | S |
| Entrenar con tus propios mejores cierres/datos | ★★★ | S |
| Aprendizaje continuo por resultado | ★★ | S |

### Etapa 5 — Medición / Atribución
| Feature | Paga | D/S |
|---|---|---|
| Medición de lift (tratamiento vs control) | ★★★ | D |
| Atribución de venta incremental ($ vs control) | ★★★ | D |
| Tracking de comisión ahorrada (cierres full-AI) | ★★ | S |
| Dashboard / reporte (ventas, lift, $ por segmento, vs control) | ★★★ | D→S |

### Etapa 6 — Orquestación / Ops
| Feature | Paga | D/S |
|---|---|---|
| Programación de oleadas/campañas | ★ | D→S |
| Refresco del pool (auto-detecta nuevos dormidos) | ★★ | S |
| Rate limiting / manejo de tiers / anti-baneo | ★ | D→S |
| Monitoreo / alertas (salud del bot, baneo) | ★ | S |
| Guardarraíles de capacidad (no sobre-vender entrega/crédito) | ★★ | S |

## 5. Corte del demo robusto

**Robusto desde el inicio** (donde paga/convierte/prueba):
- **Dinero:** next-best-product por cliente · apertura hiper-personalizada · motor de
  objeciones · speed-to-lead 24/7 · pre-aprobación de crédito en chat · targeting por riesgo.
- **Más clientes:** arquitectura de canal (`Canal`) · read-model + segmentación que amplía
  el abanico · infra de variantes de mensaje para iterar a diario.
- **Aceptación:** grupo de control riguroso · medición de lift + atribución airtight ·
  dashboard que cuenta la historia · registro de tipo de cierre.

**Deliberadamente lean** (para que shippee): monitoreo/alertas básicos · tiers básico ·
**sin** video/catálogo/Flows · **sin** cierre full-AI/materializar (demo = handoff) ·
**sin** voz/SMS construidos (arquitectura lista) · **sin** auto-optimización ML.

## 5.1 Priorización por valor (Dinero / Aceptación / Esfuerzo)

> Cada feature puntuada 0–5: **Dinero** = cuánto sube el ingreso/conversión; **Aceptación**
> = cuánto ayuda a que el dueño y el hermano DBA digan sí (prueba, riesgo, framing).
> **Build** = esfuerzo. Son **clusters distintos**: las que ganan dinero no son las que
> consiguen el sí — el demo robusto necesita ambos.

| Feature (demo) | 💰 Dinero | 🤝 Aceptación | Build |
|---|---|---|---|
| Next-best-product | █████ | ███░░ | ◆◆◆ |
| Apertura hiper-personalizada | █████ | ███░░ | ◆◆ |
| Motor de objeciones | █████ | ███░░ | ◆◆◆◆ |
| Atribución incremental | ███░░ | █████ | ◆◆ |
| Speed-to-lead 24/7 | █████ | ██░░░ | ◆◆◆ |
| Pre-aprobación de crédito en chat | ████░ | ███░░ | ◆◆◆ |
| Targeting por riesgo (4+ compras) | ███░░ | ████░ | ◆◆ |
| Grupo de control | ██░░░ | █████ | ◆◆ |
| Medición de lift | ██░░░ | █████ | ◆◆ |
| Registro tipo de cierre | ███░░ | ████░ | ◆ |
| Dashboard | ██░░░ | █████ | ◆◆◆ |
| Infra de variantes (A/B manual) | ████░ | ██░░░ | ◆◆ |
| Lista priorizada | ███░░ | ██░░░ | ◆◆ |
| Disparadores de handoff | ███░░ | ██░░░ | ◆◆ |
| Read-model de dormidos | ██░░░ | ██░░░ | ◆◆◆ |

**Orden de build:**
- **🟢 Quick wins (alto valor, bajo build → primero):** apertura personalizada · grupo de
  control · atribución · medición de lift · targeting por riesgo · registro de tipo de cierre.
- **🔵 Apuestas grandes (alto valor, más build → el grueso del tiempo):** next-best-product
  · motor de objeciones · speed-to-lead · dashboard · pre-aprobación de crédito.
- **⚪ Soporte (necesario, bajo valor directo):** read-model · lista priorizada · cadencia
  · disparadores de handoff.

**Suben la paga después (escala, alto valor + alto build):** cierre full-AI + materializar
en Microsip · A/B auto-optimizante (ML) · entrenar con tus propios datos · voz con IA.

## 6. Diseño del experimento (demo)

- **Pool:** intersección **dormido-fresco × 4+ compras × contactable** (bajo riesgo + alta
  conversión). Medir el tamaño exacto antes de fijar; arrancar por Tehuacán.
- **Asignación:** determinística/reproducible — `MOD(CLIENTE_ID × 2654435761, 100)`,
  `<60 → tratamiento`, `≥60 → control`. Verificar balance por segmento/valor.
- **Tamaño:** ~**500 tratamiento (contactados)** + ~**350 control (no se tocan)** = ~850.
  Escalable a 600/400. Poder suficiente para detectar lift ≥4-6 pp al 95%.
- **Intervención:** msg día 0 (personalizado) + recordatorios día 4 y 10 (auto-stop);
  ritmo humano, volumen bajo (riesgo de baneo bajo en clientes leales).
- **Cierre:** handoff al cerrador humano; registrar full-AI vs handoff.
- **Métrica primaria:** conversión incremental = tasa(tratamiento) − tasa(control), IC 95%.
- **Ventana:** 60-90 días desde el primer contacto.
- **Resultado esperado** (conversión ~8% supuesta): ~40 ventas tratamiento, ~30
  incrementales atribuibles, ~$215-285k en ventas (la "muestra gratis").

## 7. Modelo de datos (demo, SQLite)

> IDs y timestamps generados en Go (`uuid.New()`, `time.Now()` UTC). Texto NFC/UTF-8.
> Teléfonos E.164 (+52). Lógica en Go, no en la DB.

- **`experimento`** — `cliente_id` (Microsip), `brazo`, `cohorte`, `hash_seed`,
  `segmento`, `num_compras`, `riesgo`, `telefono_e164`, `asignado_at`.
- **`contacto`** — `id`, `cliente_id`, `canal`, `variante_id`, `enviado_at`, `estado`
  (enviado/entregado/leido/respondido/opt_out), `respondido_at`.
- **`conversacion`** — `id`, `cliente_id`, `canal`, `estado`, `intencion`, `confianza`,
  `contexto` (json), `ultima_actividad_at`.
- **`mensaje`** — `id`, `conversacion_id`, `direccion` (in/out), `contenido`, `ts`,
  `variante_id`.
- **`oferta`** — `cliente_id`, `producto_sugerido` (next-best-product), `precio`, `plazo`,
  `enganche`, `pre_aprobado`.
- **`resultado`** — `cliente_id`, `tipo_cierre` (full_ai/handoff/sin_respuesta/opt_out),
  `venta_microsip_folio` (si aplica), `monto`, `fecha`, `atribuido`.
- **`variante_mensaje`** — `id`, `nombre`, `contenido`, métricas (envíos/respuestas/conv).

**Read-model derivado de Firebird** (materializado o consultado): `cliente_id`,
`ultima_compra`, `saldo`, `num_compras`, `valor_historico`, `telefono`, `ciudad`,
`categorias_compradas` (para next-best-product), `riesgo` (4+ limpio).

**Atribución:** join `experimento` × ventas nuevas (concepto 5 en Firebird, FECHA >
contacto) → tasa por brazo y delta, con IC 95%.

## 8. Línea de tiempo honesta

- **Código + tests:** pocos días (Claude Code + IA + tests robustos).
- **Sistema afinado y confiable** (número calentado, prompts que convierten, whatsmeow
  estable 24/7): ~1-2 semanas de iteración en vivo.
- **Resultado del demo para negociar:** ~3 meses (ventana de decisión 60-90 días). **No
  se comprime** — es tiempo humano de decisión de compra.

El build no es el cuello de botella; lo son el calentamiento del número, el afinado de la
conversión contra respuestas reales, y la ventana de conversión.

## 9. Riesgos y caveats

- **No shippear** sigue siendo el riesgo #1.
- **Conversión es hipótesis hasta medirla** — el demo la fija; las primeras 4-6 semanas dan
  la tasa real y recalibran el modelo de negocio.
- **Baneo de WhatsApp** en la librería — mitigado con número dedicado/calentado, ritmo
  humano, volumen bajo, audiencia leal, número de respaldo. Arrancar verificación del API
  oficial en paralelo.
- **Capacidad de cierre** (un cerrador) limita el tratamiento del demo; a escala, ops
  (entrega/crédito) es el cuello real.
- **No tocar el control** — es la única prueba blindada de atribución.
- **Cumplimiento** — declarar IA, opt-out, PROFECO. No documentar por escrito temas
  fiscales (IVA) frente al dueño.

## 10. Fuera de alcance

- 2ª palanca de automatización de cobranza/OXXO (proyecto aparte, Fase 2-3).
- Cierre full-AI y materialización en Microsip (escala, no demo).
- Voz y SMS construidos (solo arquitectura lista en el demo).
- Reemplazo de lo fiscal/contable de Microsip.
