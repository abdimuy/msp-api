# Garantías — Tarea 5 — Reporte

Entrega A y Entrega B, cada una con su sección. Cada sección lleva la salida literal de sus gates y sus desviaciones declaradas.

## Status

DONE — los cinco gates de la Entrega A dan 0. Sin commit ni push: pendiente tu revisión.

## Archivos

| Archivo | Cambio |
|---|---|
| `internal/garantias/ports/outbound/repos.go` | Nuevo. `ListarGarantiasFiltros`, `Paginacion`, `Pagina[T]`, `GarantiaRepo` (5 métodos), `BandejaRepo`, `EventoRepo`, `ImagenRepo`, `FolioGenerator`. Firmas literales del brief. |
| `internal/garantias/ports/outbound/tx.go` | Nuevo. `TxRunner.RunInTx`. |
| `internal/garantias/ports/outbound/clock.go` | Nuevo. `Clock` + `ProductionClock`, `IDGenerator` + `UUIDGenerator`. |
| `internal/garantias/ports/outbound/identity.go` | Nuevo. `Usuario`, `Identity`. |
| `internal/garantias/ports/outbound/clock_test.go` | Nuevo. UTC de `ProductionClock`, v4 y unicidad en 1000 tiradas de `UUIDGenerator`, y `var _` de que las implementaciones satisfacen sus interfaces. |
| `internal/garantias/domain/permiso.go` | Nuevo. `Permiso` + 5 constantes + `ParsePermiso`/`IsValid`/`String`. |
| `internal/garantias/domain/permiso_test.go` | Nuevo. Los 5 códigos exactos, `Parse` de cada uno, rechazo de lo que no está en el catálogo, `String` ida y vuelta. |
| `internal/garantias/domain/imagen.go` | Nuevo. `Imagen` inmutable, `NewImagen`/`HydrateImagen`, `rutaRelativa` rechaza vacía, absoluta o con `..`. |
| `internal/garantias/domain/imagen_test.go` | Nuevo. Rutas válidas y las tres formas de escape, `SubidaPor` obligatorio, hidratación sin validar, inmutabilidad. |
| `internal/garantias/domain/garantia.go` | Modificado. `ActorParams` gana `RolDecisor`, `GPSLat`, `GPSLon`; `buildEvent` los pasa; los tres eventos de decisión rechazan sin rol. |
| `internal/garantias/domain/evento.go` | Modificado. `EventoParams` gana `GPSLat`/`GPSLon`; `newEvento` valida con `gpsValido`. |
| `internal/garantias/domain/errors.go` | Modificado. Los 10 centinelas de la tabla del brief, sólo agregados. |
| `internal/garantias/domain/garantia_test.go` | Modificado. `actorDecisor` para los caminos válidos, y las pruebas nuevas de rol y GPS. |
| `internal/garantias/garantias_contracts.go` | Nuevo. `PermisoInfo` + `Permisos()` con los 5 códigos en orden estable. |
| `internal/garantias/garantias_contracts_test.go` | Nuevo. Catálogo completo contra el spec, cada código pasa por `domain.ParsePermiso`, orden estable, sin slice aliasado. |

Nada fuera de la lista del brief. Cero migraciones, nada en `infra/`, nada en `internal/auth/`, nada en `.golangci.yml`.

## Pruebas

`go test -race -count=1 ./internal/garantias/...` — 4 paquetes, todos `ok`.

Lo nuevo que cubre el 100% que pedía el brief:

- **Rol obligatorio.** Los tres eventos de decisión (diagnóstico, cambio físico, desenlace) rechazan con `ErrRolDecisorObligatorio` y la prueba comprueba las dos mitades por separado: el artículo no se movió y la cola de pendientes no creció. Un rechazo que encolara dejaría la línea de tiempo diciendo que pasó algo que no pasó. En los eventos que no son de decisión el rol es opcional, y las dos variantes (con y sin) están probadas.
- **GPS.** Pareja completa, ninguno, cada mitad solo, fuera de rango, `NaN` e `Inf`; los bordes `±90`/`±180` se aceptan. La validación vive en `newEvento`, así que la prueba entra por una mutación y comprueba que tampoco muta ni encola al rechazar. Hay además una prueba de que `AbrirGarantia` pasa por la misma validación.
- **`Imagen`.** `ruta` es obligatoria y relativa dentro de `STORAGE_DIR`: se rechazan la vacía, la absoluta y la que trae `..` como segmento (no como subcadena, que rechazaría de más un nombre legítimo).

