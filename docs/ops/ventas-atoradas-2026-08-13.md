# Ventas que no entraron — 2026-08-13

Documento del incidente del día del cutover al API Go: nueve ventas capturadas en campo
que no llegaron al sistema y estuvieron reintentando durante horas sin ninguna
posibilidad de éxito. Recoge **qué pasó, por qué, qué se hizo y qué sigue faltando**,
porque el flujo de subida de ventas todavía necesita rediseñarse.

Documento hermano: [`migracion-go-2026-08-13.md`](migracion-go-2026-08-13.md), que registra
el cutover en sí.

---

## 1. Resumen

El 13 de agosto de 2026, **246 intentos de crear venta** fallaron contra `POST /v2/ventas`.
No eran 246 ventas: deduplicando por `Idempotency-Key` eran **31 ventas distintas**, de las
cuales **22 acabaron entrando** en algún reintento posterior. Las otras **9 nunca entraron**.

La causa raíz fue **un tope de 10 segundos para leer la petición completa**, contra cargas
de 2.0 a 3.4 MB. Nada que ver con la lógica de ventas.

Al recuperarlas se descubrió un problema más grande que el que se estaba resolviendo:
**siete de las nueve traían campos vacíos que el servidor rechaza y la app dejó capturar**.

---

## 2. Cómo sube una venta hoy

La app arma **un solo multipart** con todo:

```
POST /v2/ventas
Idempotency-Key: <UUID de la venta, generado por la app>

  parte "datos"   → JSON con cliente, dirección, montos, productos, vendedores (~1–1.5 KB)
  parte "imagen"  → foto 1
  parte "imagen"  → foto 2
  ...             → de 1 a 6 fotos, ~600 KB cada una
```

Total medido: **2.0 a 3.4 MB por venta**.

La cadena de middlewares para esa ruta es `authn → capture → idem → handler`:

1. **`capture`** (`internal/platform/failedintent`) instala un `io.TeeReader` sobre el cuerpo:
   todo byte que alguien lea se copia a un blob en disco. Por eso, ante un fallo, queda
   evidencia de exactamente lo que el servidor alcanzó a leer.
2. **`idem`** (`internal/platform/idempotency`) hace `io.ReadAll(r.Body)` — **el cuerpo entero
   a memoria** — luego lo hashea, luego busca la llave, y solo entonces reproduce la
   respuesta guardada o corre el handler.
3. El handler crea la venta. El `id` **viene del cliente** dentro de `datos` y es la llave
   primaria de `MSP_VENTAS`.

Por encima de todo, `http.Server`:

```go
// cmd/api/server.go
ReadTimeout:  cfg.HTTP.ReadTimeout,   // HTTP_READ_TIMEOUT,  default "10s"
WriteTimeout: cfg.HTTP.WriteTimeout,  // HTTP_WRITE_TIMEOUT, default "30s"
```

Producción no fijaba ninguna de las dos, así que corrían los valores de fábrica.

---

## 3. La causa raíz

`http.Server.ReadTimeout` es **el tiempo máximo para leer la petición completa, incluyendo
el cuerpo**. Diez segundos para 2 MB exige **~1.6 Mbps sostenidos**; para 3.4 MB, ~2.7 Mbps.
En campo eso no existe.

Y es **determinista**: el cronómetro es de reloj de pared y arranca con cada petición. Misma
carga y mismo enlace dan el mismo resultado siempre. El intento 52 no tenía más posibilidad
que el primero.

### La evidencia

| Medición | Valor |
|---|---|
| Duración de los `request_body_read_failed` | máx **10,012 ms**, promedio 8,938 ms |
| Blobs capturados en disco | 251 archivos, 471 MB |
| Blobs que terminan en múltiplo exacto de 4,096 bytes (flujo cortado) | **78** |
| Intentos con `request_body_read_failed` | **78** |

El máximo clavado en 10,012 ms es la huella del tope. La correspondencia 78 = 78 entre
archivos truncados e intentos fallidos cierra el caso.

### Las tres caras del mismo fallo

- **Se vence a media subida** → cuerpo cortado, blob truncado en frontera de 4 KB →
  `422 request_body_read_failed`.
- **Se vence justo al terminar** → todos los bytes ya pasaron por el tee y el blob queda
  **completo, con su frontera de cierre**, pero la lectura final topa con el plazo vencido y
  `io.ReadAll` devuelve error igual. Una sola venta tuvo **17 intentos con el mismo tamaño
  exacto de 1,977,805 bytes**, todos completos, todos rechazados. Ese dato fue el que
  descartó definitivamente la hipótesis de "mala red".
