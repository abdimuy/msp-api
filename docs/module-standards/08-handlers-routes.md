# Step 08 — Handlers & Routes

> Applies to: Type A (CRUD) y Type B (Pipeline).
> Depends on: Step 06 (app service) y Step 07 (repositorio).
> Parallel with: —
> Scope: la capa de transporte HTTP — DTOs, handlers Huma, y montaje de rutas sobre chi.

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api (Huma+chi).**

---

## ancla vs. msp-api — diferencias clave

| Aspecto | ancla-api | msp-api (real) |
|---|---|---|
| Framework HTTP | chi + `httpx` manual | **Huma v2** sobre chi vía `humachi` |
| OpenAPI | YAML manual `openapi.yaml` | **Auto-generado** desde struct tags + `huma.Register` |
| Handler signature | `func(w http.ResponseWriter, r *http.Request)` | **`func(ctx context.Context, in *XInput) (*XOutput, error)`** |
| Decode + validate | `httpx.DecodeJSON` + `validate.Struct` | **Huma lo hace** antes de invocar el handler |
| Responder | `response.OK/Created/Error(w, ...)` | **`return &XOutput{Body: dto}, nil`** o `return nil, humaErr` |
| Errores | `response.Error(w, err)` | **`mapAppError(err)`** → `huma.NewError(status, msg, detail)` |
| Auth middleware | `mw.RequirePermission(...)` en ruta | **`currentUserOrError` + `requirePerm(cu, perm)`** dentro del handler |
| Estructura de paquete | `infra/http/` | **`infra/{module}http/`** (p. ej. `venthttp`, `authhttp`) |
| DTO tags | `json:` + `validate:` | **`json:` + `doc:` + `format:` + `enum:` + `minimum:` + `required:`** (Huma) |
| Multipart | manual | **`huma.MultipartFormFiles[T]`** |
| Spec URL | N/A | `/v2/openapi.json` + Scalar UI |

> **Nota sobre `auth`:** el módulo `auth` usa chi puro + `response.JSON/Error` (patrón legado).
> Nuevos módulos siguen el patrón Huma de `ventas`. Este doc describe el estándar Huma.

---

## Archivos a crear

```
internal/{module}/infra/{module}http/
├─ dto.go              # DTOs de input/output (Input/Output wrappers + Body structs)
├─ dto_mapper.go       # domain entity → DTO (funciones to{Entity}DTO)
├─ handlers_{entity}.go   # struct Handlers + métodos Huma por entidad
├─ auth.go             # currentUserOrError, requirePerm, mapAppError
└─ routes.go           # MountRouter + registerOperations
```

Para módulos con dos entidades en el mismo módulo: un archivo `handlers_` por entidad; un único `routes.go` que registra todas las operaciones.

Package name: `{module}http` (p. ej. `venthttp`, `authhttp`). El paquete nunca es importado desde dentro del módulo — es el adaptador de borde más externo.

---

## Handler struct

```go
// internal/{module}/infra/{module}http/handlers_{entity}.go
package {module}http

import {module}app "github.com/abdimuy/msp-api/internal/{module}/app"

// Handlers groups every Huma handler for the {module} module.
type Handlers struct {
    svc *{module}app.Service
}

// NewHandlers wires a Handlers with its application service dependency.
func NewHandlers(svc *{module}app.Service) *Handlers {
    return &Handlers{svc: svc}
}
```

**Reglas:**

- El struct solo tiene `svc`. Sin logger, sin repo, sin validator (Huma valida antes de invocar).
- El handler no habla con el repositorio directamente, solo con el servicio de app.
- No registres eventos de negocio desde el handler — eso ocurre en la capa de servicio dentro de la transacción.
- Usa `r.Context()` / el `ctx` que Huma pasa — nunca lo reemplaces.

---

## Firma Huma

Todo handler Huma tiene esta firma exacta:

```go
func (h *Handlers) {Accion}{Entidad}(ctx context.Context, in *{Accion}{Entidad}Input) (*{Accion}{Entidad}Output, error)
```

Huma valida el input antes de invocar el handler. Si la validación falla (campo requerido ausente, formato `uuid` inválido, valor fuera de `enum`, etc.) el handler **nunca se ejecuta** — la respuesta 422 la genera Huma.

---

## Flujo de cada handler

