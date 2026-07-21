# Reactivación R7 · Fase 2 — Canal (Pieza B, enviador falso) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construir la maquinaria del canal de reactivación en `internal/reactivacion/` — encolar la cohorte
de tratamiento, "enviar" mensajes por un `MessageSender` (con un `FakeSender` ahora), gobernar el ritmo
anti-baneo, marcar `FUE_CONTACTADO`, y registrar todo — sin dependencia de un número de WhatsApp.

**Architecture:** Vertical slice hexagonal dentro del módulo `reactivacion` ya existente (Fase 1). Se agregan:
un entity `Mensaje` con máquina de estados, puertos `MessageSender`/`MensajeRepo`, un `Gobernador` (lógica
pura), un `Opener` (plantillas por segmento), un `EnvioService` + `EnvioWorker`, la infra Firebird para
`MSP_RX_MENSAJES`, un `FakeSender` + stub `WhatsmeowSender`, endpoints Huma y wiring fx. **Solo backend.**

**Tech Stack:** Go, Firebird (nakagami/firebirdsql), Huma v2 + chi, fx, uuid, shopspring/decimal (no aplica
aquí), caarlos0/env (config tags), testify.

**Diseño aprobado:** `docs/superpowers/specs/2026-07-20-reactivacion-r7-fase2-canal-design.md`.

**Módulo de referencia (calcar EXACTAMENTE):** la Fase 1 — `internal/reactivacion/` (domain/app/ports/infra)
y su plan `docs/superpowers/plans/2026-06-26-...`/este mismo repo. Para CADA pieza de boilerplate el patrón
literal ya existe en Fase 1; se indica el archivo exacto a calcar.

## Global Constraints

- **CLAUDE.md §1 — sin lógica en la DB:** IDs con `uuid.New()`, timestamps con `time.Now()` en Go, escritos
  con `firebird.ToWallClock(t)`; leídos con `firebird.ScanUTCTime`. Sin DEFAULT/TRIGGER/PROCEDURE en la
  migración. Todo `INSERT`/`UPDATE` pasa ID/timestamps explícitos.
- **CLAUDE.md §3 — idioma:** código, identificadores y códigos de error en inglés (snake_case en códigos);
  mensajes de usuario en español, minúscula, sin punto final. `apperror.New*(codigo_ingles, "mensaje español")`.
- **CLAUDE.md §2 — vertical slice:** cross-module solo vía contratos; nada nuevo cruza a otros módulos aquí.
- **UTF-8** en columnas `MSP_*`. Texto de Microsip se lee string plano (NO `firebird.Win1252`) — pero aquí no
  se lee texto de Microsip (el teléfono ya viene en `MSP_RX_COHORTE`).
- **Upserts/updates** dentro de `firebird.RunInTx`; UPDATE-luego-INSERT (bug MERGE del driver) cuando aplique.
- **Sin `Math.random`/`Date.now()` prohibiciones** aplican al entorno de workflows JS, NO a Go. En Go, el
  jitter usa un `*rand.Rand` con semilla inyectada para tests deterministas.
- **Gates de cobertura:** domain ≥99%, app ≥90%, infra ≥80%. Tests infra Firebird envueltos en
  `fbtestutil.WithTestTransaction` (rollback; SKIP sin `FB_DATABASE`). Datos MX realistas
  (`@muebleriamsp.mx`, nombres en español; ver memoria `feedback_realistic_test_data`).
