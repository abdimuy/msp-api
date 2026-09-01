# Un pago no puede quedarse en un cajón que nadie abre

## Contexto

El 30 de agosto una clienta pagó **dos** cuentas con quince segundos de
diferencia. Una entró a Microsip; la otra no. La oficina lo reportó como un pago
perdido y no había rastro de él en ningún sitio: ni en Microsip, ni en intentos
fallidos, ni en los registros del servidor.

Estaba guardado. En `MSP_PAGOS_RECIBIDOS`, con `ESTADO='P'`, esperando a que
algo lo trasladara a Microsip.

### Los dos defectos que lo escondieron

**1. El worker de reintentos vive 180 milisegundos.** Su bucle hereda el
contexto que `fx` entrega en `OnStart`, que lleva un **plazo de 15 segundos**.
Muere mucho antes de su primer aviso de 60. Medido en producción:

```
15:45:19.821  lifecycle: started  component=pago-retry-worker
15:45:20.001  pago_retry.tick     ← el único, 180 ms después
              ── nada más en 3 días de log ──
```

`ticks=1` en **diez** logs de arranques distintos. Es **uno de dieciséis**
workers con ese error; los otros quince ya usan `context.WithoutCancel`. Y el
mismo defecto **se diagnosticó y arregló en junio** en otro worker (`eee681d`) —
éste se quedó fuera de aquella barrida. Nunca ha funcionado un solo día desde
que nació (`d00eb49`, 1 de junio).

**2. El fallo se revierte.** En `aplicar_pago.go:75-82`, al fallar el escritor se
llama `RegistrarFallo` + `Update` y luego `return writerErr`. El `Update` se une
a la transacción externa, que acto seguido hace rollback. **`INTENTOS` y
`ULTIMO_ERROR` nunca se persisten.** Verificado en producción: las cuatro filas
atoradas tienen `UPDATED_AT` idéntico al milisegundo a `RECEIVED_AT`, contra
16,338 filas aplicadas que sí difieren.

Consecuencia combinada: como `INTENTOS` nunca sube, `elegible()` devuelve `true`
siempre, el backoff nunca entra y `MaxIntentos` nunca frena. Arreglar sólo el
contexto convertiría un worker muerto en uno que **martillea cada 60 segundos
para siempre**.

### El defecto de fondo

Un pago pasa por dos transacciones: guardarlo (se confirma) e insertarlo en
Microsip (mejor esfuerzo). El comentario del código dice *«los errores del
escritor no se propagan — el retry worker se encarga»*. **La garantía entera
vivía en un worker que nunca existió en la práctica.**

### Estado actual en producción

**4 pagos atorados, $1,450.** Ninguno bloqueado por `MaxIntentos` (todos en
`INTENTOS=0`), así que la cola entera se destraba al arreglar.

Los dos del 19 de agosto fallan con **`EX_SALDO_CARGO_EXCEDIDO`** — el importe
supera el saldo del cargo.

## Resultado buscado

Que un pago rechazado por Microsip **acabe en la pantalla de intentos fallidos**,
con su motivo escrito, en vez de en una cola invisible. Y que las pruebas
contengan el caso que causó esto.

## Global Constraints

Vinculantes para TODAS las tareas:

- **CLAUDE.md manda.** Sin lógica en la base de datos. Slices verticales
  (nunca importar `domain/`/`app/`/`infra/` de otro módulo). Código e
  identificadores en **inglés**; mensajes de cara al usuario y códigos de
  error de negocio en **español, minúsculas, sin punto final**.
- **Un solo despliegue, pero en commits separados** para poder revertir una
  pieza sin la otra. Cada tarea termina en su propio commit (formato
  convencional `<type>(<scope>): <subject>`, en español, **sin
  `Co-Authored-By` y sin ninguna atribución a Claude**).
- **Control positivo obligatorio**: cada prueba nueva debe verse **en rojo**
  antes del arreglo y en verde después. El vocabulario del repo es
  literalmente «control positivo» (`internal/ventas/infra/ventfb/aplicar_e2e_estatus_cliente_test.go:95`);
  el mejor molde de documentar qué detecta **y qué no** está en
  `internal/ventas/infra/ventfb/e2e_lineas_deltas_test.go:334-352`. Pega la salida en rojo y en verde en
  tu informe.
- **Censos antes/después** con `snapshotCounts` y comparación tabla por tabla,
  al estilo de `internal/ventas/infra/ventfb/atomicity_test.go` (9 pasos numerados).
