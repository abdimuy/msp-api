# Plan: UI de gestión de Usuarios, Roles y Permisos (API Go) — 2 repos

## Context

Los permisos/roles que la API Go (Firebird) ya enforcea (`MSP_ROLES`, `MSP_PERMISOS`, `MSP_USUARIOS_ROLES`) no tienen ninguna pantalla — hoy solo se tocan por SQL directo. Este trabajo construye la UI de gestión completa de roles y permisos contra la API Go. NO toca el sistema Firestore (`/settings`) ni el gating del web.

## Global Constraints

- Backend: rama local `feat/permisos-usuarios-endpoints` desde `main`, sin push. Commits conventional, sin atribución a Claude.
- Frontend: rama local `feat/usuarios-roles-ui` desde `main`, sin push. Commits conventional.
- Backend: NO migración, NO nuevos códigos de permiso. Los 3 endpoints reusan `usuarios:ver` / `roles:listar`. Los métodos de repo YA existen (`UsuarioRepo.RolesFor`, `UsuarioRepo.PermisosFor`, `RolRepo.PermisosFor`).
- Frontend: patrón hexagonal igual que `src/modules/configuracion/`. NO react-query. Reusa `apiClient` axios + `errorMapper`. Tab dentro de `/configuracion`, visible solo si `useAuth().isSuperAdmin()`.
- Tests duros en ambos: Go (gates cobertura + integración Firebird local) y frontend (vitest + MSW, todas las capas), lint max-warnings 0, tsc verde.
- Código en inglés; mensajes de usuario en español. UTF-8. Fechas RFC3339 UTC.

## Los 3 endpoints backend (contrato)

1. `GET /v2/usuarios/{id}/roles` [`usuarios:ver`] → `ListResponse[RolResponse]` — usa `UsuarioRepo.RolesFor`.
2. `GET /v2/usuarios/{id}/permisos` [`usuarios:ver`] → `ListResponse[PermisoResponse]` — unión efectiva; reusar `usuarios.PermisosFor(userID)`.
3. `GET /v2/roles/{id}/permisos` [`roles:listar`] → `ListResponse[PermisoResponse]` — usa `RolRepo.PermisosFor`.

Patrón: request/response en `dto.go` → mapper en `dto_mapper.go` → método de query en `internal/auth/app/` → handler en `handlers_usuarios.go`/`handlers_roles.go` → registrar en `routes.go`. Cada endpoint: happy path, 404 id inexistente, 403 sin permiso. Integración contra Firebird local con `fbtestutil.WithTestTransaction`.

## Tasks

### Task 1 — Spec doc
Persistir el diseño aprobado en `docs/superpowers/specs/2026-08-05-ui-usuarios-roles-permisos.md` (contexto, sistema dual Firestore vs Firebird, decisión "no unificar aún", contrato de los 3 endpoints, estructura del módulo FE, norte de unificación futura). Commit en la rama de msp-api.

### Task 2 — Backend: 3 endpoints de lectura
DTOs/mappers/handlers/rutas para los 3 endpoints. Query methods en `internal/auth/app/`. Handler tests (happy/404/403) siguiendo `internal/auth/infra/authhttp/*_test.go`. Integración Firebird local (`fbtestutil.WithTestTransaction`): sembrar rol+permiso+usuario en tx, leer por los 3 endpoints, assert, rollback. Gate: `go clean -testcache` → `golangci-lint run ./...` + `go test -race ./internal/auth/...` + integración auth. Commit.

### Task 3 — curl round-trip e2e (verificación, no commit)
Backend local `make run` + super_admin dev. curl: crear rol → GET roles/{id}/permisos (vacío) → asignar 2-3 permisos → releer → asignar rol a usuario → GET usuarios/{id}/roles → GET usuarios/{id}/permisos (unión) → quitar → limpiar.

### Task 4 — Frontend: domain
`src/modules/usuariosRoles/domain/`: entidades `Usuario`, `Rol`, `Permiso` (+`categoria`); helpers puros: agrupar permisos por categoría, unión efectiva, predicado anti-lockout `noPuedeQuitarseSuPropioRolInmutable(currentUid, usuario, rol)`. `DomainError` reusado. Tests puros con `it.each`.

### Task 5 — Frontend: application + infrastructure
`application/ports/UsuariosRolesPort.ts` (14 métodos), `application/usecases/*` (delegadores finos). `infrastructure/http/HttpUsuariosRolesAdapter`, `dtos.ts` (snake_case), `mappers/` (dto↔domain + reuse errorMapper). Tests: adapter con axios stub (URLs, bodies snake_case, DELETE, error→DomainError); usecases con fake port.

### Task 6 — Frontend: presentation
`presentation/context` (DI Provider + `useUsuariosRolesPort`), `composition/UsuariosRolesContainer` (memoiza adapter), `hooks/` (`useUsuarios`, `useRoles`, `useCatalogoPermisos`, `useAsignarRol`, `useEditarPermisosRol`, `useCrudRol`). Tests: `renderHook`+Provider+fake port (loading/error/refresh; mutaciones con toasts mockeados).

### Task 7 — Frontend: components + tab wiring + MSW integración
`components/UsuariosRolesScreen` con Tabs internos "Usuarios"/"Roles"; `UsuarioPanel` (Sheet: datos + toggles de roles + permisos efectivos, anti-lockout); `RolPanel` (Sheet: checklist 40 permisos por categoría); CRUD roles (bloqueado para inmutable). Tab en `components/ConfiguracionShell.tsx` (gate `isSuperAdmin()`). `src/test/msw/handlers/usuariosRoles.ts` + screen con adapter real+MSW. Screen tests RTL + fake port + `vi.mock("sonner")`.

### Task 8 — Frontend gate
`npm run lint` (max-warnings 0) + `tsc` + `npm test` verdes. Commit.
