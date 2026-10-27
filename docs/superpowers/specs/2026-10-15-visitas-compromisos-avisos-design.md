# Visitas, compromisos y avisos — diseño (documento 2)

Fecha: 2026-10-15 · Estado: **borrador para revisión del dueño** · Sin código de producto
Repos: `msp-api` (servidor), `msp-app-kt` (teléfono), `sistema-cobro-web` (escritorio, solo §6.2)

Complementa a `2026-10-15-desempeno-pulido-2-design.md` (**documento 1**: las 3 columnas, contrato
v2.2) y a `2026-10-14-desempeno-cobradores-design.md` §9 ("Fase 2: promesas y citas al servidor"),
que este documento sustituye. No cambia nada del documento 1; lo alimenta (§5.6).

---

## 0. Cómo leer este documento

### 0.1 Niveles de evidencia (CLAUDE.md §8, `docs/registro-de-evidencia.md` §4.1)

Cada afirmación lleva uno de cuatro niveles. **Ninguna afirmación por debajo de MEDIDO justifica un
cambio**: lo que aquí está en LEÍDO o INFERIDO es *lo que hay que medir antes de tocar*, no
permiso para tocar.

| Nivel | Qué significa aquí |
|---|---|
| **MEDIDO** | Se ejecutó, hay salida, fecha, lugar y control positivo. **En este documento no hay ninguna medición nueva.** Las cifras MEDIDO que aparecen vienen de entradas ya registradas y se citan con su identificador. |
| **LEÍDO** | `archivo:línea` leído por el autor el 2026-10-15. No se ejecutó. |
| **INFERIDO** | Deducción; cada una dice qué la volvería MEDIDO. |
| **SUPUESTO** | Nadie lo revisó. Se marca para que nadie lo trate como hecho. |
| **REPORTADO** | Lo afirmó otro agente o el dueño hoy y **no lo re-verifiqué**. Equivale a SUPUESTO para efectos de §8. |

### 0.2 Qué se leyó y en qué versión

- `msp-api`: `main` en `e1762f5`, con `docs/COBRANZA-SYNC.md` modificado en el árbol de trabajo (se
  leyó esa versión, 529 líneas).
- `msp-app-kt`: rama `feat/estado-cuenta-casos-compartidos` en `083c0d61`. Para los directorios de
  workers, visitas, base de datos, `:core:upload`, `:core:common/sync` y `feature/visitas`,
  `git diff --stat main` salió vacío (ejecutado 2026-10-15; `main` = `44fe4593`, release 2.20.0). No
  lo subo a MEDIDO porque no hice control positivo del comando; para efectos prácticos las líneas
  citadas son las de `main`.
- Huma `v2.37.3` (`go.mod:10`), leído en la caché de módulos local.

### 0.3 Alias de rutas (para que las citas quepan)

| Alias | Ruta |
|---|---|
| `PVW` | `msp-app-kt/app/src/main/java/com/example/msp_app/workers/PendingVisitsWorker.kt` |
| `PPW` | `msp-app-kt/app/src/main/java/com/example/msp_app/workers/PendingPaymentsWorker.kt` |
| `VUC` | `msp-app-kt/app/src/main/java/com/example/msp_app/features/visit/upload/domain/VisitUploadClassifier.kt` |
| `UD` | `msp-app-kt/core/upload/src/main/kotlin/com/example/msp_app/core/upload/UploadDecision.kt` |
| `VDAO` | `msp-app-kt/core/database/src/main/kotlin/com/example/msp_app/core/database/dao/visit/VisitDao.kt` |
| `VLDS` | `msp-app-kt/app/src/main/java/com/example/msp_app/data/local/datasource/visit/VisitsLocalDataSource.kt` |
| `RVU` | `msp-app-kt/core/common/src/main/kotlin/com/example/msp_app/core/common/sync/pendingwork/domain/usecases/ReconcileVisitsUseCase.kt` |
| `SAPW` | `msp-app-kt/core/common/src/main/kotlin/com/example/msp_app/core/common/sync/pendingwork/domain/usecases/SyncAllPendingWorkUseCase.kt` |
| `VPS` | `msp-app-kt/app/src/main/java/com/example/msp_app/core/sync/pendingwork/data/synchronizers/VisitsPendingSynchronizer.kt` |
| `WMU` | `msp-app-kt/app/src/main/java/com/example/msp_app/workmanager/WorkManagerUtils.kt` |
| `V2API` | `msp-app-kt/app/src/main/java/com/example/msp_app/data/api/services/visits/V2VisitsApi.kt` |
| `RVA` | `msp-app-kt/app/src/main/java/com/example/msp_app/data/visitas/RegistroDeVisitaAdapter.kt` |
| `IDV` | `msp-app-kt/feature/visitas/src/main/kotlin/com/example/msp_app/feature/visitas/domain/IdsDeLaVisita.kt` |
| `ECD` | `msp-app-kt/core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt` |
| `SEG` | `msp-app-kt/feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/SegmentoDeCobranza.kt` |
| `SDAO` | `msp-app-kt/core/database/src/main/kotlin/com/example/msp_app/core/database/dao/sale/SaleDao.kt` |
| `REG` | `msp-api/internal/visitas/app/registrar_visita.go` |
| `RCI` | `msp-api/internal/visitas/app/registrar_visita_con_imagenes.go` |
| `MPH` | `msp-api/internal/visitas/infra/visitashttp/multipart_helpers.go` |
| `HND` | `msp-api/internal/visitas/infra/visitashttp/handlers.go` |
| `RTS` | `msp-api/internal/visitas/infra/visitashttp/routes.go` |
| `VREPO` | `msp-api/internal/visitas/infra/visitasfb/repo.go` |
| `FIW` | `msp-api/cmd/api/failedintent_wiring.go` |
| `FI` | `msp-api/internal/platform/failedintent/failedintent.go` |
| `WM` | `msp-api/internal/cobranza/infra/ventfb/watermark.go` |
| `PH` | `msp-api/internal/cobranza/infra/ventfb/page_helpers.go` |
| `PREPO` | `msp-api/internal/cobranza/infra/ventfb/pagos_repo.go` |
| `HUMA` | `$GOMODCACHE/github.com/danielgtaylor/huma/v2@v2.37.3/huma.go` |

---

## 1. Contexto y riesgo

### 1.1 Qué se construye

Tres partes, en este orden y **sin saltarse ninguna compuerta**:

- **P1 — Envío de visitas blindado.** La visita capturada en el teléfono llega al servidor o queda
  resguardada en la bandeja de intentos fallidos; nunca se marca enviada sin prueba.
- **P2 — Promesas y citas como hechos de la visita.** Cinco columnas nuevas en `MSP_VISITAS`; el
  estado del compromiso (vigente, cumplido, incumplido, reemplazado) **se deriva, nunca se guarda**;
  una agenda por zona que el teléfono baja por un canal con cursor del mismo tipo que pagos; el
  escritorio lee la misma regla.
- **P3 — Tareas de oficina y avisos.** La oficina asigna "visitar esta venta"; llega a toda la zona;
  se cierra sola. Alarmas locales en el teléfono y push FCM **solo como despertador**.

### 1.2 Por qué es el cambio más delicado del sistema

El dueño arriesga su empleo si se pierde, duplica o corrompe información. Ya pasó con pagos:

- 2026-08-13: dos pagos marcados como enviados en el teléfono sin existir en el servidor
  (`docs/module-standards/ENTREGA_GARANTIZADA.md`, sección "Origen"). LEÍDO.
- 2026-08-15: **siete defectos simultáneos en producción con la suite en verde**
  (`docs/COBRANZA-SYNC.md:21-23`). LEÍDO.
- 2026-08-31: un pago con `GUARDADO_EN_MICROSIP = 1` que el servidor nunca recibió, con la versión
  "arreglada" ya instalada (memoria del proyecto `project_pago_marcado_enviado_sin_llegar_20260831`).
  REPORTADO por la memoria, no re-verificado.

Hoy el envío de visitas tiene **exactamente la forma** del defecto que perdió esos pagos (§3.2, H1).
Y P2 convierte a la visita en portadora de una promesa: si una visita con promesa se pierde, el
cobrador no vuelve el día prometido, o vuelve cuando ya pagaron. No mueve dinero (§7.2), pero decide
**a qué puerta se toca**, que es cobranza. Por decisión del dueño se trata con la ruta §8 completa.

### 1.3 Lo que este documento NO promete

- **Certeza absoluta no existe.** Un teléfono destruido con visitas sin subir pierde esas visitas;
  ningún diseño lo evita (`ENTREGA_GARANTIZADA.md`, "Casos fuera de alcance, declarados").
- Lo que sí se promete: (a) mientras el aparato exista y vuelva a tener red, ninguna visita se
  pierde ni se duplica; (b) **si algo diverge, se detecta el mismo día** por la reconciliación
  diaria (capa 5, §4.6) y no meses después por un cobrador que se queja.

---

## 2. Principios y la garantía, nombrada con honestidad

### 2.1 La garantía

> **Al-menos-una-vez + deduplicación por UUID en el servidor = efectivamente-una-vez.**

"Exactamente-una-vez" no existe como propiedad de la red
(`docs/investigacion/2026-10-15-sync-offline-practicas.md:325`). Lo alcanzable es: el teléfono
reintenta hasta tener prueba; el servidor reconoce el reintento por el UUID (PK) y no inserta dos
veces. Se nombra así en todo el código y en los mensajes al dueño; nadie escribe "exactly once".

### 2.2 Principios no negociables

1. **Nada se suelta sin prueba del servidor** (`ENTREGA_GARANTIZADA.md`, "El invariante"): «la
   apliqué» (existencia **completa**, §4.2) o «la tengo guardada para corregir» (`X-Intent-Captured`).
2. **Un solo lugar decide** qué hacer con una respuesta: `classifyUpload` de `:core:upload`
   (`UD:66-78`). Visitas deja de tener tabla propia.
3. **El estado del compromiso se deriva, nunca se guarda.** Ni en Firebird ni en Room. Lo que se
   guarda son hechos inmutables: la visita con su promesa/cita, el pago, la tarea, la cancelación.
4. **Sin lógica en la base** (CLAUDE.md §1): IDs, timestamps y toda regla viven en Go/Kotlin.
5. **El resync nunca toca lo pendiente.** La tabla de salida del teléfono (`Visit` con
   `GUARDADO_EN_MICROSIP = 0`) y la tabla de lo bajado del servidor son **tablas distintas** (§5.4.8).
6. **La regla duplicada (Kotlin/Go) se amarra con casos compartidos**, no con revisión de código
   (precedente E-APP-057).
7. **Ausencia no es hallazgo** sin control positivo (CLAUDE.md §8).

### 2.3 Invariantes (I1–I6 de la investigación, I7–I10 propios)

Se verifican en **cada paso** de la simulación (§8.1), no solo al final.

| ID | Invariante | Origen |
|---|---|---|
| I1 | Toda visita capturada termina en `MSP_VISITAS` **o** en `MSP_FAILED_INTENTS` con su motivo. | investigación §2.A |
| I2 | Ningún UUID aparece más de una vez en el servidor; una captura (semilla × venta) genera exactamente un UUID. | investigación §2.A; `IDV:66-67` |
| I3 | Ninguna visita pasa a "entregada" en el teléfono sin que el servidor la haya confirmado **por su UUID y con sus campos de compromiso**. | investigación §2.A, endurecido aquí |
| I4 | Para la misma zona y el mismo `desde`, sync, inventario (digest/ids) y by-ids de zona entregan conjuntos **idénticos**. | `COBRANZA-SYNC.md:138-141` |
| I5 | Para el mismo historial (visitas, pagos, tareas, "ahora"), el estado derivado del compromiso es idéntico en Go y Kotlin. | investigación §2.A |
| I6 | El cursor nunca retrocede y nunca avanza más allá de lo persistido en la misma transacción de Room. | investigación §2.A; `COBRANZA-SYNC.md:36-39` |
| I7 | Ningún camino nuevo escribe en tablas de dinero (servidor) ni en columnas de dinero (teléfono). | capa 2 |
| I8 | Un resync (epoch, cambio de zona, reinstalación) no borra ni modifica ninguna fila de `Visit` con `GUARDADO_EN_MICROSIP = 0`. | investigación P5 |
| I9 | Ningún estado de compromiso o de tarea está persistido; borrar todo lo derivado y recalcular da lo mismo. | decisión del dueño |
| I10 | Un reenvío nunca borra en el servidor un dato que ya estaba (la ausencia de un campo no es "ponlo en nulo"). | §5.2 |

### 2.4 Las cinco capas de seguridad (decisión del dueño, obligatorias)

| Capa | Qué es | Dónde se concreta |
|---|---|---|
| 1. Por construcción | Nada se suelta sin prueba del servidor. | §4.2, §5.2 |
| 2. Aislamiento | El trabajo no toca código de pagos/saldos; un chequeo automático bloquea el diff. | §7 |
| 3. Simulación y caos | Simulación determinista con fallas, propiedades, Toxiproxy, mutación, agente adversario. | §8 |
| 4. Medir antes, desplegar despacio | Consultas solo-lectura en producción; servidor primero; canario de 1–2 teléfonos; kill-switch. | §10, §11 |
| 5. Reconciliación diaria | Teléfono↔servidor, automática, con alerta el mismo día. | §4.6, §12 |

