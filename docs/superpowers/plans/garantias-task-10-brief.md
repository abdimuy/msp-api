# Garantías — Tarea 10: imágenes, firma en la base, catálogos y el módulo encendido

> **Rama:** `feat/garantias-t10`, que creas tú desde `feat/garantias-integracion` (`git fetch && git switch -c feat/garantias-t10 origin/feat/garantias-integracion`). Los PR van contra `feat/garantias-integracion`, no contra `main`.
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md), actualizado el 26 de octubre: §2.1, §2.2, **§3.3 y §3.4** (columnas nuevas), §4.1, §5, **§6 completo**, §7, §8 y §10.
> **Plazo:** tres entregas, **jueves 29 de octubre, 12pm**, **viernes 30, 12pm** y **lunes 2 de noviembre, 4pm**. La tercera es junto con Kevin. No hay holgura.
> **En paralelo:** Kevin hace la tarea 9. Los archivos de cada uno están listados abajo y **no se cruzan**.

## Qué construyes

Al terminar, el módulo de garantías **está encendido** en el API: montado, con sus cinco permisos en el catálogo, la evidencia fotográfica subiendo y bajando, la firma de la entrega guardada en la base, y los catálogos para que la pantalla no reimplemente la máquina de estados.

| Entrega | Contenido | Cuándo |
|---|---|---|
| **1** | Migración `000071`, toda la persistencia nueva en `garfb` y el helper de blobs | **Jueves 29, 12pm** |
| **2** | Subida de evidencia, descarga de imagen, catálogos y detalle con imágenes | **Viernes 30, 12pm** |
| **3** | `module.go`, cableado en `cmd/api`, permisos, prueba de arranque, barrido de seguridad final y targets del `Makefile`, **con Kevin** | **Lunes 2 de noviembre, 4pm** |

### Antes que nada: lo que queda del #28 y el #29

El #28 y el #29 se cerraron el miércoles 28 y su contenido ya está en `feat/garantias-integracion`. **Empieza tu rama desde ahí** (`git pull` primero).

Dos pendientes entran en tu **entrega 1** (el detalle está en el último comentario de cada PR):
1. `FiltroEtapaYUbicacionMismoArticulo`: siembra un folio con dos artículos, uno con la etapa pedida en otra ubicación y otro en la ubicación pedida con otra etapa, y verifica que **no** aparezca.
2. `ObtenerDetalle` en `TestLecturasPublicas_NoDejanTransaccionesAbiertas`, y un comentario en `garantia_repo.go` junto a `RunInSnapshotTx` diciendo que el nivel de aislamiento no tiene prueba.

Ojo, que ya te toca: al juntar el #29 con el #30 en la rama de integración, dos de tus pruebas dejaron de pasar contra Firebird y se arreglaron en el merge. Léelo en el mensaje del commit `chore(garantias): integra feat/garantias-consultas-http (PR #29)…`: `insertarEventoSQL` ganó las dos columnas de ubicación, y con las reglas R1 y R4 del #30 un folio de piso no pasa por recolección y un folio abierto no avanza artículos.

---

## Leer antes de escribir

1. El spec, **§3.3, §3.4, §4.1 y §6** como quedaron el 26 de octubre.
2. **El commit de contrato** de `feat/garantias-integracion` (`feat(garantias): contrato compartido de las tareas 9 y 10`). De ahí sale lo que persistes: `Imagen.Tipo()`, `Imagen.Huella()`, el `ID` opcional de `NewImagenParams`, `Evento.Huella()`, `DistanciaEntregaM()`, `AuditoriaGPS()`, `Garantia.ImagenesPendientesForRepo()`, los dos métodos nuevos de `ImagenRepo` y los centinelas al final de `domain/errors.go`. **Sus firmas no se cambian**: si una no te sirve, dímelo y la cambiamos en los dos briefs a la vez.
3. `migrations-firebird/000050_*`, `000057_*` y `000070_*`, up y down. Tu migración sigue su forma. **La `000070` tiene un defecto** (abajo, "Errores que no se repiten", punto 4) que ya quedó corregido en la integración; no copies la versión vieja.
4. `docs/module-standards/HUMA_WIRING.md`, en especial "Multipart upload" y "Validation note".
5. `internal/ventas/infra/venthttp/handlers_imagenes.go` y sus pruebas: subida y descarga de imágenes en este repo.
6. `internal/canal/module.go`: el `module.go` de otro módulo sellado. Y en `cmd/api/`, `desempeno_wiring.go`, el bloque de desempeño en `server.go` y `fx_graph_test.go`.
7. `internal/auth/domain/permission_codes.go`: cómo se declara un permiso y su categoría.