- **Calidad:** `golangci-lint run ./...` = 0 issues; `go test -race -short ./...` verde; cross-build
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/api`. Sin `--no-verify`. Commits convencionales
  **sin** atribución a Claude (memoria `feedback_no_claude_attribution`).
- **Rama:** `main` (Fase 1 ya está mergeada aquí en local, commit `798e204`). Crear rama de trabajo
  `feat/reactivacion-r7-fase2` antes de tocar código.
- **Gofumpt gotcha:** el gofumpt de golangci combina parámetros consecutivos del mismo tipo
  (`a, b string`, no `a string, b string`) y no tolera alineación extra en structs. Correr `gofumpt -w` y
  además arreglar a mano si el lint de golangci sigue marcando (pasó en Fase 1).
- **importas:** cualquier alias de import nuevo debe registrarse en `.golangci.yml` (los alias de
  `reactivacion*` ya están registrados desde Fase 1: `reactivacionapp`, `reactivaciondomain`,
  `reactivacionoutbound`, `reactivacionfb`, `reactivacionhttp`).

---

### Task 1: Migración `MSP_RX_MENSAJES` (000045)

**Files:**
- Create: `migrations-firebird/000045_create_msp_rx_mensajes.up.sql`
- Create: `migrations-firebird/000045_create_msp_rx_mensajes.down.sql`

**Patrón a calcar:** `migrations-firebird/000044_create_msp_rx_cohorte.up.sql` (creado en Fase 1) — mismo
estilo de comentario, `INSERT INTO MSP_MIGRATIONS (45, ...)`, `COMMIT;`, y el `.down` con `DROP TABLE` +
`DELETE FROM MSP_MIGRATIONS WHERE ID = 45`.

**Interfaces — columnas (spec §5):**

```
ID           CHAR(36)      CHARACTER SET ASCII  NOT NULL   -- PK, uuid desde Go
CLIENTE_ID   INTEGER                            NOT NULL
SEGMENTO     VARCHAR(24)   CHARACTER SET ASCII  NOT NULL   -- recien_liquidado | por_liquidar_hueco
TELEFONO     VARCHAR(40)                        NOT NULL
CUERPO       BLOB SUB_TYPE TEXT CHARACTER SET UTF8 NOT NULL
ESTADO       VARCHAR(16)   CHARACTER SET ASCII  NOT NULL   -- encolado|enviado|fallido|bloqueado
SENDER_KIND  VARCHAR(12)   CHARACTER SET ASCII             -- simulado|real; NULL hasta enviar
ENCOLADO_EN  TIMESTAMP                          NOT NULL
ENVIADO_EN   TIMESTAMP                                     -- NULL hasta enviar
ERROR        VARCHAR(500)                                  -- motivo de fallido/bloqueado
CREATED_AT   TIMESTAMP                          NOT NULL
UPDATED_AT   TIMESTAMP                          NOT NULL
CONSTRAINT PK_MSP_RX_MENSAJES PRIMARY KEY (ID)
```

**NO** hay UNIQUE en `CLIENTE_ID` (un cliente puede tener varias filas: el esquema admite hasta 2 toques; la
idempotencia del encolado se maneja en Go). Índices: `IDX_MSP_RX_MENSAJES_ESTADO (ESTADO)`,
`IDX_MSP_RX_MENSAJES_CLIENTE (CLIENTE_ID)`, `IDX_MSP_RX_MENSAJES_ENVIADO (ENVIADO_EN)` (para el tope diario).

- [ ] **Step 1:** Escribir `000045_create_msp_rx_mensajes.up.sql` calcando 000044 (comentario "por qué",
  CREATE TABLE con las columnas de arriba, los 3 índices, `INSERT INTO MSP_MIGRATIONS (45,
  '000045_create_msp_rx_mensajes', CURRENT_TIMESTAMP);`, `COMMIT;`).
- [ ] **Step 2:** Escribir `000045_..._down.sql` (`DROP TABLE MSP_RX_MENSAJES; COMMIT; DELETE FROM
  MSP_MIGRATIONS WHERE ID = 45; COMMIT;`).
- [ ] **Step 3:** Aplicar a la DB dev: `make fb-migrate-up` (idempotente). **Antes:** `make fb-snapshot
  NAME=pre-rx-mensajes-000045`. Verificar en el output `▶ ...000045... (id=45)` y `✔ Firebird migrations
  applied`.
- [ ] **Step 4:** Commit. `git add migrations-firebird/000045_*` ; `git commit -m "feat(reactivacion): migración MSP_RX_MENSAJES (fase 2)"`.

---

### Task 2: Domain — `EstadoMensaje`, `SenderKind`, `Mensaje`

**Files:**
- Create: `internal/reactivacion/domain/estado_mensaje.go`
- Create: `internal/reactivacion/domain/mensaje.go`
- Modify: `internal/reactivacion/domain/errors.go` (agregar sentinels)
- Test: `internal/reactivacion/domain/estado_mensaje_test.go`, `internal/reactivacion/domain/mensaje_test.go`

**Patrón a calcar:** `internal/reactivacion/domain/segmento.go` (VO con constantes + `Valido()` + `Parse*`) y
`internal/reactivacion/domain/cohorte_cliente.go` (entity: campos privados, `Crear*`/`Hydrate*`, getters,
`audit.Timestamped`).

**Interfaces — Produces (firmas exactas que Task 3/6/7 consumen):**

```go
// estado_mensaje.go
type EstadoMensaje string
const (
    EstadoEncolado  EstadoMensaje = "encolado"
    EstadoEnviado   EstadoMensaje = "enviado"
    EstadoFallido   EstadoMensaje = "fallido"
    EstadoBloqueado EstadoMensaje = "bloqueado"
)
func (e EstadoMensaje) String() string
func (e EstadoMensaje) Valido() bool
func ParseEstadoMensaje(raw string) (EstadoMensaje, error) // ErrEstadoMensajeInvalido

type SenderKind string
const (
    SenderSimulado SenderKind = "simulado"
    SenderReal     SenderKind = "real"
)
func (s SenderKind) String() string
func (s SenderKind) Valido() bool
func ParseSenderKind(raw string) (SenderKind, error) // ErrSenderKindInvalido

// mensaje.go
type Mensaje struct { /* campos privados */ }

type CrearMensajeParams struct {
    ClienteID   int
    Segmento    Segmento
    Telefono    string
    Cuerpo      string
    Now         time.Time
}
// Valida ClienteID>0, Segmento.Valido(), Telefono!="", Cuerpo!="". Genera uuid.New().
// Estado inicial = EstadoEncolado; SenderKind = "" (vacío hasta enviar); EncoladoEn = Now;
// EnviadoEn = zero; timestamps = audit.NewTimestamped(Now).
func CrearMensaje(p CrearMensajeParams) (*Mensaje, error)

type HydrateMensajeParams struct {
    ID uuid.UUID; ClienteID int; Segmento Segmento; Telefono, Cuerpo string
    Estado EstadoMensaje; SenderKind SenderKind; EncoladoEn, EnviadoEn time.Time
    Error string; CreatedAt, UpdatedAt time.Time
}
func HydrateMensaje(p HydrateMensajeParams) *Mensaje

// Transiciones (mutan estado + timestamps.MarkUpdated(now)):
func (m *Mensaje) MarcarEnviado(kind SenderKind, now time.Time) error   // solo desde EstadoEncolado; set Estado=Enviado, SenderKind=kind, EnviadoEn=now
func (m *Mensaje) MarcarFallido(motivo string, now time.Time)           // set Estado=Fallido, Error=motivo
func (m *Mensaje) MarcarBloqueado(motivo string, now time.Time)         // set Estado=Bloqueado, Error=motivo