---

## 3. Estado actual y huecos

### 3.1 El flujo de hoy

```
 TELÉFONO (Room v33)                                   SERVIDOR (msp-api)
 ───────────────────                                   ──────────────────
 RegistroDeVisitaAdapter.guardar  (RVA:168-186)
   withTransaction {
     Visit INSERT REPLACE  (VDAO:78-81)
       ID = UUID v3(semilla/venta)  (IDV:66-67)
       FECHA = reloj del teléfono   (RVA:313)
       PROMESA_*, CITA_*  ── solo Room ──────────────X  (no viajan: V2API:126-140)
     sales.updateTotal(venta, 0.0) (VLDS:99 → SDAO:281-289)
   }
   enqueueUniqueWork KEEP "sync_pending_visit_<id>" (WMU:147-162)
                │
                ▼
 PendingVisitsWorker.uploadV2 (PVW:100-177) ── POST /v2/visitas ──► CaptureMiddleware (FIW:244-255)
   2xx ─────────────────────► markDone                               Huma op, sin MaxBodyBytes (RTS:194-207)
   4xx (salvo 401/408/425/429)► markDone  ◄── H1                     decode JSON estricto (MPH:124)
   409 ─────────────────────► markDone  ◄── H1                       RegistrarVisita (REG:38-59)
   5xx intento 10 ──────────► markDone  ◄── H1                         id repetido → 201 fila VIEJA (REG:44-54) ◄── H6
   IOException ─────────────► retry                                   INSERT MSP_VISITAS (VREPO:56-63)
                                                                      anunciarVisitaConfirmada → SSE (service.go:117-122)
 Reencolado de pendientes:
   SyncAllPendingWork, 1 vez por proceso (SAPW:96), máx 50 sin ORDER BY (VPS:23, VDAO:105) ◄── H3
 Reconciliador (cada 15 min + SSE, WMU:311,339):
   by-ids solo de GUARDADO=0 (RVU:75-103) ──────── GET /v2/visitas/by-ids (ids, sin zona)
   nunca revisa las GUARDADO=1  ◄── H2
 Poda en "sincronizar ventas" (SalesViewModel.kt:163 → VDAO:294-305):
   borra GUARDADO=1 sin compromiso; con compromiso, 90 d tras su fecha (VLDS:211)
 Bajada de visitas: NO EXISTE  ◄── H8
```

### 3.2 Huecos, con evidencia

| # | Hueco | Evidencia | Nivel | Daño |
|---|---|---|---|---|
| H1 | El teléfono marca la visita enviada (`GUARDADO_EN_MICROSIP = 1`) ante **cualquier 4xx** salvo 401/408/425/429, ante **409**, y ante **5xx en el intento 10**; sin mirar `X-Intent-Captured` ni hacer GET de existencia. | `VUC:66-100` (409 → DONE en :86, 5xx → RETRY_THEN_DONE en :92, 4xx → DONE en :95); `PVW:116-147`; tope `DEFAULT_MAX_ATTEMPTS = 10` (`PVW:297`). Pruebas que **fijan** el defecto: `PendingVisitsWorkerV2Test.kt:292` (`v2_422_validation_marks_done_because_captured_server_side`), `:317` (`v2_403_marks_done`), `:447` (`v2_5xx_at_cap_marks_done`). | LEÍDO | Visita perdida en silencio cuando el 4xx/5xx no quedó capturado (pool trabado: falla la petición y la captura a la vez — el caso que perdió los pagos, `UD:59-63`). |
| H1b | Tiempo hasta el "DONE" por 5xx: ~4.3 h. | Sin `setBackoffCriteria` en `WMU:147-162` ⇒ backoff por omisión de WorkManager (exponencial, 30 s, según la investigación P3). 30 s × (2⁰+…+2⁸) = 15,330 s ≈ 4.26 h. | INFERIDO (lo vuelve MEDIDO: prueba con `TestDriver` de work-testing que cuente el retraso acumulado) | Una caída de 5 h del servidor suelta visitas. |
| H2 | El reconciliador solo pregunta por las pendientes; una visita marcada 1 por error **nunca** se vuelve a revisar y luego la poda la borra. | `RoomPendingVisitsStore.kt:22` usa `getPendingVisits` (`GUARDADO_EN_MICROSIP = 0`, `VDAO:105`); poda `VDAO:294-305` desde `SalesViewModel.kt:163`. | LEÍDO | La evidencia local del error desaparece; nadie puede reconstruirla. |
| H3 | Reencolado de pendientes **una vez por proceso**, máximo 50, sin orden. | Compuerta por proceso `InMemorySessionSyncGate.kt:14-24` usada en `SAPW:96`; `take(MAX_ITEMS_PER_SYNC)` en `VPS:23`, `MAX_ITEMS_PER_SYNC = 50` (`SAPW:121`); `getPendingVisits` sin `ORDER BY` (`VDAO:83-108`). El reconciliador periódico solo marca, no encola (`RVU:48-53`). | LEÍDO | Con >50 pendientes y la app viva días, las 51+ no se reintentan (salvo el botón manual), y cuáles quedan fuera depende del plan de SQLite. |
| H4 | Promesa y cita **solo existen en Room**. | Columnas en `VDAO:27-31`; `CrearVisitaBody` sin ellas (`V2API:126-140`); `MSP_VISITAS` sin ellas (`000047_create_msp_visitas.up.sql:70-84`); el propio teléfono lo documenta (`VLDS:172-192`). | LEÍDO | Teléfono perdido o reinstalado = promesas perdidas; el escritorio no las ve. |
| H5 | `DisallowUnknownFields` en el JSON de visitas: un APK que mande campos nuevos a un API viejo recibe 422. | `MPH:123-124`. | LEÍDO | Con H1, el teléfono **soltaría** esas visitas; con P1 sin H5 resuelto, irían a la bandeja en vez de a la tabla. |
| H6 | Reenvío del mismo id con **datos distintos** → 201 con la fila vieja; lo nuevo se descarta en silencio. | `REG:44-54`. | LEÍDO | Un backfill de promesas mostraría éxito sin guardar nada. |
| H7 | La operación `POST /visitas` no declara `MaxBodyBytes`; Huma pone 1 MiB por omisión y responde 413 al alcanzarlo. | `RTS:194-207`; `HUMA:1378-1382` (`op.MaxBodyBytes = 1024 * 1024`) y `HUMA:2127-2128` (413 cuando `count == maxBytes`). El middleware global es de 10 MB (`internal/platform/middleware/middleware.go:172-179`, `config.go:428`), así que el tope efectivo es el de Huma. | LEÍDO (que en producción una visita con fotos ≥ 1 MiB reciba 413 es INFERIDO; lo vuelve MEDIDO: petición de 1.1 MiB contra dev, y la consulta Q4 en producción). **Corrige** la auditoría de hoy, que suponía ">10 MB → 500". | Visitas con fotos grandes rechazadas; hoy el teléfono las suelta (H1). |
| H8 | No hay canal de bajada de visitas: el teléfono no sabe lo que otro cobrador de la zona registró. | `RVU:21-25` ("the phone never *downloads* visitas"). | LEÍDO | Sin bajada no hay agenda por zona. |
| H9 | by-ids confirma la visita, no sus fotos. | `V2API:108-109` devuelve solo ids; `VDAO:229-236` retiene la marca si hay foto local sin `SUBIDA_EN`, pero el servidor no dice qué fotos tiene. | LEÍDO | Una foto puede quedarse sin entregar sin que nadie lo sepa (mitigado por la retención de `VDAO:229-236`). |
| H10 | `/v2/visitas` no tiene extractor de resumen en la bandeja: las filas salen sin módulo ni resumen. | `FIW:143-150`, `:249-252`. | LEÍDO | La oficina no puede leer ni corregir con rapidez una visita rechazada. |
| H11 | `cleanupBlobs` puede borrar el archivo de una fila que **sí** quedó. | Dos peticiones concurrentes con la misma visita y la misma foto escriben la misma clave determinista (`RCI:72-77`); si la segunda falla dentro de su transacción por otra causa, su limpieza (`RCI:126-128`, `:245-250`) borra la clave que la primera ya confirmó. | INFERIDO (lo vuelve MEDIDO: prueba de concurrencia con falla inyectada en la segunda transacción y verificación del archivo) | Fila de imagen apuntando a archivo inexistente. |
| H12 | `MSP_VISITAS` no tiene `UPDATED_AT` ni `CREATED_BY`. | `000047_create_msp_visitas.up.sql:19-27`, `:70-84`. | LEÍDO | Sin `UPDATED_AT` no hay cursor. |
| H13 | Duplicados lógicos heredados del cliente viejo (UUID nuevo por envío). | REPORTADO hoy: 15,022 filas duplicadas en dev. LEÍDO en `docs/evidencia/infra.md:641-644` (dentro de E-INF-014): tres pares duplicados en dev (mismo cliente, venta, segundo, cobrador 2991009). Hoy el id es UUID v3 determinista (`IDV:66-67`). | REPORTADO / LEÍDO | Cuentas dobles en desempeño y, con P2, promesas dobles. |
| H14 | No hay telemetría del worker de visitas a Firestore (pendientes, antigüedad). | REPORTADO por la auditoría de hoy; yo solo leí que el reconciliador reporta errores (`RVU:184-203`), no conteos de pendientes. | REPORTADO | Una flota con visitas atoradas es invisible. |
| H15 | Auto Backup activo con reglas de ejemplo: la base de Room se respalda y se restaura al reinstalar. | `AndroidManifest.xml:30-32` (`allowBackup="true"`); `res/xml/data_extraction_rules.xml` y `backup_rules.xml` solo traen el ejemplo comentado. | LEÍDO (que restaure una base vieja es INFERIDO; lo vuelve MEDIDO: `adb shell bmgr backupnow` + reinstalación en emulador) | Reinstalar podría resucitar pendientes ya entregadas (inofensivo por idempotencia) o un cursor viejo (inofensivo) o **la base de otro cobrador** en un teléfono reasignado (grave). |
| H16 | Un teléfono con el reloj adelantado más de 48 h recibe 422 `visita_fecha_futura`. | `internal/visitas/domain/visita.go:25`, `:126-127`. | LEÍDO | Hoy se suelta (H1); con P1 va a la bandeja. Hay que medir cuántas (Q1). |
| H17 | Al capturar una visita el teléfono ejecuta un `UPDATE` sobre una columna de dinero (`SALDO_REST = SALDO_REST - 0.0`). | `VLDS:98-99` → `SDAO:281-289`. | LEÍDO (que sea neutro es INFERIDO, §7.2) | Ver §7.2. **No se toca**; se fija con prueba. |
| H18 | El cliente HTTP no configura `retryOnConnectionFailure`; OkHttp lo trae encendido por omisión. | Búsqueda de `retryOnConnectionFailure` en `msp-app-kt` sin resultados (control positivo pendiente: la misma búsqueda sobre una cadena que sí existe). | INFERIDO | Dos capas que reintentan (investigación P3). Inofensivo por idempotencia, pero ensucia la bandeja y el conteo de intentos. |

### 3.3 Lo que ya está bien y NO se toca

- UUID determinista por (semilla, venta), estable entre corridas (`IDV:26-45`). LEÍDO.
- Visita y su enlace con la recomendación en una sola transacción; encolado fuera, con
  `NonCancellable` (`RVA:174-185`). LEÍDO.
- Fallo al leer las fotos ⇒ no se sube la visita (`PVW:102-110`, `:209-248`). LEÍDO.
- Servidor idempotente por id también con fotos (`RCI:58-83`). LEÍDO.
- La ruta está bajo el middleware de captura con blob, así que un rechazo multipart deja fila
  (`FIW:232-255`; `TestE2E_VisitasMultipart_UnaVisitaRechazadaConFotoSeCaptura`). LEÍDO.
- `GET /v2/visitas/{id}` existe, con el permiso del rol que captura (`HND:171-190`,
  `auth.PermCobranzaVerPagos`). LEÍDO. Es la prueba de existencia que P1 necesita.

---

## 4. Parte 1 — envío de visitas blindado

### 4.1 La regla

Idéntica a pagos (`PPW:40-62`, `PPW:190-232`, `ENTREGA_GARANTIZADA.md`, tabla de decisión), con un
endurecimiento: la **existencia completa** (§4.2) en lugar de la existencia a secas, porque desde P2 la
visita lleva datos (compromiso, fotos) que pueden faltar en una fila que sí existe.

### 4.2 Teléfono

1. **Antes de subir**: si la visita ya está entregada, no se vuelve a mandar (como `PPW:101-114`).
   "Entregada" pasa a significar `CONFIRMADA_EN IS NOT NULL` (columna nueva, punto 7), no
   `GUARDADO_EN_MICROSIP = 1`.
2. **Éxito (2xx)**: dentro de `withContext(NonCancellable)` (como `PPW:164-169`): estampar
   `SUBIDA_EN` en las fotos que viajaron, marcar `GUARDADO_EN_MICROSIP = 1` y `CONFIRMADA_EN`. Hoy
   esa sección no es `NonCancellable` (`PVW:111-115`). El cuerpo 2xx debe traer el **mismo id** que
   se mandó; si no, se trata como error indeterminado (no se suelta). "HTTP 200 no es éxito" (memoria
   `feedback_verify_logs_on_pago_inject`).
