# Spec: UI de gestión de Usuarios, Roles y Permisos (contra la API Go)

> **Fecha:** 2026-08-05
> **Estado:** diseño aprobado, sin código. Este documento existe para no perder la decisión — es el primer entregable de la iniciativa.
> **Repos:** `msp-api` (rama `feat/permisos-usuarios-endpoints`) + `sistema-cobro-web` (rama `feat/usuarios-roles-ui`).
> **Alcance de esta iteración:** 3 endpoints de solo lectura en la API Go + una UI de administración completa (lectura y escritura contra los endpoints ya existentes) montada en `/configuracion`.

---

## 1. Contexto y problema

La API Go ya enforcea autorización granular contra Firebird: `MSP_ROLES`, `MSP_PERMISOS` y `MSP_USUARIOS_ROLES` sostienen un catálogo de 40 códigos de permiso agrupados en 11 categorías (`internal/auth/domain/permission_codes.go`), y el middleware `RequirePermission` (`internal/auth/infra/authhttp/authz.go`) los exige en cada ruta protegida (`internal/auth/infra/authhttp/routes.go`).

El problema es que este sistema **no tiene ninguna pantalla de administración**. Hoy, dar de alta un rol, asignar un permiso a un rol, o asignar un rol a un usuario nuevo se hace por SQL directo contra Firebird. Esto tiene dos consecuencias:

- Cada persona que se integra al equipo (nuevo vendedor con acceso web, nuevo supervisor, etc.) requiere un INSERT manual en `MSP_USUARIOS_ROLES`, o se queda bloqueada con 403 hasta que alguien lo note y lo arregle a mano.
- En Firebird, hoy **solo existe sembrado el rol `super_admin`** (`internal/auth/app/roles_catalog_sync.go`, `SyncRolesCatalog`, que hace `UpsertInmutableByName` contra el nombre fijo `"super_admin"`). No hay ningún otro rol operativo creado — todo el resto del catálogo de permisos existe en código pero no tiene ningún rol (aparte de `super_admin`, que los tiene todos) que los agrupe de forma utilizable para un supervisor u operador reales.

Esta iniciativa construye la UI que cierra ese hueco: gestión de usuarios, roles y permisos, directamente contra la API Go.

## 2. Sistema dual: Firestore vs. Firebird

Conviven hoy **dos sistemas de roles independientes** en el ecosistema msp, y es importante no confundirlos:

**(a) Firestore — gatea el front web hoy.** El campo `ROL` del documento de usuario en Firestore (constantes en `sistema-cobro-web/src/constants/roles.ts`: `ADMIN`, `SUPERVISOR`, `OPERADOR`, `VIEWER`, `SUPER_ADMIN`) es lo que consume `AuthContext` (`sistema-cobro-web/src/context/AuthContext.tsx`) para decidir qué módulos del desktop puede ver un usuario (`getAvailableModules`, `isAdmin`, `isSuperAdmin`) y qué pantallas de `/settings` puede tocar. Es un modelo de 5 roles fijos, sin permisos granulares — todo-o-nada por rol.

**(b) Firebird (`MSP_*`) — lo que enforcea la API Go.** Aquí el modelo es de permisos granulares (40 códigos, 11 categorías: `usuarios`, `roles`, `permisos`, `ventas`, `failed_intents`, `cobranza`, `inventario`, `analytics`, `clientes`, `configuracion`, `reactivacion`) agrupados en roles arbitrarios que se arman en Firebird. Cada endpoint de la API Go exige un permiso específico vía `RequirePermission`, no un rol fijo. Un usuario puede tener cero, uno o varios roles (`MSP_USUARIOS_ROLES`); sus permisos efectivos son la unión de los permisos de todos sus roles.

Hoy ambos sistemas se usan **de forma inconsistente**: el gating del front web depende de Firestore, mientras que cada request a la API Go depende de Firebird. Un usuario puede tener `ROL: ADMIN` en Firestore (así que ve todos los módulos del desktop) y no tener ningún rol en `MSP_USUARIOS_ROLES` (así que cada llamada a la API le regresa 403). Ese desacople es justamente el síntoma que hoy se resuelve a mano con SQL directo.

## 3. Decisión: "no unificar aún"

Este trabajo construye la UI de gestión **solo contra la API Go (Firebird)**. Explícitamente:

- **NO** toca `/settings` (Firestore) ni el `ROL` de Firestore.
- **NO** cambia el gating actual del web (`AuthContext.isAdmin()`, `isSuperAdmin()`, `getAvailableModules()` siguen leyendo Firestore exactamente igual que hoy).
- **SÍ** deja el sistema de permisos de Firebird — que ya es la fuente de verdad para lo que la API realmente autoriza — en un estado administrable: se puede ver y editar sin SQL directo.

