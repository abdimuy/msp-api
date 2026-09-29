# Garantías — Tarea 5: puertos, contratos y los comandos de apertura

> **Rama:** crea `feat/garantias-puertos` desde `main`, **después** de que se fusione el PR #21. Si `internal/garantias/domain/garantia.go` no existe en tu `main`, todavía no se fusionó: espera, no partas de `feat/garantias-dominio`.
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md)
> **Tanda:** 0.4 (puertos y contratos) + la primera tarea de la tanda 1 (comandos de apertura y recolección)
> **Plazo:** una semana, del miércoles 30 de septiembre al **martes 6 de octubre**, con **dos entregas**: la A el **jueves 1 de octubre** y la B el **martes 6 de octubre**, las dos al final de tu jornada. El calendario está al final.
> **En paralelo:** Ruben hace la tarea 6, el repositorio Firebird (`infra/garfb`), **contra los puertos que tú escribes**.

## Por qué esta tarea y por qué ahora

Con el PR #21 el dominio queda completo: el agregado ya sabe abrir un folio, mover artículos y dejar su evento en la misma llamada. Pero todavía no hay forma de que nadie lo use: no existe ningún puerto, ni siquiera el del repositorio. **Todo lo que queda del módulo se apoya en los puertos**: el repositorio los implementa, los comandos los consumen y el HTTP llama a los comandos.

Esta tarea tiene dos mitades, y **las dos se entregan por separado a propósito**:

- **Entrega A — los puertos y lo que les falta al dominio.** Es chica, pero **bloquea a Ruben**: su repositorio no compila sin las interfaces que tú escribes. Por eso sale primero y sola.
- **Entrega B — los comandos de apertura y recolección.** Son los primeros de `app/`, y el molde con el que se van a escribir los otros dos grupos (diagnóstico y ruta, cierre y entrega). Lo que decidas aquí se copia dos veces: vale la pena hacerlo bien.

---

## Leer antes de escribir (obligatorio, en este orden)

1. **El spec, §2.1, §2.2, §3.3, §5 y §7.** El §5 lista los puertos; el §7, los permisos; el §2.2 explica por qué el permiso no se puede importar de `auth`.
2. **`internal/flota/ports/outbound/repo.go`.** Es el otro módulo sellado del repo y ya resolvió lo que tú vas a resolver: un `TxRunner` y un `Clock` propios, declarados como puertos, sin importar nada de fuera. Cópiale la **forma** y el estilo de los comentarios.
3. **`internal/platform/firebird/transaction.go`.** Lo necesitas para entender qué hace `RunInTx` (es reentrante) y por qué `*firebird.TxManager` va a satisfacer tu `TxRunner` sin que escribas un adaptador.
4. **`internal/ventas/app/`**, el `service.go` y un comando cualquiera, como referencia de forma de un servicio con puertos inyectados. **No** te lleves el outbox: garantías no publica nada (decisión 3 de la tarea 4, sigue en pie).
5. **Tu propio `garantia.go`,** releyendo `buildEvent` y `ActorParams`: la entrega A los toca.

---

## Las decisiones ya tomadas — no las adivines

Si alguna te parece equivocada, dilo **antes** de escribir, no en el reporte.