```
1. Extraer usuario autenticado   → currentUserOrError(ctx)
2. Verificar permiso             → requirePerm(cu, perm.XYZ)
3. Parsear campos no-Huma        → parseUUIDField / parseTimeField / parseDecimalField
4. Llamar al servicio            → h.svc.{Metodo}(ctx, ...)
5. Error → return nil, mapAppError(err)
6. Éxito → return &{Accion}{Entidad}Output{Body: to{Entidad}DTO(v)}, nil
```

Huma se encarga del paso de decode+validate del body y de los path/query/header params declarados en la struct Input.

---

## Handlers — ejemplos reales

### GET por ID

```go
// ObtenerVenta es el handler para GET /v2/ventas/{id}.
func (h *Handlers) ObtenerVenta(ctx context.Context, in *ObtenerVentaInput) (*ObtenerVentaOutput, error) {
    cu, err := currentUserOrError(ctx)
    if err != nil {
        return nil, err
    }
    if err := requirePerm(cu, auth.PermVentasVer); err != nil {
        return nil, err
    }
    id, err := parseUUIDField(in.ID, "id")
    if err != nil {
        return nil, mapAppError(err)
    }
    v, err := h.svc.ObtenerVenta(ctx, id)
    if err != nil {
        return nil, mapAppError(err)
    }
    return &ObtenerVentaOutput{Body: toVentaDTO(v, nil)}, nil
}
```

### POST (multipart — creación atómica)

```go
// CrearVenta es el handler para POST /v2/ventas.
// Acepta multipart/form-data: campo `datos` (JSON) + uno o más campos `imagen`.
func (h *Handlers) CrearVenta(ctx context.Context, in *CrearVentaInput) (*CrearVentaOutput, error) {
    cu, err := currentUserOrError(ctx)
    if err != nil {
        return nil, err
    }
    if err := requirePerm(cu, auth.PermVentasCrear); err != nil {
        return nil, err
    }

    fields := in.RawBody.Data()
    body, err := decodeCrearVentaDatos(fields.Datos)
    if err != nil {
        return nil, mapAppError(err)
    }
    input, err := crearVentaBodyToAppInput(body)
    if err != nil {
        return nil, mapAppError(err)
    }
    imgUploads, openedFiles, err := parseImagenesFromMultipart(ventaID, fields.Imagen, in.RawBody.Form)
    defer closeFiles(openedFiles)
    if err != nil {
        return nil, mapAppError(err)
    }

    v, err := h.svc.CrearVentaConImagenes(ctx, input, imgUploads, cu.ID)
    if err != nil {
        return nil, mapAppError(err)
    }
    return &CrearVentaOutput{Body: toVentaDTO(v, nil)}, nil
}
```

### POST sin body (transición de estado)

```go
// RevisarVenta es el handler para POST /v2/ventas/{id}/revisar.
// No body — la transición se activa solo con el path id.
func (h *Handlers) RevisarVenta(ctx context.Context, in *RevisarVentaInput) (*RevisarVentaOutput, error) {
    cu, err := currentUserOrError(ctx)
    if err != nil {
        return nil, err
    }
    if err := requirePerm(cu, auth.PermVentasRevisar); err != nil {
        return nil, err
    }
    id, err := parseUUIDField(in.ID, "id")
    if err != nil {
        return nil, mapAppError(err)
    }
    v, err := h.svc.EnviarARevision(ctx, id, cu.ID)
    if err != nil {
        return nil, mapAppError(err)
    }
    return &RevisarVentaOutput{Body: toVentaDTO(v, nil)}, nil
}
```

### GET lista (cursor-paginada + filtros)

```go
// ListarVentas es el handler para GET /v2/ventas.
func (h *Handlers) ListarVentas(ctx context.Context, in *ListarVentasInput) (*ListarVentasOutput, error) {
    cu, err := currentUserOrError(ctx)
    if err != nil {
        return nil, err
    }
    if err := requirePerm(cu, auth.PermVentasListar); err != nil {
        return nil, err
    }
    filters, err := buildListarFilters(in)
    if err != nil {
        return nil, mapAppError(err)
    }
    page, err := h.svc.ListarVentas(ctx, ventasapp.ListarVentasInput{
        Pagination: outbound.ListParams{Cursor: in.Cursor, PageSize: in.Limit},
        Filters:    filters,
    })
    if err != nil {
        return nil, mapAppError(err)
    }
    items := make([]VentaDTO, 0, len(page.Items))
    for _, v := range page.Items {
        items = append(items, toVentaDTO(v, nil))
    }
    return &ListarVentasOutput{Body: ListResponse[VentaDTO]{Items: items, NextCursor: page.NextCursor}}, nil
}
```

---

## DTOs

