# Garantías — Tarea 4: entidades del dominio y el agregado

> **Rama:** `feat/garantias-base` (la misma, encima de lo que ya entregaste). Rebasea sobre `main` en cuanto entre el PR #10.
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md)
> **Tanda:** 0.3c — cierra el paquete `domain/`
> **Plazo:** entrega el **lunes 24 al final de tu jornada**. Ver el calendario al final.

## Dónde encaja

Los diez value objects y la tabla de transiciones que entregaste son las piezas. Esto es lo que las usa y lo que las obliga a servir para algo.

`Garantia` es el folio vivo en memoria: la raíz del agregado. `Articulo` es la cosa física bajo custodia, con su propio ciclo de vida. `Evento` es la línea de tiempo, y es lo que hace que el expediente sirva como evidencia si el cliente reclama.

Después de esto sólo queda `GA 0.4` (puertos y contratos) y se abre la tanda 1 completa — cinco tareas en paralelo. Hoy el módulo entero está esperando esta pieza.

---

## Lo primero, y es exactamente lo que acabas de aprender al revés

**Para las entidades, `New`/`Hydrate` sí es el patrón correcto.**

Hace dos semanas te hice reconvertir siete value objects para quitarles `New`/`Hydrate`/`Value`/`Equals`/`IsZero`, y estuvo bien: eran enums cerrados y les tocaba `Parse`/`IsValid`/`String`. **No apliques esa lección aquí.** `docs/module-standards/AGGREGATE_PATTERNS.md` es explícito para entidades:

> - **`NewX(...)` / `CrearX(p XParams)`** valida cada entrada y devuelve `(*X, error)`.
> - **`HydrateX(p HydrateXParams) *X`** omite la validación. Sólo lo usa el repositorio al reconstruir desde filas persistidas.

La regla corta: **enums cerrados → `Parse`/`IsValid`/`String`; entidades y VOs multi-campo → `New`/`Hydrate`.** Son dos capas distintas del estándar, cada una con su forma.

---

## Leer antes de escribir (obligatorio, en este orden)

1. **`docs/module-standards/AGGREGATE_PATTERNS.md`** — completo. Es el documento que gobierna esta tarea: dos constructores, campos privados, un getter por campo, sin setters, constructores package-private para las hijas, `iter.Seq` para las colecciones.
2. **`internal/ventas/domain/venta.go`** — el agregado de referencia del repositorio: raíz con cuatro colecciones hijas, constructores package-private, iteradores de sólo lectura. Como referencia de **forma**, no de contenido.
3. **El spec, secciones §3.1 a §3.5, §4.1, §4.4 y §5.** El §4.4 es la invariante que gobierna todo el diseño de esta tarea; el §5 dice qué espera el repositorio del agregado.
4. **Tu propio `transiciones.go` y `estado_folio.go`.** Las entidades se apoyan en `CanTransitionTo`; **no dupliques ninguna máquina de estados dentro de las entidades.**
5. **`migrations-firebird/000050_create_msp_ga_garantias.up.sql`** — la tuya. Cada campo privado sale de una columna, y los `NOT NULL` de ahí son los que la entidad tiene que hacer imposibles de violar.

---

## Las nueve decisiones ya tomadas — no las adivines

El brief anterior te pedía avisar cuando algo del diagrama no cerrara, y con razón: se te fue una y costó una revisión. Esta vez las cerré yo de antemano. Si alguna te parece equivocada, dilo **antes** de escribir las pruebas, no después.