- **Alcanza a pasar la lectura** → la siguiente llamada que mira el contexto lo encuentra
  cancelado. Sale **500** con `context canceled`. Duraciones de 2.5 a 6.6 s: ahí el teléfono
  cuelga por su cuenta, antes del tope del servidor. Causa del lado cliente, no probada.

### La trampa de Go que hay que conocer

`WriteTimeout` **se reinicia al leer el encabezado de la petición**, así que en la práctica
engloba leer el cuerpo + procesar + responder. Con `WriteTimeout=30s`, subir solo
`ReadTimeout` deja el techo efectivo en 30 s. **Hay que subir las dos.**

El SSE no se ve afectado: ese handler ya se sale del `WriteTimeout` por su cuenta
(`handlers_sse.go`, `SetWriteDeadline(time.Time{})`).

---

## 4. Por qué la app se rendía — o giraba en vacío

`UploadFailureClassifier` (app):

```kotlin
408, 409, 425, 429 -> TRANSIENT
in 400..499        -> PERMANENT
in 500..599        -> TRANSIENT
```

Y ahí está el segundo defecto de fondo: **`request_body_read_failed` es un fallo de
transporte disfrazado de error del cliente**. Sale como **422**, y cualquier cliente bien
portado lee 4xx como "tu petición está mal, no insistas".

Eso explica las dos conductas opuestas observadas:

- El teléfono que **recibió** el 422 lo clasificó permanente, dejó de reintentar y pintó
  "Pendiente de corrección" — dos intentos y alto.
- El que **no alcanzó a recibir la respuesta** cayó en el camino de excepción de red y
  reintentó sin fin — **52 veces en nueve horas**, unos 180 MB de tráfico por una venta que
  nunca iba a entrar.

---

## 5. El caso peor: la venta que sí entró y el teléfono nunca lo supo

Una venta (`fb795593…`, $6,400) **existe en `MSP_VENTAS` desde las 15:14**, con `201 Created`
guardado en `MSP_IDEMPOTENCY_KEYS`. Su teléfono siguió mostrando *"Error al enviar ·
Pendiente de corrección"* doce horas después.

El mecanismo es este: **`io.ReadAll` corre ANTES de consultar la idempotencia**, porque el
hash con que se detecta reúso de llave necesita el cuerpo completo. Un cliente que no puede
terminar de subir **jamás puede cobrar la respuesta exitosa que ya está guardada esperándolo**.
El único camino para recibir la buena noticia pasa justo por donde se rompe.

La app **sí** tiene reconciliación: ante un error HTTP, `PendingLocalSalesWorker` hace
`GET /v2/ventas/{id}` y, si la venta existe, la marca enviada y limpia el error. Está bien
pensado. Pero **solo corre durante un intento de subida**, y una venta clasificada como
permanente ya no genera intentos. Se queda marcada como fallida para siempre.

**El riesgo real no es el error en pantalla: es que alguien la recapture a mano.** Eso sí
crearía una venta duplicada con otro id.

---

## 6. Lo que se hizo

### Mitigación (aplicada en producción, 2026-08-13 22:32)

En `C:\msp-api\run.bat`, respaldo en `run.bat.bak-pretimeouts`:

```
set HTTP_READ_TIMEOUT=120s
set HTTP_WRITE_TIMEOUT=180s
```

Sin recompilar ni sacar versión de la app. Reiniciado y verificado: `http: listening`,
cuatro puertos, cero errores.

> **Nota operativa:** `Tee-Object` en `run.bat` **trunca `api.log` en cada arranque**. El
> log del día se pierde al reiniciar. Conviene cambiarlo a `-Append`.

### Recuperación de las nueve

Vía `POST /v2/_admin/failed-intents/{id}/replay-with-multipart` (permiso
`failed_intents:resolver`), con un manifiesto que reemplaza **solo** la parte `datos`
(`kind: field`) y conserva las fotos por índice (`kind: keep`) — **no se volvió a subir un
byte de imagen**.

| Corrección | Ventas |
|---|---|
| `POR CORREGIR` en ciudad | 3 |
| `POR CORREGIR` en colonia | 2 |
| `POR CORREGIR` en ambas | 2 |
| Quitar el teléfono `+52000000` | 1 |
| Ninguna (solo esperó un traspaso de inventario) | 1 |

Las nueve quedaron en `active` / `borrador`.

---

## 7. El hallazgo que importa más que el incidente

**Siete de las nueve ventas estaban enfermas antes de tropezar con el timeout:**

- 6 con `ciudad` vacía
- 4 con `colonia` vacía
- 1 con teléfono `+52000000` — el vendedor tecleó "000000" y
  `LocalSaleMappers.normalizeTelefonoE164` le antepuso el prefijo sin contar dígitos

