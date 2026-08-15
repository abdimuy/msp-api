# Entrega garantizada desde campo

**OBLIGATORIO** para todo módulo que reciba escrituras desde el teléfono
(pagos, ventas, visitas, garantías, y cualquiera que venga después).

Origen: el 2026-08-13 dos pagos de un cobrador quedaron marcados como enviados
en su teléfono, sin existir en Microsip, sin fila en `MSP_FAILED_INTENTS` y sin
quedar pendientes en cola. Se recuperaron a mano porque el cobrador notó que el
saldo no bajaba. El diseño completo está en
`docs/superpowers/specs/2026-08-15-entrega-garantizada-capturas-design.md`.

## El invariante

> El teléfono suelta una captura sólo cuando el servidor confirma una de dos
> cosas: **«la apliqué»** o **«la tengo guardada para corregir»**. Cualquier
> otra respuesta se reintenta.

Son dos pruebas distintas, con papeles distintos:

| Prueba | Pregunta que responde | Para qué sirve |
|---|---|---|
| **Existencia** (`GET /{id}`) | ¿está aplicado? | la garantía de no perder |
| **Custodia** (`X-Intent-Captured`) | ¿está resguardado? | poder detenerse ante datos inválidos |

Sin la segunda, una captura genuinamente inválida —falta saldo, permiso
denegado— se reintentaría para siempre.

**Casos fuera de alcance, declarados:** teléfono perdido, destruido, borrado o
que nunca vuelve a conectarse. La garantía es: *mientras el dispositivo exista y
vuelva a tener red, ninguna captura se pierde.*

## Los tres requisitos

### 1. Idempotencia por el `id` del cuerpo, sin caducidad

El `id` lo genera el teléfono y es la llave de deduplicación de punta a punta.
Ante el mismo `id`, el servicio devuelve **la fila existente**, no un error.

Guarda al inicio del servicio, antes de cualquier validación:

```go
if existing, err := s.ventaExistente(ctx, in.ID); err != nil || existing != nil {
    return existing, err
}
```

Reglas:

- **Sin caducidad.** La caché de idempotencia (`DefaultTTL` = 24 h, y sólo
  cachea 2xx) **no** cumple este requisito. El teléfono reintenta
  indefinidamente, así que un reintento puede llegar días después.
- **Un error de lookup que no sea «no encontrado» se propaga.** Leerlo como
  «no existe» e insertar es exactamente cómo un pool trabado convierte un
  reintento en violación de llave primaria (un 500 sin clasificar, que la tabla
  de decisión reintenta para siempre).
- **No re-emitir eventos.** El camino idempotente devuelve y sale: no vuelve a
  encolar `VentaCreada` ni equivalentes.

Referencias: `internal/cobranza/app/crear_pago.go`,
`internal/ventas/app/crear_venta.go` (`ventaExistente`).

### 2. Un `GET /{id}` alcanzable por el rol que captura

El teléfono lo consulta ante **cualquier** error HTTP, antes de clasificar. Si
responde 200, la captura se suelta con `RECONCILED_VIA_GET`.

- **200** con la entidad cuando existe; **404** limpio cuando no.
- Registrado en el router que el rol de campo alcanza — **no** en el router de
  administración. Un `GET` que sólo el admin puede llamar no sirve de nada.
- El permiso debe ser el del rol que captura (`cobranza:ver_pagos`,
  `ventas:ver`), no uno de administración.

Verificar las dos cosas: que la ruta esté montada en el router correcto **y**
que el rol tenga el permiso en `MSP_ROLES_PERMISOS`. Son fallos distintos y
ambos producen el mismo síntoma.

### 3. Cobertura del middleware de captura

La ruta debe estar dentro de `failedintent.CaptureMiddleware`, que persiste el
intento y confirma la custodia con:

```
X-Intent-Captured: <uuid del intento>
```

**La cabecera se emite sólo cuando `Store.Save` devolvió nil.** Si el save
falla —el caso del pool trabado, donde la petición falla *y* la captura falla a
la vez— se omite, y el teléfono reintenta. Emitirla sin custodia real es
precisamente el defecto que pierde dinero.

