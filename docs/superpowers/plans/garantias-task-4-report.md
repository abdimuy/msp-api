# Garantías — Tarea 4: reporte

> **Rama:** `feat/garantias-dominio` — commit `96ca3e3` base del PR #21; los
> fixes del review (4 bloqueantes + menores) están aplicados y aún sin commitear;
> el commit final se hará cuando se cierre toda la revisión.
> **Tanda:** 0.3c — paquete `domain/` del módulo garantías

## Qué se entregó

El paquete `internal/garantias/domain/` pasa de ser trece VOs sin uso a tener el
aggregate completo con su línea de tiempo:

- **`garantia.go`** — `Garantia` con `AbrirGarantia` (valida, nace en `abierto`,
  encola `folio_abierto`) e `HydrateGarantia` (sin validar, sólo repositorio); los
  once métodos de transición de la tabla del brief; `eventosPendientes` como única
  cola, persistida por `GarantiaRepo` en la misma transacción (nada al outbox);
  `Articulos()` y `EventosPendientes()` por `iter.Seq` (los pares `*ForRepo`
  devuelven una **copia** del slice para el repositorio).
- **`articulo.go`** — `Articulo` hija, constructor package-private `newArticulo`,
  mutadores internos (los cinco actualizan `UpdatedAt`); las tres validaciones
  cruzadas del brief y los helpers de destino `diagnosticoTarget` /
  `dictamenTarget` / `desenlaceTarget`.
- **`evento.go`** — `Evento` inmutable: se crea con `newEvento` (punto único de
  validación, §4.4) y no se toca más; `HydrateEvento(HydrateEventoParams)` sin
  validar para el repositorio.
- **`folio.go`** — VO `Folio`: formatea `GA-%06d` y `ParseFolio` valida para
  hidratar (puerto `FolioGenerator` todavía no existe).
- **`tipo_evento.go`** — enum `TipoEvento` con los trece valores del brief. El
  wire value de corrección es `correction` (no `correccion`), con `//nolint:misspell`
  file-level como en `internal/ventas/domain/imagen.go:1`.
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

Reproducido con la caché de tests limpia, lunes 21 de septiembre, tras cerrar el
review del PR #21:

```text
PS> gofmt -l internal/garantias
(sin salida)
PS> echo exit=0

PS> go vet ./internal/garantias/...
(sin salida)
PS> echo exit=0

PS> go build ./...
(sin salida)
PS> echo exit=0

PS> golangci-lint run ./internal/garantias/...
0 issues.
PS> echo exit=0

PS> go clean -testcache
PS> go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/
ok  github.com/abdimuy/msp-api/internal/garantias/domain  2.583s  coverage: 100.0% of statements
PS> echo exit=0

PS> go tool cover -func=cov.out | Select-Object -Last 1
total:                                     (statements)   100.0%
```

```text
== 7) make check-sealed MODULE=garantias ==
NO EJECUTADO POR SISTEMA: `make` no está instalado en esta máquina Windows.
Equivalente manual con la misma definición del target (Makefile:80-97):

$ leaked=$(go list -deps ./internal/garantias/... | grep 'msp-api/internal/'
        | grep -v 'msp-api/internal/garantias\|msp-api/internal/platform' | sort -u)
(sin fugas) — garantias is sealed (ADR 0009).  equivalente exit=0
```

`golangci-lint` corre también sobre los demás paquetes del módulo
(`infra/storage`, `ports/outbound`), preexistentes e intactos.

## Cobertura

`go tool cover -func` deja el paquete al **100.0%**. El review señaló la única
rama que quedaba al 99.8% — el fallo de `newArticulo` al crear el reemplazo —
como la señal del bloqueante 1: viva por el camino de `HydrateArticulo` (una fila
con `DESCRIPCION` en blanco), no por el de `AgregarArticulo`. El fix reordenó
`AutorizarCambioFisico` (el reemplazo se construye **antes** de mover al
original) y `TestAutorizarCambioFisico/reemplazo_falla_sin_mutacion` cubre la
rama con un artículo hidratado sin descripción: falla el sentinel, el folio queda
intacto.