---

## Las decisiones ya tomadas — no se re-litigan

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas.

### D1. Migración `000071_add_firma_y_auditoria_a_msp_ga`

Up, con la forma y el comentario de la `000057`:

```sql
ALTER TABLE MSP_GA_IMAGEN ADD TIPO VARCHAR(12) CHARACTER SET ASCII;
ALTER TABLE MSP_GA_IMAGEN ADD HUELLA CHAR(64) CHARACTER SET ASCII;
ALTER TABLE MSP_GA_EVENTO ADD HUELLA CHAR(64) CHARACTER SET ASCII;
ALTER TABLE MSP_GA_EVENTO ADD DISTANCIA_ENTREGA_M DOUBLE PRECISION;
ALTER TABLE MSP_GA_EVENTO ADD AUDITORIA_GPS VARCHAR(20) CHARACTER SET ASCII;
```

- Cada `ALTER` con su `COMMIT`, y al final `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (71, '000071_add_firma_y_auditoria_a_msp_ga', CURRENT_TIMESTAMP)`.
- **Todas nulas, sin `DEFAULT`, sin trigger, sin `CHECK`** (regla dura 1 de `CLAUDE.md`). Las filas viejas quedan con `TIPO` nulo y se **leen** como evidencia en Go (D2); Go escribe `TIPO` **siempre** en filas nuevas.
- El down en orden inverso, y termina con `DELETE FROM MSP_MIGRATIONS WHERE ID = 71`.
- **El ciclo up → down → up**, en una base **desechable** restaurada de `msp-test-db.fbk.gz` (nunca en la de desarrollo compartida), con la salida en el reporte. Necesita la `000070` ya corregida.

### D2. Persistencia en `garfb`

- `ImagenRepo.Registrar` escribe `TIPO` y `HUELLA`. Un `ID` repetido → `domain.ErrImagenDuplicada`, distinguido **antes** de `firebird.MapError` con la misma técnica que `esClaveDuplicada` (código del driver **y** nombre de la llave primaria), probado contra la base con un control que no se confunda.
- `ImagenRepo.Obtener(id)` → la imagen o `domain.ErrImagenNoEncontrada`.
- `ImagenRepo.ListarPorEventos(ids)`: **una** consulta con `WHERE EVENTO_ID IN (…)`, ordenada `EVENTO_ID, CREATED_AT, ID`; con la lista vacía, slice vacío **sin** tocar la base; en lotes de 1,000 si llegan más (el límite del `IN` de Firebird).
- La lectura de imágenes: `TIPO` nulo → `TipoImagenEvidencia`; un valor que no es del catálogo → error de hidratación (como los demás enums).
- `insertarEvento`, `scanEvento` y los mappers con `HUELLA`, `DISTANCIA_ENTREGA_M` y `AUDITORIA_GPS`.
- **`Guardar` inserta `ImagenesPendientesForRepo()`** en la misma transacción, **después** de los eventos (la llave foránea lo exige). Hoy la única es la firma de `Entregar`; Kevin la agrega en su entrega 1 (jueves 12pm).
- Borra `infra/garfb/contrato_pendiente_test.go` cuando implementes `Obtener` y `ListarPorEventos`.

### D3. El helper de blobs — `app/blob.go`, en la entrega 1

Lo usan tu subida de evidencia y la firma de Kevin, así que su firma es exactamente ésta y va el **jueves 29 a las 12pm**:

```go
// guardarBlob valida (no vacío, JPEG o PNG por contenido, máximo maxBytes) y
// guarda los bytes con una ruta única por intento bajo prefijo. Devuelve la ruta.
func (s *Service) guardarBlob(ctx context.Context, prefijo string, contenido []byte, maxBytes int64) (string, error)

// descartarBlobSiNoQuedo borra ruta salvo que la imagen imagenID exista con
// exactamente esa ruta. Nunca devuelve error: lo registra en el log.
func (s *Service) descartarBlobSiNoQuedo(ctx context.Context, imagenID uuid.UUID, ruta string)
```