3. **Error HTTP**: primero la **prueba de existencia completa**:
   `GET /v2/visitas/{id}` (P1) o `GET /v2/visitas/by-ids?ids=…&detalle=1` (P2, §5.2.4).
   - 200 y (sin P2 o con los campos de compromiso iguales a los locales) y (todas las fotos locales
     pendientes figuran en `imagen_ids`) ⇒ suelta con log `RECONCILED_VIA_GET`.
   - 200 pero falta el compromiso o una foto ⇒ **no** suelta por existencia; sigue a la tabla.
   - 404 ⇒ sigue a la tabla. Cualquier otra cosa ⇒ indeterminado, sigue a la tabla.
4. **Tabla**: `classifyUpload(code, reachedMspApi, captureConfirmed)` de `UD:66-78`, sin cambios.
   `RELEASE` ⇒ marca entregada (con `CONFIRMADA_EN` = nulo y `RESGUARDADA_EN` = ahora, columna nueva:
   la visita está en la bandeja, no en la tabla). `RETRY` ⇒ `Result.retry()`, **sin tope**.
5. **Se borra** `VisitUploadClassifier` y sus tres pruebas que fijan el defecto (`H1`). Se reemplazan
   por las pruebas de `ENTREGA_GARANTIZADA.md` §"Pruebas que no pueden faltar", lado teléfono, más:
   `5xx sostenido 12 h no suelta`, `409 nunca suelta sin existencia completa`,
   `200 con id distinto no suelta`, `existencia sin compromiso no suelta`.
6. **Reencolado** (cierra H3): el reconciliador periódico (cada 15 min, `WMU:311`) también reencola
   **todas** las pendientes, en orden `FECHA ASC, ID ASC`, con `KEEP`. Disparadores adicionales: abrir
   la app y red recuperada (investigación P7). Se quita el tope de 50 del camino de visitas (el tope
   existía para "borrar datos y reinstalar"; con `KEEP` y nombre único por id no hay amplificación).
   El `SessionSyncGate` se conserva para los otros cuatro sincronizadores (aislamiento: `SAPW` no se
   toca; el reencolado nuevo vive en el worker de reconciliación de visitas).
7. **Room v33 → v34** (solo tablas de visitas): `Visit` gana `CONFIRMADA_EN TEXT NULL` y
   `RESGUARDADA_EN TEXT NULL`. Migración que no reescribe filas: las existentes quedan en nulo y la
   primera reconciliación inversa (§4.6) las confirma o las devuelve a pendiente.
8. **Poda segura** (cierra H2): `deleteUploadedVisits` exige además `CONFIRMADA_EN IS NOT NULL` y
   `CONFIRMADA_EN < ahora − retención` (DA-4). Una visita `RESGUARDADA_EN` no se poda hasta que la
   oficina la resuelva y el servidor la tenga, o pasen 90 días (y entonces se reporta antes de podar).
9. **Auto Backup** (cierra H15): excluir el dominio `database` en `<cloud-backup>` y
   `<device-transfer>` de `data_extraction_rules.xml`, y en `backup_rules.xml` (API < 31).
10. **Telemetría** (cierra H14), sin datos personales (mismo criterio que `COBRANZA-SYNC.md:506-511`):
    por corrida, `visitas.pendientes` (conteo), `visitas.pendiente_mas_vieja_min`,
    `visitas.resguardadas`, `visitas.confirmadas_24h`, versión de app, y los cuatro permisos de §6.6.

### 4.3 Servidor

1. **`MaxBodyBytes`** explícito en `POST /visitas` igual al tope global (`HTTP_MAX_BODY_SIZE_MB`),
   **después** de medir H7 (Q4, prueba de 1.1 MiB en dev). Hasta medirlo, nada cambia.
2. **Extractor de resumen** para `/v2/visitas` (cierra H10): una línea en `FIW:146-150` y su
   implementación en `internal/visitas/infra/failedintents/` (cliente, venta, tipo, fecha; nunca la
   nota completa). Es el único cambio permitido en `FIW` (§7.1).
3. **Limpieza de blobs sin pisar a otros** (cierra H11): la clave de almacenamiento gana un sufijo por
   intento: `visitas/<visita_id>/<imagen_id>-<nonce><ext>`. La limpieza de un intento solo borra **sus**
   claves; una fila confirmada siempre apunta a la clave de su propio intento. Los huérfanos los barre
   el conserje que `RCI:241-244` ya prevé. Antes del cambio, H11 se mide (prueba de concurrencia).
4. **Prueba de existencia completa**: `GET /v2/visitas/{id}` agrega `imagen_ids` y (P2) los cinco
   campos de compromiso. Aditivo: el teléfono viejo ignora campos extra (Gson, `V2API:142-161`).
5. **Rechazo por reloj** (H16): sin cambio de regla en P1; se mide (Q1) y se decide aparte.

### 4.4 Qué hace el teléfono con la bandeja

Una visita `RESGUARDADA_EN` se muestra en la pantalla de pendientes como "en revisión de oficina"
(no "enviada"). Cuando la oficina la reaplica desde la bandeja y el servidor la inserta, la
reconciliación inversa la confirma (`CONFIRMADA_EN`). Si la oficina la descarta, se queda resguardada
hasta la retención de DA-4 y se reporta en la conciliación diaria.

### 4.5 Por qué esto cierra I1 e I3

- I1: cada salida del worker termina en `CONFIRMADA_EN` (está en `MSP_VISITAS`, probado por GET o
  by-ids), `RESGUARDADA_EN` (hay fila en `MSP_FAILED_INTENTS`, probado por la cabecera, que solo se
  emite si `Store.Save` devolvió nil, `FI:61-69`) o sigue pendiente. No existe una cuarta salida.
- I3: las únicas escrituras de `CONFIRMADA_EN`/`RESGUARDADA_EN` están en dos funciones; una prueba de
  arquitectura (búsqueda en el código) falla si aparece una tercera.

### 4.6 Reconciliación diaria (capa 5) — inversa y directa

Una vez al día (y al abrir la app si pasaron > 24 h), por teléfono:

1. **Inversa**: todas las visitas locales con `GUARDADO_EN_MICROSIP = 1` de los últimos N días
   (DA-4) se preguntan a `by-ids?detalle=1` en tandas de 100 (`maxIDsPorRequest`,
   `handlers_visitas_existentes_por_ids.go:65`). Si el servidor **contestó la tanda** y la visita no
   está: vuelve a pendiente (`GUARDADO_EN_MICROSIP = 0`, `CONFIRMADA_EN = NULL`) y se reporta
   `visitas.reconcilia.devuelta_a_pendiente` (alerta). Si está pero con compromiso distinto o sin
   fotos: se reporta `visitas.reconcilia.incompleta` y se reenvía (el servidor rellena nulos, §5.2).
   Una tanda que falló no decide nada (mismo criterio que `RVU:105-110`).
2. **Directa (P2)**: el teléfono manda al servidor el resumen de su agenda derivada (por venta: id del
   compromiso vigente y su estado, sin datos personales) y el servidor la compara con la suya
   (§5.5). Diferencias ⇒ alerta el mismo día.
3. Resultado por teléfono: `visitas.conciliacion {revisadas, faltantes, incompletas, diferencias_agenda}`.

---

### 4.7 Motivo del último fallo por pago y por visita (pedido del dueño, 2026-10-16)

Hoy (LEÍDO) `Payment` y `Visit` solo guardan `GUARDADO_EN_MICROSIP` (+ `DOCTO_CC_ID = -1` en pagos);
los workers escriben el error solo en logcat (`PendingPaymentsWorker.kt:175-181,224-230`,
`PendingVisitsWorker.kt:116-175`). Control positivo: `RemoteLogger|logger.` da 0 en ambos workers y 18
en `PendingLocalSalesWorker`. Ventas locales sí lo guardan (`LocalSaleEntity.kt:41-45`, migración
25→26) y lo pintan (`UnifiedSalesScreen.kt:809-814`), pero ahí `LAST_UPLOAD_PERMANENT` **decide**
pendientes (`LocalSaleDao.kt:30,319`): eso **no** se copia. El servidor ya captura 4xx/5xx en
`MSP_FAILED_INTENTS` (`cmd/api/failedintent_wiring.go:228,253,291-292`); lo que solo sabe el teléfono
(INFERIDO; MEDIDO con corte de red en dev + control positivo de un 422): red/DNS/TLS, tiempo agotado,
5xx/HTML del túnel, comprobantes ilegibles (`PendingPaymentsWorker.kt:147`), `Store.Save` fallido, app
cerrada a la mitad.

Diseño:
- **Tabla nueva** `envio_diagnostico (TIPO 'pago'|'visita', FILA_ID, ULTIMO_ERROR_CODIGO,
  ULTIMO_ERROR_MENSAJE, ULTIMO_ERROR_HTTP, INTENTOS, ULTIMO_INTENTO_AT)`, PK (TIPO, FILA_ID); entra en
  v34 (§4.2 punto 7) como tabla nueva, sin reescribir filas y sin tocar `PaymentEntity` (vedado §7.1).
- Visitas: la escribe el worker de visitas en cada salida.
- Pagos: interceptor OkHttp (`RetrofitClientFactory.kt:121-131`, no vedado) solo sobre POST
  `/v2/cobranza/pagos` con `Idempotency-Key`; usa `peekBody`, registra también el fallo de red y
  re-lanza; todo dentro de `try/catch` que nunca altera la respuesta. Clasificación (código, mensaje)
  en función pura probada sin Room.
- **Regla dura:** ninguna consulta de envío o de pendientes lee esta tabla; prueba de arquitectura que
  falla si alguien la lee fuera de la pantalla de pendientes, el depurador remoto o la telemetría.
- Se ve en: pantalla de pendientes (motivo), depurador remoto de Room, `RemoteLogger` (id + código,
  sin datos personales).
- Riesgo: el interceptor está en el camino de pagos → cambio de dinero (§8): dos compuertas, entrada
  E-APP, prueba de que una excepción dentro del interceptor no cambia la decisión del worker, y prueba
  MockWebServer de que `X-Intent-Captured` sigue legible tras `peekBody`.

## 5. Parte 2 — promesas y citas como hechos de la visita

### 5.1 Modelo de datos (sin lógica en la base)

Migración `0000NN_msp_visitas_compromisos` (número = siguiente libre al escribirla; hoy el último es
`000065`). Todo `ADD` de columna nullable, que no reescribe filas (precedente
`000061_failed_intents_modulo_resumen.up.sql:53-66`).

| Columna | Tipo | Notas |
|---|---|---|
| `PROMESA_VENTA_ID` | `INTEGER` | `DOCTO_CC_ACR_ID` de la venta prometida. |
| `PROMESA_FECHA` | `DATE` | Fecha **civil** del negocio, sin zona horaria. |
| `PROMESA_MONTO_CENTAVOS` | `BIGINT` | Centavos enteros, nunca flotante (`RVA:57-61`). |
| `CITA_FECHA` | `DATE` | Civil. |
| `CITA_HORA` | `CHAR(5) CHARACTER SET ASCII` | `HH:MM` civil, como en Room (`RVA:427-431`). Validado en Go. |
| `UPDATED_AT` | `TIMESTAMP` | Cursor. Lo pone Go en cada `INSERT` y en cada relleno (§5.2.3), estampado **dentro** de la transacción (§5.4.4). Nulo en filas viejas hasta el relleno de §5.1.2. |
| `CREATED_BY` | `CHAR(36) CHARACTER SET ASCII` | Usuario autenticado que subió la visita. Nulo en filas viejas. |

Índices: `IDX_MSP_VISITAS_ZONA_UPD (ZONA_CLIENTE_ID, UPDATED_AT, ID)` para el cursor;
`IDX_MSP_VISITAS_PROM_VENTA (PROMESA_VENTA_ID)` e `IDX_MSP_VISITAS_IMPTE (IMPTE_DOCTO_CC_ID)` solo si
el `PLAN` medido los pide (`IDX_MSP_VISITAS_ZONA_FECHA` ya existe, `000065…up.sql:49`).

Prohibido (CLAUDE.md §1): `DEFAULT`, trigger, `CHECK` de negocio, procedimiento. El estado del
compromiso **no tiene columna**.

#### 5.1.1 `DATE` y la conversión del driver

Que `nakagami/firebirdsql v0.9.19` (`go.mod:18`) devuelva un `DATE` sin correrlo de día según la zona
del proceso es **SUPUESTO**. Compuerta: prueba de ida y vuelta en dev con `TZ` del proceso en
`America/Mexico_City` y en `UTC`, para fechas en los bordes de cambio de día. Si cualquier caso
difiere, las dos columnas de fecha pasan a `CHAR(10) CHARACTER SET ASCII` (`YYYY-MM-DD`) y la
comparación se hace en Go. La decisión queda tomada por el resultado de la prueba, no por gusto.

#### 5.1.2 Relleno de `UPDATED_AT` en filas viejas

- Solo filas con `FECHA >= hoy − V` (V de DA-3), con `UPDATED_AT = FECHA` (reparte los valores y evita
  el grupo empatado gigante del incidente 1, `COBRANZA-SYNC.md:48-54`; aun así el cursor es par).
