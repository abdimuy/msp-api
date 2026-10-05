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

## Transacciones

Las operaciones `Crear`, `Guardar` y `ObtenerParaActualizar` requieren una transacción activa.

Esto permite que los cambios realizados a una garantía y sus eventos se guarden juntos.

Para comprobarlo hice una prueba donde se cambia la etapa de un artículo y después falla la inserción de un evento por una clave de idempotencia duplicada.

Se utilizó un `SAVEPOINT` y después `ROLLBACK TO SAVEPOINT`. Al revisar nuevamente el artículo, su etapa seguía con el valor anterior y el evento que falló no quedó guardado.

## Bloqueo

`ObtenerParaActualizar` utiliza `WITH LOCK`.

Se realizó una prueba con dos transacciones. La primera toma el bloqueo de la garantía y la segunda intenta obtener la misma garantía utilizando `RunInTxNoWait`.

La segunda transacción recibe `firebird_lock_conflict`, comprobando que no puede tomar el mismo registro mientras la primera transacción mantiene el bloqueo.

## Manejo de fechas

Para los campos `TIMESTAMP` se utiliza `firebird.ToWallClock` al escribir y las funciones de lectura de Firebird al recuperar los valores.

`VIGENCIA_HASTA` se maneja diferente porque es un campo `DATE`.

En este caso se conserva solamente año, mes y día, y al leerlo se reconstruye a medianoche UTC. Así no se cambia el día debido a una conversión de zona horaria.

## Textos

Las tablas nuevas de garantías utilizan UTF-8.

Se probaron textos con acentos y caracteres como `ñ`, por ejemplo descripciones de artículos y garantías, y los valores regresaron correctamente de Firebird.

## Casos probados

Las pruebas de integración comprobaron:

- Creación y lectura de garantías de origen cliente.
- Creación y lectura de garantías de origen piso.
- Campos opcionales y valores nulos.
- Acentos y caracteres UTF-8.
- Avance de etapas y almacenamiento de eventos.
- Creación de artículos de reemplazo.
- Relación `REEMPLAZA_A`.
- Claves de idempotencia duplicadas.
- Operaciones sin transacción.
- Garantías no encontradas.
- Atomicidad entre cambio de etapa y evento.
- Orden de eventos por fecha del dispositivo.
- Generación de folios.
- Ruta de reparación y dictamen.
- Desenlace y cierre.
- Bloqueo de una garantía entre dos transacciones.

## Validación

Se ejecutaron las siguientes validaciones:

`go vet ./internal/garantias/...`

Resultado: sin errores.

`go build ./...`

Resultado: compilación correcta.

`go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/infra/garfb/`

Resultado: pruebas correctas, sin errores de carrera.

Cobertura obtenida:

`80.9%`

También se ejecutó:

`make check-sealed MODULE=garantias`

Resultado:

`✔ garantias is sealed`

## Resultado

El repositorio Firebird permite guardar y recuperar garantías con sus artículos y eventos respetando las transacciones del módulo.

También se comprobó que un fallo durante el guardado no deje cambios parciales y que el bloqueo impida que dos transacciones actualicen al mismo tiempo la misma garantía.