- **El éxito devuelve 200**, no 201 (`internal/cobranza/infra/cobranzahttp/routes.go:118`,
  `DefaultStatus`). No cambiar eso.
- **Nunca `--no-verify`.** Las compuertas de lefthook y `.golangci.yml` mandan.
- **Nada de datos de prueba persistentes**: transacciones con rollback
  (`fbtestutil.WithTestTransaction`) o dobles. La única excepción es
  `internal/cobranza/infra/microsip`, que commitea por diseño y debe seguir su
  contrato de limpieza (hijos antes que padres) **sin descartar el error del
  borrado**.
- Antes de cualquier verificación: `go clean -testcache`.

## Lo que esto NO resuelve (no lo intentes)

- Los dos pagos del 19 de agosto (`EX_SALDO_CARGO_EXCEDIDO`): decisión de oficina.
- Que la app no lea `sincronizacion` (`V2PaymentsApi.kt:90`).
- `FB_STATEMENT_TIMEOUT = 10m`.
- Los diez teléfonos por debajo de la 2.17.3.
- Renombrar `AplicarPago` (el nombre miente: sólo inserta). Merece renombrarse,
  pero **no en este cambio**.

---

## Task 1 — El worker: que el bucle sobreviva al arranque

**Archivo de producción:** `internal/cobranza/app/pago_retry_worker.go`

En `Start` (alrededor de la línea 107) el bucle deriva su contexto del que `fx`
entrega en `OnStart`, que trae un deadline de `StartTimeout` (15 s por defecto).
Una línea, copiando el patrón de los otros quince workers del repo:

```go
loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
```

Busca en el repo cómo lo escriben los otros quince workers (`grep -rn
"WithoutCancel" internal/`) y **copia ese patrón exacto**, incluido cualquier
comentario explicativo que sea la convención local, en vez de inventar uno
nuevo.

**La prueba ya existe y está sin commitear:**
`internal/cobranza/app/pago_retry_worker_fx_test.go` (archivo sin seguimiento en
git). Monta el worker a través de `fx` igual que producción y exige varios
ticks. **Hoy falla; con el arreglo pasa.**

### Lo que tienes que hacer

1. **Control positivo primero.** Con el código SIN tocar, corre
   `go clean -testcache && go test -race -run 'TestPagoRetryWorker|TestFx_' ./internal/cobranza/app/`
   y guarda la salida **en rojo** en tu informe. Debe fallar
   `TestPagoRetryWorker_SigueTickeandoTrasElArranqueDeFx` con `ticks=1`.
2. Aplica el arreglo de una línea.
3. Corre lo mismo otra vez y guarda la salida **en verde**.
4. Comprueba que `TestPagoRetryWorker_StopSigueDeteniendoElBucle` sigue
   pasando: el apagado ordenado por `Stop` debe seguir mandando.
5. `gofmt -l`, `go vet` y `golangci-lint run ./internal/cobranza/...`.
6. **Commit propio** con los dos archivos (producción + la prueba fx que hasta
   ahora estaba sin commitear).

### Fuera de alcance

No toques `aplicar_pago.go`, `crear_pago*.go` ni nada de la Parte 1. El worker
**sigue haciendo falta**, pero cambia de papel: de ser la red que sostiene el
sistema pasa a atender sólo `AplicarPagoForzar` y sus propios fallos.

---

## Task 2 — Fundir las transacciones: el rechazo de Microsip propaga

Ésta es la pieza central. **Por cada línea de producción hay tres o cuatro de
prueba.**

### Producción

**`internal/cobranza/app/crear_pago_con_imagenes.go`**

- Eliminar `insertAndApplyPago` (≈:257) y `tryApplyAfterCreate` (≈:277). El
  escritor de Microsip pasa **dentro** de la misma transacción, como último
  paso.
- El camino **sin imágenes** (`len(imgs)==0`, ≈:105) hoy no tiene transacción
  alguna: pasa a usar la misma que el camino con imágenes. **Es el que usa el
  teléfono**, porque el pago va sin foto. Ojo con las guardas de dependencias
  al principio del método: hoy `txMgr`/`storage`/`pagosImagenes` sólo se exigen
  cuando `len(imgs)>0`; ahora `txMgr` y `microsipPago` hacen falta **siempre**.