### Convenciones de tipos en DTOs

| Tipo de dato | DTO Go | Tag Huma |
|---|---|---|
| UUID | `string` | `format:"uuid"` |
| Timestamp | `string` (RFC3339 UTC) | `format:"date-time"` |
| Decimal / dinero | `string` | sin formato especial |
| Enum | `string` | `enum:"A,B,C"` |
| Campo opcional | `*T` | `omitempty` en JSON tag |
| Número entero acotado | `int` | `minimum:"N" maximum:"M"` |
| Booleano opcional | `bool` | — |

**Reglas inquebrantables:**

- Los tipos de dominio **nunca** viajan por el wire — siempre structs DTO.
- Los decimales viajan como `string` para preservar precisión exacta. Serializa con `decimal.StringFixed(scale)`.
- Los timestamps en respuesta siempre salen en UTC con sufijo `Z`: `t.UTC().Format(time.RFC3339Nano)`.
- Los timestamps en request se aceptan en cualquier TZ RFC3339; el handler los convierte a UTC con `.UTC()` antes de cruzarlos al app layer.
- `format:"uuid"` hace que Huma valide el formato antes de que el handler corra.
- Los campos de query `*time.Time` **no funcionan** en Huma — declara `string` y parsea manualmente con `parseTimeField`.

### Input wrapper (Huma lee los tags de este struct)

```go
// ObtenerVentaInput carries the path parameter.
type ObtenerVentaInput struct {
    ID string `path:"id" format:"uuid"`
}

// ObtenerVentaOutput is the response wrapper.
type ObtenerVentaOutput struct {
    Body VentaDTO
}
```

El wrapper Input agrupa todos los orígenes del request: `path:`, `query:`, `header:`, `Body T`.

```go
// CancelarVentaInput carries path param, idempotency header, and body.
type CancelarVentaInput struct {
    ID             string `path:"id"                format:"uuid"`
    IdempotencyKey string `header:"Idempotency-Key" doc:"Idempotency key opcional"`
    Body           CancelarVentaBody
}
```

### Body struct (campos JSON del body)

```go
// CancelarVentaBody is the JSON body for PATCH /v2/ventas/{id}/cancel.
type CancelarVentaBody struct {
    Reason string `json:"reason" minLength:"1" maxLength:"500"`
}
```

**Reglas:**

- Create: incluye todos los campos requeridos + campos inmutables (tipo de venta, id).
- Update/PATCH de header: solo campos mutables; los sub-recursos (productos, vendedores) tienen sus propios endpoints PUT.
- Transiciones de estado sin body: el Input solo tiene `ID` en path y el header de idempotencia.
- Lista: todos los filtros como `query:` en el Input wrapper directamente (sin Body anidado).

### Multipart

```go
// CrearVentaMultipartFields is the typed projection of the multipart body.
type CrearVentaMultipartFields struct {
    Datos  string          `form:"datos"  required:"true"`
    Imagen []huma.FormFile `form:"imagen" contentType:"image/jpeg,image/png,image/gif,image/webp" required:"true"`
}

// CrearVentaInput is the multipart body for POST /v2/ventas.
type CrearVentaInput struct {
    IdempotencyKey string                                             `header:"Idempotency-Key"`
    RawBody        huma.MultipartFormFiles[CrearVentaMultipartFields] `doc:"..."`
}
```

**Advertencias multipart:**

- Usa `required:"false"` en el tag `form:`, nunca `form:"x,omitempty"` — la coma no se soporta.
- Al construir tests, usa `mw.CreatePart(textproto.MIMEHeader{...})` y fija el `Content-Type` de cada part manualmente — `CreateFormFile` no lo hace y la validación de `contentType:` falla.

### Response wrapper genérico de lista

```go
// ListResponse is the generic cursor-paginated envelope.
type ListResponse[T any] struct {
    Items      []T    `json:"items"`
    NextCursor string `json:"next_cursor,omitempty"`
}
```

---

## Parsers locales

Huma no tiene helpers para parsear strings en tipos fuertes; los defines localmente en `handlers_{entity}.go`:

```go
// parseUUIDField parses a string into uuid.UUID with a stable apperror.
func parseUUIDField(raw, name string) (uuid.UUID, error) {
    id, err := uuid.Parse(raw)
    if err != nil {
        return uuid.Nil, apperror.NewValidation(
            "invalid_uuid", "el identificador en la URL no es un UUID válido",
        ).WithField("param", name).WithError(err)
    }
    return id, nil
}

// parseTimeField parses an RFC3339 timestamp into time.Time with a stable apperror.
func parseTimeField(raw, name string) (time.Time, error) {
    t, err := time.Parse(time.RFC3339, raw)
    if err != nil {
        return time.Time{}, apperror.NewValidation(
            "invalid_datetime", "el valor de fecha-hora no es ISO8601 válido",
        ).WithField("field", name).WithError(err)
    }
    return t, nil
}

// parseDecimalField parses a numeric string into decimal.Decimal with a stable apperror.
func parseDecimalField(raw, name string) (decimal.Decimal, error) {
    d, err := decimal.NewFromString(raw)
    if err != nil {
        return decimal.Zero, apperror.NewValidation(
            "invalid_decimal", "el valor numérico no es válido",
        ).WithField("field", name).WithError(err)
    }
    return d, nil
}
```

Para slices con índice: usa un helper `fieldRef` para producir labels tipo `"productos[0].precio_anual"` que el cliente puede correlacionar con su forma.

```go
func fieldRef(arr string, idx int, leaf string) string {
    return arr + "[" + strconv.Itoa(idx) + "]." + leaf
}
```

---

## Auth, permisos y mapeo de errores

Estos tres helpers viven en `auth.go` dentro del paquete `{module}http`:

```go
// currentUserOrError extrae el CurrentUser del contexto plantado por el middleware authn.
// Devuelve 401 si no está presente.
func currentUserOrError(ctx context.Context) (auth.CurrentUser, error) {
    cu, ok := auth.CurrentUserFromContext(ctx)
    if !ok {
        return auth.CurrentUser{}, huma.Error401Unauthorized("no autenticado")
    }
    return cu, nil
}

// requirePerm verifica que el principal tenga todos los permisos en perms.
// Devuelve 403 en el primer código ausente.
func requirePerm(cu auth.CurrentUser, perms ...auth.Permission) error {
    held := make(map[string]struct{}, len(cu.Permisos))
    for _, p := range cu.Permisos {
        held[p] = struct{}{}
    }
    for _, required := range perms {
        if _, ok := held[string(required)]; !ok {
            return huma.NewError(http.StatusForbidden, "permiso denegado",
                &huma.ErrorDetail{
                    Message:  "el principal no tiene el permiso requerido",
                    Location: "header.authorization",
                    Value:    string(required),
                })
        }
    }
    return nil
}

// mapAppError traduce apperror.Error al huma.StatusError equivalente.
// Errores no-apperror caen como 500.
func mapAppError(err error) error {
    if err == nil {
        return nil
    }
    var ae *apperror.Error
    if !errors.As(err, &ae) {
        return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno",
            &huma.ErrorDetail{Message: err.Error()})
    }
    status := ae.Kind.HTTPStatus()
    return huma.NewError(status, ae.Message,
        &huma.ErrorDetail{Message: "code=" + ae.Code})
}
```

**Por qué permisos en el handler y no en middleware Huma:** `depguard` prohíbe que `internal/{module}/infra` importe otro módulo `infra`. El helper `RequirePermission` de `authhttp` vive en `internal/auth/infra/authhttp` — fuera de alcance. El check en el handler mantiene cada módulo autónomo.

---

## Mapper entity → DTO

En `dto_mapper.go`. Las funciones de mapeo son privadas al paquete (minúsculas) y se llaman desde los handlers:

```go
// toVentaDTO projects a domain.Venta into its JSON DTO.
// nombres maps usuario IDs to display names (nil = campos *_nombre vacíos).
func toVentaDTO(v *domain.Venta, nombres map[uuid.UUID]string) VentaDTO {
    a := v.Audit()
    return VentaDTO{
        ID:         v.ID().String(),
        FechaVenta: formatTime(v.FechaVenta()),   // ← UTC con Z
        TipoVenta:  v.TipoVenta().String(),
        // ...
        CreatedAt:  formatTime(a.CreatedAt()),
        UpdatedAt:  formatTime(a.UpdatedAt()),
        CreatedBy:  a.CreatedBy().String(),
    }
}

// formatTime serializa time.Time a RFC3339Nano UTC ("2026-05-13T18:00:00Z").
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// formatTimePtr serializa *time.Time; devuelve nil si el puntero es nil.
func formatTimePtr(t *time.Time) *string {
    if t == nil {
        return nil
    }
    s := formatTime(*t)
    return &s
}
```

Los decimales de dinero usan `StringFixed(moneyScale)` (donde `moneyScale = 2`) para garantizar precisión fija en el wire; los de cantidad usan `StringFixed(cantidadScale)` (4).

