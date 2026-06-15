# Step 09 — Module Wiring

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: Steps 06–08 (infra, HTTP, outbox enqueuer).
> Parallel with: —
> Scope: cómo registrar un módulo completo en el grafo de dependencias de uber/fx,
> montar su router en `/v2` y conectar workers de fondo al ciclo de vida.

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api (uber/fx).**
> Diferencias clave vs. ancla: (1) msp-api **no usa `fx.Module`** — cada módulo expone un
> archivo `{module}_wiring.go` en `cmd/api/` con funciones `provide{Module}X` planas; el grafo
> se construye en `cmd/api/main.go` con una lista única de `fx.Provide`; (2) el enrutado
> HTTP **no vive dentro del módulo** — lo hace `provideRootHandler` en `cmd/api/server.go`,
> donde se monta cada router de módulo bajo `/v2` aplicando los middlewares compartidos de
> autenticación, idempotencia y captura de fallos; (3) los workers de fondo implementan
> `lifecycle.Hooks` (interfaz del paquete `internal/platform/lifecycle`) y se registran con
> `lifecycle.Append`, no con hooks inline; (4) las dependencias cruzadas entre módulos van
> a través de un adaptador en `cmd/api/{consumer}_wiring.go` (no en la infra del módulo
> consumidor), o vía el contrato del módulo productor (`{module}_contracts.go`).

---

## Comparación ancla vs. msp-api (real)

| Aspecto | ancla-api | msp-api (real) |
|---|---|---|
| Unidad de composición | `var Module = fx.Module(...)` por módulo | Funciones `provide{Module}X` planas en `cmd/api/{module}_wiring.go` |
| Registro en el grafo | Referencia al `Module` en `cmd/` | Lista única de `fx.Provide(...)` en `cmd/api/main.go` |
| `fx.As` para interfaces | Usado dentro del `fx.Module` | Usado en el return type de la función `provide` (la función devuelve la interfaz directamente) |
| Enrutado HTTP | Proveído por el módulo | `provideRootHandler` en `cmd/api/server.go` llama a `{module}http.MountRouter` |
| Workers de fondo | `fx.Lifecycle` inline | `lifecycle.Append(lc, "nombre", worker)` via `register{Module}XLifecycle` |
| Dep. cruzadas | Adaptador en `infra/clients/` del módulo consumidor | Adaptador en `cmd/api/{consumer}_wiring.go` (un `type` + `provide` locales) |
| Alias de imports | No requerido explícitamente | `importas.alias` en `.golangci.yml`; `no-extra-aliases: true` es hard |

---

## Archivos a crear / editar

```
cmd/api/{module}_wiring.go            ← funciones provide{Module}X para este módulo
cmd/api/main.go                       ← agregar cada provide{Module}X a fx.Provide(...)
cmd/api/server.go                     ← montar {module}http.MountRouter en provideRootHandler
.golangci.yml                         ← registrar aliases del módulo en importas.alias
```

Sin `{module}_module.go` dentro de `internal/{module}/` — en msp-api la composición
vive íntegramente en `cmd/api/`.

---

## 1. El archivo de wiring del módulo

Crea `cmd/api/{module}_wiring.go`. El encabezado es package `main` y lleva la directiva
`//nolint:misspell` cuando el vocabulario del módulo es español.

```go
//nolint:misspell // {module} vocabulary is Spanish per project convention.
package main

import (
    {module}app  "github.com/abdimuy/msp-api/internal/{module}/app"
    "{module}fb" "github.com/abdimuy/msp-api/internal/{module}/infra/{module}fb"
    "{module}outbox" "github.com/abdimuy/msp-api/internal/{module}/infra/{module}outbox"
    {module}outbound "github.com/abdimuy/msp-api/internal/{module}/ports/outbound"
    "github.com/abdimuy/msp-api/internal/platform/firebird"
)
```

### Regla principal: una función `provide` por implementación de puerto