// Getters: ID() uuid.UUID, ClienteID() int, Segmento() Segmento, Telefono() string, Cuerpo() string,
//   Estado() EstadoMensaje, SenderKind() SenderKind, EncoladoEn() time.Time, EnviadoEn() time.Time,
//   Error() string, CreatedAt() time.Time, UpdatedAt() time.Time
```

**Nota `audit.Timestamped`:** `audit.NewTimestamped(now)`, `audit.HydrateTimestamped(created, updated)`,
`.CreatedAt()`, `.UpdatedAt()`. Para `MarkUpdated`, revisar `internal/platform/audit/audit.go` — si
`Timestamped` no expone un mutador, guardar `updatedAt time.Time` propio y setearlo en las transiciones
(como haga falta; `cohorte_cliente.go` solo lee timestamps, así que revisar el API disponible y usar el
mutador que exista o reconstruir con `HydrateTimestamped(created, now)`).

**Errores (errors.go):** agregar `ErrEstadoMensajeInvalido`, `ErrSenderKindInvalido`,
`ErrMensajeClienteIDInvalido`, `ErrMensajeTelefonoRequerido`, `ErrMensajeCuerpoRequerido`,
`ErrMensajeTransicionInvalida` (para `MarcarEnviado` desde un estado != encolado) — todos con
`apperror.NewValidation("reactivacion_...", "mensaje español")`, mismo estilo que los existentes.

- [ ] **Step 1:** Escribir `estado_mensaje_test.go` (black-box `package domain_test`): `String`, `Valido`
  (válidos + vacío + basura), `Parse*` válidos e inválidos → `ErrEstadoMensajeInvalido`/`ErrSenderKindInvalido`.
- [ ] **Step 2:** Escribir `estado_mensaje.go` para pasar.
- [ ] **Step 3:** Escribir `mensaje_test.go`: `CrearMensaje` éxito (estado encolado, senderKind vacío, uuid
  no nil, timestamps), cada invariante (ClienteID≤0, segmento inválido, teléfono vacío, cuerpo vacío) →
  su error; `MarcarEnviado` desde encolado (estado=enviado, kind, enviadoEn) y desde no-encolado →
  `ErrMensajeTransicionInvalida`; `MarcarFallido`/`MarcarBloqueado` (estado + error); `HydrateMensaje`
  round-trip de todos los campos.
- [ ] **Step 4:** Escribir `mensaje.go` + agregar sentinels a `errors.go` para pasar.
- [ ] **Step 5:** `go test ./internal/reactivacion/domain/...` verde; cobertura domain ≥99%
  (`go test -cover`). Commit: `feat(reactivacion): domain Mensaje + estados (fase 2)`.

---

### Task 3: Ports — `MessageSender`, `MensajeRepo`, y extensión `CohorteRepo.MarcarContactado`

**Files:**
- Create: `internal/reactivacion/ports/outbound/sender.go`
- Create: `internal/reactivacion/ports/outbound/mensaje_repo.go`
- Modify: `internal/reactivacion/ports/outbound/cohorte_repo.go` (agregar 2 métodos)

**Patrón a calcar:** `internal/reactivacion/ports/outbound/cohorte_repo.go` y `.../universo.go` (interfaces
+ structs planos + doc).

**Interfaces — Produces:**

```go
// sender.go
type Destino struct { ClienteID int; Telefono string }
type MessageSender interface {
    // Enviar entrega cuerpo a dest. Devuelve error si el canal rechaza el mensaje.
    Enviar(ctx context.Context, dest Destino, cuerpo string) error
    // Kind identifica al enviador para la integridad de la medición.
    Kind() domain.SenderKind
}

// mensaje_repo.go
type ListarMensajesParams struct {
    Estado   domain.EstadoMensaje // "" = sin filtro
    Segmento domain.Segmento      // "" = sin filtro
    Limit    int                  // 0 = sin cap explícito
}
type MensajeRepo interface {
    Insertar(ctx context.Context, mensajes []*domain.Mensaje) error          // bulk (encolar)
    ListarPendientes(ctx context.Context, limit int) ([]*domain.Mensaje, error) // ESTADO='encolado', ENCOLADO_EN ASC
    Actualizar(ctx context.Context, m *domain.Mensaje) error                 // UPDATE por ID: estado, sender_kind, enviado_en, error, updated_at
    Listar(ctx context.Context, p ListarMensajesParams) ([]*domain.Mensaje, error)
    ContarEnviadosHoy(ctx context.Context, desde time.Time) (int, error)     // COUNT ESTADO='enviado' AND ENVIADO_EN >= desde
    ClientesConMensaje(ctx context.Context) (map[int]bool, error)            // DISTINCT CLIENTE_ID → true (idempotencia de encolado)
}

// cohorte_repo.go — AGREGAR a la interfaz CohorteRepo existente:
    // MarcarContactado pone FUE_CONTACTADO=1 para clienteID con UPDATED_AT=now (UPDATE puntual; NO toca
    // EN_CONTROL/COHORTE_FECHA). El service pasa `now` (CLAUDE.md §1: timestamps desde Go).
    MarcarContactado(ctx context.Context, clienteID int, now time.Time) error
```

- [ ] **Step 1:** Escribir `sender.go` y `mensaje_repo.go` con las firmas de arriba + doc-comments en inglés.
- [ ] **Step 2:** Agregar `MarcarContactado` a la interfaz `CohorteRepo` en `cohorte_repo.go`.
- [ ] **Step 3:** `go build ./internal/reactivacion/ports/...` (no compila aún el infra — está bien; el build
  del módulo fallará hasta Task 7 implemente los nuevos métodos; NO commitear roto: incluir esta task junto a
  Task 7 en el mismo commit, o commitear solo cuando el módulo compile). **Decisión:** commitear al final de
  Task 7 (infra) junto con los ports, para no dejar el build roto. Marcar este step como "diferir commit".

---

### Task 4: App — `Opener` (plantillas por segmento)

**Files:**
- Create: `internal/reactivacion/app/opener.go`
- Test: `internal/reactivacion/app/opener_test.go`

**Interfaces — Produces:**

```go
// Opener genera el cuerpo del mensaje inicial por segmento. Punto de reemplazo del copiloto (Fase 3):
// misma firma, otra implementación.
type Opener struct{}
func NewOpener() Opener
// Generar devuelve el cuerpo para un cliente de la cohorte. nombre puede venir vacío (se usa un saludo
// genérico). Nunca devuelve cadena vacía.
func (Opener) Generar(seg domain.Segmento, nombre string) (string, error) // ErrSegmentoInvalido si seg no es válido
```

**Contenido (español neutro, corto, tono cercano; sin montos):**
- `SegmentoRecienLiquidado`: felicitación por terminar de pagar + invitación a la siguiente compra con
  beneficio. Ej: `"Hola {nombre}, le saluda Mueblería MSP. ¡Felicidades por completar su pago! Tenemos algo
  especial para usted en su próxima compra. ¿Le comparto opciones?"`
