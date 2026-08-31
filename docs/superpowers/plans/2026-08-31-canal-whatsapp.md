# Plan — Canal de WhatsApp · `internal/canal` + `cmd/winback`

- **Fecha:** 2026-08-31
- **Spec / autoridad vinculante:** [`docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md`](../../adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md) (aceptado 2026-08-30)
- **Specs que ADR-0010 enmienda:** `docs/superpowers/specs/2026-06-06-ventas-ai-winback-system-design.md` §3.1 · `docs/superpowers/specs/2026-07-21-reactivacion-r7-fase3-design.md` §3b
- **Rama:** `feat/canal-whatsapp`

## Contexto

`internal/reactivacion/` está terminado —cohorte, atribución, copiloto de IA, allowlist,
gobernador, cola de envío, bandeja— **excepto el canal**. `whatsmeow_stub.go` devuelve
`whatsmeow_no_configurado` y `whatsmeow` ni siquiera está en `go.mod`. Sin canal no hay
mensajes, sin mensajes no hay ventas, y la comisión se paga por venta.

**ADR-0010** ya decidió la arquitectura: canal oficial de Meta en vez de whatsmeow, y un
binario siempre-encendido en un VPS con URL pública, porque **Meta empuja webhooks y no
hay endpoint que consultar** — si no nos alcanza, la respuesta del cliente se pierde, no
se atrasa. El servidor de la tienda arranca a mano desde un `.bat` y su túnel rota cada
60 minutos.

**Resultado buscado:** poder mandar y recibir mensajes de WhatsApp de forma confiable,
sin riesgo de baneo, con el piloto de Tehuacán corriendo esta semana.

### Arquitectura

```
Meta ──webhook──▶ VPS · cmd/winback ──push──▶ tienda · cmd/api
     ◀──enviar──   internal/canal (sellado)      reactivacion
                   SQLite: buzón durable          Firebird
```

### Rutas — versionadas y con transporte nombrado

```
VPS      GET/POST /canal/v1/webhook/whatsapp
         POST     /canal/v1/salientes
         GET      /canal/v1/salud
tienda   POST     (reutilizar mensaje-entrante si sirve)
```

El día que entre SMS es `/canal/v1/webhook/sms` y nada más se mueve.

### Lo que YA existe — no reconstruir

| | |
|---|---|
| `internal/reactivacion/ports/outbound/sender.go` | `MessageSender{Enviar, Kind}`. **ADR-0010 dice que su firma no cambia** |
| `internal/comprobantes/ports/outbound/sender.go:30` | `Sender{Enviar(ctx, Destino, Documento, plantilla, variables) (string, error); Canal()}` — ya diseñado para plantillas |
| `internal/reactivacion/infra/reactivacionhttp/copiloto_handlers.go` | `POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante` |
| `internal/platform/llm/openai_compatible.go` | El único cliente HTTP saliente del repo. **Molde obligatorio** |
| `internal/platform/reliability` | `NewRetry[T]`, `NewCircuit[T]` sobre failsafe-go. Sin usar aún para HTTP |
| `internal/reactivacion/app/envio_worker.go` | Molde de worker con `lifecycle.Hooks` |
| `internal/platform/httptesting/fakes.go` | Fakes compartidos para tests de composición |

## Global Constraints

Estas ligan **todas** las tareas. Un revisor las usa como lente de atención.

1. 🔴 **CI corre `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`** — `cmd/winback`
   debe cross-compilar para Windows aunque corra en Linux. **Solo dependencias Go puro.**
   `modernc.org/sqlite` sí; `mattn/go-sqlite3` **rompe CI**.
2. 🔴 **`hmac.Equal`, nunca `==`** — `gosec` marca la comparación no constante.
3. 🔴 **Validar la firma contra los bytes crudos, antes de parsear JSON.** Cualquier
   middleware que decodifique y recodifique la invalida. Meta además escapa Unicode.
4. 🔴 **fx cancela el ctx de `OnStart` al terminar el arranque** — el loop del worker no
   puede heredarlo. Molde: `internal/reactivacion/app/envio_worker.go:87`.