---

## Rutas — MountRouter

```go
// internal/{module}/infra/{module}http/routes.go
package {module}http

// MountRouter mounts the {module} module's HTTP routes onto r.
// The supplied chi router must have ALREADY applied authn + idempotency middlewares.
func MountRouter(r chi.Router, svc *{module}app.Service) huma.API {
    config := huma.DefaultConfig("MSP API · {Module}", "v2")
    config.DocsRenderer = huma.DocsRendererScalar
    if config.Components == nil {
        config.Components = &huma.Components{}
    }
    if config.Components.SecuritySchemes == nil {
        config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
    }
    config.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
        Type:         "http",
        Scheme:       "bearer",
        BearerFormat: "JWT",
        Description:  "Token de Firebase ID propagado al backend como Bearer.",
    }
    config.Servers = append(config.Servers, &huma.Server{URL: "/v2"})

    api := humachi.New(r, config)
    handlers := NewHandlers(svc)
    registerOperations(api, handlers)
    return api
}
```

### registerOperations — registro de cada operación

```go
//nolint:funlen // una llamada huma.Register por operación — mechanical wiring.
func registerOperations(api huma.API, h *Handlers) {
    security := []map[string][]string{{"bearerAuth": {}}}
    tags := []string{"{module}"}

    huma.Register(api, huma.Operation{
        OperationID:   "listar-{entidades}",
        Method:        http.MethodGet,
        Path:          "/{entidades}",
        Summary:       "Listar {entidades}",
        Description:   "Devuelve una página cursor-paginada de {entidades}.",
        Tags:          tags,
        Security:      security,
        DefaultStatus: http.StatusOK,
    }, h.Listar{Entidades})

    huma.Register(api, huma.Operation{
        OperationID:   "obtener-{entidad}",
        Method:        http.MethodGet,
        Path:          "/{entidades}/{id}",
        Summary:       "Obtener {entidad}",
        Tags:          tags,
        Security:      security,
        DefaultStatus: http.StatusOK,
    }, h.Obtener{Entidad})

    huma.Register(api, huma.Operation{
        OperationID:   "crear-{entidad}",
        Method:        http.MethodPost,
        Path:          "/{entidades}",
        Summary:       "Crear {entidad}",
        Tags:          tags,
        Security:      security,
        DefaultStatus: http.StatusCreated,
    }, h.Crear{Entidad})

    // Mutaciones idempotentes (PATCH/PUT): DefaultStatus 200.
    // Transiciones de estado sin body: Method POST, path /{entidades}/{id}/{accion}.
    // Deletes sin body de respuesta: DefaultStatus 204.
}
```

**Tabla de operaciones estándar (Tipo A):**

| Método | Path | OperationID | DefaultStatus |
|---|---|---|---|
| GET | `/{entidades}` | `listar-{entidades}` | 200 |
| GET | `/{entidades}/{id}` | `obtener-{entidad}` | 200 |
| POST | `/{entidades}` | `crear-{entidad}` | **201** |
| PATCH | `/{entidades}/{id}` | `actualizar-{entidad}` | 200 |
| PUT | `/{entidades}/{id}/{sub}` | `reemplazar-{sub}-{entidad}` | 200 |
| DELETE | `/{entidades}/{id}` | `eliminar-{entidad}` | **204** |

**Tabla de operaciones estándar (Tipo B — pipeline):**

| Método | Path | OperationID | DefaultStatus |
|---|---|---|---|
| GET | `/{entidades}` | `listar-{entidades}` | 200 |
| GET | `/{entidades}/{id}` | `obtener-{entidad}` | 200 |
| POST | `/{entidades}` | `crear-{entidad}` | **201** |
| POST | `/{entidades}/{id}/{estado}` | `{accion}-{entidad}` | 200 |

---

## Composición en cmd/api/server.go

Los módulos Huma se montan **dentro** de un sub-grupo chi `/v2` que aplica los middlewares de authn e idempotencia **antes** de que Huma tome control. Esto garantiza que en producción el middleware authn rechaza el token antes de que Huma llegue a validar el input:

```go
r.Route("/v2", func(r chi.Router) {
    // auth: rutas anónimas (login) + rutas autenticadas — chi puro
    authhttp.MountRouter(r, authSvc, fb, usuarioRepo, idemStore)

    // módulos Huma: authn + idempotencia aplicados antes de Huma
    r.Group(func(r chi.Router) {
        r.Use(authn.Handler, idem)
        venthttp.MountRouter(r, ventasSvc)
        // {module}http.MountRouter(r, {module}Svc)
    })
})
```

