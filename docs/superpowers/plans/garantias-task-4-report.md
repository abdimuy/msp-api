# Garantías — Tarea 4: reporte

> **Rama:** `feat/garantias-dominio` (sin commit aún; pendiente revisión del brief completo)
> **Tanda:** 0.3c — paquete `domain/` del módulo garantías

## Qué se entregó

El paquete `internal/garantias/domain/` pasa de ser trece VOs sin uso a tener el
aggregate completo con su línea de tiempo:

- **`garantia.go`** — `Garantia` con `AbrirGarantia` (valida, nace en `abierto`,
  encola `folio_abierto`) e `HydrateGarantia` (sin validar, sólo repositorio); los
  once métodos de transición de la tabla del brief; `eventosPendientes` como única
  cola, persistida por `GarantiaRepo` en la misma transacción (nada al outbox);
  `Articulos()` y `EventosPendientesForRepo()` por `iter.Seq`.
- **`articulo.go`** — `Articulo` hija, constructor package-private `newArticulo`,
  mutadores internos; las tres validaciones cruzadas del brief y los helpers de
  destino `diagnosticoTarget` / `dictamenTarget` / `desenlaceTarget`.
- **`evento.go`** — `Evento` inmutable: se crea con `newEvento` (punto único de
  validación, §4.4) y no se toca más; campo `correction` reservado para hechos de
  corrección.
- **`folio.go`** — VO `Folio`: formatea `GA-%06d` y `ParseFolio` valida para
  hidratar (puerto `FolioGenerator` todavía no existe).
- **`tipo_evento.go`** — enum `TipoEvento` con los trece valores del brief.
- **`errors.go`** — los sentineles nuevos, `apperror.New*` con código inglés y
  mensaje en español.

Cinco suites en `package domain_test` (caja negra, tabla-driven, `t.Parallel()`)
que cubren: abrir folio con cada rechazo por origen; cada método de transición
desde cada estado con las **dos mitades** de la invariante §4.4 (estado intacto y
sin evento encolado); el encadenamiento de reemplazos y la guardia de
`MarcarListoEntrega`; hidratación con basura a propósito; y el catálogo de
eventos.

### Invariante §4.4

El evento se **construye antes** de tocar el estado: `buildEvent` valida los NOT
NULL del actor (usuario / clave de idempotencia / `deviceCreatedAt`) sin mutar,
y el append al buffer ocurre sólo después de que la transición fue validada y
aplicada. En ningún método existe una rama que mueva una etapa sin encolar su
evento; una prueba fallida deja el folio exactamente como estaba.

## Verification (salida literal)

Reproducido con la caché de tests limpia, martes 15 del calendario:

```text
== 1) gofmt -l internal/garantias ==
(empty)                          exit=0

== 2) go vet ./internal/garantias/... ==
(no output)                      exit=0

== 3) go build ./... ==
(no output)                      exit=0

== 4) golangci-lint run ./internal/garantias/... ==
0 issues.
                                 exit=0

== 5) go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/ ==
ok  github.com/abdimuy/msp-api/internal/garantias/domain  2.510s  coverage: 99.8% of statements
                                 exit=0

== 6) go tool cover -func=cov.out | tail -1 ==
total:   (statements)   99.8%
```

```text
== 7) make check-sealed MODULE=garantias ==
NO EJECUTADO POR SISTEMA: `make` no está instalado en esta máquina Windows.
Equivalente manual con la misma definición del target (Makefile:80-97):

$ leaked=$(go list -deps ./internal/garantias/... | grep 'msp-api/internal/'
        | grep -v 'msp-api/internal/garantias\|msp-api/internal/platform' | sort -u)
→ vacío.  "garantias is sealed (ADR 0009)".  equivalente exit=0
```

`golangci-lint` corre también sobre los demás paquetes del módulo
(`infra/storage`, `ports/outbound`), preexistentes e intactos.

