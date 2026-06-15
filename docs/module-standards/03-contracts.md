# Step 03 — Contracts

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: Step 01 (domain entities), Step 02 (value objects + errors).
> Parallel with: —

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api.**
> Diferencias clave vs. ancla: (1) **el módulo `auth` expone un tercer género de
> contrato** — re-exportación de constantes de permiso y un tipo alias (`Permission`)
> que otros módulos usan en middleware, algo que ancla no necesita; (2) la función
> mapper del módulo `auth` se llama `ToContract` (sin prefijo de entidad) porque el
> módulo exporta un solo tipo de contrato (`CurrentUser`); ancla exige siempre
> `To{Entity}Contract`; (3) **no hay `pipeline_contracts.go` en msp-api** — ancla
> lo usa para documentar sus cadenas de procesamiento de documentos legales; msp-api
> no tiene pipelines entre módulos que justifiquen un archivo centralizado; (4)
> **el contrato puede incluir funciones utilitarias de contexto** (`PlantCurrentUser`,
> `CurrentUserFromContext`) siempre que no filtren lógica de dominio — lo demuestra
> `auth_contracts.go` + `ctxuser.go` actuales; (5) msp-api no usa optimistic locking
> en ningún módulo, por lo que los contratos no incluyen campo `version`.

| Aspecto | ancla-api | msp-api (real) |
|---|---|---|
| Tipos de contrato | Data-only structs | Data-only structs (igual) |
| Nombre del mapper | `To{Entity}Contract` | `To{Entity}Contract`; excepcional: `ToContract` en módulos con un solo tipo exportado |
| Permiso re-export | No aplica | `type Permission = domain.Permission` + `const` block en `auth_contracts.go` |
| Contexto util | No aplica | `PlantCurrentUser`/`CurrentUserFromContext` en `ctxuser.go` (mismo package que el contrato) |
| `pipeline_contracts.go` | Sí (en `platform/`) | No — msp-api no tiene cadenas inter-módulo de ese tipo |
| `version` en struct | No (ancla tampoco) | No — msp-api no usa optimistic locking |
| Package doc header | No especificado | Obligatorio (patrón `auth`: enumera qué produce y quién puede importar) |
| Imports del mapper | `github.com/ancla-ai/ancla/...` | `github.com/abdimuy/msp-api/...` |

---

## Files to create

```
internal/{module}/{module}_contracts.go          ← tipos que el módulo expone al exterior
internal/{module}/{module}_contracts_mapper.go    ← domain → contrato; SOLO lo llaman infra/clients de otros módulos
```

El package root `internal/{module}` es la **única** superficie de importación permitida
para otros módulos. El linter `depguard` prohíbe importar `internal/{x}/domain`,
`internal/{x}/app` o `internal/{x}/infra` desde otro módulo.

---

## Propósito

Los contratos definen la **superficie pública** del módulo. Cuando el módulo A necesita
datos del módulo B, importa los tipos de contrato de B — nunca el dominio, app ni infra de B.

Un agente que lee `{module}_contracts.go` sabe todo lo que el módulo expone. No necesita
leer ningún otro archivo.

---

## `{module}_contracts.go`

### Package doc — obligatorio

Cada archivo de contratos comienza con un package doc que declara qué produce el módulo
y quién puede importarlo. Patrón real del módulo `auth`:

```go
// Package auth is the cross-module surface of the auth bounded context.
// Other modules import only this package — never internal/auth/domain,
// internal/auth/app, or internal/auth/infra. The depguard linter enforces
// the rule.
//
// The contract exports:
//   - CurrentUser: la vista proyectada del principal autenticado.
//   - Permission: type alias de domain.Permission.
//   - Re-exportaciones de códigos de permiso (una constante por permiso).
//   - PlantCurrentUser / CurrentUserFromContext (en ctxuser.go).
package auth
```

Un agente que modifica un contrato lee el package doc para saber qué módulos se ven
afectados.

### Struct de contrato