- Vacío → `ErrImagenVacia`; ni JPEG ni PNG según `http.DetectContentType` sobre los bytes (no lo que diga el cliente) → `ErrImagenFormatoInvalido`; más de `maxBytes` → `ErrImagenMuyGrande`.
- La ruta es **única por intento**: `garantias/<garantía>/<uuid nuevo>.<ext>`. Nunca se deriva de la clave: si un reintento escribiera en la misma ruta y luego la borrara por ser reintento, borraría la evidencia del primero.
- `descartarBlobSiNoQuedo` es la **única** forma de borrar: compara la ruta de la fila con la ruta que se acaba de escribir, y sólo borra si no coinciden o si no hay fila.

### D4. Subida de evidencia — `AdjuntarImagen`

- `app/adjuntar_imagen.go` (tuyo): `func (s *Service) AdjuntarImagen(ctx, cmd AdjuntarImagenCmd) (*domain.Imagen, error)`, con `AdjuntarImagenCmd{GarantiaID, EventoID uuid.UUID; Description string; Contenido []byte; ClaveIdempotencia string}`.
- Orden: usuario → permiso **`actualizar`** → clave normalizada → huella con **`huellaDe(cmd)`** de Kevin (`app/huella.go`, entrega 1 del jueves; incluye los bytes) → `guardarBlob` (máximo **10 MB**) → transacción: el evento existe **y** es de esa garantía (si no, `ErrImagenEventoAjeno`), ¿ya hay imagen con `ID` = clave? (misma huella → devuelve la existente sin escribir; otra huella o sin huella → `ErrClaveIdempotenciaMismatch`), `NewImagen` con `ID` = clave, `Tipo: TipoImagenEvidencia`, `SubidaPor` del usuario, `Huella`; `Registrar` → al final, **siempre**, `descartarBlobSiNoQuedo`.
- Un `ErrImagenDuplicada` de `Registrar` (dos subidas simultáneas con la misma clave) se resuelve como en `resolverDuplicada`: se relee y se compara la huella.
- **Nunca** crea firmas: el formulario no tiene campo de tipo.
- Ruta: `POST /garantias/{id}/eventos/{eid}/imagenes` (`adjuntar-imagen`), multipart con `archivo` y `descripcion`, embebiendo `EncabezadoIdempotencia`. Responde **200** con la imagen (`id`, `tipo`, `descripcion`, `subida_por`, `created_at`, `url`).

### D5. Descarga, catálogos y detalle

- **`GET /garantias/{id}/eventos/{eid}/imagenes/{iid}`** (`obtener-imagen`), permiso **`leer`**: el binario con su `Content-Type`. La imagen tiene que existir, ser de ese evento, y el evento de esa garantía; cualquier otra cosa → 404, sin decir cuál falló.
- **`GET /garantias/catalogos`** (`obtener-catalogos`), permiso **`leer`**: `etapas` y `ubicaciones` en el orden de `domain.Etapas()` y `domain.Ubicaciones()`, estados de folio, tipos de evento, y `transiciones` por origen y etapa con `domain.SiguientesPorAvanzar`. Nada escrito a mano: todo sale del dominio.
- **El detalle** (`obtener-garantia`) trae, en cada evento, sus imágenes (sin la huella) y, en `folio_cerrado`, la distancia y la auditoría. **Cuatro consultas fijas** (folio, artículos, eventos, imágenes), sin importar cuántos eventos tenga: contadas con el contador de `query_count_test.go`. Si para eso el detalle necesita algo más del repositorio, va por un puerto declarado en `outbound`, no por una aserción de tipo escondida.
- Los métodos de lectura nuevos van en `app.Consultas` (`consultas.go`, tuyo). Sus dependencias nuevas, en `ConsultasDeps`.

### D6. El módulo encendido — entrega 3