- `SegmentoPorLiquidarHueco`: con tacto, completar el juego con un pago que cabe. Ej: `"Hola {nombre}, le
  saluda Mueblería MSP. Ya casi termina de pagar su compra. ¿Le gustaría completar su juego con un pago que
  se acomode a usted?"`
- `{nombre}` vacío → usar `"¿Cómo está?"` en lugar de `"Hola {nombre},"` (helper `saludo(nombre)`).

- [ ] **Step 1:** `opener_test.go`: `Generar` para cada segmento (no vacío, contiene nombre cuando se pasa),
  nombre vacío (saludo genérico, no `"Hola ,"`), segmento inválido → `ErrSegmentoInvalido`.
- [ ] **Step 2:** `opener.go` con las plantillas (constantes) + interpolación.
- [ ] **Step 3:** `go test ./internal/reactivacion/app/... -run Opener` verde. Commit al final de Task 6.

---

### Task 5: App — `Gobernador` (anti-baneo, lógica pura)

**Files:**
- Create: `internal/reactivacion/app/gobernador.go`
- Test: `internal/reactivacion/app/gobernador_test.go`

**Interfaces — Produces:**

```go
type PerfilEnvio string
const (
    PerfilProduccion PerfilEnvio = "produccion"
    PerfilDemo       PerfilEnvio = "demo"
)

type GobernadorConfig struct {
    TopeDiario int           // máx mensajes ENVIADOS por día (produccion default 30; demo 100000)
    JitterMin  time.Duration // produccion default 90s; demo 0
    JitterMax  time.Duration // produccion default 8m; demo 0
    HoraInicio int           // hora local de inicio de ventana (produccion 9; demo 0)
    HoraFin    int           // hora local de fin exclusivo (produccion 18; demo 24)
    Zona       *time.Location // hora de negocio (default America/Mexico_City)
}
// PerfilConfig devuelve la config default de un perfil (Zona = firebird.BusinessTZ o
// time.LoadLocation("America/Mexico_City")).
func PerfilConfig(p PerfilEnvio) GobernadorConfig

type Decision struct {
    Permitido bool
    Motivo    string        // "" si permitido; else "tope_diario" | "fuera_de_horario" | "jitter"
    Esperar   time.Duration // cuánto falta para el próximo intento cuando !Permitido (0 si tope_diario agotado hoy)
}

type Gobernador struct { /* cfg, rng *rand.Rand */ }
// NewGobernador: rng con semilla fija en tests (inyectada) o time-derived en prod. Firma:
func NewGobernador(cfg GobernadorConfig, rng *rand.Rand) *Gobernador
// PuedeEnviar decide si se puede enviar AHORA:
//  1. enviadosHoy >= TopeDiario → {false, "tope_diario", 0}
//  2. now fuera de [HoraInicio, HoraFin) en Zona (o domingo) → {false, "fuera_de_horario", duración hasta próxima ventana}
//  3. now < ultimoEnvio + jitter(rng, JitterMin, JitterMax) → {false, "jitter", tiempo restante}
//  4. else → {true, "", 0}
// ultimoEnvio zero = sin envío previo (pasa el jitter).
func (g *Gobernador) PuedeEnviar(enviadosHoy int, ultimoEnvio, now time.Time) Decision
```

**Detalles:**
- Días hábiles: L-S (excluir domingo). Domingo → `fuera_de_horario`, `Esperar` = hasta el lunes HoraInicio.
- El jitter se calcula una vez por decisión con `rng.Int63n`; para tests deterministas se inyecta un `rng`
  con `rand.New(rand.NewSource(semilla))`. En perfil demo JitterMin=JitterMax=0 → siempre pasa el jitter.
- `PerfilConfig` centraliza los defaults; el wiring elige el perfil por env.

- [ ] **Step 1:** `gobernador_test.go` (black-box) con reloj y rng fijos:
  - tope agotado → `{false, "tope_diario"}`;
  - dentro de horario, sin envío previo, cupo → `{true}`;
  - fuera de horario (ej. 07:00 y 20:00) → `{false, "fuera_de_horario"}` con `Esperar>0`;
  - domingo → `fuera_de_horario`;
  - `now` = `ultimoEnvio + 10s` con jitter [90s,8m] → `{false, "jitter"}` con `Esperar>0`;
  - perfil demo (jitter 0, horas 0-24, tope alto) → siempre `{true}` en cualquier hora;
  - `PerfilConfig(PerfilProduccion/Demo)` devuelve los defaults esperados.
- [ ] **Step 2:** `gobernador.go` para pasar.
- [ ] **Step 3:** `go test ./internal/reactivacion/app/... -run Gobernador` verde, cobertura de las ramas.
  Commit al final de Task 6.

---

### Task 6: App — `EnvioService` + `EnvioWorker`

**Files:**
- Create: `internal/reactivacion/app/envio_service.go`
- Create: `internal/reactivacion/app/envio_worker.go`
- Test: `internal/reactivacion/app/envio_service_test.go`, `internal/reactivacion/app/envio_worker_test.go`
- Test helpers: extender `internal/reactivacion/app/fakes_test.go` con `fakeMensajeRepo` + `fakeSender` +
  `MarcarContactado` en el `fakeCohorteRepo`.

