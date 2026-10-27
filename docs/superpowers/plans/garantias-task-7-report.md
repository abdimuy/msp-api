# Garantías — Tarea 7 — Reporte

## Entrega 1 — Repositorio de lectura

Se completó la parte de lectura de Firebird correspondiente a la bandeja de garantías, el repositorio de imágenes y la validación de claves duplicadas.

### Implementado

- `BandejaRepo.Listar`.
- Filtros por estado, origen, cliente, etapa, ubicación y fechas.
- Paginación por `CREATED_AT DESC, ID DESC`.
- Cursor con desempate por ID.
- `Desde` inclusivo y `Hasta` exclusivo.
- Carga de artículos en una segunda consulta.
- Uso de `EXISTS` para filtros de artículos.
- `ImagenRepo.Registrar`.
- `ImagenRepo.ListarPorEvento`.
- Validación de FK en imágenes.
- Transacción obligatoria para registrar imágenes.
- Helper `esClaveDuplicada`.
- Lecturas protegidas con `RunInReadTx`.
- Validación de cursores decodificados inválidos.
- Desempate de imágenes por `CREATED_AT, ID`.
- Lista vacía no nula cuando un evento no tiene imágenes.

## Consultas por página

La bandeja realiza exactamente dos consultas por página:

1. Consulta de garantías.
2. Consulta de artículos mediante `GARANTIA_ID IN (...)`.

La prueba utilizada es:

```text
TestBandejaRepo_ListarUsaDosConsultasPorPagina
```

La prueba pasa por el método público `BandejaRepo.Listar` y comprueba que se realicen exactamente dos consultas por página.

## Asociación de artículos

Se agregó la prueba:

```text
TestBandejaRepo_Listar_AsociaArticulosAlFolioCorrecto
```

Utiliza dos garantías con artículos diferentes y comprueba que cada artículo quede asociado al folio correspondiente.

Con esto se detecta el caso en el que los artículos de varios folios fueran asignados por error al primer folio de la página.

## Aislamiento de las pruebas

Las pruebas identificadas durante la revisión fueron acotadas mediante ventanas de fecha propias para evitar depender de filas dejadas por otras personas en la base de desarrollo.

Entre las pruebas ajustadas se encuentran:

```text
TestBandejaRepo_Listar_FiltroEstado
TestBandejaRepo_Listar_FiltroOrigen
TestBandejaRepo_Listar_FiltroEtapa
TestBandejaRepo_Listar_FiltroDesde
TestBandejaRepo_Listar_FiltroHasta
TestBandejaRepo_Listar_FiltroUbicacion
TestBandejaRepo_Listar_NoDuplicaGarantiaConDosArticulos
TestBandejaRepo_Listar_PaginacionSinRepetirNiSaltar
TestBandejaRepo_Listar_PaginacionConCreatedAtIgual
TestBandejaRepo_ListarYObtener_ArticulosOrdenados
TestBandejaRepo_ListarUsaDosConsultasPorPagina
```

También se agregó:

```text
TestBandejaRepo_Listar_IgnoraFilasAjenasFueraDeLaVentana
```

La prueba siembra cuatro folios ajenos fuera de la ventana consultada y comprueba que sólo se devuelva el folio creado para el caso.

## Orden de artículos

La prueba de orden utiliza UUID fijos deliberadamente desordenados respecto al orden de creación. De esta forma, tanto `ORDER BY ID ASC` como `ORDER BY ID DESC` producen un resultado incorrecto y la prueba falla.

El orden correcto es:

```text
CREATED_AT, ID
```

## Validación del cursor

Se agregó una prueba específica para cursores que pueden decodificarse, pero contienen información inválida:

- versión incorrecta;
- ID faltante;
- fecha faltante.

Prueba:

```text
TestBandejaRepo_Listar_CursorDecodificadoInvalido
```

El error esperado es de validación con código:

```text
warranty_cursor_invalid
```

## ImagenRepo

`ImagenRepo.ListarPorEvento` devuelve los registros ordenados por:

```text
CREATED_AT, ID
```

Para comprobar el desempate se agregó:

```text
TestImagenRepo_ListarPorEvento_DesempataPorID
```

La prueba usa tres imágenes con el mismo `CREATED_AT` y UUID fijos.

También se verificó que un evento sin imágenes devuelva una lista vacía y no `nil`.

## Clave duplicada

`esClaveDuplicada` comprueba tanto el código GDS de Firebird:

```text
335544665
```

como el nombre de la restricción:

```text
UQ_MSP_GA_EVENTO_CLAVE
```

Validar solamente el código no es suficiente porque una PK u otra restricción única puede producir el mismo tipo de error.

Las pruebas comprueban que otra restricción no sea identificada como la clave de idempotencia.

## Etapa y ubicación

Cuando los filtros `Etapa` y `Ubicacion` se utilizan al mismo tiempo, las dos condiciones se evalúan dentro del mismo `EXISTS`.

Esto significa que debe existir un mismo artículo que cumpla simultáneamente la etapa y la ubicación solicitadas.

Se agregó la prueba combinada:

```text
TestBandejaRepo_Listar_FiltroEtapaYUbicacionMismoArticulo
```

## Mutantes de Entrega 1

### Mutantes requeridos por el brief

