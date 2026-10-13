# Garantías — Tarea 8: el dominio completo y todos los comandos que faltan

> **Ramas:**
> - Entrega 1: `feat/garantias-dominio-v2`, ya creada desde `main` (`git fetch && git switch feat/garantias-dominio-v2`).
> - Entrega 2: `feat/garantias-comandos-v2`, que creas tú desde la entrega 1 **cuando el #26 ya esté en `main`**.
>
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md), §3.2, §3.3, §4 completo, §5, §7 y §8.
> **Plazo:** dos entregas, **viernes 16 de octubre, 12pm** y **martes 20 de octubre, 12pm**. El módulo va atrasado y ésta es la ruta crítica: no hay holgura.

## Qué construyes

Al terminar esta tarea, el folio de garantía puede recorrer **todo** su ciclo de vida desde `app`: apertura, recolección, diagnóstico, las dos rutas de reparación, cambio físico, la devolución fallida, el flujo de standby, entrega, cierre y cancelación. Cada paso deja su evento, su ubicación física y su desenlace.

| Entrega | Contenido | Cuándo |
|---|---|---|
| **1** | Corrección del dominio y migración `000065` | **Viernes 16, 12pm** |
| **2** | Los 10 comandos que faltan, el repositorio al día y las tres pruebas de punta a punta | **Martes 20, 12pm** |

**Antes que nada, el miércoles 14 en la mañana:** rebasea el #26 sobre `main` y cambia su base a `main`, como te pedí. Es el que desbloquea la entrega 2.

---

## Por qué hace falta corregir el dominio

Una auditoría del módulo midió seis huecos en el dominio que ya está en `main`. Cada uno se reprodujo con una prueba:

1. **Con el permiso de actualizar se saltan decisiones del permiso de autorizar.** `AvanzarArticulo` lleva un artículo de `dictamen_recibido` a `listo_entrega` sin dictamen, y de `standby` a `segunda_mano` sin desenlace y sin rol.
2. **La ubicación nunca cambia.** Un artículo llega a `reingresado_inventario` y sigue en `domicilio_cliente`. Es el requisito central del módulo (§1: saber dónde está cada cosa).
3. **Las reglas por origen no se aplican.** Un folio de cliente se salta la recolección; uno de piso entra a recolección.
4. **Se mueven artículos de un folio cancelado.**
5. **`IniciarProceso` y `MarcarListoEntrega` dejan el mismo evento**, `etapa_avanzada` sin desde ni hasta, y no se distinguen.
6. **La rama "no se pudo devolver" no se puede recorrer, y `devuelto`, `reparado` y `reemplazado` nunca se asignan.**

Esta tarea los cierra todos. Las reglas de negocio ya están decididas: abajo.

---

## Leer antes de escribir

1. El spec, **§4 completo**, con calma: es la máquina que vas a terminar.
2. Tu propio dominio en `main`: `garantia.go`, `articulo.go`, `transiciones.go`, `estado_folio.go` y `tipo_evento.go`.
3. Tu `app/service.go` del #26: `ejecutarFolio` es el molde de los 10 comandos nuevos.
4. `internal/garantias/infra/garfb/` de Ruben, en `main`: lo vas a tocar en la entrega 2.
5. `migrations-firebird/000050_*` y `000057_*`, up y down: tu migración sigue su forma.
6. `docs/module-standards/ENCODING_HANDLING.md`, por la normalización NFC.

---

## Las reglas de negocio, ya decididas — no se re-litigan

### R1. Origen

| Origen | Recorrido de entrada | Final del camino de reparación |
|---|---|---|
| `cliente` | `registrado → pendiente_recoleccion → recolectado → en_revision` (**obligatorio** pasar por la recolección) | `listo_entrega → entregado` |
| `piso` | `registrado → en_revision` (**nunca** recolección) | `listo_entrega → reingresado_inventario`, y también `en_revision → reingresado_inventario` directo |

- `entregado` sólo existe para `cliente` y `reingresado_inventario` sólo para `piso`.
- Agrega a la tabla la arista `listo_entrega → reingresado_inventario`. Sin ella, un artículo de piso reparado no tiene a dónde ir.
- **La regla vive en `transiciones.go`**, como un filtro por origen sobre la tabla única. No es una segunda tabla ni un `if` en los métodos.

