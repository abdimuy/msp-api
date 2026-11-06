# Garantías — Tarea 11: lo que quedó de la tarea 9 y la captura en el teléfono

> **Dos repos, dos ramas, dos PR.**
> - **Parte 1, `msp-api`:** rama `feat/garantias-t11-pendientes`, que creas tú desde `main` **una vez fusionado el PR #33** (`git fetch && git switch -c feat/garantias-t11-pendientes origin/main`, y antes `git log origin/main --oneline | grep -c '#33'` debe dar 1). PR contra `main`.
> - **Parte 2, `msp-app-kt`:** rama `feat/garantias-captura`, que creas tú desde `origin/main` (`git fetch && git switch -c feat/garantias-captura origin/main`). PR contra `main`.
>
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md): §3.3, §3.4, **§4.1**, §4.2, **§6 completo** y **§7**.
> **Plazo:** **una sola entrega, viernes 13 de noviembre, 1pm**, con las dos partes completas, sus dos PR y sus dos reportes. Todo se entrega al final, de una vez: no hay entregas parciales, avances obligatorios ni revisiones intermedias. No hay holgura.

## Qué construyes

Al terminar, la tarea 9 queda cerrada de verdad —cada regla con una prueba que se pone roja al quitarla y un reporte que se puede reproducir— y el cobrador puede **abrir, recolectar, consultar y entregar un folio de garantía desde el teléfono, con o sin señal**, sin que un reintento se convierta en un 409 ni en una operación perdida.

| Parte | Repo | Contenido | Orden sugerido |
|---|---|---|---|
| **1** | `msp-api` | Pruebas que faltan de la tarea 9, la decisión sobre la huella de `entregar`, y el reporte de la tarea 9 corregido | Primero |
| **2** | `msp-app-kt` | Módulo `:feature:garantias`: abrir folio, recolección con fotos y GPS, mis folios con su línea de tiempo, entrega con firma, todo offline-first | Después |

### Antes que nada: lo que ya se corrigió del #33

Dos cosas del #33 ya quedaron resueltas en la rama de integración y **no** son parte de esta tarea:

1. `abrir-garantia` copiaba el GPS del agente al GPS del domicilio, y un folio de piso abierto con GPS respondía 422. Lo corrige el commit `bb6aa4cf` (`fix(garantias): no copia el GPS del agente al domicilio al abrir folio`). Léelo: el GPS de quien guarda va al evento; el del domicilio, al folio, y son datos distintos (`app/abrir_garantia.go`, comentario de `AbrirGarantiaCmd`).
2. El commit `2e9df694` había reescrito `docs/superpowers/plans/task-9-report.md`, que es el reporte de la tarea 9 de **analytics**, no de garantías. Se restauró a su versión de `main` en `3c3b4948`.

Todo lo demás que salió de la revisión del #33 entra en la parte 1.

---

## Leer antes de escribir

**Para la parte 1 (`msp-api`):**
1. El último comentario del #33 (la revisión completa): cada pendiente de la parte 1 sale de ahí, con el mutante que hoy sobrevive.
2. `internal/garantias/app/service.go`: `revisarClave` y `resolverDuplicada`. Y el commit `0c102ad4` del #26, de donde salen las pruebas de carrera que hay que portar.
3. `internal/garantias/app/cerrar.go` (`puntosDeAuditoria`) y `app/entregar.go` (`firmaIDDe`).
4. `internal/garantias/app/huella.go` y `infra/garhttp/handlers_comandos.go` (el handler de `entregar-garantia`).
5. Tu reporte, `docs/superpowers/plans/garantias-task-9-report.md`, tal como quedó en `main`.