**Patrón a calcar:** `internal/reactivacion/app/construir_cohorte.go` (`runInTx`, `ConstruirEnSegundoPlano`
single-flight con `atomic.Bool`, logging) y `internal/analytics/app/refresh_worker.go` (ticker + Start/Stop
lifecycle).

**Consumes:** `outbound.MensajeRepo`, `outbound.MessageSender`, `outbound.CohorteRepo` (con
`MarcarContactado` + `ListarCohorte`), `outbound.Clock`, `app.Opener`, `app.Gobernador`, `app.TxRunner`.

**Interfaces — Produces:**

```go
// EnvioService se agrega al Service existente (mismo struct) O es un tipo aparte. DECISIÓN: extender el
// Service de Fase 1 con los nuevos campos (sender, mensajeRepo, opener, gobernador, autoSend) vía un
// constructor/o setters With*, para reusar runInTx/clock/logger/txMgr. Ver NewService actual y agregar
// campos + un WithCanal(...) que los settea. Mantener NewService de Fase 1 compatible.

// EncolarResult resume el encolado.
type EncolarResult struct { Encolados int }
// EncolarCohorte lee la cohorte de TRATAMIENTO (ListarCohorte{SoloTratamiento:true}), excluye a los que ya
// tienen mensaje (ClientesConMensaje), genera el opener por segmento, arma []*domain.Mensaje y los Inserta
// en runInTx. Idempotente: correr dos veces no duplica.
func (s *Service) EncolarCohorte(ctx context.Context) (EncolarResult, error)
// EncolarEnSegundoPlano: single-flight (atomic.Bool) + goroutine con context.Background(), como
// ConstruirEnSegundoPlano. Devuelve bool.
func (s *Service) EncolarEnSegundoPlano() bool

// DrenarResult resume una tanda de drenado.
type DrenarResult struct { Enviados, Bloqueados, Saltados int }
// DrenarCola procesa hasta `max` pendientes: para cada uno consulta el Gobernador (ContarEnviadosHoy +
// último envío). Si !auto_send → NO envía (deja encolado, cuenta Saltados). Si el gobernador no permite →
// para la tanda (respeta tope/horario/jitter). Si permite → sender.Enviar; éxito → m.MarcarEnviado(kind) +
// repo.Actualizar + cohorteRepo.MarcarContactado (todo en runInTx por mensaje); error → m.MarcarFallido +
// Actualizar. Devuelve el resumen.
func (s *Service) DrenarCola(ctx context.Context, max int) (DrenarResult, error)
```

**`EnvioWorker`** (`envio_worker.go`): calcar `analytics/app/refresh_worker.go` — struct con `svc`, `clock`,
`cfg` (intervalo del ticker), `logger`; `Start(ctx)`/`Stop(ctx)` (lifecycle.Hooks); en cada tick, si
`auto_send`, llama `svc.DrenarCola(ctx, batch)`. Cuando `auto_send=false`, el worker no hace nada (o no se
registra). Intervalo default configurable.

**`auto_send`** es un campo del Service (bool) seteado por el wiring desde config.

**Integridad de medición:** `MarcarContactado` SOLO se llama tras un envío exitoso; `SenderKind` viene de
`sender.Kind()`.

- [ ] **Step 1:** Extender `fakes_test.go`: `fakeMensajeRepo` (guarda insertados, lista pendientes,
  actualiza por ID, cuenta enviados hoy, clientes-con-mensaje) thread-safe (mutex, como el `fakeCohorteRepo`
  de Fase 1); `fakeSender` (registra envíos, `Kind()` configurable, puede forzar error); agregar
  `MarcarContactado` + captura al `fakeCohorteRepo`.
- [ ] **Step 2:** `envio_service_test.go` (black-box, fakes):
  - `EncolarCohorte`: encola solo tratamiento (control excluido), genera cuerpo no vacío por segmento,
    idempotente (2ª corrida no duplica: usa ClientesConMensaje), respeta `SoloTratamiento:true`;
  - `DrenarCola` con `auto_send=true`, gobernador demo: envía todos, marca enviado + FUE_CONTACTADO,
    `SenderKind` = el del sender;
  - `DrenarCola` con `auto_send=false`: no envía nada (Saltados = pendientes, nadie contactado);
  - gobernador que niega (tope) → para la tanda (Enviados=0);
  - sender que falla → mensaje `fallido`, NO se marca contactado;
  - errores de repo propagan.
- [ ] **Step 3:** Implementar `envio_service.go` (+ extender `service.go` de Fase 1 con los campos/`WithCanal`)
  y `opener.go`/`gobernador.go` ya existen (Tasks 4/5).
- [ ] **Step 4:** `envio_worker_test.go`: un tick con `auto_send=true` llama `DrenarCola` (verificar vía fake
  service o contador); `auto_send=false` no envía; `Start`/`Stop` no truena. Calcar el test del refresh
  worker.
- [ ] **Step 5:** Implementar `envio_worker.go`.
- [ ] **Step 6:** `go test -race ./internal/reactivacion/app/...` verde; cobertura app ≥90%. Commit:
  `feat(reactivacion): encolar + drenar + gobernador + opener (fase 2)` (incluye Tasks 4,5,6; los ports de
  Task 3 se commitean con Task 7 para no romper el build — o incluir todo cuando compile).

---

### Task 7: Infra `reactivacionfb` — `MensajeRepo` + `CohorteRepo.MarcarContactado`

**Files:**
- Create: `internal/reactivacion/infra/reactivacionfb/mensaje_repo.go`
- Create: `internal/reactivacion/infra/reactivacionfb/mensaje_queries.go`
- Modify: `internal/reactivacion/infra/reactivacionfb/rowmappers.go` (agregar `mensajeRowRaw` + assemble)
- Modify: `internal/reactivacion/infra/reactivacionfb/repo.go` (agregar métodos al `*Repo` existente +
  assertion `_ outbound.MensajeRepo = (*Repo)(nil)` + `MarcarContactado`)
