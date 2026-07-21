# Diseño — Reactivación R7 · Fase 2: Canal (Pieza B, con enviador falso)

> Continúa el diseño aprobado en [`2026-07-20-reactivacion-r7-design.md`](2026-07-20-reactivacion-r7-design.md)
> (§3 Pieza B, §7 gobierno, §9 gobernador). La Fase 1 (universo + cohorte + atribución) está mergeada a
> `main` local (commit `798e204`). Esta fase construye la **maquinaria del canal** — sin número de WhatsApp
> todavía — para que, cuando llegue el chip, el envío real sea un enchufe, no una reescritura.

## 1. Contexto y objetivo

La Fase 1 dejó lista la lista de a quién contactar (`MSP_RX_COHORTE`) y la medición (atribución tratamiento
vs control). Pero **nadie se contacta todavía**: falta el canal. Esta fase construye el canal completo —
encolar, gobernar el ritmo anti-baneo, "enviar", marcar `FUE_CONTACTADO`, registrar todo — con un **enviador
falso** que simula el envío. El adaptador real de `whatsmeow` (pareo QR, enviar/recibir) queda como una
implementación concreta separada que se enchufa cuando exista el número.

**Objetivo:** un flujo demostrable de punta a punta en backend — "encola la cohorte → el worker la drena al
ritmo del gobernador → los clientes quedan `CONTACTADO` → la atribución ya puede medir" — sin dependencia del
número y sin escrituras reales a WhatsApp.

## 2. Decisiones cerradas (con el usuario)

- **Alcance:** canal completo con **enviador falso**. El adaptador `whatsmeow` real se difiere hasta tener el
  número (se enchufa por config, sin rehacer nada).
- **Contenido:** **plantilla estática por segmento** (nombre interpolado). Sin cálculo de enganche/parcialidad
  — eso es del copiloto de IA en Fase 3. Fase 3 sólo reemplaza el generador de texto.
- **Disparo:** **ambos modos con flag `auto_send`.** `on` = el worker auto-drena la cola al ritmo del
  gobernador; `off` = los mensajes quedan `encolado` esperando aprobación (la acción/UI de aprobación es
  Fase 3). Deja lista la base para el "modo sombra".
- **Solo backend.** La bandeja/UI de conversaciones es Fase 3. No se toca `sistema-cobro-web` en esta fase.
- **Outbound-only.** Recibir mensajes (inbound) y el triaje llegan con el adaptador real + el copiloto
  (Fase 3). El `FakeSender` no recibe nada.

## 3. Frontera de la fase (qué NO es)

Queda explícitamente para **Fase 3**: copiloto de IA (opener personalizado, redactado, dictado), triaje de
respuestas entrantes, recepción de mensajes, UI de bandeja/aprobación, montos reales de enganche/parcialidad,
y el adaptador `whatsmeow` productivo (su código y su validación en vivo con chip de prueba).

## 4. Arquitectura

Todo dentro de `internal/reactivacion/` (mismo patrón hexagonal que Fase 1). Piezas nuevas:

| Pieza | Capa | Qué hace |
|---|---|---|
| **`MessageSender`** (puerto outbound) | `ports/outbound` | Interfaz `Enviar(ctx, dest, cuerpo) (SendResult, error)`. Implementaciones: `FakeSender` (registra y simula éxito) ahora; `WhatsmeowSender` después. Selección por config en el composition root. |
| **Gobernador** | `app` | Lógica pura que decide si el siguiente mensaje puede salir *ahora*: tope diario, jitter, horario hábil, circuit-breaker. Reloj inyectado → testeable sin esperas. |
| **Generador de opener** | `app` | Plantilla estática por `Segmento` + nombre. Devuelve el cuerpo del mensaje. Punto de reemplazo del copiloto en Fase 3. |
| **`EnvioWorker`** | `app` | Drena la cola de tratamiento respetando gobernador + `auto_send`; llama al sender; marca `FUE_CONTACTADO`; persiste el resultado. |
| **`MensajeRepo`** (puerto + `reactivacionfb`) | `ports/outbound` + `infra` | CRUD de `MSP_RX_MENSAJES`: encolar, listar pendientes, marcar enviado/fallido. |

`FakeSender` vive en un paquete infra propio (`reactivacion/infra/reactivacionsender` o similar) — no en app —
para respetar la regla de capas.