**Para la parte 2 (`msp-app-kt`):**
1. El archivo de reglas de la raíz del repo, completo, en especial las compuertas (`:59-72`), las pruebas (`:76-81`) y los textos de la interfaz (`:87-108`). Y `INTEGRACION.md` (`:42` sobre el esquema de Room, `:71-80` sobre cómo se da de alta un módulo).
2. El spec de garantías, **§6** (rutas, idempotencia, códigos) y **§7** (permisos). Del lado del API, sólo para leer: `internal/garantias/infra/garhttp/dto.go`, `dto_comandos.go`, `dto_imagenes.go`, `idempotencia.go`, `routes.go` y `routes_comandos.go`. De ahí salen los nombres exactos de los campos; no los adivines.
3. La captura de visitas, que es el patrón a seguir:
   - `feature/visitas/.../domain/port/VisitaPorts.kt` (puertos `RegistroDeVisitaPort` y `UbicacionPort`);
   - `app/.../data/visitas/RegistroDeVisitaAdapter.kt:168-225` (guardar en una transacción, fotos fuera de ella, encolar);
   - `app/.../workers/PendingVisitsWorker.kt` (reintento con la misma clave, multipart, borrar el archivo sólo de lo que sí subió, `:62-80`, `:100-110`, `:197-274`);
   - `app/.../workmanager/WorkManagerUtils.kt:26-40` y `:147-162` (nombre único y `ExistingWorkPolicy.KEEP`) y su guardia `WorkEnqueuePolicyGuardTest.kt`;
   - `app/.../data/api/services/visits/V2VisitsApi.kt:42-86` (`@Header("Idempotency-Key")`);
   - `app/.../data/visitas/ComprobantesDeVisitaAdapter.kt` (foto a `cacheDir`, compresión a `filesDir`, barrido de huérfanos) y `UbicacionDeVisitaAdapter.kt:36-57` (GPS).
4. `core/upload/.../UploadDecision.kt:36-101`: la clasificación compartida de respuestas. Ojo con el 409 (abajo, D6).
5. Room: `core/database/.../AppDatabase.kt`, `migrations/Migration32to33.kt`, `src/test/.../migration/Migration32to33Test.kt` y `SchemaIntegrityTest.kt`.
6. Hilt y red: `feature/visitas/.../di/VisitasDataModule.kt:45-65`, `app/.../di/VisitasPortsModule.kt:65-104`, y `app/.../di/NetworkModule.kt:32-72` con su guardia `NetworkKillSwitchGuardTest.kt`.
7. Roborazzi: `feature/pagos/src/test/.../screenshot/GarantiaScreenshotTest.kt` y su base `PagosScreenshotTest.kt:42-84`; y `core/designsystem/src/test/.../ElRenderEffectSeAplicaDeVerdadTest.kt:57-61`, que explica por qué `testDebugUnitTest` solo no compara nada.
8. Navegación y menú: `feature/visitas/.../ui/VisitasRutas.kt`, `app/.../navigation/DestinosDeCobranzaGraph.kt:303` y `app/.../navigation/menu/CatalogoDelMenu.kt`.
9. `feature/visitas/build.gradle.kts`: el módulo nuevo se arma igual.

---

## Las decisiones ya tomadas — no se re-litigan

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas.

### Parte 1

#### P1. Cada pendiente se cierra con una prueba que mata a su mutante

La revisión del #33 midió estos mutantes y **todos sobreviven hoy**. Tu prueba nueva tiene que matarlos; el parche de cada uno es el de la tabla de mutantes de abajo.

- **Getters que devuelven copia.** Una prueba **por getter** (`Dictamen()`, `Ruta()`, `Desenlace()`): se modifica el valor devuelto (`*a.Dictamen() = …rechazada`) y se comprueba que el agregado no cambió.
- **`UpdatedAt` del folio en los trece métodos** de `garantia.go` (de `AgregarArticulo` a `Cancelar`; la lista con líneas está en la revisión). Cada prueba avanza el reloj a una hora **distinta** antes de cada paso y comprueba `g.UpdatedAt()` igual a esa hora. Una hora repetida entre pasos no prueba nada: el mutante sigue vivo.
- **`UpdatedAt` a nivel artículo en `marcarNoDevuelto` y `moverA`**, con la misma técnica. La prueba tiene que matar el mutante que deja la bandera de modificado pero no mueve `UpdatedAt`; la bandera la cubre otra prueba.
- **Identity.** El usuario de Identity en `app` se llama con un nombre que no aparece en **ningún** otro fake ni evento sembrado (hoy es `"Juan"`, que también es `AbiertoPor` y el `Usuario` de los eventos). Por ejemplo `"Itzel Domínguez Ochoa"`. `TestComandos_UsuarioSaleDeIdentity` se extiende a **los catorce comandos**, no a dos.
- **Las pruebas de carrera del #26**, portadas del commit `0c102ad4`: `…CarreraConClaveDeOtroComando`, `…ClaveMismoComandoOtroArticulo` y `…ClaveMismoComandoOtraEtapa`. Ejercitan el camino de `resolverDuplicada` (después del rollback por clave duplicada) con una huella distinta, y esperan 409 `warranty_idempotency_key_mismatch`. Tienen que ponerse rojas con `resolverDuplicada` comparando sólo el folio.
- **Recolección sin GPS en `app/cerrar.go`** (`puntosDeAuditoria`): una prueba a nivel `app` con una recolección sin coordenadas, que da `no_aplica` y no truena. Hoy esa línea tiene 0 ejecuciones.
- **`firmaIDDe` con clave inválida** (`app/entregar.go`): devuelve `uuid.Nil` y el blob se borra.