- Test: `internal/reactivacion/infra/reactivacionfb/mensaje_repo_test.go`

**Patrón a calcar EXACTO:** el mismo `repo.go`/`queries.go`/`rowmappers.go` de Fase 1
(`internal/reactivacion/infra/reactivacionfb/`). El `*Repo` ya tiene `pool`, `GetQuerier`, `MapError`,
`RunInReadTx`, `RunInTx`, scan helpers (`nullStringVal`, `scanNullableTime`, `parseUUIDColumn`). Reusarlos.

**Detalles de implementación:**
- `Insertar`: bulk. Reusar el patrón EXECUTE BLOCK de `buildUpsertBlock` de Fase 1, PERO como es INSERT puro
  (siempre nuevas filas, PK uuid único) puede ser un `INSERT` por fila en un loop dentro del querier
  ambiente, o un EXECUTE BLOCK de solo-INSERT. Preferir el INSERT-por-fila simple (más legible; el volumen
  por tanda es acotado) salvo que se encolen miles de golpe → usar EXECUTE BLOCK batched (calcar Fase 1).
  **Elegir EXECUTE BLOCK batched** (encolar puede meter ~3,300 filas). `CUERPO` es BLOB TEXT → bindear como
  string plano Go (UTF8). `SENDER_KIND`/`ENVIADO_EN`/`ERROR` van NULL al insertar (encolado).
- `Actualizar`: UPDATE por `ID` set `ESTADO, SENDER_KIND, ENVIADO_EN, ERROR, UPDATED_AT` (nullables vía
  `nullableWallClockArg`/NULL string). Un solo statement.
- `ListarPendientes`: `SELECT ... WHERE ESTADO='encolado' ORDER BY ENCOLADO_EN ASC ROWS ?`.
- `Listar`: WHERE builder por estado/segmento (calcar `buildCohorteWhere`), `ORDER BY ENCOLADO_EN`.
- `ContarEnviadosHoy`: `SELECT COUNT(*) FROM MSP_RX_MENSAJES WHERE ESTADO='enviado' AND ENVIADO_EN >= ?`
  (bind `firebird.ToWallClock(desde)`).
- `ClientesConMensaje`: `SELECT DISTINCT CLIENTE_ID FROM MSP_RX_MENSAJES` → map.
- `MarcarContactado(ctx, clienteID int, now time.Time)` (en repo.go, sobre MSP_RX_COHORTE):
  `UPDATE MSP_RX_COHORTE SET FUE_CONTACTADO=1, UPDATED_AT=? WHERE CLIENTE_ID=?`, bindeando
  `firebird.ToWallClock(now)` (NO toca EN_CONTROL/COHORTE_FECHA). El service pasa `now = s.clock.Now()`
  (CLAUDE.md §1). Usar `firebird.GetQuerier`/`MapError`.

**`mensajeRowRaw`** en rowmappers.go: `idRaw string`, `clienteID int`, `segmento string`, `telefono
sql.NullString`, `cuerpo sql.NullString` (BLOB TEXT lee como string; usar `sql.NullString` o `string`),
`estado string`, `senderKind sql.NullString`, `encoladoEn any`, `enviadoEn any`, `error sql.NullString`,
`createdAt any`, `updatedAt any`. `assembleMensaje` → `domain.HydrateMensaje` (parsear segmento/estado;
senderKind vacío→"" via `ParseSenderKind` solo si no vacío).

**Tests de integración** (`WithTestTransaction`, SKIP sin `FB_DATABASE`; IDs sintéticos grandes >900000000):
- Insertar + Listar round-trip (todos los campos, incl. CUERPO UTF-8 con acentos "Ñ");
- Actualizar: encolado → enviado (senderKind/enviadoEn/estado) se refleja;
- ListarPendientes solo devuelve encolados, orden por encolado_en;
- ContarEnviadosHoy cuenta solo enviados de hoy;
- ClientesConMensaje;
- MarcarContactado pone FUE_CONTACTADO=1 sin tocar EN_CONTROL/COHORTE_FECHA (insertar una cohorte antes,
  reusar `makeCohorte` de Fase 1, marcar, leer con ListarCohorte y verificar).

- [ ] **Step 1:** `mensaje_queries.go` (consts SQL) + `mensajeRowRaw`/`assembleMensaje` en rowmappers.go.
- [ ] **Step 2:** `mensaje_repo.go` con los métodos (o agregarlos a repo.go; preferir archivo aparte
  `mensaje_repo.go` con métodos sobre `*Repo`). Agregar `_ outbound.MensajeRepo = (*Repo)(nil)` y
  `MarcarContactado` (en repo.go o mensaje_repo.go).
- [ ] **Step 3:** `mensaje_repo_test.go` (integración). Correr con `FB_DATABASE` seteado (source .env):
  `set -a && . ./.env && set +a && go test -race ./internal/reactivacion/infra/reactivacionfb/...` → NO SKIP,
  verde.
- [ ] **Step 4:** Ahora el módulo compila con los ports (Task 3). `go build ./internal/reactivacion/...`.
  Commit: `feat(reactivacion): infra MSP_RX_MENSAJES repo + marcar contactado (fase 2)` (incluye Task 3 ports).

---

### Task 8: Infra sender — `FakeSender` + `WhatsmeowSender` (stub)

**Files:**
- Create: `internal/reactivacion/infra/reactivacionsender/fake.go`
- Create: `internal/reactivacion/infra/reactivacionsender/whatsmeow_stub.go`
- Test: `internal/reactivacion/infra/reactivacionsender/fake_test.go`

**Interfaces — Produces:**

