# Garantías — Tarea 6: el repositorio Firebird (`infra/garfb`)

> **Rama:** `feat/garantias-garfb`, ya creada desde `main` con el dominio de garantías adentro. No la crees: `git fetch && git switch feat/garantias-garfb`. El jueves 1 Kevin entrega los puertos, y desde ese día trabajas sobre ellos (ver "Cómo arrancar sin esperar").
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md)
> **Tanda:** 1 — el repositorio Firebird, lado de escritura
> **Plazo:** una semana, del miércoles 30 de septiembre al **martes 6 de octubre**; entregas ese día, al final de tu jornada. Única, no por partes. El calendario está al final.
> **En paralelo:** Kevin hace la tarea 5: los puertos que tú implementas y los primeros comandos que los van a usar.

## Por qué esta tarea

El módulo de garantías ya tiene su dominio completo: el folio, sus artículos y la línea de tiempo viven en memoria y respetan sus reglas. **Nada de eso se guarda todavía.** Esta tarea es la que lo lleva a la base: cuatro tablas `MSP_GA_*` que hoy están vacías en todos los entornos.

Es la tarea que más se parece a lo que practicaste en `txlab`, y no por casualidad. La regla más importante del módulo es de transacciones: **un artículo no cambia de etapa sin que su evento quede guardado en la misma transacción** (spec §4.4). El dominio garantiza que los dos cambios ocurren juntos en memoria; que lleguen juntos a la base, o que no llegue ninguno, depende de tu código.

Un defecto aquí no truena: deja un expediente que miente. Si la etapa se guarda y el evento no, el sistema dice que el mueble está en el taller sin que nadie sepa quién lo mandó ni cuándo. Ese expediente es la evidencia si un cliente reclama.

### Qué entra esta semana y qué no

Esta semana es **el lado de escritura**: guardar un folio, cargarlo, cargarlo con candado, la línea de tiempo y el generador de folio. Es donde vive la regla de §4.4, y es lo que necesitan los comandos de Kevin.

**No entran**, y serán tu siguiente semana: `BandejaRepo.Listar` (la bandeja con filtros y paginación) e `ImagenRepo` (la evidencia). Kevin los declara en los puertos, pero **tú no los implementas todavía**. No escribas ni un borrador: un método a medias en la rama es código que nadie revisó.

---

## Leer antes de escribir (obligatorio, en este orden)

1. **`CLAUDE.md`**, las reglas duras 1, 2 y 7. La 1 (nada de lógica en la base) dice cómo se escribe cada `INSERT`; la 7, cómo se prueba contra Firebird sin ensuciar la base compartida.
2. **El spec, §2.1, §2.4, §3 completo, §4.4 y §5.** El §3 es tu esquema, y el §5, lo que se espera de ti.
3. **`migrations-firebird/000050_create_msp_ga_garantias.up.sql`** y **`000057_add_reemplaza_a_msp_ga_articulo.up.sql`**. Son las tablas reales, con sus `NOT NULL`, sus `UNIQUE` y sus llaves foráneas. Si el spec y la migración no coinciden, **gana la migración**.
4. **`docs/module-standards/DATETIME_HANDLING.md`** y **`ENCODING_HANDLING.md`**. Son obligatorios, y de aquí sale el error más común de un repositorio nuevo.
5. **`internal/platform/firebird/transaction.go`**: `GetQuerier`, `RequireTx` y `RunInTx`. Ya los conoces de `txlab`; aquí se usan los de la plataforma, **no** tu paquete de práctica.
6. **`internal/flota/infra/flotafb/repo.go` y `repo_test.go`.** Es el repositorio de otro módulo sellado y tu referencia de forma, tanto del código como de las pruebas de integración.
7. **`internal/garantias/domain/`**, en particular `garantia.go`, `articulo.go` y `evento.go`. Tú no escribes en el dominio: lo **lees** para construir y reconstruir entidades. Fíjate en los `Hydrate*` (los usas al leer) y en los `*ForRepo` (los usas al escribir).
8. **`internal/ventas/infra/ventfb/venta_repo.go`**, sólo `Save`, `Update` y `LockByID`: es el repositorio de referencia del repo para escribir un agregado con hijas y para tomar un candado.

---

## Las decisiones ya tomadas — no las adivines

Si alguna te parece equivocada, dilo **antes** de escribir las pruebas, no después.

1. **Las interfaces las escribe Kevin, no tú.** Viven en `internal/garantias/ports/outbound/repos.go`, y sus firmas están fijadas en el brief de la tarea 5 (§A.1), letra por letra. **Léelas ahí**. Tú escribes los tipos concretos que las cumplen, y cada uno lleva la aserción de compilación:
   ```go
   var _ outbound.GarantiaRepo = (*GarantiaRepo)(nil)
   ```
   Si una firma te parece mal, dilo y la cambiamos en los dos briefs a la vez. **No la cambies tú**: son dos tareas y nunca escriben el mismo archivo.