5. **`err113`**: nada de `fmt.Errorf` suelto. Sentinelas + `%w`.
6. **`funlen` 80/50, `cyclop` 15, `gocognit` 25.** Solo `cmd/*/main.go` está exento.
7. **`importas` con `no-extra-aliases`** — cada alias nuevo va en `.golangci.yml`.
8. **`godot`**: todo comentario termina en punto. **`revive`**: doc en cada símbolo exportado.
9. **Código en inglés, mensajes al usuario en español** (CLAUDE.md §3). Sentinelas
   `apperror` con código inglés snake_case y mensaje español en minúsculas sin punto final.
10. **Sin lógica en la base**: ni `AUTOINCREMENT` ni `DEFAULT CURRENT_TIMESTAMP` ni
    triggers. IDs con `uuid.New()`, timestamps con el `Clock` inyectado.
11. **UTF-8 NFC en domain**; `requireBounded` mide en **codepoints**, no en bytes.
12. **`internal/canal` es un módulo SELLADO (ADR-0009)**: importa solo stdlib, `uuid`,
    `decimal`, `internal/canal/…` y `internal/platform/…`. Nada de `auth`, nada de
    `reactivacion`. Verificado por `make check-sealed`.
13. **Dos constructores por entidad** (`New…` que genera identidad/tiempo, `Rehydrate…`/
    `Restore…` que reconstruye desde la base), campos privados, getters.

### Desviaciones conscientes del estándar — ya decididas, no relitigar

| Estándar | Desviación | Razón |
|---|---|---|
| `infra/{module}fb/` Firebird | **`infra/canalsqlite/`** | El VPS no tiene Firebird. Precedente: `reactivacionllm`, `ventas/infra/storage` |
| `firebird.ToWallClock` / `ScanUTCTime` "sin excepción" | **NO usarlos** | Existen solo porque el cliente Delphi de Microsip lee wall-clock CDMX. Aplicarlos a SQLite **es un bug activo**. Se guarda RFC3339Nano UTC en `TEXT` |
| `txMgr *firebird.TxManager` concreto | **`TxRunner` local**, como `internal/reactivacion/app/service.go:25` | `app/` no puede importar infra |
| `{module}outbox/` sobre `MSP_OUTBOX_EVENTS` | **El forwarder ES el outbox** | La tabla vive en Firebird on-premise. La cola de reenvío con reintento cumple la misma función |
| `migrations-firebird/` | **DDL embebido en Go** (`//go:embed`) | El glob de lefthook dispara `make test-firebird-all` sobre un esquema que Firebird nunca ve |
| Huma para todo | **chi crudo para el webhook** | `docs/module-standards/08-handlers-routes.md:588` lo permite. El `GET` de Meta devuelve `hub.challenge` en `text/plain`, y el `POST` necesita **bytes crudos** para el HMAC |
| Permisos de `auth` | **Ninguno** | Meta autentica por firma HMAC; la tienda por token compartido. Ningún usuario Firebase toca esto |

**Todo lo demás del estándar aplica sin cambio.** Antes de escribir código, leer
`docs/module-standards/MODULE_TEMPLATE.md` y los específicos que aplican a la capa que
se toca.

### Verificación que corre el implementador antes de reportar

```bash
go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
golangci-lint run ./...            # al menos sobre los paquetes tocados
go test ./... -race -count=1 -short -timeout 300s
```

## Riesgos abiertos

- **Meta cobra por conversación** y el número real no existe. Todo contra fakes; las
  credenciales entran por variables de entorno. **Ningún test toca la red.**
- **`comprobantes` no se cablea** en este build, pero `platform/whatsapp` soporta
  plantilla, texto y media desde el inicio para que enchufe sin rediseño.
- **El endpoint del lado tienda expone el Windows Server 2016.** Superficie mínima: un
  path, token, y filtrado por IP del VPS si se puede.

---

## Task 1 — `internal/platform/whatsapp/` (transporte)

*Ola 0. Bloquea todo lo demás.*

Cliente HTTP de la WhatsApp Cloud API de Meta, en `internal/platform/whatsapp/`.