## Verificación

Con `go clean -testcache` antes de la corrida con cobertura.

| Gate | Resultado |
|---|---|
| `gofmt -l internal/garantias` | sin salida |
| `go vet ./internal/garantias/...` | limpio |
| `go build ./...` | limpio |
| `golangci-lint run ./internal/garantias/...` | `0 issues.` |
| `go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/` | `ok 2.426s coverage: 100.0% of statements` |
| `go tool cover -func=cov.out \| tail -5` | `total: (statements) 100.0%` |
| `make check-sealed MODULE=garantias` | `garantias is sealed`, exit 0 |

El `go test` del brief pide `./internal/garantias/domain/ ./internal/garantias/app/`. `app/` no existe todavía: es justo lo que construye la Entrega B. Corrí el gate de `domain/` solo, que es lo que esta entrega puede mover. El piso de 90% de `app` queda para B.

## `check-sealed`: por qué costó correrlo, y cómo se corrió

El brief lo pide (línea 323) y la línea 352 lo cuenta como parte de "terminado". En esta máquina fallaba con exit 2, y la causa no era el código.

**Causa.** El recipe de `Makefile:80-100` es shell POSIX puro: `for...done`, `[ ]`, `$$()`, `sed`, `grep`. GNU Make en Windows necesita un `sh.exe` en el PATH; si no lo encuentra cae a `cmd.exe`, que no entiende esa sintaxis y aborta antes de ejecutar nada. Esta máquina **sí tiene Git Bash** (`C:\Program Files\Git\bin\bash.exe`, bash 5.2.37), pero sólo `C:\Program Files\Git\cmd` está en el PATH, y esa carpeta trae `git.exe` pero **no** `sh.exe`. Faltaba `C:\Program Files\Git\usr\bin`.

**Salida literal** de `make check-sealed MODULE=garantias` corrido desde Git Bash:

```
✔ garantias is sealed
```

exit 0.

Sobre el glifo: en la terminal el `Makefile:97` emite `✔` (U+2714), y en esta máquina de Windows sale doble-codificado. Lo que la consola pinta es `â€”` en lugar del check. Es la codificación de la consola, no el resultado del chequeo: el `od -c` del comando da

```
0000000 303 242 305 223 342 200 235       g   a   r   a   n   t   i   a
0000020       i   s       s   e   a   l   e   d  \n
```

`303 242 305 223 342 200 235` es el `✔` de `Makefile:97` pasado dos veces por latin-1. Lo que importa del gate es que `garantias` está sellado y el comando devuelve 0.

Como control, corrí el target completo (los cuatro módulos sellados, sin `MODULE=`):

```
· internal/asistencia does not exist yet — skipped
✔ garantias is sealed
✔ flota is sealed
✔ canal is sealed
```

exit 0. `asistencia` todavía no existe en el repo y el target lo salta por diseño.

**Pendiente de infraestructura, no de esta entrega.** Mientras el líder revisa, la idea es agregar `C:\Program Files\Git\usr\bin` al PATH del usuario para que el comando funcione en PowerShell sin depender de abrir Git Bash. Nota para quien lo haga: el PATH de usuario de esta máquina ya mide 1284 caracteres, así que **hay que usar `[Environment]::SetEnvironmentVariable('Path', ..., 'User')` y nunca `setx`**, que trunca en 1024 y se llevaría por delante nvm, Go, Docker y Chocolatey. Requiere abrir una terminal nueva después.

## Dos notas (ninguna bloquea la entrega)

### 1. `Descripcion` y el linter — se queda como está

`PermisoInfo.Descripcion` está escrito así porque el brief lo fija letra por letra (línea 205), y así lo usa el resto del repo (`internal/inventario`, `internal/ventas`, `internal/cobranza`).

