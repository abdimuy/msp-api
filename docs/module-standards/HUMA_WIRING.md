# HTTP Wiring with Huma over chi

Reference: `internal/ventas/infra/venthttp/` is the first module to use Huma. The `auth` module still uses chi + manual `openapi.yaml` (slated for migration as a follow-up).

## Why Huma

- Auto-generated OpenAPI 3.1 from Go struct tags — no hand-maintained YAML drifting from code.
- Built-in input validation (path, query, header, body) with stable error format.
- Multipart-form decoding via `huma.MultipartFormFiles[T]`.
- Plays nicely with chi via the `humachi` adapter — existing chi middlewares (auth, idempotency) compose unchanged.

## Mount sequence

```go
// internal/ventas/infra/venthttp/routes.go
func MountRouter(r chi.Router, svc *ventasapp.Service) huma.API {
    cfg := huma.DefaultConfig("MSP API · Ventas", "v2")
    cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
        "bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
    }
    api := humachi.New(r, cfg)

    h := NewHandlers(svc)

    huma.Register(api, huma.Operation{
        OperationID:   "crear-venta",
        Method:        http.MethodPost,
        Path:          "/ventas",
        Summary:       "Crear venta",
        Tags:          []string{"ventas"},
        Security:      []map[string][]string{{"bearerAuth": {}}},
        DefaultStatus: http.StatusCreated,
    }, h.CrearVenta)

    // ... register every operation similarly.
    return api
}
```

## Authentication and authorization

chi middlewares are applied at the `/v2` sub-group in `cmd/api/server.go`, OUTSIDE Huma:

```go
r.Route("/v2", func(r chi.Router) {
    authhttp.MountRouter(r, authSvc, ...)
    r.Group(func(r chi.Router) {
        r.Use(authn.Handler, idem)
        venthttp.MountRouter(r, ventasSvc)
    })
})
```

- `authn.Handler` extracts the bearer token, looks up the usuario, plants `auth.CurrentUser` on the request context.
- `idempotency.Middleware` short-circuits non-POST/PATCH internally.

Inside each Huma handler, perform authorization explicitly:

```go
func (h *Handlers) CrearVenta(ctx context.Context, in *CrearVentaRequest) (*CrearVentaResponse, error) {
    cu, err := currentUserOrError(ctx)
    if err != nil { return nil, err }
    if err := requirePerm(cu, authdomain.PermVentasCrear); err != nil { return nil, err }
    // ... call service ...
}
```

Why explicit in-handler perm checks instead of `huma.Middlewares`? depguard forbids `internal/{module}/infra` cross-module imports, and `authhttp.RequirePermission` is in `internal/auth/infra/authhttp`. The in-handler check is the path of least resistance and keeps each module self-contained.

## Error mapping

The `apperror.Error` typed error model maps cleanly to Huma's error helpers. Translate at the handler boundary:

```go
// auth.go
func mapAppError(err error) error {
    appErr, ok := apperror.As(err)
    if !ok {
        return huma.Error500InternalServerError("internal", err)
    }
    switch appErr.Kind {
    case apperror.KindValidation: return huma.Error422UnprocessableEntity(appErr.Message)
    case apperror.KindNotFound:   return huma.Error404NotFound(appErr.Message)
    case apperror.KindConflict:   return huma.Error409Conflict(appErr.Message)
    case apperror.KindForbidden:  return huma.Error403Forbidden(appErr.Message)
    case apperror.KindUnauthorized: return huma.Error401Unauthorized(appErr.Message)
    default: return huma.Error500InternalServerError(appErr.Message)
    }
}
```

## Validation note: order of operations

Huma validates the input struct BEFORE calling the handler. If the request body is missing a required field, the handler never runs and the response is 422. This means:

- **Tests that intend to verify authn/authz must provide a syntactically valid input** (valid body + valid path params), otherwise the request short-circuits at validation. See `internal/ventas/infra/venthttp/security_test.go` for how the authn/authz sweep builds valid bodies per route.
- Production authn happens at the chi middleware layer first, so production never sees this ordering issue — the chi authn middleware shortcuts 401 before Huma's input validation runs.

## DTO conventions

- Decimals travel as `string` in JSON to preserve precision (`decimal.Decimal.String()`). The handler parses `decimal.NewFromString(...)`.
- Timestamps travel as RFC3339Nano strings (`time.RFC3339Nano`).
- UUIDs travel as canonical strings; use `format:"uuid"` on the struct tag so Huma validates.
- Optional fields use `omitempty` on the JSON tag and a pointer Go type.

## Multipart upload

```go
type AdjuntarImagenRequest struct {
    ID      string `path:"id" format:"uuid"`
    RawBody huma.MultipartFormFiles[struct {
        File        huma.FormFile `form:"file" contentType:"image/*"`
        Descripcion string        `form:"descripcion" required:"false"`
    }]
}
```

In tests, build the multipart body with `mw.CreatePart(textproto.MIMEHeader{...})` — Huma reads `Content-Type` from each part's headers, NOT the envelope. `CreateFormFile` does not set the part's Content-Type, so the upload validation will fail.

## OpenAPI introspection

Huma serves the spec at the router root. Mounted under `/v2` in our case, the URL is `/v2/openapi.json`. Add an automated test that asserts every expected operation path is registered (see `TestOpenAPI_PathsRegistered` in `handlers_test.go`).

## What does NOT work

- **Pointer types in query parameters**: Huma does not support `*time.Time` query params. Use `string` and parse in the handler, surfacing a typed `apperror.NewValidation(...)` to keep the response stable.
- **`form:"x,omitempty"`**: the `form` tag does not accept `,omitempty`. Use `required:"false"` instead.
- **Methods spanning Go packages**: see CQRS_PATTERN.md. The Huma handler struct (`Handlers`) lives in the `{module}http` package; all operations on it live there.