1. **Las firmas de los puertos están fijadas en este brief, letra por letra.** Ruben las está programando en paralelo contra este mismo texto. Si cambias un nombre, un tipo o el orden de un parámetro, rompes su tarea. Si una firma te parece mal, **me lo dices y la cambiamos en los dos briefs a la vez**; no la cambies tú.
2. **Dos métodos de escritura en el repositorio, no uno.** `Crear` para un folio que no existe y `Guardar` para uno que se cargó. El agregado no sabe si es nuevo, y un `Guardar` que lo adivinara con un `SELECT` previo sería una consulta de más en cada escritura y una carrera en la apertura.
3. **Todo comando que modifica un folio lo carga con candado.** `ObtenerParaActualizar` toma `WITH LOCK` sobre la fila del folio dentro de la transacción. Sin eso, dos teléfonos que muevan dos artículos distintos del mismo folio al mismo tiempo se pisan: el segundo `Guardar` reescribe el artículo del primero con la etapa vieja, y **los dos eventos quedan guardados**. El expediente diría que el artículo avanzó y la fila diría que no. Es exactamente la mentira que el §4.4 prohíbe.
4. **El permiso se verifica en `app`, no en `http`.** El puerto `Identity` existe para eso. El HTTP va a tener su barrido de seguridad por ruta (tanda 2), pero la regla vive en el comando, que es quien sabe qué acción está pidiendo.
5. **El permiso es un tipo del dominio,** `domain.Permiso`, con sus cinco constantes. No puede vivir en el paquete raíz `garantias`: `module.go` va a vivir ahí e importar los puertos, y si los puertos importaran el paquete raíz habría un ciclo.
6. **Una clave de idempotencia repetida no es un error: es una repetición.** El teléfono sincroniza dos veces por mala señal y reenvía la misma `ClaveIdempotencia`. El comando responde **éxito con el estado actual del folio**, sin volver a aplicar nada (§3.3). La única excepción: si la clave ya existe **en otro folio**, eso sí es un error (`ErrClaveIdempotenciaDeOtroFolio`), porque ahí el teléfono está confundido y no conviene esconderlo.
7. **`RolDecisor` y el GPS del evento entran por `ActorParams`** y cierran el pendiente 10 del review de la tarea 4. `RolDecisor` es **obligatorio** en los tres eventos de decisión: `RegistrarDiagnostico`, `AutorizarCambioFisico` y `RegistrarDesenlace`. En los demás es opcional y, si viene, se guarda. El GPS es opcional siempre, pero **o vienen los dos o ninguno**, y cada uno en su rango.
8. **`Imagen` entra ahora como entidad,** porque `ImagenRepo` la necesita para tener firma. Es chica: sin transiciones y sin hijas.
9. **Un agregado se guarda una vez.** El comando lo carga, lo muta, lo guarda y lo descarta. El repositorio no vacía `eventosPendientes` después de guardar, y ningún comando reutiliza un agregado ya guardado. Anótalo en el comentario del servicio.

---

## Entrega A — puertos, contratos y lo que le falta al dominio

### A.1 — `internal/garantias/ports/outbound/repos.go`

**Letra por letra.** Los comentarios los puedes mejorar; las firmas no.