`misspell` lo marca como `Description` mal escrito, y lo marca **precisamente porque** `descripcion` no está en `ignore-rules` de `.golangci.yml` — de hecho la palabra no aparece en ninguna parte de ese archivo. Esa lista es la excepción: le dice al linter qué palabras dejar en paz; lo que no está en ella, lo corrige. Por eso las palabras ya agregadas ahí (`inventario`, `transito`, `observacion`) no producen aviso y la mía sí.

La prueba está en el gate: antes de poner los `//nolint`, `golangci-lint run ./internal/garantias/...` reportó 15 problemas, 13 de ellos `misspell`. Con los `//nolint` en su lugar, `0 issues.`. Si la palabra ya estuviera ignorada, el `nolint` no habría hecho falta.

Lo dejé resuelto con `//nolint:misspell` a nivel de archivo en `garantias_contracts.go` e `imagen.go`, que es lo que ya hace `internal/inventario/inventario_contracts.go` para el mismo `Descripcion`, y no se toca nada fuera de la lista del brief.

**Por qué la excepción local sí se queda, y no es una desviación.** El brief exige las dos cosas: la línea 205 fija el nombre del campo como `Descripcion`, y la línea 320 exige que `golangci-lint run ./internal/garantias/...` devuelva 0. La excepción local es la única salida — la alternativa sería agregar la palabra a `.golangci.yml`, que la línea 308 prohíbe explícitamente.

**Recomendación** (para una tarea aparte, no aquí): agregar `descripcion` y `Descripcion` a `ignore-rules` en `.golangci.yml`, junto a las entradas que ya existen para el mismo problema (`inventario`, `transito`, `observacion`). Con eso se pueden borrar los dos `nolint`.

### 2. Cobertura de `infra/storage` en 83.8% — no aplica a esta tarea

Lo anoté porque se ve al correr la suite del módulo:

- El 85% sale del **spec §8** (línea 328), no del brief.
- El brief no lo menciona. Lo único que dice de coberturas es el 90% de `app` (línea 278).
- El comando de cobertura del brief (línea 321) corre `./internal/garantias/domain/` y `./internal/garantias/app/`. `infra/storage` ni siquiera se mide.

El paquete da 83.8% hoy. 

## Correcciones del review (PR #24)

El líder marcó un bloqueante y cuatro menores en la revisión de esta entrega. Se corrigieron en este mismo PR (sin commit todavía, como la entrega):

- **Bloqueante — el catálogo llegaba a la DB.** `EstadoCuenta("basura")` y `RolDecisor("gerencia")` pasaban de largo porque nadie los validaba al construir: `AbrirGarantia` los guardaba tal cual y `newEvento` los incrustaba en el evento. Ahora `AbrirGarantia` rechaza `EstadoCuenta` no válido con `ErrEstadoCuentaInvalido` y `newEvento` rechaza un `RolDecisor` presente pero fuera del catálogo con `ErrRolDecisorInvalido`, antes de mutar nada. Las pruebas de los tres eventos de decisión comprueban las dos mitades: el artículo no se movió ni se encoló evento.
- **Menor — anchos de columna.** Se agregaron los centinelas de longitud que la migración ya impone y el dominio no: `AbiertoPor`/`Usuario`/`SubidaPor` ≤ 64, `Ruta`/`Descripcion` de imagen ≤ 500, `Clave` de artículo ≤ 30 (contados en caracteres, como los `VARCHAR(n)` UTF8). Cada uno con prueba de rechazo por encima y de aceptación en el límite.
- **Menor — `ClaveIdempotencia` es un UUID.** El teléfono la genera como UUID y la columna es `CHAR(36)`; `newEvento` ahora la valida con `uuid.Parse`. Los helpers de prueba dejaron de usar `"clave-1"` y cargan un UUID real.
- **Menor — `NewImagen`.** Rechaza `EventoID == uuid.Nil` y `CreatedAt` cero, los dos con su centinela.

Verificación tras las correcciones (mismos gates de la tabla de arriba, `go test -race -count=1`):

