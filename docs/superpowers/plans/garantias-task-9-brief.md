# Garantías — Tarea 9: firma, auditoría GPS, huella completa y las rutas de comandos

> **Rama:** `feat/garantias-t9`, que creas tú desde `feat/garantias-integracion` (`git fetch && git switch -c feat/garantias-t9 origin/feat/garantias-integracion`). El PR va contra `feat/garantias-integracion`, no contra `main`.
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md), actualizado el 26 de octubre: §3.3, §3.4, **§4.1 completo**, §6 (idempotencia y rutas), §7, §8 y §10.
> **Plazo:** **una sola entrega, martes 3 de noviembre, 1pm**, con la tarea completa e integrada con Ruben (tarea 10). No hay entregas parciales ni revisiones intermedias: empujas a tu rama antes de irte cada día (1pm) y avanzas lo más que puedas. No hay holgura.
> **En paralelo:** Ruben hace la tarea 10. Los archivos de cada uno están listados abajo y **no se cruzan**.

## Qué construyes

Al terminar, el folio se maneja **completo por HTTP**: las catorce rutas `POST` de comandos responden, cada una con su permiso, su validación y su idempotencia; `Entregar` exige la firma del cliente y `Cerrar` audita el GPS de la entrega contra el de la recolección.

| Parte | Contenido | Orden sugerido |
|---|---|---|
| **1** | Reglas de dominio (firma y auditoría GPS), la huella completa en `app` y las rutas JSON | Primero |
| **2** | `entregar` multipart con la firma, `cerrar` con justificación, los tres e2e por HTTP, mutantes y cobertura | Después |

### Antes que nada: lo que queda del #30

El #30 se cerró el miércoles 28 y su contenido ya está en `feat/garantias-integracion`, con la `000070` corregida y la regla 1 de piso ya arreglada. **Empieza tu rama desde ahí** (`git pull` primero). El #26 también está cerrado.

Dos pendientes del #30 entran en esta tarea (el detalle está en el último comentario del #30):
1. Pruebas que hoy no pueden fallar: los getters `Dictamen()`, `Ruta()` y `Desenlace()` (`*a.Dictamen() = rechazada` no cambia el agregado); `UpdatedAt` del folio con una hora distinta por paso en los diez comandos, y en `marcarNoDevuelto` y `moverA` a nivel artículo; el usuario de Identity con un nombre que no aparezca en ningún otro fake.
2. Las pruebas de la huella del #26 (`ClaveMismoComandoOtroArticulo`, `…OtraEtapa`, `CarreraConClaveDeOtroComando` y las demás) se portan junto con D1: son las que guardan la huella completa.

---

## Leer antes de escribir

1. El spec, **§4.1 y §6** como quedaron el 26 de octubre. Son las reglas de esta tarea.
2. **El commit de contrato** de `feat/garantias-integracion` (`feat(garantias): contrato compartido de las tareas 9 y 10`): `domain/tipo_imagen.go`, `auditoria_gps.go`, `huella.go`, los campos nuevos de `Imagen` y `Evento`, `ActorParams.Huella`, `ImagenesPendientesForRepo`, los centinelas nuevos al final de `errors.go`, `garhttp/idempotencia.go` y `garhttp/routes_comandos.go`. **Sus firmas no se cambian**: si una no te sirve, dímelo y la cambiamos en los dos briefs a la vez.
3. Tu `app/service.go`: `ejecutarFolio`, `revisarClave` y `coincideHuella`. Es lo que vas a cambiar en la parte 1.
4. `docs/module-standards/HUMA_WIRING.md`, completo. En especial "Validation note" (el orden de las validaciones) y "Multipart upload".
5. De Ruben, sólo para leer: `infra/garhttp/routes.go`, `dto.go`, `dto_mapper.go`, `errores.go` y `handlers_test.go`. Tus respuestas usan su `GarantiaDTO` y su `mapAppError`; no los copies.
6. `internal/ventas/infra/venthttp/handlers_imagenes.go` y sus pruebas: la referencia de un multipart en este repo.