```go
package {module}

import (
    "time"

    "github.com/google/uuid"
)

type {Entity}Contract struct {
    ID        uuid.UUID
    Campo1    string
    Campo2    int
    Campo3    *string   // puntero solo cuando el campo es genuinamente opcional
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Reglas

1. **Todos los campos exportados.** Los consumidores acceden `contract.Campo`, no
   `contract.Campo()`.

2. **Solo tipos primitivos.** Los VOs del dominio se convierten a su tipo de respaldo:

   | Tipo en dominio | Tipo en contrato |
   |---|---|
   | `TipoVenta` (enum VO) | `string` |
   | `EstadoRegistro` (state VO) | `string` |
   | `Direccion` (composite VO) | Campos primitivos inlineados (`Calle string`, `Colonia string`, …) |
   | `audit.Auditable` | Inlineado: `CreatedAt`, `UpdatedAt`, `CreatedBy`, `UpdatedBy` |
   | `audit.Timestamped` | Inlineado: `CreatedAt`, `UpdatedAt` |
   | `audit.MicrosipSync` | Inlineado: `MicrosipID *int`, `PushedAt *time.Time`, `PulledAt *time.Time` |
   | `decimal.Decimal` / `Money` | `string` (representación textual exacta, sin `float64`) |

3. **Sin funciones de comportamiento.** Los contratos son datos, no comportamiento.

4. **Sin interfaces.** Las interfaces van en `ports/outbound/`.

5. **Sin imports de otros módulos.** Solo stdlib + `github.com/google/uuid`.

6. **Sin errores de dominio.** Los errores pertenecen al dominio. Los consumidores
   reciben el error traducido por la capa de servicio como `*apperror.Error`.

7. **Sin campo `version`.** msp-api no usa optimistic locking en ningún módulo.

### Extensión: re-exportación de constantes (patrón `auth`)

Cuando el módulo gestiona recursos que otros módulos necesitan referenciar por código
(p. ej. permisos), el contrato puede re-exportar constantes y alias de tipo del dominio.
Esto **no rompe el aislamiento** porque los type alias son transparentes al compilador.

```go
// Permission is re-exported from the auth domain so other modules can
// reference permission codes by their typed form without importing auth/domain.
type Permission = domain.Permission

// Re-exportaciones de códigos. Agregar un código nuevo aquí requiere también
// agregarlo en internal/auth/domain/permission_codes.go.
const (
    PermVentasCrear    = domain.PermVentasCrear
    PermVentasCancelar = domain.PermVentasCancelar
    // ...
)
```

Esta extensión es específica del módulo `auth`. **No la apliques a módulos de negocio
ordinarios** (ventas, cobranza, traspasos): sus contratos son solo structs de datos.

---

## `{module}_contracts_mapper.go`

Una función mapper por tipo de contrato. Proyecta la entidad de dominio al struct de
contrato.

### Plantilla — Tipo A / C

```go
package {module}

import "github.com/abdimuy/msp-api/internal/{module}/domain"

// To{Entity}Contract projects the domain entity into the cross-module view.
// Called only from infra/clients of consumer modules — never from this
// module's own service layer.
func To{Entity}Contract(e *domain.{Entity}) {Entity}Contract {
    return {Entity}Contract{
        ID:        e.ID(),
        Campo1:    e.Campo1(),
        Campo2:    e.Campo2().String(),    // enum VO → string
        Campo3:    e.Campo3(),             // campo primitivo directo
        CreatedAt: e.Audit().CreatedAt(),  // audit.Auditable
        UpdatedAt: e.Audit().UpdatedAt(),
        CreatedBy: e.Audit().CreatedBy(),
        UpdatedBy: e.Audit().UpdatedBy(),
    }
}
```

### Plantilla — Tipo B (pipeline / máquina de estados)

```go
func To{Entity}Contract(e *domain.{Entity}) {Entity}Contract {
    return {Entity}Contract{
        ID:        e.ID(),
        Estado:    e.Estado().String(),          // state VO → string
        Campo1:    e.Campo1(),
        CreatedAt: e.Timestamps().CreatedAt(),   // audit.Timestamped
        UpdatedAt: e.Timestamps().UpdatedAt(),
    }
}
```

### Mapper con nombre simplificado (patrón `auth`)

Cuando el módulo exporta un **único tipo de contrato** y el contexto hace inequívoca la
entidad, se permite omitir el nombre de la entidad en la función:

```go
// ToContract projects a domain Usuario plus its derived permission codes
// into the cross-module CurrentUser view.
func ToContract(u *domain.Usuario, perms []domain.Permission) CurrentUser {
    codes := make([]string, len(perms))
    for i, p := range perms {
        codes[i] = string(p)
    }
    return CurrentUser{
        ID:          u.ID(),
        FirebaseUID: u.FirebaseUID().Value(),
        Email:       u.Email().Value(),
        Nombre:      u.Nombre().Value(),
        AlmacenID:   u.AlmacenID(),
        Permisos:    codes,
    }
}
```

El nombre por defecto para módulos con múltiples entidades exportadas es
`To{Entity}Contract`.

### Helper de slice

Si los consumidores necesitan listas, agrega:

```go
func To{Entity}Contracts(entities []*domain.{Entity}) []{Entity}Contract {
    result := make([]{Entity}Contract, len(entities))
    for i, e := range entities {
        result[i] = To{Entity}Contract(e)
    }
    return result
}
```

### Reglas del mapper

1. **Una función por tipo de contrato.** Nombre: `To{Entity}Contract` (o `ToContract`
   en módulos con un solo tipo exportado).
2. **Usa los getters de la entidad.** Nunca accede a campos privados directamente.
3. **Convierte VOs a primitivos.** Llama `.String()` en enum/state VOs; extrae campos
   de composite VOs mediante sus getters.
4. **Sin validación.** El dominio ya validó. El mapper solo transforma.
5. **Solo lo llaman los adaptadores cross-module** (`infra/clients/` del módulo consumidor).
   Nunca lo llama el servicio propio del módulo — haría circular el grafo de importación
   (raíz del módulo → dominio está bien; servicio → raíz del módulo crearía un ciclo).
6. **Sin `fmt.Errorf`/`errors.New`.** Si el mapper necesita reportar un error (raro),
   usa un sentinela `apperror` del dominio.

---

## Consumo cross-module

Cuando el módulo A necesita datos del módulo B:

### 1. El módulo B expone contratos

```go
// internal/ventas/ventas_contracts.go (cuando se cree)
package ventas