- `golangci-lint run ./internal/garantias/...` → `0 issues.` (se añadió un `//nolint:misspell` puntual por `DESCRIPCION`, mismo vocabulario español ya documentado en la nota 1).
- `go test ./internal/garantias/domain/ -cover` → `100.0%`.
- `go test -race -short ./internal/garantias/...` → 4 paquetes `ok`.
- `make check-sealed MODULE=garantias` → `garantias is sealed`.

## Correcciones del review, ronda 2 (PR #24)

El líder revisó `c932ce9` y dio dos puntos nuevos (el bloqueante y los cuatro menores anteriores quedaron cerrados: mató los seis mutantes, incluidos los límites exactos):

- **`ClaveIdempotencia` admitía cuatro grafías y se guardaba tal cual.** `uuid.Parse` acepta `36`, `{38}`, `urn:uuid:45` y `32` dígitos sin guiones. Almacenar el texto crudo haría que la misma petición entrara dos veces con cuatro valores distintos para el `UNIQUE` y la idempotencia dejara de funcionar en silencio. `newEvento` ahora guarda `parsed.String()`: siempre 36 caracteres minúsculas, canónico y dentro de la columna. Prueba `TestClaveIdempotencia_FormaCanonica`: las cuatro grafías más la minúscula convergen en la misma clave, de longitud 36.
- **Siete columnas más sin control de largo.** `validarDomicilioLargo` (llamado en `AbrirGarantia` después de `validarOrigenCliente` y de `validarOrigenPiso`) y el `newArticulo` ahora miden en caracteres, como los `VARCHAR(n)` UTF8: `Calle` 300, `NumeroExterior` 20, `Colonia`/`Localidad`/`Ciudad` 100, `CodigoPostal` 10, `Description` de artículo 300 — cada uno con su centinela, así el mensaje dice cuál campo falló.

### Ejercicio contra la migración 000050: cada `VARCHAR`

| Columna | Ancho | Cubierta por |
|---|---|---|
| `MSP_GA_GARANTIA.FOLIO` | 12 | no se captura: lo genera `GEN_MSP_GA_FOLIO` en Go |
| `.ORIGEN` | 10 | `OrigenFolio.IsValid()` |
| `.ESTADO_CUENTA` | 20 | `EstadoCuenta.IsValid()` (ronda 1) |
| `.ESTADO` | 24 | máquina de estados, ya cerrada |
| `.CALLE` | 300 | ronda 2 |
| `.NUMERO_EXTERIOR` | 20 | ronda 2 |
| `.COLONIA` / `.LOCALIDAD` / `.CIUDAD` | 100 | ronda 2 |
| `.CODIGO_POSTAL` | 10 | ronda 2 |
| `.ABIERTO_POR` | 64 | ronda 1 |
| `MSP_GA_ARTICULO.ROL` / `.RUTA` / `.ETAPA` / `.UBICACION` / `.DICTAMEN` / `.DESENLACE` | 12–28 | enums cerrados (`IsValid`) |
| `.CLAVE` | 30 | ronda 1 |
| `.DESCRIPCION` | 300 | ronda 2 |
| `MSP_GA_EVENTO.TIPO` / `.ETAPA_*` / `.ROL_DECISOR` | 16–28 | enums cerrados; `ROL_DECISOR` validado en ronda 1 |
| `.USUARIO` | 64 | ronda 1 |
| `.CLAVE_IDEMPOTENCIA` | `CHAR(36)` | `uuid.Parse` + forma canónica (ronda 2) |
| `MSP_GA_IMAGEN.RUTA` / `.DESCRIPCION` | 500 | ronda 1 |
| `.SUBIDA_POR` | 64 | ronda 1 |
| `DESCRIPCION` de folio y de evento | — | `BLOB`, sin límite |

Verificación de la ronda 2 (mismos gates): `gofmt`/`go vet`/`go build` limpios, `golangci-lint run ./internal/garantias/...` → `0 issues.`, `go test -race -count=1 ./internal/garantias/...` → 4 paquetes `ok`, dominio `100.0%`, `make check-sealed MODULE=garantias` → `garantias is sealed`.

---

## Entrega B — comandos de apertura y recolección

### Status