---

## Las decisiones ya tomadas — no se re-litigan

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas.

### D1. La huella es el contenido completo de la petición

Hoy `huellaEvento` compara `tipo`, `articuloRef` y `etapaHasta`. No basta: con la misma clave, `MoverArticulo` a **otra** ubicación responde "ya lo hice" y la segunda operación se pierde (leído: `app/mover_articulo.go`, la huella es tipo + artículo; `app/service.go`, `coincideHuella`). Lo mismo `Cancelar` con otro motivo.

- `app/huella.go` (tuyo): `func huellaDe(cmd any) string`. Devuelve el SHA-256 en hex minúsculas de: el **nombre del tipo** del comando (`reflect.TypeOf(cmd).Name()`), un separador, y el JSON del comando con el campo `ClaveIdempotencia` puesto en vacío. Todos los demás campos cuentan, incluidos `GarantiaID` y `ArticuloID` (que vienen de la ruta), `DeviceCreatedAt` y el GPS.
- Ruben usa **la misma función** para `AdjuntarImagenCmd`, que lleva los bytes de la imagen. Por eso la firma es exactamente ésa, y la subes a tu rama el **jueves 29 temprano** y le avisas a Ruben.
- Cada comando la calcula **una vez**, la pone en `ActorParams.Huella` y el dominio la copia a cada evento.
- `revisarClave` y `resolverDuplicada` comparan **la huella guardada contra la de la petición**. Huella igual → repetición, 200 sin escribir. Huella distinta → `ErrClaveIdempotenciaMismatch` (409 `warranty_idempotency_key_mismatch`). Un evento **sin** huella nunca es repetición. La clave en otro folio sigue siendo `ErrClaveIdempotenciaDeOtroFolio`.
- `AbrirGarantia` sigue la misma regla.
- `huellaEvento` y `coincideHuella` desaparecen o se quedan sólo como defensa adicional (el tipo); decide y dilo en el reporte.

### D2. Firma de conformidad (spec §4.1)

- `Garantia.Entregar` recibe la firma como parámetro. Firma es `Ruta` (ya guardada por `app`) y, si quieres, el `ContentType`; el `ID` de la imagen lo deriva el dominio de la clave del comando con `claveDerivada(clave, "firma")`, para que un reintento caiga sobre la misma llave primaria.
- Sin firma → `ErrFirmaRequerida`, **sin** cambiar estado ni encolar eventos.
- Con firma: el agregado construye una `Imagen` con `Tipo: TipoImagenFirma`, `EventoID` = el ID del evento `folio_entregado` de **esa** llamada, `SubidaPor` = el usuario del actor y la `Huella` del actor, y la agrega a `imagenesPendientes`. Ruben hace que `Guardar` la inserte en la misma transacción, después de los eventos.
- `app.Entregar` recibe los bytes de la firma en el comando (`Firma []byte`, más el tipo de contenido). Antes de abrir la transacción: GPS presente (como hoy), firma no vacía, y guarda el blob con el helper de Ruben (D5). Después de la operación, **siempre**, llama a su "descartar si no quedó" (D5): si hubo error o fue un reintento, el blob que acabas de escribir se borra; si la fila quedó con esa ruta, se queda.
- Firma: JPEG o PNG, **máximo 1 MB**.

### D3. Auditoría GPS en `Cerrar` (spec §4.1)