El servidor exige `calle`, `colonia`, `poblacion` y `ciudad` no vacías
(`internal/ventas/domain/direccion.go`). **`NewSaleFormValidator` de la app no valida ninguna
de las tres últimas.** El teléfono es opcional para todo tipo de venta, pero si llega con
valor se valida — y la app en CONTADO aceptaba cualquier cosa.

O sea: **la app encola ventas que el servidor va a rechazar, y el vendedor se entera horas
después, o nunca.** El timeout solo hizo visible un problema que ya estaba ahí.

Es también la confirmación práctica de la discusión sobre el catálogo de ciudades: no era
un problema teórico, estaba bloqueando seis de las nueve ventas.

---

## 8. Lo que falta

En orden de rendimiento por esfuerzo:

1. **Validar en la captura lo mismo que valida el servidor.** (En curso.) Ciudad, colonia y
   población obligatorias; teléfono validado también en contado; `normalizeTelefonoE164` que
   nunca emita un número inválido. Es el arreglo que evita la mayoría de los casos.

2. **Que `request_body_read_failed` deje de ser 422.** Un fallo de transporte debe salir con
   un código reintentable. Mientras sea 4xx, cada teléfono que lo reciba se rendirá para
   siempre ante un problema pasajero.

3. **Reconciliación que no dependa de un intento de subida.** Hoy una venta que sí entró
   puede quedar marcada como fallida indefinidamente. Basta un `GET` periódico por las
   pendientes, o reconciliar al abrir la pantalla.

4. **Consultar idempotencia antes de leer el cuerpo.** Requiere separar la detección de
   reúso de llave (que necesita el hash) de la reproducción de un éxito ya guardado (que no
   lo necesita). Alternativa más simple: si la lectura del cuerpo falla **y** existe una
   respuesta 2xx guardada para esa llave, reproducirla.

5. **Dividir la venta de sus imágenes.** El endpoint **ya existe**:
   `POST /v2/ventas/{id}/imagenes` ("Adjuntar imagen"). La app simplemente no lo usa al
   crear: manda todo junto en `crearVenta(@Part("datos"), @Part imagen: List<...>)`.

   Es como lo hace la industria — Google Photos sube los bytes primero y crea el elemento en
   otra llamada, "para manejar fallas de forma independiente en cada etapa sin perder el
   progreso"; X/Twitter hace `INIT → APPEND → FINALIZE` y solo entonces crea el post; el
   IETF estandariza subidas reanudables (`draft-ietf-httpbis-resumable-upload`) precisamente
   porque las interrupciones exigen reanudar, no retransmitir.

   **Con las fotos obligatorias** —validan la venta y traen su nota— la división no puede ser
   "la venta entra y las fotos luego": la venta debe entrar como **borrador que no puede
   avanzar** hasta tener su evidencia completa. Eso obliga a un dato nuevo en la creación:
   **cuántas fotos esperar**, para distinguir "todavía no llegan" de "no lleva ninguna".

6. **Reducir el peso.** El servidor ya reprocesa las imágenes a **1920 px / JPEG 85**
   (`adjuntar_imagen.go`, log `imageprocessor.enabled`). La app ya comprime, pero manda
   **hasta 6 fotos de ~600 KB**. El peso no viene de fotos pesadas sino de su número; conviene
   revisar que la compresión del teléfono apunte al mismo objetivo que el servidor.

---

## 9. Cómo diagnosticar esto la próxima vez

Cinco trampas que costaron tiempo:

1. **`MSP_FAILED_INTENTS.BODY` dice `null`.** La carga real vive en disco, en la ruta de
   `BODY_BLOB_PATH`. Verificar `BODY IS NULL` da falsos negativos.

2. **Un intento no es una venta.** Deduplicar siempre por `IDEMPOTENCY_KEY`, que además **es
   el id de la venta** — llaves distintas son ventas distintas por construcción.

3. **Un intento fallido no significa que no entró.** Lo pendiente de verdad es lo que no
   tiene registro exitoso. Y `STATUS` sigue en `'new'` para todo: nadie resuelve esa cola,
   así que ese campo no sirve como señal.

4. **Tras un `replay`, la consulta de pendientes miente.** El reenvío **acuña una llave de
   idempotencia nueva a propósito** (`handlers.go`, "Always mint a fresh Idempotency-Key"),
   así que el registro de éxito no queda bajo la llave original y esas ventas se reportan
   como pendientes para siempre. La verdad está en `MSP_VENTAS`.

5. **`findstr` no encuentra las líneas de error del log** (falla con líneas largas).
   PowerShell `Select-String` sí. Y ojo: los 500 se registran como `INFO` con
   `error_code=""` — de 62 respuestas 500 solo había 7 líneas `level=ERROR`. **La causa de un
   500 casi nunca queda grabada**, que fue lo que más costó en este incidente.