import (
    "time"

    "github.com/google/uuid"
)

// Module: ventas
// Produces: VentaContract
// Consumed by: cobranza (para vincular pagos a ventas)

type VentaContract struct {
    ID             uuid.UUID
    Folio          string
    TipoVenta      string    // TipoVenta VO → string
    Estado         string    // EstadoRegistro VO → string
    ClienteID      *int      // FK Microsip, nil si aún no sincronizada
    AlmacenID      int
    TotalStr       string    // decimal.Decimal → string exacto
    CreatedAt      time.Time
    UpdatedAt      time.Time
    CreatedBy      uuid.UUID
    UpdatedBy      uuid.UUID
}
```

### 2. El módulo A define un puerto outbound usando los tipos de B

```go
// internal/cobranza/ports/outbound/ventas_client_port.go
package outbound

import (
    "context"

    "github.com/google/uuid"
    "github.com/abdimuy/msp-api/internal/ventas"
)

// VentasClient is the port cobranza uses to look up sale data without
// crossing the ventas module boundary.
type VentasClient interface {
    GetVenta(ctx context.Context, id uuid.UUID) (*ventas.VentaContract, error)
}
```

### 3. El módulo A implementa el cliente en infra

```go
// internal/cobranza/infra/clients/ventas_client.go
package clients

import (
    "context"

    "github.com/google/uuid"
    "github.com/abdimuy/msp-api/internal/ventas"
)

type VentasClientImpl struct {
    // Depende del servicio o repo de ventas — inyectado vía wiring.
}

func NewVentasClient(...) *VentasClientImpl { ... }

// GetVenta implements outbound.VentasClient.
func (c *VentasClientImpl) GetVenta(ctx context.Context, id uuid.UUID) (*ventas.VentaContract, error) {
    // Llama al servicio/repo de ventas y proyecta con ventas.To{Entity}Contract.
    ...
}
```

### 4. El módulo `auth` ya muestra el patrón completo y real

El módulo `auth` es la referencia concreta de este flujo. Otros módulos importan
`"github.com/abdimuy/msp-api/internal/auth"` para acceder a `auth.CurrentUser`,
`auth.CurrentUserFromContext`, `auth.Permission` y las constantes de permiso. Lo hace
`internal/ventas/infra/venthttp/auth.go`:

```go
import "github.com/abdimuy/msp-api/internal/auth"

func currentUserOrError(ctx context.Context) (auth.CurrentUser, error) {
    cu, ok := auth.CurrentUserFromContext(ctx)
    if !ok {
        return auth.CurrentUser{}, huma.Error401Unauthorized("no autenticado")
    }
    return cu, nil
}

func requirePerm(cu auth.CurrentUser, perms ...auth.Permission) error { ... }
```

Ninguna línea en `venthttp` importa `internal/auth/domain`; depguard lo impide.

### Fórmulas de navegación cross-module

| Necesidad | Ruta deducible |
|---|---|
| ¿Qué expone el módulo X? | `internal/{x}/{x}_contracts.go` |
| ¿Cómo proyecta X su dominio? | `internal/{x}/{x}_contracts_mapper.go` |
| ¿Cómo declara Y que necesita X? | `internal/{y}/ports/outbound/{x}_client_port.go` |
| ¿Dónde implementa Y el cliente? | `internal/{y}/infra/clients/{x}_client.go` |

---

## Depguard — reglas vigentes

Las reglas relevantes del `.golangci.yml` que hacen obligatorio este patrón:

```yaml
no-cross-module-internals:
  list-mode: lax
  files:
    - "**/internal/!(platform)/**/*.go"
  deny:
    - pkg: "github.com/abdimuy/msp-api/internal/*/domain"
      desc: "do not import another module's domain — use its contracts via the module root package"
    - pkg: "github.com/abdimuy/msp-api/internal/*/app"
      desc: "do not import another module's app — use its contracts and a port"
    - pkg: "github.com/abdimuy/msp-api/internal/*/infra"
      desc: "do not import another module's infra — use its contracts and a port"
