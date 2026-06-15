# Step 02 — Value Objects & Errors

> Applies to: Type A, B, C.
> Depends on: —
> Parallel with: Step 01 (domain entities).

> **Adaptado de `ancla-api`, reconciliado con el código real de `ventas`.** Diferencias clave vs.
> ancla: (1) los VO validan con **sentinelas `apperror`**, nunca `fmt.Errorf`/`errors.New` (linter
> `err113`); (2) los enums calzan la forma **persistida en Microsip** (normalmente MAYÚSCULAS), sin
> `strings.ToLower`; (3) los composite VO usan **struct de params**; (4) **un solo `errors.go` por
> módulo** (no por entidad); (5) mensajes en **español minúscula sin punto final**; (6) archivo VO =
> `{concepto}.go` (sin sufijo `_vo`).

---

## Files to create

```
internal/{module}/domain/{concepto}.go        # un archivo por VO (tipo_venta.go, direccion.go, frec_pago.go)
internal/{module}/domain/errors.go            # UN archivo de errores por módulo
```

Package `domain`. Imports permitidos: stdlib + `uuid` + `shopspring/decimal` +
`internal/platform/{apperror,domain,audit}`.

---

## VO Categoría 1 — Enum VO

String-backed, valores finitos, sin transiciones. **3 métodos: Parse, IsValid, String.**
`Parse` devuelve un **sentinela `apperror`** (no `fmt.Errorf`). Los valores calzan la columna
Microsip (normalmente MAYÚSCULAS); **no** se hace `strings.ToLower`.

### Ejemplo real — `tipo_venta.go`

```go
package domain

// TipoVenta enumerates the kind of sale: cash (CONTADO) or credit (CREDITO).
type TipoVenta string

const (
    TipoVentaContado TipoVenta = "CONTADO" // calza MSP_VENTAS.TIPO_VENTA
    TipoVentaCredito TipoVenta = "CREDITO"
)

func ParseTipoVenta(s string) (TipoVenta, error) {
    t := TipoVenta(s)
    if !t.IsValid() {
        return "", ErrTipoVentaInvalido // sentinela apperror, NO fmt.Errorf
    }
    return t, nil
}

func (t TipoVenta) IsValid() bool {
    switch t {
    case TipoVentaContado, TipoVentaCredito:
        return true
    }
    return false
}

func (t TipoVenta) String() string { return string(t) }
```

| Método | Firma |
|---|---|
| Parse | `Parse{VO}(string) ({VO}, error)` → sentinela `apperror` en fallo |
| IsValid | `(v {VO}) IsValid() bool` |
| String | `(v {VO}) String() string` |

> Si el input del cliente puede venir en otra caja (minúsculas), normaliza **antes** de `Parse`
> (p. ej. `strings.ToUpper`) en el borde, no dentro del VO — el VO calza la forma canónica Microsip.

---

## VO Categoría 2 — State VO

String-backed con máquina de estados (para Tipo B). **5 métodos + mapa de transiciones.**
`Parse` devuelve sentinela `apperror`.

```go
package domain

type {VO} string

const (
    {VO}Pendiente {VO} = "PENDIENTE"
    {VO}Activo    {VO} = "ACTIVO"
    {VO}Completado {VO} = "COMPLETADO"
    {VO}Fallido   {VO} = "FALLIDO"
)

var valid{VO}Transitions = map[{VO}][]{VO}{
    {VO}Pendiente: {{VO}Activo, {VO}Fallido},
    {VO}Activo:    {{VO}Completado, {VO}Fallido},
}

func Parse{VO}(s string) ({VO}, error) {
    v := {VO}(s)
    if !v.IsValid() {
        return "", Err{VO}Invalido
    }
    return v, nil
}

func (v {VO}) IsValid() bool { /* switch sobre los valores */ }
func (v {VO}) String() string { return string(v) }

func (v {VO}) CanTransitionTo(target {VO}) bool {
    for _, a := range valid{VO}Transitions[v] {
        if a == target {
            return true
        }
    }
    return false
}

func (v {VO}) IsTerminal() bool {
    switch v {
    case {VO}Completado, {VO}Fallido:
        return true
    }
    return false
}
```

