# Plan — Búsqueda de ventas locales con Meilisearch (API + Front)

## Context

El buscador de "ventas locales" en `sistema-cobro-web` está muerto: la caja recoge el texto
pero `src/services/api/getVentasLocales.ts` **nunca lo manda** a la API y el endpoint Go
`/v2/ventas` **no tiene búsqueda de texto** en ninguna capa. Antes funcionaba (la API vieja
de Node hacía `UPPER(NOMBRE_CLIENTE) LIKE '%q%'`); la migración a Go lo perdió.

**Objetivo:** búsqueda real, fuerte y rápida sobre TODO el dataset, con Meilisearch, igual que
el módulo `clientes`. El front no filtra nada: manda params y pinta lo que devuelve la API.
Arquitectura hexagonal en ambos repos. Tests robustos (TDD).

**Repos:**
- Backend: `/Volumes/M2-1TB/Developer/msp-api` (branch `feat/ventas-busqueda-meili`)
- Frontend: `/Volumes/M2-1TB/Developer/sistema-cobro-web` (branch `feat/ventas-busqueda-meili`)

**Referencia gold-standard a copiar (backend):** `internal/clientes/infra/clientessearch/`,
`internal/clientes/ports/outbound/directory_index.go`, `cmd/api/search_wiring.go`,
`cmd/api/clientes_wiring.go`, `internal/platform/meilisearch/`.

**Decisiones (defaults aprobados):**
- **Read path:** el listado completo de `/v2/ventas` pasa por Meili (búsqueda+filtros+orden+
  paginación); Firebird solo hidrata por ID. **Fallback** al listado Firebird actual (keyset)
  cuando Meili no está configurado (dev sin Meili sigue funcionando, sin full-text).
- **Frescura:** incremental por outbox (tiempo real) + worker de reconciliación periódica de
  respaldo (warm-up al boot + recuperación de drift), como clientes.
- **Venta cancelada** = soft-delete: upsert con flag `estado`/`situacion` (sigue buscable con
  `incluir_canceladas`), **nunca** DeleteDocs.

## Global Constraints (OBLIGATORIO — el revisor las usa como lente)

- **No lógica en la DB.** Todo (IDs, timestamps, defaults, derivados) vive en Go. Nada nuevo
  en `migrations-firebird/` para esta feature (índice es Meili, no DB).
- **Vertical slices / hex.** Cross-module solo vía contracts. `depguard` lo obliga. Domain no
  importa nada fuera de stdlib+uuid+decimal+platform/{domain,apperror}.
- **Código en inglés, mensajes de usuario en español** (lowercase, sin punto final). Códigos
  de error `apperror.New*` en snake_case inglés.
- **UTF-8 plano** en columnas `MSP_*`. No `firebird.Win1252` para tablas nuestras.
- **Fechas:** UTC en Go; `firebird.ToWallClock` al escribir, `firebird.ScanUTCTime` al leer.
  En Meili: epoch-segundos `_ts` para sortable.
- **Contrato `VentaDTO` idéntico** — el read path hidrata por ID desde Firebird para preservar
  el DTO byte-por-byte; cero cambios forzados en el front.
- **Reusar** el paquete genérico `internal/platform/meilisearch/` y el patrón `clientes` tal
  cual. Cliente Meili singleton compartido. `AllowUnconfigured` respetado (skip en
  `ErrMeilisearchNotConfigured`).
- **Commits convencionales** (`feat(ventas): ...`), sin atribución a Claude, sin `--no-verify`.
- **Tests:** todo código nuevo lleva tests. Backend Go con gates de cobertura. Integración
  gateada por env (`FB_DATABASE`, `MEILISEARCH_URL`) con `WithTestTransaction`/fakes — nunca
  INSERTs persistentes en la DB compartida. Front con vitest.
- `MaxTotalHitsVentas = 50_000` compartida entre el índice y el guard de cursor.

---

