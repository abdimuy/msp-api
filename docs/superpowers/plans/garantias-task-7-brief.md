# Garantías — Tarea 7: el lado de lectura completo, de la base al HTTP

> **Rama:** `feat/garantias-bandeja`, ya creada desde tu `feat/garantias-garfb`: `git fetch && git switch feat/garantias-bandeja`. La entrega 2 va en una rama que creas tú desde ésta.
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md), §2.1, §2.2, §5, §6, §7 y §8.
> **Plazo:** dos entregas, **lunes 12 de octubre, 12pm** y **martes 13 de octubre, 12pm**. El módulo va atrasado y esta tarea está en la ruta crítica: no hay holgura.

## Qué construyes

Toda la ruta de **lectura** del módulo, desde la base hasta el HTTP:

```
GET /v2/garantias        →  garhttp  →  app.Consultas.Bandeja  →  garfb.BandejaRepo.Listar
GET /v2/garantias/{id}   →  garhttp  →  app.Consultas.Detalle  →  garfb.GarantiaRepo.Obtener + EventoRepo.ListarPorGarantia
```

Más la evidencia en el repositorio (`ImagenRepo`) y el adaptador de identidad, que es la pieza que deja al módulo saber quién llama sin romper el sellado.

La bandeja es la pantalla más usada del módulo: oficina la abre para saber qué folios están en el taller, cuáles con el proveedor y cuáles listos para entregar. Si filtra mal, un mueble se pierde de vista, que es lo que el módulo existe para evitar (spec §1).

| Entrega | Contenido | Cuándo |
|---|---|---|
| **1** | Repositorio: `BandejaRepo.Listar`, `ImagenRepo` y el helper de la clave duplicada | **Lunes 12, 12pm** |
| **2** | `app.Consultas`, el adaptador de identidad y `garhttp` con las dos rutas de lectura | **Martes 13, 12pm** |

Dos PRs, uno por entrega. La 1 se revisa mientras haces la 2.

---

## La vara

Esta tarea se revisa igual que la 6: leyendo el código completo, **con mutantes y midiendo contra Firebird**. Lo que rebota una entrega:

- Una prueba que no puede fallar: comprueba la mitad fácil, o pasa con cualquier implementación.
- Un filtro, un permiso o un error sin su caso negativo.
- Un mutante de la lista que sobrevive.
- Un número del reporte que no coincide con lo que mido.
- Cualquier import fuera de `internal/garantias/**` e `internal/platform/**`: el módulo es sellado y `make check-sealed` lo caza.

**Pisos de cobertura**, más altos que los del spec a propósito:

| Paquete | Piso |
|---|---|
| `infra/garfb` | ≥ 85% con `FB_DATABASE` |
| `app` (lo tuyo) | ≥ 95% |
| `infra/identidad` | 100% |
| `infra/garhttp` | ≥ 85% |

---

## Leer antes de escribir

1. **Tu tarea 6 y sus dos revisiones** en el #25. Se reusan los patrones: `RunInReadTx` en las lecturas, `RequireTx` en las escrituras y errores de hidratación como error interno.
2. **`docs/module-standards/HUMA_WIRING.md`** completo, y **`CQRS_PATTERN.md`**.
3. **`internal/ventas/infra/venthttp/`**: `routes.go`, `dto.go`, `dto_mapper.go`, `auth.go`, `security_test.go` y el `TestOpenAPI_PathsRegistered` de `handlers_test.go`. Es la referencia de forma de la capa HTTP. **Ojo con la diferencia:** `ventas` revisa permisos en el handler importando `auth`. **Garantías no puede**, porque es sellado: aquí el permiso lo revisa `app` a través del puerto `Identity` (decisión 4 de la tarea 5).
4. **`docs/module-standards/DATETIME_HANDLING.md`**: el contrato con el frontend es RFC3339 en UTC.
5. **`internal/garantias/ports/outbound/`**: las firmas ya fusionadas. **No se cambian.**

---

## Decisiones ya tomadas

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas.

### Entrega 1, repositorio