La unificación de ambos sistemas (mover el gating del front a depender de los permisos de la API Go en vez del `ROL` de Firestore) es un proyecto grande y aparte, deliberadamente fuera de alcance aquí. Intentar unificar y construir la UI de administración en el mismo esfuerzo mezclaría una migración de arquitectura de autorización con una herramienta de administración; se separan a propósito.

Este trabajo es, sin embargo, el cimiento correcto para esa unificación futura: sin una UI para administrar roles/permisos en Firebird, migrar el gating hacia la API Go sería impracticable (cada cambio de acceso seguiría requiriendo SQL directo). Con la UI en su lugar, la migración progresiva del gating se vuelve una decisión de producto, no un bloqueo operativo.

## 4. Contrato de los 3 endpoints nuevos (solo lectura)

Los 3 endpoints son de solo lectura, no requieren migración de esquema ni códigos de permiso nuevos, y reusan métodos de repositorio que ya existen en `internal/auth/ports/outbound/repos.go`. Confirmado contra el código actual:

### `GET /v2/usuarios/{id}/roles`
- **Permiso requerido:** `usuarios:ver` (`domain.PermUsuariosVer`).
- **Respuesta:** `ListResponse[RolResponse]` — los roles asignados a ese usuario.
- **Fuente de datos:** `UsuarioRepo.RolesFor(ctx, usuarioID) ([]*domain.Rol, error)`.

### `GET /v2/usuarios/{id}/permisos`
- **Permiso requerido:** `usuarios:ver` (`domain.PermUsuariosVer`).
- **Respuesta:** `ListResponse[PermisoResponse]` — la unión efectiva de permisos de todos los roles del usuario (no permisos por rol individual).
- **Fuente de datos:** reusa `UsuarioRepo.PermisosFor(ctx, usuarioID) ([]domain.Permission, error)` — el mismo método que hoy invoca `AuthnMiddleware` (`internal/auth/infra/authhttp/authn.go`) en cada request autenticado para poblar los permisos del `CurrentUser`, que luego `RequirePermission` (`internal/auth/infra/authhttp/authz.go`) verifica contra el permiso exigido por la ruta. Se necesita un mapeo adicional de `domain.Permission` (código) a `PermisoResponse` (código + descripción + categoría) resolviendo contra el catálogo (`PermisoRepo.FindAll` / `FindByCodigo`), ya que `PermisosFor` solo regresa códigos.

### `GET /v2/roles/{id}/permisos`
- **Permiso requerido:** `roles:listar` (`domain.PermRolesListar`).
- **Respuesta:** `ListResponse[PermisoResponse]` — los permisos asignados a ese rol.
- **Fuente de datos:** `RolRepo.PermisosFor(ctx, rolID) ([]domain.Permission, error)` (misma nota de mapeo a `PermisoResponse` que el endpoint anterior).

### Formas de respuesta (verificadas en `internal/auth/infra/authhttp/dto.go`)

```go
type RolResponse struct {
    ID          string  `json:"id"`
    Nombre      string  `json:"nombre"`
    Description *string `json:"description,omitempty"`
    Inmutable   bool    `json:"inmutable"`
    Activo      bool    `json:"activo"`
    CreatedAt   string  `json:"created_at"`
    UpdatedAt   string  `json:"updated_at"`
}

type PermisoResponse struct {
    Codigo      string `json:"codigo"`
    Description string `json:"description"`
    Categoria   string `json:"categoria"`
}

type ListResponse[T any] struct {
    Items      []T    `json:"items"`
    NextCursor string `json:"next_cursor,omitempty"`
}
```

Los 3 nuevos endpoints se suman a los ya existentes en `routes.go` bajo `/usuarios/{id}/...` y `/roles/{id}/...` (asignar/revocar rol, CRUD de roles, asignar/revocar permiso), siguiendo exactamente el mismo patrón de montaje: `r.With(RequirePermission(...)).Get(...)`.

**Nota de alcance:** sin migración de Firebird, sin nuevos códigos de permiso — los tres métodos de repositorio (`RolesFor`, `PermisosFor` de `UsuarioRepo`, `PermisosFor` de `RolRepo`) ya existen y están declarados en `internal/auth/ports/outbound/repos.go`. El trabajo de esta iniciativa es exponerlos por HTTP (DTO + mapper + query en `internal/auth/app/` + handler + ruta), no construir nueva persistencia.

## 5. Estructura del módulo frontend

