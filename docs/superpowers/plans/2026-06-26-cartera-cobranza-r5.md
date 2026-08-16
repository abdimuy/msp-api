# Plan — Rebanada 5: Cartera & Cobranza (dashboard de oficina)

> Rama: `feat/cartera-cobranza-r5` (ambos repos: msp-api + sistema-cobro-web).
> Ejecución subagent-driven. Mapeo: Task 1=B1, 2=B2, 3=B3, 4=B4, 5=B5, 6=B6,
> 7=F1, 8=F2, 9=F3, 10=F4. B7 (verificación de rama backend) y el review
> whole-branch son pasos del controlador, no tareas de implementador.
> Orden de ejecución (dependencias): 1→2→3→4→5→6 (backend completo) → 7→8→9→10 (FE).
> (B5 antes que B6 porque el endpoint `/roll-rate` de B6 llama a `Service.ObtenerRollRate` de B5.)

## Context
R5 es la primera rebanada del **pilar 2 (inteligencia de NEGOCIO)** del sistema "vender con IA". No es
una feature de venta directa: es el tablero de **salud de la cartera de crédito** para la oficina.
Aporta de forma indirecta a vender mejor porque (1) es el guardrail de las ventas (en crédito, vender de
más a quien no paga destruye valor — "LFL alto + PAR alto = ventas irresponsables"), (2) protege el
efectivo que financia todo el motor (cobranza ≈ 78% del cash), y (3) muestra la **rentabilidad real**
(margen − pérdida) que el margen bruto oculta.

Los cimientos ya existen: cachés `MSP_SALDOS_VENTAS`/`MSP_PAGOS_VENTAS` (mantenidos por trigger), el
read-model `MSP_AN_WINBACK_CANDIDATOS` con señales por cliente, las funciones de scoring/cobranza en
`analytics/app/scoring.go`, el patrón de endpoints agregados (winback/attribution) y el refresh worker.
R5 **agrega la vista agregada** de oficina sobre esos cimientos, demo-first.

Resultado buscado: una sección nueva "Cartera & Cobranza" en la app de oficina con vista ejecutiva
integrada (salud + rentabilidad + dónde actuar): PAR, tasa de cobranza (CEI), aging, cosechas (vintage),
roll-rate, ranking por cobrador, lista accionable de cuentas en riesgo, alertas de deterioro, margen
real y cumplimiento de pago esperado — cortable por zona/cobrador/periodo.

## Decisiones (brainstorming 2026-06-26 + investigación SOTA)
- **Alcance:** dashboard de **oficina (web)** en `sistema-cobro-web` + endpoints en `msp-api`. La
  **captura de PTP** (promesas en ruta) queda **fuera** (rebanada futura / app Android).
- **Vista ejecutiva integrada** (las tres a la vez): salud arriba, rentabilidad real, drill-down a dónde
  actuar.
- **Cumplimiento de pago esperado** = **derivado** del `próximo pago estimado` que ya computamos
  (`computeProxPago` + `MONTO_PROX_PAGO` + `PCT_PAGOS_A_TIEMPO`), sin captura de promesas. El PTP
  negociado real entra cuando haya captura.
- **Vista por cobrador:** desempeño (su CEI, cumplimiento, cobertura, PAR de su cartera) **sin ranking
  competitivo/gamificación por defecto** (evita presión que degrade promesas; el research lo señaló). La
  detección de fraude/float (Z-score) es **R6**.
- **Backend en el módulo `analytics`** (no cobranza): ya tiene el patrón de agregados, el refresh worker
  (para materializar roll-rate) y las funciones de scoring; y ya lee `MSP_SALDOS_VENTAS`/`MSP_PAGOS_VENTAS`.
- **Roll-rate** requiere historia de buckets → nueva tabla `MSP_AN_CARTERA_SNAPSHOT` materializada en el
  refresh (tick full @ 3 AM UTC). Roll-rate = comparar snapshots consecutivos; **será significativo tras
  acumular ≥2 periodos** (en el demo arranca poblando el primer snapshot).
