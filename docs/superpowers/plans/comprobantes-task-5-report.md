# Comprobantes — Tarea 5: reporte

> **Alcance:** entrega completa de la tarea — los once puertos outbound. Incluye el punto de control del viernes 21 (`envio_repo.go`) y el cierre con los diez puertos restantes.
> **Commits:** `ab5941b`, `ba08fb9`, `5882875`, `cb1c9ed`, y el commit de la ronda de revisión que aplica los cuatro cambios que pidió el líder (`errors.go`, `envio_repo.go`, `saldo_reader.go` y este reporte).
> **Rama:** `feat/comprobantes-domain`, encima de `main` (`c06acb4`)
> **Fechas:** checkpoint viernes 2026-08-21 · cierre adelantado antes del martes 25 a pedido del líder.

Escrito abriendo los archivos y contando lo que hay.

## Qué se entregó

1. `ab5941b` — el arreglo de un minuto que pide el brief antes de empezar:
   - `internal/comprobantes/domain/envio.go`: `transitionTo(nuevo EstadoEnvio, now time.Time)` con `e.MarkUpdatedAt(now)`. Las cinco transiciones (`Reclamar`, `MarcarEnviado`, `MarcarFallido`, `Reenviar`, `Detener`) pasan su `now`. Verificado: las cinco llamadas + la firma son las únicas ocurrencias de `transitionTo(` en el archivo.
   - `internal/comprobantes/domain/envio_test.go`: una prueba nueva, `TestReclamar_FijaUpdatedAt` — tras `Reclamar(fecha fija)`, `UpdatedAt()` es esa fecha exacta. Única prueba de la tarea, la que ordena el brief.
2. `ba08fb9` — punto de control del viernes: `internal/comprobantes/ports/outbound/envio_repo.go`.
3. Commit único final — los diez puertos restantes y este reporte:
   - `storage.go`, `sender.go` — copiados tal cual de las secciones «El puerto — copiar exactamente» del brief task-3, comentarios incluidos.
   - `venta_reader.go`, `pago_reader.go`, `saldo_reader.go`, `cliente_reader.go`, `renderer.go`, `cursor_repo.go`, `config_repo.go` — transcritos del brief task-5.
   - `clock.go` — copia del puerto de ventas.

## Los once puertos (contados abriendo los archivos)

| # | Archivo | Tipos | Métodos |
|---|---|---|---|
| 1 | `storage.go` | `StorageObject`, `StorageProvider` | `Store`, `Get`, `Delete` |
| 2 | `sender.go` | `Destino`, `Documento`, `Sender` | `Enviar`, `Canal` |
| 3 | `venta_reader.go` | `DatosVenta`, `ArticuloVenta`, `VentaReader` | `Leer` |
| 4 | `pago_reader.go` | `DatosPago`, `CambioPago`, `PagoReader` | `Cambios`, `Datos` |
| 5 | `saldo_reader.go` | `SaldoReader` | `SaldoRestante` |
| 6 | `cliente_reader.go` | `DatosCliente`, `ClienteReader` | `Leer` |
| 7 | `renderer.go` | `Renderer` | `Venta`, `Pago` |
| 8 | `envio_repo.go` | `FiltroEnvios`, `EnvioRepo` | `Guardar`, `Obtener`, `Listar`, `ReclamarLote`, `DetenerSiEnEspera` |
| 9 | `cursor_repo.go` | `CursorRepo` | `Leer`, `Guardar` |
| 10 | `config_repo.go` | `Config`, `ConfigRepo` | `Leer`, `Actualizar` |
| 11 | `clock.go` | `Clock`, `ProductionClock` | `Now` |

No hay `IDGenerator` (lo suelta el brief: `CrearEnvio` ya genera el UUID). No hay `comprobantes_contracts.go`. No hay implementaciones. Los lectores devuelven structs propios de `outbound`, no modelos de dominio; solo `Renderer` y `EnvioRepo` reciben/devuelven tipos de `domain`.