### R2. Qué alcanza `AvanzarArticulo` y qué no

`AvanzarArticulo` (permiso `actualizar`) es para los pasos sin decisión. **Nunca** puede llegar a:

| Destino o salida | Sólo por |
|---|---|
| cualquier salida de `dictamen_recibido` | `RegistrarDictamen` |
| `espera_respuesta_cliente → cambio_autorizado` / `en_taller → cambio_autorizado` | `AutorizarCambioFisico` |
| `espera_respuesta_cliente → standby` | `MarcarNoDevuelto` (nuevo) |
| cualquier salida de `standby` | `RegistrarDesenlace` |
| `entregado` | `Entregar` (lo mueve el folio, no el artículo) |

`espera_respuesta_cliente → listo_entrega` (el cliente acepta el mueble como está) sí va por `AvanzarArticulo`.

### R3. Ubicación: automática por etapa, más el comando manual

Al **llegar** a estas etapas, la ubicación cambia sola:

| Etapa | Ubicación |
|---|---|
| `recolectado` | `en_transito` |
| `en_revision` | `almacen_revision` |
| `enviado_proveedor` | `proveedor` |
| `en_taller` | `taller` |
| `entregado` | `entregado` |
| `reingresado_inventario` | `almacen_revision` |
| `segunda_mano` | `almacen_segunda_mano` |
| `desarmado`, `merma` | `baja` |
| cualquier otra | **no cambia** |

El "no cambia" es a propósito. `dictamen_recibido` no mueve el mueble, porque puede seguir con el proveedor (§3.2). Ese regreso se registra a mano.

**El comando manual `MoverArticulo(articuloID, hasta Ubicacion, …)`:**
- Puede llevar a `en_transito`, `almacen_revision`, `taller`, `proveedor` o `almacen_segunda_mano`.
- **Nunca** lleva a `entregado` ni a `baja`: ésas sólo las pone la etapa.
- Mover a la misma ubicación en la que ya está se rechaza.
- Un artículo en etapa terminal no se mueve.

**El rastro:** todo evento que cambie la ubicación, automático o manual, guarda `UbicacionDesde` y `UbicacionHasta`. El manual deja un evento propio, `articulo_movido`.

**La tabla etapa → ubicación vive en `transiciones.go`**, junto a la de etapas.

### R4. Estado del folio: qué se permite

| Estado del folio | Operaciones sobre artículos |
|---|---|
| `abierto` | sólo `AgregarArticulo` |
| `en_proceso`, `listo_entrega` | todas |
| `entregado`, `cerrado`, `cancelado` | **ninguna**, salvo `RegistrarDesenlace` y `MoverArticulo` sobre artículos en `standby` |

`AgregarArticulo` conserva su regla actual: sólo en `abierto` y `en_proceso`. Un folio que ya está en `listo_entrega` no recibe artículos nuevos, porque rompería la guardia de `MarcarListoEntrega`.

La excepción del standby es el requisito del §3.2: el folio se cierra con el original todavía en standby, y su destino final se registra después. Implementa la regla en **una sola función** (estado del folio × operación); que no quede repartida en `if` por los métodos. Necesita su centinela, por ejemplo `ErrFolioNoAdmiteOperacion`.

### R5. Cancelar con un mueble fuera

`Cancelar` se **rechaza** si algún artículo del folio tiene ubicación `proveedor` o `en_transito` (centinela propio). El mueble tiene que regresar antes de cancelar. El `taller` es interno y no bloquea.

### R6. Desenlace automático

Al llegar a una etapa final del camino del cliente o del piso (`entregado` o `reingresado_inventario`), el artículo recibe su desenlace sin que nadie lo capture:

| Condición, en este orden | Desenlace |
|---|---|
| `rol = reemplazo` | `reemplazado` |
| dictamen `rechazada` o `sin_falla` | `devuelto` |
| pasó por `reparado_proveedor` o `reparado_taller` | `reparado` |
| ninguna de las anteriores (piso revisado sin reparación) | sin desenlace (`nil`) |

- `segunda_mano`, `desarmado` y `merma` se siguen capturando con `RegistrarDesenlace` desde standby, como hoy.
- `reparado`, `reemplazado` y `devuelto` **nunca** se capturan a mano: el centinela `ErrArticuloDesenlaceSoloParalelo` sigue.
- La regla de que "desenlace sólo en etapa terminal" se mantiene.