```go
// ListarGarantiasFiltros narrows the bandeja. Every field is optional; nil
// means "no filter". Etapa and Ubicacion match a folio when AT LEAST ONE of
// its articles is in that stage or place.
type ListarGarantiasFiltros struct {
	Estado    *domain.EstadoFolio
	Origen    *domain.OrigenFolio
	ClienteID *int
	Etapa     *domain.Etapa
	Ubicacion *domain.Ubicacion
	Desde     *time.Time // CREATED_AT >= Desde
	Hasta     *time.Time // CREATED_AT <  Hasta
}

// Paginacion is the keyset page request. Cursor is opaque and empty on the
// first page.
type Paginacion struct {
	Cursor string
	Limite int
}

// Pagina is one page of results. SiguienteCursor is empty on the last page.
type Pagina[T any] struct {
	Items           []T
	SiguienteCursor string
}

// GarantiaRepo persists the folio aggregate: header, articles and the
// pending timeline events, always together.
type GarantiaRepo interface {
	// Crear inserts a folio that does not exist yet: the header, every
	// article and every pending event. Must run inside a transaction.
	Crear(ctx context.Context, g *domain.Garantia) error
	// Guardar persists a folio loaded with ObtenerParaActualizar: updates
	// the header, updates each existing article, inserts the new ones and
	// inserts every pending event. Must run inside a transaction.
	// Returns domain.ErrGarantiaNoEncontrada when the header row is gone.
	Guardar(ctx context.Context, g *domain.Garantia) error
	// ObtenerParaActualizar loads the folio with its articles and locks the
	// header row (SELECT ... WITH LOCK) until the transaction ends. Must run
	// inside a transaction.
	ObtenerParaActualizar(ctx context.Context, id uuid.UUID) (*domain.Garantia, error)
	// Obtener loads the folio with its articles, without locking.
	Obtener(ctx context.Context, id uuid.UUID) (*domain.Garantia, error)
	// ObtenerPorFolio is Obtener by the human-readable folio.
	ObtenerPorFolio(ctx context.Context, folio domain.Folio) (*domain.Garantia, error)
}

// BandejaRepo is the read side of the folio list. It is split from
// GarantiaRepo because the commands never list, and because it ships in a
// later task than the write side.
type BandejaRepo interface {
	// Listar returns the bandeja, newest first (CREATED_AT DESC, ID DESC),
	// each folio with its articles.
	Listar(ctx context.Context, f ListarGarantiasFiltros, p Paginacion) (Pagina[*domain.Garantia], error)
}

// EventoRepo reads the timeline. It is read-only by design (spec §5):
// events are written only through GarantiaRepo, together with the change
// they record.
type EventoRepo interface {
	// ListarPorGarantia returns the folio's timeline ordered by
	// DEVICE_CREATED_AT, then CREATED_AT, then ID.
	ListarPorGarantia(ctx context.Context, garantiaID uuid.UUID) ([]*domain.Evento, error)
	// ObtenerPorClaveIdempotencia returns the event carrying that key, or
	// (nil, nil) when none does.
	ObtenerPorClaveIdempotencia(ctx context.Context, clave string) (*domain.Evento, error)
}

// ImagenRepo persists evidence metadata. The blob itself goes through
// StorageProvider.
type ImagenRepo interface {
	Registrar(ctx context.Context, img *domain.Imagen) error
	ListarPorEvento(ctx context.Context, eventoID uuid.UUID) ([]*domain.Imagen, error)
}

// FolioGenerator hands out the next folio number from GEN_MSP_GA_FOLIO.
// Numbers are never reused, so a rolled-back transaction leaves a gap.
type FolioGenerator interface {
	Siguiente(ctx context.Context) (int, error)
}
```

Todos los "no encontrado" son **`domain.ErrGarantiaNoEncontrada`**, y el choque de la clave de idempotencia es **`domain.ErrClaveIdempotenciaDuplicada`** (A.4).

### A.2 — `internal/garantias/ports/outbound/tx.go`, `clock.go`, `identity.go`

```go
// tx.go
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// clock.go
type Clock interface{ Now() time.Time }
type ProductionClock struct{}
func (ProductionClock) Now() time.Time { return time.Now().UTC() }

type IDGenerator interface{ Nuevo() uuid.UUID }
type UUIDGenerator struct{}
func (UUIDGenerator) Nuevo() uuid.UUID { return uuid.New() }

// identity.go
type Usuario struct {
	ID     string
	Nombre string
}
type Identity interface {
	// UsuarioActual returns the authenticated user, or
	// domain.ErrUsuarioNoAutenticado.
	UsuarioActual(ctx context.Context) (Usuario, error)
	TienePermiso(ctx context.Context, p domain.Permiso) (bool, error)
}
```

`*firebird.TxManager` satisface `TxRunner` tal cual: **no escribas adaptador**. El adaptador de `Identity` es de la tanda 2 y no es tuyo.

### A.3 — `internal/garantias/domain/permiso.go` y `imagen.go`

**`Permiso`**: enum cerrado con la forma `Parse`/`IsValid`/`String`, como `etapa.go`. Tiene los cinco valores del §7: `garantias:leer`, `garantias:crear`, `garantias:actualizar`, `garantias:autorizar` y `garantias:cerrar`.

**`Imagen`**: entidad con `NewImagen(p NewImagenParams) (*Imagen, error)` y `HydrateImagen(p HydrateImagenParams) *Imagen`. Sus campos salen de `MSP_GA_IMAGEN`: `id`, `eventoID`, `ruta`, `descripcion`, `subidaPor` y `createdAt`. `ruta` y `subidaPor` son obligatorios. `ruta` se rechaza si es absoluta o si trae `..`: es una ruta relativa dentro de `STORAGE_DIR`, y el dominio no debe aceptar algo que se salga de ahí. Es **inmutable**: sin setters, como el evento.