- **Margen real (v1) = proxy agregado**: `margen_verificado (52.8%, project_verified_unit_economics) ×
  ventas − pérdida_esperada` (pérdida de PAR×LGD / score de crédito). El **COGS por producto preciso se
  difiere** (solapa R4 — Ventas & Productos, que sí trae el análisis de costo).
- **Matriz riesgo×disposición**: incluida como enriquecimiento de la lista accionable (reusa
  `computeCreditoScore` + segmento RFM), no como motor aparte.

## Global Constraints (no negociables — CLAUDE.md)
- Hexagonal por slices (§2): backend nuevo en `analytics`; FE módulo nuevo `src/modules/cartera/`. Las
  cachés `MSP_SALDOS_VENTAS`/`MSP_PAGOS_VENTAS` son read-model trigger-driven (exención ADR-0006) — leerlas
  está OK (analytics ya lo hace); la tabla nueva `MSP_AN_CARTERA_SNAPSHOT` sigue §1 (valores desde Go, sin
  defaults/triggers). Código EN / mensajes ES (§3). Fechas UTC; `CAST(... AS NUMERIC)` por el bug del
  driver; upserts con `EXECUTE BLOCK` (no MERGE con params).
- **§1 No lógica en BD:** migraciones solo estructurales. UUID via `uuid.New()` en Go, timestamps via
  `time.Now()` + `firebird.ToWallClock(t)` al escribir, `firebird.ScanUTCTime` al leer. Cada INSERT pasa
  ID/CREATED_AT/UPDATED_AT explícitos. Sin DEFAULT, sin CREATE TRIGGER, sin CREATE PROCEDURE, sin CHECK de
  reglas de negocio en tablas `MSP_*`. Trailer `MSP_MIGRATIONS` en cada migración.
- **§3 idioma:** identificadores/códigos de error/comentarios en inglés; mensajes a usuario en español
  (minúscula, sin punto final). Ej. `apperror.NewValidation("code_en_ingles", "mensaje en español")`.
- **Dinero:** en el contrato HTTP el dinero va como **string** (decimal), nunca float. Fechas RFC3339 UTC.
- **firebirdsql driver (v0.9.19):** agregados NUMERIC se devuelven sin escalar → castear
  `CAST(SUM(...) AS NUMERIC(18,s))`. MERGE con `?` falla (-804) → usar UPDATE-luego-INSERT o `EXECUTE BLOCK`.
- **Tests súper robustos** (mandato): domain ≥99% (+ property `pgregory.net/rapid` + fuzz), app ≥90%
  (fakes a mano), infra-fb ≥80% (`fbtestutil.WithTestTransaction` rollback-only, gate `FB_DATABASE`),
  http ≥70% (handler + e2e), **mutation ≥80%** (`gremlins` sobre domain/app nuevos). FE: vitest por capa
  (usecase + hook con FakePort + componente con testing-library), `tsc --noEmit` limpio, eslint 0 warnings.
- **No persistent test data en BD compartida:** tests de integración solo rollback-only
  (`fbtestutil.WithTestTransaction`). Nunca INSERTs crudos persistentes.

## Arquitectura — dónde vive cada cosa (verificado en exploración)

### Mapa de datos por métrica (todo confirmado)
- **PAR**: `MSP_SALDOS_VENTAS` — num = `SUM(SALDO) WHERE CARGO_CANCELADO='N' AND today−FECHA_ULT_PAGO>umbral`,
  den = `SUM(PRECIO_TOTAL)`. `GROUP BY ZONA_CLIENTE_ID` (índice `IDX_MSP_SALDOS_ZONA_SALDO`). Umbral 30d
  de `scoring.go` (no hay vencimiento contractual → proxy `today−FECHA_ULT_PAGO`; NULL = nunca pagó = moroso).