#### P2. La huella de `entregar`: se decide midiendo

La huella incluye hoy el `Content-Type` que manda la parte multipart de la firma y `DeviceCreatedAt` con su zona horaria. Eso puede dar 409 en un reintento legítimo: los mismos bytes enviados como `application/octet-stream`, o la misma hora escrita con otro offset.

1. **Primero se mide** con `httptest` y el router real, y los dos casos van al reporte como MEDIDO, con la salida: (a) misma firma, misma clave, `image/png` y luego `application/octet-stream`; (b) la misma hora como `2026-11-10T18:00:00Z` y luego `2026-11-10T12:00:00-06:00`.
2. **Después decides**, y lo escribes en el reporte con el porqué. El criterio es éste: la huella describe **el contenido** de la operación, no cómo viajó. Si un reintento con el mismo contenido puede dar 409, se normaliza antes de calcularla (por ejemplo, la hora a UTC y el tipo de la firma detectado por sus bytes, que es lo que el helper de blobs ya valida). Si decides no cambiar nada, el reporte dice por qué ese 409 es correcto.
3. Lo que decidas queda guardado por una prueba que se pone roja al revertirlo, y la prueba de reflexión de la huella sigue verde.

#### P3. El reporte de la tarea 9 se corrige, no se reescribe

`garantias-task-9-report.md` se corrige en su lugar, con una nota al principio que diga qué cambió y en qué commit:

- Cada mutante K1–K15 con su **parche `diff`** y su salida **roja** y **verde**, reproducibles en el SHA citado.
- Cada prueba citada **existe** en ese SHA. Lo compruebas con `git grep -n "func <Nombre>(" <SHA>`, y la salida va al reporte. Las cuatro equivocadas (K1, K2, K3 y K10) llevan el nombre de la prueba que realmente lo mata.
- Cada afirmación lleva su nivel: **MEDIDO** (se corrió, con fecha, lugar y salida), **LEÍDO** (`archivo:línea`), **INFERIDO** (y qué lo volvería MEDIDO). Lo que no se pudo medir lo dice así.
- Salen las frases "Ejecutada sobre el árbol de trabajo actual" y la cobertura de `infra/storage` que no se reproduce. Si la atribuyes a Windows, va como INFERIDO y con el comando que lo comprobaría.
- Se declara el cambio en `garfb/guardar_test.go`, y se reponen el ejemplo de petición y respuesta por ruta y la línea de tiempo leída de la base por cada e2e.

### Parte 2

#### D1. Arquitectura: todo en `:feature:garantias`, nada en el código anterior

