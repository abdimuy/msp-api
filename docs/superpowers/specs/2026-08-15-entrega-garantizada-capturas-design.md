# Entrega garantizada de capturas de campo — Etapas 1 y 2

**Fecha:** 2026-08-15
**Estado:** diseño aprobado, pendiente de plan de implementación
**Alcance:** pagos (etapa 1) y contrato compartido pagos+ventas (etapa 2)
**Fuera de alcance:** visitas y garantías (etapa 3, posterior)

---

## 1. Por qué

El 2026-08-13 dos pagos de un cobrador (`d07e818b…` y `984dac82…`, $800) quedaron
en su teléfono marcados como enviados —`GUARDADO_EN_MICROSIP = 1`— pero sin
existir en Microsip, sin fila en `MSP_FAILED_INTENTS` y sin quedar pendientes en
cola. Se recuperaron a mano el 2026-08-14 sólo porque el cobrador notó que el
saldo del cliente no bajaba.

Medido durante la investigación:

- **Pagos: se pueden perder.** Hay dos rutas que marcan un pago como entregado
  sin prueba de que el servidor lo tenga.
- **Ventas: no se pierden.** Ya tienen verificación por GET y su rama de fallo
  permanente no marca la venta como enviada, así que vuelve a la cohorte y el
  arranque de la app la reencola. De 86 ventas rechazadas ese día, 77 entraron
  solas.
- **Cuatro módulos, cuatro políticas distintas** para el mismo problema.

| Módulo | 401 | 4xx | 5xx |
|---|---|---|---|
| Pagos | reintenta | **suelta** (sin verificar si llegó al API) | suelta sólo si llegó |
| Ventas | **suelta** | suelta | reintenta |
| Visitas | reintenta | suelta | `RETRY_THEN_DONE` con tope |
| Garantías | — | — | sin clasificador: reintenta todo, siempre |

Nadie diseñó esa divergencia: cada módulo resolvió lo mismo por separado.

---

## 2. El invariante

> **El teléfono suelta una captura sólo cuando el servidor confirma una de dos
> cosas: "la apliqué" o "la tengo guardada para corregir". Cualquier otra
> respuesta se reintenta.**

Dos pruebas distintas, con papeles distintos:

| Prueba | Pregunta que responde | Para qué sirve |
|---|---|---|
| **Existencia** (GET por id) | ¿está aplicado? | la garantía de no perder |
| **Custodia** (captura confirmada) | ¿está resguardado? | poder detenerse ante datos inválidos |

Sin la segunda, un pago genuinamente inválido (falta saldo, permiso denegado)
se reintentaría para siempre. El middleware de captura **no deduplica** —86
ventas produjeron 498 filas en `MSP_FAILED_INTENTS`—, así que reintentar sin
condición de paro vuelve inservible esa tabla.

### Casos imposibles, declarados

Fuera del alcance de cualquier garantía: teléfono perdido, destruido, borrado o
que nunca vuelve a conectarse. La garantía es: **mientras el dispositivo exista
y vuelva a tener red, ninguna captura se pierde.**

---

## 3. Lo que ya funciona y NO se toca

Verificado en código durante el análisis. Nada de esto debe cambiarse:

- `getPendingPayments()` = `WHERE GUARDADO_EN_MICROSIP = 0`; `getPendingSales()`
  = `ENVIADO = 0`. La cohorte pendiente es la fuente de verdad.
- El sync de sesión (`PendingWorkSyncFactory`) reencola **cada** pendiente al
  arrancar la app.
- WorkManager: `enqueueUniqueWork` con nombre único por id, política `KEEP`,
  restricción de red, **sin tope de reintentos**.
- Ninguna ruta borra pendientes: el reconciliador y la limpieza por zona los
  excluyen explícitamente (auditado en `d6720906`).
- `crear_pago.go` ya es idempotente por el `id` del cuerpo, **sin caducidad**:
  ante el mismo id devuelve el pago existente.
- El `id` que genera el teléfono es la **PK** de `MSP_VENTAS`: duplicar una
  venta es imposible aunque expire la caché de idempotencia.
- Ventas ya verifica por GET (`obtenerVenta`) ante cualquier error HTTP, antes
  de clasificar, y rota su `Idempotency-Key` cuando el cuerpo se corrige.

---

## 4. Etapa 1 — Pagos

Objetivo único: **que un pago no se pueda perder.** No toca ventas.

### 4.1 Servidor (Go)

**1a. Exponer la verificación de pago en el router del cobrador.**

`GET /v2/cobranza/pagos/{id}` ya existe (`cobranza-obtener-pago-recibido`,
`routes.go:218`) pero está registrado en `MountAdminRouter`, que se monta bajo
`/v2/_admin/saldos` y exige permiso de administración. El cobrador no lo
alcanza.