- `app.Cerrar`, ya con el candado del folio, lee la línea de tiempo con `EventoRepo.ListarPorGarantia` y le pasa al dominio: el GPS del `folio_entregado` y los GPS de cada evento `etapa_avanzada` con `EtapaHasta = recolectado` que **tenga** coordenadas.
- El dominio calcula la distancia **haversine** con radio **6,371,000 m** y se queda con la **mayor**.
- `UmbralDesvioMetros = 300.0`, constante exportada del dominio, con un comentario que diga que es ajustable y que cambiarla es editar la constante y correr las pruebas.
- Distancia **mayor** que el umbral (no mayor o igual) → `con_desvio`, y exige `justificacion_desvio` no vacía (recortada, NFC). Sin ella → `ErrJustificacionDesvioRequerida`, sin tocar nada.
- Distancia menor o igual → `sin_desvio`. Si viene justificación, se guarda igual.
- Sin ningún punto de recolección con GPS → `no_aplica`, distancia `nil`, y el folio cierra.
- El evento `folio_cerrado` lleva `DistanciaEntregaM`, `AuditoriaGPS` y la justificación en `Description`.
- La regla y el cálculo viven en el dominio. `app` sólo junta los puntos.

### D4. Las catorce rutas `POST`

Todas en `garhttp/routes_comandos.go`, registradas desde `registrarComandos` (que `routes.go` ya llama), bajo el `security` de `bearerAuth`, con la entrada embebiendo `EncabezadoIdempotencia`. El handler copia `IdempotencyKey` a `ClaveIdempotencia` del comando: **nunca** genera una.

| OperationID | Ruta | Comando | Cuerpo |
|---|---|---|---|
| `abrir-garantia` | `POST /garantias` | `AbrirGarantia` | JSON |
| `iniciar-proceso` | `POST /garantias/{id}/iniciar-proceso` | `IniciarProceso` | JSON |
| `marcar-listo-entrega` | `POST /garantias/{id}/listo-entrega` | `MarcarListoEntrega` | JSON |
| `entregar-garantia` | `POST /garantias/{id}/entregar` | `Entregar` | **multipart** (D2) |
| `cerrar-garantia` | `POST /garantias/{id}/cerrar` | `Cerrar` | JSON, con `justificacion_desvio` opcional |
| `cancelar-garantia` | `POST /garantias/{id}/cancelar` | `Cancelar` | JSON |
| `agregar-articulo` | `POST /garantias/{id}/articulos` | `AgregarArticulo` | JSON |
| `avanzar-articulo` | `POST /garantias/{id}/articulos/{aid}/avanzar` | `AvanzarArticulo` | JSON |
| `registrar-diagnostico` | `…/articulos/{aid}/diagnostico` | `RegistrarDiagnostico` | JSON |
| `registrar-dictamen` | `…/articulos/{aid}/dictamen` | `RegistrarDictamen` | JSON |
| `mover-articulo` | `…/articulos/{aid}/mover` | `MoverArticulo` | JSON |
| `autorizar-cambio-fisico` | `…/articulos/{aid}/cambio-fisico` | `AutorizarCambioFisico` | JSON |
| `marcar-no-devuelto` | `…/articulos/{aid}/no-devuelto` | `MarcarNoDevuelto` | JSON |
| `registrar-desenlace` | `…/articulos/{aid}/desenlace` | `RegistrarDesenlace` | JSON |

- `{id}` y `{aid}` con `format:"uuid"`. Campos en `snake_case`, fechas RFC3339, enums con `enum:"…"` **y** parseados con el `Parse` del dominio.
- La respuesta de todas es **200** con el folio (`GarantiaDTO` de Ruben, sin línea de tiempo), la primera vez y en el reintento.
- El permiso lo revisa `app`, no el handler (decisión 4 de la tarea 5).
- Los errores salen por `mapAppError`.

### D5. Lo que tomas de Ruben

Ruben lo sube a su rama el **jueves 29 temprano**, en `app/blob.go`; lo traes con un merge de su rama:

```go
// guardarBlob valida (no vacío, JPEG o PNG por contenido, máximo maxBytes) y
// guarda los bytes con una ruta única por intento bajo prefijo. Devuelve la ruta.
func (s *Service) guardarBlob(ctx context.Context, prefijo string, contenido []byte, maxBytes int64) (string, error)

// descartarBlobSiNoQuedo borra ruta salvo que la imagen imagenID exista con
// exactamente esa ruta. Nunca devuelve error: lo registra en el log.
func (s *Service) descartarBlobSiNoQuedo(ctx context.Context, imagenID uuid.UUID, ruta string)
```