2. **Todo lo que escribe exige transacción.** `Crear`, `Guardar` y `ObtenerParaActualizar` empiezan con `firebird.RequireTx(ctx)` y **fallan** si no hay transacción, en vez de abrir una propia. Un `Guardar` que abriera su propia transacción rompería la regla de §4.4 en cuanto el comando haga dos cosas: el candado de `ObtenerParaActualizar` se soltaría antes de guardar. Las lecturas (`Obtener`, `ObtenerPorFolio`, `ListarPorGarantia`…) usan `firebird.GetQuerier(ctx, pool.DB)` y funcionan dentro o fuera de una transacción. **Nunca uses `pool.DB` directo.**
3. **`Crear` inserta todo; `Guardar` actualiza y agrega.**
   - `Crear`: `INSERT` del folio, de cada artículo y de cada evento pendiente, en ese orden (las llaves foráneas lo exigen).
   - `Guardar`:
     - `UPDATE` del folio, **sólo de las columnas que el dominio puede cambiar**: `ESTADO`, `CERRADO_EN` y `UPDATED_AT`. Si afecta 0 filas, devuelve `domain.ErrGarantiaNoEncontrada`.
     - Por cada artículo, un `UPDATE ... WHERE ID = ? AND GARANTIA_ID = ?` de sus columnas mutables. Si afecta 0 filas, el artículo es nuevo y va un `INSERT`.
     - Después, un `INSERT` por cada evento pendiente.

   **No uses `UPDATE OR INSERT` ni `MERGE`:** el driver tiene un defecto conocido con los parámetros de `MERGE`, y el "actualiza; si no había, inserta" se lee y se prueba mejor.
4. **Los artículos se escriben en el orden en que el agregado los tiene.** Un reemplazo apunta a su original con `REEMPLAZA_A`, que es llave foránea a la misma tabla: el original tiene que existir antes. `ArticulosForRepo()` ya los devuelve en ese orden.
5. **`ObtenerParaActualizar` toma el candado sobre el folio**, con `SELECT ... FROM MSP_GA_GARANTIA WHERE ID = ? WITH LOCK`, y después lee los artículos. El candado dura hasta el commit o el rollback. Por qué importa: dos teléfonos mueven dos artículos distintos del mismo folio a la vez. Sin candado, el segundo `Guardar` reescribe el artículo del primero con la etapa vieja, y los dos eventos quedan guardados. El expediente queda mintiendo.
6. **La clave de idempotencia repetida se traduce a un error del dominio.** Si el `INSERT` del evento choca con `UQ_MSP_GA_EVENTO_CLAVE`, devuelves **`domain.ErrClaveIdempotenciaDuplicada`**, no el error genérico. `firebird.MapError` convierte cualquier `UNIQUE` en `firebird_unique_violation` y pierde el nombre de la restricción, así que tienes que distinguirlo **antes** de mapear, buscando el nombre de la restricción en el error del driver. El comando de Kevin depende de este error para responder "éxito" cuando el teléfono reenvía, así que **pruébalo contra la base real**, no con un mock.
7. **Fechas:** `firebird.ToWallClock(t)` al escribir todo `TIMESTAMP`, y `firebird.ScanUTCTime` / `ScanNullUTCTime` al leer. **`VIGENCIA_HASTA` es `DATE`, no `TIMESTAMP`**: es un día del calendario, no un instante, y no se convierte de zona horaria. Escribe el año-mes-día tal cual llega y reléelo a medianoche UTC. Pruébalo con una fecha y declara en el reporte cómo lo resolviste.
8. **Textos:** las columnas `MSP_GA_*` son `UTF8`. **Nada de `firebird.Win1252` ni de `EncodeWin1252`**: eso es para las tablas de Microsip, y éstas no lo son. Los `BLOB SUB_TYPE TEXT` (`DESCRIPCION` del folio y del evento) se escanean a `[]byte`; mira `internal/platform/outboxfb/dispatcher.go:239` para ver cómo.
9. **`FolioGenerator.Siguiente` es `SELECT GEN_ID(GEN_MSP_GA_FOLIO, 1) FROM RDB$DATABASE`.** Ojo: el generador **no es transaccional**. Un número que se pidió dentro de una transacción que después se deshizo queda gastado para siempre. En producción eso deja huecos en los folios, y es aceptable; en las pruebas, cada corrida gasta números de la base de desarrollo, y también es aceptable. Es el único efecto permanente que tus pruebas pueden dejar.

---

## Entregables

### `internal/garantias/infra/garfb/`