**Molde exacto:** `internal/platform/llm` y `internal/platform/meilisearch`. Léelos
completos antes de escribir. La estructura de paquete, el `doc.go`, el patrón
`factory.go`, la clasificación de errores y la forma de los tests salen de ahí — no se
inventa nada nuevo.

### Entregables

- **`doc.go`** con la regla de genericidad del paquete: `platform/whatsapp` no sabe de
  `canal`, ni de `reactivacion`, ni de `comprobantes`. Expone un transporte; el dominio
  vive en los módulos.
- **`Client` interface**, pequeña (`interfacebloat` está activo). Debe cubrir:
  - enviar **texto** libre,
  - enviar **plantilla** (nombre, código de idioma, variables posicionales del body),
  - enviar **documento por `media_id`** (con `filename` y `caption`),
  - **subir media** (`POST /{phone_number_id}/media`, multipart) devolviendo el `media_id`.
- **`cloudapi.go`** — la implementación real contra Graph API. `https://graph.facebook.com/{version}/{phone_number_id}/messages`, `Authorization: Bearer <token>`,
  JSON. Devuelve el `wamid` de la respuesta (`messages[0].id`).
- **`disabled.go`** — implementación que devuelve una sentinela cuando no hay
  configuración, igual que hace `llm` cuando falta el proveedor.
- **`factory.go`** — matriz de selección explícita: qué configuración produce qué
  implementación, con la rama de "no configurado" devolviendo la de `disabled.go`.
- **`config.WhatsApp`** en `internal/platform/config` — token, phone number ID,
  business account ID, app secret (para el HMAC del webhook), versión de la Graph API,
  timeout, base URL (para que los tests apunten a `httptest`). Sigue el patrón de las
  otras secciones de config del repo, incluidas sus reglas de validación.

### Clasificación de errores

Copiar la forma de `internal/platform/llm/openai_compatible.go`:

- `TransientError` + `IsTransient(err) bool`.
- **429 y ≥500 son transitorios**; los demás 4xx son permanentes.
- El error lleva un **snippet del body** de la respuesta (acotado) para que sea
  diagnosticable, y `%w` para no romper `errors.Is`.
- Mapear los códigos de error de Meta, exportados como sentinelas del paquete:
  - **131047** — ventana de 24 h cerrada (hay que usar plantilla).
  - **131026** — número inválido / no tiene WhatsApp.
  - **130429** — límite de tasa de Meta.

### Tests

`httptest.NewServer` como en `internal/platform/llm/llm_test.go`. Sin red real, sin
credenciales. Cubrir: envío de texto ok, envío de plantilla ok, documento por media_id,
subida de media, 429 → transitorio, 500 → transitorio, 400 → permanente, cada uno de
los tres códigos de Meta, y `disabled.go` devolviendo la sentinela.

### Alias `importas`

Este paquete se importará como `platformwhatsapp`. Agrega esa entrada a la lista
`importas.alias` de `.golangci.yml` en esta misma tarea (`no-extra-aliases: true` hace
que sin ella no compile el lint).

---

## Task 2 — `internal/canal/domain` + `internal/canal/ports/outbound`

*Ola 1. Depende de Task 1 solo conceptualmente; no importa `platform/whatsapp`.*

El dominio del módulo sellado. **Gate de cobertura: ≥99%.**

### `internal/canal/domain`

- **`MensajeEntrante`** — la entidad. Deduplicada por **`wamid`** (el ID de mensaje que
  asigna Meta): el `wamid` es la clave natural de idempotencia y el dominio debe
  exigirlo no vacío. Campos mínimos: `wamid`, teléfono del remitente, teléfono
  destino/`phone_number_id`, tipo de mensaje, texto (o referencia a media), timestamp
  de Meta, timestamp de recepción, y el estado de reenvío. Dos constructores
  (`NewMensajeEntrante` genera ID y usa el `Clock`; `RehydrateMensajeEntrante` reconstruye
  desde la base sin validar identidad ni tiempo). Campos privados + getters.