- Módulo nuevo `:feature:garantias`, armado como `feature/visitas/build.gradle.kts`: `msp.android.library`, `msp.android.compose`, `msp.hilt`, `msp.test`, `msp.kover`, `msp.detekt`, ktlint y roborazzi. Se da de alta en `settings.gradle.kts` y en `app/build.gradle.kts` (`INTEGRACION.md:71-80`).
- Capas dentro del módulo: `domain/` (modelos y puertos, sin Android), `data/` (Room, Retrofit, cola), `ui/` (pantallas y ViewModels), `di/`. Lo que necesite piezas de `:app` (WorkManager, cámara, GPS) entra por **puertos** del módulo y se cablea en un `app/.../di/GarantiasPortsModule.kt`, como visitas.
- **El código de garantías que ya existe en `:app` no se toca:** `features/guarantees/**`, `PendingGuaranteesWorker`, `PendingGuaranteeEventsWorker`, `GuaranteesApi` y las tablas `garantias`, `garantia_imagenes`, `garantia_eventos`. Es otro sistema (rutas `garantias/nueva`, sin `Idempotency-Key`) y sigue funcionando como hoy. Lo nuevo no lee ni escribe esas tablas.
- La entrada del menú `GARANTIAS` sigue apuntando a la pantalla actual. La pantalla nueva se registra con su `NavGraphBuilder.destinoDe…()` y sólo es alcanzable en el sabor `devlocal`. Cambiar la entrada del menú es una decisión aparte, no de esta tarea.

#### D2. La lógica vive en el servidor; el teléfono pinta

- El teléfono no calcula etapas, transiciones, distancias, auditoría ni estados. Las acciones que ofrece para un artículo salen de `GET /v2/garantias/catalogos` (la tabla de transiciones del servidor), guardado en caché para usarse sin señal. Si el catálogo dice que una acción no es válida, el botón no aparece; si el teléfono se equivoca por un catálogo viejo, el servidor responde 422 y eso se muestra.
- Los permisos los decide el servidor (§7). Un 403 se muestra como "Sin permiso" y no se reintenta.
- Las fechas que se muestran vienen del servidor en RFC3339 UTC y sólo se formatean a la hora local.
- Las ubicaciones son **estimaciones**: nunca se rotulan "Casa" ni "Trabajo". El GPS del cobrador se rotula como lo que es ("Ubicación de la recolección", "Ubicación de la entrega").
- Textos: español, primera letra mayúscula, sin punto final, dos a cuatro palabras (reglas de la raíz del repo, `:87-108`). Nunca "ciclo"; se dice "semana".

#### D3. La cola: lo que se encola no cambia nunca

Éste es el punto de toda la parte 2. El API responde 409 `warranty_idempotency_key_mismatch` a un reintento con la misma clave y **cualquier** campo distinto, incluida la hora del dispositivo con otro formato.

- Tabla `garantia_comandos_pendientes`: una fila por comando, con su `clave` (UUID generado **una vez**, al guardar), la operación, la ruta ya resuelta o la referencia local del folio, el **cuerpo ya serializado** (texto JSON exacto, o los campos del multipart como texto y la ruta del archivo), el `device_created_at` ya escrito en UTC con `Z`, el estado (`pendiente`, `enviado`, `rechazado`) y el último error.
- El trabajador reenvía **esos mismos bytes** con **esa misma clave**. No reconstruye el cuerpo desde objetos del dominio, no vuelve a leer la hora ni el GPS, no regenera la clave. El `Content-Type` de la firma es fijo (`image/png`).
- Una fila encolada **no se edita**. Si el usuario quiere otra cosa, es otro comando con otra clave.
- Encolado con nombre único y `ExistingWorkPolicy.KEEP`, como `WorkManagerUtils.kt:147-162`, y la guardia `WorkEnqueuePolicyGuardTest` cubre el nombre nuevo.
- Guardar el comando y sus fotos va en una sola transacción de Room para la fila del comando; la foto nunca bloquea el guardado (el patrón de `RegistroDeVisitaAdapter`).

#### D4. Folios abiertos sin señal: los comandos esperan a su folio

El `id` del folio lo genera el servidor. Un folio abierto sin señal todavía no lo tiene, y los comandos que dependen de él (agregar artículo, recolección) no pueden enviarse.

- Cada folio abierto en el teléfono tiene una **referencia local** (UUID local, nunca enviado como `id`). Los comandos que dependen de él guardan esa referencia.
- La cola se procesa **en orden por folio**: un comando no se envía mientras su anterior en el mismo folio siga pendiente. Al responder `abrir-garantia`, el `id` del folio se guarda y se resuelve en los comandos que esperaban.
- Para saber qué artículo creó un `agregar-articulo` o qué evento creó una recolección, el teléfono pide el detalle (`GET /v2/garantias/{id}`) y busca el evento con `clave_idempotencia` igual a la clave del comando: su `articulo_ref` o su `id` es el que buscaba. El teléfono no adivina por orden ni por fecha.
- Si un comando queda `rechazado`, los que dependen de él en el mismo folio se quedan detenidos y la pantalla lo muestra. No se envían.