- **Aging buckets**: `MSP_SALDOS_VENTAS` por `today−FECHA_ULT_PAGO` (0-30/31-60/61-90/>90), índice
  `IDX_MSP_SALDOS_ZONA_FUP`. Reusa el bucketing de `estadoPagoFor()`.
- **CEI / tasa de cobranza**: `MSP_PAGOS_VENTAS WHERE CONCEPTO_CC_ID IN (87327,155,11) AND CANCELADO='N'
  AND APLICADO='S' AND FECHA BETWEEN…` (índice `IDX_MSP_PAGOS_ZONA_FECHA`). Constantes de concepto ya en
  `analyticsfb/queries.go`.
- **Vintage/cosechas**: `MSP_SALDOS_VENTAS.FECHA_CARGO` (índice `IDX_MSP_SALDOS_FECHA_CARGO`); mes de vida
  por aritmética de EXTRACT (patrón ya usado en `leerAnclasRFMClose`).
- **Ventas×cartera**: ya cruzado en `MSP_AN_WINBACK_CANDIDATOS` (MONETARY ventas + SALDO cartera + ZONA).
- **Cumplimiento / cuentas en riesgo**: `MSP_AN_WINBACK_CANDIDATOS` + funciones `estadoPagoFor`,
  `computeCobranzaTier`, `ajustarCobranzaRecencia`, `computeCreditoScore`, `computeProxPago`,
  `computeSegmentoScore` (todas en `app/scoring.go`; reusar directo).
- **Cobrador**: JOIN `MSP_SALDOS_VENTAS`/`CLIENTES.COBRADOR_ID` → `COBRADORES.NOMBRE` (zona es directa por
  `ZONA_CLIENTE_ID`).
- **Roll-rate**: HUECO → nueva `MSP_AN_CARTERA_SNAPSHOT` (distribución de buckets por zona/cobrador por
  fecha de corte), materializada en el refresh; roll-rate = comparar cortes consecutivos.
- **Margen real**: HUECO de COGS por producto → v1 proxy agregado (margen verificado × ventas − pérdida
  esperada). COGS preciso diferido a R4.

### Backend (msp-api) — módulo `analytics`
Patrón espejo de winback/attribution: `app/cartera_query.go` (Service: pull cachés/read-model → agrega en
Go) + `analyticsfb` queries agregadas (CAST AS NUMERIC, GROUP BY zona) + `analyticshttp/cartera.go`
(huma endpoints, dinero como string) + permiso nuevo `analytics:cartera_ver`. Roll-rate vía nueva tabla +
hook en `refresh_worker.go` (full tick). Reusa `RunInSnapshotTx`, rowmappers, conceptos.

**Archivos espejo existentes (leer antes de escribir):**
- `internal/analytics/app/winback_query.go` + `atribucion_query.go` — patrón Service (pull repo → agrega en Go).
- `internal/analytics/app/scoring.go` — `estadoPagoFor`, `computeCobranzaTier`, `ajustarCobranzaRecencia`,
  `computeCreditoScore`, `computeProxPago`, `computeSegmentoScore` (reusar directo).
- `internal/analytics/infra/analyticsfb/queries.go` — constantes de concepto + queries; `repo.go`,
  `rowmappers.go`, `RunInSnapshotTx`.
- `internal/analytics/infra/analyticshttp/winback.go` + `routes.go` + `dtos.go` + `dto_mapper.go` + `auth.go`.
- `internal/analytics/ports/outbound/repos.go` — puertos de repositorio.
- `internal/analytics/analytics_contracts.go` + `analytics_contracts_mapper.go`.
- `internal/analytics/app/refresh_worker.go` — full tick (junto a `LogDistribucionBandasCredito`).
- `internal/auth/domain/permission_codes.go` — catálogo de permisos (boot-sync concede a super_admin).
- Migración nueva: `000042` (la última es `000041`).