- **`EstadoReenvio`** — **State VO** con `valid…Transitions`. **Molde exacto:**
  `internal/garantias/domain/estado_folio.go`. Léelo y cópiale la forma: tipo string
  con constantes, `Parse…`, `String()`, mapa de transiciones válidas, método de
  transición que devuelve error de dominio si es inválida. Estados sugeridos:
  `pendiente` → `reenviado` | `fallido`, y `fallido` → `pendiente` (reintento).
  Ajusta si el diseño del worker de Task 4 lo pide, pero **el mapa de transiciones es
  obligatorio** — nada de comparar strings sueltos.
- **`errors.go`** — **un solo archivo** con todas las sentinelas del dominio,
  `apperror` con código inglés y mensaje español.
- **`events.go`** — eventos de dominio con nombres bajo el prefijo **`canal.*`**
  (p. ej. `canal.mensaje_entrante_recibido`, `canal.mensaje_reenviado`,
  `canal.reenvio_fallido`). Sigue el patrón de eventos de dominio de
  `docs/module-standards/AGGREGATE_PATTERNS.md`.

Validación de texto con `requireBounded` midiendo **codepoints**; normalización **NFC**.

### `internal/canal/ports/outbound`

- **`clock.go`** — la interfaz `Clock`. **Este archivo lleva el package doc** del
  paquete `outbound` (una sola declaración de doc por paquete).
- **`buzon_repo.go`** — el puerto del buzón durable: guardar un entrante de forma
  idempotente por `wamid`, listar los pendientes de reenvío acotados, marcar reenviado,
  marcar fallido con el mensaje de error, contar pendientes (para `/salud`).
- **`forwarder.go`** — el puerto que empuja un entrante al servidor de la tienda.

### Tests

Tabla por VO, **property-based con `pgregory.net/rapid`** sobre las invariantes del
State VO (toda transición fuera del mapa falla; toda transición dentro del mapa
preserva la invariante), y **fuzz** sobre el parseo/validación de entrada.
Revisa `docs/module-standards/TESTING_REQUIREMENTS.md` para la forma exacta que el
repo espera de estos tres. **Gate: ≥99% en `internal/canal/domain`.**

---

## Task 3 — Configuración del repo para el módulo sellado

*Ola 1. Depende de Task 2 (los paquetes deben existir para que `check-sealed` corra).*

Solo configuración; nada de lógica.

- **`.golangci.yml` · `importas.alias`** — agregar `canaldomain`, `canalapp`,
  `canaloutbound`, `canalhttp`, `canalsqlite` para
  `internal/canal/{domain,app,ports/outbound,infra/canalhttp,infra/canalsqlite}`.
  (`platformwhatsapp` ya lo agregó Task 1; verifica que esté y no lo dupliques.)
- **`.golangci.yml` · regla `domain-pure`** — **verificar, no editar.** La regla (línea
  ~398) selecciona por glob `**/internal/*/domain/*.go`, así que `internal/canal/domain`
  ya queda cubierta sola: no hay allowlist de módulos que tocar. Confirma con un
  **control positivo** —que la regla efectivamente dispara sobre un import prohibido
  metido a propósito en `internal/canal/domain`, y bórralo— antes de declarar que
  aplica. Si al hacerlo descubres que no dispara, eso sí es un cambio a hacer, y dilo
  en el reporte.
- **`.golangci.yml` · regla `canal-sealed`** — copiar el bloque `flota-sealed`
  (está alrededor de la línea 470) cambiando `flota` por `canal`, con su `desc`
  explicando que canal es sellado por ADR-0009.
- **`Makefile`** — agregar `canal` a `SEALED_MODULES` (línea 15).
- **`Makefile`** — target `build-winback` que cross-compila el binario para **Linux**
  (`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`), porque el VPS es Linux, a diferencia de
  `build` que apunta a Windows. Declararlo en `.PHONY`. El target puede existir antes
  que `cmd/winback` (Task 8); no lo invoques en esta tarea.
- **`Makefile`** — target `coverage-canal` copiando la forma de `coverage-auth`
  (los gates del plan se miden con `go test -coverprofile`; **`make test-coverage` no
  existe en este Makefile** — no lo invoques ni lo inventes).