## 5. Persistencia — `MSP_RX_MENSAJES` (migración 000045)

Un registro por mensaje saliente. IDs (`uuid.New()`) y timestamps (`time.Now()`) desde Go (CLAUDE.md §1);
`CHARACTER SET UTF8`; sin lógica en la DB.

| Columna | Tipo | Nota |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK, UUID desde Go |
| `CLIENTE_ID` | `INTEGER` | del cliente de la cohorte |
| `SEGMENTO` | `VARCHAR(24)` ASCII | `recien_liquidado` / `por_liquidar_hueco` |
| `TELEFONO` | `VARCHAR(40)` | destino (copiado de la cohorte al encolar) |
| `CUERPO` | `BLOB SUB_TYPE TEXT` UTF8 | texto del mensaje |
| `ESTADO` | `VARCHAR(16)` ASCII | `encolado` / `enviado` / `fallido` / `bloqueado` |
| `SENDER_KIND` | `VARCHAR(12)` ASCII | `simulado` / `real` — integridad de la medición |
| `ENCOLADO_EN` | `TIMESTAMP` | |
| `ENVIADO_EN` | `TIMESTAMP` nullable | |
| `ERROR` | `VARCHAR(500)` nullable | motivo de `fallido`/`bloqueado` |
| `CREATED_AT` / `UPDATED_AT` | `TIMESTAMP` | auditoría estándar |

Índices: `CLIENTE_ID`, `ESTADO`. UNIQUE `(CLIENTE_ID)` **no** se aplica — un cliente puede recibir hasta 2
toques (opener + 1 recordatorio suave), así que se permiten varias filas por cliente; la idempotencia del
encolado se maneja en Go (no re-encolar a quien ya tiene un mensaje `encolado`/`enviado` de ese tipo).

`FUE_CONTACTADO` sigue viviendo en `MSP_RX_COHORTE` (Fase 1); esta fase lo pone en `1` (UPDATE puntual, sin
tocar `EN_CONTROL`/`COHORTE_FECHA`).

## 6. Máquina de estados del envío (subconjunto de Fase 2)

```
(cohorte, tratamiento, no-contactado)
        │  encolar
        ▼
    ENCOLADO ──[auto_send=off]──► espera aprobación (acción/UI = Fase 3)
        │
        │ [auto_send=on] y [gobernador: hay cupo + horario + jitter cumplido]
        ▼
     enviar (sender) ──éxito──► ENVIADO ─► marca FUE_CONTACTADO=1
        │
        └──error/breaker──► FALLIDO / BLOQUEADO (auto-pausa + queda para reintento)
```

- **Fase 2 manda solo el opener (1 toque saliente).** El 2º toque (recordatorio suave del spec padre) se
  difiere a Fase 3 — el esquema de `MSP_RX_MENSAJES` ya lo admite (varias filas por cliente), pero la lógica
  de re-encolado del recordatorio no se implementa aquí.
- El control **nunca** entra a esta cola (se filtra por `EN_CONTROL=0`).

## 7. Gobernador de envío (anti-baneo)

Lógica pura configurable (`GobernadorConfig`), calcada de la política del spec padre §9:

- **Tope diario:** N mensajes/día (default 30; rango 20-40). Cuenta `MSP_RX_MENSAJES` con `ENVIADO_EN` de hoy.
- **Jitter:** espera aleatoria entre mensajes (default 90s–8min). La fuente de aleatoriedad se inyecta
  (`*rand.Rand` con semilla, o derivada del reloj/índice) para que los tests sean deterministas.
- **Horario hábil:** solo dentro de una ventana (default L-S, 9:00-18:00 hora de negocio), tope de horas/día.
- **Circuit-breaker:** si una señal de salud cae del umbral → auto-pausa + estado `bloqueado` + log de alerta.
  En Fase 2 (fake) la señal es trivial (siempre sana); el gancho queda listo para la tasa-de-respuesta real.
- **Perfiles:** `produccion` (pacing real) y `demo` (topes altos + jitter ~0) para ver el flujo completo en
  segundos con el fake. El worker recibe el perfil por config.

El gobernador NO duerme hilos reales en producción por sí mismo: expone "¿puede salir ahora? ¿cuánto falta?"
y el worker agenda el siguiente tick. Así se testea con reloj falso sin esperas.