### Frontend (sistema-cobro-web) — módulo nuevo `src/modules/cartera/`
Espejo de `src/modules/rutas/` (estructura) + `src/modules/winback/` (filtros + panel agregado):
`domain/entities` → `infrastructure/http/{apiClient,dtos,HttpCarteraAdapter}` → `infrastructure/mappers`
→ `application/ports/CarteraPort.ts` → `application/usecases/*` → `presentation/{context,hooks,composition}`
→ `components/CarteraScreen.tsx` + widgets. Registro en navegación (4 archivos: `constants/modules.ts`,
`constants/desktopModules.ts`, `App.tsx` con `<ProtectedRoute requiredModule="CARTERA">`, opcional grupo
"Negocio" en `AppSidebar.tsx`). Reusos: `KpiCell`/`Panel` (clientes/ficha), `semaphoreConfig`
(rutas/lib/desgloseUx), badges (winback), `recharts@3.8.1` (BarChart aging/roll, AreaChart cosechas/tendencia),
tablas sortable (rutas/lib/tableOps), filtros (WinbackFilters), hooks con AbortController+tick.

---

## Task 1 — B1: Migración `MSP_AN_CARTERA_SNAPSHOT` + domain entity (snapshot para roll-rate/tendencia)
- `migrations-firebird/000042_create_msp_an_cartera_snapshot.{up,down}.sql`: tabla con corte por
  (FECHA_CORTE, ZONA_CLIENTE_ID, COBRADOR_ID, BUCKET) → saldo/conteo; PK CHAR(36) ASCII, sin defaults/
  triggers (§1), trailer MSP_MIGRATIONS. Domain entity `CarteraSnapshot` + constructor (uuid+time en Go).
- Tests: domain del entity (constructor, validación) ≥99%.

**Notas de diseño (controlador):**
- Columnas sugeridas: `ID CHAR(36)` PK, `FECHA_CORTE TIMESTAMP NOT NULL`, `ZONA_CLIENTE_ID INTEGER NOT NULL`,
  `COBRADOR_ID INTEGER` (nullable si no hay cobrador), `BUCKET VARCHAR(16) NOT NULL` (los nombres de bucket
  de aging: `0-30`/`31-60`/`61-90`/`90+`), `SALDO NUMERIC(18,2) NOT NULL`, `CONTEO INTEGER NOT NULL`,
  `CREATED_AT TIMESTAMP NOT NULL`, `UPDATED_AT TIMESTAMP NOT NULL`. UNIQUE(`FECHA_CORTE`,`ZONA_CLIENTE_ID`,
  `COBRADOR_ID`,`BUCKET`). Índice por `FECHA_CORTE` para traer cortes recientes. Mira migración 000040 como
  plantilla de estilo + trailer `INSERT INTO MSP_MIGRATIONS`. El nombre exacto de bucket debe coincidir con
  el VO de aging de la Task 2 — define el set canónico de nombres de bucket aquí y reúsalo.
- Entity en `internal/analytics/domain/cartera_snapshot.go`: campos privados, constructor `NewCarteraSnapshot(...)`
  que llama `uuid.New()` y recibe `now time.Time` (NO llama `time.Now()` adentro — el caller lo pasa), valida
  bucket no vacío, saldo≥0, conteo≥0, zona>0. Sigue el patrón de otras entities del módulo (`winback_candidato.go`).

## Task 2 — B2: Domain puro: matemática de cartera (aging/PAR/vintage/roll-rate/cumplimiento)
- `internal/analytics/domain/cartera.go` (NEW): VOs + funciones puras — bucket de aging por días,
  ratio PAR, cohorte vintage (año*12+mes, edad en meses), roll-rate entre dos distribuciones de buckets,
  cumplimiento esperado (today vs proxPago + saldo). Sin `time.Now()` (recibe `now`).
- Tests (≥99% + property `rapid` + fuzz): buckets correctos en fronteras; PAR∈[0,1]; roll-rate monotónico;
  cohorte/edad correctas; sin panics.