## Desviaciones deliberadas

Las que el brief deja al criterio o la máquina de estados fuerza:

1. **Swap encadenado en la entidad.** La decisión 9 dice que el reemplazo también
   puede salir malo; `transiciones.go` no ofrece salida de `listo_entrega`. El
   atajo vive en `autorizarCambioFisico` (articulo.go): si el rol es reemplazo y
   la etapa es `listo_entrega`, el swap se hace directo — único caso en que la
   entidad se sale de la máquina, y es exactamente el que la decisión 9 pide.
   Para el original se validan las **aristas que se aplican**: `espera_respuesta_cliente`
   por su arista directa a `standby`; `en_taller` por el camino compuesto
   `en_taller → cambio_autorizado → standby`, cuya guardia efectiva es la arista
   `cambio_autorizado → standby` (la primera arista la garantiza la máquina,
   que es una constante en `transiciones.go`). `standby` y `cambio_autorizado`
   quedan vetados para `AvanzarArticulo` (menor 7 del review): sólo el swap entra
   ahí, y el swap siempre crea el reemplazo.
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
   artículo; el expediente gana legibilidad con un solo hecho por decisión. El
   evento registra el swap real: `desde` es la etapa del original (`en_taller`,
   `espera_respuesta_cliente` o `listo_entrega` en el carve-out) y `hasta` es
   `standby`; para `en_taller` el par describe el camino compuesto de la máquina,
   no una arista directa (menor 5 del review).
5. **`registrarDesenlace` recibe el destino ya computado.** La validación
   (incluida la de enum inválido y la de desenlaces no paralelos, `tanda 1`) vive
   en `desenlaceTarget`; el mutador interno sólo aplica la transición. Un solo
   lugar de verdad, sin ramas muertas de un segundo switch. Nota: el brief
   (§Entregable 2) dice "desenlace no se puede fijar si la etapa no es terminal",
   pero su propia tabla de métodos exige `RegistrarDesenlace` "desde `standby`",
   que no es terminal. La contradicción se resolvió a favor de la tabla: el
   desenlace sale de `standby` vía `CanTransitionTo` hacia la etapa terminal.
6. **Piso rechaza el GPS completo (declarado).** El review preguntó por qué
   `validarOrigenPiso` rechaza también las coordenadas cuando el brief sólo
   prohíbe el domicilio. Es deliberado: un folio `piso` no tiene canal de captura
   de coordenadas en el flujo actual; rechazar el GPS entero evita datos muertos.
   Para cliente el GPS sigue opcional (los seis campos de domicilio sí son
   obligatorios — bloqueante 3 del review).
7. **`rolDecisor`, `gpsLat` y `gpsLon` del evento sin escritor (GA 0.4).**
   `ROL_DECISOR` sale NULL en todos los eventos, incluso en
   `AutorizarCambioFisico`, que el spec §3.3 usa como ejemplo canónico de quién
   decide. Los campos existen en el esquema y en `EventoParams`; ningún comando
   captura hoy al decisor ni las coordenadas, así que se dejan nulos y el canal
   de decisión queda para GA 0.4 (menor 10 del review).

## Archivos de esta entrega

```
internal/garantias/domain/garantia.go            (nuevo)
internal/garantias/domain/articulo.go            (nuevo)
internal/garantias/domain/evento.go              (nuevo)
internal/garantias/domain/folio.go               (nuevo)
internal/garantias/domain/tipo_evento.go         (nuevo)
internal/garantias/domain/errors.go              (+26 centinelas, 10 preexistentes → 36; sólo añadidos)
internal/garantias/domain/garantia_test.go       (nuevo)
internal/garantias/domain/articulo_test.go       (nuevo)
internal/garantias/domain/evento_test.go         (nuevo)
internal/garantias/domain/folio_test.go          (nuevo)
internal/garantias/domain/tipo_evento_test.go    (nuevo)
docs/superpowers/plans/garantias-task-4-report.md (este archivo)
```

No se tocaron VOs preexistentes, `transiciones.go`, `.golangci.yml` ni ninguna
migración. `git status` muestra sólo los archivos de arriba.