Detalle de implementación que hay que respetar: en un 4xx/5xx el
`captureWriter` **retiene** el estado y el cuerpo en lugar de reenviarlos,
porque la cabecera sólo puede añadirse mientras el bloque de cabeceras sigue
abierto, y si hay que añadirla no se sabe hasta que `Store.Save` corrió —
después de que el handler retornó. Los 2xx/3xx se reenvían al escribirse y no
se almacenan.

El middleware **no deduplica**: 86 ventas produjeron 498 filas. Reintentar sin
condición de paro vuelve inservible `MSP_FAILED_INTENTS`, y por eso el
requisito 3 no es opcional.

## La tabla de decisión (lado teléfono)

Vive una sola vez, en `:core:upload`. Ningún módulo escribe la suya.

Se evalúa **después** de que la verificación por GET no haya resuelto:

| Situación | Decisión | Razón |
|---|---|---|
| 2xx | `SUELTA` | aplicado, o ya aplicado por idempotencia |
| GET devolvió 200 | `SUELTA` | existe en el servidor |
| 401 | `REINTENTA` | parpadeo de token |
| 408, 409, 425, 429 | `REINTENTA` | señales de backoff |
| 4xx o 5xx **con** `X-Intent-Captured` | `SUELTA` | resguardado; lo corrige la oficina |
| 4xx o 5xx **sin** `X-Intent-Captured` | `REINTENTA` | nadie lo tiene |
| cualquier otro código | `REINTENTA` | ante la duda, conservar |
| `IOException` (red) | `REINTENTA` | el servidor no lo vio |

**Regla dura:** la fila local se marca como entregada únicamente en `SUELTA`.
No hay otra ruta.

`reachedMspApi` (inferido del `Content-Type`) **no es prueba de custodia**:
queda sólo como dato de diagnóstico para el log.

## Orden de despliegue

**Servidor primero, app después.** El cambio del servidor es aditivo —ruta
nueva y cabecera nueva—; ninguna app existente se ve afectada.

Desplegar la app primero sería el error: buscaría `X-Intent-Captured` en un
servidor que no la emite y, como la tabla manda reintentar cuando falta, todo
rechazo legítimo entraría en reintento indefinido.

El modo de falla del orden equivocado es **ruido, no pérdida**. Es la dirección
correcta en la que fallar, y es deliberada.

## Pruebas que no pueden faltar

Cada corrección con una prueba que **falle revirtiendo la línea**.

Servidor:

- Reintento con el mismo `id` devuelve la fila existente, sin insertar de nuevo
  y sin re-emitir el evento.
- Un error de lookup indeterminado **no** inserta.
- `GET /{id}` responde 200 para una entidad existente y 404 para una
  inexistente, **con el token del rol que captura**.
- Un 4xx provocado devuelve `X-Intent-Captured`; **con el store forzado a
  fallar, la cabecera no aparece.**

Teléfono:

- `classify(404, reached=false, captured=false)` → `REINTENTA`.
- `classify(500, reached=true, captured=false)` → `REINTENTA`.
- `classify(401, …)` → `REINTENTA` en toda combinación.
- GET 200 tras un 500 → suelta **una** vez, con `RECONCILED_VIA_GET` en el log.
- `IOException` → nunca suelta.

## Lo que ya funciona y NO se toca

Verificado en código. Nada de esto debe cambiarse al implementar un módulo
nuevo:

- La cohorte pendiente es la fuente de verdad
  (`WHERE GUARDADO_EN_MICROSIP = 0`, `WHERE ENVIADO = 0`).
- El sync de sesión reencola **cada** pendiente al arrancar la app.
- WorkManager: `enqueueUniqueWork` con nombre único por id, política `KEEP`,
  restricción de red, **sin tope de reintentos**.
- Ninguna ruta borra pendientes: el reconciliador y la limpieza por zona los
  excluyen explícitamente.
- La rama de fallo permanente **no** marca la captura como enviada.