**Notas de diseño (controlador):**
- `AgingBucket` VO + `BucketForDays(days int) AgingBucket`: fronteras 0-30 / 31-60 / 61-90 / 90+. days<0
  trata como 0 (o documenta). Los nombres string deben ser IDÉNTICOS a los de la migración Task 1.
- `PARRatio(saldoMoroso, saldoTotal decimal.Decimal) decimal.Decimal` (o float en [0,1]): den=0 → 0. Clamp [0,1].
- `VintageCohort(fechaCargo time.Time) int` = año*12+mes (mes ordinal). `CohortAgeMonths(fechaCargo, now) int`.
- `RollRate(prev, curr map[AgingBucket]decimal/int)` → migración entre buckets consecutivos. Define el
  contrato exacto (qué representa: % del saldo que pasó de bucket i a i+1, o transición neta). Documenta y
  haz la property: roll-rate "monotónico" = si todo el saldo empeora un bucket, el roll-forward sube.
- `CumplimientoEsperado(now, proxPago time.Time, saldo decimal)` → estado de cumplimiento derivado
  (al-corriente / vencido-leve / vencido) usando solo proxPago vs now + saldo>0. Reusa la lógica de
  `estadoPagoFor` si aplica (sin duplicarla — si ya existe en scoring.go, considera moverla/reusarla, pero
  NO rompas scoring.go; consulta si hay ambigüedad). Todas las funciones reciben `now` como parámetro.

## Task 3 — B3: analyticsfb — queries agregadas + read/write snapshot
- `internal/analytics/infra/analyticsfb/cartera_queries.go` (NEW): agregaciones sobre `MSP_SALDOS_VENTAS`
  (PAR, aging, vintage, saldo por zona/cobrador — JOIN CLIENTES.COBRADOR_ID) y `MSP_PAGOS_VENTAS`
  (CEI por periodo/zona/cobrador). `CAST(... AS NUMERIC)`; reusa índices existentes; rowmappers.
- Read/write de `MSP_AN_CARTERA_SNAPSHOT` (upsert `EXECUTE BLOCK`). Métodos en el puerto `WinbackRepo`
  o un puerto nuevo `CarteraRepo`.
- Tests integración (`fbtestutil.WithTestTransaction`, rollback-only, gate `FB_DATABASE`, ≥80%):
  insertar saldos/pagos de prueba y verificar PAR/aging/CEI + round-trip snapshot.

**Notas de diseño (controlador):**
- Puerto nuevo `CarteraRepo` en `ports/outbound/repos.go` (no contamines `WinbackRepo`). Métodos:
  agregaciones de saldos por zona/cobrador con conteo+saldo por bucket de aging; CEI por periodo;
  vintage por mes-de-corte; `SaveCarteraSnapshot([]CarteraSnapshot)` y `ListRecentSnapshots(limit)`.
- Adapter en `analyticsfb/cartera_queries.go`. Usa `RunInSnapshotTx`/conexión como el repo existente.
  Castea TODO agregado NUMERIC con `CAST(... AS NUMERIC(18,2))`. Lee fechas con `firebird.ScanUTCTime`,
  escribe con `firebird.ToWallClock`. Upsert snapshot vía `EXECUTE BLOCK` (no MERGE con params).
- Reusa constantes de concepto de `queries.go` (87327,155,11). Reusa `rowmappers.go`.
- Tests: patrón `repo_test.go`/`cobranza_signals_test.go` (WithTestTransaction). Inserta filas de saldo/pago
  de prueba DENTRO de la txn rollback, calcula PAR/aging/CEI esperados a mano, compara. Round-trip de snapshot.

## Task 4 — B4: Service `cartera_query.go` (todas las métricas on-the-fly) + contrato
- `internal/analytics/app/cartera_query.go` (NEW): `ObtenerSaludCartera`, `ObtenerAging`,
  `ObtenerCosechas`, `ObtenerRankingCobradores`, `ListarCuentasRiesgo` (reusa score + segmento → matriz
  riesgo×disposición), `ObtenerCumplimiento`, `MargenReal` (proxy). Patrón Atribucion (pull + agrega en Go),
  reusa funciones de `scoring.go`. Cortes zona/cobrador/periodo. Contratos en `analytics_contracts.go`.