### A.4 — Cambios en el dominio que ya existe

- **`ActorParams`** gana `GPSLat`, `GPSLon *float64` y `RolDecisor *RolDecisor`. `buildEvent` los pasa al evento. `EventoParams` gana `GPSLat`/`GPSLon`, y `newEvento` valida los rangos y la regla de los dos o ninguno.
- **Los tres eventos de decisión** rechazan con `ErrRolDecisorObligatorio` si `RolDecisor` es `nil`, **antes de mutar nada**. Aplica el mismo patrón de siempre: construir el evento primero, mutar después, encolar al final.
- **Centinelas nuevos en `errors.go`** (sólo agregar):

| Centinela | Tipo `apperror` | Código |
|---|---|---|
| `ErrGarantiaNoEncontrada` | `NewNotFound` | `warranty_not_found` |
| `ErrClaveIdempotenciaDuplicada` | `NewConflict` | `warranty_idempotency_key_duplicate` |
| `ErrClaveIdempotenciaDeOtroFolio` | `NewConflict` | `warranty_idempotency_key_other_folio` |
| `ErrRolDecisorObligatorio` | `NewValidation` | `warranty_decision_role_required` |
| `ErrEventoGPSInvalido` | `NewValidation` | `warranty_event_gps_invalid` |
| `ErrPermisoInvalido` | `NewValidation` | `warranty_permission_invalid` |
| `ErrPermisoDenegado` | `NewForbidden` | `warranty_permission_denied` |
| `ErrUsuarioNoAutenticado` | `NewUnauthorized` | `warranty_user_unauthenticated` |
| `ErrImagenRutaInvalida` | `NewValidation` | `warranty_image_path_invalid` |
| `ErrImagenSubidaPorObligatorio` | `NewValidation` | `warranty_image_uploader_required` |

Los mensajes, en español, en minúsculas y sin punto final. **Los códigos, en inglés puro**: fue el menor 9 de tu review, no lo repitas.

### A.5 — `internal/garantias/garantias_contracts.go`

Lo único que el módulo expone hacia fuera. Por el §2.2 es la costura con `auth`: el composition root lo va a leer para registrar los cinco códigos en el catálogo de permisos.

```go
// PermisoInfo describes one permission this module defines, for the
// composition root to register in the auth catalog.
type PermisoInfo struct {
	Codigo      string
	Descripcion string
}

// Permisos returns the five permission codes of spec §7, in a stable order.
func Permisos() []PermisoInfo
```

La descripción, en español: es lo que va a ver oficina en la pantalla de roles.

### Entrega A — pruebas

- `domain`: sigue en **≥ 99%** (hoy está en 100%). Cada centinela nuevo con su caso probado con `errors.Is`. Los tres eventos de decisión sin rol se rechazan **sin mutar y sin encolar**: las dos mitades, como en el review.
- `garantias_contracts.go`: una prueba que diga que los cinco códigos de `Permisos()` se parsean con `domain.ParsePermiso`. Si mañana alguien agrega un sexto permiso en un solo lado, esa prueba es la que lo caza.
- Los puertos son interfaces: no llevan pruebas propias. `ProductionClock` y `UUIDGenerator` sí, una línea cada uno.

---

## Entrega B — comandos de apertura y recolección

### B.1 — `internal/garantias/app/service.go`

Un `Service` con sus dependencias inyectadas **como interfaces de `ports/outbound`**: `GarantiaRepo`, `EventoRepo`, `FolioGenerator`, `TxRunner`, `Identity`, `Clock` e `IDGenerator`. Un `NewService(deps Deps) *Service`, sin `fx` todavía (`module.go` es de la tanda 3).

### B.2 — El molde de un comando que modifica un folio

Éste es el molde que van a copiar los otros dos grupos. **Escríbelo una vez, en un helper privado del servicio**, no once veces a mano. Fue la observación de fondo de tu review: la invariante no se sostiene por disciplina si hay una sola firma por la que pasan todos.