- **En tandas** (≤ 10,000 filas por `COMMIT`), fuera de horario: una sentencia larga de lectura-
  escritura **congela el watermark de pagos para toda la flota** (`WM:47-60`; memoria
  `reference_isql_produccion_trampas` §6). Tras cada tanda:
  `SELECT COUNT(*) FROM MON$STATEMENTS WHERE MON$STATE = 2`.
- Es un relleno de dato, como el de `000064_msp_ventas_version.up.sql` (VERSION = 0); no es lógica.

### 5.2 Contrato de escritura (`POST /v2/visitas`, contrato de visitas 3)

#### 5.2.1 Negociación de capacidad (cierra H5 sin depender del orden de despliegue)

- Nuevo `GET /v2/visitas/capacidades` → `{"contrato": 3, "compromisos": true|false}`. `false` mientras
  la bandera de servidor `VISITAS_COMPROMISOS_ENABLED` esté apagada.
- El teléfono lo consulta al abrir sesión y cada 6 h. **Solo** si `contrato >= 3 && compromisos`
  manda los campos nuevos. Un 404 (API vieja) = contrato 2: manda el cuerpo de hoy y la visita queda
  con `COMPROMISO_PENDIENTE = 1` local para el relleno posterior (§5.2.5).
- Así, un APK nuevo contra un API vieja **nunca** recibe el 422 de `MPH:124`.

#### 5.2.2 Cuerpo

Se agregan cinco campos opcionales, planos, con los mismos nombres que Room:

```jsonc
{ /* …los 12 campos de hoy (dto.go:73-86)… */
  "promesa_venta_id": 13914738,        // opcional
  "promesa_fecha": "2026-10-20",       // obligatoria si viene cualquier promesa_*
  "promesa_monto_centavos": 15000,     // opcional, > 0
  "cita_fecha": "2026-10-18",          // opcional
  "cita_hora": "16:30"                 // opcional; exige cita_fecha
}
```

Validación (en dominio, códigos en inglés, mensajes en español):

| Regla | Código | HTTP |
|---|---|---|
| `promesa_fecha` con formato distinto de `YYYY-MM-DD` válido | `visita_promesa_fecha_invalida` | 422 |
| algún `promesa_*` sin `promesa_fecha` | `visita_promesa_incompleta` | 422 |
| `promesa_monto_centavos <= 0` | `visita_promesa_monto_invalido` | 422 |
| `promesa_venta_id <= 0` | `visita_promesa_venta_invalida` | 422 |
| `cita_hora` sin `cita_fecha` | `visita_cita_incompleta` | 422 |
| `cita_fecha`/`cita_hora` mal formadas | `visita_cita_invalida` | 422 |

**No** se rechaza una fecha civil en el pasado ni muy lejana: rechazar el valor perdería visitas (mismo
criterio que `HND:130-142` con las coordenadas). Lo raro se cuenta: métrica
`visitas.compromiso.fecha_atipica`.

#### 5.2.3 Reenvío del mismo id (cierra H6) — tabla completa

La promesa y la cita son dos **bloques** independientes; cada bloque se compara entero.

| Fila en servidor | Petición | Resultado | Respuesta |
|---|---|---|---|
| no existe | cualquiera válida | `INSERT` con `UPDATED_AT`/`CREATED_BY` | 201, fila |
| existe; bloque nulo | bloque presente | **rellena** ese bloque: `UPDATE … SET <bloque>, UPDATED_AT = ? WHERE ID = ? AND <columnas del bloque> IS NULL` | 201, fila completa |
| existe; bloque igual | bloque igual | nada | 201, fila |
| existe; bloque presente | bloque **ausente** | nada (I10: la ausencia nunca borra) | 201, fila |
| existe; bloque presente | bloque **distinto** | nada; la petición queda en la bandeja | **422** `visita_compromiso_distinto` (ver DA-1) + `X-Intent-Captured` |
| existe; algún campo base distinto | — | fase A: nada + log/métrica `visitas.reenvio.base_distinta {campo}`; fase B según DA-12 | fase A: 201, fila |

- El relleno es compare-and-set en la misma transacción. Si afecta 0 filas (otro reenvío rellenó en
  medio), se relee y se reevalúa **una** vez.
- "Base" = `CLIENTE_ID`, `COBRADOR_ID`, `FECHA` (al segundo, UTC), `TIPO_VISITA`,
  `IMPTE_DOCTO_CC_ID` (nulo ≡ 0), `ZONA_CLIENTE_ID`, `FORMA_COBRO_ID`, `NOTA` (NFC), `LAT`/`LNG`
  (tolerancia 1e-7). En fase A solo se mide: una normalización distinta entre teléfono y servidor no
  debe poder bloquear a nadie antes de conocer su frecuencia.
- El evento SSE se sigue anunciando en todos los caminos de éxito (`service.go:99-122`).

#### 5.2.4 Lectura

- `GET /v2/visitas/{id}`: agrega los cinco campos, `updated_at` e `imagen_ids`.
- `GET /v2/visitas/by-ids?ids=…&detalle=1`: arreglo de objetos
  `{id, promesa_venta_id, promesa_fecha, promesa_monto_centavos, cita_fecha, cita_hora, imagen_ids}`.
  Sin `detalle` responde exactamente lo de hoy (arreglo de ids) para los teléfonos viejos.
  **No** es uno de los tres canales de I4: su pregunta es "¿tienes lo que subí?", sin zona (Ruling B,
  `V2API:101-103`).

#### 5.2.5 Relleno (backfill) desde los teléfonos

- Cuando el teléfono ve `compromisos = true`, encola el reenvío de toda visita local con
  `PROMESA_FECHA` o `CITA_FECHA` no nulos y `COMPROMISO_CONFIRMADO_EN` nulo (columna nueva local),
  **con el mismo id**. El worker es el mismo de §4.2; la existencia completa exige los bloques iguales.
- Resultado esperado por fila: 201 con el bloque relleno, o 201 porque ya estaba igual, o 422
  `visita_compromiso_distinto` resguardado. Nunca "suelto sin guardar" (I3).
- Las visitas que solo existen en el servidor (subidas por el APK viejo y ya podadas del teléfono)
  pierden su promesa: ya no hay dónde leerla. Se cuentan (Q5, depurador remoto) para que el dueño sepa
  cuántas son **antes** del despliegue.

### 5.3 Reglas de dominio del compromiso (una sola definición, dos implementaciones)

Función pura en Go (`internal/visitas/domain/compromiso.go`) y en Kotlin
(`core/common/.../visitas/domain/Compromisos.kt`, archivo nuevo, **no** dentro de `ECD`):

```
DerivarCompromisos(cuentas, visitas, pagos, tareas, ahora, zonaNegocio) -> map[venta] EstadoCompromiso
```

"Ahora" y la zona de negocio se **inyectan**; ninguna de las dos lee el reloj.

#### 5.3.1 Definiciones

- **Visita aplicable a una venta V**: la misma regla de aplicabilidad del deriver actual — alcance
  VENTA con `IMPTE_DOCTO_CC_ID = V`, o alcance CLIENTE del cliente de V (Go `deriver.go:234-262`;
  Kotlin `ECD`, `IndiceVisitas`). Ver DA-6.
- **Compromiso de una visita**: su bloque promesa (si `PROMESA_FECHA` no es nula y
  `PROMESA_VENTA_ID` = V, o `PROMESA_VENTA_ID` nulo y la visita aplica a V) y/o su bloque cita.
- **Compromiso vigente de V**: el de la **visita aplicable más reciente** a V, si esa visita trae
  compromiso. Más reciente = mayor `FECHA`; empate ⇒ menor id en orden lexicográfico (el desempate ya
  fijado en E-INF-014, `docs/evidencia/infra.md:630-633`).
- **Instante del compromiso** = `FECHA` de su visita (UTC). **Día del compromiso** = `PROMESA_FECHA`
  o `CITA_FECHA` (civil). **Hora de la cita** = `CITA_HORA` civil en la zona de negocio.
- **Pago que cierra**: cualquier pago a V con instante posterior al instante del compromiso, de
  cualquier forma de cobro, **incluida la condonación** (forma 137026 / concepto 27969,
  `internal/desempeno/domain/formas_cobro.go:12-22`), **excluido el rechazado**. Ver DA-7 (pagos sin
  hora) y DA-8 (definición única de rechazado).

#### 5.3.2 Estados (derivados, nunca guardados)

Para el compromiso vigente de V, en este orden (el primero que aplique gana):

| Estado | Condición |
|---|---|
| `cumplida` | promesa: existe pago que cierra. |
| `cita_cumplida` | cita: existe pago que cierra **o** visita aplicable con instante ≥ inicio de la cita (día+hora civil; sin hora: inicio del día). |
| `vigente_futura` | día del compromiso > hoy (civil). |
| `vigente_hoy` | día del compromiso = hoy. |
| `incumplida` | día del compromiso < hoy y hoy − día ≤ 14. |
| `cerrada_sin_cumplir` | hoy − día > 14. No se muestra. |

`reemplazada` no es un estado del vigente: es lo que le pasa a un compromiso que **dejó de ser** el
de la visita más reciente; no se muestra.

Visibilidad: lo capturado antes del inicio de semana **sigue visible** en semanas posteriores (la
derivación no usa la ventana de la semana; hoy `ECD:105` y `deriver.go:130-136` sí la usan — por eso es
función nueva y no un cambio a esas).

Columnas (documento 1, `2026-10-15-desempeno-pulido-2-design.md:85-95`, alimentado aquí):
`vigente_hoy`, `incumplida` y **toda cita no cumplida** (incluidas las futuras) ⇒ "Por visitar";
promesa `vigente_futura` ⇒ "No paga"; el resto lo decide el documento 1.

#### 5.3.3 Cierre sin señal

El teléfono cuenta como pago que cierra también la **captura local pendiente** (aún sin subir), salvo
que esté rechazada. Es lo que permite que la promesa se cierre en el momento del cobro sin red. Cuando
el servidor rechaza esa captura, la promesa reaparece en la siguiente derivación (no hay estado que
"deshacer": I9).

#### 5.3.4 Casos compartidos (I5)

- Juego nuevo `testdata/compromisos/` en `msp-api` con copia idéntica en `msp-app-kt`, mismo mecanismo
  de `VERSION` (SHA-256 de los `.json` ordenados) que `testdata/estado-cuenta/` (E-APP-057).
- Mínimo obligatorio: cada fila de la tabla 5.3.2 en su borde (día = hoy, hoy+1, hoy−14, hoy−15);
  pago un segundo antes y un segundo después del compromiso; condonación; rechazado; captura local
  pendiente; promesa reemplazada por "no estaba" (alcance cliente) y por visita de otro cobrador;
  cita sin hora; cita con visita posterior sin pago; promesa sin fecha (no es compromiso, DA-16);
  dos visitas en el mismo segundo; cambio de día en la zona de negocio vs UTC (visita 23:30 local);
  pago de oficina sin hora (DA-7).
- `rapid` genera historiales; toda discrepancia Go↔Kotlin se congela como caso nuevo.

### 5.4 Canal de bajada de visitas por zona

Copia **deliberada** del protocolo de pagos (`COBRANZA-SYNC.md` §2–§4), con las diferencias marcadas.

#### 5.4.1 Endpoints (tres canales, I4)

| Canal | Ruta |
|---|---|
| Sync | `GET /v2/visitas/sync/zona/{zona_id}?desde=&cursor=&after_id=&limit=` |
| Inventario | `GET /v2/visitas/sync/zona/{zona_id}/digest?desde=` y `/ids?desde=&…` |
| Rescate | `GET /v2/visitas/sync/zona/{zona_id}/by-ids?desde=&ids=` |

Respuesta de sync: `{items, cursor, after_id, has_more, sync_epoch, server_now}`; `after_id` es el UUID
(string). Página vacía devuelve el **cursor recibido** y `after_id = null` = "sin posición nueva"
(`COBRANZA-SYNC.md:61-73`).

#### 5.4.2 Predicado único

```sql
v.ZONA_CLIENTE_ID = ?
AND v.UPDATED_AT IS NOT NULL
AND v.FECHA >= ?          -- desde: obligatorio con default de servidor = hoy − V (DA-3)
```

Una sola función Go arma este fragmento y la usan los tres canales y la derivación del servidor
(§5.5). Usa `IDX_MSP_VISITAS_ZONA_FECHA` (`000065…up.sql:49`); su `PLAN` se mide antes (E-VIS-015).
Ninguna tabla de enriquecimiento es `INNER JOIN` (`COBRANZA-SYNC.md:211-214`).

Consecuencia que se acepta por diseño: un compromiso de una visita con `FECHA < hoy − V` **no existe**
para la agenda en ningún lado (teléfono ni servidor). Así la paridad es por construcción.

#### 5.4.3 Cursor par

`ORDER BY v.UPDATED_AT, v.ID`; el teléfono persiste `(CURSOR, AFTER_ID)` **en la misma transacción de
Room que aplica la página** (investigación P4). Donde se escribe uno se escribe el otro; todo camino
que deje `CURSOR` nulo deja `AFTER_ID` nulo (`COBRANZA-SYNC.md:36-39`). El orden de `ID` en producción
(`CHAR(36) CHARACTER SET NONE`, `000063…up.sql:33-40`) contra dev (`ASCII`) debe medirse igual
(E-VIS-016): si difiere, el cursor salta filas.

#### 5.4.4 Commits tardíos — la marca segura