1. **`Listar` pagina por cursor.** Orden `CREATED_AT DESC, ID DESC`, y la página siguiente arranca en `(CREATED_AT < ?) OR (CREATED_AT = ? AND ID < ?)`. Pide `Limite + 1` filas para saber si hay otra página sin `COUNT`. Offset no: si entra un folio mientras alguien pagina, uno sale repetido u omitido.
2. **El cursor** se codifica con `pagination.EncodeCursor` y se decodifica con `pagination.DecodeCursor`. El campo `UpdatedAt` del `pagination.Cursor` lleva el `CREATED_AT`: déjalo dicho en un comentario. Un cursor que no decodifica es **error de validación** (`apperror.NewValidation`, código `warranty_cursor_invalid`), nunca 500.
3. **`Limite`:** si es ≤ 0 se usan 20; si pasa de 100 se usan 100.
4. **Dos consultas por página, siempre:** la de folios y **una** de artículos con `WHERE GARANTIA_ID IN (...)`, repartida en memoria y ordenada `CREATED_AT, ID`. Las dos, dentro del **mismo** `RunInReadTx`.
5. **Etapa y ubicación filtran por artículo, con `EXISTS`, no con `JOIN`.** Un folio entra si **al menos uno** de sus artículos cumple, y aparece una sola vez.
6. **`Desde` es inclusivo y `Hasta` es exclusivo**, y las dos van con `firebird.ToWallClock`.
7. **Sólo parámetros.** Del `WHERE` sólo se concatenan fragmentos fijos según qué filtros vienen. Ningún valor del cliente toca el texto del SQL.
8. **`ImagenRepo.Registrar` exige transacción** (`RequireTx`). `ListarPorEvento` va ordenado por `CREATED_AT, ID`, dentro de `RunInReadTx`. Si el evento no existe, el error sale como lo mapea `firebird.MapError` (`firebird_fk_violation`): no inventes centinelas del dominio, que es de Kevin.
9. **El helper de la clave duplicada.** Saca a una función con nombre la detección que hoy vive dentro de `insertarEvento` (`esClaveDuplicada(err) bool`), con un comentario que diga de qué texto del driver depende.
10. **`Imagen` está cambiando de nombre** (`Descripcion` → `Description`, en el #24 de Kevin, que se fusiona hoy). Haz `ImagenRepo` **después** de que se fusione. Hasta entonces, trabaja en la bandeja.

### Entrega 2, consultas, identidad y HTTP

11. **`app.Consultas` es un struct aparte de `app.Service`** (CQRS): lectura y escritura no comparten dependencias. Va en `app/consultas.go`, con `NewConsultas(ConsultasDeps)`, y sus dependencias son `outbound.GarantiaRepo`, `outbound.BandejaRepo`, `outbound.EventoRepo` y `outbound.Identity`. **No toques ningún otro archivo de `app/`**: son de Kevin, y en el mismo paquete no puede haber dos tareas escribiendo el mismo archivo. Tus fakes van en `app/consultas_fakes_test.go`, con prefijo propio (`fakeConsulta…`) para no chocar con los suyos cuando se junten las ramas.
12. **Dos consultas:**
    - `Bandeja(ctx, BandejaQuery) (outbound.Pagina[*domain.Garantia], error)`
    - `Detalle(ctx, id uuid.UUID) (Detalle, error)`, donde `type Detalle struct { Garantia *domain.Garantia; Eventos []*domain.Evento }`

    Las dos exigen usuario autenticado y el permiso `garantias:leer`, **antes** de tocar cualquier repositorio. `BandejaQuery` lleva tipos del dominio y primitivos, nunca DTOs de HTTP.
13. **El adaptador de identidad** va en `internal/garantias/infra/identidad/` y **no importa `internal/auth`**: el módulo es sellado. Recibe dos funciones que le va a inyectar el arranque del API:
    ```go
    type UsuarioFn  func(ctx context.Context) (id, nombre string, ok bool)
    type PermisoFn  func(ctx context.Context, codigo string) (bool, error)
    func New(usuario UsuarioFn, permiso PermisoFn) *Identidad   // implementa outbound.Identity
    ```
    - Sin usuario, o con nombre vacío → `domain.ErrUsuarioNoAutenticado`.
    - Un `domain.Permiso` que no es `IsValid()` se rechaza **sin** llamar a `PermisoFn`.
    - El error de `PermisoFn` se propaga tal cual.
    - Si alguna de las dos funciones es `nil`, `New` debe fallar en el arranque (panic con mensaje claro), no en la primera petición.
14. **`garhttp` sigue `HUMA_WIRING.md`.**
    - Monta con `MountRouter(r chi.Router, d Deps) huma.API`, donde `Deps` lleva `Consultas *app.Consultas`. Kevin agregará los comandos a `Deps` en su tarea: deja el struct listo para crecer.
    - Rutas: `GET /garantias` (`listar-garantias`) y `GET /garantias/{id}` (`obtener-garantia`, con `format:"uuid"`).
    - El handler no revisa permisos: los revisa `app`. El handler sólo traduce la entrada, llama y traduce el error.
15. **El contrato JSON:**
    - Campos en `snake_case`.
    - Fechas RFC3339 en **UTC**, con `Z`. `vigencia_hasta` es fecha sola, `YYYY-MM-DD`.
    - UUIDs canónicos.
    - Opcionales con `omitempty` y puntero.
    - Los enums como su valor de catálogo.

    La respuesta de la bandeja es `{ "items": [...], "next_cursor": "...", "has_more": bool }`, y cada item trae el folio con sus artículos. El detalle trae el folio, sus artículos y su línea de tiempo.
16. **Query params** (`HUMA_WIRING.md`, "What does NOT work"): `cursor`, `limit`, `estado`, `origen`, `cliente_id`, `etapa`, `ubicacion`, `desde`, `hasta`, todos como `string`.
    - Los enums llevan `enum:"..."` en el tag y además se parsean con el `Parse` del dominio.
    - Las fechas se parsean en el handler.
    - Un valor inválido → **422** con un error tipado, nunca 500.
17. **Mapeo de errores:**
    - validación → 422
    - no encontrado → 404
    - conflicto → 409
    - prohibido → 403
    - no autenticado → 401
    - cualquier otro → 500 con mensaje **genérico**

    Un 500 **nunca** devuelve el texto del error interno: ni del driver ni de Firebird. Es una función local del paquete, porque no puedes importar la de `ventas`.

---

## Entregables

**Entrega 1**, en `internal/garantias/infra/garfb/`: `bandeja_repo.go`, `imagen_repo.go`, el helper en `garantia_repo.go`, y lo necesario en `queries.go` y `mappers.go`.

**Entrega 2:**
- `internal/garantias/app/consultas.go`
- `internal/garantias/infra/identidad/identidad.go`
- `internal/garantias/infra/garhttp/`: `routes.go`, `dto.go`, `dto_mapper.go`, `handlers_consultas.go` y `errores.go`

Todos con sus pruebas.

---

## Pruebas

### Entrega 1, contra Firebird

Todo dentro de `WithTestTransaction`, con agregados construidos con el dominio y datos realistas.

1. **Los 7 filtros**, cada uno con un folio que entra **y uno que no**.
2. **Tres filtros combinados**, con un folio que cumple dos de los tres y por eso queda fuera.
3. **Bordes de fecha:** un folio exactamente en `Desde` **entra**; uno exactamente en `Hasta`, **no**.
4. **Paginación:** 5 folios con `Limite = 2` dan tres páginas, sin repetir ni saltar, y la última con el cursor vacío.
5. **El empate:** varios folios con **el mismo** `CREATED_AT`, partidos entre dos páginas.
6. **Sin duplicados:** un folio con dos artículos en la etapa filtrada sale una sola vez.
7. **Dos consultas por página**, contadas, en una página de varios folios con varios artículos.
8. **El orden de los artículos** en `Listar` **y** en `Obtener`: tres artículos guardados en desorden salen ordenados.
9. **Cursor inválido** → error de validación.
10. **Lecturas sin transacciones abiertas:** agrega `Listar` y `ListarPorEvento` a tu prueba `TestLecturasPublicas_NoDejanTransaccionesAbiertas`.
11. **`ImagenRepo`, ida y vuelta** campo por campo, con acentos y `ñ` en la ruta y la descripción.
12. **`ListarPorEvento`:** sólo las imágenes del evento pedido, en orden; vacío y sin error si no hay.
13. **Evento inexistente** → `firebird_fk_violation`.
14. **`Registrar` sin transacción** → `ErrNoTx`, sin escribir nada.
15. **`esClaveDuplicada`:** reconoce la clave repetida contra la base y **no** reconoce otro `UNIQUE`. Usa un `FOLIO` repetido como control.

### Entrega 2

16. **`Consultas`, con fakes:**
    - Sin usuario → 401; sin `leer` → 403. En los dos casos, **ningún repositorio recibió una llamada**: compruébalo, no lo supongas.
    - El camino feliz de cada una.
    - Los errores de cada puerto se propagan.
    - El detalle de un folio inexistente da `ErrGarantiaNoEncontrada`.
17. **`identidad`:** las cuatro reglas de la decisión 13, cada una con su prueba, y 100% de cobertura.
18. **`garhttp`, con `httptest`:**
    - Las dos rutas en su camino feliz, comparando el JSON **completo** contra lo esperado. No basta el status: un campo que cambia de nombre rompe al frontend sin que nada truene.
    - Cada query param inválido → 422.
    - UUID inválido en la ruta → 422, no 500.
    - Folio inexistente → 404.
    - **Barrido de seguridad** sobre cada ruta, sin usuario → 401 y sin permiso → 403, con entradas válidas (`HUMA_WIRING.md`, "Validation note").
    - **Un 500 no filtra el error interno:** con un repositorio que falle con un texto conocido, comprueba que ese texto **no** aparece en la respuesta.
    - `TestOpenAPI_PathsRegistered`.
    - Un parámetro con texto de inyección SQL (`' OR 1=1 --`) en cada filtro de texto → 422, y nada más.
19. **Una prueba de punta a punta:** HTTP → `Consultas` → `garfb` real, dentro de `WithTestTransaction`. Siembra tres folios, pide la bandeja con un filtro por HTTP y comprueba que salen exactamente los que deben, en orden.

### Mutantes: los haces tú y los reportas, con su resultado

| # | Mutante | Debe matarlo |
|---|---|---|
| 1 | Quitar el `ID` del desempate del cursor | prueba 5 |
| 2 | Cambiar un `EXISTS` por `JOIN` | prueba 6 |
| 3 | Quitar el `+1` del límite | prueba 4 |
| 4 | Quitar el `RunInReadTx` de `Listar` | prueba 10 |
| 5 | `esClaveDuplicada` regresa siempre `true` | prueba 15 |
| 6 | `Consultas` sin el chequeo de permiso | prueba 16 |
| 7 | El adaptador llama a `PermisoFn` con un permiso inválido | prueba 17 |
| 8 | El 500 devuelve el mensaje interno | prueba 18 |
| 9 | `Hasta` inclusivo | prueba 3 |

Si alguno sobrevive, falta una prueba. Agrégala antes de entregar.

---

## Archivos que puedes tocar

```
internal/garantias/infra/garfb/**
internal/garantias/app/consultas.go, consultas_test.go, consultas_fakes_test.go
internal/garantias/infra/identidad/**
internal/garantias/infra/garhttp/**
docs/superpowers/plans/garantias-task-7-report.md
```

Nada más. En particular: nada en `domain/` ni en `ports/`, ningún otro archivo de `app/`, ninguna migración, y **nada en `cmd/api/`** (conectar el módulo al arranque es la tanda 3).

---

## Verification, en cada entrega

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
go tool cover -func=cov.out | grep -E 'garfb|identidad|garhttp|consultas'
make check-sealed MODULE=garantias
```

Con la base arriba, y una vez sin `FB_DATABASE`: lo de Firebird se salta y nada falla.

---

## Reporte

`docs/superpowers/plans/garantias-task-7-report.md`, uno solo que crece con las dos entregas. Lleva:
- la salida **literal** de los comandos;
- la tabla de mutantes con su resultado;
- las consultas por página, medidas;
- un ejemplo real de la respuesta JSON de cada ruta.

Lo que hayas hecho distinto al brief, declarado con el porqué.

---

## Calendario

| Cuándo | Qué tiene que estar empujado |
|---|---|
| **Jueves 8, al cierre** | `Listar` con las pruebas 1 a 6 |
| **Viernes 9, al cierre** | Pruebas 7 a 10, el helper con su prueba 15, y `ImagenRepo` si el #24 ya se fusionó |
| **Lunes 12, 12pm** | **Entrega 1**: PR contra `main` con los mutantes 1 a 5 y 9 |
| **Lunes 12, al cierre** | `Consultas` e `identidad` completos, con sus pruebas |
| **Martes 13, 12pm** | **Entrega 2**: PR con `garhttp`, la prueba de punta a punta y los mutantes 6 a 8 |

Los avances diarios los reviso. Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