- Un fallo del escritor **propaga**: rollback de todo y error HTTP. Con
  imágenes, los blobs ya escritos se limpian por el camino de `cleanupBlobs`
  que ya existe.
- El camino idempotente (`domain.ErrPagoYaExiste` → `errIdempotentReplay` →
  devolver el existente) **se conserva tal cual**.

**`internal/cobranza/app/aplicar_pago.go`**

- Separar los dos usos:
  - **Desde la creación**: el escritor corre en la transacción ya abierta. Sin
    `LockByID` ni recarga: la fila la acaba de insertar esta misma transacción.
  - **Desde el retry worker y desde `AplicarPagoForzar`**: sigue necesitando
    abrir la suya con su bloqueo pesimista (`LockByID` + `FindByID` + camino
    idempotente si ya está aplicada), porque opera sobre filas ya confirmadas.
- **Arreglar el rollback del fallo**: hoy `RegistrarFallo` + `Update` se unen a
  la transacción que acto seguido revierte, así que `INTENTOS` y `ULTIMO_ERROR`
  **nunca se persisten**. Sácalos de la transacción que se deshace. Busca en
  `internal/platform/firebird` cómo se abre una transacción **independiente**
  (el patrón que usa `saveIntent` con `context.WithoutCancel` es la referencia
  de intención, no necesariamente de mecanismo) y usa el mecanismo que ya exista
  en el repo. Si no existe ninguno, **es una decisión de diseño: documenta en el
  informe qué elegiste y por qué.**
- **Ese registro de fallo pertenece al camino del worker/forzar.** En el camino
  de creación no hay fila que actualizar: la transacción entera se revierte y el
  pago desaparece de `MSP_PAGOS_RECIBIDOS` — que es exactamente lo que se busca,
  porque el rechazo pasa a vivir en `MSP_FAILED_INTENTS` (una sola tabla, no
  dos).

**`internal/cobranza/infra/microsip/pago_writer.go` — no se toca.** Ya usa
`firebird.GetQuerier(ctx, …)`, o sea la transacción que le llega.

**Verificado y no es un riesgo (no lo re-litigues):** fundir **no alarga la
ventana de bloqueo**. El escritor ya es lo último dentro de su transacción y los
blobs ya se escriben fuera; lo que se antepone (el `INSERT` del pago) va antes
de tocar `DOCTOS_CC`. Sólo hay **dos filas compartidas** con `Cxc.exe` —
`SALDOS_CC(cliente,año,mes)` y `MSP_SALDOS_VENTAS(cargo)` —, el paso caro
(desglose de impuestos) está desactivado por `ESTATUS='P'`, y `GEN_FOLIO_TEMP`
no toma bloqueo (los huecos por rollback son cosméticos: 1.6 M de 100 M).

### Por qué esto vivió tres meses (léelo antes de tocar las pruebas)

**Ninguna prueba afirma el contrato viejo en voz alta.** Está afirmado *por
omisión*, ~25 veces: los tests construyen el servicio con el escritor en `nil` y
exigen que el pago se guarde igual. El helper lo dice: *«el fast-path fallará,
que es el comportamiento esperado; CrearPago sigue teniendo éxito»*
(`crear_pago_test.go:189-212`, `crear_pago_con_imagenes_test.go:28-50`).

Y faltan por completo: **cero** pruebas de fallo del escritor que atraviesen la
creación, **cero** a nivel HTTP, **cero** de fallo parcial.

### Pruebas existentes a reescribir (~25 en 4 archivos de este paquete)

| Archivo | Qué |
|---|---|
| `app/crear_pago_test.go` | ~10 tests + el helper `newWriteSvc` (≈:189). Dos (≈:455, ≈:489) verifican un bloque que desaparece |
| `app/crear_pago_con_imagenes_test.go` | ~6 de 19 + el helper (≈:28). Los de rollback se salvan |
| `app/crear_pago_con_imagenes_property_test.go` | 2 casos + los 3 `NewService` |
| `app/aplicar_pago_test.go` | 2 (≈:262, ≈:294) — la semántica `pendiente`+`Intentos=1` pasa a ser exclusiva del worker |

El helper que construye el servicio con el escritor en `nil` deja de servir:
ahora un escritor ausente **debe** hacer fallar la creación. Dale un escritor
que tenga éxito por defecto y reserva el fallo para las pruebas que lo buscan.

### Pruebas nuevas de este paquete (obligatorias)