Para saber si "pasó por reparado", el artículo necesita recordarlo. No lo deduzcas del evento: el agregado no carga la línea de tiempo (decisión 7 de la tarea 4). Guárdalo en el propio artículo con un dato que tenga columna, o derívalo de `RUTA` + `DICTAMEN`. Si decides que hace falta una columna, va en la `000065` y lo declaras en el reporte.

### R7. Eventos del folio con tipo propio

`TipoEvento` gana cuatro valores:

| Valor | Lo emite |
|---|---|
| `folio_en_proceso` | `IniciarProceso` |
| `folio_listo_entrega` | `MarcarListoEntrega` |
| `articulo_movido` | `MoverArticulo` |
| `devolucion_fallida` | `MarcarNoDevuelto` |

Todos caben en `VARCHAR(28)`. Revisa que `Parse` y la prueba del catálogo los incluyan.

### R8. `MarcarNoDevuelto`: la rama "no se pudo devolver"

Lleva un artículo de `espera_respuesta_cliente` a `standby` **sin crear reemplazo**: el cliente nunca respondió o no quiso recibir un mueble funcional (§4.2). Es una decisión, así que exige `RolDecisor`. El permiso va en `app`: `autorizar`.

### R9. `Entregar`

- Exige el **GPS del agente**, que es la auditoría de la entrega (§4.1). Sin GPS se rechaza con su centinela.
- En un folio de **cliente**, cada artículo en `listo_entrega` pasa a `entregado`, con su ubicación `entregado`, su desenlace (R6) y **un evento por artículo**, más el `folio_entregado`.
- Los artículos en `standby` no se tocan.

### R10. El agregado dice qué cambió

`Articulo` recuerda si es nuevo, si fue modificado en esta sesión o si está limpio:
- `newArticulo` lo marca nuevo;
- `HydrateArticulo` lo deja limpio;
- cada mutador lo marca modificado, salvo que sea nuevo.

`Garantia` expone `ArticulosNuevosForRepo()` y `ArticulosModificadosForRepo()`, devolviendo copias. Con eso, en la entrega 2, `Guardar` deja de reescribir todos los artículos.

### R11. Catálogos para la pantalla

Exporta, sin exponer los mapas internos:
- `domain.Etapas()` y `domain.Ubicaciones()`, en orden estable;
- `domain.SiguientesPorAvanzar(etapa Etapa, origen OrigenFolio) []Etapa`: lo que `AvanzarArticulo` permite desde esa etapa para ese origen, ya filtrado por R1 y R2.

Con esto el frontend no reimplementa la máquina (spec §6).

### R12. Pendientes chicos que se cierran aquí

- **El GPS del domicilio** del folio se valida igual que el del evento: el par completo o nada, en rango y finito.
- **NFC:** todo texto libre que entra al dominio se normaliza con `norm.NFC` (`golang.org/x/text/unicode/norm`) en el constructor o mutador que lo recibe. Va en `ENCODING_HANDLING.md`, y sin eso `"Juárez"` escrito de dos formas son dos textos distintos.
- **`PermisoInfo.Descripcion` → `Description`.**
- **Una prueba** que distinga, en `AbrirGarantia`, el folio releído del agregado en memoria (nota 1 de tu última revisión).

---

## Entrega 1 — dominio y migración (viernes 16, 12pm)

**Migración `000065_add_ubicacion_a_msp_ga_evento`**, up y down, con la forma y el comentario de la `000057`:
- Agrega `UBICACION_DESDE` y `UBICACION_HASTA`, ambas `VARCHAR(28) CHARACTER SET ASCII` y nulas, a `MSP_GA_EVENTO`.
- Si R6 lo pide, incluye también la columna del artículo.
- Termina con el registro en `MSP_MIGRATIONS`.

Ojo con el número: `main` en GitHub llega a la `000062`, pero la `000063` y la `000064` ya existen en producción. **La tuya es la `000065`.** Aplícala en tu base de desarrollo con `make fb-migrate-up` y prueba también el down.

**Dominio:** R1 a R12, todo lo que es del dominio. `Evento` gana `ubicacionDesde` y `ubicacionHasta` (`*Ubicacion`) en `EventoParams`, `HydrateEventoParams` y sus getters.

### Pruebas de la entrega 1