1. **Entran tres entidades:** `Garantia`, `Articulo` y `Evento`. **`Imagen` no entra** — cuelga del evento, tiene su propio puerto (`ImagenRepo`) y su propia tarea.
2. **Los métodos de transición entran en esta tarea.** Una entidad sin ellos no hace nada y las pruebas no probarían nada.
3. **El buffer de eventos se persiste en la misma transacción.** Ojo con el nombre: en `ventas`, `pendingEvents` son eventos de dominio que se drenan **después** del commit, hacia el outbox. **Aquí no es eso.** `MSP_GA_EVENTO` es una tabla del expediente y el §4.4 exige que la fila se guarde junto con el cambio de etapa. Un solo buffer, `eventosPendientes []*Evento`, que `GarantiaRepo.Guardar` persistirá en la misma transacción. **Garantías no publica al outbox.** No copies el patrón de `ventas` por inercia.
4. **El estado del folio se mueve explícito, no derivado.** El comando pide la transición y el agregado la **rechaza** si los artículos no la permiten. La máquina del folio es la de tu `estado_folio.go`: `abierto → en_proceso → listo_entrega → entregado → cerrado`, con `cancelado` colgando de los dos primeros.
   La guardia que importa está en **`listo_entrega`**: sólo se permite cuando **cada artículo del folio está en `listo_entrega`, en `standby`, o en una etapa terminal**. Ni uno puede quedar en el camino del cliente. Eso es exactamente lo que permite el requisito del §3.2 —folio `cerrado` con el original todavía en `standby`— sin dejar que el folio cierre con un artículo olvidado en el taller.
5. **`origen = piso`** ⇒ `clienteID`, `ventaID`, `estadoCuenta` y domicilio **nil, y se rechazan si vienen**. **`origen = cliente`** ⇒ los cuatro obligatorios; GPS opcional. Un folio de cliente siempre nace de una venta identificada.
6. **El folio lo formatea el dominio.** Un VO `Folio` en `folio.go`: recibe el **entero** que dará `FolioGenerator.Siguiente()` y formatea `GA-%06d`; `ParseFolio(s)` valida el formato para hidratar. El puerto todavía no existe (`GA 2.4`) — tú recibes el entero como parámetro.
7. **El agregado no carga la línea de tiempo histórica.** Sólo acumula los eventos nuevos. El histórico se lee por `EventoRepo.ListarPorGarantia`, que es un puerto aparte de sólo lectura. Un folio con cuarenta eventos no debe cargarse entero para avanzar una etapa.
8. **`claveIdempotencia` y `deviceCreatedAt` son parámetros obligatorios de quien crea el evento.** El dominio **no** los inventa: la clave la genera el teléfono y es lo que hace viable el offline-first. Cuando el evento nace en el servidor, la capa de aplicación pasa un UUID nuevo y `now` — pero eso lo decide ella, no la entidad.
9. **Un artículo puede reemplazarse más de una vez.** El reemplazo también puede salir malo. Por eso `Articulo` lleva `reemplazaA *uuid.UUID` (nil en los originales). La columna `REEMPLAZA_A` llega en la migración `000057`, que escribo yo — **tú no toques ninguna migración**, sólo el campo en la entidad.

---

## Entregable 1 — `internal/garantias/domain/garantia.go`

### Campos

Uno por columna de `MSP_GA_GARANTIA` (§3.1), **todos privados**: `id`, `folio` (`Folio`), `origen` (`OrigenFolio`), `clienteID`, `ventaID`, `estadoCuenta` (`*EstadoCuenta`), `estado` (`EstadoFolio`), `descripcion`, `vigenciaHasta` (`*time.Time`), el domicilio (`calle`, `numeroExterior`, `colonia`, `localidad`, `ciudad`, `codigoPostal`), `gpsLat`/`gpsLon` (`*float64`), `abiertoPor`, `cerradoEn` (`*time.Time`).

Embebe **`audit.Timestamped`**, no `audit.Auditable`: `Auditable` guarda `createdBy`/`updatedBy` como `uuid.UUID`, y aquí `ABIERTO_POR` es un `VARCHAR(64)` con el nombre del operador. Ese campo va como campo propio de la entidad.

Colecciones hijas: `articulos []*Articulo` y `eventosPendientes []*Evento`. Se leen con `iter.Seq`, nunca devolviendo el slice.

### Constructores

```go
func AbrirGarantia(p AbrirGarantiaParams) (*Garantia, error)  // valida; nace en abierto
func HydrateGarantia(p HydrateGarantiaParams) *Garantia       // sin validar, sólo el repositorio
```

`AbrirGarantia` genera el `id` con `uuid.New()`, recibe `now` como parámetro (nunca `time.Now()` dentro del dominio), nace en `EstadoFolioAbierto` y **deja encolado el evento de apertura**. `vigenciaHasta` se acepta tal cual: el §9.2 dice que no se valida ninguna política de vigencia, ni siquiera que sea futura.