- **`internal/garantias/module.go`**: `func Module() fx.Option`, como `canal`. Construye repositorios, `FilesystemProvider` sobre `STORAGE_DIR/garantias`, `identidad`, `app.Service`, `app.Consultas` y `garhttp.Deps`. No importa nada fuera de `internal/garantias/**` e `internal/platform/**`: las dos funciones de identidad (`identidad.UsuarioFn` y `PermisoFn`) llegan del grafo.
- **`cmd/api/garantias_wiring.go`**: provee esas dos funciones a partir de `auth` (el usuario plantado por el middleware y la revisión de permiso).
- **`cmd/api/main.go`**: `garantias.Module()` en `appOptions()`, una línea.
- **`cmd/api/server.go`**: un `r.Group` con `skipAuthForPublicDocs(authn.Handler)` y `garhttp.MountRouter`. **Sin** el middleware `idem` (la idempotencia es la del dominio, spec §6) y sin captura de intentos fallidos en v1.
- **`internal/auth/domain/permission_codes.go`**: los cinco códigos del §7 con la categoría `garantias`, con las descripciones de `garantias.Permisos()`. Una prueba que compare las dos listas.
- **Prueba de arranque**, en `cmd/api/garantias_wiring_test.go`: el grafo de fx resuelve, y sobre el router real `GET /v2/garantias` sin token → 401 y las diecinueve operaciones aparecen en el OpenAPI montado.
- **`Makefile`**: `coverage-garantias` con `scripts/go-cover-gate.sh` y un `scripts/cover-floors-garantias.txt` (`domain` 99, `app` 95, `infra/garfb` 85, `infra/garhttp` 90, `infra/storage` 85, `infra/identidad` 100, `DEFAULT` 70), y `test-mutation-garantias-domain` / `-app` con `$(GREMLINS_GATE)` al 80/90, como los de visitas. Agrega `./internal/garantias/infra/...` a `test-firebird-all`.

### D7. El barrido de seguridad final — con Kevin, entrega 3

En `garhttp/security_test.go` (tuyo): table-driven sobre **todas** las operaciones del OpenAPI montado (diecinueve). Por cada una, sin usuario → 401 y sin el permiso → 403, **sin ninguna escritura**; y con cada uno de los otros cuatro permisos → 403. La tabla se compara contra `api.OpenAPI().Paths`: una operación sin fila **hace fallar** la prueba. Para las catorce de Kevin usas su `ejemplosComandos()` (`garhttp/ejemplos_comandos_test.go`, entrega 1 de él), que trae una petición válida de cada una; las tuyas las agregas con el mismo tipo `ejemploOperacion`.

### D8. Cómo convivir en el mismo paquete

- Tus fakes y helpers de prueba nuevos en `app` llevan el prefijo **`fakeAdjunto…`** / **`adjunto…`**. Los de Kevin llevan `fakeFirma…` / `firma…`.
- `routes.go` es tuyo, pero la llamada a `registrarComandos(api, d.Comandos)` **no se quita ni se mueve**: es la costura con Kevin.

---

## Entrega 1 — jueves 29, 12pm

D1, D2 y D3.

### Pruebas de la entrega 1

1. **Ciclo de la migración** up → down → up en la base desechable, con la salida literal.
2. **Ida y vuelta** contra Firebird de `TIPO`, `HUELLA`, `DISTANCIA_ENTREGA_M` y `AUDITORIA_GPS`, campo por campo, con valores y con nulos.
3. Una fila de imagen con `TIPO` nulo, insertada a mano con SQL dentro de la prueba, se lee como evidencia.
4. `ErrImagenDuplicada` contra la base, con el control de otra llave que no se confunda.
5. `ListarPorEventos`: sólo los eventos pedidos, en orden, **una** consulta contada; lista vacía sin consulta; más de 1,000 IDs en lotes.
6. `Obtener` de una imagen inexistente → `ErrImagenNoEncontrada`.
7. `guardarBlob` y `descartarBlobSiNoQuedo` con fakes: cada rechazo; ruta distinta en dos llamadas con los mismos bytes; borra si no hay fila; borra si la fila tiene otra ruta; **no** borra si la fila tiene esa ruta.

### Mutantes de la entrega 1

Los corres tú y los reportas, cada uno como **parche `diff`** con la salida roja de la prueba que lo mata y la verde al revertirlo.

| # | Mutante |
|---|---|
| R1 | `Registrar` no escribe `TIPO` |
| R2 | la lectura convierte `TIPO` nulo en `firma` |
| R3 | `insertarEvento` no escribe `HUELLA` |
| R4 | `ListarPorEventos` ignora la lista y devuelve todas las imágenes |
| R5 | `descartarBlobSiNoQuedo` borra aunque la fila tenga esa ruta |
| R6 | `guardarBlob` confía en el tipo que declara el cliente |
| R7 | el down no borra la fila de `MSP_MIGRATIONS` |