| Método | Firma |
|---|---|
| Parse | `Parse{VO}(string) ({VO}, error)` |
| IsValid | `(v {VO}) IsValid() bool` |
| String | `(v {VO}) String() string` |
| CanTransitionTo | `(v {VO}) CanTransitionTo({VO}) bool` |
| IsTerminal | `(v {VO}) IsTerminal() bool` |
| Mapa | `var valid{VO}Transitions map[{VO}][]{VO}` |

Referencia real: `internal/ventas/domain/estado_registro.go`, `situacion.go`.

---

## VO Categoría 3 — Composite VO

Struct-backed, inmutable, múltiples campos. **Se pasa por valor, sin setters.**
Para VO multi-campo, usar **struct de params** (`New{VO}Params`); `Hydrate{VO}` reusa el mismo
struct. La validación devuelve **sentinelas `apperror`**. `String()`/`Equals()` son **opcionales**
(solo cuando el concepto los necesita — `Direccion` no los tiene).

### Ejemplo real — `direccion.go` (recortado)

```go
package domain

import "strings"

const maxCalleLength = 300 // ... espeja anchos de columna Firebird

type Direccion struct {
    calle          string
    numeroExterior *string
    colonia        string
    poblacion      string
    ciudad         string
    zonaClienteID  *int
}

type NewDireccionParams struct {
    Calle, Colonia, Poblacion, Ciudad string
    NumeroExterior *string
    ZonaClienteID  *int
}

func NewDireccion(p NewDireccionParams) (Direccion, error) {
    calle, err := requireBoundedUpper(p.Calle, maxCalleLength, ErrCalleRequerida, ErrCalleDemasiadoLarga)
    if err != nil {
        return Direccion{}, err
    }
    // ... idem colonia/poblacion/ciudad; numExt via trimOptionalBoundedUpper
    return Direccion{calle: calle /* ... */}, nil
}

func HydrateDireccion(p NewDireccionParams) Direccion { // reusa el mismo params struct
    return Direccion{calle: p.Calle /* ... */}
}

func (d Direccion) Calle() string          { return d.calle }       // getter por valor
func (d Direccion) NumeroExterior() *string { return d.numeroExterior }
```

| Método | Requerido |
|---|---|
| `New{VO}(p New{VO}Params) ({VO}, error)` | siempre (valida) |
| `Hydrate{VO}(p New{VO}Params) {VO}` | siempre (sin validación) |
| getters por valor | uno por campo |
| `String()` / `Equals()` | **opcional** (solo si se necesita) |
| métodos de dominio (`IsActive`, `Contains`…) | solo si el concepto lo pide |

### Helpers de string compartidos (rigor msp-api)

Los campos de texto pasan por un pipeline: **TrimSpace → NFC → rechazar vacío → longitud en runes
(no bytes, por `CHARACTER SET UTF8`) → safe-chars (sin NUL/control)**, con variante **ALL-CAPS fold**
para texto que Microsip guarda en mayúsculas. Ver `requireBounded`/`requireBoundedUpper`/
`trimOptionalBounded(Upper)` en `internal/ventas/domain/direccion.go`. Reutiliza este patrón; no
reinventes la validación de strings. (Encoding: ver `ENCODING_HANDLING.md`.)

---

## VOs transversales (multi-módulo)

Viven en `internal/platform/domain/` (los de audit en `internal/platform/audit`). No los redefinas
por módulo.

| VO | Ubicación |
|---|---|
| `Money` (sobre `shopspring/decimal`) | `internal/platform/domain/money_vo.go` |
| `RFC` | `internal/platform/domain/rfc_vo.go` |
| `Telefono` | `internal/platform/domain/telefono_vo.go` |
| `Auditable` / `Timestamped` / `MicrosipSync` | `internal/platform/audit/audit.go` |

