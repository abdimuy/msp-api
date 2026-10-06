# Garantías — Tarea 6: Repositorio Firebird

## Resumen

En esta tarea implementé la persistencia en Firebird para el módulo de garantías.

Se agregaron los repositorios para guardar y consultar garantías, artículos y eventos, además del generador de folios.

La implementación se encuentra en:

`internal/garantias/infra/garfb/`

## Archivos realizados

- `doc.go`
- `queries.go`
- `garantia_repo.go`
- `evento_repo.go`
- `folio_generator.go`
- `mappers.go`
- pruebas de integración

También se organizaron las pruebas en archivos separados para garantías, eventos, transacciones, guardado, folios y bloqueo.

## Transacciones y atomicidad

Las operaciones `Crear`, `Guardar` y `ObtenerParaActualizar` requieren una transacción activa.

Para comprobar la atomicidad se utiliza la prueba:

`TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento`

En la prueba se crea una garantía y después se avanza la etapa de un artículo. Al guardar, la inserción del evento falla porque utiliza una clave de idempotencia duplicada.

Se utiliza un `SAVEPOINT` antes de ejecutar `Guardar` y, al producirse el error, se ejecuta `ROLLBACK TO SAVEPOINT`.

Después del rollback se comprueba que:

- La etapa del artículo continúa con el valor anterior.
- El evento que produjo el error no queda guardado.
- Solamente permanecen los eventos creados originalmente.

## Bloqueo

`ObtenerParaActualizar` utiliza `WITH LOCK`.

La prueba `TestGarantiaRepo_Candado_Commit` utiliza dos transacciones. La primera obtiene la garantía con bloqueo y la segunda intenta obtener el mismo registro usando `RunInTxNoWait`.

Mientras la primera transacción conserva el bloqueo, la segunda recibe un conflicto y no puede continuar.

## Manejo de fechas

Los campos `TIMESTAMP` se escriben utilizando `firebird.ToWallClock` y al leerlos se convierten nuevamente a UTC.

`VIGENCIA_HASTA` es diferente porque en Firebird es un campo `DATE`.

Para este campo se conserva solamente año, mes y día. Al leerlo se reconstruye a medianoche UTC para evitar que una conversión de zona horaria cambie el día almacenado.

## UTF-8

Las tablas `MSP_GA_*` trabajan con UTF-8.

Se agregó la prueba `TestGarantiaRepo_PersisteUTF8`, utilizando explícitamente los caracteres `ñ`, `á` y `ü` en:

- Descripción de la garantía.
- Descripción del artículo.
- Descripción de un evento.

Los tres textos fueron guardados y recuperados sin cambios.

Salida:

```text
=== RUN   TestGarantiaRepo_PersisteUTF8
2026/10/06 11:29:07 INFO firebird: connected host=localhost database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5
--- PASS: TestGarantiaRepo_PersisteUTF8 (0.14s)
PASS
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	0.143s
```

## Orden de eventos

`ListarPorGarantia` ordena los eventos de esta manera:

`DEVICE_CREATED_AT, CREATED_AT, ID`

La prueba `TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt` utiliza dos eventos con el mismo `DEVICE_CREATED_AT`.

El primer evento tiene `CREATED_AT` a las `00:01` y el segundo a las `00:02`, comprobando que `CREATED_AT` funciona como segundo criterio de ordenamiento.

Salida final de la prueba:

```text
=== RUN   TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt
2026/10/06 11:27:10 INFO firebird: connected host=localhost database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5
--- PASS: TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt (0.12s)
PASS
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	0.127s
```

## Número de consultas de Obtener

Se agregó la prueba:

`TestGarantiaRepo_ObtenerUsaDosConsultas`

La prueba crea una garantía con tres artículos y utiliza un `countingQuerier` para contar las consultas realizadas durante la lectura.

El resultado medido fue:

`2 consultas`

Las consultas corresponden a:

1. Lectura de la garantía.
2. Lectura de todos sus artículos.

No se realiza una consulta adicional por cada artículo.

Salida:

```text
=== RUN   TestGarantiaRepo_ObtenerUsaDosConsultas
2026/10/06 11:32:11 INFO firebird: connected host=localhost database=/firebird/data/MSPTEST.FDB charset=UTF8 pool_size=5
--- PASS: TestGarantiaRepo_ObtenerUsaDosConsultas (0.13s)
PASS
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	0.135s
```

## Casos probados

Las pruebas de integración cubren:

- Garantía de origen cliente con todos sus campos.
- Garantía de origen piso con valores nulos.
- Dos artículos en una garantía.
- Textos UTF-8 con `ñ`, `á` y `ü`.
- Avance de etapas y almacenamiento de eventos.
- `ETAPA_DESDE` y `ETAPA_HASTA`.
- Creación de artículos nuevos.
- Artículos de reemplazo.
- Relación `REEMPLAZA_A`.
- Claves de idempotencia duplicadas.
- Control positivo con claves diferentes.
- Garantías no encontradas.
- Escrituras sin transacción.
- Atomicidad cuando falla la inserción de un evento.
- Orden por `DEVICE_CREATED_AT`, `CREATED_AT` e `ID`.
- Generación de folios diferentes y crecientes.
- Ruta de reparación y dictamen.
- Desenlace y cierre.
- Bloqueo entre dos transacciones.
- Número de consultas utilizado por `Obtener`.

## Verificación

Se ejecutaron las validaciones del módulo.

### Formato, vet, build, lint y módulo sellado

```text
$ gofmt -l internal/garantias
$ go vet ./internal/garantias/...
$ go build ./...
$ $(go env GOPATH)/bin/golangci-lint run ./internal/garantias/... --timeout 10m
0 issues.
$ make check-sealed MODULE=garantias
✔ garantias is sealed
```

### Pruebas con race detector y cobertura

```text
$ go clean -testcache
$ go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/infra/garfb/
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	2.397s	coverage: 80.9% of statements
$ go tool cover -func=cov.out | tail -1
total:								(statements)			80.9%
```

La cobertura final obtenida fue de `80.9%`, superando el mínimo requerido de `80%`.

## Ejecución sin FB_DATABASE

También se comprobó que las pruebas de integración no fallen cuando `FB_DATABASE` no está configurada.

Comando utilizado:

```text
$ env -u FB_DATABASE go test -v ./internal/garantias/infra/garfb/
```

Salida:

```text
=== RUN   TestGarantiaRepo_Candado_Commit
    candado_test.go:20: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_Candado_Commit (0.00s)
=== RUN   TestEventoRepo_ListarYObtenerPorClave
    evento_repo_test.go:18: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestEventoRepo_ListarYObtenerPorClave (0.00s)
=== RUN   TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt
    evento_repo_test.go:114: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt (0.00s)
=== RUN   TestFolioGenerator_Siguiente
    folio_generator_test.go:15: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestFolioGenerator_Siguiente (0.00s)
=== RUN   TestGarantiaRepo_CrearYObtener_Cliente
    garantia_repo_test.go:18: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_CrearYObtener_Cliente (0.00s)
=== RUN   TestGarantiaRepo_ClaveIdempotenciaDuplicada
    garantia_repo_test.go:185: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_ClaveIdempotenciaDuplicada (0.00s)
=== RUN   TestGarantiaRepo_NoEncontrada
    garantia_repo_test.go:275: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_NoEncontrada (0.00s)
=== RUN   TestGarantiaRepo_CrearYObtener_Piso
    garantia_repo_test.go:345: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_CrearYObtener_Piso (0.00s)
=== RUN   TestGarantiaRepo_GuardarAvanceYEvento
    guardar_test.go:19: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_GuardarAvanceYEvento (0.00s)
=== RUN   TestGarantiaRepo_GuardarArticuloNuevoYReemplazo
    guardar_test.go:166: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_GuardarArticuloNuevoYReemplazo (0.00s)
=== RUN   TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento
    guardar_test.go:359: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento (0.00s)
=== RUN   TestGarantiaRepo_PersisteRutaDictamenYDatosDeEvento
    guardar_test.go:491: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_PersisteRutaDictamenYDatosDeEvento (0.00s)
=== RUN   TestGarantiaRepo_PersisteDesenlaceYCierre
    guardar_test.go:710: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_PersisteDesenlaceYCierre (0.00s)
=== RUN   TestGarantiaRepo_OperacionesDeEscrituraRequierenTransaccion
    transacciones_test.go:19: FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests
--- SKIP: TestGarantiaRepo_OperacionesDeEscrituraRequierenTransaccion (0.00s)
PASS
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	0.002s
```