#### D5. Las cuatro capturas

1. **Abrir folio** (`POST /v2/garantias`): origen `piso` o `cliente`. En `cliente`, el cliente y la venta se eligen de los datos que el teléfono ya tiene; el domicilio se copia del cliente. Nunca se manda el GPS del cobrador como GPS del domicilio. Se abre y se agrega el primer artículo (`POST …/articulos`) como dos comandos de la misma cola.
2. **Recolección**: `POST …/articulos/{aid}/avanzar` con `etapa_destino=recolectado`, el GPS del cobrador si lo hay, y las fotos. Cada foto se sube después a `POST …/eventos/{eid}/imagenes` (campo `archivo`, máximo 10 MB) con **su propia clave**, generada una vez al tomar la foto, porque esa clave es el `id` de la imagen en el servidor. Sin GPS, la recolección se guarda igual y la pantalla dice "Sin ubicación".
3. **Mis folios y su detalle**: el API no tiene hoy filtro por usuario, así que "mis folios" son los folios que este teléfono abrió o tocó (tabla `garantia_folios_seguidos`), cada uno leído del servidor con `GET /v2/garantias/{id}`; los que aún no se envían aparecen como "Pendiente de envío". La línea de tiempo se pinta tal como viene en `eventos`, con sus imágenes (`GET …/imagenes/{iid}` con el token). Esta limitación se declara en el reporte: no se filtra la bandeja en el teléfono.
4. **Entrega con firma** (`POST …/entregar`, multipart con `device_created_at`, `gps_lat`, `gps_lon` y `firma`): la firma se dibuja en pantalla, se guarda como PNG de máximo 1 MB en `filesDir` antes de encolar, y el GPS es **obligatorio** para encolar. Sin GPS el botón queda deshabilitado con "Esperando ubicación": un comando encolado sin GPS daría 422 para siempre.

#### D6. Qué se reintenta y qué no

`core.upload.classifyUpload` trata el 409 como reintento. Para garantías eso es un error: un 409 de huella se repetiría para siempre. Por eso el módulo tiene su propio clasificador, `ClasificadorDeEnvio`, que decide por el **código** del cuerpo y no sólo por el status, y su KDoc explica por qué no se reusa `UploadDecision`:

| Respuesta | Decisión |
|---|---|
| 2xx | enviado |
| `IOException`, 408, 425, 429, 5xx | reintentar, mismos bytes y misma clave |
| 401 | reintentar después de renovar el token |
| 409 `warranty_idempotency_key_mismatch` o `warranty_idempotency_key_other_folio` | **rechazado**, no se reintenta, se muestra |
| cualquier otro 409 (la carrera de la clave duplicada) | reintentar |
| 403 | rechazado, "Sin permiso" |
| 422 y demás 4xx | rechazado, con el `detail` del servidor |

Los códigos exactos se copian de `internal/garantias/domain/errors.go` en `msp-api` y la tabla del reporte los cita con `archivo:línea`.

#### D7. Room: una migración aditiva, la siguiente versión

- Tu versión es la de `AppDatabase.kt` en tu base **más uno**. Antes del lunes 9 se integra en `main` trabajo pendiente que lleva Room a la **39**, así que la tuya es la **40** (`Migration39to40`). Saca tu rama de `main` **después** de hacer `git pull` el lunes y confirma la versión en `AppDatabase.kt`; si al empezar `main` sigue en 33, avisa en el PR antes de crear la migración. Si `main` sube antes de fusionar, rebasas y renumeras; una versión que ya está en `main` no se reutiliza nunca.
- La migración sólo **crea tablas nuevas** (`garantia_comandos_pendientes`, `garantia_fotos_pendientes`, `garantia_folios_seguidos`, y la caché de catálogos si la guardas en Room) con sus índices. Nada de `DROP`, `ALTER COLUMN`, cambio de llave o de índice existente, ni `fallbackToDestructiveMigration`. Las migraciones y los esquemas anteriores no se tocan.
- En `AppDatabase.kt` sólo cambian: `version` +1, las entidades nuevas, los DAO, los imports y `addMigrations`.
- El JSON del esquema lo genera `:core:database:kspDebugKotlin` (`INTEGRACION.md:42`); no se escribe a mano. `SchemaIntegrityTest.LATEST_SCHEMA_VERSION` sube a tu versión.
- La migración tiene su prueba con `MigrationTestHelper`, como `Migration32to33Test`. El paquete de migraciones está en piso de cobertura **100%**.