**El caso de Juana, tal cual, es obligatorio.** Dos pagos del mismo cliente con
segundos de diferencia; uno entra y a otro Microsip lo rechaza. Exigir que el
rechazado **acabe visible** (a este nivel: que el error propague, para que la
capa HTTP lo pueda capturar) y que la tabla de pagos quede **sin su fila**.

1. **App** — escritor falla ⟹ error propagado + 0 pagos + 0 imágenes + 0 blobs.
   Con `snapshottingTxRunner` (`crear_pago_con_imagenes_test.go:220`), el único
   doble que modela el rollback de verdad.
2. **App** — idem por el camino **sin imágenes**, que es el que más cambia.
3. **App/propiedad** — nuevo `faultMicrosipWriter` en la tabla de
   `crear_pago_con_imagenes_property_test.go:32`, con el mismo invariante.
4. **App** — idempotencia tras un rollback por Microsip: el UUID **no queda
   quemado** y el reintento del teléfono funciona.
5. **Persistencia del fallo** — que `INTENTOS` y `ULTIMO_ERROR` **sobrevivan** al
   error en el camino del worker. Control positivo directo del segundo defecto.

### Cómo trabajar

Escribe primero las pruebas nuevas, córrelas contra el código **sin arreglar** y
guarda la salida **en rojo** (control positivo). Después aplica el cambio de
producción, reescribe las ~25 existentes y guarda la salida **en verde**.

Verificación de la tarea:
`go clean -testcache && go test -race -count=1 ./internal/cobranza/app/` más
`gofmt -l`, `go vet` y `golangci-lint run ./internal/cobranza/...`.

**Commit propio**, separado del de Task 1.

---

## Task 3 — El contrato en HTTP: un rechazo ya no devuelve 2xx

### Existente a corregir

`internal/cobranza/infra/cobranzahttp/handlers_pago_multipart_e2e_test.go`: 1 aserción y
el comentario (≈:100 y ≈:213) — «post-commit» deja de describir la realidad.

### Nuevas

5. **HTTP** — escritor falla ⟹ respuesta `>= 400` + 0 filas. Hoy **no existe
   ninguna** prueba de fallo del escritor a nivel HTTP.
6. **HTTP/e2e Firebird** — el fallo se **captura** con su resumen y su blob.
   Ampliar `internal/cobranza/infra/cobranzahttp/capture_replay_lifecycle_e2e_test.go`, que ya monta la cadena real
   con el `Store` de Firebird; hay que darle un campo `err` a
   `recordingMicrosipWriter`.
7. **HTTP/e2e Firebird** — tras el fallo, `MSP_PAGOS_RECIBIDOS` queda vacío; y el
   replay corregido deja **exactamente una** fila.

Recuerda: **el éxito sigue devolviendo 200**, no 201
(`internal/cobranza/infra/cobranzahttp/routes.go:118`, `DefaultStatus`).

Las pruebas que tocan Firebird se saltan solas sin `FB_DATABASE`. Corre las dos
formas: sin la variable (deben saltarse) y con ella (deben pasar):

```
go clean -testcache
env -u FB_DATABASE go test -short -race ./internal/cobranza/infra/cobranzahttp/...
set -a; source .env; set +a
go test -race -count=1 ./internal/cobranza/infra/cobranzahttp/...
```

Control positivo: cada prueba nueva, en rojo antes (revierte temporalmente el
cambio de producción de Task 2 con `git stash` si hace falta) y en verde después.
Censos antes/después con `snapshotCounts`.

**Commit propio.**

---

## Task 4 — La prueba contra Firebird real: un INSERT que revienta no deja rastro

8. **`internal/cobranza/infra/microsip`** — fallo **real** del tercer INSERT (FK
   inválida) ⟹ nada queda en `DOCTOS_CC`. Es el único sitio donde se puede
   probar de verdad que el rollback abarca los cinco INSERTs.

**Ese paquete commitea a la base compartida** y está fuera de
`make test-firebird-all` **por diseño**. Sigue su contrato de limpieza
(`internal/cobranza/infra/microsip/pago_writer_e2e_test.go:26-33`): hijos antes que padres, **incluido
`MSP_SALDOS_VENTAS`**, y **ningún borrado puede descartar su error** — si lo
descartas, las filas se quedan para siempre y la limpieza *parece* que funcionó.