La spec se sirve en `/v2/openapi.json`; la UI Scalar en `/v2/docs`.

---

## Endpoints binarios (imagen GET)

Cuando Huma no es apto (streaming de blobs, `ETag`, `Cache-Control`, `Content-Disposition`), monta la ruta directamente en chi con `http.ResponseWriter` y documéntala en `api/openapi.yaml` a mano:

```go
// En MountRouter, después de registerOperations:
mountObtenerImagen(r, handlers)

func mountObtenerImagen(r chi.Router, h *Handlers) {
    r.Get("/ventas/{id}/imagenes/{img_id}", h.ObtenerImagen)
}
```

Esto aplica solo para endpoints que devuelven binario arbitrario. Todo lo demás usa Huma.

---

## Verificaciones en tiempo de compilación

Agrega estas aserciones al final de `handlers_{entity}.go` para detectar desfases de firma antes del primer test:

```go
var (
    _ func(context.Context, *Crear{Entidad}Input) (*Crear{Entidad}Output, error)   = (*Handlers)(nil).Crear{Entidad}
    _ func(context.Context, *Obtener{Entidad}Input) (*Obtener{Entidad}Output, error) = (*Handlers)(nil).Obtener{Entidad}
    _ func(context.Context, *Listar{Entidades}Input) (*Listar{Entidades}Output, error) = (*Handlers)(nil).Listar{Entidades}
)
```

---

## Test de cobertura de rutas

Agrega un test que comprueba que cada `OperationID` esperado está registrado en la spec generada:

```go
func TestOpenAPI_PathsRegistered(t *testing.T) {
    r := chi.NewRouter()
    api := {module}http.MountRouter(r, fakeSvc())
    spec := api.OpenAPI()
    registered := make(map[string]bool)
    for _, op := range spec.Paths {
        for _, method := range op {
            if method != nil {
                registered[method.OperationID] = true
            }
        }
    }
    for _, want := range []string{
        "listar-{entidades}", "obtener-{entidad}", "crear-{entidad}",
        // ...
    } {
        assert.True(t, registered[want], "operación no registrada: %s", want)
    }
}
```

---

## Agent checklist

- [ ] Paquete nombrado `{module}http` (p. ej. `venthttp`); nunca importado desde dentro del módulo.
- [ ] Struct `Handlers` solo tiene `svc *{module}app.Service` — sin logger, sin repo, sin validator.
- [ ] Todos los handlers siguen la firma Huma: `func(ctx, *XInput) (*XOutput, error)`.
- [ ] Flujo: `currentUserOrError` → `requirePerm` → parse → service → `mapAppError` o return output.
- [ ] Nunca se llama `response.OK/Created` (patrón ancla) — se devuelve `&XOutput{Body: dto}, nil`.
- [ ] `mapAppError` en `auth.go`; cubre todos los `apperror.Kind` incluyendo `KindUnauthorized`.
- [ ] Todos los timestamps en respuesta pasan por `formatTime` (RFC3339Nano UTC con Z).
- [ ] Timestamps en request se parsean con `parseTimeField` y se convierten a UTC con `.UTC()`.
- [ ] Decimales en respuesta usan `StringFixed(scale)` con la escala de la columna Firebird.
- [ ] Campos UUID en DTOs tienen `format:"uuid"` para que Huma valide formato.
- [ ] Query params de fecha declarados como `string` en el Input (no `*time.Time`).
- [ ] Multipart: `required:"false"` en tag `form:`, nunca `form:"x,omitempty"`.
- [ ] `MountRouter` devuelve `huma.API` para que los tests puedan introspectarla.
- [ ] `registerOperations` marcado `//nolint:funlen` con el comentario justificativo.
- [ ] Operaciones DELETE usan `DefaultStatus: http.StatusNoContent`.
- [ ] Operaciones POST de creación usan `DefaultStatus: http.StatusCreated`.
- [ ] Test `TestOpenAPI_PathsRegistered` verifica que todos los `OperationID` esperados están presentes.
- [ ] Aserciones de firma en tiempo de compilación (`var _ func...`) al final del archivo de handlers.
- [ ] Rutas binarias (streaming de blobs) montadas directamente en chi y documentadas en `api/openapi.yaml`.
- [ ] El módulo se monta bajo el sub-grupo chi `/v2` que ya aplica `authn.Handler` e `idem`.