| # | Mutante | Resultado |
|---|---|---|
| 1 | Quitar el `ID` del desempate del cursor | Killed |
| 2 | Cambiar `EXISTS` por `JOIN` | Killed |
| 3 | Quitar el `+1` del límite | Killed |
| 4 | Quitar `RunInReadTx` de `Listar` | Killed |
| 5 | `esClaveDuplicada` regresa siempre `true` | Killed |
| 9 | Cambiar `Hasta` exclusivo por inclusivo | Killed |

### Mutantes adicionales de revisión

| Mutante | Cambio | Resultado |
|---|---|---|
| M20 | Asociar los artículos de la página al primer folio en vez de a su folio real | Killed |
| Adicional | Ordenar artículos solamente por `ID ASC` | Killed |
| Adicional | Ordenar artículos solamente por `ID DESC` | Killed |
| Adicional | Omitir validación de versión, ID o fecha del cursor decodificado | Killed |
| Adicional | Quitar el desempate por `ID` de imágenes con el mismo `CREATED_AT` | Killed |
| Adicional | Devolver `nil` cuando un evento no tiene imágenes | Killed |
| Adicional | Agregar una tercera consulta durante `BandejaRepo.Listar` | Killed |
| Adicional | Reconocer como duplicado una restricción distinta de `UQ_MSP_GA_EVENTO_CLAVE` | Killed |

## Verificación de garfb con race

Comando:

```sh
FB_HOST=localhost \
FB_PORT=3050 \
FB_DATABASE=/firebird/data/MSPTEST.FDB \
FB_USER=SYSDBA \
FB_PASSWORD="$FB_PASSWORD" \
FB_CHARSET=UTF8 \
go test -race -count=1 \
./internal/garantias/infra/garfb
```

Salida:

```text
ok      github.com/abdimuy/msp-api/internal/garantias/infra/garfb   5.968s
```

## Cobertura con Firebird

Comando:

```sh
go clean -testcache && \
FB_HOST=localhost \
FB_PORT=3050 \
FB_DATABASE=/firebird/data/MSPTEST.FDB \
FB_USER=SYSDBA \
FB_PASSWORD="$FB_PASSWORD" \
FB_CHARSET=UTF8 \
go test -race -count=1 -p 1 \
-coverprofile=cov.out \
./internal/garantias/...
```

Salida:

```text
ok      github.com/abdimuy/msp-api/internal/garantias   1.026s  coverage: 100.0% of statements
ok      github.com/abdimuy/msp-api/internal/garantias/domain    1.081s  coverage: 100.0% of statements
ok      github.com/abdimuy/msp-api/internal/garantias/infra/garfb   6.209s  coverage: 86.8% of statements
ok      github.com/abdimuy/msp-api/internal/garantias/infra/storage 1.056s  coverage: 85.5% of statements
ok      github.com/abdimuy/msp-api/internal/garantias/ports/outbound    1.028s  coverage: 100.0% of statements
```

El piso requerido para `infra/garfb` es 85%.

Resultado obtenido:

```text
86.8%
```

## Formato, vet y compilación

Comandos:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
```

Los tres comandos terminaron correctamente y sin salida.

## Linter

Comando:

```sh
golangci-lint run ./internal/garantias/...
```

Salida:

```text
0 issues.
```

## Sellado del módulo

Comando:

```sh
make check-sealed MODULE=garantias
```

Salida:

```text
✔ garantias is sealed
```

## Pruebas sin FB_DATABASE

Comando:

```sh
env -u FB_DATABASE \
go test -race -count=1 -p 1 ./internal/garantias/...
```

Salida:

```text
ok      github.com/abdimuy/msp-api/internal/garantias   1.022s
ok      github.com/abdimuy/msp-api/internal/garantias/domain    1.060s
ok      github.com/abdimuy/msp-api/internal/garantias/infra/garfb   1.037s
ok      github.com/abdimuy/msp-api/internal/garantias/infra/storage 1.044s
ok      github.com/abdimuy/msp-api/internal/garantias/ports/outbound    1.026s
```

## Resultado de la Entrega 1

La entrega queda con las pruebas de bandeja e imágenes reforzadas, paginación estable, asociación correcta de artículos y detección específica de la clave duplicada. Las pruebas señaladas durante la revisión fueron acotadas con ventanas de fecha para no depender de filas ajenas.

La cobertura final de `infra/garfb` es:

```text
86.8%
```

También se verificó el módulo con `race`, `go vet`, compilación, linter y `check-sealed`.

## Observaciones

La detección de la clave duplicada ahora exige tanto el código GDS `335544665` como el nombre `UQ_MSP_GA_EVENTO_CLAVE`.

Los filtros `Etapa` y `Ubicacion` se aplican al mismo artículo dentro del mismo `EXISTS`. Este comportamiento también está cubierto por una prueba combinada.

Una fila corrupta que no pueda hidratarse provoca actualmente que falle la lectura completa de la bandeja y termine como error 500. Se mantiene documentado como límite conocido de esta entrega.

En consultas sin filtros Firebird puede terminar realizando un recorrido y ordenamiento general de la tabla (`PLAN SORT (G NATURAL)`). No afecta la corrección de esta entrega, pero deberá revisarse si aumenta considerablemente el volumen de garantías.