### Métodos

| Método | Qué hace | Encola evento |
|---|---|---|
| `AgregarArticulo(p, now)` | crea la hija en etapa `registrado`, ubicación según el origen | `articulo_agregado` |
| `AvanzarArticulo(articuloID, hasta, p, now)` | delega en el artículo; si la transición no es válida, no muta nada | `etapa_avanzada` |
| `RegistrarDiagnostico(articuloID, ruta, p, now)` | fija `RUTA` | `diagnostico_registrado` |
| `RegistrarDictamen(articuloID, dictamen, p, now)` | sólo ruta proveedor | `dictamen_registrado` |
| `AutorizarCambioFisico(articuloID, p, now)` | manda el original a `standby` y **crea la fila `reemplazo`** en `listo_entrega`, con `reemplazaA` apuntando al original | `cambio_autorizado` |
| `RegistrarDesenlace(articuloID, desenlace, p, now)` | desde `standby` | `desenlace_registrado` |
| `IniciarProceso(p, now)` | folio `abierto → en_proceso`; exige al menos un artículo | `etapa_avanzada` |
| `MarcarListoEntrega(p, now)` | folio `en_proceso → listo_entrega`; **aquí vive la guardia de la decisión 4** | `etapa_avanzada` |
| `Entregar(p, now)` | folio `listo_entrega → entregado` | `folio_entregado` |
| `Cerrar(p, now)` | folio → `cerrado`; fija `cerradoEn` | `folio_cerrado` |
| `Cancelar(motivo, p, now)` | folio → `cancelado` (terminal) | `folio_cancelado` |

**Ningún método muta sin comprobar la transición primero**, contra `estado_folio.go` para el folio y contra `transiciones.go` para el artículo. Si la transición no procede, devuelve el centinela y **el agregado queda exactamente como estaba** — ni el estado a medias, ni el evento encolado.

Todos actualizan `UpdatedAt`.

### La invariante del §4.4

**No hay cambio de etapa sin evento, y los dos ocurren en la misma llamada.** El agregado llena `etapaDesde` y `etapaHasta` del evento; no los recibe de quien llama. Si escribes un camino que mueva un artículo sin encolar su evento, esa es la falla que esta tarea existe para hacer imposible.

---

## Entregable 2 — `internal/garantias/domain/articulo.go`

Entidad hija. **Constructor package-private** (`newArticulo`), como manda el estándar: sólo la raíz puede crearla, así nunca existe un artículo huérfano.

Campos de `MSP_GA_ARTICULO` (§3.2), privados: `id`, `garantiaID`, `rol` (`RolArticulo`), `reemplazaA` (`*uuid.UUID`), `articuloID` (`*int`), `clave`, `descripcion`, `ruta` (`*RutaReparacion`), `etapa` (`Etapa`), `ubicacion` (`Ubicacion`), `dictamen` (`*Dictamen`), `desenlace` (`*Desenlace`), `cerradoEn`. Embebe `audit.Timestamped`.

`articuloID` y `clave` son nullable a propósito: un mueble viejo puede no tener referencia en Microsip. `descripcion` nunca lo es.

**Validaciones cruzadas — las tres son centinelas propios:**

- `dictamen` no se puede fijar si `ruta != proveedor`.
- `desenlace` no se puede fijar si la etapa no es terminal.
- no se puede salir de `en_revision` sin `ruta` fijada.

---

## Entregable 3 — `internal/garantias/domain/evento.go`

Entidad hija, **inmutable**: sin setters, sin `UPDATED_AT`, sin borrado. Se crea y no se toca más. Si algo salió mal, se agrega un evento de corrección (§3.3).

Campos: `id`, `garantiaID`, `articuloRef` (`*uuid.UUID`), `tipo` (`TipoEvento`), `descripcion`, `etapaDesde`/`etapaHasta` (`*Etapa`), `usuario`, `rolDecisor` (`*RolDecisor`), `gpsLat`/`gpsLon`, `createdAt`, `deviceCreatedAt`, `claveIdempotencia`. **No embebe `audit.*`**: la tabla sólo tiene `CREATED_AT`, así que ni `Timestamped` aplica.