## Task 1 — Backend: Meili core (config + puerto + adapter ventsearch + unit tests)

Reusar tal cual el paquete `internal/platform/meilisearch/` y el patrón de
`internal/clientes/infra/clientessearch/`. TDD.

### 1.1 Config — `internal/platform/config/config.go`
`Meilisearch.IndexName` ya es de clientes. **Agregar** campo `VentasIndexName`
(env `MEILISEARCH_VENTAS_INDEX_NAME`, default `"ventas"`). Reusar `SyncInterval`,
`AllowUnconfigured`, `URL`, `APIKey`, cliente singleton compartido. No tocar el campo de
clientes.

### 1.2 Puerto de búsqueda — nuevo `internal/ventas/ports/outbound/search_index.go`
Espejo de `directory_index.go`:
- `VentaSearchDoc` — struct de puerto, **sin JSON tags**, tipos exactos del dominio
  (`decimal.Decimal`, `time.Time`, `uuid.UUID`). Campos: id, nombre_cliente, telefono,
  direccion, folio, vendedor, tipo_venta, situacion, sincronizacion, zona_cliente_id,
  vendedor_email, cliente_id, estado, fecha_venta, precio_total, created_at.
- `VentasSearchQuery` — `Q string` + filtros como **punteros** (nil = sin filtro): TipoVenta,
  Situacion, Sincronizacion, ZonaClienteID, VendedorEmail, ClienteID, Estado, IncluirCanceladas,
  FechaDesde/FechaHasta (*time.Time), PrecioMin/PrecioMax (*decimal.Decimal) + SortBy string +
  SortOrder string + Offset int + Limit int.
- `VentasSearchResultado{ IDs []uuid.UUID; Total int }` — devuelve **IDs ordenados** (el read
  path hidrata por ID para preservar el `VentaDTO` idéntico).
- Interfaz `VentaSearchIndex`:
  `Buscar(ctx, q VentasSearchQuery) (VentasSearchResultado, error)`,
  `Reconciliar(ctx, docs []VentaSearchDoc) error`,
  `IndexarUno(ctx, doc VentaSearchDoc) error`,
  `Eliminar(ctx, id uuid.UUID) error`.
- Const `MaxTotalHitsVentas = 50_000`.

Mirror exacto de los nombres/estilo del puerto de clientes (`DirectoryIndex` etc.). Si clientes
usa nombres distintos (p.ej. `Indexar`/`Reconcile`), **seguir el estilo real de clientes**
observado en el código, no inventar.

### 1.3 Adapter Meili — nuevo `internal/ventas/infra/ventsearch/`
- `document.go` — `VentaDoc` (flat, **snake_case JSON tags**).
  - `searchableAttributes = [nombre_cliente, telefono, direccion, folio, vendedor]`
  - `filterableAttributes = [tipo_venta, situacion, sincronizacion, zona_cliente_id,
    vendedor_email, cliente_id, estado, fecha_venta_ts, precio_total]`
  - `sortableAttributes = [fecha_venta_ts, precio_total, nombre_cliente, created_at_ts]`
  - `DefaultIndexConfig(indexName string) meilisearch.IndexConfig` (espejo de clientes,
    incluye `MaxTotalHits = MaxTotalHitsVentas` si clientes lo setea así).
  - **Dinero:** patrón Saldo/SaldoStr de clientes — `float64` sort-key (`precio_total`) +
    string exacto (`precio_total_str`) para no perder precisión.
  - **Fechas:** epoch-segundos `_ts` sortable (`fecha_venta_ts`, `created_at_ts`).
  - `direccion` = calle+colonia+población+ciudad combinado (un solo string).
  - `vendedor` = nombres concatenados.
  - PK `id = venta.ID().String()`.
- `index.go` — `MeilisearchVentaSearchIndex{client, indexName}` implementa el puerto:
  `Reconciliar` (batch `UpsertDocs`), `IndexarUno` (`UpsertDocs` de 1), `Eliminar`
  (`DeleteDocs`), `mapDoc` (VentaSearchDoc→VentaDoc). Constructor
  `NewMeilisearchVentaSearchIndex(client, indexName)` espejo de clientes.