Cada función:
- Recibe dependencias como parámetros (fx las resuelve).
- Devuelve **la interfaz del puerto** (no el tipo concreto), excepto cuando el
  consumidor necesita el tipo concreto (p. ej. para pasarlo a un janitor).
- Lleva un comentario de una línea que explica qué construye y por qué.

```go
// provide{Module}Repo builds the Firebird-backed {Entity}Repo.
func provide{Module}Repo(p *firebird.Pool) {module}outbound.{Entity}Repo {
    return {module}fb.New{Entity}Repo(p)
}

// provide{Module}Clock returns the production clock used by {module}.
func provide{Module}Clock() {module}outbound.Clock {
    return {module}outbound.ProductionClock{}
}

// provide{Module}OutboxEnqueuer builds the {module}-module wrapper around the
// platform outbox. Backed by Firebird per ADR-0008.
func provide{Module}OutboxEnqueuer(p *firebird.Pool) {module}outbound.OutboxEnqueuer {
    return {module}outbox.NewEnqueuer(p)
}

// provide{Module}Service assembles the {module} application service.
func provide{Module}Service(
    repo   {module}outbound.{Entity}Repo,
    clock  {module}outbound.Clock,
    outbox {module}outbound.OutboxEnqueuer,
    txMgr  *firebird.TxManager,
) *{module}app.Service {
    return {module}app.NewService(repo, clock, outbox, txMgr)
}
```

Ejemplo real (módulo `auth`, `auth_wiring.go`):

```go
func provideAuthUsuarioRepo(p *firebird.Pool) outbound.UsuarioRepo {
    return authfb.NewUsuarioRepo(p)
}

func provideAuthClock() outbound.Clock { return outbound.ProductionClock{} }

func provideAuthOutboxEnqueuer(p *firebird.Pool) outbound.OutboxEnqueuer {
    return authoutbox.NewEnqueuer(p)
}

func provideAuthService(
    usuarios outbound.UsuarioRepo,
    roles    outbound.RolRepo,
    permisos outbound.PermisoRepo,
    clock    outbound.Clock,
    outboxEnq outbound.OutboxEnqueuer,
    fb       outbound.FirebaseClient,
    fbTxMgr  *firebird.TxManager,
    nombreResolver outbound.NombreResolver,
) *app.Service {
    return app.NewService(usuarios, roles, permisos, clock, outboxEnq, fb, fbTxMgr).
        WithNombreResolver(nombreResolver)
}
```

---

## 2. Registro en `cmd/api/main.go`

Añade cada función `provide{Module}X` a la lista `fx.Provide(...)` en `appOptions()`.
**Nunca construyas dependencias manualmente** — todo pasa por `fx.Provide`.

```go
func appOptions() []fx.Option {
    return []fx.Option{
        fx.Provide(
            // ... proveedores existentes ...
            provide{Module}Repo,
            provide{Module}Clock,
            provide{Module}OutboxEnqueuer,
            provide{Module}Service,
        ),
        fx.Invoke(
            // ... hooks existentes ...
            register{Module}OutboxHandlers,   // si el módulo registra handlers
            register{Module}WorkerLifecycle,  // si tiene worker de fondo
        ),
        fx.NopLogger,
    }
}
```

Ejemplo real — extracto de `main.go` para el módulo `ventas`:

```go
provideVentasRepo,
provideVentasClienteChecker,
provideVentasUsuarioChecker,
provideVentasStorage,
provideVentasClock,
provideVentasOutboxEnqueuer,
provideVentasEventReader,
provideVentasUsuarioResolver,
provideVentasAlmacenResolver,
provideVentasImageProcessor,
provideVentasAplicarConfig,
provideVentasMicrosipWriter,
provideVentasMicrosipClienteWriter,
provideVentasService,
```

---

## 3. Montar el router en `cmd/api/server.go`

Todos los routers de módulo se montan dentro de `provideRootHandler`, bajo el bloque
`r.Route("/v2", ...)`. Hay cuatro patrones según los middlewares que necesita el módulo:

### 3a. Módulo con autenticación + idempotencia + captura de fallos (patrón `ventas`)

Para endpoints de escritura que necesitan los tres middlewares compartidos:

```go
r.Group(func(r chi.Router) {
    // capture envuelve idem: captura 409/400 de idempotencia antes de que
    // idem responda con Idempotent-Replay. authn usa skipAuthForPublicDocs
    // para que /v2/docs y /v2/openapi bypass la autenticación.
    r.Use(skipAuthForPublicDocs(authn.Handler), capture, idem)
    {module}http.MountRouter(r, {module}Svc)
})
```

### 3b. Módulo de solo lectura con autenticación (patrón `microsip`, `inventario`)

Sin idempotencia (sin escrituras) ni captura de fallos:

```go
r.Group(func(r chi.Router) {
    r.Use(skipAuthForPublicDocs(authn.Handler))
    {module}http.MountRouter(r, {module}Svc)
})
```

### 3c. Módulo con subrutas admin + usuario (patrón `cobranza`)

Cuando el módulo expone rutas de lectura, rutas admin y rutas de usuario propias:

```go
r.Route("/{module}", func(r chi.Router) {
    r.Use(authn.Handler)
    {module}http.MountReadRouter(r, {module}Svc)
})

r.Route("/_admin/{module}", func(r chi.Router) {
    r.Use(authn.Handler)
    {module}http.MountAdminRouter(r, {module}Svc)
})
```

### 3d. Añadir el servicio a la firma de `provideRootHandler`

Cada módulo nuevo que monta un router añade su `*{module}app.Service` (u otros
ports que el handler necesite) como parámetro de `provideRootHandler`. fx los
resuelve automáticamente.

```go
func provideRootHandler(
    // ... parámetros existentes ...
    {module}Svc *{module}app.Service,
    // ...
) RootHandler { ... }
```

---

## 4. Middlewares compartidos — cómo se construyen

Los middlewares `authn`, `idem` y `capture` se construyen **una sola vez** dentro
de `provideRootHandler` y se comparten entre todos los grupos que los usan:

```go
// Autenticación — Firebase token + lookup usuario en MSP_USUARIOS.
authn := authhttp.NewAuthnMiddleware(authFirebase, authUsuarios, authSvc)

// Idempotencia — almacena hash de body en MSP_IDEMPOTENCY_KEYS; TTL 24h.
idem := idempotency.Middleware(idempotency.Config{
    Store:      idemStore,
    TTL:        24 * time.Hour,
    Methods:    []string{http.MethodPost, http.MethodPatch},
    RequireKey: false,
})

// Captura de fallos — persiste requests fallidos en MSP_FAILED_INTENTS.
capture := failedintent.CaptureMiddleware(fiCaptureCfg)
```

Los endpoints de plataforma (`/healthz`, `/readyz`, `/version`) viven **fuera** de
`/v2` y no pasan por ninguno de estos middlewares.

---

## 5. Workers de fondo — patrón `lifecycle.Append`

Para workers con ticker (reconciliadores, janitors, retry workers), la secuencia es:

**a) El worker implementa `lifecycle.Hooks`** — es decir, tiene `Start(context.Context) error`
y `Stop(context.Context) error`.

**b) Un `provide{Module}XWorker` lo construye** como proveedor plano.

**c) Un `register{Module}XLifecycle` lo conecta al ciclo de vida** con `lifecycle.Append`:

```go
func register{Module}XLifecycle(lc fx.Lifecycle, w *{module}app.XWorker) {
    lifecycle.Append(lc, "{module}-x-worker", w)
}
```

`lifecycle.Append` (en `internal/platform/lifecycle/lifecycle.go`) envuelve
`OnStart`/`OnStop` con logs de `slog` y los registra en `lc`.

Ejemplo real — worker de reintento de pagos en cobranza (`cobranza_wiring.go`):