---

## Entrega 2 — viernes 30, 12pm

D2 (`Guardar` con las imágenes pendientes, ya con el dominio de Kevin), D4 y D5.

### Pruebas de la entrega 2

- `Guardar` con la firma de `Entregar`: la imagen queda en la base, con `TIPO='firma'`, colgada del `folio_entregado`, en la misma transacción (si el insert de la imagen falla, no queda ni el evento).
- `AdjuntarImagen`, con fakes: 401 y 403 **sin escribir ni guardar blob**; evento de otra garantía → 404 y blob borrado; reintento con los mismos bytes → la misma imagen, una sola fila, **ningún blob huérfano**; reintento con otros bytes o con otra descripción → 409 y el blob del reintento borrado; error de la base → blob borrado; éxito → blob presente.
- HTTP: camino feliz de las tres rutas con el JSON **completo**; sin `Idempotency-Key` → 422; archivo vacío, de más de 10 MB o que no es imagen → 422; descarga con el `Content-Type` y los bytes exactos; descarga cruzada (imagen de otro evento, evento de otra garantía) → 404.
- El detalle con imágenes en **cuatro** consultas, contadas, para un folio con varios eventos y varias imágenes.
- Una prueba de punta a punta por HTTP: subir una imagen y leerla en el detalle y en la descarga, contra `garfb` real dentro de `WithTestTransaction`.

### Mutantes de la entrega 2

| # | Mutante |
|---|---|
| R8 | `Guardar` no inserta las imágenes pendientes |
| R9 | `AdjuntarImagen` calcula la huella sin los bytes |
| R10 | `AdjuntarImagen` no revisa que el evento sea de la garantía |
| R11 | `AdjuntarImagen` pide `leer` en vez de `actualizar` |
| R12 | la descarga no revisa que el evento sea de la garantía |
| R13 | el detalle lee las imágenes evento por evento |

---

## Entrega 3 — lunes 2 de noviembre, 4pm, con Kevin

D6 y D7, sobre `feat/garantias-integracion` con las entregas 2 de los dos ya fusionadas.

| # | Mutante |
|---|---|
| R14 | quitar una fila de la tabla del barrido de seguridad |
| R15 | montar garantías dentro del grupo con `idem` |
| R16 | un permiso de garantías falta en `permission_codes.go` |

**Pisos:** `app` (lo tuyo) ≥ 95%, `garhttp` ≥ 90%, `garfb` ≥ 85% (con `FB_DATABASE`), medidos con `make coverage-garantias` sobre el SHA que entregas.

---

## Archivos que puedes tocar

```
migrations-firebird/000071_add_firma_y_auditoria_a_msp_ga.up.sql / .down.sql
internal/garantias/infra/garfb/**                 menos e2e_test.go, que es de Kevin
internal/garantias/infra/storage/**
internal/garantias/infra/identidad/**
internal/garantias/app/blob.go, adjuntar_imagen.go, consultas.go
internal/garantias/app/blob_test.go, adjuntar_imagen_test.go, adjunto_*_test.go, consultas_*test.go
internal/garantias/infra/garhttp/routes.go, dto.go, dto_mapper.go, errores.go, handlers_consultas.go
internal/garantias/infra/garhttp/handlers_imagenes.go, dto_imagenes.go, handlers_catalogos.go
internal/garantias/infra/garhttp/security_test.go y las pruebas que ya son tuyas
                                                  (handlers_test.go, handlers_fakes_test.go, e2e_test.go,
                                                  routes_test.go) más *_imagenes_test.go y *_catalogos_test.go
internal/garantias/module.go, module_test.go
cmd/api/garantias_wiring.go, garantias_wiring_test.go, main.go (la línea), server.go (el grupo)
internal/auth/domain/permission_codes.go y su prueba
Makefile (los targets de garantías y test-firebird-all), scripts/cover-floors-garantias.txt
docs/superpowers/plans/garantias-task-10-report.md
```

Nada más. En particular: **nada** en `domain/`, `ports/` ni en los demás archivos de `app/` (son de Kevin o del contrato); nada en `routes_comandos.go`, `handlers_comandos.go`, `dto_comandos.go` ni `*_comandos_test.go`. Si necesitas algo de un archivo de Kevin, pídeselo.

---

## Errores que no se repiten