Registrar la misma operación en `MountReadRouter` bajo `/v2/cobranza/pagos/{id}`,
con permiso de cobranza. Debe responder:

- **200** con el pago cuando existe
- **404** cuando no existe

Sin cambios en el repositorio ni en el servicio: la consulta ya está escrita.

**1b. Confirmar la captura con una cabecera.**

En `internal/platform/failedintent`, `saveIntent` hoy sólo escribe un
`slog.Error` si el `Store.Save` falla, y la respuesta sale igual. El cliente no
puede distinguir "capturado" de "se perdió". Peor: `Store.Save` escribe a
Firebird, así que **cuando el pool está trabado la petición falla Y la captura
falla a la vez**.

El middleware debe añadir a la respuesta:

```
X-Intent-Captured: <uuid del intento>
```

**sólo cuando `Store.Save` devolvió nil.** Si falla, la cabecera se omite.

Aplica a `CaptureMiddleware` completo, así que ventas la recibe también sin
trabajo extra. No cambia códigos de estado ni cuerpos.

### 4.2 App (Kotlin)

**1c. Verificación por GET antes de decidir.**

`PendingPaymentsWorker.uploadV2`, en el `catch (e: HttpException)`, antes de
clasificar: consultar `GET /v2/cobranza/pagos/{id}`.

- **200** → el servidor lo tiene → `markDone` + log `RECONCILED_VIA_GET`
- **404** → no lo tiene → seguir a la decisión
- cualquier otro resultado o excepción → indeterminado → seguir a la decisión

Mismo patrón que `PendingLocalSalesWorker` ya usa. Es la pieza que sustituye a
la inferencia por `Content-Type`.

**1d. La tabla de decisión.**

Reemplaza a `PaymentUploadClassifier.classify(code, reachedMspApi)`. La nueva
firma recibe además si la captura fue confirmada:

```kotlin
classify(
    code: Int,
    reachedMspApi: Boolean,     // Content-Type contiene problem+json
    captureConfirmed: Boolean,  // llegó X-Intent-Captured
): Decision
```

Se evalúa **después** de que la verificación por GET no haya resuelto.

| Situación | Decisión | Razón |
|---|---|---|
| 2xx | `SUELTA` | aplicado, o ya aplicado por idempotencia del repo |
| GET devolvió 200 | `SUELTA` | existe en el servidor |
| 401 | `REINTENTA` | parpadeo de token |
| 408, 409, 425, 429 | `REINTENTA` | señales de backoff |
| 4xx o 5xx **con** `X-Intent-Captured` | `SUELTA` | resguardado; lo corrige la oficina |
| 4xx o 5xx **sin** `X-Intent-Captured` | `REINTENTA` | nadie lo tiene |
| cualquier otro código | `REINTENTA` | ante la duda, conservar |
| `IOException` (red) | `REINTENTA` | el servidor no lo vio |

`reachedMspApi` **deja de usarse como prueba de custodia**: pasa a ser sólo un
dato de diagnóstico para el log. La prueba es la cabecera.

**Regla dura:** `markDone` se llama únicamente en las filas marcadas `SUELTA`.
No hay otra ruta.

### 4.3 Pruebas de la etapa 1

Cada corrección con una prueba que **falle revirtiendo la línea** (convención
del proyecto).

Función pura, en JVM:
- `classify(404, reachedMspApi=false, captureConfirmed=false)` → `REINTENTA`
  (hoy devuelve `DONE`; es el 404 del túnel)
- `classify(500, reachedMspApi=true, captureConfirmed=false)` → `REINTENTA`
  (hoy `DONE`; es el caso del pool trabado)
- `classify(422, reachedMspApi=true, captureConfirmed=true)` → `SUELTA`
- `classify(401, …)` → `REINTENTA` en toda combinación

Worker, con API falsa:
- GET 200 tras un 500 → `markDone` **una** vez y `RECONCILED_VIA_GET` en el log
- GET 404 tras un 500 sin cabecera → **no** llama `markDone`, devuelve retry
- `IOException` → nunca `markDone`

Go, integración:
- `GET /v2/cobranza/pagos/{id}` responde 200 para un pago existente y 404 para
  uno inexistente, con token de cobrador
- Un 422 provocado devuelve `X-Intent-Captured`; con el store forzado a fallar,
  la cabecera **no** aparece

---

## 5. Etapa 2 — Contrato compartido

Objetivo: que pagos y ventas usen **la misma política**, y que un módulo nuevo
la herede en vez de inventarla.

### 5.1 App: módulo `:core:upload`

Módulo Gradle nuevo, siguiendo la arquitectura nueva (nada en `app/` legacy).
Contiene **dos piezas y nada más**:

**1 · La función pura de decisión.** La tabla de decisión de §4.2 (punto 1d),
sin dependencias de Android, verificable en JVM. Única definición de la
política.