## 8. Opener por segmento (plantilla estática)

Un `OpenerTemplater` con una plantilla por `Segmento`, nombre del cliente interpolado, tono cercano y corto
(español neutro). Ej. conceptual:

- **recien_liquidado:** felicitación por terminar de pagar + invitación a la siguiente compra con beneficio.
- **por_liquidar_hueco:** con tacto, completar el juego/artículo con un pago que cabe (sin liderar con "compra
  más").

Sin montos reales (Fase 3). El texto exacto se define en el plan; es trivialmente reemplazable por el copiloto
de IA después (misma firma `Generar(cohorteCliente) → cuerpo`).

## 9. Integridad de la medición

`FUE_CONTACTADO=1` se marca al llegar a `ENVIADO`, con `SENDER_KIND` registrando `simulado` vs `real`. Así el
experimento real (Fase 3, sender real) nunca cuenta un contacto simulado. En dev (DB vieja + fake) se puede
jugar libremente; para medir de verdad se usa el `WhatsmeowSender`. La atribución de Fase 1 no cambia; opera
sobre `FUE_CONTACTADO` igual.

## 10. Endpoints (Huma, backend)

Bajo `/v2/reactivacion`, mismos middlewares y gating in-handler que Fase 1:

- `POST /v2/reactivacion/envios/encolar` → `reactivacion:administrar`. Encola la cohorte de tratamiento
  (no control, no ya-contactados) generando el opener por segmento. 202, background.
- `POST /v2/reactivacion/envios/drenar` → `reactivacion:administrar`. Corre el worker una tanda respetando el
  gobernador y `auto_send` (útil para demo/manual; además hay un worker automático en el ciclo fx).
- `GET /v2/reactivacion/envios` → `reactivacion:leer`. Lista/estado de `MSP_RX_MENSAJES` (filtros por estado /
  segmento) para inspeccionar el flujo.

## 11. Wiring (fx)

`cmd/api/reactivacion_wiring.go` extiende: provee `MensajeRepo` (reactivacionfb), el `MessageSender` elegido
por config (`REACTIVACION_SENDER=fake|whatsmeow`, default `fake`), el gobernador (perfil por config), y el
`EnvioWorker` registrado en el ciclo de vida fx (arranca/para con la app, como los demás workers). El
`WhatsmeowSender` puede quedar como stub que devuelve "no configurado" hasta Fase 3.

## 12. Pruebas

- **Gobernador y opener:** lógica pura, cobertura alta con reloj/semilla inyectados (sin esperas reales).
- **EnvioWorker:** con fakes de repo + sender (éxito, error, breaker, `auto_send` on/off, respeto del tope).
- **`MensajeRepo`:** tests de integración `WithTestTransaction` (rollback; SKIP sin `FB_DATABASE`).
- **Endpoints:** 401/403/200/202 con el patrón de handler de Fase 1.
- Mismos gates (domain ≥99%, app ≥90%, infra ≥80%), lint 0, cross-build Windows, `-race`.

## 13. Fuera de alcance (Fase 3)

Copiloto de IA (opener personalizado + montos reales + redactado + dictado), triaje de respuestas, recepción
inbound, UI de bandeja/aprobación, adaptador `whatsmeow` productivo y su validación en vivo, y la capa de
gobierno de contenido (allowlist §8 del spec padre).

## 14. Riesgos

- **El fake oculta problemas del canal real:** mitigado porque el `MessageSender` es una interfaz estrecha y
  el `WhatsmeowSender` se valida por separado en Fase 3 con chip de prueba.
- **Marcar `FUE_CONTACTADO` con el fake contamina la medición:** mitigado por `SENDER_KIND` + usar el sender
  real para el experimento de verdad.
- **Sobre-ingeniería del gobernador:** se implementa la política del spec padre §9, sin más; los perfiles
  demo/producción evitan un demo de días.

## 15. Referencias

- Spec padre: [`2026-07-20-reactivacion-r7-design.md`](2026-07-20-reactivacion-r7-design.md)
- Investigación del canal: [`../../ventas-ai-chatbot-research-2026.md`](../../ventas-ai-chatbot-research-2026.md) (§1 anti-baneo)
- Fase 1 (mergeada, `798e204`): módulo `internal/reactivacion/` (universo, cohorte, atribución).