#### D8. Hilt y red

- La interfaz Retrofit de garantías v2 sale del mismo proveedor v2 que usan visitas, con el token del `BearerAuthInterceptor`. **Nada de `@Singleton` sobre algo que dependa de la URL base**: congela el interruptor de la flota (`NetworkModule.kt:32-72`). `NetworkKillSwitchGuardTest` tiene que seguir verde con tu módulo adentro.
- `config/api_settings` no se toca.

---

## Parte 1 (`msp-api`)

Todo lo de P1, P2 y P3.

### Pruebas de la parte 1

Las de P1, una por pendiente. Las de P2: los dos casos medidos y la prueba que guarda tu decisión. Todas siembran sus propios datos y comparan sólo los IDs que sembraron.

### Mutantes de la parte 1

Los corres tú y los reportas, cada uno como **parche `diff`** con la salida roja de la prueba que lo mata y la verde al revertirlo.

| # | Mutante |
|---|---|
| T1 | `Dictamen()` devuelve el puntero interno |
| T2 | `Ruta()` devuelve el puntero interno |
| T3 | `Desenlace()` devuelve el puntero interno |
| T4 | un método del folio no llama `MarkUpdatedAt` (uno por cada uno de los trece: T4a–T4m) |
| T5 | `marcarNoDevuelto` marca modificado pero no mueve `UpdatedAt` |
| T6 | `moverA` marca modificado pero no mueve `UpdatedAt` |
| T7 | el actor sale de `g.AbiertoPor()` y no de Identity (debe morir en `app`, no sólo en `garhttp`) |
| T8 | `resolverDuplicada` compara sólo el folio, no la huella |
| T9 | `puntosDeAuditoria` sin el filtro `lat == nil \|\| lon == nil` |
| T10 | `firmaIDDe` con clave inválida devuelve un UUID en vez de `uuid.Nil` |
| T11 | se revierte tu decisión de P2 |

**Pisos**, sobre el SHA que entregas: `domain` 99% (el del archivo de pisos), `app` ≥ 96%, `garhttp` ≥ 90%, `storage` ≥ 85%, `garfb` ≥ 85% con `FB_DATABASE`. Y `make test-mutation-garantias-app` en verde.

---

## Parte 2 (`msp-app-kt`)

D1 a D8: el módulo, sus tablas y su migración, la cola con su trabajador y su clasificador, las cuatro capturas con sus pantallas, y la integración en `:app` (puertos, destinos de navegación, guardia de encolado).

### Pruebas de la parte 2

Con fakes, sin MockK (reglas de la raíz del repo, `:76-81`), y con Room en memoria.

1. **La cola no cambia lo encolado.** Se encola un comando, el servidor falso responde 503, se reintenta: las **dos** peticiones tienen la misma clave y el mismo cuerpo **byte por byte** (incluido el multipart de la firma con su `Content-Type`). Se compara el cuerpo recibido, no el objeto.
2. **Clasificador**, tabla completa de D6: cada fila con su respuesta y su decisión. El 409 de huella es rechazado; el otro 409, reintento; `IOException`, reintento.
3. **Orden por folio (D4):** un folio abierto sin señal con artículo y recolección; mientras `abrir` falla, los otros dos no salen; cuando `abrir` responde, salen en orden y con el `id` del servidor en la ruta. Un comando rechazado detiene a los que dependen de él.
4. **Resolver por clave:** el artículo y el evento se toman del detalle por `clave_idempotencia`, con un detalle falso que tiene **otro** evento del mismo tipo antes, para que tomar "el último" o "el primero" falle.
5. **Fotos:** cada foto con su clave fija; un reintento de la foto usa la misma clave; el archivo se borra sólo cuando su subida respondió 2xx.
6. **Entrega sin GPS no se encola** y la firma de más de 1 MB no se acepta.
7. **ViewModels:** cada pantalla en sus estados (cargando, con datos, sin señal con pendientes, rechazado) con fakes de los puertos.
8. **Migración Room:** de tu versión menos uno a tu versión con `MigrationTestHelper` y `runMigrationsAndValidate`, más `SchemaIntegrityTest` actualizado. Que las tablas existentes no cambian lo comprueba la validación del esquema.
9. **Capturas Roborazzi** de las cuatro pantallas, claro y oscuro, con los tres tamaños de letra, como `GarantiaScreenshotTest`. Antes de verificar se borra `feature/garantias/build/outputs/roborazzi`, y se corre `:feature:garantias:verifyRoborazziDebug`. `testDebugUnitTest` solo **no** compara imágenes. Las imágenes grabadas se revisan una por una, y el umbral no se sube.