## Salida literal de las cinco compuertas, sobre el árbol entregado

```
=== gofmt -l internal/comprobantes ===
(sin salida)

=== go vet ./internal/comprobantes/... ===
(exit 0, sin salida)

=== go build ./... ===
(exit 0, sin salida)

=== golangci-lint run ./internal/comprobantes/... ===
0 issues.

=== make check-sealed MODULE=comprobantes ===
✔ comprobantes is sealed
```

Además, la prueba que manda el brief:

```
go test ./internal/comprobantes/... -short -count=1
ok  	github.com/abdimuy/msp-api/internal/comprobantes/domain	1.394s
?   	github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound	[no test files]
```

## Decisiones donde el brief dejaba elegir o tensaba consigo mismo

1. **`clock.go`: una palabra ajustada en el comentario de paquete.** El brief dice «mismo contenido, mismo comentario» (línea 255), pero copiar verbatim habría dejado dentro de comprobantes un comentario que dice *"the interfaces the ventas module needs"*. La línea 16 autoriza ajustar comentarios; se cambió `ventas`→`comprobantes`. Todo lo demás idéntico.
2. **`//nolint:misspell` en `renderer.go` y `venta_reader.go`.** El linter marcó `informativo` (la etiqueta obligatoria del §6.3 que el brief manda citar) y `Descripcion` (campo mandatorio del brief). Es la convención que ya usa `domain/envio.go` para vocabulario español.
3. **`gofmt -w` sobre los archivos existentes de domain.** Necesario para pasar la primera compuerta en este clon. El motivo quedó aclarado en la ronda de revisión (hallazgo 3 corregido): el repo está limpio, era mi `core.autocrlf`; el cambio real es solo fin de línea y esos archivos no entraron en ningún commit.

## Incidente de CI entre el checkpoint y esta entrega

El primer push dejó CI en rojo: `revive package-comments` marcaba `envio_repo.go`. Causa: el comentario de paquete de `outbound` vive en `clock.go`, que en el checkpoint aún no estaba commiteado — el paquete quedaba parcialmente entregado. El lint local había pasado porque veía los once archivos del working tree, no solo los empujados. Resolución: al completar la tarea entra `clock.go` con su comentario y la regla queda satisfecha sin tocar lo ya commiteado.

## Hallazgos para el líder

1. **El spec §7 difiere del brief en `PagoReader.Datos`.** La tabla del spec (`2026-07-29-comprobantes-whatsapp-design.md`, línea 246) dice fuente `MSP_PAGOS_VENTAS`; el brief manda leer de `DOCTOS_CC` y prohibir el caché. Origen: el spec es del 07-29, el defecto `f665c62` es del 08-13 — la tabla quedó anterior al incidente. Se siguió el brief.
2. **Falta una regla estática acotada a `domain`, `app` y `ports`.** El módulo **no** es sellado — el diseño (`2026-07-29-comprobantes-whatsapp-design.md`, línea 29) dice literal: *"No es un módulo sellado: consume contratos de `ventas`, `cobranza` y `clientes` a través de puertos, que es la regla general del repositorio"*. Por eso `comprobantes-sealed` no debe existir: reventaría el día que aterrice `infra/clients`, tarea que el propio brief anuncia. Quien invoca ADR-0009 de más es el brief. `make check-sealed MODULE=comprobantes` verifica un invariante **temporal** —cierto solo mientras el módulo tenga nada más que `domain` y `ports`—: como compuerta de esta tarea sirve, pero no es una regla del módulo. Hoy las únicas reglas estáticas del repo son `domain-pure` (solo `domain/*.go`) y `app-no-infra`; ni una negación del resto de `internal/` que deje `infra/clients` libre. Esa regla y la corrección del brief las escribe el líder, no esta tarea.
3. **El repo no tiene `.gitattributes`, y eso es lo pendiente de verdad.** El repo **no** tiene `domain` commiteado con CRLF — lo verifiqué con `git ls-files --eol`: `i/lf w/lf` en los catorce archivos y blobs sin un solo `\r`. Lo que fallaba en mi clon era mi `core.autocrlf=true` de Windows convirtiendo LF→CRLF al sacar los archivos, y gofmt 1.26 marcándolos después; por eso la compuerta pasa limpia sobre el mismo commit desde fuera. El pendiente real es la falta de `.gitattributes`, que deja la normalización de fin de línea a merced de la config de cada máquina y cualquiera en Windows se topa con esto. No se agrega `.gitattributes` en este PR (lo ve el líder). Como mitigación local se aplicó `git config core.autocrlf input` en el clon.
4. **Fecha del mensaje de asignación.** Dice punto de control «viernes 22»; el brief corregido (`e0a48dc`) dice viernes 21. Se trabajó con la fecha del brief.
5. *(Housekeeping)* Hay archivos sin trackear en la raíz del repo: `$null`, `cov`, `gate-output.txt`, `verificar-comprobantes.*`, dos `.md` de instrucciones. No se tocó nada, solo se nota.