DONE — los gates de la B dan 0 y las coberturas están sobre sus pisos (domain 100.0% ≥ 99%, app 94.3% ≥ 90%). Sin commit ni push: pendiente tu revisión.

### Archivos

Todos dentro de la lista del brief (líneas 299-304): 5 de producción + 8 de prueba en `package app_test`. Nada fuera.

| Archivo | Qué es |
|---|---|
| `internal/garantias/app/service.go` | `Deps`, `Service`, `NewService(deps Deps) *Service` (sin `fx`). El molde B.2 completo vive aquí **una sola vez**: `ejecutarFolio`. Ayudantes `usuario`, `exigirPermiso`, `actorDe`. |
| `internal/garantias/app/abrir_garantia.go` | `AbrirGarantia` + `AbrirGarantiaCmd` + `actorDeApertura`. La excepción al molde: no hay folio que bloquear, pero todo el alta corre en una sola transacción. |
| `internal/garantias/app/agregar_articulo.go` | `AgregarArticulo` + `AgregarArticuloCmd`. Molde B.2 con `g.AgregarArticulo`. |
| `internal/garantias/app/avanzar_articulo.go` | `AvanzarArticulo` + `AvanzarArticuloCmd`. Molde B.2 con `g.AvanzarArticulo`. |
| `internal/garantias/app/iniciar_proceso.go` | `IniciarProceso` + `IniciarProcesoCmd`. Molde B.2 con `g.IniciarProceso`. |
| `internal/garantias/app/fakes_test.go` | Fakes de `TxRunner`, `Identity`, `Clock` y helpers `seedFolio`, `hydrateEvento`, `firstArticuloID`. |
| `internal/garantias/app/fakes_repo_test.go` | `fakeGarantiaRepo` (con `onSave` para la carrera real), `fakeEventoRepo`, `fakeFolioGen`, `recordingRepo` y los clones. |
| `internal/garantias/app/transaccion_test.go` | `TestEscrituraDentroDeTransaccion`: cada `Crear`/`Guardar` ocurre con la transacción abierta. |
| `internal/garantias/app/errores_test.go` | `TestErroresDePuertoSePropagan`: cada error de puerto sale del comando tal cual. |
| 4 × `{comando}_test.go` | 3 de apertura (el 4º, de clave repetida, vive en `iniciar_proceso_test.go`), 7 de `agregar`, 7 de `avanzar`, 8 de `iniciar` (7 + el de apertura). |

### Qué entregaste de verdad

**El molde B.2, una sola vez.** `ejecutarFolio(ctx, garantiaID, permiso, actor, mutar)` (`service.go:69-119`) es los cinco pasos en una sola firma:

1. `usuario := s.usuario(ctx)` — de `Identity.UsuarioActual`, nunca del comando; el error (p. ej. `ErrUsuarioNoAutenticado`) se propaga tal cual.
2. `s.exigirPermiso(ctx, permiso)` — `Identity.TienePermiso`; si no, `ErrPermisoDenegado`.
3. `s.tx.RunInTx`:
   a. `s.eventos.ObtenerPorClaveIdempotencia(clave)` (`service.go:86-98`): `nil` sigue; `vista.GarantiaID() == garantiaID` → repetición, `return nil` sin mutar; de otro folio → `ErrClaveIdempotenciaDeOtroFolio`.
   b. `s.garantias.ObtenerParaActualizar(id)`.
   c. `mutar(ctx, g, actor)` — el método del dominio, único argumento que cambia por comando.
   d. `s.garantias.Guardar(g)`.
4. Si `Guardar` devuelve `ErrClaveIdempotenciaDuplicada` → `return nil`, se trata como repetición (`service.go:108-112`).
5. Fuera de la tx: `s.garantias.Obtener(id)` y se devuelve (`service.go:118`).

Los tres comandos de folio son el cascarón del archivo: `cmd` propio + `ejecutarFolio(ctx, cmd.GarantiaID, permiso, actorDe(...), closure)`. Lo único distinto es el cierre (`g.AgregarArticulo` / `g.AvanzarArticulo` / `g.IniciarProceso`) y el permiso según la tabla B.3: `crear`, `actualizar`, `actualizar`. No existe un segundo archivo que copie los pasos 3-5: si lo buscas, `ObtenerParaActualizar` aparece una vez en producción (`service.go:100`) y `RunInTx` dos (`service.go:85` y `abrir_garantia.go:46`).