- `search_query.go` — `Buscar`: `buildFilter(q)` (cláusulas `AND`; strings con `%q` estilo
  clientes; rangos fecha/precio con `>=`/`<`; cancelada/estado según `IncluirCanceladas`),
  `buildSort(q)`, offset desde `q.Offset`; mapea 503 → `ErrMeilisearchNotConfigured`/
  `ErrMeilisearchTransient` (mismo mapeo que clientes); devuelve IDs (parseando el `id` de cada
  hit a uuid.UUID) + Total (clamp a `MaxTotalHitsVentas`).
- `export_test.go` — expone `buildFilter`/`buildSort`/`mapDoc` (y lo que clientes exponga) +
  reusa/replica el fake `recorder` de clientes para tests.

### 1.4 Tests unit puros (TDD, sin DB/red)
- `document_test.go` — `mapDoc` VentaSearchDoc→VentaDoc: dinero (float+str), fechas→`_ts`,
  direccion combinada, vendedor concatenado, PK. `DefaultIndexConfig` con las 3 listas exactas.
- `search_query_test.go` — `buildFilter` (cada filtro nil vs set; incluir_canceladas on/off;
  rangos fecha/precio) y `buildSort` (cada SortBy/SortOrder; default).
- `index_test.go` — `recorder` fake de `Client`: `Reconciliar`/`IndexarUno`/`Eliminar` llaman
  Upsert/Delete con los docs correctos; `Buscar` mapea la respuesta a IDs+Total.

**Entregable Task 1:** puerto + `ventsearch` completos, unit tests verdes, `golangci-lint run
./...` limpio en los paquetes tocados, build nativo OK. Commit(s) convencionales.

---

## Task 2 — Backend: indexado incremental + read path + wiring + integración

Depende de Task 1. TDD donde aplique.

### 2.1 Indexado incremental — nuevo `internal/ventas/infra/ventoutbox/search_handler.go`
El outbox emite un evento por cada mutación de venta (los `EventType*` en
`internal/ventas/domain/events.go`), drenados post-commit en `app/service.go`. El
`HandlerRegistry` exige un `EventType()` único por handler y el dispatcher solo reclama
`KnownTypes()`. Registrar un `outboxfb.Handler` por cada `EventType*` de venta (wrappers finos
sobre un mismo closure de reindex):
- `Handle(ctx, ev)` → `repo.FindByID(ev.AggregateID)` → `index.IndexarUno(mapDoc(venta))`.
- Idempotente (dispatcher es at-least-once). Mapear `ErrMeilisearchTransient` →
  `outboxfb.ErrTransient` para reintentos.
- Handler `venta.*` = reload-y-upsert: cubre creada/editada/aprobada/cancelada/aplicada/
  imágenes por igual (siempre refleja el estado actual). Cancelada = upsert con flag, no delete.

### 2.2 Read path — `app` + `venthttp`
- App: nuevo método `BuscarVentas(ctx, in)` en `internal/ventas/app/` que orquesta
  `index.Buscar` + hidratación por ID (nuevo `VentaRepo.FindByIDs(ids)` batcheado, o reuso de
  `assembleListItems`, reordenando por el orden de Meili). `ListarVentas` existente queda como
  **fallback** (camino Firebird keyset) cuando el index es nil/no configurado.
- `dto.go` (`ListarVentasInput`): agregar query params `Q` (`query:"search"`), `ZonaClienteID`,
  `VendedorEmail`, `PrecioMin`, `PrecioMax`, `SortBy` (`enum:"fecha_venta,precio_total,
  nombre_cliente"`), `SortOrder` (`enum:"asc,desc"`). No romper params existentes
  (desde/hasta/tipo/situación/sincronización/incluir_canceladas/cursor/limit).
