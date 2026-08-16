# Plan — Listado de zonas (rutas) read-only · API + UI

## Context
El usuario quiere, en **este** sistema (msp-api + sistema-cobro-web), únicamente un **listado de zonas (rutas) read-only con resumen**: una tabla con `nombre de zona · cobrador asignado · # de clientes · saldo total`. El dashboard completo de ruta (cobranza en vivo, mapa GPS, cierre de ruta, PTP, etc.) **NO se construye aquí** — el usuario lo hará en un producto separado que venderá aparte. Por lo tanto este plan es deliberadamente pequeño: un endpoint de lectura + una pantalla de tabla, reusando los patrones existentes. **No requiere migración** (lee tablas que ya existen).

Datos confirmados en exploración:
- Ruta = **zona** (`ZONAS_CLIENTES`). Cobrador asignado por zona en `MSP_CFG_ZONA_CAJA.COBRADOR_ID` (`-1` = sin asignar) → nombre en `COBRADORES`.
- `# clientes` desde `CLIENTES` (`ESTATUS IN ('A','B')`, `ZONA_CLIENTE_ID`).
- `saldo total` desde el cache ya materializado `MSP_SALDOS_VENTAS` (denormalizado por `ZONA_CLIENTE_ID`, `CARGO_CANCELADO <> 'S'`).
- No existe módulo `rutas` ni endpoint de zonas hoy; sería el primero.

## Task 1 — Backend (`internal/rutas/` — nuevo módulo, vertical slice mínimo)
Sigue el patrón Huma+chi (referencia: `internal/analytics/infra/analyticshttp/routes.go` `MountRouter`, y `internal/clientes`).

1. **`internal/rutas/app/listar_rutas.go`** — `Service.ListarRutas(ctx) ([]RutaResumen, error)`. `RutaResumen` read-model: `ZonaID int, ZonaNombre string, CobradorID *int, CobradorNombre string, NumClientes int, SaldoTotal decimal.Decimal`.
2. **`internal/rutas/ports/outbound/repos.go`** — `RutasRepo` interface con `ListarRutas(ctx) ([]RutaResumen, error)`.
3. **`internal/rutas/infra/rutasfb/repo.go` + `queries.go`** — repo Firebird. Para **evitar fan-out / doble conteo**, usar dos agregados por zona como subconsultas (espejo del saldo agrupado en `internal/clientes/infra/clientesfb/queries.go`):
   ```sql
   SELECT z.ZONA_CLIENTE_ID, z.NOMBRE,
          cfg.COBRADOR_ID, cob.NOMBRE AS COBRADOR_NOMBRE,
          COALESCE(nc.N, 0) AS NUM_CLIENTES,
          COALESCE(sv.SALDO, 0) AS SALDO_TOTAL
   FROM ZONAS_CLIENTES z
   LEFT JOIN MSP_CFG_ZONA_CAJA cfg ON cfg.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
   LEFT JOIN COBRADORES cob ON cob.COBRADOR_ID = cfg.COBRADOR_ID
   LEFT JOIN (SELECT ZONA_CLIENTE_ID, COUNT(*) N FROM CLIENTES
              WHERE ESTATUS IN ('A','B') GROUP BY ZONA_CLIENTE_ID) nc
          ON nc.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
   LEFT JOIN (SELECT ZONA_CLIENTE_ID, CAST(SUM(SALDO) AS NUMERIC(18,2)) SALDO
              FROM MSP_SALDOS_VENTAS WHERE CARGO_CANCELADO <> 'S'
              GROUP BY ZONA_CLIENTE_ID) sv
          ON sv.ZONA_CLIENTE_ID = z.ZONA_CLIENTE_ID
   ORDER BY z.NOMBRE
   ```
   - **`CAST(SUM(..) AS NUMERIC(18,2))`** obligatorio (gotcha firebirdsql: agregados NUMERIC sin escalar).
   - Cobrador `-1`/NULL → `CobradorNombre=""` (sin asignar), `CobradorID=nil`.
   - `COBRADORES.NOMBRE`/`ZONAS_CLIENTES.NOMBRE` ISO8859_1 (sin CAST WIN1252); NFC en Go si aplica.
4. **`internal/rutas/infra/rutashttp/routes.go` + `handlers.go` + `dto.go`** — `MountRouter(r chi.Router, svc *rutasapp.Service)` registra `GET /rutas` (OperationID `listar-rutas`), seguridad bearer, `DefaultStatus 200`. DTO `RutaResumenDTO` json snake_case (`zona_id`, `zona_nombre`, `cobrador_id`, `cobrador_nombre`, `num_clientes`, `saldo_total` como string decimal — convención de dinero del proyecto).
5. **`cmd/api/rutas_wiring.go`** — providers (repo, service) + wire; en `cmd/api/server.go` montar `r.Route("/rutas", ... rutashttp.MountRouter(r, rutasSvc) ...)` dentro del grupo `/v2` ya autenticado (junto a clientes/analytics, ~líneas 229-242). Path final: `GET /v2/rutas`.
6. **Permiso/auth**: la ruta vive bajo `/v2` que ya aplica auth (bearer Firebird). Gate con un permiso de lectura — reusar el patrón `auth.Perm*` (añadir `PermRutasRead` análogo a clientes/analytics, validado con `auth.CurrentUser`), o en v1 reusar un permiso de lectura existente.