**`AbrirGarantia` no es el molde porque aún no existe el folio.** Todo el alta corre dentro de la misma transacción (`abrir_garantia.go:46-94`), como pide la línea 256: `FolioGenerator.Siguiente` → `domain.NewFolio` → `domain.AbrirGarantia` → `GarantiaRepo.Crear`. `AbiertoPor` = `actor.Usuario`, que sale de `Identity` (nunca del comando). La clave repetida —ya visible antes de escribir, o reventando `Crear` con `ErrClaveIdempotenciaDuplicada` (la carrera del paso 4)— se trata como el éxito que es: se relee el folio que abrió esa clave y se devuelve (vía `ObtenerPorClaveIdempotencia` → `Obtener`). Si después de una carrera la clave ya no aparece en el repo de eventos, devuelve `ErrClaveIdempotenciaDuplicada`, el mismo sentinel del paso 4.

**`ActorParams` lo arma el servicio.** `Usuario` sale de `Identity` (paso 1 del molde; `actorDeApertura` hace lo mismo), `ClaveIdempotencia`, `DeviceCreatedAt`, `GPS` salen del comando (`actorDe`, `service.go:147-154`) y `now` sale de `Clock`. Ningún comando de esta tanda lleva `RolDecisor` (no hay comando de decisión en el alcance): es la base de la Desviación 1.

**`IDGenerator` está en `Deps`** como pide B.1 (`service.go:23`). Ningún comando de la B lo usa todavía: los eventos los construye el dominio (`buildEvent`, Entrega A) y estos cuatro comandos no generan IDs propios. Entra como dependencia para tandas posteriores.

### Pruebas

27 `func Test` en 8 archivos, `package app_test`, fakes en memoria, sin Firebird (línea 267).

Por cada comando de folio:
- Camino feliz: folio devuelto y estado del fake (artículos, transición, guardado único, `tx.calls == 1`).
- Sin usuario → `ErrUsuarioNoAutenticado` y **cero escrituras** (`tx.calls == 0`, `saved == 0`, `created == 0` — se comprueba, no se supone).
- Sin permiso → `ErrPermisoDenegado`, cero escrituras. El fake niega un único permiso a la vez (`id.denegar = &permCrear`).
- Repetición por clave (paso 3a): éxito y `saved == 0`.
- **Repetición por carrera (paso 4): éxito y `saved == 0`.** No es un replay barato: `fakeGarantiaRepo.onSave` registra el evento del gemelo **después** de que la pre-comprobación pasó vacía y justo antes de devolver el error (`fakes_repo_test.go:52-60`), así que el comando cae en `Guardar → ErrClaveIdempotenciaDuplicada → repetición`. Sin `onSave` el test tocaría la rama 3a y dejaría el paso 4 verde pero nunca ejercitado.
- Clave de otro folio → `ErrClaveIdempotenciaDeOtroFolio`, sin escrituras.
- El rechazo del dominio sin guardar: `AgregarArticulo` con descripción vacía, `AvanzarArticulo` con transición inválida, `IniciarProceso` con folio sin artículos.

`AbrirGarantia` (4 pruebas en total, la 4ª en `iniciar_proceso_test.go:154`): camino feliz (folio con `AbiertoPor` = nombre de `Identity`, folio asignado una vez, `created == 1`), sin usuario, sin permiso, y `TestAbrirGarantia_ClaveRepetida_ReusaElFolio`.

Corte transversal: `TestEscrituraDentroDeTransaccion` con `recordingRepo` — toda escritura ocurre con la tx abierta, la invariante del agregado por la que el molde existe; y `TestErroresDePuertoSePropagan` — cada sentinel de puerto se propaga sin ser tragado.

### Verificación

Con `go clean -testcache` antes de la corrida con cobertura.