- **`.env.example`** — las variables nuevas: credenciales de Meta (token, phone number
  id, business account id, app secret, versión de Graph API), el `verify_token` del
  webhook, la ruta del archivo SQLite, la URL del receptor de la tienda y el token
  compartido, y el puerto de `winback`. Con comentario y valores de ejemplo obviamente
  falsos.

Verificación: `golangci-lint run ./...` limpio y `make check-sealed` verde con `canal`
incluido.

---

## Task 4 — `internal/canal/app`

*Ola 2. Depende de Task 2.* **Gate de cobertura: ≥90%.**

- **`service.go`** — el servicio de aplicación. `TxRunner` **local** declarado en el
  propio `app/` (molde: `internal/reactivacion/app/service.go:25`), porque `app/` no
  puede importar infra. Constructor con dependencias inyectadas (repo, forwarder,
  clock, logger, config del worker).
- **Recepción idempotente por `wamid`** — recibir un entrante ya visto **no** duplica
  ni vuelve a emitir el evento; devuelve el existente. Ese es el invariante central de
  la tarea y debe tener su propio test.
- **Worker de reenvío** — loop con backoff y reintento usando
  `internal/platform/reliability` (`NewRetry[T]`, `NewCircuit[T]`). Molde de ciclo de
  vida: `internal/reactivacion/app/envio_worker.go`, con `lifecycle.Hooks`.
  🔴 **El ctx del loop NO puede heredar el de `OnStart`** — fx lo cancela al terminar
  el arranque. Mira `envio_worker.go:87` para ver exactamente cómo se resuelve ahí y
  hazlo igual.
- El error que se persiste vía el puerto debe ser **legible**: es lo que un humano leerá
  cuando un reenvío falle.

### Tests

**Fakes a mano en `fakes_test.go`** (nada de mocks generados) y un `fixedClock`.
Cubrir: recepción nueva, recepción duplicada por `wamid`, reenvío ok, reenvío que falla
y marca fallido con mensaje legible, reintento con backoff, y el apagado limpio del
worker. `go test -race` debe pasar. **Gate: ≥90%.**

---

## Task 5 — `internal/canal/infra/canalsqlite`

*Ola 2. Depende de Task 2 (implementa `outbound.BuzonRepo`).*

Repositorio SQLite del buzón durable.

- **Driver: `modernc.org/sqlite`** (Go puro). 🔴 `mattn/go-sqlite3` rompe el build de CI
  por cgo. Agregar la dependencia a `go.mod`.
- **DDL embebido en Go** con `//go:embed` de un `.sql` que vive junto al código —
  **no** en `migrations-firebird/`. Sin `AUTOINCREMENT`, sin `DEFAULT CURRENT_TIMESTAMP`,
  sin triggers: el ID y los tiempos vienen de Go.
- Índice **único sobre `wamid`** — es la dedup a nivel de almacenamiento, y el `INSERT`
  idempotente se apoya en él.
- **Timestamps: RFC3339Nano UTC en columnas `TEXT`.** 🔴 **No usar
  `firebird.ToWallClock` ni `firebird.ScanUTCTime`** — existen para el cliente Delphi de
  Microsip y aplicarlos aquí es un bug activo.
- `var _ outbound.BuzonRepo = (*Repo)(nil)` como aserción de compilación.
- Mapear los errores de SQLite a las sentinelas del dominio; nada de `fmt.Errorf` suelto.

### Tests

Contra `:memory:`. Cubrir: crear esquema, insertar, insertar duplicado por `wamid` (no
duplica), listar pendientes acotados, marcar reenviado, marcar fallido con mensaje,
contar pendientes, y round-trip de un timestamp con nanosegundos verificando que vuelve
idéntico en UTC.

---

## Task 6 — `internal/canal/infra/canalhttp`

*Ola 3. Depende de Task 4.* **Gate de cobertura: ≥70%.**

- **Webhook en chi crudo** — `GET /canal/v1/webhook/whatsapp` responde el
  `hub.challenge` en **`text/plain`** cuando `hub.mode=subscribe` y el `hub.verify_token`
  coincide (comparado con `hmac.Equal` sobre los bytes, no con `==`); si no coincide,
  403 sin cuerpo útil.