**2 · El puerto de verificación.**

```kotlin
fun interface ExistenceVerifier {
    /** true = existe, false = no existe, null = indeterminado. */
    suspend fun exists(id: String): Boolean?
}
```

Cada módulo lo implementa con su propio endpoint. Nada más se comparte: los
workers se quedan en su módulo porque difieren en multipart, imágenes y rutas.
Forzarlos a un molde común sería peor que la duplicación que elimina.

### 5.2 App: migración

- **Pagos** pasa a usar `:core:upload`. Su clasificador propio se elimina.
- **Ventas** pasa a usar `:core:upload`. `UploadFailureClassifier` se elimina.
  Esto **cambia comportamiento hoy correcto** — ver riesgos.

Cambios de comportamiento en ventas, todos deliberados:

| Antes | Después |
|---|---|
| 401 → permanente | reintenta |
| 4xx del túnel → permanente | reintenta |
| 4xx con captura confirmada → permanente | suelta (equivalente, ahora probado) |
| 5xx → transitorio siempre | suelta sólo con captura confirmada |

Se conservan intactos: la verificación por GET, la rotación de
`Idempotency-Key`, y que la rama de fallo **no** marque la venta como enviada.

### 5.3 Servidor (Go)

**2a. Idempotencia por `id` del cuerpo en `crear_venta`.**

Hoy ventas depende de la caché de idempotencia (`DefaultTTL = 24 h`, y sólo
cachea 2xx). Pasadas 24 h, un reintento de una venta ya creada choca contra la
PK y sale como **500 sin clasificar**.

`crear_venta` debe hacer lo que `crear_pago.go:62` ya hace: si existe una venta
con ese `id`, devolver la existente en vez de intentar insertarla. Es una guarda
al inicio del servicio, no un refactor.

Con eso los dos módulos tienen la misma propiedad y el reintento indefinido es
seguro en ambos.

**2b. Documentar el estándar.**

Nuevo documento en `docs/module-standards/` — *"Entrega garantizada desde
campo"*. Todo módulo que reciba escrituras desde el teléfono debe cumplir:

1. Idempotencia por el `id` del cuerpo, sin caducidad, devolviendo el existente.
2. Un `GET /{id}` alcanzable por el rol que captura, con 200/404 limpios.
3. Estar cubierto por el middleware de captura, que confirma con
   `X-Intent-Captured`.

Se referencia desde `MODULE_TEMPLATE.md`.

### 5.4 Pruebas de la etapa 2

- La función pura conserva **todas** las pruebas de la etapa 1, ahora en
  `:core:upload`.
- Ventas: una prueba por cada renglón de la tabla de cambios de comportamiento.
- Go: reintento de `crear_venta` con el mismo `id` devuelve la venta existente
  con 200, no un 500 ni una fila duplicada.
- Compuerta completa del proyecto: `./gradlew test detekt ktlintCheck` y
  `go test -short ./...`, con `go clean -testcache` antes de la verificación
  final.

---

## 6. Fuera de alcance

Se descartan explícitamente para que no se cuelen a media implementación:

- **Visitas y garantías** (etapa 3). Garantías hoy no tiene clasificador y
  reintenta todo indefinidamente: nunca pierde, nunca se detiene.
- **Deduplicación del middleware de captura.** Real (86 ventas → 498 filas)
  pero es ruido, no pérdida. Va aparte.
- **`request_body_read_failed` como 422.** Es un fallo de transporte disfrazado
  de error del cliente. Va aparte.
- **Purga de gemelos y persistir `docto_cc_id`.** Corrigen los totales inflados
  en pantalla (17 filas / $3,550 en un cobrador medido). No son pérdida.
- **La pantalla de oficina.** Necesaria para que nadie tarde dos días en
  enterarse, pero es otro proyecto.

---

## 7. Riesgos

**Ventas funciona hoy y la etapa 2 le cambia el comportamiento.** Es el riesgo
principal. Mitigación: la etapa 1 no la toca, y la etapa 2 no se empieza hasta
que la 1 esté verificada en producción.

**Reintento indefinido sin condición de paro.** Si la cabecera
`X-Intent-Captured` no se implementa bien, una captura inválida reintenta para
siempre y llena `MSP_FAILED_INTENTS`. Mitigación: la cabecera es lo primero de
la etapa 1, y su prueba fuerza el fallo del store para comprobar que se omite.

**El GET añade una petición por error.** Sólo ocurre en el camino de error, no
en el feliz. Ventas ya lo hace y no ha sido problema.

**Orden obligatorio.** 1b (cabecera) antes que 1d (decisión), porque la tabla
depende de ella. Si se implementa 1d primero, todo 4xx/5xx reintentaría sin
condición de paro.