| Gate | Resultado |
|---|---|
| `gofmt -l internal/garantias` | sin salida |
| `go vet ./internal/garantias/...` | limpio |
| `go build ./...` | limpio |
| `golangci-lint run ./internal/garantias/...` | `0 issues.` |
| `go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/ ./internal/garantias/app/` | `ok  .../domain 2.576s coverage: 100.0% of statements`<br>`ok  .../app 2.440s coverage: 94.3% of statements` |
| `go tool cover -func=cov.out \| grep -E 'garantias/(domain\|app)' \| tail -5` | las 5 últimas líneas son de `domain/` (el `tail` corta del final y `app/` queda arriba en el orden alfabético): `total: (statements) 100.0%` |
| `make check-sealed MODULE=garantias` | `✔ garantias is sealed`, exit 0 (el glifo puede salir doble-codificado en la consola de Windows, igual que en la Entrega A) |

Los dos pisos por separado, como sugiere la nota de la línea 326 (el `tail -5` del comando compuesto sólo muestra la cola de la lista):

```
go test -race -count=1 -coverprofile=domain.out ./internal/garantias/domain/
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	2.607s	coverage: 100.0% of statements
```

```
go test -race -count=1 -coverprofile=app.out ./internal/garantias/app/
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.467s	coverage: 94.3% of statements
```

Pisos del brief: domain 99% → 100.0%; app 90% → 94.3%. Los `cov.out`, `domain.out` y `app.out` se borraron al terminar.

### Desviaciones declaradas

1. **Los `Cmd` traen el VO ya tipado, no el `string` + `Parse`.** El brief (línea 261) pide que "los strings que llegan de fuera (origen, etapa, estado_cuenta, rol_decisor) se parsean en el comando". Entregado: `AbrirGarantiaCmd.Origen` es `domain.OrigenFolio`, `.EstadoCuenta` es `*domain.EstadoCuenta`, `AvanzarArticuloCmd.EtapaDestino` es `domain.Etapa` — el `Parse` del VO queda para el adapter HTTP de la tanda 3, que ni existe, y el error del `Parse` se devolverá tal cual allí. Por qué: la misma línea permite en el `cmd` "tipos primitivos **o del dominio**", y sin un transport que mande `string` no hay nada que parsear en el servicio. Si lo prefieres literal, son tres campos y sus tests, ~30 min.
2. **Los pasos 1-2 (usuario + permiso) están escritos dos veces.** En `ejecutarFolio` y —más cortos— en `actorDeApertura`. El criterio de "terminado" (línea 352) es que los pasos 1-5 **del molde** existan una sola vez, y eso se cumple: los pasos 3-5 viven únicamente en `ejecutarFolio` y los tres comandos de folio pasan por esa única firma. `AbrirGarantia` no puede usarlo: no hay folio que bloquear, y el propio brief le da un flujo distinto (línea 256). La duplicación son ~6 líneas; se puede extraer a un `resolverActor(ctx, permiso)` si lo pides.
3. **Fakes en dos archivos, no uno.** El brief (línea 267) dice fakes en `fakes_test.go`. Entregados en `fakes_test.go` + `fakes_repo_test.go` (el repo grande, con `onSave` y `recordingRepo`). Es organización interna de un paquete de test, pero lo declaro para que no parezca otra cosa.
4. **El `Usuario` del evento no es observable en `app`.** El camino feliz comprueba el estado del folio y del fake; el campo `Usuario` del evento lo imprime el dominio con `actor.Usuario` (probado en Entrega A) pero la garantía no expone sus eventos pendientes (no hay acceso al campo privado y el dominio está sellado), así que a nivel `app` el único punto donde el nombre de `Identity` es observable es `AbiertoPor` en la apertura, que sí se comprueba. En los tres comandos de folio no hay nada legible por el puerto para asertarlo; quedará cubierto end-to-end cuando el repo real persista eventos.

### Nota de rama (no es desviación)

La B nació de `feat/garantias-puertos` (la rama de la A) y no de `main`, porque la A sigue en revisión. El brief lo prevé expresamente (línea 348): "Si la A todavía está en revisión cuando empiezas la B, parte de la rama de la A y rebasea cuando se fusione". Se rebasará sobre `main` cuando la A se fusione.

## Report Path

`docs/superpowers/plans/garantias-task-5-report.md`