**Cómo lo hace pagos** (LEÍDO): `TX_ID < MinActiveTransactionID` (transacciones de lectura-escritura
activas, `WM:64-87`) y `UPDATED_AT <= CURRENT_TIMESTAMP − 1 s` (`PH:21`, `PH:212-258`,
`PREPO:382-414`); el `TX_ID` lo escriben triggers en cachés exentos por ADR-0006.

**Hallazgo lateral (INFERIDO, fuera de alcance, se reporta al dueño — DA-17):** esa combinación no
basta por sí sola. Contraejemplo: T10 empieza, T11 empieza; T11 escribe con `UPDATED_AT = 100`; T10
escribe con `UPDATED_AT = 101` y confirma; con T11 abierta la marca es 11, T10 (10 < 11) se entrega y
el cursor queda en 101; T11 confirma con 100 < cursor y el sync **ya no la entrega**. La red de pagos
es el inventario (digest/ids) del reconciliador. Lo vuelve MEDIDO: la prueba de dos transacciones de
la investigación P4 contra el canal de pagos en dev. **No se toca pagos aquí** (capa 2).

**Visitas exige lo mismo que pagos y más:**

1. En `MSP_VISITAS` no puede haber trigger (CLAUDE.md §1), así que no hay `TX_ID`. La marca es **por
   tiempo**: `upper = min(server_now − 1 s, inicio_rw_mas_viejo − 1 s)`, donde
   `inicio_rw_mas_viejo = MIN(MON$TIMESTAMP)` de `MON$TRANSACTIONS` con `MON$STATE = 1`,
   `MON$READ_ONLY = 0` y distinta de la propia (mismo filtro que `WM:64-87`).
2. Requisito para que la marca sea correcta: toda escritura de `UPDATED_AT` en `MSP_VISITAS` se estampa
   en Go **después** de que la transacción existe en el servidor (primera sentencia de la transacción:
   una lectura trivial que fuerza su inicio; luego `time.Now()`; luego el `INSERT`/`UPDATE`). Con eso,
   `UPDATED_AT ≥ inicio de su transacción`, y ninguna transacción abierta puede confirmar una fila por
   debajo de `upper`. Hoy `RCI:100` toma `now` **antes** de escribir los blobs y de abrir la
   transacción: el `UPDATED_AT` nuevo **no** puede reutilizar ese `now`.
3. Requisito de reloj: Go y Firebird en el mismo servidor; desfase medido (E-VIS-014). Si se separan,
   el margen sube al desfase medido + 1 s.
4. **Además**, el inventario (digest/ids) y el rescate (by-ids de zona) del reconciliador, como en
   pagos: si la marca fallara por una razón no prevista, el inventario lo detecta.
5. Escritores de `MSP_VISITAS`: solo este API. Que el backend Node ya no escriba en producción es
   **SUPUESTO**; se mide (Q6) antes de confiar en el requisito 2.
6. Prueba que lo vuelve MEDIDO (E-VIS-013): dos transacciones en dev, la que empezó primero confirma al
   final; el sync **aún** la entrega. Y la misma prueba con la marca desactivada debe ponerse roja
   (control positivo).

Costo conocido: una transacción de lectura-escritura larga de Microsip congela también esta bajada
(como congeló pagos el 2026-08-17, `WM:50-58`). Se vigila con la misma alerta (§12).

#### 5.4.5 Generación propia (`sync_epoch`)

- Filas nuevas en `MSP_CFG_SYNC_EPOCH` con `RECURSO = 'visitas'` y `'tareas'`, `ZONA_CLIENTE_ID = 0`,
  `EPOCH = 0`, `UPDATED_AT` literal (patrón de `000055…up.sql`, `MOTIVO` explicativo). `VARCHAR(20)`
  alcanza.
- Las seis reglas de `COBRANZA-SYNC.md:96-102` aplican tal cual (persistir solo al terminar, un
  reinicio por corrida, inválido = apagado, retroceso también replica, no persistir si cambia a media
  paginación).
- **Se sube siempre que cambie el `WHERE` o la proyección** (`COBRANZA-SYNC.md:108-114`), incluido
  cambiar V.
- Nunca se suben los epochs de `pagos` o `ventas` por este trabajo.

#### 5.4.6 Paridad

`TestE2E_ParidadCanales_Visitas` y `TestE2E_ParidadCanales_Tareas` (mismo patrón que
`TestE2E_ParidadCanales_Pagos`, `COBRANZA-SYNC.md:155-156`), más una prueba de forma que falla si
alguno de los tres canales no llama a la función del predicado.

#### 5.4.7 Bajas y tombstones

- Ninguna ruta borra filas de `MSP_VISITAS` ni de `MSP_TAREAS_VISITA`. Invariante con prueba: una
  búsqueda de `DELETE FROM MSP_VISITAS` / `MSP_TAREAS_VISITA` en `internal/` (excluyendo pruebas)
  falla la compilación de pruebas. Por eso **no hay tombstones**.
- La salida de una fila de la ventana (envejece más allá de V) no es una baja: el teléfono poda su
  copia de lectura con la **misma** regla (`FECHA < hoy − V`), función compartida con casos.
- Si algún día se necesita borrar, se agrega tombstone y se sube el epoch; no antes.

#### 5.4.8 Teléfono: dónde va lo bajado

- Tabla Room nueva `visita_zona` (lectura): columnas de la visita + compromiso + `UPDATED_AT`.
  **Nunca** se escribe en `Visit` (la tabla de salida): un `INSERT REPLACE` (`VDAO:78-81`) de lo bajado
  sobre una pendiente con el mismo id pisaría lo no enviado.
- La derivación usa la unión por id: `Visit` (lo propio, pendiente o no) ∪ `visita_zona`; ante el mismo
  id gana `Visit` (lo local más fresco).
- Estado del sync en `visitas_sync_state (ZONA, CURSOR, AFTER_ID, EPOCH)`. Cambio de zona ⇒ cursor
  nulo y `AFTER_ID` nulo en la misma operación (`COBRANZA-SYNC.md:81`); `visita_zona` de la zona vieja
  se borra; **`Visit` no se toca** (I8).
- Resync por epoch: borra `visita_zona` y el cursor; **`Visit` no se toca** (I8).

### 5.5 Escritorio y servidor derivan igual

- `GET /v2/visitas/agenda?zona_id=&hoy=` (permiso de lectura de cobranza) devuelve por venta el
  compromiso vigente y su estado, calculados con `DerivarCompromisos` sobre: visitas del predicado de
  §5.4.2, pagos de esas ventas leídos **en modo lectura** por el repositorio propio de visitas
  (`MSP_PAGOS_RECIBIDOS`, `IMPORTES_DOCTOS_CC`; el mismo tipo de lectura que ya hace
  `internal/desempeno/infra/desempenofb/actividades_repo.go:68-168`), y tareas.
- El escritorio solo pinta (memoria `feedback_logica_siempre_en_servidor`).
- `POST /v2/visitas/agenda/conciliacion` recibe del teléfono `{zona, hoy, [ (venta, id_visita_vigente,
  estado) ]}` y responde las diferencias; las registra como métrica (§4.6-2). Sin datos personales.

### 5.6 Interfaz con el documento 1

El documento 1 define `ColumnaDe` y la lista; este documento le entrega, por venta, `EstadoCompromiso`
(tabla 5.3.2). Ningún cambio de este documento altera la regla de dinero del documento 1
(`pagado`/`parcial`).

### 5.7 Citas invisibles en la app hoy (reporte del dueño, 2026-10-16)

El dueño reportó: una cita capturada no aparece en el estado de la venta, ni en el detalle, ni en la
tarjeta de la lista. Lectura del código de `msp-app-kt` (LEÍDO; **cuál ocurrió en campo es INFERIDO**
y se mide con el depurador remoto de Room: `SELECT FECHA, TIPO_VISITA, CITA_FECHA, CITA_HORA,
IMPTE_DOCTO_CC_ID FROM Visit WHERE CLIENTE_ID=?` cruzado con `FECHA_CARGA_INICIAL`, los pagos de la
semana y el aparato donde se miró). La captura y el deriver **sí** leen la cita
(`RegistroDeVisitaAdapter.kt:327-328`, `EstadoCuentaDeriver.kt:165,220-223`); la esconden cinco
condiciones de diseño:

| # | Condición | Dónde | Lo cubre |
|---|---|---|---|
| A | La ventana filtra por fecha de la **visita**, no del compromiso: cita de la semana pasada para esta semana desaparece | `EstadoCuentaDeriver.kt:105` | §5.3 (visibilidad tras inicio de semana); caso compartido nuevo "cita de la semana pasada para esta semana" |
| B | Cualquier abono de la semana tapa la cita ("Abonó parcial") | `EstadoCuentaDeriver.kt:152-159` | **Nuevo:** el chip de dinero y el compromiso se muestran juntos; el dinero sigue mandando en la columna (doc 1) |
| C | Venta nueva sin abonos pinta "Primer abono $X" en lugar del chip | `PiezasDeLaLista.kt:~447-470` (`cf9313bf`) | **Nuevo:** la tarjeta muestra el compromiso también en venta nueva |
| D | La cita solo vive en el teléfono que la capturó | `VisitDao.kt:249-251` | Partes 1–2 (contrato v3 + bajada por zona) |
| E | Ningún renglón de "próxima cita / promesa" en el detalle | `feature/pagos/ui` (control positivo: el grep sí halla `fechaCita` en `EstadoCuentaUi.kt`) | **Nuevo:** renglón de compromiso vigente en el detalle de la venta |

Las promesas sufren A, B, D y E.

**F — la asimetría (cita no se ve, promesa sí; LEÍDO).** La promesa no se guarda sin fecha
(`ReglasDeLaVisita.kt:90`). La cita solo exige el día (`:111`); la hora arranca vacía con el chip "Sin
hora" ya elegido (`CapturaDeVisita.kt:38`, `RegistrarVisitaScreen.kt:656-660`). Con `CITA_HORA` nula,
`EstadoCuentaUi.kt:131-132` la degrada al trato REGRESAS: "Cita sin hora / Sin hora, regresas", ámbar
como "Vuelvo", **sin el día** (`:173`, `:206`, `:247`), y `SegmentoDeCobranza.kt:86-88` la manda a
"Volver" antes de mirar la fecha (`:104`). Tarjeta y detalle usan el mismo `estadoVisualDe`
(`PiezasComunes.kt:117`, `DetalleVentaScreen.kt:401,429`). El caso compartido 025 solo prueba el
deriver (que sí devuelve cita); la degradación ocurre después, sin prueba. **Arreglo:** una cita sin
hora es una cita con su día (trato cita, violeta, segmento por fecha), coherente con DA-15 (aviso a
las 8:00); se agregan casos compartidos a nivel de presentación. Para MEDIDO: `SELECT ID, CLIENTE_ID,
IMPTE_DOCTO_CC_ID, FECHA, TIPO_VISITA, CITA_FECHA, CITA_HORA FROM Visit WHERE TIPO_VISITA = 'Pidió que
regrese otro día' ORDER BY FECHA DESC LIMIT 50` en el teléfono del dueño.

**Medición 2026-10-16 11:31 (teléfono del dueño, app de prueba `com.example.msp_app.test`
2.19.3-local+dbd2f454, Firebase dev; copia binaria con `run-as`).** 11 visitas; 3 con cita, **las 3
con día y hora** (`CITA_HORA` = 16:30, 06:30, 01:15): **F no explica este teléfono** (sigue siendo un
defecto real para quien elija "Sin hora"). Lo que sí aparece: dos de las tres citas están en ventas
**sin ningún abono** (Y00002184, Y00002215; `Payment` sin filas) → en este build (incluye
`cf9313bf`) la tarjeta pinta "Primer abono" en lugar del estado (C); la tercera (Y00001895, cita al
26-sep) quedó reemplazada por "No se encontraba" del 29-sep, que es lo esperado. Hoy las tres visitas
son de semanas anteriores (22–23 sep) → ocultas por A en tarjeta y detalle. Control positivo: la misma
consulta sí devolvió las 3 promesas. Sin medir: qué build tenía el teléfono el 22-sep y qué se vio
entonces. La poda `VisitDao.kt:298` (conserva filas con compromiso
90 días) cambia junto con DA-4.

---

## 6. Parte 3 — tareas de oficina y avisos

### 6.1 Modelo de datos