## Cobertura

`go tool cover -func=cov.out` deja **una sola función por debajo del 100%**:

```text
github.com/abdimuy/msp-api/internal/garantias/domain/garantia.go:323:  AutorizarCambioFisico   94.1%
```

Es `16/17` statements. El statement sin cubrir es la rama de error de
`newArticulo` al crear el reemplazo (garantia.go:337-347): la descripción del
reemplazo se copia de `articulo.Description()` de un original ya validado, así
que no hay forma de que falle — muerto por construcción. Total **99.8% ≥ 99%**.

## Desviaciones deliberadas

Las que el brief deja al criterio o la máquina de estados fuerza:

1. **Swap encadenado en la entidad.** La decisión 9 dice que el reemplazo también
   puede salir malo; `transiciones.go` no ofrece salida de `listo_entrega`. El
   atajo vive en `autorizarCambioFisico` (articulo.go): si el rol es reemplazo y
   la etapa es `listo_entrega`, el `CanTransitionTo(EtapaCambioAutorizado)` se
   omite — único caso en que la entidad se sale de la máquina, y es exactamente
   el que la decisión 9 pide.
2. **Nombres `description` / `correction` en inglés.** El brief escribe
   `descripcion` y `correccion`, pero (a) identificadores y códigos van en inglés
   (regla 3 de CLAUDE.md) y (b) misspell los corrige a `description` /
   `correction` — que es también el wire value de `TipoEventoCorrection`. Los
   mensajes de error siguen en español. Los demás identificadores del módulo que
   no chocan con el diccionario de misspell conservan su forma.
3. **Eventos de folio sin etapas.** Todos los hechos a nivel de folio —
   `folio_abierto`, `folio_entregado`, `folio_cerrado`, `folio_cancelado` y los
   `etapa_avanzada` de `IniciarProceso`/`MarcarListoEntrega` — llevan
   `etapaDesde`/`etapaHasta` nil: el folio no es una `Etapa`, y las etapas del
   evento quedan reservadas para los movimientos de artículo.
4. **Un solo `cambio_autorizado`** por swap (no dos eventos de etapa para
   original y reemplazo a la vez). El documento de diseño habla de uno por
   artículo; el expediente gana legibilidad con un solo hecho por decisión.
5. **`registrarDesenlace` recibe el destino ya computado.** La validación
   (incluida la de enum inválido y la de desenlaces no paralelos, `tanda 1`) vive
   en `desenlaceTarget`; el mutador interno sólo aplica la transición. Un solo
   lugar de verdad, sin ramas muertas de un segundo switch. Nota: el brief
   (§Entregable 2) dice "desenlace no se puede fijar si la etapa no es terminal",
   pero su propia tabla de métodos exige `RegistrarDesenlace` "desde `standby`",
   que no es terminal. La contradicción se resolvió a favor de la tabla: el
   desenlace sale de `standby` vía `CanTransitionTo` hacia la etapa terminal.

## Archivos de esta entrega

```
internal/garantias/domain/garantia.go            (nuevo)
internal/garantias/domain/articulo.go            (nuevo)
internal/garantias/domain/evento.go              (nuevo)
internal/garantias/domain/folio.go               (nuevo)
internal/garantias/domain/tipo_evento.go         (nuevo)
internal/garantias/domain/errors.go              (+23 centinelas, sólo añadidos)
internal/garantias/domain/garantia_test.go       (nuevo)
internal/garantias/domain/articulo_test.go       (nuevo)
internal/garantias/domain/evento_test.go         (nuevo)
internal/garantias/domain/folio_test.go          (nuevo)
internal/garantias/domain/tipo_evento_test.go    (nuevo)
docs/superpowers/plans/garantias-task-4-report.md (este archivo)
```

No se tocaron VOs preexistentes, `transiciones.go`, `.golangci.yml` ni ninguna
migración. `git status` muestra sólo los archivos de arriba.