| Archivo | Qué |
|---|---|
| `doc.go` | Paquete, y un párrafo sobre la regla de §4.4 y cómo la cumple este paquete |
| `queries.go` | Todo el SQL como constantes, **con la lista de columnas explícita**: nunca `SELECT *` |
| `garantia_repo.go` | `GarantiaRepo`: `Crear`, `Guardar`, `ObtenerParaActualizar`, `Obtener` y `ObtenerPorFolio` |
| `evento_repo.go` | `EventoRepo`: `ListarPorGarantia` y `ObtenerPorClaveIdempotencia` |
| `folio_generator.go` | `FolioGenerator`: `Siguiente` |
| `mappers.go` | De fila a entidad con `Hydrate*`, y de entidad a argumentos del `INSERT` |

Cada tipo con su constructor, `NewGarantiaRepo(pool *firebird.Pool) *GarantiaRepo` y así los demás, y con su aserción de compilación contra el puerto.

**Todo `INSERT` pasa `ID`, `CREATED_AT` y `UPDATED_AT` explícitos**, sacados de la entidad (regla dura 1). Si alguna columna `NOT NULL` te falta, el problema está en cómo lees la entidad, no en la tabla: **no toques ninguna migración**.

---

## Pruebas — de integración, contra Firebird

En `package garfb_test`. Se saltan solas sin `FB_DATABASE` (así sigue verde el CI) y **toda escritura va dentro de `fbtestutil.WithTestTransaction`**, que siempre hace rollback. La única excepción es la prueba del candado, más abajo.

Para armar los datos, **construye los agregados con el dominio** (`domain.AbrirGarantia`, `g.AgregarArticulo`, …), no con `INSERT` a mano. Si una prueba escribe filas que el dominio nunca produciría, lo que prueba es otra cosa. Usa nombres y domicilios mexicanos realistas, no `"test"` ni `"foo"`.

Lo mínimo:

1. **Ida y vuelta completa.** Un folio de origen `cliente` con todos los campos llenos (domicilio, GPS, vigencia, dos artículos): `Crear`, `Obtener` y comparar **campo por campo**, incluidos los de cada artículo. Lo mismo con uno de origen `piso`, con sus nulos. Cualquier campo que no compares es un campo que se puede perder sin que nadie se entere.
2. **Acentos y eñes.** Una descripción con `ñ`, `á` y `ü` en el folio, en el artículo y en el evento, y que vuelva idéntica.
3. **`Guardar` después de avanzar.** Cargar con `ObtenerParaActualizar`, avanzar un artículo, `Guardar`, releer: la etapa nueva está guardada **y** el evento también, con `ETAPA_DESDE`/`ETAPA_HASTA` correctos.
4. **`Guardar` con un artículo nuevo y un reemplazo.** `AgregarArticulo` sobre un folio ya guardado genera un `INSERT`; `AutorizarCambioFisico` guarda el original en `standby` y el reemplazo con `REEMPLAZA_A` apuntando al original.
5. **Todo o nada.** Provoca que falle el `INSERT` de un evento a mitad de `Guardar` (una clave de idempotencia que ya existe sirve) y comprueba que **tampoco quedó el cambio de etapa**. Ésta es **la prueba de §4.4** y la más importante de la tarea. Pista: dentro de `WithTestTransaction` todo se deshace al final de cualquier modo, así que para ver el "nada" necesitas un punto de guardado (`SAVEPOINT` / `ROLLBACK TO SAVEPOINT`) o comprobar el estado que ve el propio comando. Decide cómo, y explícalo en el reporte.
6. **La clave repetida.** Dos eventos con la misma `ClaveIdempotencia` → `errors.Is(err, domain.ErrClaveIdempotenciaDuplicada)`. Y el **control positivo**: una clave distinta **no** da ese error. Sin el control, la prueba pasaría también si todo error se convirtiera en ése.
7. **Los "no encontrado".** `Obtener`, `ObtenerPorFolio`, `ObtenerParaActualizar` y `Guardar` sobre un ID que no existe → `domain.ErrGarantiaNoEncontrada`.
8. **Sin transacción.** `Crear`, `Guardar` y `ObtenerParaActualizar` fuera de una transacción fallan, y **no escribieron nada**.
9. **`ListarPorGarantia`** en el orden que fija el puerto (`DEVICE_CREATED_AT`, luego `CREATED_AT`, luego `ID`), con dos eventos de mismo `DEVICE_CREATED_AT` para que el desempate por `CREATED_AT` se vea.
10. **`FolioGenerator`:** dos llamadas seguidas dan números distintos y crecientes.
11. **El candado.** Una prueba que demuestre que, con el folio tomado por `ObtenerParaActualizar` en una transacción, **una segunda transacción no puede tomarlo**. Con `RunInTxNoWait`, el segundo intento falla de inmediato con conflicto de candado en vez de quedarse esperando. Esta prueba necesita que el folio **exista en la base de verdad** (una fila sin commit no la ve la otra transacción), así que:
    - Es **la única** que hace commit. Llámala `TestGarantiaRepo_Candado_Commit` para que se vea.
    - Registra un `t.Cleanup` que borre **eventos, artículos y folio, en ese orden** (hijos antes que padres).
    - **Nunca descartes el error del borrado**: `require.NoError` en cada `DELETE`. Un cleanup que ignora su error deja las filas para siempre y aparenta que funcionó (regla dura 7).