- `POST /canal/v1/webhook/whatsapp` — 🔴 **leer el body crudo, validar
  `X-Hub-Signature-256` (`sha256=<hex>`) con HMAC-SHA256 sobre esos bytes exactos ANTES
  de parsear JSON**, con **`hmac.Equal`**. Ningún middleware puede decodificar y
  recodificar el cuerpo antes de esto. Firma inválida o ausente → 403. Firma válida →
  parsear, entregar al servicio y **responder 200 de inmediato**, incluso si el
  procesamiento posterior falla (Meta reintenta y duplicaría).
- **API interna en Huma** — `POST /canal/v1/salientes` (encolar/enviar un saliente) y
  `GET /canal/v1/salud` (estado + pendientes en el buzón). DTOs según
  `docs/module-standards/HUMA_WIRING.md`.
- **Middleware de token compartido** para las rutas internas: header con el token,
  comparado con `hmac.Equal`. **No** usa `auth` ni Firebase. `/salud` decide
  explícitamente si va protegido o no y lo documenta.
- **`mapAppError`** copiado de `internal/reactivacion/infra/reactivacionhttp/auth.go`
  (misma traducción de sentinelas a códigos HTTP).
- **`forwarder_client.go`** — el adaptador HTTP que cumple el puerto
  `outbound.Forwarder` de Task 2: empuja un `MensajeEntrante` al servidor de la tienda
  por HTTP con el **token compartido** en el header, con timeout y clasificación de
  error transitorio/permanente (el worker de Task 4 reintenta solo los transitorios).
  *(Movido aquí desde Task 10 por decisión del controlador: Task 7 cablea el módulo y
  Task 8 lo arranca, así que la implementación del puerto tiene que existir antes. La
  URL, el token y la forma del payload son configuración; Task 10 verifica el contrato
  contra el endpoint real de la tienda y ajusta este archivo si el DTO difiere.)*
  La URL base y el token salen de `config`, nunca hardcodeados. Nunca registrar el
  token en logs.

### Tests

`httptest` sobre el router real. Cubrir: challenge ok, challenge con token equivocado,
POST con firma válida, POST con firma inválida, POST sin header de firma, POST con body
alterado tras firmar, el mismo `wamid` dos veces (200 las dos, sin duplicar), rutas
internas con y sin token. **Gate: ≥70%.**

---

## Task 7 — `internal/canal/module.go`

*Ola 3. Depende de Tasks 4, 5, 6.*

El `fx.Option` del módulo, siguiendo el patrón de ADR-0009 y el `module.go` de los otros
módulos sellados (`internal/asistencia`, `internal/flota`, `internal/garantias`) —
léelos y copia la forma. Providers, `fx.Invoke` del registro de rutas, y los hooks de
ciclo de vida del worker. Nada de lógica de negocio aquí.

---

## Task 8 — `cmd/winback/`

*Ola 4. Depende de Task 7.*

El binario siempre-encendido del VPS. Molde: `cmd/api` — léelo entero.

- **cobra + fx**, subcomando `serve`.
- **`_ "time/tzdata"`** importado (el VPS puede no tener tzdata).
- Providers en archivos `*_wiring.go`, no en `main.go`.
- `lifecycle.Append` para arranque y apagado ordenado del servidor HTTP y del worker.
- **`TestAppGraph_IsValid`** con `fx.ValidateApp` — el test que prueba que el grafo se
  resuelve sin arrancar nada. Molde: el equivalente en `cmd/api`.