**Sin migración.** Solo lee `ZONAS_CLIENTES`, `CLIENTES`, `COBRADORES`, `MSP_CFG_ZONA_CAJA`, `MSP_SALDOS_VENTAS`.

### Verificación Task 1 (Backend)
- `go build ./...`; test del repo (fake/skip-mode, patrón `narrativa_repo_test.go`) + test del service; `golangci-lint run ./...` (0 issues). Smoke contra la DB local (contenedor `mueblera-firebird`, `MUEBLERA.FDB`): correr la query en isql y validar que devuelve las 46 zonas con cobrador/# clientes/saldo coherentes. Curl autenticado `GET /v2/rutas`.

## Task 2 — Frontend (`src/modules/rutas/` — nuevo módulo, patrón hexagonal mínimo)
Espejo de `src/modules/clientes/` (y `winback`), una sola pantalla de tabla read-only.

1. **`domain/entities/Ruta.ts`** — `Ruta { zonaId, zonaNombre, cobradorNombre, numClientes, saldoTotal: string }`.
2. **`infrastructure/http/dtos.ts`** (`RutaResumenDTO`, snake_case) + **`mappers/dtoToRuta.ts`** (DTO→`Ruta`, lanza `DomainError`, patrón `dtoToCliente.ts`). Reusar **`infrastructure/http/apiClient.ts`** existente (axios + Bearer Firebase; **no crear otro**).
3. **`application/ports/RutasPort.ts`** (`listarRutas(signal?) : Promise<Ruta[]>`) + **`usecases/listarRutas.ts`** + **`application/__tests__/fakeRutasPort.ts`**.
4. **`infrastructure/http/HttpRutasAdapter.ts`** — implementa el port, `GET /rutas`, mapea con `dtoToRuta`, errores vía `apperrorToDomainError`.
5. **`presentation/context/RutasContext.tsx` + `composition/RutasContainer.tsx` + `hooks/useRutas.ts`** — patrón estándar (AbortController, `{ rutas, isLoading, error, refresh }`).
6. **`components/RutasScreen.tsx`** — tabla con `@/components/ui/table` y `Panel`; columnas Zona · Cobrador · # Clientes · Saldo total (usar `formatMoney` de `components/lib/format.ts`). Read-only.
7. **`Rutas.tsx`** (entry: `<RutasContainer><RutasScreen/></RutasContainer>`).
8. **Routing + acceso**: en `src/App.tsx` añadir `<Route path="/rutas" element={<ProtectedRoute requiredModule="RUTAS"><Rutas/></ProtectedRoute>} />`. En `src/constants/modules.ts` añadir `key:'RUTAS'`, mapeo `'/rutas':'RUTAS'`, y `'RUTAS'` a `PROTECTED_MODULES`. Añadir item de navegación junto a clientes/winback.

### Verificación Task 2 (Frontend)
- `npx tsc --noEmit`; `npx vitest run src/modules/rutas` (test `dtoToRuta` happy + inválido, y `useRutas` con `FakeRutasPort`). Verificación visual: `/rutas` lista las zonas con las 4 columnas.

## Reuse (no reinventar)
- BE: `MountRouter`/Huma de `analyticshttp/routes.go`; subconsulta de saldo agrupado de `clientesfb/queries.go`; CAST SUM NUMERIC; lectura zona+cobrador de `cobranza/infra/ventfb/ventas_repo.go`.
- FE: `apiClient.ts`, `Panel`, `format.ts`, shadcn `table`, patrón hook/port/mapper + tests (`FakePort`, `buildValidDTO`).

## Fuera de alcance (explícito)
Todo el dashboard de ruta (KPIs de cobranza en vivo, mapa GPS/verificación, recorrido plan/real, PTP, cartera por etapa, tendencia, perfil de clientes IA, cierre de ruta/liquidación, feed de pagos). → Va en el **producto separado** del usuario. Aquí solo el listado de zonas.

## Global Constraints (binding)
- CLAUDE.md §1: no logic in DB — esta tarea es read-only, no migración, no triggers.
- CLAUDE.md §2: vertical slices — cross-module access solo vía contracts; no reach into otro módulo `domain/app/infra`.
- CLAUDE.md §3: código en inglés; mensajes user-facing en español (lowercase, sin punto final). Códigos de error English snake_case.
- Dinero como string decimal en el DTO (convención del proyecto), `decimal.Decimal` en Go.
- `CAST(SUM(..) AS NUMERIC(18,2))` obligatorio en el agregado de saldo (gotcha firebirdsql).
- Cobrador `-1`/NULL → `CobradorID=nil`, `CobradorNombre=""`.
- Datetime/encoding: UTF-8 en columnas MSP_*; tablas legacy (ZONAS_CLIENTES, COBRADORES) ISO8859_1 — el adapter Microsip las aísla; NFC en Go.