- `handlers_ventas.go` (`ListarVentas`): si Meili configurado → construir `VentasSearchQuery`
  desde params → `BuscarVentas` → IDs ordenados → hidratar → reordenar → `toVentaDTO` →
  `ListResponse[VentaDTO]{Items, NextCursor}` (cursor = offset codificado, guard
  `MaxTotalHitsVentas`). Si no configurado → camino Firebird actual intacto.
- Contrato `VentaDTO` idéntico → cero cambios forzados en el front.

### 2.3 Wiring — `cmd/api/`
- `search_wiring.go`: segundo bootstrap `EnsureIndex(ventsearch.DefaultIndexConfig(
  cfg.Meilisearch.VentasIndexName))` (detached-goroutine + `context.WithoutCancel`, skip en
  `ErrMeilisearchNotConfigured`) — espejo de `registerMeilisearchBootstrapLifecycle`.
- `ventas_wiring.go`: `provideVentasSearchIndex(client, cfg)`, `provideVentasReindexHandlers(
  repo, index)`, `registerVentasOutboxHandlers(reg, handlers...)` (espejo de
  `registerAuthOutboxHandlers`/clientes), `provideVentasReconcileWorker` + su lifecycle (espejo
  de `provideClientesDirectoryReconcileWorker`: warm-up al boot + interval `SyncInterval`).
  Pasar el `index` al service para `BuscarVentas`.
- `main.go`: sumar los `fx.Provide`/`fx.Invoke` nuevos. El endpoint sigue montado vía
  `venthttp.MountRouter` (server.go:205) — pasarle el index/nuevo service.
- Permiso: reusar `PermVentasListar` para la búsqueda; `POST /ventas/_search/refresh` con
  permiso de reindex (espejo `RefrescarBusqueda` de clientes).

### 2.4 Tests backend (robustos, gate por env)
- **Unit handler outbox**: fake `Client` + fake/real `VentaRepo`; asserts `UpsertDocs` y mapeo
  `ErrTransient`. Reusar `fakeOutbox` de `app/fakes_test.go` para verificar que cada mutación
  drena su evento (plantilla: `edit_events_test.go`).
- **Integración Firebird** (`WithTestTransaction`, `fbtestutil`, skip sin `FB_DATABASE`):
  sembrar ventas con `repo.Save`, transicionar, correr `BuscarVentas`/reconcile con un index
  fake o real, asserts.
- **Integración Meili** (skip sin `MEILISEARCH_URL`, patrón `integration_test.go` del platform):
  round-trip EnsureIndex→Upsert→Search→hidratar; polling de `/indexes/{name}/stats`.

**Entregable Task 2:** indexado incremental + read path + wiring completos; `ListarVentas`
enruta Meili/fallback; tests verdes (unit siempre; integración cuando hay env);
`golangci-lint run ./...` limpio; builds nativo+windows OK. Commit(s) convencionales.

---

## Task 3 — Frontend: capa hex de búsqueda + refactor de consumo + vitest (sistema-cobro-web)

Repo `/Volumes/M2-1TB/Developer/sistema-cobro-web`, branch `feat/ventas-busqueda-meili`.
Replicar el módulo de búsqueda de `clientes` (Port + use case + adapter inyectable + hook +
context). **No hay filtrado client-side que quitar** (confirmado). El bug vive en
`src/services/api/getVentasLocales.ts` (params dropeados) y `useGetVentasLocales` (no pasa
`signal` a axios). TDD.

**Referencia gold-standard (front):** el módulo de búsqueda de clientes en `src/modules/`
(Port + usecase + Http*Adapter + Provider/Container + hook). Seguir su estilo exacto.