```
1. usuario := Identity.UsuarioActual          → ErrUsuarioNoAutenticado
2. Identity.TienePermiso(permiso del comando)  → ErrPermisoDenegado
3. TxRunner.RunInTx:
   a. ev := EventoRepo.ObtenerPorClaveIdempotencia(clave)
      - ev != nil y ev.GarantiaID() == id  → repetición: salir sin mutar
      - ev != nil y es de otro folio       → ErrClaveIdempotenciaDeOtroFolio
   b. g := GarantiaRepo.ObtenerParaActualizar(id)
   c. mutar g (el método del dominio)
   d. GarantiaRepo.Guardar(g)
4. Si 3 devolvió ErrClaveIdempotenciaDuplicada (la misma clave en dos
   peticiones a la vez: el paso 3a no la vio porque la otra aún no hacía
   commit) → tratar como repetición.
5. Devolver el folio con GarantiaRepo.Obtener(id), fuera de la transacción.
```

El paso 4 no es teórico: con mala señal, el teléfono reintenta antes de que llegue la primera respuesta. Pruébalo con un fake que devuelva `ErrClaveIdempotenciaDuplicada` desde `Guardar`.

`ActorParams` lo arma el servicio: `Usuario` sale de `Identity` (**nunca** del comando: si viniera del teléfono, cualquiera firmaría como otro), mientras que `ClaveIdempotencia`, `DeviceCreatedAt`, el GPS y `RolDecisor` salen del comando. `now` sale de `Clock`.

### B.3 — Los comandos, un archivo cada uno

| Archivo | Método | Permiso | Qué hace |
|---|---|---|---|
| `abrir_garantia.go` | `AbrirGarantia(ctx, cmd) (*domain.Garantia, error)` | `crear` | `FolioGenerator.Siguiente` → `domain.NewFolio` → `domain.AbrirGarantia` → `GarantiaRepo.Crear`, **todo dentro de la misma transacción**. `AbiertoPor` = nombre del usuario. La repetición por clave devuelve el folio que abrió esa clave |
| `agregar_articulo.go` | `AgregarArticulo(ctx, cmd) (*domain.Garantia, error)` | `crear` | molde B.2 con `g.AgregarArticulo` |
| `avanzar_articulo.go` | `AvanzarArticulo(ctx, cmd) (*domain.Garantia, error)` | `actualizar` | molde B.2 con `g.AvanzarArticulo`. Cubre la recolección: `registrado → pendiente_recoleccion → recolectado → en_revision` |
| `iniciar_proceso.go` | `IniciarProceso(ctx, cmd) (*domain.Garantia, error)` | `actualizar` | molde B.2 con `g.IniciarProceso` |

Cada `cmd` es un struct propio del archivo (`AbrirGarantiaCmd`, …) con **tipos primitivos o del dominio**, nunca DTOs de HTTP. Los strings que llegan de fuera (`origen`, `etapa`, `estado_cuenta`, `rol_decisor`) se parsean en el comando con el `Parse` del VO, y el error del `Parse` se devuelve tal cual.

**Fuera de esta tarea**, para que no te extiendas: diagnóstico, dictamen, cambio físico, desenlace, entregar, cerrar, cancelar y todas las consultas. Son otras tareas, y dos tareas nunca escriben el mismo archivo.

### Entrega B — pruebas

En `package app_test`, con **fakes en memoria** de los puertos, en `fakes_test.go`. **No uses Firebird**: la integración contra la base es de la tanda 3, y el repositorio real es de Ruben.

Por cada comando:

- El camino feliz: lo que devuelve y lo que quedó en el fake del repositorio, incluido el evento con `Usuario` sacado de `Identity`.
- Sin usuario → `ErrUsuarioNoAutenticado`. Sin permiso → `ErrPermisoDenegado`. En los dos casos, **el fake del repositorio no recibió ninguna escritura**: compruébalo, no lo supongas.
- La repetición por clave (paso 3a) y la repetición por carrera (paso 4): éxito, y **cero escrituras nuevas**.
- La clave de otro folio → `ErrClaveIdempotenciaDeOtroFolio`, sin escrituras.
- El error del dominio (una transición inválida, por ejemplo) se propaga con su centinela y **no llama a `Guardar`**.
- Que todo pase dentro de `RunInTx`: el fake de `TxRunner` registra si la escritura ocurrió dentro o fuera. Una escritura fuera de la transacción es el defecto más caro de esta capa y el más fácil de no ver.