- Tests app (fake repo, ≥90%): distribuciones conocidas → métricas correctas; cortes; degradación.

**Notas de diseño (controlador):**
- Service nuevo (o métodos en el Service existente `app/service.go`) que toma el `CarteraRepo` (Task 3) y
  las funciones puras (Task 2) + `scoring.go`. Patrón espejo de `atribucion_query.go`/`winback_query.go`:
  pull del repo → agrega/clasifica en Go. NO metas SQL aquí.
- `MargenReal` proxy = `0.528 × ventas − pérdida_esperada`. Define la constante de margen verificado
  (52.8%, ver memoria project_verified_unit_economics) como const documentada. Pérdida esperada = PAR×LGD
  o derivada del score de crédito — documenta la fórmula v1.
- `ListarCuentasRiesgo` enriquece cada cuenta con tier (`computeCobranzaTier`/`computeCreditoScore`) +
  segmento RFM (`computeSegmentoScore`) → matriz riesgo×disposición. Reusa lo de scoring.go, no reimplementes.
- Contratos en `analytics_contracts.go` (+ mapper si aplica): structs de salida (dinero como `decimal.Decimal`
  en el contrato Go; la capa http lo convierte a string). Cortes por zona/cobrador/periodo como parámetros.
- Tests con fake `CarteraRepo` a mano (mira los fakes de `winback_query_test.go`/`atribucion_query_test.go`).

## Task 5 — B5: Materialización snapshot en refresh worker + roll-rate
- En `refresh_worker.go` (hook del full tick, junto a `LogDistribucionBandasCredito`): computar la
  distribución de buckets actual y persistir un corte en `MSP_AN_CARTERA_SNAPSHOT` + `SaveRefreshState`
  job `cartera_snapshot`. `Service.ObtenerRollRate` compara los 2 cortes más recientes.
- Tests app (≥90%): roll-rate desde 2 snapshots fixture; primer snapshot → roll-rate vacío/no-disponible.

**Notas de diseño (controlador):**
- Hook en el full tick de `refresh_worker.go` (mira cómo se llama `LogDistribucionBandasCredito` y
  `SaveRefreshState`). Computa la distribución actual de buckets por zona/cobrador (via `CarteraRepo`
  agregaciones de Task 3) y persiste un corte con `FECHA_CORTE = now` (Go time) vía `SaveCarteraSnapshot`.
  Registra `SaveRefreshState` con job `cartera_snapshot`.
- `Service.ObtenerRollRate(corte params)` lee los 2 cortes más recientes (`ListRecentSnapshots`), arma las
  distribuciones por bucket y llama `domain.RollRate` (Task 2). Si hay <2 cortes → resultado "no disponible"
  / "acumulando datos" (flag en el contrato, no error).
- Tests app: fixtures de 2 snapshots → roll-rate correcto; 1 snapshot → no-disponible.

## Task 6 — B6: analyticshttp/cartera.go — endpoints + permiso + DTOs
- `internal/analytics/infra/analyticshttp/cartera.go` (NEW) + DTOs (dinero string, fechas RFC3339):
  `GET /v2/analytics/cartera/salud`, `/aging`, `/cosechas`, `/cobradores`, `/cuentas-riesgo`,
  `/roll-rate` con `?zona=&cobrador=&periodo=`. Permiso nuevo `PermAnalyticsCarteraRead = "analytics:cartera_ver"`
  en `auth/domain/permission_codes.go` (boot-sync lo concede a super_admin). Handlers espejo de winback.
- Tests handler (auth + serialización, ≥70%) + e2e composición.