En `domain`, **100% de cobertura**, que hoy ya tienes. Además:

1. **La máquina completa, exhaustiva:** para cada par (origen × etapa × destino), `AvanzarArticulo` acepta exactamente lo que dicen R1 y R2, y rechaza todo lo demás con su centinela. Es una tabla generada, no ejemplos: la máquina es finita, y el §8 pide recorrerla entera.
2. **Ubicación:**
   - cada fila de R3 con su caso;
   - el "no cambia" probado en al menos `dictamen_recibido`, `reparado_proveedor` y `listo_entrega`;
   - `MoverArticulo` con cada destino permitido, cada destino prohibido, la misma ubicación y un artículo terminal;
   - el evento guarda desde y hasta en los dos casos, automático y manual.
3. **Estado del folio (R4):** la tabla completa, estado × operación, generada, incluida la excepción del standby **con el folio cerrado**.
4. **R5:** cancelar con un artículo en `proveedor`, en `en_transito` y en `taller`; sólo el último pasa.
5. **R6:** cada fila de la tabla de desenlace, más el orden de prioridad (un reemplazo con dictamen `rechazada` es `reemplazado`).
6. **R7:** cada comando de folio emite su tipo.
7. **R8 y R9**, con sus rechazos. En R9: un folio con un artículo en `listo_entrega` y otro en `standby` entrega sólo el primero, y deja dos eventos de artículo más el del folio.
8. **R10:** nuevo, modificado y limpio en cada mutador, y después de hidratar.
9. **R11:** `SiguientesPorAvanzar` coincide con la tabla de la prueba 1, para cada etapa y cada origen.
10. **En cada rechazo, las dos mitades:** el estado no cambió **y** no se encoló ningún evento.

**Mutantes de la entrega 1:** los corres tú y los reportas. Cada uno tiene que hacer fallar alguna prueba:

| # | Mutante |
|---|---|
| D1 | quitar el filtro por origen |
| D2 | permitir que `AvanzarArticulo` salga de `dictamen_recibido` |
| D3 | quitar una fila de la tabla etapa → ubicación |
| D4 | permitir `MoverArticulo` a `entregado` |
| D5 | quitar la excepción del standby en R4 |
| D6 | dejar cancelar con un artículo en `proveedor` |
| D7 | invertir el orden de prioridad de R6 |
| D8 | que `Entregar` no exija GPS |
| D9 | que un mutador no marque el artículo como modificado |
| D10 | quitar la normalización NFC de un campo |

---

## Entrega 2 — comandos, repositorio y punta a punta (martes 20, 12pm)

**Los 10 comandos**, cada uno en su archivo y todos por `ejecutarFolio`:

| Comando | Permiso | Extra |
|---|---|---|
| `RegistrarDiagnostico` | `actualizar` | `RolDecisor` obligatorio |
| `RegistrarDictamen` | `actualizar` | |
| `AutorizarCambioFisico` | `autorizar` | `RolDecisor` obligatorio |
| `MarcarNoDevuelto` | `autorizar` | `RolDecisor` obligatorio |
| `RegistrarDesenlace` | `autorizar` | `RolDecisor` obligatorio |
| `MoverArticulo` | `actualizar` | |
| `MarcarListoEntrega` | `actualizar` | |
| `Entregar` | `cerrar` | GPS del agente obligatorio |
| `Cerrar` | `cerrar` | |
| `Cancelar` | `cerrar` | motivo obligatorio |

- Cada comando pasa a `ejecutarFolio` el **tipo de evento** que produce, igual que los del #26.
- Los strings que vienen de fuera se parsean con el `Parse` del dominio, y el error se devuelve tal cual.

**El repositorio** (`infra/garfb`), una vez que el trabajo de Ruben de esta semana esté en `main`:
- `insertarEvento`, `scanEvento` y los mappers con `UBICACION_DESDE` y `UBICACION_HASTA`, y la columna de R6 si la agregaste.
- **`Guardar` con R10:** inserta los nuevos, actualiza **sólo** los modificados, y un `UPDATE` que afecta 0 filas pasa a ser **error**, no un `INSERT` silencioso.
- Sus pruebas de integración al día. Las de Ruben siguen pasando.