```

El package root `internal/{module}` (donde viven `{module}_contracts.go` y
`{module}_contracts_mapper.go`) es el único path que no está en la deny-list. Esto hace
que importar el package root sea la única vía legal para comunicación cross-module.

---

## Ejemplo completo — módulo `auth` (código real)

### `internal/auth/auth_contracts.go` (extracto)

```go
package auth

import (
    "github.com/google/uuid"
    "github.com/abdimuy/msp-api/internal/auth/domain"
)

// CurrentUser is the projected, cross-module view of the authenticated principal.
// Flat struct of primitive values — other modules consume it without importing
// the auth domain types.
type CurrentUser struct {
    ID          uuid.UUID
    FirebaseUID string
    Email       string
    Nombre      string
    AlmacenID   *int
    Permisos    []string
}

// Permission is re-exported from the auth domain so other modules can reference
// permission codes by their typed form without importing auth/domain directly.
type Permission = domain.Permission

const (
    PermVentasCrear            = domain.PermVentasCrear
    PermVentasCancelar         = domain.PermVentasCancelar
    PermVentasSubirImagenes    = domain.PermVentasSubirImagenes
    // ... resto de códigos
)
```

### `internal/auth/auth_contracts_mapper.go` (código real completo)

```go
package auth

import "github.com/abdimuy/msp-api/internal/auth/domain"

// ToContract projects a domain Usuario plus its derived permission codes
// into the cross-module CurrentUser view. The conversion is pure: it
// allocates a fresh slice for Permisos so the caller can hand the result
// off to long-lived context values without aliasing the input slice.
func ToContract(u *domain.Usuario, perms []domain.Permission) CurrentUser {
    codes := make([]string, len(perms))
    for i, p := range perms {
        codes[i] = string(p)
    }
    return CurrentUser{
        ID:          u.ID(),
        FirebaseUID: u.FirebaseUID().Value(),
        Email:       u.Email().Value(),
        Nombre:      u.Nombre().Value(),
        AlmacenID:   u.AlmacenID(),
        Permisos:    codes,
    }
}
```

### `internal/auth/ctxuser.go` — funciones utilitarias de contexto (mismo package)

Las funciones `PlantCurrentUser` y `CurrentUserFromContext` viven en un archivo
separado dentro del mismo package raíz del módulo. Esto es aceptable porque:
- No filtran lógica de dominio — solo gestionan la clave de contexto.
- Forman parte de la superficie pública del módulo (otros módulos los llaman).
- El package doc en `auth_contracts.go` las lista explícitamente.

Solo el módulo `auth` necesita este patrón. No lo repliques en módulos ordinarios.

---

## Agent checklist

- [ ] Archivo nombrado `{module}_contracts.go` (no `contracts.go`).
- [ ] Archivo nombrado `{module}_contracts_mapper.go` (no `contracts_mapper.go`).
- [ ] Package doc enumera qué produce el módulo y qué está prohibido importar.
- [ ] Todos los campos del struct son exportados (PascalCase).
- [ ] Todos los tipos son primitivos: no VOs de dominio, no tipos custom de otros módulos.
- [ ] Sin campo `version` — msp-api no usa optimistic locking.
- [ ] Sin funciones de comportamiento en el archivo de contratos (solo tipos y constantes).
- [ ] Sin interfaces en el archivo de contratos — van en `ports/outbound/`.
- [ ] Sin imports de otros módulos en `{module}_contracts.go` (solo stdlib + `uuid`).
- [ ] El mapper usa getters de la entidad, nunca campos privados.
- [ ] El mapper convierte VOs a primitivos vía `.String()` o extracción de campos.
- [ ] `To{Entity}Contracts` (slice helper) existe si los consumidores necesitan listas.
- [ ] El mapper solo lo llaman adaptadores cross-module (`infra/clients/` del consumidor).
- [ ] El puerto outbound del módulo consumidor sigue la convención `{producer}_client_port.go`.
- [ ] La implementación del cliente sigue la convención `{producer}_client.go` en `infra/clients/`.
- [ ] Re-exportación de constantes solo en el módulo `auth` (o equivalente de seguridad/identidad).
- [ ] No se creó `pipeline_contracts.go` — msp-api no usa ese patrón.