**Notas de diseño (controlador):**
- Endpoints huma espejo de `analyticshttp/winback.go`. Registra rutas en `routes.go`. DTOs en `dtos.go`
  (o un `cartera.go` propio) con dinero como **string**, fechas RFC3339. Mapper de contrato→DTO en
  `dto_mapper.go` (o local). `requirePerm(PermAnalyticsCarteraRead)` en cada handler (mira `auth.go`/winback).
- Permiso nuevo en `permission_codes.go`: `PermAnalyticsCarteraRead = "analytics:cartera_ver"` agregado al
  catálogo (el boot-sync `invokeAuthCatalogSync` lo concede a super_admin en el arranque — no requiere
  auth-bootstrap). Mira cómo están declarados los demás `Perm*` y el slice del catálogo.
- Tests: patrón `handlers_test.go`/`narrativa_test.go` (auth + serialización). Verifica 403 sin permiso,
  200 con permiso + DTO correcto (dinero string). e2e de composición como el de winback.

## Task 7 — F1: Andamiaje módulo `cartera/` + navegación + datos base (sistema-cobro-web)
- Crear `src/modules/cartera/` (espejo `rutas/` + `winback/`): `apiClient` (con `authStateReady`),
  `CarteraPort`, `HttpCarteraAdapter`, `CarteraContext`/`Container`, `CarteraScreen` shell con filtros
  (zona/cobrador/periodo, espejo `WinbackFilters`) + hook con AbortController+tick. Registro en
  navegación (4 archivos) + permiso `CARTERA` (`modules.ts`, `desktopModules.ts`, `App.tsx`,
  `AppSidebar.tsx` grupo "Negocio"). Sin datos reales aún (pantalla vacía con filtros).
- Tests: usecase + hook (FakePort) + render del shell.

**Notas de diseño (controlador):**
- Trabaja en el repo FE: `/Volumes/M2-1TB/Developer/sistema-cobro-web`. Rama `feat/cartera-cobranza-r5`.
- Estructura espejo de `src/modules/rutas/` (capas) + `src/modules/winback/` (filtros + panel agregado).
  Capas: `domain/entities` → `infrastructure/http/{apiClient,dtos,HttpCarteraAdapter}` →
  `infrastructure/mappers` → `application/ports/CarteraPort.ts` → `application/usecases/*` →
  `presentation/{context,hooks,composition}` → `components/CarteraScreen.tsx`.
- Navegación: 4 archivos — `src/constants/modules.ts`, `src/constants/desktopModules.ts`, `src/App.tsx`
  (`<ProtectedRoute requiredModule="CARTERA">`), `src/components/AppSidebar.tsx` (grupo "Negocio"). Permiso
  de módulo `CARTERA` (mira cómo está registrado `WINBACK`/`RUTAS`). El backend expone `analytics:cartera_ver`.
- Esta tarea NO pinta datos: shell con filtros + hook que llama al adapter (AbortController + tick como
  winback). Tests: usecase + hook (FakePort) + render del shell (testing-library). `tsc --noEmit` limpio,
  eslint 0 warnings, vitest verde.

## Task 8 — F2: Salud + KPIs + aging + tendencia + alertas (tab/sección Resumen)
- KPI hero (PAR, CEI/tasa cobranza, saldo total, cuentas en mora, margen real) con flechas/semáforo;
  aging (BarChart apilado); tendencia PAR (LineChart); banda de alertas de deterioro. Recipe completo
  (entity→dto→mapper→port→usecase→fake→adapter→hook→component) + tests por capa.

**Notas de diseño (controlador):**
- Consume los endpoints `/salud`, `/aging` (y `/roll-rate` solo para tendencia si aplica). Recipe vertical
  completo por dato: entity→dto→mapper→port→usecase→fake→adapter→hook→component. Reusa `KpiCell`/`Panel`
  (de clientes/ficha), `semaphoreConfig` (rutas/lib/desgloseUx), `recharts@3.8.1` (BarChart apilado aging,
  LineChart tendencia). Dinero llega como string → parsea/formatea con el helper existente (`formatMoney`).