**Cobertura ≥ 90%** en `internal/garantias/app` (el piso del §8).

---

## Archivos que puedes tocar

```
Entrega A
internal/garantias/ports/outbound/repos.go
internal/garantias/ports/outbound/tx.go
internal/garantias/ports/outbound/clock.go
internal/garantias/ports/outbound/identity.go
internal/garantias/domain/permiso.go
internal/garantias/domain/imagen.go
internal/garantias/domain/garantia.go        (sólo ActorParams y buildEvent)
internal/garantias/domain/evento.go          (sólo GPS en EventoParams y su validación)
internal/garantias/domain/errors.go          (sólo agregar centinelas)
internal/garantias/garantias_contracts.go
+ un _test.go por cada archivo nuevo, y los que ya existen de lo que tocaste

Entrega B
internal/garantias/app/service.go
internal/garantias/app/abrir_garantia.go
internal/garantias/app/agregar_articulo.go
internal/garantias/app/avanzar_articulo.go
internal/garantias/app/iniciar_proceso.go
internal/garantias/app/*_test.go
docs/superpowers/plans/garantias-task-5-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** En particular: **ninguna migración**, nada en `infra/` (es de Ruben), nada en `internal/auth/` y nada en `.golangci.yml`.

---

## Verification

Con la caché de tests limpia. Todos tienen que devolver 0:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/ ./internal/garantias/app/
go tool cover -func=cov.out | grep -E 'garantias/(domain|app)' | tail -5
make check-sealed MODULE=garantias
```

`go tool cover -func` sobre un perfil con dos paquetes da **un solo total**. Si quieres ver cada piso por separado (domain ≥ 99%, app ≥ 90%), corre la prueba de cada paquete con su propio `-coverprofile` y pega **las dos** salidas.

---

## Reporte

`docs/superpowers/plans/garantias-task-5-report.md`, **uno solo que crece**: en la entrega A tiene la sección A, y en la B le agregas la B. Cada sección lleva la salida **literal** de los comandos, sin parafrasear (fue el menor 14 del review), y lo que entregaste de verdad. Si algo del brief no lo hiciste o lo hiciste distinto, dilo ahí con el porqué: una desviación declarada se discute, y una escondida se rechaza.

---

## Calendario y entrega

| Cuándo | Qué |
|---|---|
| Martes 29 (hoy) | Lectura del brief y del spec. |
| Miércoles 30 | Entrega A: puertos, `Permiso`, `Imagen`, `ActorParams`. |
| **Jueves 1 de octubre, fin de tu jornada** | **Entrega A.** PR contra `main` con la sección A del reporte. Ruben está esperando esto: si el miércoles ves que no llegas, **dímelo el miércoles**. |
| Viernes 2 y lunes 5 | Entrega B. Empuja la rama al final de cada jornada: no es entrega y no lleva mensaje. |
| **Martes 6 de octubre, fin de tu jornada** | **Entrega B.** Los cuatro comandos, sus pruebas y el reporte completo. |

La B tiene tres jornadas y está apretada a propósito: el molde de B.2 es lo que cuesta, y los cuatro comandos son casi iguales una vez que existe. Si el lunes 5 ves que no llegas, dímelo el lunes, no el martes.

La B va en una rama nueva desde `main`, **con la A ya fusionada**. Si la A todavía está en revisión cuando empiezas la B, parte de la rama de la A y rebasea cuando se fusione.

## Qué significa "terminado"

Que los comandos de Verification devuelvan 0 y las coberturas no bajen de sus pisos. Y, en la B, que el molde de B.2 exista **una sola vez**: si al revisar encuentro los pasos 1 a 5 copiados en cuatro archivos, la entrega rebota aunque todo esté verde.

Si necesitas algo, dime.