Módulo nuevo `src/modules/usuariosRoles/` en `sistema-cobro-web`, siguiendo el mismo patrón hexagonal que el módulo de referencia `src/modules/configuracion/` (confirmado su árbol de directorios: `application/{ports,usecases}`, `components/`, `domain/entities`, `infrastructure/{http,mappers}`, `presentation/{composition,context,hooks}`):

```
src/modules/usuariosRoles/
  domain/            entidades Usuario, Rol, Permiso; helpers puros
                      (agrupar por categoría, unión efectiva, anti-lockout)
  application/
    ports/           UsuariosRolesPort.ts (contrato hacia infra)
    usecases/        delegadores finos sobre el port
  infrastructure/
    http/            HttpUsuariosRolesAdapter + apiClient propio del módulo
                      (mismo patrón que configuracion/infrastructure/http/apiClient.ts:
                      axios.create con baseURL `${URL_API_V2}/v2` + interceptor que
                      inyecta el Bearer token de Firebase)
    mappers/         dto ↔ domain, más errorMapper.ts (mismo patrón que
                      configuracion/infrastructure/mappers/errorMapper.ts)
  presentation/
    context/         Provider de DI + hook useUsuariosRolesPort
    composition/      UsuariosRolesContainer (memoiza el adapter)
    hooks/           useUsuarios, useRoles, useCatalogoPermisos,
                      useAsignarRol, useEditarPermisosRol, useCrudRol
  components/        UsuariosRolesScreen (tabs internos Usuarios/Roles),
                      UsuarioPanel, RolPanel
```

**Montaje:** se agrega como tab nuevo ("Usuarios y roles") dentro de `ConfiguracionShell.tsx` (`src/modules/configuracion/components/ConfiguracionShell.tsx`), que hoy ya monta las tabs "Vendedores" y "Zonas y cajas" con `Tabs`/`TabsList`/`TabsContent`. El tab nuevo es visible **solo si** `useAuth().isSuperAdmin()` (`AuthContext.isSuperAdmin`, que compara `state.userData?.ROL === ROLES.SUPER_ADMIN` — el gate sigue siendo el `ROL` de Firestore, consistente con la decisión de la sección 3: esta UI vive detrás de la misma puerta de acceso que el resto de `/configuracion`, no introduce un gate nuevo basado en permisos de Firebird).

**Reuso de infraestructura:** cada módulo de `sistema-cobro-web` construye su propio `apiClient` de axios y su propio `errorMapper` siguiendo el mismo patrón (confirmado en `configuracion`, `rutas`, `cartera`, `clientes`, `ventasLocales`, `winback`, `bandeja`, `failedIntents`); `usuariosRoles` sigue esa misma convención en vez de introducir un cliente HTTP compartido nuevo.

**Helpers de dominio clave** (funciones puras en `domain/`):
- **Agrupar permisos por categoría** — para renderizar el checklist de 40 permisos del `RolPanel` organizado por las 11 categorías del catálogo, en vez de una lista plana.
- **Unión efectiva** — replicar en el cliente, para previsualización en `UsuarioPanel`, el mismo cálculo que hace `UsuarioRepo.PermisosFor` en el servidor (unión de los permisos de todos los roles activos del usuario).
- **Predicado anti-lockout** — `noPuedeQuitarseSuPropioRolInmutable(currentUid, usuario, rol)`: bloquea en la UI que un `super_admin` se quite a sí mismo el rol inmutable `super_admin`, para no dejar el sistema sin ningún administrador con acceso a la propia herramienta de administración.

## 6. El norte: unificación futura

El estado actual — gating del front en Firestore, autorización de la API en Firebird, sin punto de contacto entre ambos — no es el destino, es una etapa de transición. La visión de largo plazo es migrar progresivamente **todo** el gating (hoy repartido entre Firestore y Firebird) hacia la API Go como fuente única de verdad de autorización: el front dejaría de preguntar "¿tu `ROL` de Firestore es `ADMIN`?" y empezaría a preguntar "¿tienes el permiso `X` según la API?", usando el mismo catálogo granular de 40 códigos que ya gobierna cada endpoint.

Esta UI es el primer paso concreto hacia ese norte, no un desvío: hasta hoy el sistema de permisos de Firebird era correcto pero inutilizable en la práctica (solo administrable por SQL directo), lo cual hacía impensable apoyarse en él para gatear nada más. Con administración real —crear roles, asignar permisos, asignar roles a usuarios, todo sin tocar la base de datos a mano— el sistema de Firebird se vuelve una base sobre la que sí tiene sentido, en un proyecto futuro y deliberado, empezar a migrar el gating del front pantalla por pantalla.