### Mutantes de la parte 2

Mismo formato: parche, salida roja, salida verde.

| # | Mutante |
|---|---|
| A1 | el trabajador genera una clave nueva en cada reintento |
| A2 | el trabajador reconstruye el cuerpo en cada intento (con la hora actual) |
| A3 | el clasificador manda el 409 `warranty_idempotency_key_mismatch` a reintento |
| A4 | el clasificador manda `IOException` a rechazado |
| A5 | un comando sale aunque su anterior del mismo folio siga pendiente |
| A6 | el evento de la recolección se toma como el último de la línea de tiempo y no por clave |
| A7 | la foto se borra antes de que su subida responda 2xx |
| A8 | la entrega se encola sin GPS |
| A9 | el `Content-Type` de la firma sale del archivo y no es fijo |
| A10 | el encolado usa `ExistingWorkPolicy.REPLACE` |

**Pisos:** `:feature:garantias` con piso de Kover **85% de líneas** en `domain` y `data`, declarado en su `build.gradle.kts`. El paquete de migraciones de `:core:database` sigue en 100%.

### Prueba contra el API local

- En **tu** base local, nunca en una compartida: `make fb-snapshot NAME=antes-t11`, `make fb-migrate-status`, y si faltan, `make fb-migrate-up` hasta la `000070` y la `000071`. La salida de `fb-migrate-status` va al reporte.
- El API local en Docker y la app en el sabor `devlocal`, apuntando a él. Ningún otro sabor.
- Recorres los cuatro flujos de punta a punta, **uno de ellos con el teléfono sin señal** al guardar y con señal después. El reporte lleva, por cada flujo: la petición y la respuesta (con la clave), la línea de tiempo leída de la base al final (`MSP_GA_EVENTO` del folio, con `CLAVE_IDEMPOTENCIA`) y las imágenes en `MSP_GA_IMAGEN` con su `TIPO`. Y un reintento forzado (corta la red después de que el servidor guardó) que da 200 y **una sola fila**.
- No se genera ni se publica APK, y no se toca nada de producción.

---

## Archivos que puedes tocar

**Parte 1 (`msp-api`):**
```
internal/garantias/domain/*_test.go
internal/garantias/app/*_test.go
internal/garantias/infra/garhttp/*_test.go
internal/garantias/app/huella.go, app/entregar.go,
internal/garantias/infra/garhttp/handlers_comandos.go     sólo si tu decisión de P2 lo pide
docs/superpowers/plans/garantias-task-9-report.md
docs/superpowers/plans/garantias-task-11-report.md
```

**Parte 2 (`msp-app-kt`):**
```
feature/garantias/**
settings.gradle.kts, app/build.gradle.kts                (el alta del módulo)
core/database/.../AppDatabase.kt                          (sólo lo que permite D7)
core/database/.../migrations/Migration39to40.kt y su prueba (o el número que te toque)
core/database/schemas/.../40.json                         (generado)
core/database/src/test/.../SchemaIntegrityTest.kt         (la versión)
core/database/.../entity/garantiav2/**, dao/garantiav2/**
app/.../di/GarantiasPortsModule.kt
app/.../workers/PendingGarantiasV2Worker.kt
app/.../workmanager/WorkManagerUtils.kt                   (sólo la función de encolado nueva)
app/src/test/.../workmanager/WorkEnqueuePolicyGuardTest.kt (el nombre nuevo)
app/.../navigation/DestinosDeCobranzaGraph.kt             (el destino nuevo)
docs/garantias-captura-report.md
```