- Banda de alertas de deterioro: deriva de las métricas (PAR alto, CEI bajo). UI text minimalista (2-4
  palabras), tono profesional español, sin oraciones de reassurance.
- Tests por capa (usecase, mapper, hook con FakePort, componente). `tsc --noEmit`, eslint 0, vitest verde.

## Task 9 — F3: Ranking por cobrador + cuentas en riesgo (dónde actuar)
- Tabla sortable por cobrador (CEI, cumplimiento, cobertura, PAR de su cartera) + lista accionable de
  cuentas en riesgo (badges de tier/estado, matriz riesgo×disposición, link a ficha 360). Recipe + tests.

**Notas de diseño (controlador):**
- Consume `/cobradores` y `/cuentas-riesgo`. Tabla sortable (reusa `rutas/lib/tableOps`). SIN ranking
  competitivo/gamificación — es vista de desempeño, no leaderboard (decisión de producto). Badges de
  tier/estado espejo de winback. Link a ficha 360 del cliente (reusa la ruta existente de clientes).
- Recipe vertical por dato + tests por capa. Mismos gates (tsc/eslint/vitest).

## Task 10 — F4: Cosechas (vintage) + roll-rate
- Heatmap/triángulo de cosechas (recharts) + visualización de roll-rate entre buckets. Estado
  "acumulando datos" para roll-rate hasta tener ≥2 cortes. Recipe + tests.

**Notas de diseño (controlador):**
- Consume `/cosechas` y `/roll-rate`. Cosechas: heatmap/triángulo (recharts — AreaChart o grid de celdas).
  Roll-rate: visualización de migración entre buckets; cuando el backend responde "no disponible/acumulando
  datos" (<2 cortes), muestra estado vacío "Acumulando datos" (UI text minimalista). Recipe + tests por capa.

---

## Secuencia (demo-first, referencia)
1. **B1+B2** (migración+domain) → **B3** (queries) → **B4** (service on-the-fly) → **B5** (snapshot) →
   **B6** (endpoints) — habilita salud/aging/CEI/cosechas/cobradores/cuentas/roll-rate en vivo.
2. **F1** (andamiaje) → **F2** (salud) → **F3** (cobradores+cuentas) → **F4** (cosechas + roll-rate).
3. **B7** verificación de rama backend (controlador) → review whole-branch (opus) → verificación live → merge.

## Verificación (end-to-end)
- Go: `go build ./...` + Windows CGO-free (`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`);
  `golangci-lint run ./internal/analytics/... ./cmd/...` 0 issues; `go test ./internal/analytics/...`;
  `make test-firebird-*` (integración rollback de B3); `gremlins` ≥80%; migración 000042 aplica.
- FE: `npx tsc --noEmit` limpio; `npx vitest run src/modules/cartera` verde; `eslint` 0 warnings.
- Live (la BD dev ya está poblada; `noe@gmail.com` = super_admin): correr la API local (dev mode) y
  verificar cada endpoint `/v2/analytics/cartera/*` 200 con datos reales. Cargar la pantalla `/cartera`.

## Fuera de alcance
- Captura de PTP (promesas en ruta) y app Android — rebanada futura.
- Fraude/float del cobrador (Z-score) — R6 (Control & Fugas).
- COGS por producto preciso para margen real — R4; R5 usa proxy agregado.
- Proyección de recuperación por cuenta (prob/tiempo) — opcional futuro.
- Visitas.

## Notas / riesgos conocidos
- **Roll-rate** es significativo solo tras ≥2 cortes del snapshot; en el demo muestra "acumulando datos".
- **Aging** usa `today−FECHA_ULT_PAGO` (no hay vencimiento contractual); refinamiento cadencia-aware es opcional.
- **Margen real** es proxy agregado; el número fino llega con R4.
- `Pagos90D` del read-model se congela al refresh; aceptable para el dashboard de oficina.
</content>
</invoke>