```go
package reactivacionsender
// FakeSender simula el envío: registra (opcional) y devuelve éxito. Kind() = SenderSimulado.
type FakeSender struct { logger *slog.Logger }
func NewFakeSender(logger *slog.Logger) *FakeSender
func (f *FakeSender) Enviar(ctx context.Context, dest outbound.Destino, cuerpo string) error // loguea, nil
func (f *FakeSender) Kind() domain.SenderKind // domain.SenderSimulado

// WhatsmeowSender es un stub hasta Fase 3: Enviar devuelve un apperror "canal no configurado".
type WhatsmeowSender struct{}
func NewWhatsmeowSender() *WhatsmeowSender
func (w *WhatsmeowSender) Enviar(...) error // apperror.NewInternal("whatsmeow_no_configurado", "el canal de whatsapp aún no está configurado")
func (w *WhatsmeowSender) Kind() domain.SenderKind // domain.SenderReal

var _ outbound.MessageSender = (*FakeSender)(nil)
var _ outbound.MessageSender = (*WhatsmeowSender)(nil)
```

- [ ] **Step 1:** `fake_test.go`: `Enviar` devuelve nil, `Kind()`==SenderSimulado; (opcional) verifica que
  loguea. `WhatsmeowSender.Enviar` devuelve error; `Kind()`==SenderReal.
- [ ] **Step 2:** `fake.go` + `whatsmeow_stub.go`.
- [ ] **Step 3:** `go test ./internal/reactivacion/infra/reactivacionsender/...` verde (cobertura infra
  ≥80%). Commit: `feat(reactivacion): fake sender + whatsmeow stub (fase 2)`.

---

### Task 9: Infra HTTP — endpoints de envíos

**Files:**
- Modify: `internal/reactivacion/infra/reactivacionhttp/routes.go` (registrar 3 operaciones)
- Modify: `internal/reactivacion/infra/reactivacionhttp/handlers.go` (3 handlers + mappers)
- Modify: `internal/reactivacion/infra/reactivacionhttp/dto.go` (DTOs)
- Test: `internal/reactivacion/infra/reactivacionhttp/envios_test.go`

**Patrón a calcar EXACTO:** los handlers/rutas/DTOs de Fase 1 en el mismo paquete (`ListCohorte`, `Construir`,
`auth.go` con `currentUserOrError`/`requirePerm`/`mapAppError`). Reusar `auth.go` tal cual.

**Endpoints (spec §10):**
- `POST /v2/reactivacion/envios/encolar` → `auth.PermReactivacionAdministrar`; llama
  `svc.EncolarEnSegundoPlano()`; 202 con `{status, mensaje}` (calcar `Construir` de Fase 1).
- `POST /v2/reactivacion/envios/drenar` → `PermReactivacionAdministrar`; llama `svc.DrenarCola(ctx, batch)`
  (batch default, ej. 200); 200 con el resumen `{enviados, bloqueados, saltados}`.
- `GET /v2/reactivacion/envios` → `PermReactivacionLeer`; query `estado`, `segmento`, `limit`; llama
  `svc.Listar...` (agregar al service un `ListarMensajes(ctx, params)` que envuelva el repo, o exponer via
  service) → `{items: []MensajeDTO}`.

**DTOs** (snake_case, tagliatelle; fechas RFC3339 UTC; `enviado_en` nullable `*string`):
`MensajeDTO{ cliente_id, segmento, telefono, cuerpo, estado, sender_kind, encolado_en, enviado_en?, error? }`.
Agregar `ListarMensajes` al `Service` (app) que valide el estado/segmento (Parse*) y llame
`repo.Listar`.

- [ ] **Step 1:** Agregar `Service.ListarMensajes(ctx, ListarMensajesParams) ([]*domain.Mensaje, error)` en
  app (envuelve `mensajeRepo.Listar`, valida filtros). Actualizar Task 6 si hace falta.
- [ ] **Step 2:** `dto.go`: DTOs de envíos + input/output structs.
- [ ] **Step 3:** `handlers.go`: 3 handlers + `toMensajeDTOs`.
- [ ] **Step 4:** `routes.go`: registrar las 3 operaciones (tag "reactivacion", security bearer).
- [ ] **Step 5:** `envios_test.go`: para cada endpoint 401 sin user, 403 sin permiso, 200/202 con permiso
  (calcar `handlers_test.go` de Fase 1: `fakeReader`/`fakeRepo`/`fakeSender`, `buildService`, `planter`,
  `do`). Verificar que `drenar` con auto_send/gobernador demo reporta enviados; `encolar` 202.
- [ ] **Step 6:** `go test -race ./internal/reactivacion/infra/reactivacionhttp/...` verde. Commit:
  `feat(reactivacion): endpoints de envíos (fase 2)`.

---

### Task 10: Wiring fx + config

**Files:**
- Modify: `cmd/api/reactivacion_wiring.go` (providers nuevos + service extendido)
- Modify: `cmd/api/main.go` (registrar providers + worker lifecycle)
- Modify: `internal/platform/config/config.go` (bloque `Reactivacion` con env tags)
- Modify: `.env` y `.env.example` si existe (documentar las nuevas env)

**Config (caarlos0/env tags, calcar `Microsip`/`Cobranza` en config.go):**

```go
type Reactivacion struct {
    Sender    string `env:"REACTIVACION_SENDER" envDefault:"fake"`         // fake|whatsmeow
    Perfil    string `env:"REACTIVACION_PERFIL_ENVIO" envDefault:"demo"`   // produccion|demo
    AutoSend  bool   `env:"REACTIVACION_AUTO_SEND" envDefault:"false"`
    ControlPct int   `env:"REACTIVACION_CONTROL_PCT" envDefault:"50"`      // mover el 50 de Fase 1 aquí
    WorkerIntervalSeg int `env:"REACTIVACION_WORKER_INTERVAL_SEG" envDefault:"30"`
}
// Agregar campo Reactivacion a Config y al parse.
```

