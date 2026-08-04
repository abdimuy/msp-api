# Plan: Migrar VISITAS al API v2 con robustez de pagos + cierre de deploy

## Context

Las **visitas de cobranza NO están migradas** al API en Go. Hoy van 100% por el Node legacy:
`PendingVisitsWorker` → `VisitsApi.saveVisit` (`@POST("visitas")` vía `ApiProvider` legacy) → tabla
Firebird `MSP_VISITAS` (~226k filas). Visitas es greenfield total en el server.

Dos problemas que este plan resuelve con la misma robustez y tests que pagos:
1. **Bug de pérdida de datos.** `SalesViewModel.syncSales()` llama `visitsStore.deleteAllVisits()` =
   `DELETE FROM Visit` **sin WHERE** en un tap rutinario del botón de sincronizar. Borra visitas aún sin
   subir (`GUARDADO_EN_MICROSIP=0`) antes de que el worker las envíe. `VisitDao` no tiene un
   `deleteUploaded()` scopeado.
2. **Worker frágil.** `PendingVisitsWorker` es la versión vieja: cualquier `Exception` → `Result.retry()`,
   sin clasificación HTTP, sin idempotencia, sin captura durable de fallidos, sin seams de test.

Hallazgos de exploración:
- `MSP_VISITAS` guarda bytes UTF-8 (verificado empíricamente) aunque sus columnas de texto son
  `CHARACTER SET NONE` (creada por el Node). → El writer escribe **UTF-8 plano** (NO
  `firebird.EncodeWin1252`; NONE acepta los bytes tal cual, sin `-303`).
- Schema real de `MSP_VISITAS`: `ID CHAR(36)`, `COBRADOR VARCHAR(150)`, `COBRADOR_ID INT`,
  `FECHA TIMESTAMP`, `FORMA_COBRO_ID INT`, `LAT/LNG DOUBLE`, `NOTA VARCHAR(10000) nullable`,
  `TIPO_VISITA VARCHAR(100)`, `ZONA_CLIENTE_ID INT`, `CLIENTE_ID INT`, `IMPTE_DOCTO_CC_ID INT nullable`.
  Sin imágenes, sin aplicación a Microsip → más simple que pagos.
- El pipeline de la app ya es estructuralmente idéntico al de pagos; solo hay que endurecer el worker y
  agregar la capa v2.

## Decisiones (del dueño)
- Nuevo módulo `internal/visitas` (vertical slice).
- Migración create-if-not-exists de `MSP_VISITAS` + escribir a la tabla existente.
- Reusar el permiso `PermCobranzaVerPagos` (el cobrador ya lo tiene).
- Solo camino de escritura (subir visitas) + robustez.
- Encoding: UTF-8 plano.

## Global Constraints
- Reglas CLAUDE.md: cero lógica en la BD (sin DEFAULT CURRENT_TIMESTAMP, sin triggers, UUID/time en Go),
  vertical slices, código en inglés / mensajes al usuario en español, encoding UTF-8 para tablas MSP_*.
- Writer escribe UTF-8 plano en `COBRADOR/NOTA/TIPO_VISITA` (bind directo del string Go; NO
  `EncodeWin1252`). Lee texto como `string` plano; evitar `COALESCE(col,'')` sobre columnas NONE.
- Fechas: UTC en Go, `firebird.ToWallClock` al escribir, `firebird.ScanUTCTime` al leer; contrato RFC3339
  UTC con el front.
- Idempotencia por ID, espejo de cobranza (Insert → si existe → FindByID → devolver existente).
- Cross-check Idempotency-Key vs body.id con skip `!httpdispatch.IsInternal(ctx)` (verbatim de `CrearPago`).
- Permiso: `auth.PermCobranzaVerPagos`.
- Cobertura pisos: domain ≥99 / app ≥90 / fb ≥80 / http ≥70; gremlins ≥80% en domain+app.
- Tests server dentro de `WithTestTransaction` (rollback). AndroidTest Room in-memory + MockWebServer.
- No atribución de Claude en commits.

---

## Parte A — Server: nuevo módulo `internal/visitas` (msp-api)

Referencia: esqueleto de `internal/rutas` (routes.go/auth.go/Huma-humachi) + mecánica de
INSERT+idempotencia de `internal/cobranza` (simplificada, sin Microsip ni multipart).

**A1. Migración `migrations-firebird/000047_create_msp_visitas.up.sql` (+ `.down.sql`).**
`EXECUTE BLOCK` idempotente que crea `MSP_VISITAS` solo si no existe (checa `RDB$RELATIONS`), con
convenciones para envs nuevos: `ID CHAR(36) CHARACTER SET ASCII PK`, texto `CHARACTER SET UTF8`,
`FECHA TIMESTAMP`, `LAT/LNG DOUBLE PRECISION`, sin `DEFAULT CURRENT_TIMESTAMP`, sin triggers. En
test/prod la tabla ya existe (NONE, bytes UTF-8) → el bloque la respeta. Footer
`INSERT INTO MSP_MIGRATIONS`. Documentar en el `.up.sql` que la tabla preexistente es NONE-con-bytes-UTF8
y el writer escribe UTF-8 plano.