Hasta que llegue, en tus pruebas de `Entregar` usa los fakes de almacenamiento con tu prefijo (abajo). Si el jueves a la 1pm no está, avísame ese mismo día.

### D6. Cómo convivir en el mismo paquete

Los dos escriben en `app` y en `garhttp`. Para que la fusión compile:
- Tus fakes y helpers de prueba nuevos llevan el prefijo **`fakeFirma…`** / **`firma…`**. Los de Ruben llevan `fakeAdjunto…` / `adjunto…`.
- En `garhttp/ejemplos_comandos_test.go` (tuyo, **el jueves 29**) defines `type ejemploOperacion struct { OperationID, Method, Path, ContentType string; Body []byte }` y `func ejemplosComandos() []ejemploOperacion`, con una petición **válida** por cada una de tus operaciones: las trece JSON y `entregar-garantia`. Ruben la usa en el barrido de seguridad final; tú la usas en tus pruebas de 401/403.

---

## Parte 1

- **Dominio:** D2 (firma en `Entregar`) y D3 (auditoría en `Cerrar`).
- **`app`:** D1 completa (`huella.go` + `revisarClave`/`resolverDuplicada` + `AbrirGarantia`), `Cerrar` con D3, `Entregar` con la firma usando fakes.
- **`garhttp`:** las trece rutas JSON de D4 (todas menos `entregar-garantia`) y `ejemplosComandos()` con esas trece.

### Pruebas de la parte 1

1. **La huella, por reflexión, sobre cada `*Cmd`.** Una prueba table-driven que recorre **cada** comando exportado de `app` y, para **cada campo** salvo `ClaveIdempotencia`, cambia ese campo a otro valor válido y comprueba que la huella cambia. Y que cambiar sólo `ClaveIdempotencia` **no** la cambia. La lista de comandos no se escribe a mano: se saca con `go/parser` de los `type …Cmd struct` exportados de `app/*.go` (sin `_test.go`) y la prueba falla si alguno no está en la tabla. Así, un comando o un campo nuevo sin huella se pone rojo solo.
2. **Dos comandos con la misma forma** (`IniciarProcesoCmd` y `MarcarListoEntregaCmd`, por ejemplo) y los mismos valores dan huellas **distintas**.
3. **Por comando**, con fakes: misma clave + mismo contenido → éxito sin escribir; misma clave + un campo distinto → 409 `warranty_idempotency_key_mismatch` sin escribir; evento guardado sin huella → 409.
4. **Firma:** sin firma → `ErrFirmaRequerida` con estado y eventos intactos; con firma → una sola imagen pendiente, `TipoImagenFirma`, colgada del `folio_entregado` de esa llamada (no de un evento de artículo), con el ID derivado de la clave.
5. **Auditoría GPS, con distancias que puedes comprobar a mano:** un minuto de arco de latitud sobre un meridiano mide `6,371,000 × π / 10,800 ≈ 1,853.25 m`; úsalo para probar el cálculo. Para el umbral, puntos sobre el mismo meridiano a **299.9 m** y a **300.1 m** (Δlat = metros / 111,194.93). Más: varias recolecciones, gana la mayor; sin recolección → `no_aplica`; recolección sin GPS → `no_aplica`; desvío sin justificación → error sin cambios; desvío con justificación → `con_desvio` con la justificación en el evento.
6. **HTTP, con `httptest`**, por cada una de las trece rutas: camino feliz con el JSON **completo** de la respuesta; sin `Idempotency-Key` → 422; clave que no es UUID → 422; UUID inválido en la ruta → 422; un enum inválido → 422; error del dominio → su status; y **que la clave del encabezado es la que llega al comando** (no una generada).