```go
func provideCobranzaPagoRetryWorker(
    svc    *cobranzaapp.Service,
    repo   cobranzaoutbound.PagosRecibidosRepo,
    clock  cobranzaoutbound.Clock,
    logger *slog.Logger,
) *cobranzaapp.PagoRetryWorker {
    return cobranzaapp.NewPagoRetryWorker(svc, repo, clock, cobranzaapp.PagoRetryWorkerConfig{}, logger)
}

func registerCobranzaPagoRetryWorkerLifecycle(lc fx.Lifecycle, w *cobranzaapp.PagoRetryWorker) {
    lifecycle.Append(lc, "pago-retry-worker", w)
}
```

Ejemplo real — janitor con ticker inline para fuentes de eventos (`cobranza_wiring.go`):

```go
func registerCobranzaFbEventSourceLifecycle(lc fx.Lifecycle, src cobranzaoutbound.FbEventSource) {
    lc.Append(fx.Hook{
        OnStop: func(_ context.Context) error {
            return src.Close()
        },
    })
}
```

> **Orden de LIFO en OnStop**: fx detiene los hooks en orden inverso a su registro.
> Si el listener depende de la fuente de eventos, `registerCobranzaFbEventSourceLifecycle`
> debe invokearse **antes** que `registerCobranzaFbEventListenerLifecycle` en la lista
> `fx.Invoke`, de modo que el listener (registrado después) se detenga primero.

Para el worker de analytics que vendrá: el patrón es idéntico — un ticker en
`Start` que lanza la goroutine, y un `context.CancelFunc` en `Stop` que la detiene.

---

## 6. Dependencias cruzadas entre módulos

Cuando el módulo A necesita datos del módulo B, la dependencia **nunca** cruza
directamente hacia `internal/B/domain` o `internal/B/app` — depguard lo prohíbe.
En su lugar:

**Paso 1 — Puerto en A.** Define la interfaz que A necesita en
`internal/A/ports/outbound/`:

```go
// internal/A/ports/outbound/b_client.go
type BClient interface {
    GetEntidad(ctx context.Context, id uuid.UUID) (*A.BContract, error)
}
```

**Paso 2 — Adaptador en `cmd/api/A_wiring.go`.** Implementa el adaptador que
traduce entre el contrato del módulo B y el puerto de A. En msp-api el adaptador
vive en `cmd/api/` (o raramente como `type` local en el wiring), no en
`internal/A/infra/clients/`:

```go
// cmd/api/{consumer}_wiring.go
type {consumer}{Producer}Adapter struct {
    inner {producer}.TraspasoService // contrato expuesto por {producer}_contracts.go
}

func (a *{consumer}{Producer}Adapter) GetEntidad(ctx context.Context, id uuid.UUID) (*{consumer}outbound.BContract, error) {
    // ... delega + mapea tipos ...
}

func provide{Consumer}{Producer}Adapter(svc {producer}.TraspasoService) {consumer}outbound.BClient {
    return &{consumer}{Producer}Adapter{inner: svc}
}
```

Ejemplo real — módulo `ventas` consume `inventario` (`inventario_wiring.go`):

```go
type ventasInventarioAdapter struct {
    inv            inventario.TraspasoService
    almacenDestino int
}

func provideVentasInventarioAdapter(
    inv inventario.TraspasoService,
    cfg *config.Config,
) ventasoutbound.InventarioService {
    return &ventasInventarioAdapter{
        inv:            inv,
        almacenDestino: cfg.Inventario.AlmacenDestinoVentasID,
    }
}
```

El módulo productor expone `{module}_contracts.go` con la interfaz pública;
el adaptador en `cmd/api/` traduce sin romper la frontera de paquetes.

---

## 7. Alias de imports en `.golangci.yml`

Con `no-extra-aliases: true`, el linter rechaza cualquier alias no registrado.
Para cada paquete que requiera alias (colisión de nombre con stdlib u otro módulo),
añade una entrada en la sección `importas.alias` de `.golangci.yml`:

```yaml
importas:
  no-extra-aliases: true
  alias:
    # ... entradas existentes ...
    - pkg: github.com/abdimuy/msp-api/internal/{module}/app
      alias: {module}app
    - pkg: github.com/abdimuy/msp-api/internal/{module}/ports/outbound
      alias: {module}outbound
    - pkg: github.com/abdimuy/msp-api/internal/{module}/infra/{module}fb
      alias: {module}fb          # si colisiona; omitir si no hay colisión
```

Entradas reales registradas hoy:

| Paquete | Alias |
|---|---|
| `internal/platform/apperror` | `apperror` |
| `internal/platform/idempotency/firebird` | `idempotencyfb` |
| `internal/auth/ports/outbound` | `authoutbound` |
| `internal/auth/infra/firebird` | `authfb` |
| `internal/auth/app` | `authapp` |
| `internal/ventas/app` | `ventasapp` |
| `internal/ventas/ports/outbound` | `ventasoutbound` |
| `internal/cobranza/app` | `cobranzaapp` |
| `internal/cobranza/infra/ventfb` | `cobranzaventfb` |
| `internal/cobranza/ports/outbound` | `cobranzaoutbound` |
| `internal/inventario/app` | `inventarioapp` |
| `internal/inventario/ports/outbound` | `inventariooutbound` |

Nunca uses alias no registrados aunque el compilador los acepte — el linter fallará
en `pre-push`.

---

## 8. Infraestructura compartida de plataforma

Los providers de plataforma se registran directamente en `appOptions()` sin
archivo `_wiring.go` propio porque son singleton de aplicación, no pertenecen
a un módulo de dominio:

| Función | Qué construye |
|---|---|
| `provideFirebirdPool` | `*firebird.Pool` |
| `provideFirebirdTxManager` | `*firebird.TxManager` |
| `provideOutboxRegistry` | `*outboxfb.HandlerRegistry` |
| `provideOutboxDispatcher` | `*outboxfb.Dispatcher` |
| `provideIdempotencyStore` | `idempotency.Store` |
| `provideIdempotencyJanitor` | `*idempotencyfb.Janitor` |
| `provideHealthService` | `*healthcheck.Service` |

Sus lifecycle hooks:

```go
fx.Invoke(
    registerFirebirdLifecycle,       // lifecycle.Append(lc, "firebird", p)
    registerOutboxLifecycle,         // lifecycle.Append(lc, "outboxfb-dispatcher", d)
    registerIdempotencyJanitorLifecycle,
    registerHTTPLifecycle,           // OnStart: srv.Serve; OnStop: srv.Shutdown
)
```

---

## Agent checklist

- [ ] `cmd/api/{module}_wiring.go` existe; package `main`; `//nolint:misspell` si el módulo es en español.
- [ ] Una función `provide{Module}X` por implementación de puerto; cada una devuelve la interfaz del puerto.
- [ ] Cada `provide{Module}X` está en la lista `fx.Provide(...)` de `appOptions()` en `main.go`.
- [ ] El router del módulo se monta en `provideRootHandler` (`server.go`) bajo `/v2`, con el patrón de middlewares correcto (authn + idem + capture / solo authn / solo admin).
- [ ] `provideRootHandler` incluye el `*{module}app.Service` (u otros tipos necesarios) en su firma.
- [ ] Si el módulo tiene worker de fondo: `provide{Module}XWorker` + `register{Module}XLifecycle` que llama a `lifecycle.Append`.
- [ ] Orden de `fx.Invoke` respeta LIFO para shutdown de componentes dependientes.
- [ ] Dependencias cruzadas van por el contrato del módulo productor + adaptador en `cmd/api/`, nunca por `internal/{producer}/domain` o `/app` directamente.
- [ ] Aliases de imports del módulo registrados en `.golangci.yml` `importas.alias`; sin alias no registrados.
- [ ] `golangci-lint run ./...` pasa sin errores de `importas`, `depguard`, ni `misspell`.
- [ ] `go build ./...` y `go vet ./...` limpios antes de hacer push.