VOs específicos del módulo se quedan en `internal/{module}/domain/`. **Dinero siempre como `Money`/
`decimal.Decimal`, nunca `float64`** (ver escala en `internal/ventas/domain/decimal_scale.go`).

---

## Errores

Un **único `errors.go` por módulo** (no por entidad), agrupado por entidad con headers de comentario.
Todos vía `apperror.New{Validation,NotFound,Conflict,Forbidden,Internal}`. **Nunca** `errors.New`/
`fmt.Errorf` (linter `err113`).

### Convención de mensajes (regla del proyecto)
- **Código** (1er arg): inglés `snake_case`, globalmente único.
- **Mensaje** (2º arg): **español, minúscula, sin punto final**.

### Ejemplo real — `errors.go`

```go
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

var (
    // --- Venta ---
    ErrVentaNotFound = apperror.NewNotFound(
        "venta_not_found",
        "venta no encontrada",
    )
    ErrVentaYaCancelada = apperror.NewConflict(
        "venta_ya_cancelada",
        "la venta ya está cancelada",
    )
    // --- TipoVenta (VO) ---
    ErrTipoVentaInvalido = apperror.NewValidation(
        "tipo_venta_invalido",
        "el tipo de venta no es válido",
    )
    // --- Direccion (VO) ---
    ErrCalleRequerida = apperror.NewValidation(
        "calle_requerida",
        "la calle es obligatoria",
    )
)
```

### Fórmula de nombre
- Variable Go: `Err{Entity}{Category}{Detail}` (o `Err{VO}{Detail}` para VOs).
- Código: `{entity}_{category}_{detail}` snake_case.

### Selección de constructor

| Situación | Constructor | HTTP |
|---|---|---|
| No encontrado | `apperror.NewNotFound` | 404 |
| Campo requerido vacío | `apperror.NewValidation` | 422 |
| VO no parsea | `apperror.NewValidation` | 422 |
| Transición de estado inválida | `apperror.NewValidation` | 422 |
| Regla de negocio violada | `apperror.NewValidation` | 422 |
| Unicidad violada | `apperror.NewConflict` | 409 |
| FK impide borrar | `apperror.NewConflict` | 409 |
| Ya en estado terminal | `apperror.NewConflict` | 409 |
| Sin permiso (raro en domain) | `apperror.NewForbidden` | 403 |

> **No incluimos** `ConcurrentModification` — msp-api no usa optimistic locking.

### Reglas
- Todos los errores son `var` (nunca `const`), un solo bloque `var ( )` por archivo.
- Solo constructores `apperror`. En la capa de servicio se enriquece con `.WithSource()`/`.WithError()`
  (devuelven copia; el sentinela no se muta).
- Si el módulo tiene vocabulario español, agrega `//nolint:misspell` al package doc (como `ventas`).

> **Opción para módulos grandes:** si los errores crecen mucho (ventas tiene ~90 en un archivo),
> se permite partir en `{entity}_errors.go` por entidad (patrón ancla). Por defecto: un `errors.go`.

---

## Agent checklist

- [ ] Enum VO: exactamente Parse/IsValid/String; `Parse` devuelve **sentinela `apperror`**.
- [ ] Enum VO: valores calzan la forma persistida en Microsip (sin `strings.ToLower` interno).
- [ ] State VO: 5 métodos + mapa de transiciones; `Parse` con sentinela.
- [ ] Composite VO: `New{VO}Params`, `New` valida, `Hydrate` no; getters por valor; sin setters.
- [ ] Composite VO: `String`/`Equals` solo si se necesitan; strings vía helpers (NFC/runes/safe-chars).
- [ ] `errors.go` único por módulo; todos `apperror`; código inglés snake_case; mensaje español minúscula sin punto.
- [ ] Sin `fmt.Errorf`/`errors.New`; sin `ConcurrentModification`.
- [ ] Dinero como `Money`/`decimal`, nunca `float64`.
- [ ] VOs transversales en `internal/platform/domain`; específicos en el módulo.
- [ ] Archivo VO = `{concepto}.go` (sin sufijo `_vo`); package `domain`.