### Mutantes de la parte 1

Los corres tú y los reportas, cada uno como **parche `diff`** con la salida roja de la prueba que lo mata y la verde al revertirlo.

| # | Mutante |
|---|---|
| K1 | `Entregar` no exige firma |
| K2 | el umbral compara con `>=` en vez de `>` |
| K3 | `Cerrar` se queda con la **menor** distancia |
| K4 | `Cerrar` con desvío no exige justificación |
| K5 | sin recolección, la auditoría da `sin_desvio` en vez de `no_aplica` |
| K6 | `huellaDe` ignora un campo (por ejemplo `GPSLat`) |
| K7 | `huellaDe` incluye `ClaveIdempotencia` |
| K8 | `revisarClave` toma un evento sin huella como repetición |
| K9 | `revisarClave` compara sólo el tipo |
| K10 | un handler pasa `uuid.NewString()` en vez del encabezado |
| K11 | la firma se cuelga del último evento de artículo y no del `folio_entregado` |

---

## Parte 2

- `entregar-garantia` multipart (D2), con `gps_lat`, `gps_lon`, `device_created_at` y el archivo `firma`, usando el helper de Ruben.
- Los **tres e2e por HTTP** del §8 (piso; cliente por proveedor; cliente por taller con cambio físico), de `httptest` a `app` a `garfb` real, dentro de `WithTestTransaction`, en `garhttp/e2e_comandos_test.go`. Además de lo que ya piden en la tarea 8:
  - el de cliente entrega **con firma**, y la imagen se lee de la base con `TIPO='firma'`, colgada del `folio_entregado`;
  - el de proveedor cierra **con desvío** y justificación; el de taller, **sin desvío**; el de piso, con `no_aplica`;
  - en uno de ellos, el **mismo** `POST` se manda dos veces con la misma clave (200 las dos, una sola fila) y una tercera con la misma clave y otro cuerpo (409, nada escrito).
- Tus e2e de `infra/garfb/e2e_test.go` al día con la firma y la auditoría.

### Pruebas de la parte 2

- `entregar-garantia`: sin firma → 422 `warranty_signature_required`; firma de 0 bytes, de más de 1 MB o que no es JPEG/PNG → 422; reintento con la misma firma → 200 y **ningún blob huérfano**; reintento con otra firma → 409 y el blob del reintento borrado; error de base → el blob borrado; éxito → el blob **sigue ahí**.
- 401 y 403 en las catorce, con las peticiones válidas de `ejemplosComandos()`.

### Mutantes de la parte 2

| # | Mutante |
|---|---|
| K12 | `Entregar` borra el blob aun cuando la imagen quedó registrada |
| K13 | `Entregar` no borra el blob en un reintento |
| K14 | `app.Entregar` pide `actualizar` en vez de `cerrar` |
| K15 | el handler de `entregar` lee `gps_lat` en el lugar de `gps_lon` |

**Pisos:** `domain` 100%, `app` ≥ 95%, `garhttp` ≥ 90%, `garfb` ≥ 85% (con `FB_DATABASE`). Medidos sobre el SHA que entregas.

---

## Archivos que puedes tocar

```
internal/garantias/domain/**                    menos tipo_imagen.go, auditoria_gps.go, huella.go
                                                y contrato_*_test.go, que son del contrato y no se editan;
                                                los campos y centinelas del contrato no cambian de firma
internal/garantias/app/**                       menos adjuntar_imagen*.go, blob*.go, consultas*.go
                                                y adjunto_*_test.go, que son de Ruben
internal/garantias/infra/garhttp/routes_comandos.go
internal/garantias/infra/garhttp/handlers_comandos.go, dto_comandos.go
internal/garantias/infra/garhttp/*_comandos_test.go, ejemplos_comandos_test.go
internal/garantias/infra/garfb/e2e_test.go
docs/superpowers/plans/garantias-task-9-report.md
```