## Verificación de que nada fuera de la lista cambió

Commits de la tarea: `ab5941b` toca `envio.go` y `envio_test.go`; `ba08fb9` crea `envio_repo.go`; `cb1c9ed` crea los diez `.go` restantes bajo `ports/outbound/` y este reporte; el commit de la ronda de revisión toca `errors.go` (extensión autorizada explícitamente por el líder), `envio_repo.go`, `saldo_reader.go` y vuelve a actualizar este reporte. Ningún otro archivo fue commiteado.

Los doce archivos que el `git status` marcaba en `domain` eran solo la conversión de fin de línea de este clon (contenido idéntico al índice, verificado con `git diff`: cero diferencias). Tras `git config core.autocrlf input` y un re-hash del índice quedaron limpios y siguen fuera de todo commit.

## Ronda de revisión — los cuatro cambios que pidió el líder

El líder revisó contra el brief, verificó la transcripción, corrió las compuertas de su lado y aprobó la base de la tarea ("Buen trabajo"). Antes de mergear pidió cuatro cambios:

1. **`envio_repo.go` + `errors.go` — el desenlace normal del duplicado.** Dado que reprocesar un tramo del changelog choca con `UNIQUE(TIPO, REFERENCIA)` y se descarta (§12), la capa de aplicación debía poder distinguir ese choque de una falla real sin inventar su propio centinela. Se agregó `ErrEnvioDuplicado` (`apperror.NewConflict`), se documentó en `Guardar` que el descarte es el camino normal y que la implementación no debe leer-antes-de-escribir, y `Obtener` ahora declara que devuelve `ErrEnvioNoEncontrado` (`apperror.NewNotFound`) cuando el id no existe. `errors.go` fue la única extensión a la lista de archivos que el líder autorizó.
2. **`saldo_reader.go` — la misma advertencia que `PagoReader`.** El §7 del spec —anterior al defecto `f665c62`— también manda este puerto a `MSP_SALDOS_VENTAS`, otra caché que no señalé. Se documentó como exigencia, no como prohibición: el saldo debe ser el **posterior** al pago que se comprueba, y si se lee la caché hay que demostrar que ya se actualizó en la misma transacción (spec §11: "no se supone, se prueba").
3. **Reporte, hallazgo 2 reescrito.** El módulo no es sellado (diseño línea 29); `check-sealed` es un invariante temporal, no una regla de CI. Falta una regla acotada a `domain`/`app`/`ports` que niegue el resto de `internal/` y deje libre `infra/clients`; la escribe el líder.
4. **Reporte, hallazgo 3 reescrito.** El repo no tiene `domain` con CRLF; era mi `core.autocrlf` local. El pendiente real es la falta de `.gitattributes`, fuera del alcance de este PR.

Además, la descripción del PR se actualizó a la entrega completa (dejó de ser la de checkpoint).