**Las tres pruebas de punta a punta del §8,** a nivel `app` + `garfb` real, dentro de `WithTestTransaction`:
1. **Origen piso:** apertura, revisión, taller, reparado, `reingresado_inventario`, desenlace `reparado`, cierre.
2. **Cliente por proveedor:** recolección, diagnóstico proveedor, envío (ubicación `proveedor`), dictamen `aceptada`, regreso manual con `MoverArticulo`, `listo_entrega`, `Entregar` con GPS, desenlace `reparado`, cierre.
3. **Cliente por taller con cambio físico:** taller, `AutorizarCambioFisico`, se entrega el reemplazo (desenlace `reemplazado`), se cierra el folio **con el original en `standby`**, y **después de cerrado**, `RegistrarDesenlace` → `segunda_mano` (ubicación `almacen_segunda_mano`).

En las tres, al final:
- cada cambio tiene su evento en la base, con etapas y ubicaciones desde/hasta correctas;
- cero artículos o folios sin su evento;
- la línea de tiempo leída de la base cuenta la historia completa, en orden.

### Pruebas de la entrega 2

- Cada comando: su camino feliz, sin usuario → 401, sin permiso → 403 **sin ninguna escritura**, la repetición por clave, la clave de otro folio o de otro tipo, el rol faltante donde aplica y el error del dominio propagado.
- Una prueba que recorra la **tabla de permisos completa**: cada comando con cada uno de los cinco permisos, y que sólo pase el que le corresponde.

**Pisos:** `app` ≥ 95% y `garfb` ≥ 85%, con `FB_DATABASE`.

**Mutantes de la entrega 2:**

| # | Mutante |
|---|---|
| C1 | `AutorizarCambioFisico` pide `actualizar` en vez de `autorizar` |
| C2 | `Entregar` sin exigir GPS en `app` |
| C3 | `Guardar` actualiza todos los artículos, como antes |
| C4 | `Guardar` inserta en silencio cuando el `UPDATE` afecta 0 filas |
| C5 | `insertarEvento` no escribe `UBICACION_HASTA` |
| C6 | un comando pasa el tipo de evento equivocado a `ejecutarFolio` |

---

## Archivos que puedes tocar

```
Entrega 1
migrations-firebird/000065_*.up.sql / .down.sql
internal/garantias/domain/**
internal/garantias/garantias_contracts.go y su prueba
internal/garantias/app/abrir_garantia_test.go       (sólo la prueba de R12)

Entrega 2
internal/garantias/app/**          (excepto consultas*.go, que son de Ruben)
internal/garantias/infra/garfb/**
docs/superpowers/plans/garantias-task-8-report.md
```

Nada en `infra/garhttp/`, `infra/identidad/` ni `app/consultas*.go`: son de Ruben esta semana. Nada en `cmd/api/`.

---

## Verification, en cada entrega

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
go tool cover -func=cov.out | grep -E 'garantias/(domain|app|infra/garfb)'
make check-sealed MODULE=garantias
grep -rn 'Ã' internal/garantias        # debe salir vacío
```

Más el up y el down de la `000065` aplicados en tu base de desarrollo, con la salida pegada en el reporte.

---

## Reporte

`docs/superpowers/plans/garantias-task-8-report.md`, uno solo que crece con las dos entregas. Lleva:
- la salida **literal** de los comandos;
- las dos tablas de mutantes con su resultado;
- cómo resolviste el "pasó por reparado" de R6 y por qué;
- la línea de tiempo completa, leída de la base, de cada una de las tres pruebas de punta a punta.

Lo que hayas hecho distinto al brief, declarado con el porqué.

---

## Calendario

| Cuándo | Qué tiene que estar empujado |
|---|---|
| **Miércoles 14, en la mañana** | El #26 rebaseado sobre `main`, con su base cambiada a `main` |
| **Miércoles 14, al cierre** | Migración 000065, R1, R2 y R7, con sus pruebas |
| **Jueves 15, al cierre** | R3, R4, R5, R6 y R8, con sus pruebas |
| **Viernes 16, 12pm** | **Entrega 1**: R9 a R12, mutantes D1–D10 y PR |
| **Lunes 19, al cierre** | Los 10 comandos con sus pruebas |
| **Martes 20, 12pm** | **Entrega 2**: el repositorio al día, las tres pruebas de punta a punta, mutantes C1–C6 y PR |

Los avances diarios los reviso. Si en algún momento ves que no llegas, avísame ese mismo día.

Si necesitas algo, dime.