Los siete tests que ya existen contra Microsip real son **todos camino feliz**;
éste es el primero que no lo es.

Antes de escribir: `make fb-snapshot NAME=pre-pago-writer-fk`.

Verificación (a mano, porque commitea):
```
go clean -testcache
set -a; source .env; set +a
go test -race -count=1 ./internal/cobranza/infra/microsip/...
```
Y después, comprobar que la base quedó **como estaba**: censo antes/después.

**Commit propio.**

---

## Task 5 — La captura del 401

`cmd/api/server.go:361` — `r.Use(authn.Handler, cobranzaCapture)`: la
autenticación va primero y al rechazar corta la cadena, así que **ningún 401 se
captura jamás**. Igual en ventas (:246) y visitas (:348).

Sin esto, el diseño nuevo mantiene un agujero por el que se cuela justo este
caso: el teléfono manda el pago con la sesión vencida, el API responde 401 y no
queda rastro de nada.

La captura de dentro necesita el `CurrentUser` para poder reproducir la petición
—por eso está ahí—, así que la solución es una **segunda captura por fuera**,
acotada a los 401, que guarde lo que se pueda aunque no sepa qué usuario era.

Diseño esperado: extender `failedintent.Config` con una restricción de estados
(por ejemplo, capturar sólo un conjunto dado) para que la instancia externa no
duplique lo que la interna ya captura. La interna sigue viendo todo lo que llega
autenticado; la externa **sólo** los 401. Mira `shouldCapture` y `handle` en
`internal/platform/failedintent/failedintent.go` y la dedup del `Store` antes de
decidir la forma. **Justifica en el informe** por qué la externa no puede
duplicar filas de la interna.

Prueba obligatoria:

11. **Captura del 401** — que un pago con sesión vencida deje fila en
    `MSP_FAILED_INTENTS`, con su cuerpo, aunque `UsuarioID` vaya nulo. Y el
    control negativo: que un 4xx **autenticado** siga dejando **una sola** fila,
    no dos.

Aplica el mismo tratamiento a ventas (:246) y visitas (:348) — es el mismo
agujero — o **justifica en el informe** por qué se quedan fuera.

**Commit propio.**

---

## Task 6 — El caos

- **Fallo a mitad**: que entre `DOCTOS_CC` pero no el importe. **Hoy ningún doble
  lo permite** — hay que construirlo, o con un `Querier` que cuente
  `ExecContext` y reviente en el N-ésimo, o contra Firebird real violando una FK.
- **Concurrencia**: dos POST con el mismo UUID a la vez. Ya hay molde en
  `internal/cobranza/app/crear_pago_con_imagenes_property_test.go:120` y en
  `internal/cobranza/infra/ventfb/pagos_recibidos_concurrency_test.go`.
- **Conflicto de bloqueo con `Cxc.exe`**: que el rechazo por bloqueo se comporte
  y **no deje la conexión colgada**. Ojo con
  `reference_firebirdsql_ctx_cancel_poisons_pool`: cancelar el contexto envenena
  el pool del driver.

Cada una con su control positivo y su censo. Si alguna resulta imposible de
montar con honestidad, **dilo en el informe y explica por qué** en vez de
escribir una prueba que no prueba nada.

**Commit propio.**

---

## Task 7 — Verificación completa

```
go clean -testcache
gofmt -l internal/ cmd/
go vet ./internal/... ./cmd/...
golangci-lint run ./internal/... ./cmd/...
env -u FB_DATABASE go test -short -race ./internal/...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./internal/... ./cmd/...
set -a; source .env; set +a
make FB_DATABASE="$FB_DATABASE" test-firebird-all
go test -race -count=1 ./internal/cobranza/infra/microsip/...   # a mano: commitea
```

Notas del repo que ahorran un diagnóstico falso:

- `make FB_DATABASE=... test-firebird-all` necesita `-timeout 1800s` (ya está en
  el target).
- `internal/analytics/infra/analyticsfb` **se cuelga** con `.env` sourceado en un
  `go test ./...` completo. No es culpa de este cambio.
- `golangci-lint` hay que correrlo **a mano sobre `./...`**: el
  `--new-from-rev` de lefthook deja pasar cosas.

Pega la salida real de cada comando en el informe. Si algo falla, **dilo**: no
declares verde lo que no viste verde.

**Sin commit propio** (esta tarea no cambia código; si hace falta un arreglo,
va en el commit de la pieza que corresponda).