- 🔴 Debe cross-compilar: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`
  **y** `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/winback`.
- Solo `cmd/*/main.go` está exento de `funlen`/`cyclop`; el resto de `cmd/winback` no.

Verificación manual que debe quedar documentada en el reporte:
`go build -o bin/winback ./cmd/winback && ./bin/winback serve` arranca y responde
`GET /canal/v1/salud`.

---

## Task 9 — `reactivacionsender/cloudapi.go` + selector

*Ola 1 por dependencias (solo necesita Task 1), se ejecuta aquí.*

Implementación de `MessageSender` de reactivación sobre `platform/whatsapp`.

- Archivo nuevo `internal/reactivacion/infra/reactivacionsender/cloudapi.go`.
  **La firma de `outbound.MessageSender` no cambia** (ADR-0010).
- **`Kind()` devuelve `domain.SenderReal`** — 🔴 **no inventar un tercer `SenderKind`**:
  rompe el linter `exhaustive` y `ParseSenderKind`.
- ⚠️ El error que devuelve `Enviar` **se persiste** vía `MarcarFallido(sendErr.Error())`
  y lo leerá un humano: debe ser legible, en español donde sea mensaje de usuario, y
  llevar el código de Meta cuando lo haya.
- 🔴 **Nunca devolver `nil` por un envío encolado o parcial.** `nil` significa "Meta
  aceptó el mensaje y devolvió un `wamid`". Cualquier otra cosa es error.
- Agregar la rama al selector `provideReactivacionSender`
  (`cmd/api/reactivacion_wiring.go:61`) y el valor nuevo a la variable de entorno
  `REACTIVACION_SENDER` (y a `.env.example`).
- **No borrar `whatsmeow_stub.go`** salvo que quede sin referencias tras el cambio; si
  queda sin referencias, dilo en el reporte en vez de borrarlo por tu cuenta.

Tests con el fake/`httptest` de `platform/whatsapp`: envío ok devuelve `wamid`, error de
Meta produce un mensaje legible, ventana cerrada (131047) se distingue de número
inválido (131026).

---

## Task 10 — Lado tienda: recepción del entrante empujado por el VPS

*Ola 4. Depende de Tasks 6 y 7.*

🔴 **Primera acción obligatoria: verificar si
`POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante`
(`internal/reactivacion/infra/reactivacionhttp/copiloto_handlers.go`) ya sirve como
endpoint del lado tienda.** Léelo, mira qué DTO acepta, qué autenticación exige y qué
hace con el mensaje.

- **Si sirve:** **no se crea endpoint nuevo.** Solo se agrega el camino de
  autenticación por **token compartido máquina-a-máquina** para que el VPS pueda
  llamarlo sin un usuario Firebase, y el mapeo del payload del VPS a su DTO.
- **Si no sirve:** justifica en el reporte exactamente por qué, y agrega la ruta mínima
  que falte reutilizando el servicio de reactivación que ya existe.

En cualquier caso:

- **Autenticación máquina-a-máquina, no Firebase.** Token compartido comparado con
  `hmac.Equal`.
- **Superficie mínima**: un solo path, el token, y filtrado por IP del VPS si la
  configuración lo permite (opcional, configurable, apagado por omisión).
- El adaptador cliente que empuja (`internal/canal/infra/canalhttp/forwarder_client.go`,
  el que cumple `outbound.Forwarder`) **ya existe: lo escribió Task 6.** Aquí solo se
  **verifica el contrato contra el endpoint real** y se ajusta el payload/DTO si difiere.
  No lo reescribas.
- Documentar en el reporte la superficie expuesta del Windows Server 2016.

---

## Task 11 — Tests de composición y de seguridad

*Ola 5. Depende de todas las anteriores.*

`docs/module-standards/TESTING_REQUIREMENTS.md:119`: *"una ruta nueva no está terminada
hasta que aterriza su test de composición."*

- **`TestE2E_…`** que ejerciten la **cadena real de middleware** del router de
  `cmd/winback` —no un handler aislado—, usando `internal/platform/httptesting/fakes.go`
  donde aplique. Como mínimo: challenge de Meta, webhook firmado que aterriza en SQLite,
  el mismo `wamid` repetido que no duplica, y el reenvío al receptor de la tienda.
- **La prueba de durabilidad**: con el receptor de la tienda caído, el mensaje **queda
  en SQLite**; cuando el receptor vuelve, **se reenvía**. Es el invariante que justifica
  el buzón entero.
- **`security_test.go`** sobre las rutas del VPS: firma inválida, firma ausente, body
  alterado tras firmar, token interno ausente o equivocado, y que ninguna respuesta de
  error filtre el secreto ni el token.

Todo con SQLite `:memory:` o archivo temporal, `httptest`, y **sin red real ni
credenciales de Meta**.