**A2. `internal/visitas/domain/visita.go`.** Entidad `Visita` (campos privados, dos constructores:
`NewVisita` genera `uuid.New()`/`time.Now()` donde aplique; `RehydrateVisita` para reads) + VO/validación
(campos requeridos; `FECHA` parseable; sin guard agresivo de antigüedad; opcional: rechazar fecha
absurdamente futura) + sentinel errors en español-código: `ErrVisitaYaExiste`,
`ErrVisitaClienteRequerido`, `ErrVisitaTipoRequerido`, etc. Embede `audit.Auditable` (Tipo A CRUD).

**A3. `internal/visitas/app/` — `service.go` + `registrar_visita.go`.** `Service.RegistrarVisita(ctx, in, by)`
idempotente por ID espejo de `cobranza.insertAndApplyPago` pero SIN aplicar-a-Microsip: `repo.Insert` → si
`ErrVisitaYaExiste` → `repo.FindByID` → devolver existente; si no, devolver la insertada. Puerto
`ports/outbound.VisitasRepo` (`Insert`, `FindByID`).

**A4. `internal/visitas/infra/visitasfb/repo.go`.** `Insert`/`FindByID` sobre `MSP_VISITAS`, patrón de
`cobranza.PagosRecibidosRepo.Insert`: `firebird.RunInTx` + `firebird.GetQuerier`, `ID().String()`, UTF-8
plano en `COBRADOR/NOTA/TIPO_VISITA` (bind directo; NO `EncodeWin1252`), `firebird.ToWallClock(FECHA)`,
`LAT/LNG` como `float64`. En `FindByID` scan de texto como `string` plano y `firebird.ScanUTCTime(FECHA)`;
evitar `COALESCE(col,'')` sobre columnas de texto NONE. Traducir unique-violation (`firebird.MapError` →
`ae.Code=="firebird_unique_violation"`) a `domain.ErrVisitaYaExiste`.

**A5. `internal/visitas/infra/visitashttp/` — `dto.go`, `handlers.go`, `auth.go`, `routes.go`.**
`POST /visitas` (Huma JSON, no multipart): preámbulo `currentUserOrError` →
`requirePerm(cu, auth.PermCobranzaVerPagos)` → decodear `in.Body` (struct typed) → parsear `ID` uuid →
cross-check Idempotency-Key vs body.id con skip `!httpdispatch.IsInternal(ctx)` (verbatim de `CrearPago`)
→ `svc.RegistrarVisita`. `auth.go` copiado de `rutas`/`cobranza`. `routes.go` con `humachi.New` +
`huma.Register` (op `POST /visitas`).

**A6. Wiring en `cmd/api/server.go`.**
`visitasCapture := failedintent.CaptureMiddleware(failedintent.Config{Store: fiCaptureCfg.Store,
PathPrefixes: []string{"/v2/visitas"}, Methods: []string{http.MethodPost}})` (Blob nil — visitas es JSON).
Montar: `r.Route("/visitas", func(r chi.Router){ r.Use(authn.Handler, visitasCapture);
visitashttp.MountRouter(r, visitasSvc) })`. Proveer `visitasSvc` por fx (repo `visitasfb.New(pool)`).

**A7. Tests server (paridad pagos, dentro de `WithTestTransaction` rollback).**
- Unit `domain` (≥99%) y `app` (≥90%) con fakes.
- e2e real-FB `visitas_lifecycle_e2e_test.go`: (1) POST válido → fila en `MSP_VISITAS` + 2xx;
  (2) idempotencia: 2º POST mismo ID → misma fila campo-por-campo; (3) negativo "2xx no se captura";
  (4) ciclo desk: POST inválido → 422 capturado en `MSP_FAILED_INTENTS` → `replay-with` (JSON, editar
  body) → 2xx → fila en `MSP_VISITAS` → `resolve`→`resolved_manual`. Reusa
  `failedintenthttp.NewService`+`SettableReplayDispatcher`+`failedintentfb.New`.
- `security_test.go` table-driven (401 sin auth / 403 sin permiso / path-injection→no-500).
- Acentos: e2e que inserta `NOTA` con acentos (ñ/ó) y la lee de vuelta idéntica (round-trip UTF-8).

---

## Parte B — App: worker v2 + fix del bug + tests (msp-app-kt)

**B1. `V2VisitsApi` + `VisitV2Mappers`.** `data/api/services/visits/V2VisitsApi.kt`:
`@POST("v2/visitas") suspend fun crearVisita(@Header("Idempotency-Key") id: String, @Body body:
CrearVisitaBody)` (JSON, no multipart). `CrearVisitaBody` (id, cliente_id, cobrador_id, cobrador,
forma_cobro_id, lat, lng, nota, tipo_visita, zona_cliente_id, impte_docto_cc_id, fecha RFC3339-UTC sin
fracciones vía `AppTime`) + `VisitEntity.toCrearVisitaBody()`.