**Además, un catálogo nuevo: `TipoEvento`** en `tipo_evento.go`, Enum VO con la forma de siempre. Trece valores, uno por hecho registrable:

```
folio_abierto · articulo_agregado · etapa_avanzada · diagnostico_registrado ·
dictamen_registrado · cambio_autorizado · desenlace_registrado ·
folio_entregado · folio_cerrado · folio_cancelado · evidencia_adjuntada ·
correccion · nota
```

El más largo es `diagnostico_registrado` (22) y la columna es `VARCHAR(28)`: cabe.

---

## Pruebas

En `package domain_test`, caja negra, tabla-driven, con el mismo rigor que las que ya entregaste.

- `AbrirGarantia` con entradas válidas y **cada validación rechazada con su centinela**, verificado con `errors.Is`. Incluye los dos orígenes y los campos que cada uno prohíbe.
- **Cada método de transición desde cada estado**: el válido que funciona, y los inválidos que devuelven centinela **sin mutar nada**. Comprueba las dos mitades: que `Estado()` siga igual **y que no se haya encolado un evento**. Esa segunda mitad es la que prueba la invariante del §4.4.
- Que `AutorizarCambioFisico` deje el original en `standby`, cree el reemplazo en `listo_entrega` con `reemplazaA` apuntando al original, y que **se pueda encadenar**: reemplazar el reemplazo produce una tercera fila.
- La guardia de la decisión 4, en `MarcarListoEntrega`: un folio con el original en `standby` y el reemplazo en `listo_entrega` **sí** pasa; el mismo folio con un tercer artículo en `en_taller` **no**, y al rechazarlo no debe quedar ni el estado movido ni el evento encolado.
- Que `HydrateGarantia` reconstruya sin validar, con un caso de basura a propósito.

**Cobertura ≥ 99%** en `internal/garantias/domain`. Hoy está en 100%; no la bajes.

---

## Archivos que puedes tocar

```
internal/garantias/domain/garantia.go
internal/garantias/domain/articulo.go
internal/garantias/domain/evento.go
internal/garantias/domain/folio.go
internal/garantias/domain/tipo_evento.go
internal/garantias/domain/errors.go          (sólo agregar centinelas)
+ un _test.go por cada archivo nuevo
docs/superpowers/plans/garantias-task-4-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** En particular: **ninguna migración**, ni la `000050` ni la `000057`; no toques los value objects ya entregados, ni `transiciones.go`, ni `.golangci.yml`.

---

## Verification

Con la caché de tests limpia. Los siete tienen que devolver 0:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/
go tool cover -func=cov.out | tail -1        # ≥ 99.0%
make check-sealed MODULE=garantias
```

`go clean -testcache` no es opcional: un `ok` cacheado no prueba que tu cambio corrió.

---

## Reporte

`docs/superpowers/plans/garantias-task-4-report.md`, con la salida **literal** de esos comandos pegada y una sección que describa lo que entregaste de verdad. Si el reporte no coincide con el código, gana el código: corrige el reporte. Y revisa los números antes de mandarlo — en el anterior quedó un "25" donde había 24.

---

## Calendario y puntos de control

| Cuándo | Qué |
|---|---|
| **Jueves 20, fin de jornada** | **Punto de control: `garantia.go` y `articulo.go` escritos, antes de sus pruebas.** Mándamelos. Es donde salen las preguntas, y contestarlas con el archivo en la mano cuesta minutos; después de sesenta casos de prueba, cuesta rehacerlos. |
| Viernes 21 | `evento.go`, `folio.go`, `tipo_evento.go` y los centinelas. |
| **Lunes 24, fin de jornada** | **Entrega.** |

El plazo sale de tu propio ritmo: los catálogos más el mapa te tomaron tres días. Esto es más grande, y por eso el punto de control va al segundo día y no al tercero.

Si te atoras más de dos horas en una sola cosa, avisa. Y si alguna de las nueve decisiones de arriba no te cuadra con el spec, dilo antes de escribir las pruebas — esta vez las cerré yo, pero eso no las vuelve correctas.