Cada uno ya pasó en este módulo. Una entrega que repita uno rebota.

1. **Huella incompleta.** Una huella que deja fuera un campo convierte un reintento con otro contenido en "ya lo hice", y la operación se pierde sin error. En la subida, el campo que más fácil se olvida son los bytes: por eso R9.
2. **Pruebas que no fallan al revertir el arreglo.** Cada regla nueva tiene una prueba que se pone **roja** al quitarla. En el reporte va el parche del mutante, la salida roja y la verde. Una prueba que sigue verde con el mutante no prueba la regla.
3. **Evidencia que no sale del SHA commiteado.** Cada número del reporte (cobertura, consultas contadas, mutantes, salidas) cita el **SHA** y el **comando** exacto con que se obtuvo, corrido sobre ese SHA con el árbol limpio y `go clean -testcache`. Un número del árbol de trabajo no cuenta.
4. **Renombrar sin cambiar el contenido.** La `000070` cambió de nombre de archivo y adentro siguió diciendo `VALUES (65, '000065_…')`. Medido el 26 de octubre en una base desechable: `make fb-migrate-up` truena con `PK_MSP_MIGRATIONS ("ID" = 65)` y deja las columnas creadas sin registro. En la tuya, el ID interno es **71** y el nombre es el del archivo, y lo compruebas con `git grep -n "VALUES (71, '000071_add_firma_y_auditoria_a_msp_ga'" migrations-firebird/` y `git grep -n "ID = 71" migrations-firebird/000071_*`, con la salida en el reporte.
5. **Pruebas que dependen de una tabla vacía o de datos previos.** Las de la bandeja hacen `require.Len(t, pagina.Items, 1)` sobre todo lo que cumple el filtro (`bandeja_repo_test.go`, nueve lugares; revisa cada uno): pasan en una base vacía y truenan en la de desarrollo en cuanto alguien guardó un folio en esa etapa. Eso ya se corrigió en el #28; en todo lo nuevo compara **sólo los IDs que sembraste** en esa prueba, sembrando también el caso que **no** debe aparecer.

---

## Verification, en cada entrega

```sh
gofmt -l internal/garantias cmd/api
go vet ./internal/garantias/... ./cmd/api/...
go build ./...
golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/... ./cmd/api/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/... ./cmd/api/...
go tool cover -func=cov.out | grep -E 'garantias/(app|infra)'
make check-sealed MODULE=garantias
grep -rn 'Ã' internal/garantias        # debe salir vacío
```

Desde la entrega 3, además `make coverage-garantias` y `make test-mutation-garantias-app`. Con la base arriba, y una vez sin `FB_DATABASE`: lo de Firebird se salta y nada falla.

---

## Reporte

`docs/superpowers/plans/garantias-task-10-report.md`, uno solo que crece con las tres entregas. Lleva:
- el SHA de cada entrega y la salida **literal** de los comandos de arriba sobre ese SHA;
- el ciclo up → down → up de la `000071` y el `git grep` del punto 4;
- la tabla de mutantes R1–R16, cada uno con su parche y su salida roja/verde;
- las consultas del detalle, contadas;
- un ejemplo real de petición y respuesta de cada ruta tuya;
- la tabla del barrido de seguridad con las diecinueve operaciones.

Lo que hayas hecho distinto al brief, declarado con el porqué.

---

## Calendario

| Cuándo | Qué tiene que estar empujado |
|---|---|
| **Miércoles 28, hasta la 1pm** | Leer el brief, el spec y el commit de contrato; rama creada desde `feat/garantias-integracion` |
| **Jueves 29, 12pm** | **Entrega 1**: la `000071` con su ciclo, D2 completa, `app/blob.go`, R1–R7, los dos pendientes del #28 y el #29, y PR contra `feat/garantias-integracion` |
| **Jueves 29, al cierre** | `AdjuntarImagen` con sus pruebas |
| **Viernes 30, 12pm** | **Entrega 2**: las tres rutas, el detalle con imágenes, `Guardar` con la firma, R8–R13 y PR |
| **Viernes 30, al cierre** | `module.go`, el cableado y los permisos |
| **Lunes 2 de noviembre, 4pm** | **Entrega 3**, con Kevin: barrido de seguridad final, prueba de arranque, `Makefile`, R14–R16 y PR |

Los avances diarios los reviso. Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