**Cobertura ≥ 80%** en `internal/garantias/infra/garfb`, medida **con** `FB_DATABASE` (§8). Sin la base, las pruebas se saltan y la cobertura no significa nada.

---

## Cómo arrancar sin esperar

Los puertos de Kevin llegan el **jueves 1**. Hasta entonces:

- **Miércoles 30:** lectura. Las cuatro tablas, el dominio y `flotafb`.
- **Jueves 1:** `queries.go` completo, `folio_generator.go` con su prueba (no depende de ningún puerto: devuelve un `int`) y el andamiaje de pruebas, es decir, `requireFBEnv`, el pool y los constructores de agregados de ejemplo, que sólo usan el dominio del PR #21.
- **Desde el viernes 2:** trae los puertos. Si el PR de Kevin ya se fusionó, rebasea sobre `main`; si no, rebasea sobre su rama `feat/garantias-puertos`, y cuando se fusione, otra vez sobre `main`.

Si el viernes 2 los puertos todavía no existen, **dímelo ese día**.

---

## Archivos que puedes tocar

```
internal/garantias/infra/garfb/*.go
internal/garantias/infra/garfb/*_test.go
docs/superpowers/plans/garantias-task-6-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** En particular: nada en `domain/`, `ports/` ni `app/` (son de Kevin), **ninguna migración** y nada en `.golangci.yml`. Si necesitas algo del dominio que no existe, **me lo pides**; no lo agregues tú.

---

## Verification

Con la caché de tests limpia y con la base local arriba. Todos tienen que devolver 0:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && FB_DATABASE=/firebird/data/MUEBLERA.FDB go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/infra/garfb/
go tool cover -func=cov.out | tail -1        # ≥ 80.0%
make check-sealed MODULE=garantias
```

Después, **una vez**, sin `FB_DATABASE`: `go test ./internal/garantias/infra/garfb/`. Tiene que decir que se saltaron, no que fallaron. Así es como corre el CI.

---

## Reporte

`docs/superpowers/plans/garantias-task-6-report.md`, con:

- La salida **literal** de los comandos de Verification, pegada, no resumida.
- Qué entregaste de verdad, y las dos cosas que el brief te deja decidir: cómo guardas `VIGENCIA_HASTA` (decisión 7) y cómo probaste el "todo o nada" (prueba 5).
- **Cuántas consultas hace `Obtener`** para un folio con tres artículos, **medido**: contado en la prueba, no deducido. Tienen que ser dos, una para el folio y una para los artículos, no una por artículo.

Si algo del brief no lo hiciste o lo hiciste distinto, dilo ahí con el porqué. Una desviación declarada se discute; una escondida se rechaza. Si el reporte no coincide con el código, gana el código.

---

## Calendario y entrega

| Cuándo | Qué |
|---|---|
| Martes 29 (hoy) | Lectura del brief. |
| Miércoles 30 y jueves 1 | Lo de "Cómo arrancar sin esperar". |
| Viernes 2 y lunes 5 | `GarantiaRepo` y `EventoRepo` con sus pruebas. Empuja la rama al final de cada jornada: no es entrega y no lleva mensaje. |
| **Martes 6 de octubre, fin de tu jornada** | **Entrega.** Los tres repositorios de esta semana, sus pruebas y el reporte, de una sola vez, en un PR contra `main`. |

Los commits van con el formato del repo, `feat(garantias): ...`: el hook `commit-msg` rechaza cualquier otro. **Nunca uses `--no-verify`**: si un hook falla, el hook tiene razón hasta que se demuestre lo contrario, y si no entiendes por qué falla, pregúntame.

## Qué significa "terminado"

No es que compile. Es que **los comandos de Verification devuelvan 0 con la base arriba**, que la cobertura sea ≥ 80%, y que las pruebas 5, 6 y 11 existan y fallen si quitas lo que prueban. Haz esa prueba tú mismo antes de entregar: quita el candado, o el mapeo de la clave, y mira que la prueba se ponga roja. Una prueba que nunca viste fallar no sabes si prueba algo.

Si el lunes 5 ves que no llegas, dímelo el lunes, no el martes. La prioridad, si hay que elegir: las pruebas 5, 6 y 11 antes que cualquier otra cosa.

Si necesitas algo, dime.
