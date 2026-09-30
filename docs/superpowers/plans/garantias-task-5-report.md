# Garantías — Tarea 5 — Reporte

Entrega A solamente. La B (comandos de apertura y recolección) no se empezó.

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
| `go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/` | `ok 2.375s coverage: 100.0% of statements` |
| `go tool cover -func=cov.out \| tail -5` | `total: (statements) 100.0%` |
| `make check-sealed MODULE=garantias` | **no corrió**, ver abajo |

El `go test` del brief pide `./internal/garantias/domain/ ./internal/garantias/app/`. `app/` no existe todavía: es justo lo que construye la Entrega B. Corrí el gate de `domain/` solo, que es lo que esta entrega puede mover. El piso de 90% de `app` queda para B.

## Sobre `check-sealed`

`make check-sealed MODULE=garantias` no se puede ejecutar en esta máquina: el target usa un recipe de shell POSIX y Windows lo rechaza con exit 2, antes de llegar al código. No es un defecto de este cambio.

Corrí el equivalente a mano, que es lo que el target verifica:

```
go list -deps ./internal/garantias/... |
  Select-String 'msp-api/internal/' |
  Where-Object { $_ -notmatch 'msp-api/internal/garantias|msp-api/internal/platform' } |
  Sort-Object -Unique
```

Salida vacía. El módulo sellado no importa nada fuera de sí mismo y de `internal/platform`. Los puertos nuevos sólo importan `context`, `time`, `uuid` y `internal/garantias/domain`.

## Dos notas (ninguna bloquea la entrega)

### 1. `Descripcion` y el linter — se queda como está

`PermisoInfo.Descripcion` está escrito así porque el brief lo fija letra por letra (línea 205), y así lo usa el resto del repo (`internal/inventario`, `internal/ventas`, `internal/cobranza`). `misspell` lo marca como `Description` mal escrito: `descripcion` no está en `ignore-rules` de `.golangci.yml`.

Lo dejé resuelto con `//nolint:misspell` a nivel de archivo en `garantias_contracts.go` e `imagen.go`, que es lo que ya hace `internal/inventario/inventario_contracts.go` para el mismo `Descripcion`. El gate queda en cero y no se toca nada fuera de la lista del brief.

**Por qué la excepción local sí se queda, y no es una desviación.** El brief exige las dos cosas: la línea 205 fija el nombre del campo como `Descripcion`, y la línea 320 exige que `golangci-lint run ./internal/garantias/...` devuelva 0. La excepción local es la única salida — la alternativa sería agregar la palabra a `.golangci.yml`, que la línea 308 prohíbe explícitamente.

**Recomendación** (para una tarea aparte, no aquí): agregar `descripcion` y `Descripcion` a `ignore-rules` en `.golangci.yml`, junto a las entradas que ya existen para el mismo problema (`inventario`, `transito`, `observacion`). Con eso se pueden borrar los dos `nolint`.

### 2. Cobertura de `infra/storage` en 83.8% — no aplica a esta tarea

Lo anoté porque se ve al correr la suite del módulo, pero para que conste que **no es un gate de la tarea 5**:

- El 85% sale del **spec §8** (línea 328), no del brief.
- El brief no lo menciona. Lo único que dice de coberturas es el 90% de `app` (línea 278).
- El comando de cobertura del brief (línea 321) corre `./internal/garantias/domain/` y `./internal/garantias/app/`. `infra/storage` ni siquiera se mide.

El paquete da 83.8% hoy. Es código de Ruben, de la tarea 4, y no lo toqué (`git status` de `internal/garantias/infra/` está vacío). Queda anotado para que él lo atienda con su entrega.

## Report Path

`docs/superpowers/plans/garantias-task-5-report.md`