`MSP_TAREAS_VISITA` (tabla greenfield, CLAUDE.md §1 sin excepciones):

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36) ASCII` PK | UUID del escritorio (idempotencia). |
| `VENTA_ID` | `INTEGER NOT NULL` | `DOCTO_CC_ACR_ID`. |
| `CLIENTE_ID` | `INTEGER NOT NULL` | |
| `ZONA_CLIENTE_ID` | `INTEGER NOT NULL` | Zona de la venta **al asignar** (DA-9). |
| `FECHA` | `DATE NOT NULL` | Civil (misma compuerta de §5.1.1). |
| `HORA` | `CHAR(5) ASCII` | Opcional. |
| `NOTA` | `VARCHAR(500) UTF8` | Opcional. |
| `REEMPLAZA_A` | `CHAR(36) ASCII` | "Editar = cancelar + asignar otra" deja el rastro. |
| `CREATED_AT`, `CREATED_BY`, `UPDATED_AT`, `UPDATED_BY` | | Desde Go, `UPDATED_AT` estampado dentro de la transacción (§5.4.4). |
| `CANCELADA_AT`, `CANCELADA_BY` | | Hecho de cancelación; nulos si no. |

Índices: `(ZONA_CLIENTE_ID, UPDATED_AT, ID)`, `(VENTA_ID)`. El estado **no** tiene columna.

### 6.2 Contratos HTTP

| Método y ruta | Quién | Efecto | Errores |
|---|---|---|---|
| `POST /v2/visitas/tareas` `{id, venta_id, fecha, hora?, nota?, reemplaza_a?}` | escritorio, permiso nuevo `visitas:asignar_tareas` (DA-13) | inserta; mismo id y mismo contenido ⇒ 201 igual; mismo id distinto contenido ⇒ 422 | `tarea_venta_no_encontrada` 422, `tarea_fecha_pasada` 422 (la oficina está en línea y corrige al momento), `tarea_hora_invalida` 422, `tarea_contenido_distinto` 422, `tarea_reemplaza_inexistente` 422 |
| `POST /v2/visitas/tareas/{id}/cancelar` | escritorio, mismo permiso | estampa `CANCELADA_*` y `UPDATED_AT` si estaba nula; repetida ⇒ 200 igual | `tarea_no_encontrada` 404 |
| `GET /v2/visitas/tareas?zona_id=&estado=&desde=` | escritorio | lista con estado derivado | — |
| `GET /v2/visitas/sync/tareas/zona/{zona_id}` + `/digest`, `/ids`, `/by-ids` | teléfono | canal de bajada (§5.4 completo, epoch `tareas`) | — |

El escritorio genera el UUID al abrir el formulario (no al enviar), para que un doble clic o un
reintento sea idempotente.

### 6.3 Estado derivado de la tarea

En este orden: `cancelada` (hay `CANCELADA_AT`) · `atendida` (existe pago a la venta o visita
aplicable — DA-6 — con instante > `CREATED_AT`; se informa **quién** y **hora** del primero) ·
`no_atendida` (hoy − `FECHA` > 14 y no atendida; se cierra) · `pendiente`. Va en el mismo juego de casos
compartidos (§5.3.4).

### 6.4 Push FCM: solo despertador

- Tras el `COMMIT` de crear o cancelar una tarea (nunca dentro de la transacción, mismo criterio que
  `service.go:102-105`), el servidor manda un mensaje de **datos** al tema `zona-<id>` con
  `collapse_key = tareas-zona-<id>`, prioridad alta, TTL 4 h, y payload `{"tipo":"sync"}`: sin ids, sin
  nombres. FCM sin cuerpo con autoridad: el teléfono siempre hace pull por el canal con cursor.
- Si el envío falla: log + métrica, sin reintentos en bucle. El periódico de 15 min trae la tarea.
- Teléfono: `onMessageReceived` y `onDeletedMessages` encolan un trabajo único `sync_tareas_zona` con
  `setExpedited(RUN_AS_NON_EXPEDITED_WORK_REQUEST)` (nunca `DROP_WORK_REQUEST`, investigación P3).
  `onNewToken` resuscribe al tema de su zona; cambio de zona desuscribe la vieja.
- Dependencias nuevas: `firebase-messaging` en la app (hoy no existe: sin servicio FCM en el manifiesto,
  `AndroidManifest.xml:10-26` solo declara los permisos listados; control positivo: la misma lectura
  sí encuentra `ACCESS_FINE_LOCATION`); en el servidor, una llamada HTTPS a FCM v1 con credencial de
  cuenta de servicio guardada en el servidor (CLAUDE.md §5: no requiere binario externo). La credencial
  la instala el dueño (memoria `feedback_no_manejar_credenciales_del_usuario`). Ver DA-10.

### 6.5 Aviso inmediato al asignar

Cuando el sync de tareas trae una tarea `pendiente` que el teléfono no había notificado, emite una
notificación "Visita asignada" y guarda el id en `aviso_emitido` (Room). Idempotente: el mismo id nunca
notifica dos veces, aunque llegue por push, por periódico y por resync.

### 6.6 Alarmas locales

- **Fuente de verdad**: tabla Room `aviso_programado (ID determinista = hash(tipo, origen_id, instante),
  INSTANTE, TIPO, ORIGEN_ID)`, recalculada desde la derivación. Las alarmas del sistema son una
  proyección y se reconstruyen.
- **Cuándo**: 30 min antes de cita o tarea con hora; a las 8:00 (DA-2) del día de la promesa, de la
  cita sin hora (DA-15) y de la tarea sin hora.
- **Reprogramar todo** desde la tabla en: `BOOT_COMPLETED`, `MY_PACKAGE_REPLACED` (INFERIDO en la
  investigación P8, verificar en emulador), `TIME_SET`/`TIMEZONE_CHANGED`, abrir la app, tras cada
  sync y tras cada captura. Reprogramar es idempotente (cancela por id y vuelve a poner).
- **Exactas**: `setExactAndAllowWhileIdle` si `canScheduleExactAlarms()`; si no, `setWindow` de 10 min y
  se reporta. Se declara `SCHEDULE_EXACT_ALARM`, **nunca** `USE_EXACT_ALARM` (Play lo rechaza para esta
  app, investigación P8). Se escucha `ACTION_SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED`.
- **`POST_NOTIFICATIONS`** (targetSdk 35, `app/build.gradle.kts:80`; hoy no declarado): se pide en el
  alta; si se niega, la lista sigue mostrando todo y se reporta.
- **Fabricantes**: en el alta del equipo se excluye de la optimización de batería; se reporta
  `isIgnoringBatteryOptimizations`.
- **Degradación**: una alarma perdida no pierde información; la columna "Por visitar" sigue mostrando
  el compromiso. Las alarmas son comodidad, nunca fuente de verdad.
- Telemetría (§4.2-10): `permiso.notificaciones`, `permiso.alarmas_exactas`, `bateria.sin_optimizar`,
  `avisos.programados`, `avisos.disparados_24h`.

---

## 7. Capa 2 — aislamiento del dinero

### 7.1 Lista de archivos vedados y el chequeo automático

Script nuevo `scripts/guardia-aislamiento.sh` en cada repo, con su lista en
`scripts/guardia-aislamiento.lista`. Corre en `lefthook` pre-commit y en CI sobre
`git diff --name-only origin/main...HEAD`; sale con código distinto de cero si el diff toca un patrón de
la lista. **No hay bandera para saltarlo**: quitar una línea de la lista es un PR aparte que aprueba el
dueño.

`msp-api`:
```
internal/cobranza/**
internal/ventas/**
internal/rutas/**
internal/clientes/**
internal/desempeno/domain/cobro.go
internal/desempeno/domain/formas_cobro.go
internal/platform/firebird/**
internal/platform/idempotency/**
internal/platform/failedintent/**
migrations-firebird/*pagos*  migrations-firebird/*saldos*  migrations-firebird/*sync_epoch*
docs/COBRANZA-SYNC.md
```
Excepciones enumeradas, con verificación de contenido del diff (el script compara el hunk):
- `cmd/api/failedintent_wiring.go`: solo se permite **agregar** la línea `.Registrar("/v2/visitas", …)`.
- Una migración nueva que inserta filas `'visitas'`/`'tareas'` en `MSP_CFG_SYNC_EPOCH`: se permite si no
  contiene `UPDATE` ni filas `'pagos'`/`'ventas'`.

`msp-app-kt`:
```
app/src/main/java/com/example/msp_app/workers/PendingPaymentsWorker.kt
core/upload/**
app/src/main/java/com/example/msp_app/core/sync/cobranza/**
app/src/main/java/com/example/msp_app/data/pagos/**
app/src/main/java/com/example/msp_app/data/api/services/payment/**
app/src/main/java/com/example/msp_app/data/api/services/cobranza/**
core/database/src/main/kotlin/com/example/msp_app/core/database/dao/payment/**
core/database/src/main/kotlin/com/example/msp_app/core/database/dao/sale/SaleDao.kt
core/database/src/main/kotlin/com/example/msp_app/core/database/entities/PaymentEntity.kt
core/database/src/main/kotlin/com/example/msp_app/core/database/migrations/Migration[0-9]*to3[0-3].kt
core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt
core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/VentanaCobro.kt
feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/data/**
```
Excepción enumerada: `AppDatabase.kt` solo puede cambiar `version`, la lista `entities` y agregar
accesores de DAO nuevos, más sus `import` y el registro de la migración nueva en `addMigrations`
(sin esa línea ningún teléfono abre la base; enmienda del 2026-10-16 tras la revisión independiente de
`feat/diagnostico-envio`); nada más. **Ojo:** `feat/diagnostico-envio` ya usa v34 (`envio_diagnostico`,
§4.7); el cambio de visitas de §4.2 punto 7 pasa a **v35**.

Prueba del chequeo: un commit de prueba que toca `PendingPaymentsWorker.kt` debe ser rechazado
(control positivo), y uno que solo toca `PendingVisitsWorker.kt` debe pasar.

### 7.2 ¿Visitas y compromisos mueven dinero? Verificación

- Servidor: el registro inserta solo en `MSP_VISITAS` (`VREPO:56-63`) y `MSP_VISITAS_IMAGENES`
  (`internal/visitas/infra/visitasfb/imagenes_repo.go:23`). LEÍDO. Las partes nuevas solo **leen**
  tablas de pagos (§5.5). Prueba nueva: con un `firebird` instrumentado, la suite de visitas no emite
  ninguna sentencia `INSERT/UPDATE/DELETE` sobre `DOCTOS_CC`, `IMPORTES_DOCTOS_CC`, `MSP_PAGOS_*`,
  `MSP_SALDOS_*`.
- Teléfono: **sí hay un toque** (H17). Al capturar una visita de alcance VENTA,
  `insertVisitAndUpdateState` llama `saleDao.updateTotal(saleId, 0.0, newState)` (`VLDS:98-99`), que
  ejecuta `SALDO_REST = SALDO_REST - :amount` (`SDAO:281-289`). Con `amount = 0.0` el valor no cambia
  para todo `SALDO_REST` finito y para `NULL` (INFERIDO por aritmética IEEE 754 y semántica de `NULL` en
  SQLite; lo vuelve MEDIDO E-APP-061: prueba con Room en memoria sobre 0, negativos, decimales, `-0.0` y
  `NULL`). **Este trabajo no lo cambia ni lo llama desde código nuevo**; su retiro, si se quiere, es un
  cambio de dinero con su propia ruta §8.
- Conclusión honesta: P1–P3 no escriben dinero; el camino existente de captura de visita sí ejecuta una
  sentencia neutra sobre una columna de dinero, que queda fijada por prueba.

---

## 8. Estrategia de pruebas

### 8.1 Simulación determinista (lo más valioso)

**Go** (`internal/visitas/sim/`, solo pruebas):
- Teléfono modelo: tabla de salida, `CONFIRMADA_EN`/`RESGUARDADA_EN`, cursor, `visita_zona`.
- Servidor: handlers **reales** sobre `fbtestutil.WithTestTransaction` (versión lenta) y sobre repos
  falsos (versión rápida, miles de semillas).
- Red simulada con RNG sembrado; tiempo virtual con `testing/synctest` (Go 1.25, `go.mod:3`).
- `pgregory.net/rapid` (`go.mod:32`, ya es dependencia) con `StateMachineActions`; la acción de
  chequeo corre I1–I10 después de **cada** acción.

**Kotlin**: el mismo planificador sobre la lógica real del cliente con Room en memoria, `MockWebServer`
(`gradle/libs.versions.toml:84`), `work-testing` (`:89`) y `runTest`. Kotest y PIT son dependencias
nuevas (DA-14).

**Catálogo de fallas** (cada una con al menos una semilla fija de regresión):

| Grupo | Falla |
|---|---|
| Red | perder la petición; **perder la respuesta tras un commit**; duplicar; reordenar; timeout a mitad de multipart; 200 con cuerpo truncado. |
| Proceso teléfono | morir entre commit de Room y encolado; entre 2xx y `markDone`; a mitad de una página de bajada; entre aplicar página y guardar cursor; durante el relleno. |
| Servidor | caída después del commit y antes de responder; commit fallido; dos peticiones concurrentes con el mismo id (con y sin foto); commit tardío (§5.4.4); subida de epoch con pendientes; `Store.Save` de la bandeja fallando a la vez que la petición; 413; 422 por campo desconocido (API vieja). |
| Reloj | teléfono adelantado 1 h y 49 h; atrasado; salto durante la captura; cambio de horario de verano en la zona de negocio. |
| Android | worker detenido por perder la restricción; reinicio (alarmas borradas); permiso de exactas revocado; notificaciones negadas; Auto Backup restaurando una base de otro usuario. |

### 8.2 Pruebas rojas al revertir (obligatorio por arreglo)

Cada arreglo de §4 y §5 trae una prueba que se pone **roja** al revertir el arreglo, comprobado de
verdad: revertir, correr, ver rojo, restaurar, verificar que no quedaron restos
(`COBRANZA-SYNC.md:522-525`). Lista mínima: H1 (cada código), H2, H3 (51 pendientes y orden), H5
(capacidad), H6 (cada fila de la tabla 5.2.3), H7, H10, H11, H15, I8 (epoch con pendientes), §5.4.4
(dos transacciones), §5.4.3 (empate de `UPDATED_AT` con 2,000 filas), página vacía conserva `AFTER_ID`.

### 8.3 Casos compartidos Kotlin↔Go

§5.3.4. Los dos lados cargan los mismos archivos; el hash de `VERSION` es idéntico en ambos repos.

### 8.4 Caos con Toxiproxy (solo dev)

Dos proxys: teléfono/emulador → API y API → Firebird (3050). Tóxicos: `latency`, `timeout`,
`reset_peer`, `slicer`, `limit_data`, `down`. Hipótesis de estado estable: tras quitar las fallas,
paridad = 0 y ninguna visita pendiente de más de 30 min. El proxy API → Firebird ejercita además el
envenenamiento del pool al cancelar contexto (memoria `reference_firebirdsql_ctx_cancel_poisons_pool`).
Hoy no hay Toxiproxy en el repo (`fb_event_listener_integration_test.go:513-524` lo menciona como
ausente). Producción nunca.

### 8.5 Mutación

Gremlins (Go, ya en `Makefile:375-379`) y PIT (Kotlin, nuevo) **apuntados solo** a: la decisión de
soltar, la existencia completa, el avance del cursor, la marca segura, la comparación de digests, el
predicado, la derivación de compromisos y tareas, la poda. Todo mutante vivo se mata con una prueba o
se documenta como equivalente.

### 8.6 Historial + verificador

Cada corrida (simulación y caos) escribe un historial `captura / envío / respuesta / marca / pull` y un
verificador fuera de línea comprueba I1–I10 sobre él. Las semillas que fallen se congelan.

### 8.7 Agente adversario

Un agente independiente recibe el diseño y el código, con un solo encargo: **"pierde, duplica o
corrompe una visita o un compromiso"**. Cada escenario que proponga se convierte en una semilla o una
prueba; si alguno pasa, se detiene el despliegue.

---

## 9. Evidencia y compuertas (CLAUDE.md §8)

### 9.1 Dónde

- Área nueva **`docs/evidencia/visitas.md`** (prevista en `registro-de-evidencia.md:125-127`; hoy no
  existe), identificadores **`E-VIS-001`** en adelante: hechos de servidor y de contrato.
- Hechos solo del teléfono: `docs/evidencia/app-android.md`, siguiente libre **`E-APP-058`**
  (`app-android.md:50`).
- Hechos de Android-plataforma/infra: `docs/evidencia/infra.md`, siguiente libre **`E-INF-017`**
  (`infra.md:37`). El documento 1 también reserva `E-INF-017` para su primera entrada
  (`2026-10-15-desempeno-pulido-2-design.md:390`): el número se asigna **al escribir la entrada**;
  quien llegue segundo renumera y corrige sus citas (`registro-de-evidencia.md:257-259`).

### 9.2 Entradas a crear (todas nacen como LEÍDO/INFERIDO y deben llegar a MEDIDO antes del cambio que las usa)

| Id | Afirmación | Cómo se mide |
|---|---|---|
| E-VIS-001 | El teléfono suelta visitas sin prueba (H1). | Prueba de worker con 500 sin cabecera ×10 ⇒ hoy marca; control: con 2xx marca. |
| E-VIS-002 | Reenvío con datos distintos devuelve 201 y descarta (H6). | Prueba e2e en dev. |
| E-VIS-003 | `POST /visitas` responde 413 desde 1 MiB (H7). | Petición de 1,048,576 y 1,048,575 bytes en dev. |
| E-VIS-004 | `cleanupBlobs` puede borrar el archivo de una fila confirmada (H11). | Prueba de concurrencia con falla inyectada. |
| E-VIS-005 | Cuántas visitas rechazadas hay en la bandeja, por código (Q1). | Producción, solo lectura. |
| E-VIS-006 | Duplicados lógicos desde 2026-08-13 (Q2). | Producción, solo lectura. |
| E-VIS-007 | Versión de UUID por mes (Q3). | Producción, solo lectura. |
| E-VIS-008 | Fotos con fila y sin archivo; tamaño máximo por visita (Q4). | Producción, solo lectura + listado de disco. |
| E-VIS-009 | Visitas `GUARDADO = 1` en teléfonos que no están en el servidor (Q5). | Depurador remoto (con autorización) + Q5b. |
| E-VIS-010 | Promesas/citas solo en teléfono, por teléfono (Q5). | Depurador remoto. |
| E-VIS-011 | Ningún otro escritor de `MSP_VISITAS` (Q6). | Producción, solo lectura. |
| E-VIS-012 | Desfase de reloj Go↔Firebird en el servidor. | Comparación en el servidor de producción (solo lectura). |
| E-VIS-013 | La marca segura por tiempo entrega el commit tardío; sin ella no. | Prueba de dos transacciones en dev. |
| E-VIS-014 | `DATE` ida y vuelta sin corrimiento (§5.1.1). | Prueba en dev con dos `TZ`. |
| E-VIS-015 | `PLAN` del predicado de bajada y su costo por zona. | Dev y producción (`SET PLANONLY`). |
| E-VIS-016 | Orden de `ID` igual en `CHARACTER SET NONE` (prod) y `ASCII` (dev). | Consulta de orden en ambos. |
| E-VIS-017 | El hueco de commit tardío de pagos (§5.4.4, hallazgo lateral). | Prueba de dos transacciones contra el sync de pagos en dev. Solo se registra; no se arregla aquí. |
| E-APP-058 | Backoff real del worker de visitas (H1b). | `work-testing`. |
| E-APP-059 | Reencolado una vez por proceso, tope 50, sin orden (H3). | Prueba con 60 pendientes. |
| E-APP-060 | Auto Backup restaura la base de Room (H15). | Emulador con `bmgr`. |
| E-APP-061 | `SALDO_REST - 0.0` es neutro (H17, §7.2). | Room en memoria. |
| E-APP-062 | `retryOnConnectionFailure` efectivo en el cliente v2 (H18). | MockWebServer que corta la conexión tras recibir el cuerpo. |
| E-INF-017 | Alarmas tras reinicio, actualización y revocación de exactas. | Emulador API 34. |
| E-INF-018 | FCM: tasa de llegada y colapso en la flota canario. | Telemetría de 7 días. |

### 9.3 Compuertas

- **Antes** (agente independiente, recibe la pregunta y no la conclusión) para cada parte: P1 se
  apoya en E-VIS-001, -003, -004, -005, -009, E-APP-058/059/060/062; P2 en E-VIS-002, -006, -007, -010,
  -011, -012, -013, -014, -015, -016; P3 en E-INF-017/018. Todo lo que no sea SOSTIENE detiene.
- **Después** (otro agente): prueba roja al revertir, **enumeración** de consumidores de lo tocado
  (`MSP_VISITAS`: `visitasfb/repo.go`, `desempenofb/actividades_repo.go:170-198`, la bandeja, el
  escritorio; `Visit` en Room: la búsqueda de `getPendingVisits`/`Visit` da `Home.kt:166,202`,
  `AuthViewModel.kt:128`, `VisitsViewModel.kt:34-39`, `RoomPendingVisitsStore.kt:22`,
  `PendingWorkSyncFactory.kt:100` — la lista completa se rehace con búsqueda en el momento), y lo que
  quedó sin verificar dicho en voz alta.
- Commits citan el identificador (`Cierra E-VIS-002.`); las pruebas lo llevan en el nombre.

---

## 10. Mediciones previas (producción, SOLO LECTURA)

Reglas: por `.bat` con `isql -i` (memoria `reference_isql_produccion_trampas`); transacción
`SET TRANSACTION READ ONLY ISOLATION LEVEL READ COMMITTED;` al inicio (una de solo lectura no congela
el watermark, `WM:47-60`); leer **los errores antes que los números**; sin `CAST(... AS VARCHAR(n))`
concatenado; cada consulta se corre **primero en dev** con su control positivo. Tras el lote:
`SELECT COUNT(*) FROM MON$STATEMENTS WHERE MON$STATE = 2;` debe dar 0.

**Q1 — rechazos de visitas en la bandeja**
```sql
SELECT HTTP_STATUS, ERROR_CODE, STATUS, COUNT(*)
FROM MSP_FAILED_INTENTS
WHERE PATH STARTING WITH '/v2/visitas' AND METHOD = 'POST'
GROUP BY HTTP_STATUS, ERROR_CODE, STATUS;

SELECT COUNT(*), COUNT(DISTINCT IDEMPOTENCY_KEY)
FROM MSP_FAILED_INTENTS
WHERE PATH STARTING WITH '/v2/visitas' AND METHOD = 'POST';

-- rechazadas cuya visita NO está en la tabla (las que hoy el teléfono soltó y nadie tiene aplicadas)
SELECT COUNT(DISTINCT f.IDEMPOTENCY_KEY)
FROM MSP_FAILED_INTENTS f
WHERE f.PATH STARTING WITH '/v2/visitas' AND f.METHOD = 'POST'
  AND f.IDEMPOTENCY_KEY IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM MSP_VISITAS v WHERE v.ID = f.IDEMPOTENCY_KEY);
```
Control positivo (dev): provocar un 422 de visita y verificar que aparece en las tres consultas.

**Q2 — duplicados lógicos desde 2026-08-13**
```sql
SELECT COUNT(*), SUM(N - 1)
FROM (SELECT v.CLIENTE_ID, v.IMPTE_DOCTO_CC_ID, v.COBRADOR_ID, v.TIPO_VISITA, v.FECHA, COUNT(*) AS N
      FROM MSP_VISITAS v
      WHERE v.FECHA >= TIMESTAMP '2026-08-13 00:00:00'
      GROUP BY v.CLIENTE_ID, v.IMPTE_DOCTO_CC_ID, v.COBRADOR_ID, v.TIPO_VISITA, v.FECHA
      HAVING COUNT(*) > 1);
```
Control positivo (dev): los tres pares de E-INF-014 (zona 2990693, `infra.md:641-644`) deben contarse.

**Q3 — versión de UUID por mes**
```sql
SELECT EXTRACT(YEAR FROM v.FECHA), EXTRACT(MONTH FROM v.FECHA), SUBSTRING(v.ID FROM 15 FOR 1), COUNT(*)
FROM MSP_VISITAS v
WHERE v.FECHA >= TIMESTAMP '2026-06-01 00:00:00'
GROUP BY EXTRACT(YEAR FROM v.FECHA), EXTRACT(MONTH FROM v.FECHA), SUBSTRING(v.ID FROM 15 FOR 1);
```
Control positivo: un id v3 conocido (de `IDV`) y uno v4 (`uuid.New`) insertados en dev salen con `3` y `4`.

**Q4 — fotos de visitas**
```sql
SELECT COUNT(*), MAX(SIZE_BYTES) FROM MSP_VISITAS_IMAGENES;
SELECT MAX(T) FROM (SELECT VISITA_ID, SUM(SIZE_BYTES) AS T FROM MSP_VISITAS_IMAGENES GROUP BY VISITA_ID);
SELECT STORAGE_KEY FROM MSP_VISITAS_IMAGENES;   -- se compara contra `dir /b /s` de STORAGE_DIR\visitas
```
Si el máximo por visita queda justo bajo 1 MiB, refuerza H7. Control positivo: una clave inventada en la
lista de comparación sale como "sin archivo".

**Q5 — teléfonos (depurador remoto de Room; solo `SELECT`; requiere autorización explícita del dueño;
`config/db_debug.allowedDevices`, `COBRANZA-SYNC.md:416-428`; ojo: `blockDangerousQueries` está en
`true`, E-INF-007, que **contradice** `COBRANZA-SYNC.md:444-445`)**
```sql
SELECT COUNT(*), MIN(FECHA) FROM Visit WHERE GUARDADO_EN_MICROSIP = 0;
SELECT COUNT(*) FROM Visit WHERE PROMESA_FECHA IS NOT NULL OR CITA_FECHA IS NOT NULL;
SELECT ID FROM Visit WHERE GUARDADO_EN_MICROSIP = 1;
```
**Q5b** (producción) con esos ids, en tandas de ≤ 120 (trampa 5 de la memoria de `isql`):
```sql
SELECT ID FROM MSP_VISITAS WHERE ID IN ('…', '…');
```
Las que falten son visitas que el teléfono cree entregadas y el servidor no tiene. Control positivo: un
id que sí existe sale.

**Q6 — otros escritores**
```sql
SELECT COUNT(*), MIN(FECHA), MAX(FECHA) FROM MSP_VISITAS
WHERE FECHA >= TIMESTAMP '2026-08-13 00:00:00' AND SUBSTRING(ID FROM 15 FOR 1) NOT IN ('3', '4');
SELECT MON$REMOTE_PROCESS, COUNT(*) FROM MON$ATTACHMENTS GROUP BY MON$REMOTE_PROCESS;
```
(La segunda muestra qué procesos están conectados; un proceso de Node conectado es señal de alerta.)

**Q7 — zona de la visita vs zona actual del cliente (DA-9)**
```sql
SELECT COUNT(*)
FROM MSP_VISITAS v JOIN CLIENTES c ON c.CLIENTE_ID = v.CLIENTE_ID
WHERE v.FECHA >= TIMESTAMP '2026-08-13 00:00:00' AND c.ZONA_CLIENTE_ID <> v.ZONA_CLIENTE_ID;
```

**Q8 — volumen por zona en la ventana V** (para costo de la primera bajada):
```sql
SELECT v.ZONA_CLIENTE_ID, COUNT(*) FROM MSP_VISITAS v
WHERE v.FECHA >= DATEADD(-92 DAY TO CURRENT_DATE)
GROUP BY v.ZONA_CLIENTE_ID;
```
Referencia ya medida en dev: 3,919 visitas de una zona en 92 días (`infra.md:711-717`, E-INF-014).

---

## 11. Orden de despliegue

Snapshot antes de cada migración (`make fb-snapshot NAME=antes-<mig>`, `COBRANZA-SYNC.md:396`);
respaldo del binario anterior en el servidor; las migraciones se aplican a mano
(`COBRANZA-SYNC.md:393-394`), así que binario y migración son pasos separables.

| # | Paso | Avanza si | Reversa |
|---|---|---|---|
| 0 | Mediciones §10 + compuertas "antes" de P1. | Todas SOSTIENE; el dueño vio las cifras (Q1, Q5b). | — |
| 1 | **Servidor P1** (compatible): `MaxBodyBytes` (si E-VIS-003 lo confirma), extractor de bandeja, nonce de blobs, `imagen_ids` en GET. Sin migración. | 48 h sin aumento de 5xx en `/v2/visitas`; bandeja legible. | Binario anterior (probado en dev el mismo día). |
| 2 | **App P1 canario**: el teléfono del dueño (DA-5) varios días. Kill-switch remoto: documento nuevo `config/visitas_envio` en Firestore (`modo: v3 | v2_actual`); **nunca** `config/api_settings` (`COBRANZA-SYNC.md:442-443`). | Cero visitas pendientes > 30 min con red; reconciliación inversa sin faltantes; cero diferencias durante el periodo. | Kill-switch a `v2_actual` (el código viejo sigue en el APK durante P1) o APK anterior. |
| 3 | **App P1 a la flota**; versión mínima con `:core:appgate`. | Adopción ≥ 95 % y mismas métricas 7 días. | Igual que 2. |
| 4 | **Migración P2** (columnas + índices + epoch `visitas`/`tareas` en 0), fuera de horario; relleno de `UPDATED_AT` en tandas. | `MON$STATEMENTS` sin atorados; sync de pagos sin retraso (alerta §12). | No se borran columnas: el binario anterior las ignora (`INSERT`/`SELECT` con columnas explícitas, `VREPO:42-63`). |
| 5 | **Servidor P2** con `VISITAS_COMPROMISOS_ENABLED=false`: acepta los campos (contrato 3), canal de bajada, agenda, conciliación. `capacidades` responde `compromisos:false`. | Paridad e2e verde en dev; en producción, digest = conteo de Q8 por zona. | Binario anterior. |
| 6 | Bandera en `true`. Solo los APK nuevos mandan campos. | 24 h sin `visita_compromiso_distinto` inesperados. | Bandera en `false` (teléfonos dejan de mandar en ≤ 6 h; lo ya guardado se queda). |
| 7 | **App P2 canario**: manda compromisos, rellena, baja agenda, conciliación directa. | Varios días con **cero diferencias** teléfono↔servidor (§4.6-2) y relleno completo de sus visitas con compromiso. | Kill-switch `config/visitas_compromisos.bajada=false` (deja de bajar; la derivación cae a lo propio). |
| 8 | App P2 a la flota. | Cero diferencias 7 días. | Igual. |
| 9 | **Servidor P3** (migración `MSP_TAREAS_VISITA`, endpoints, FCM apagado), **escritorio** con "Asignar visita". | Tareas visibles en canario vía periódico. | Binario/escritorio anterior; la tabla se queda. |
| 10 | **App P3 canario** (alarmas, notificaciones, FCM encendido para su zona). | E-INF-017/018 medidos; ningún aviso duplicado; ninguna tarea perdida. | `config/visitas_avisos.fcm=false`, `alarmas=false`. |
| 11 | App P3 a la flota. | 7 días estables. | Igual. |

Orden de despliegue de las dos restricciones al estilo de `COBRANZA-SYNC.md` §8: el teléfono con
cursor par debe estar en la flota **antes** de cualquier subida del epoch `visitas` (paso 8 antes de
cualquier subida); hoy no se planea ninguna.

---

## 12. Observabilidad

**Servidor** (log estructurado + contador, sin datos personales):
`visitas.post {codigo}`, `visitas.reenvio.relleno`, `visitas.reenvio.igual`,
`visitas.reenvio.compromiso_distinto`, `visitas.reenvio.base_distinta {campo}`,
`visitas.sync.pagina {zona, filas, ms}`, `visitas.sync.marca_retenida_s` (cuánto atrasa la marca
segura), `visitas.agenda.diferencias {zona}`, `tareas.fcm {ok|error}`, `visitas.blob.huerfanos`.

**Teléfono** (Firestore, lista blanca cerrada de claves como `COBRANZA-SYNC.md:510-511`): §4.2-10 y
§6.6, más `visitas_sync.cursor_stalled` con el diseño "cierre contra cierre entre corridas"
(`COBRANZA-SYNC.md:495-504`).

**Alertas el mismo día** (al dueño):
1. Cualquier `visitas.reconcilia.devuelta_a_pendiente` > 0.
2. Diferencias de agenda > 0 en cualquier zona.
3. Visita pendiente más vieja > 2 h con el teléfono reportando red (DA-18 para el umbral).
4. `visitas.sync.marca_retenida_s` > 300 (una transacción larga congela la bajada).
5. Teléfono sin reporte > 24 h.
6. Permisos de notificación/alarma en `false` en algún teléfono.
7. `visita_compromiso_distinto` o `base_distinta` en aumento día contra día.

---

## 13. Decisiones (cerradas por el dueño el 2026-10-16)

El dueño respondió "todas como recomiendas", con dos precisiones: **DA-5** — el único canario es el teléfono del dueño (no hay cobradores disponibles para probar), así que P1 canario dura más y la reconciliación inversa se corre a diario sobre ese teléfono antes de abrir a la flota; **DA-6** — todo lo que se captura en la pantalla de visitas cuenta como visita (incluido "no estaba" y cualquier resultado); solo pagos y condonaciones no son visitas. La columna "Recomendación" queda como **lo decidido**.

| # | Pregunta | Recomendación |
|---|---|---|
| DA-1 | **Código para "mismo id, compromiso distinto".** El dueño decidió 409. La tabla compartida trata 409 como "reintenta siempre" (`UD:36`), así que el teléfono reintentaría para siempre y cada intento dejaría una fila nueva en la bandeja (el middleware no deduplica, `ENTREGA_GARANTIZADA.md`: 86 ventas → 498 filas). Cambiar la tabla toca `:core:upload`, vedado (§7.1). | **422 `visita_compromiso_distinto`** con `X-Intent-Captured`: el teléfono suelta y la oficina decide. Requiere confirmación explícita porque contradice lo decidido. |
| DA-2 | Hora del aviso de la mañana. | 8:00 fija para todos; configurable después si se pide. |
| DA-3 | Ventana V de la bajada (cuánto hacia atrás cuenta una visita para la agenda). | 92 días (mismo `RangoMaximo` de desempeño; ~4k filas por zona según E-INF-014). |
| DA-4 | Retención local de visitas confirmadas (para la reconciliación inversa) y de las resguardadas. | Confirmadas: 30 días tras confirmar. Resguardadas: hasta resolverse o 90 días, con reporte antes de podar. Las claves del servidor (PK) **no caducan** nunca (`ENTREGA_GARANTIZADA.md` requisito 1). |
| DA-5 | Teléfonos canario. | **Decidido:** solo el teléfono del dueño. |
| DA-6 | "Visita a la venta" para reemplazo, cita cumplida y tarea atendida: ¿cuenta una visita de alcance CLIENTE ("no estaba")? | **Decidido:** sí. Todo lo capturado en la pantalla de visitas es visita; pagos y condonaciones no. |
| DA-7 | Pago de oficina sin hora (`DOCTOS_CC.FECHA` es solo fecha, `COBRANZA-SYNC.md:367-371`): ¿cierra una promesa capturada ese mismo día? | Sí, si su fecha civil ≥ la fecha civil de la visita de la promesa. |
| DA-8 | Definición única de "pago rechazado". El teléfono usa `DOCTO_CC_ID = -1` (`ECD`, E-APP-057); el servidor marca rechazado al pendiente con `INTENTOS > 0` (`actividades_repo.go:523`). | Medir cuántos difieren y fijar una sola definición en los casos compartidos antes de P2. |
| DA-9 | Zona de una visita/tarea si el cliente cambia de zona. | La zona capturada (hecho inmutable); medir Q7 primero. |
| DA-10 | FCM por **tema de zona** o por token por usuario. | Tema de zona: sin tabla de tokens que mantener. |
| DA-11 | "No atendida 14 días": ¿desde la fecha de la tarea o desde que se asignó? | Desde la fecha de la tarea. |
| DA-12 | Qué hacer con reenvíos de **base distinta** tras medirlos (fase B). | Decidir con la cifra de `visitas.reenvio.base_distinta`. |
| DA-13 | Permiso `visitas:asignar_tareas`: quién lo tiene. | Oficina y dueño. (Ojo: un permiso nuevo reinicia el auto-sync de permisos, memoria `reference_msp_api_permission_boot_sync`.) |
| DA-14 | Dependencias nuevas de prueba (Kotest, PIT, Toxiproxy) y de producto (`firebase-messaging`, credencial FCM en el servidor). | Aprobar. |
| DA-15 | Cita sin hora: ¿aviso a las 8:00? | Sí. |
| DA-16 | Promesa sin fecha: ¿es compromiso? | No (no vence, no avisa); el documento 1 ya la manda a "regresas". |
| DA-17 | Hallazgo lateral de pagos (§5.4.4): ¿se investiga y mide aparte? | Sí, como trabajo propio con su ruta §8; no dentro de este. |
| DA-18 | Umbral de la alerta "visita pendiente vieja". | 2 h con red. |

---

## 14. Fuera de alcance

- Todo lo del **documento 1** (3 columnas, semana vencida, mayoreo, horario, mapa, totales,
  contrato v2.2 de desempeño).
- Cualquier cambio en pagos, saldos, ventas, sync de cobranza o `:core:upload` (§7).
- Retirar el `UPDATE … SALDO_REST - 0.0` de la captura de visitas (H17).
- Limpiar los duplicados heredados (H13): se miden (Q2); su limpieza es otra decisión.
- Arreglar el hueco de commit tardío de pagos (DA-17).
- GPS continuo, sanciones, nómina.

---

## 15. Auto-revisión (2026-10-15)

- **Sin TBD.** Lo no decidido está en §13 con recomendación; lo no medido está en §9.2 con su forma de
  medirse; las dos elecciones técnicas condicionadas (`DATE` vs `CHAR(10)`, `MaxBodyBytes`) quedan
  decididas por el resultado de una prueba nombrada, no abiertas.
- **Contra `COBRANZA-SYNC.md`**: cursor par (§5.4.3 ↔ §2); página vacía conserva posición (§5.4.1 ↔
  §2); epoch con sus seis reglas y "subir si cambia el `WHERE`" (§5.4.5 ↔ §3); tres canales idénticos
  con prueba de paridad (§5.4.1, §5.4.6 ↔ §4); sin `INNER JOIN` de enriquecimiento (§5.4.2 ↔ §5); no
  tocar `config/api_settings` (§11 ↔ §9); orden de despliegue cursor-antes-que-epoch (§11 ↔ §8). Los
  gemelos (§6 de aquel) no aplican: la visita no cambia de id entre teléfono y servidor. Única
  contradicción encontrada **dentro** del material leído: `COBRANZA-SYNC.md:444-445` dice que
  `blockDangerousQueries` está en `false`; E-INF-007 midió `true`. Se cita aquí y no se corrige ese
  documento (fuera de alcance y vedado por §7.1).
- **Contra las decisiones del dueño**: todas se respetan salvo DA-1, donde el 409 decidido no funciona
  con la tabla compartida; se presenta la razón y se pide confirmación en lugar de cambiarlo en silencio.
- **Ambigüedades cerradas**: orden de estados (§5.3.2), qué es "más reciente" y su desempate, qué cuenta
  como pago que cierra, qué hace la ausencia de un campo en un reenvío (I10), dónde va lo bajado
  (`visita_zona`, nunca `Visit`), cuándo se emite un aviso y cuántas veces.
- **Citas**: cada `archivo:línea` se leyó el 2026-10-15 en las versiones de §0.2. Correcciones respecto
  a lo recibido: el tope del cuerpo es **1 MiB por Huma con 413**, no "10 MB con 500" (H7); el comentario
  de `IDV:49-51` cita `handlers.go:46` para el `uuid.Parse`, que hoy está en `HND:85` (`parseVisitaID`):
  cita vieja en el otro repo, no se toca aquí.
- **Lo que quedó sin verificar al escribir esto** (dicho en voz alta): el backoff real (H1b), que
  OkHttp reintente el POST (H18), el 413 real (H7), H11, que Auto Backup restaure (H15), la conversión
  de `DATE` del driver, el orden de `ID` con `CHARACTER SET NONE`, la ausencia de otros escritores de
  `MSP_VISITAS`, el desfase de reloj, la conducta de `MY_PACKAGE_REPLACED`, la entrega de FCM por tema,
  las 15,022 filas duplicadas reportadas y la ausencia de telemetría de visitas (H14), y todas las
  cifras de producción de §10.