## Pruebas de mutación

Se realizaron las tres comprobaciones de mutación solicitadas.

### 1. Bloqueo

Se eliminó temporalmente `WITH LOCK` de la consulta utilizada por `ObtenerParaActualizar`.

La prueba `TestGarantiaRepo_Candado_Commit` se puso en rojo:

```text
Error:       An error is expected but got nil.
Test:        TestGarantiaRepo_Candado_Commit
--- FAIL: TestGarantiaRepo_Candado_Commit
```

Después se restauró `queries.go`.

### 2. Mapeo de clave de idempotencia duplicada

Se modificó temporalmente la detección de la restricción `UQ_MSP_GA_EVENTO_CLAVE`.

La prueba `TestGarantiaRepo_ClaveIdempotenciaDuplicada` se puso en rojo:

```text
Error:       Target error should be in err chain:
             expected: "warranty_idempotency_key_duplicate: clave de idempotencia duplicada"
             in chain: "firebird_unique_violation: registro duplicado: violation of PRIMARY or UNIQUE KEY constraint UQ_MSP_GA_EVENTO_CLAVE on table MSP_GA_EVENTO
Test:        TestGarantiaRepo_ClaveIdempotenciaDuplicada
--- FAIL: TestGarantiaRepo_ClaveIdempotenciaDuplicada
```

Después se restauró `garantia_repo.go`.

### 3. Error durante Guardar

Se modificó temporalmente `Guardar` para ignorar el error producido al insertar un evento.

La prueba `TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento` se puso en rojo:

```text
Error:       Expected error with "warranty_idempotency_key_duplicate: clave de idempotencia duplicada" in chain but got nil.
Test:        TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento
--- FAIL: TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento
```

Después se restauró `garantia_repo.go`.

Estas pruebas demuestran que los tests detectan la eliminación de los comportamientos importantes de bloqueo, mapeo de idempotencia y manejo del error durante el guardado.

## Desviaciones

El brief utiliza en su ejemplo de verificación:

`FB_DATABASE=/firebird/data/MUEBLERA.FDB`

En el entorno local utilizado para esta tarea la base disponible y migrada es:

`FB_DATABASE=/firebird/data/MSPTEST.FDB`

Por esa razón las pruebas de integración y la cobertura se ejecutaron contra `MSPTEST.FDB`.

No se realizaron cambios en migraciones, dominio, puertos ni configuración para modificar esta diferencia del entorno local.

## Resultado

La persistencia Firebird de garantías permite crear, actualizar y recuperar garantías, artículos y eventos respetando las transacciones del módulo.

Se comprobó además:

- Cobertura de `80.9%`.
- Funcionamiento de UTF-8.
- Orden correcto de eventos.
- Dos consultas para recuperar una garantía con tres artículos.
- Manejo de claves de idempotencia duplicadas.
- Atomicidad ante errores.
- Bloqueo de concurrencia.
- Ejecución segura de las pruebas cuando `FB_DATABASE` no está configurada.