Nada más. En particular: **nada** en `routes.go`, `dto.go`, `dto_mapper.go`, `errores.go`, `handlers_consultas.go` ni en el resto de `garfb/`; ninguna migración; nada en `cmd/api/`, `internal/auth/` ni el `Makefile`. Si necesitas algo de un archivo de Ruben, pídeselo.

---

## Errores que no se repiten

Cada uno ya pasó en este módulo. Si la entrega repite uno, rebota.

1. **Huella incompleta.** Una huella que deja fuera un campo convierte un reintento con otro contenido en "ya lo hice", y la operación se pierde sin error. Por eso la prueba 1 es por reflexión y su lista sale del código, no de memoria.
2. **Pruebas que no fallan al revertir el arreglo.** Cada regla nueva tiene una prueba que se pone **roja** al quitarla. En el reporte va el parche del mutante, la salida roja y la verde. Una prueba que sigue verde con el mutante no prueba la regla.
3. **Evidencia que no sale del SHA commiteado.** Cada número del reporte (cobertura, mutantes, salidas) cita el **SHA** y el **comando** exacto con que se obtuvo, corrido sobre ese SHA con el árbol limpio y `go clean -testcache`. Un número del árbol de trabajo no cuenta.
4. **Renombrar sin cambiar el contenido.** La `000070` cambió de nombre de archivo y adentro siguió diciendo 65 (ver arriba). Si tocas una migración, el ID interno es igual al número del archivo, y lo compruebas con `git grep -n "VALUES (70" migrations-firebird/000070_*` y `git grep -n "ID = 70" migrations-firebird/000070_*`, con la salida en el reporte.
5. **Pruebas que dependen de una tabla vacía o de datos previos.** Una prueba que cuenta "todo lo que hay" pasa en una base vacía y truena en la de desarrollo en cuanto alguien guardó un folio. Compara **sólo los IDs que sembraste** en esa prueba, y siembra también el caso que **no** debe aparecer.

---

## Verification, antes de entregar

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
go tool cover -func=cov.out | grep -E 'garantias/(domain|app|infra/garhttp|infra/garfb)'
make check-sealed MODULE=garantias
grep -rn 'Ã' internal/garantias        # debe salir vacío
```

Con la base arriba, y una vez sin `FB_DATABASE`: lo de Firebird se salta y nada falla.

---

## Reporte

`docs/superpowers/plans/garantias-task-9-report.md`, uno solo. Lleva:
- el SHA que entregas y la salida **literal** de los comandos de arriba sobre ese SHA;
- la tabla de mutantes K1–K15, cada uno con su parche y su salida roja/verde;
- la lista de comandos que encontró la prueba de la huella, con sus campos;
- un ejemplo real de petición y respuesta de cada ruta;
- la línea de tiempo leída de la base de cada e2e, con la distancia y el resultado de la auditoría.

Lo que hayas hecho distinto al brief, declarado con el porqué.

---

## Calendario

| Cuándo | Qué tiene que estar empujado |
|---|---|
| **Miércoles 28, hasta la 1pm** | Leer el brief, el spec y el commit de contrato; rama creada desde `feat/garantias-integracion` y empezar D1 |
| **Jueves 29, temprano** | `app/huella.go` empujado a tu rama y avisado a Ruben |
| **Jueves 29, 1pm** | Empujado lo que lleves (orden sugerido: D1, D2 y D3, las trece rutas JSON, los pendientes del #30) |
| **Viernes 30, 1pm** | Empujado `entregar-garantia` multipart y `cerrar` con justificación |
| **Lunes 2, 1pm** | Tu parte terminada: los tres e2e por HTTP, K1–K15 y cobertura |
| **Martes 3, 1pm** | **Entrega única**, con Ruben: integración, barrido de seguridad final sobre las diecinueve operaciones, reporte y PR contra `feat/garantias-integracion` |

Los avances diarios los reviso. Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