### 3.1 Capa hex nueva en `src/modules/ventasLocales/`
- `application/ports/VentasListPort.ts` — `buscarVentas(input, signal?): Promise<Output>`.
- `application/dto/BuscarVentasInput.ts` / `BuscarVentasOutput.ts` — input readonly (`search,
  tipoVenta, situacion, sincronizacion, zonaClienteId, vendedorEmail, almacenId, precioMin,
  precioMax, fechaInicio, fechaFin, incluirCanceladas, sortBy, sortOrder, cursor, limit`);
  output `{ items: VentaLocal[]; nextCursor: string }`.
- `application/usecases/buscarVentas.ts` — validación (limit ≤ MAX, sortBy allowlist, sortOrder
  ∈ {asc,desc}) → `port.buscarVentas`. `DomainError(code,msg)`.
- `infrastructure/http/HttpVentasListAdapter.ts` — `constructor(client: AxiosInstance)`, mapea
  **TODOS** los params a snake_case (`search`, `tipo_venta`, `situacion`, `sincronizacion`,
  `zona_cliente_id`, `vendedor_email`, `precio_min`, `precio_max`, `desde`, `hasta`,
  `incluir_canceladas`, `sort_by`, `sort_order`, `cursor`, `limit`), `GET /ventas` con
  `{params, signal}`, `next_cursor` ausente → `""`, errores → `apperrorToDomainError`. Reusar
  `infrastructure/http/apiClient.ts` (axios `${URL_API_V2}/v2` + interceptor Firebase). Omitir
  params nil/undefined (no mandar `precio_min` vacío, etc.).
- `presentation/composition/` + `context` — inyectar el adapter (patrón `ClientesContainer`/
  `ClientesProvider`/`useVentasListPort`), o extender el `ventasLocalesContainer` singleton
  existente si lo hay.

### 3.2 Refactor de consumo
- `useGetVentasLocales` (o nuevo `useBuscarVentas`): consumir el use case vía el port del
  context; **pasar `signal` a axios** (hoy no lo hace); mantener la forma de retorno que ya
  consumen `VentasLocales.tsx`/`VentasTable`/`VentasFilters` para no romper la UI.
- `getVentasLocales.ts`: reemplazar la función `getVentasLocales` por el adapter hex; conservar
  `adaptVentaV2ToLocal` (VentaDTO→VentaLocal) y las demás funciones (detalle/imágenes/resumen/
  vendedores) que aún pegan a la API legacy.
- `VentasLocales.tsx`: ya manda `search` y filtros a `setParams` — solo empezarán a viajar.
  Verificar que `sortBy/sortOrder` de las vistas se envíen (hoy se dropean).

### 3.3 Tests front (vitest — jsdom + RTL + MSW + fakes ya listos)
- **Use case** (`buscarVentas.test.ts` + `FakeVentasListPort`): forwarding de input, códigos
  `DomainError` de validación, propagación de `signal`.
- **Adapter** (`HttpVentasListAdapter.test.ts`, axios stub): **asserts de qué params snake_case
  se envían/omiten** — ataca directamente el bug (params dropeados) + `next_cursor`→`""`.
- **Hook** (`useBuscarVentas.test.tsx`, fake port vía Provider): primera página, `loadMore`
  (append por cursor), reset al cambiar filtros, error, abort.

**Entregable Task 3:** capa hex + refactor + tests vitest verdes (`npm test`). La caja de
búsqueda de ventas locales manda `search`+filtros+orden a la API. Commit(s) convencionales.

---

## Verificación (end-to-end, tras las 3 tasks + review)

1. Backend: `golangci-lint run ./...`, `go test -race -short ./...`, `make test-firebird-all`
   (con `FB_DATABASE`+`MEILISEARCH_URL`) — verde.
2. API viva: `go run ./cmd/api serve` con Meili local; `GET /v2/ventas?search=<cliente>` con
   token dev devuelve solo matches (nombre/teléfono/dirección/folio); filtros+orden operan;
   editar venta → aparece/actualiza en búsqueda (incremental por outbox).
3. Front: `npm test` verde; front contra API local, caja busca sobre todo el dataset.
4. Sin regresión: Meili apagado → listado responde vía Firebird keyset actual.