**Wiring (calcar los providers de Fase 1 en reactivacion_wiring.go + analytics worker lifecycle):**
- `provideReactivacionMensajeRepo(*reactivacionfb.Repo) outbound.MensajeRepo` (el `*Repo` ya lo implementa).
- `provideReactivacionSender(cfg, logger) outbound.MessageSender` — switch por `cfg.Reactivacion.Sender`:
  `fake` → `reactivacionsender.NewFakeSender(logger)`; `whatsmeow` → `NewWhatsmeowSender()`.
- Extender `provideReactivacionService` para setear el canal: `.WithCanal(mensajeRepo, sender,
  NewOpener(), NewGobernador(PerfilConfig(perfil), rng), autoSend)`. `rng`: `rand.New(rand.NewSource(
  time.Now().UnixNano()))` (en un provider; documentar que la no-determinística es intencional en prod).
- `provideReactivacionEnvioWorker(svc, clock, cfg, logger) *EnvioWorker`.
- `registerReactivacionEnvioWorkerLifecycle(lc, w)` → `lifecycle.Append(lc, "reactivacion-envio-worker", w)`.
- Registrar los providers nuevos en `main.go` `fx.Provide(...)` y el lifecycle en `fx.Invoke(...)`.
- `ControlPct`: mover el `pilotoControlPct=50` de Fase 1 a leerse de `cfg.Reactivacion.ControlPct` (o dejar
  la constante y sumar la env; mínimo: no romper Fase 1).

- [ ] **Step 1:** Agregar `Reactivacion` a config.go + al struct `Config` + parse. `go build ./...`.
- [ ] **Step 2:** Providers en `reactivacion_wiring.go` + `WithCanal` en el Service (app). Registrar en
  main.go (Provide + Invoke worker lifecycle).
- [ ] **Step 3:** `go build ./...` + cross-build Windows verde.
- [ ] **Step 4:** Boot smoke (sin escribir): `set -a && . ./.env && set +a; APP_PORT=3019
  FIREBASE_DEV_MODE=true APP_ENV=development /tmp/bin serve` en background; `curl` a
  `POST /v2/reactivacion/envios/encolar` (401 sin token = ruta montada), `GET /v2/reactivacion/envios` (401),
  `GET /v2/reactivacion/envios-nope` (404). Matar el server. (Calcar el smoke de Fase 1.)
- [ ] **Step 5:** Commit: `feat(reactivacion): wiring canal + config + worker (fase 2)`.

---

## Verification (global, al final)

1. `go build ./...` + `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/api` verdes.
2. `golangci-lint run ./...` → **0 issues** (correr `gofumpt -w internal/reactivacion cmd/api` + arreglar a
   mano lo que golangci marque; registrar cualquier alias nuevo en `.golangci.yml`).
3. `go test -race -short ./internal/reactivacion/... ./cmd/...` verde.
4. **Integración con `FB_DATABASE`** (source `.env`; migración 000045 aplicada): los tests
   `reactivacionfb` corren SIN SKIP y pasan (incluye MSP_RX_MENSAJES round-trip + MarcarContactado).
5. Cobertura: `go test -cover ./internal/reactivacion/domain/... ./internal/reactivacion/app/...` →
   domain ≥99%, app ≥90%; infra ≥80%.
6. **e2e local con enviador falso** (perfil demo, auto_send=on): bootear el server con
   `REACTIVACION_PERFIL_ENVIO=demo REACTIVACION_AUTO_SEND=true`, con un token super_admin dev; luego:
   - `POST /v2/reactivacion/cohorte/construir` (Fase 1) → construir la cohorte;
   - `POST /v2/reactivacion/envios/encolar` → 202; esperar; `GET /v2/reactivacion/envios?estado=enviado`
     devuelve mensajes; cross-check en Firebird: `SELECT ESTADO, SENDER_KIND, COUNT(*) FROM MSP_RX_MENSAJES
     GROUP BY 1,2` (enviado/simulado) y `SELECT COUNT(*) FROM MSP_RX_COHORTE WHERE FUE_CONTACTADO=1`;
   - `GET /v2/reactivacion/atribucion` responde con tratamiento poblado.
   **Este paso escribe filas persistentes** → hacer `make fb-snapshot` antes y `DELETE FROM MSP_RX_MENSAJES`
   + `UPDATE MSP_RX_COHORTE SET FUE_CONTACTADO=0` (o restaurar snapshot) al terminar, salvo indicación
   contraria. **Marcar este e2e como OPCIONAL/manual** — no es obligatorio para cerrar la fase; los tests de
   integración ya cubren el repo y el server-boot cubre el wiring.
7. Sin escrituras persistentes a la DB compartida fuera de los tests rollback (salvo el e2e opcional del §6,
   que se limpia).

## Notas de robustez (para el agente ejecutor)

- **No dejar el build roto entre commits:** Task 3 (ports) se commitea junto con Task 7 (infra) porque los
  nuevos métodos de interfaz rompen la assertion `_ outbound.CohorteRepo = (*Repo)(nil)` hasta implementarse.
- **Fase 1 es el molde:** ante cualquier duda de estilo (scan helpers, EXECUTE BLOCK, DTO, handler, wiring,
  test de integración), abrir el archivo equivalente de Fase 1 y calcarlo. No inventar patrones nuevos.
- **Determinismo:** todo lo que use tiempo/aleatoriedad se inyecta (clock, rng) para tests estables; nada de
  `time.Now()`/`rand` global dentro de lógica testeable.
- **Idempotencia:** `EncolarCohorte` no debe duplicar (usar `ClientesConMensaje`); `DrenarCola` no debe
  re-enviar (solo toma `encolado`).

## Fuera de alcance (Fase 3)

Copiloto de IA, triaje de respuestas, recepción inbound, UI de bandeja/aprobación, montos reales de
enganche/parcialidad, adaptador whatsmeow productivo + su validación en vivo, 2º toque de recordatorio, y la
capa de gobierno de contenido (allowlist).