Nada más. En particular, nada en `app/.../features/guarantees/**`, en los trabajadores de garantías actuales, en `core/upload/**`, en `feature/pagos/**`, en `config/api_settings` ni en migraciones existentes. Si necesitas tocar algo fuera de la lista, pregúntame antes.

---

## Errores que no se repiten

Cada uno pasó en la entrega anterior. Si la entrega repite uno, rebota.

1. **Entregar tarde.** La tarea 9 se integró el 5 de noviembre, dos días después de su fecha. Si ves que no llegas, avisas **ese mismo día**, no después de la fecha.
2. **Empujar directo a una rama compartida.** Siempre rama propia y PR contra la rama que dice este brief. En los dos repos.
3. **Saltarse los hooks.** Nada de `--no-verify`, `LEFTHOOK_EXCLUDE` ni equivalentes. En `msp-app-kt` los hooks se instalan con `./gradlew installGitHook` y su variante de pre-push; si un hook falla, se arregla la causa.
4. **Un reporte que no se puede reproducir.** Cada prueba citada existe en el SHA citado; cada mutante lleva su parche y sus dos salidas; cada número sale del SHA commiteado, con el árbol limpio y la caché de pruebas limpia (`go clean -testcache` en `msp-api`, `--rerun-tasks` en `msp-app-kt`). Nada del árbol de trabajo. Cada afirmación con su nivel: MEDIDO, LEÍDO o INFERIDO.
5. **Commits fuera del alcance.** Un commit que toca un archivo fuera de la lista de arriba —como el reporte de analytics de la vez pasada— rebota completo. Antes de abrir el PR: `git diff --name-only origin/main...HEAD`, y la salida va al reporte.
6. **Pruebas que dependen de datos previos.** Comparas sólo los IDs que sembraste, y siembras también el caso que **no** debe aparecer.

---

## Verificación, antes de entregar

**Parte 1 (`msp-api`):**
```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
go tool cover -func=cov.out | grep -E 'garantias/(domain|app|infra)'
make coverage-garantias
make test-mutation-garantias-app
make check-sealed MODULE=garantias
grep -rn 'Ã' internal/garantias        # debe salir vacío
```
Con la base arriba, y una vez sin `FB_DATABASE`: lo de Firebird se salta y nada falla.

**Parte 2 (`msp-app-kt`):**
```sh
./gradlew test detekt ktlintCheck
rm -rf feature/garantias/build/outputs/roborazzi
./gradlew :feature:garantias:verifyRoborazziDebug
./gradlew :feature:garantias:koverVerify :core:database:koverVerify
./gradlew :core:database:testDebugUnitTest --tests '*Migration*' --tests '*SchemaIntegrity*'
./gradlew prePushCheck --rerun-tasks
```

---

## Reporte

Dos reportes, uno por repo:

- **`msp-api`:** `docs/superpowers/plans/garantias-task-11-report.md`. El SHA, la salida **literal** de la verificación sobre ese SHA, la tabla T1–T11 con parches y salidas, los dos casos medidos de P2 y tu decisión con el porqué, el `git grep` de cada prueba citada y el `git diff --name-only`.
- **`msp-app-kt`:** `docs/garantias-captura-report.md`. El SHA, la salida literal de la verificación, la tabla A1–A10 con parches y salidas, la tabla del clasificador con los códigos citados de `errors.go`, la prueba de la migración, la lista de capturas Roborazzi revisadas, y la evidencia contra el API local de cada flujo (peticiones, respuestas y filas leídas de la base).

Lo que hayas hecho distinto al brief, declarado con el porqué. Lo que no pudiste medir, dicho así.

---

## Calendario

| Cuándo | Qué |
|---|---|
| **Viernes 13 de noviembre, 1pm** | **Entrega única**: las dos partes completas, los dos PR (contra `main` en cada repo) y los dos reportes |

Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