**B2. Flag `VISITAS_USE_V2`.** `buildConfigField("boolean","VISITAS_USE_V2",...)` por flavor en
`app/build.gradle.kts` (devlocal=true, devserver=true, prod=false), espejo de `PAGOS_USE_V2`. `V2_BASE_URL`
se reusa.

**B3. Reescribir `PendingVisitsWorker`** espejo de `PendingPaymentsWorker`: `@JvmOverloads` + seams
`@VisibleForTesting` (`visitsStore`, `v2Api`, `legacyApi`, `useV2=BuildConfig.VISITAS_USE_V2`,
`maxAttempts`). `doWork`: `useV2 ? uploadV2 : uploadLegacy`. `uploadV2`: `v2Api.crearVisita(id,
toCrearVisitaBody())` → `markDone`; `HttpException`→clasificar; `IOException`→`retry`; idempotencia por ID.
Clasificación: reusar `PaymentUploadClassifier.classifyHttpCode` o copiar a
`features/visit/upload/domain/VisitUploadClassifier.kt`.

**B4. FIX bug de pérdida de datos.** Agregar `VisitDao.deleteUploadedVisits()` =
`@Query("DELETE FROM Visit WHERE GUARDADO_EN_MICROSIP = 1")` + `VisitsLocalDataSource.deleteUploadedVisits()`.
En `SalesViewModel.kt:154` reemplazar `visitsStore.deleteAllVisits()` por `deleteUploadedVisits()`.
Comentar el porqué.

**B5. `VisitFactory.fromSale(...)`** en `features/visit/newvisit/VisitFactory.kt`: extraer la construcción
inline de `Visit` de `NewVisitDialog.handleSaveVisit()` (refactor puro) → `fromSale(sale, currentUser,
tipoVisita, formaCobroId, nota, id, fecha)`. Congela atribución: `COBRADOR=sale.NOMBRE_COBRADOR`,
`COBRADOR_ID=currentUser.COBRADOR_ID`, `ZONA_CLIENTE_ID=sale.ZONA_CLIENTE_ID`,
`IMPTE_DOCTO_CC_ID=sale.DOCTO_CC_ACR_ID`, `FORMA_COBRO_ID=0`, `LAT/LNG=0.0`, `GUARDADO_EN_MICROSIP=0`.

**B6. Tests app (paridad pagos).**
- Robolectric: `VisitFactoryTest` (congela atribución, incl. `COBRADOR_ID != sale.COBRADOR_ID`);
  `PendingVisitsWorkerV2Test` (clasificación 2xx/4xx/5xx/red, idempotencia); test de regresión del bug —
  `syncSales()` con pendientes `GUARDADO=0` + subidas `=1` → tras sync sobreviven los pendientes (usar
  `VisitDao` real en Room in-memory).
- Instrumentado `PendingVisitsWorkerE2ETest.kt` (reusa infra `PagosE2ETestBase`): worker+scheduler real vs
  `MockWebServer` → `GUARDADO=1`; red-caída → retry, nunca marca listo; 200 duplicado → idempotente.

---

## Verificación
- **Server:** `FIREBIRD=1 FB_DATABASE=... make test-firebird-all` verde; `go test ./... -short -race`;
  `golangci-lint run ./...` = 0; `gremlins` ≥80% en domain+app (target `test-mutation-visitas`); cobertura
  pisos (domain ≥99 / app ≥90 / fb ≥80 / http ≥70). Boot: `docker restart msp-api-dev` →
  `healthz/readyz 200` + `POST /v2/visitas` sin auth → 401. SQL: cero residuo.
- **App:** `./gradlew testDevlocalDebugUnitTest testDevserverDebugUnitTest` verdes;
  `connectedDevlocalDebugAndroidTest` verde (emulador `Pixel_9_Pro_XL`); `ktlintCheck` verde.
- **Higiene:** e2e FB en `WithTestTransaction` (rollback); androidTest Room in-memory + MockWebServer;
  snapshot antes de escritura interactiva al Firebird compartido.

---

## Parte D — Deploy (pagos + visitas comparten deploy) — REQUIERE APROBACIÓN POR ACCIÓN
1. Integrar a `main` local ambos repos (commits + merge).
2. Push a remoto — requiere `gh abdimuy`.
3. Deploy de msp-api al server de prueba `apidev.loclx.io` (Windows): cross-compile → SSH → binario →
   reiniciar Task Scheduler. Runbook `docs/deploy-test-server-runbook.md`.
4. Aplicar migración 000047 en el Firebird del server de prueba (no-op si la tabla ya existe).
5. Canary en test (con snapshot previo): Pagos (1 real + 1 forzado a fallar→replay); Visitas (1 real con
   acentos + 1 forzado a fallar→replay). Conceder permiso cobranza a cobradores de prueba.
6. Build + distribución del APK `devserver` (pagos + visitas v2 flags=true).
7. Producción (posterior).

## Secuencia
1. Parte A (módulo server + tests) → test-firebird-all limpio + boot verde.
2. Parte B (app worker v2 + fix bug + tests).
3. Verificación completa.
4. Parte D: integrar y desplegar (con aprobación en cada acción consecuente).
