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

---

## Entrega 2 — Consultas e interfaz HTTP

Se completó el lado de lectura desde la capa de aplicación hasta HTTP y se atendieron las observaciones de revisión de la Entrega 2.

### Implementado

- `app.Consultas`.

- Consulta de bandeja.

- Consulta de detalle.

- Validación de usuario autenticado.

- Validación del permiso `garantias:leer`.

- Adaptador `identidad`.

- Rutas HTTP:

   - `GET /garantias`

   - `GET /garantias/{id}`

- Filtros: `cursor`, `limit`, `estado`, `origen`, `cliente_id`, `etapa`, `ubicacion`, `desde`, `hasta`.

- Respuestas JSON en `snake_case`.

- Fechas y horas en RFC3339 UTC.

- `vigencia_hasta` en formato `YYYY-MM-DD`.

- Traducción de errores HTTP.

- Registro de rutas y errores esperados en OpenAPI.

- Pruebas E2E HTTP → Consultas → garfb → Firebird.

- Lectura de detalle de garantía y eventos dentro de una sola transacción de lectura.

## Correcciones realizadas después de revisión

- `cliente_id` se valida con rango de entero de 32 bits.

- `cliente_id=0` devuelve 422.

- `cliente_id=2147483648` devuelve 422.

- `limit=0` devuelve 422.

- `estado`, `origen`, `etapa` y `ubicacion` inválidos devuelven 422 con mensaje en español y código estable.

- UUID inválido en `/garantias/{id}` devuelve 422 con código `invalid_uuid`.

- Los parámetros repetidos son rechazados con 422.

- `desde` posterior a `hasta` devuelve 422.

- Los errores de fecha indican correctamente que se requiere RFC3339.

- Una fecha de origen con zona distinta de UTC se proyecta en la respuesta con sufijo `Z`.

- Una garantía sin eventos devuelve `"eventos":[]` y no `null`.

- `KindTooManyRequests` conserva HTTP 429.

- `KindServiceUnavailable` conserva HTTP 503.

- Los errores internos 500 se registran mediante `slog` y no se exponen al cliente.

- `MountRouter` falla inmediatamente si `Consultas` es `nil`.

- OpenAPI incluye formatos `uuid`, `date-time` y `date`.

- OpenAPI documenta respuestas 401, 403, 404 y 422.

- El E2E de bandeja valida únicamente los IDs sembrados por la propia prueba.

- Se agregó un E2E real del detalle contra Firebird.

- Se agregó una prueba directa de `GarantiaRepo.ObtenerDetalle`.

## Seguridad

Las consultas verifican primero la identidad y después el permiso `garantias:leer`.

Sin autenticación:

```text

401 Unauthorized

```

Sin permiso:

```text

403 Forbidden

```

En ambos casos se verificó que los repositorios no fueran llamados.

Pruebas principales:

```text

TestGarHTTP_Seguridad_401_403

TestConsultas_Bandeja_SinUsuarioNoTocaRepos

TestConsultas_Bandeja_SinPermisoNoTocaRepos

TestConsultas_Detalle_SinUsuarioNoTocaRepos

TestConsultas_Detalle_SinPermisoNoTocaRepos

```

## Validaciones HTTP

Los parámetros inválidos producen:

```text

422 Unprocessable Entity

```

Entre los casos comprobados están:

```text

limit=0

cliente_id=0

cliente_id=2147483648

estado inválido

origen inválido

etapa inválida

ubicacion inválida

UUID inválido

parámetro repetido

desde > hasta

fecha que no cumple RFC3339

```

Pruebas relacionadas:

```text

TestListarGarantias_QueryParamsInvalidos_422

TestListarGarantias_QueryParamRepetido_422

TestListarGarantias_DesdePosteriorAHasta_422

TestObtenerGarantia_UUIDInvalido_422

TestListarGarantias_CursorInvalido_422

TestListarGarantias_InyeccionSQLFiltros_422

```

## Manejo de errores

Mapeo HTTP verificado:

```text

Validación             → 422

No encontrado          → 404

Conflicto              → 409

Sin permiso            → 403

No autenticado         → 401

Too Many Requests      → 429

Service Unavailable    → 503

Error interno          → 500

```

Para los errores internos se devuelve:

```text

ocurrió un error interno

```

El error original se registra mediante `slog`, pero no se expone en la respuesta HTTP.

Pruebas:

```text

TestListarGarantias_ErrorInterno_500NoFiltraMensaje

TestListarGarantias_ServiceUnavailable_503

TestListarGarantias_TooManyRequests_429

TestObtenerGarantia_NoEncontrada_404

```

## Fechas y colecciones

Los valores de fecha y hora se serializan en UTC mediante RFC3339.

Se comprobó con una fixture creada en zona UTC-06:00 y la respuesta mantuvo el instante correcto en UTC con sufijo `Z`.

También se verificó que una garantía sin eventos devuelva:

```json

"eventos": []

```

en lugar de:

```json

"eventos": null

```

Prueba:

```text

TestObtenerGarantia_SinEventos_DevuelveListaVacia

```

## OpenAPI

La prueba `TestOpenAPI_PathsRegistered` verifica que el documento incluya:

```text

/garantias

/garantias/{id}

listar-garantias

obtener-garantia

bearerAuth

format: uuid

format: date-time

format: date

401

403

404

422

```

También se verificó que `MountRouter` rechace una configuración sin `Consultas`:

```text

TestMountRouter_RechazaConsultasNil

```

## Consistencia del detalle

Para evitar que la garantía y sus eventos se obtengan en transacciones independientes se agregó:

```text

garfb.GarantiaRepo.ObtenerDetalle

```

Este método abre un solo `firebird.RunInReadTx` y usa el mismo `Querier` para obtener garantía, artículos y eventos.

`RunInReadTx` es reentrante, por lo que las operaciones internas reutilizan la transacción existente.

La implementación mantiene los puertos existentes sin modificar `ports/outbound`.

Prueba directa:

```text

TestGarantiaRepo_ObtenerDetalle

```

Resultado con Firebird:

```text

=== RUN   TestGarantiaRepo_ObtenerDetalle

INFO firebird: connected host=127.0.0.1 database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5

--- PASS: TestGarantiaRepo_ObtenerDetalle

PASS

```

## E2E HTTP → Firebird

Se verificó el flujo completo:

```text

HTTP

→ garhttp

→ app.Consultas

→ garfb

→ Firebird

```

Base utilizada:

```text

/firebird/data/MSPTEST.FDB

```

Pruebas:

```text

TestGarHTTP_E2E_BandejaFiltradaConFirebird

TestGarHTTP_E2E_DetalleConFirebird

```

El E2E de bandeja ya no presupone que existan exactamente dos garantías `en_proceso` en toda la tabla. La prueba identifica y valida únicamente las garantías sembradas por ella misma.

El E2E de detalle crea una garantía real, la consulta mediante `GET /garantias/{id}` y verifica que la garantía y los eventos devueltos correspondan al mismo ID.

Resultado directo del detalle:

```text

=== RUN   TestGarHTTP_E2E_DetalleConFirebird

INFO firebird: connected host=127.0.0.1 database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5

--- PASS: TestGarHTTP_E2E_DetalleConFirebird

PASS

ok github.com/abdimuy/msp-api/internal/garantias/infra/garhttp

```

Todas las escrituras de estas pruebas se ejecutan dentro de `fbtestutil.WithTestTransaction` y son revertidas al finalizar.

## Cobertura final

Comando:

```text

go clean -testcache && \

go test -race -count=1 -p 1 \

-coverprofile=cov.out \

./internal/garantias/...

```

Resultado:

```text

garantias                         100.0%

app                                86.2%

domain                            100.0%

garfb                              86.9%

garhttp                            96.9%

identidad                         100.0%

storage                            85.5%

ports/outbound                    100.0%

```

Cobertura de las consultas:

```text

NewConsultas        100.0%

Bandeja             100.0%

Detalle              71.4%

autorizarLectura    100.0%

```

Cobertura relevante de Firebird:

```text

GarantiaRepo.Obtener          100.0%

GarantiaRepo.ObtenerDetalle    84.6%

BandejaRepo.Listar             92.3%

EventoRepo.ListarPorGarantia   88.9%

```

Cobertura total de HTTP:

```text

garhttp 96.9%

```

Los mappers HTTP principales quedaron en:

```text

garantiaDTO       100.0%

articuloDTO       100.0%

eventoDTO         100.0%

formatTime        100.0%

stringOpcional    100.0%

```

## Mutation testing manual

Se mantienen los tres mutantes requeridos para la Entrega 2:

| Mutante | Cambio | Resultado |

|---|---|---|

| 6 | Eliminar la comprobación de `garantias:leer` | Killed |

| 7 | Permitir que `Identidad` llame a `PermisoFn` con permiso inválido | Killed |

| 8 | Exponer `err.Error()` en un error HTTP 500 | Killed |

Los tres defectos fueron detectados por las pruebas y después se restauró el código correcto.

## Gates finales

Formato:

```text

gofmt -l internal/garantias

```

Resultado: sin salida.

Análisis estático:

```text

go vet ./internal/garantias/...

```

Resultado: sin salida.

Build:

```text

go build ./...

```

Resultado: sin salida.

Lint:

```text

golangci-lint run ./internal/garantias/...

```

Resultado:

```text

0 issues.

```

Pruebas con race y Firebird:

```text

go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...

```

Resultado: PASS.

Módulo sellado:

```text

make check-sealed MODULE=garantias

```

Resultado:

```text

✔ garantias is sealed

```

## Resultado final

```text

app                 86.2%

identidad           100.0%

garhttp              96.9%

garfb                86.9%

ObtenerDetalle       84.6%

golangci-lint        0 issues

go vet               OK

go build             OK

race tests           PASS

E2E bandeja          PASS

E2E detalle          PASS

check-sealed         PASS

mutantes 6,7,8       Killed

```

## Desviaciones

No se modificaron archivos fuera de los permitidos por la Tarea 7.

No se realizaron cambios en dominio, puertos, migraciones ni `cmd/api`.

Las pruebas Firebird utilizan `/firebird/data/MSPTEST.FDB` y las escrituras están protegidas por `fbtestutil.WithTestTransaction`, por lo que se revierten al finalizar.
