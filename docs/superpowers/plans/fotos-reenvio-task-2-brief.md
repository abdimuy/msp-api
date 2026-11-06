# Fotos de pagos — Tarea 2: corregir el PR #35 y arreglar el borrado del comprobante al reintentar

> **Para:** Joao · **Entrega única:** miércoles 11 de noviembre, 1pm
> **Ramas:** la Parte 1 va en la misma rama y el mismo PR (`test/fotos-reenvio-medicion`, PR #35). La Parte 2 va en una rama nueva, `fix/fotos-reenvio-no-borra`, con un PR contra `main`.
> **Tipo de tarea:** corrección de una medición y arreglo de un defecto que **toca dinero**. Se aplica la §8 de `CLAUDE.md` completa.
> Todo se entrega al final, de una vez. No hay entregas parciales, avances obligatorios ni revisiones intermedias.

---

> **Antes que nada: el nombre y el contrato del arreglo ya están fijados.**
>
> Hay una versión más amplia de este arreglo en trabajo del dueño que todavía no está en `main`. Cuando se integre, se queda esa versión, y **tus pruebas tienen que seguir en verde sobre ella**: se vuelven la guarda de las dos. Por eso:
> - Usas exactamente el nombre y el contrato de `cleanupBlobsSinFila` que fija D3. No lo renombres ni le cambies la firma.
> - Haces el arreglo **mínimo** de D2 y D3. No tocas la validación del saldo ni nada fuera de lo que dicen las decisiones.
> - Números de evidencia: la Parte 1 usa `E-COB-060`; la Parte 2, `E-COB-061` y `E-VEN-014` en adelante. Ya están verificados como libres; no busques otros.

---

## Dónde encaja

En la tarea 1 escribiste la prueba que demuestra el defecto: un reintento del teléfono escribe la foto del comprobante **en la misma ruta** que la original (`pagos/<pago>/<imagen><ext>`), descubre que el pago ya existía y llama a `cleanupBlobs`. Eso borra la foto del pago que sí quedó. El control positivo de tu prueba **sí se reproduce**: el dueño lo rehízo el 5 de noviembre, quitó la línea y la prueba se puso roja.

Esta tarea tiene dos partes:
1. **Corregir el PR #35** para que la evidencia sostenga lo que afirma, la herramienta no dé un conteo falso y el proceso quede limpio.
2. **El arreglo**: que un reintento nunca borre ni sobrescriba el comprobante que ya quedó. Con eso, tu prueba de la tarea 1 se invierte. Además, medir con pruebas si ventas y visitas tienen el mismo defecto.

---

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md`, la **§8** otra vez, despacio. Esta vez hay un arreglo de por medio y aplican las dos compuertas.
2. `docs/registro-de-evidencia.md` **§4.3** (la forma de una afirmación) y **§4.5** (las dos compuertas). Después, una entrada completa como modelo: `E-COB-018` en `docs/evidencia/cobranza.md`.
3. Los hallazgos de la revisión de tu PR, en el último comentario del #35. **Todos** se atienden.
4. `internal/cobranza/app/crear_pago_con_imagenes.go` y `internal/cobranza/app/adjuntar_imagen_pago.go` completos.
5. `internal/visitas/app/registrar_visita_con_imagenes.go`, **líneas 60-83 y 139-170**. Es el precedente de este arreglo **dentro del repo**: visitas pregunta qué existe antes de escribir y por eso un reintento no borra nada.
6. `internal/garantias/app/blob.go` en `origin/feat/garantias-integracion` (`git show origin/feat/garantias-integracion:internal/garantias/app/blob.go`). Es la otra forma de resolverlo, una ruta única por intento, y aquí **no** se adopta (ver D4).
7. `internal/ventas/app/crear_venta_con_imagenes.go` completo, `internal/ventas/infra/venthttp/multipart.go:20-30` y `internal/ventas/infra/venthttp/handlers_ventas.go:48-57`.
8. `docs/module-standards/DATETIME_HANDLING.md`, por el `CREATED_AT` de la herramienta.

---

## Parte 1 — corregir el PR #35

Misma rama (`test/fotos-reenvio-medicion`) y mismo PR. **No abras otro.** Sube las correcciones en cuanto las tengas. No cuenta como avance ni espera revisión: el dueño fusiona el #35 cuando lo vea listo.

### Bloqueantes

**B1. La entrada de evidencia.** Hoy dice `E-COB-019` y declara MEDIDO con la salida de la herramienta (`Total de filas: 0`). Esa salida no mide la afirmación, y además no es literal: dice `Con archivo`, pero el código imprime `ConArchivo`.
- **Número: `E-COB-060`.** El 019 ya lo usa `docs/registro-de-evidencia.md:169` como ejemplo, y los planes de visitas lo dan por ocupado (`docs/superpowers/plans/2026-10-16-visitas-p1-envio-blindado.md:37`). Del 020 al 059 también están tomados. Cambia la cabecera "Siguiente identificador libre" a `E-COB-061`. Si al momento de escribir el dueño te asigna otro número en el PR, usa ése.
- Usa la forma de `docs/registro-de-evidencia.md` §4.3. La entrada lleva:
  - un título después del número (`### E-COB-060 · Un reintento de un pago con imagen borra el comprobante original`);
  - `Nivel: MEDIDO` y `Estado: VIGENTE`;
  - la fecha, dónde se midió (tu máquina, y si fue WSL, dilo) y el **SHA**;
  - **Cómo:** el comando de la **prueba** (`go test -run TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal -v ./internal/cobranza/app/`) y su salida literal. La herramienta de conteo **no** mide esta afirmación: mide otra cosa (cuántos comprobantes faltan) y va en "Qué falta";
  - **Control positivo:** la salida roja al comentar `s.cleanupBlobs(ctx, storedKeys)` del caso `errIdempotentReplay`, y la verde al restaurarla, las dos de **este** SHA;
  - **Vive en:** `crear_pago_con_imagenes.go:123-133` y `cobranzahttp/multipart_helpers.go:36-42`;
  - **Guardada por:** el nombre de la prueba;
  - **Qué falta:** el conteo de producción con la herramienta, que lo corre el dueño;
  - **Caduca si:** la ruta del comprobante deja de ser estable o el teléfono deja de mandar `id_<n>`.

**B2. Las salidas del reporte se reproducen en el SHA del head, no en la base.** Tu reporte declara `f2bd21ba`, que es `main`, y ahí no existen ni la prueba ni la herramienta. Además, la salida roja que pegaste (`:137 … Should be true`) es de una versión anterior de la prueba: en tu head, la aserción está en `:146` y dice `An error is expected but got nil`. Hazlo así: haz commit de todo, anota el SHA, corre **cada** comando sobre ese SHA con el árbol limpio y después de `go clean -testcache`, y pega la salida literal. Quita la sección 6 duplicada.

**B3. `STORAGE_DIR` es obligatorio.** Hoy, si falta, la herramienta usa `./var/uploads` relativo al directorio desde donde se corre, y no avisa. El dueño lo midió: con la misma fila, sin `STORAGE_DIR` salió `Sin archivo: 1`. En producción, eso sería una falsa alarma de comprobantes perdidos. La herramienta tiene que:
- terminar con un error en español y salida **distinta de 0** si `STORAGE_DIR` falta o está vacía;
- terminar igual si el directorio no existe o no es un directorio;
- imprimir en la primera línea la **ruta absoluta** resuelta (`filepath.Abs`).

Cada uno de los tres casos lleva su prueba.

**B4. No se saltan hooks.** La descripción del PR dice que excluiste `lint-full` y `test-firebird-all` con `LEFTHOOK_EXCLUDE` por "errores heredados de staticcheck en main". Eso es falso: `golangci-lint run --timeout 15m ./...` sobre `main` da `0 issues.` con la versión que fija `ci.yml`, y el check de CI de tu PR está en verde. Excluir un hook equivale a un `--no-verify`. Corrige la descripción del PR. Si un hook te falla en tu máquina, el camino es **preguntar en el PR con la salida pegada**, no excluirlo. Si `test-firebird-all` falla porque falta el seed, pega la salida en el PR y pregunta.

### Menores (también se atienden)

1. **La guarda anti-escritura** (`main_test.go`) no distingue mayúsculas, recorre **todos** los `.go` del paquete que no sean de prueba y prohíbe también `Exec`. Su control positivo: con un mutante `db.Exec("delete from MSP_PAGOS_IMAGENES")`, la prueba tiene que ponerse roja. Pega el parche y la salida.
2. **`os.Stat`:** separa `errors.Is(err, fs.ErrNotExist)` de cualquier otro error. Los otros se cuentan aparte (`Error al revisar: N`) y se listan. Un "acceso denegado" no es un comprobante perdido.
3. **`CREATED_AT`** se lee con `firebird.ScanUTCTime` (DATETIME_HANDLING), no crudo.
4. **El comentario de la prueba** dice que afirma el comportamiento **actual**, que se pondrá roja con el arreglo de la tarea 2 y que ahí se invierte, citando `E-COB-060`.
5. **El commit** cita `E-COB-060` en el mensaje.
6. **El storage de la prueba:** declara en el reporte que usaste `localTestStorage` en lugar del `FilesystemProvider` real porque depguard impide importar infra desde `app` (`app-no-infra`, `.golangci.yml:459`). Fue correcto no forzarlo. Lo correcto también era decirlo o preguntarlo, no resolverlo en silencio. En la Parte 2 se cierra con una prueba en `cmd/api` (D6).
7. **Ventas en el reporte:** baja la respuesta a **INFERIDO**. Agrega que la ruta es estable (`venthttp/multipart.go:24-30`) y que no está demostrado que un reintento llegue a `storeAllImagenBlobs`: antes están las validaciones (`crear_venta_con_imagenes.go:61, 68, 80`) y el middleware de idempotencia (`cmd/api/server.go:221`). La Parte 2 lo mide.

### Archivos de la Parte 1

Los mismos cinco del #35 y ninguno más:
```
internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go
scripts/medicion/fotos-pagos-faltantes/main.go
scripts/medicion/fotos-pagos-faltantes/main_test.go
docs/evidencia/cobranza.md                       (sólo tu entrada y la cabecera)
docs/superpowers/plans/fotos-reenvio-task-1-report.md
```

---

## Parte 2 — el arreglo

Rama `fix/fotos-reenvio-no-borra`, sacada de `main` **después** de que se fusione el #35. Si el #35 sigue sin fusionar cuando necesites empezar, sácala del head del #35, dilo en la descripción del PR y haz rebase sobre `main` cuando se fusione.

### Las decisiones ya tomadas — no se re-litigan

Si alguna te parece equivocada, dilo en el PR **antes** de escribir las pruebas. Todas las citas son de `origin/main` (`f2bd21ba`) y las LEÍ el 2026-11-06.

**D1. Los pagos que ya existen no cambian de ruta.** No hay migración, no se renombra ningún archivo y no se toca `STORAGE_KEY` en filas existentes. Nadie reconstruye la ruta: todo el que la usa la lee de la fila.
- La descarga: `adjuntar_imagen_pago.go:192` (`s.storage.Get(ctx, img.Storage().Key())`).
- El borrado: `:155`.
- El repositorio: `ventfb/pagos_recibidos_repo.go:270-290`.
- La respuesta al cliente **no** expone la ruta: `cobranzahttp/dto_pago_imagenes.go:25-35` sólo lleva `id`, `mime`, tamaño, descripción y fechas.
- La única construcción de `"pagos/"` en Go es `cobranzahttp/multipart_helpers.go:41`.

**D2. Un intento sólo borra lo que él mismo escribió y que ninguna fila guardada nombra.** Las tres llamadas a `cleanupBlobs` de `CrearPagoConImagenes` (`crear_pago_con_imagenes.go:125, 132, 135`) pasan a:

```go
// cleanupBlobsSinFila borra las llaves que escribió este intento SALVO las
// que nombra una fila de MSP_PAGOS_IMAGENES de pagoID. Si las filas no se
// pueden leer, no borra nada: un archivo huérfano es el mal menor; una fila
// sin su archivo es un comprobante perdido.
func (s *Service) cleanupBlobsSinFila(ctx context.Context, pagoID uuid.UUID, keys []string)
```

- Lee las filas con `s.pagosImagenes.ListImagenes(ctx, pagoID)`: **una** consulta por intento, no una por imagen.
- Si la lectura falla, registra un `slog.WarnContext` con el pago y el error, y no borra nada. Es el mismo principio que `descartarBlobSiNoQuedo` de garantías (`blob.go:60-93`), que tampoco borra cuando no puede leer (`:73-81`).
- `cleanupBlobs` deja de usarse en este archivo. **No** cambies `bestEffortDeleteBlob` (`adjuntar_imagen_pago.go:131`), porque lo usan `AdjuntarImagenPago` y `EliminarImagenPago`.

**D3. La réplica se resuelve antes de escribir cualquier blob.** Hoy el reintento escribe primero (`storeAllBlobs`, `:123`) y se entera después, dentro de la transacción (`persistPagoTx`, `:189-197`). En ese momento ya sobrescribió el archivo original. El arreglo agrega, **justo antes** de `storeAllBlobs` y **después** de `validateCargo` (`:93`), una lectura `s.pagosRecibidos.FindByID(ctx, in.ID)`:
- si encuentra el pago → devuelve el existente **sin escribir, sin sobrescribir y sin borrar ningún blob**;
- `domain.ErrPagoNoEncontrado` (`domain/errors.go:60-61`) → se sigue el camino normal;
- **cualquier otro error se propaga**. Nunca se lee como "no existe": seguir con una respuesta indeterminada escribiría sobre el original.

Es el patrón de visitas (`registrar_visita_con_imagenes.go:106-117` y `planImagenes :139-148`). La rama `errIdempotentReplay` dentro de la transacción **se queda**: cubre la carrera en la que dos envíos simultáneos fallan los dos la lectura previa, y ahí D2 es lo que protege.
- **No** muevas la lectura antes de `validateFechaHoraPago` ni de `validateCargo`. Que el reintento del pago que saldó la venta reciba 422 es otro defecto, con su propia evidencia, y no entra en esta tarea.
- Las imágenes de un reintento cuyo pago ya existe se descartan sin escribirse. Hoy pasa lo mismo: se escriben y se borran. Si el primer envío llegó sin foto y el reintento la trae, la foto **no** se agrega. Eso no cambia en esta tarea y se declara en el reporte como no resuelto.

**D4. La ruta sigue siendo estable; no se adopta la ruta única por intento de garantías.** `newStorageKeyPago` no se toca. Las razones:
- con D3, un reintento en secuencia ya no escribe nada;
- lo único que queda es la carrera de dos envíos simultáneos, en la que el segundo `Store` sobrescribe el archivo con los bytes del segundo envío. Eso hoy es INFERIDO, y la §8 no deja cambiar nada sobre una inferencia;
- cambiar la ruta toca la capa HTTP y el reenvío de intentos fallidos.

Lo que sí haces es **medir** esa carrera con una prueba (T4) y dejarla registrada. Si la medición demuestra que el archivo de la fila termina con bytes distintos a los del primer envío, eso va en "Qué falta" de tu entrada, como propuesta para el dueño.

**D5. Un reintento cuyo primer intento sí guardó el archivo no lo pierde, y el archivo no cambia.** La prueba invertida de la tarea 1 no se conforma con que el archivo "exista". Compara los **bytes** después del reintento con los del primer envío, y cuenta que `Store` se llamó **una sola vez** en total.

**D6. El proveedor real se prueba donde sí se puede importar.** En `cmd/api`, que es la raíz de composición, una prueba arma el `Service` de cobranza con `provideCobranzaStorage` (`cmd/api/cobranza_wiring.go:103-109`) sobre `ventas/infra/storage.NewFilesystemProvider(t.TempDir())`, y repite T1 y T2. Así pasa a MEDIDO lo que en la tarea 1 quedó INFERIDO: que `localTestStorage` se comporta como el proveedor real.

### Pruebas de cobranza

En `internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go`, salvo T7. Cada nombre cita su identificador.

| # | Prueba | Qué afirma |
|---|---|---|
| T1 | `TestCrearPagoConImagenes_E_COB_061_Reintento_ConservaElComprobante` (la de la tarea 1, **invertida**, y se renombra) | Después del reintento, el archivo existe, sus bytes son los del primer envío, `Store` se llamó una vez y la respuesta es el pago existente |
| T2 | `..._E_COB_061_Carrera_FilaNombraLaLlave_NoSeBorra` | La lectura previa no encuentra el pago, `Insert` responde `ErrPagoYaExiste` y la fila de la imagen nombra esa llave → el archivo sigue |
| T3 | `..._E_COB_061_LecturaPreviaFalla_NoEscribeNada` | `FindByID` devuelve un error distinto de "no encontrado" → se propaga el error y no se escribe ningún blob |
| T4 | `..._E_COB_061_Carrera_OtrosBytes_Mide` | La carrera con bytes distintos en el segundo envío: afirma **lo que pasa hoy** con los bytes del archivo y lo documenta (D4) |
| T5 | `..._E_COB_061_ListarFilasFalla_NoBorraNada` | `ListImagenes` falla en la limpieza → no se borra ningún archivo |
| T6 | `..._E_COB_061_MicrosipRechaza_BorraLoEscrito` | Un error real en la transacción, sin fila → el archivo que escribió **este** intento sí se borra |
| T7 | En `cmd/api/cobranza_storage_reintento_test.go`: T1 y T2 con el proveedor real (D6) | Igual que T1 y T2 |

La prueba del camino normal de la tarea 1 se queda.

### Mutantes de cobranza

Los corres tú. Cada uno va en el reporte como **parche `diff`**, con la salida **roja** de la prueba que lo mata y la **verde** al revertirlo. Una prueba que sigue verde con el mutante no prueba la regla.

| # | Mutante | Lo mata |
|---|---|---|
| M1 | Quitar la lectura previa de D3 | T1 (`Store` dos veces) |
| M2 | `cleanupBlobsSinFila` vuelve a borrar todas las llaves | T2 y T7 |
| M3 | Un error de `FindByID` distinto de "no encontrado" se trata como "no existe" | T3 |
| M4 | Un error de `ListImagenes` se ignora y se borra todo | T5 |
| M5 | `cleanupBlobsSinFila` nunca borra | T6 |
| M6 | **El revert completo del arreglo** (`git revert` de tu commit del arreglo, o `git stash` del archivo) | T1, T2 y T7 en rojo |

M6 es el control positivo de la §8: "la prueba que lo guarda se pone roja al revertir el arreglo".

### Ventas: medir, sin arreglar

La revisión dejó esto como INFERIDO. Puede que el reintento muera antes de llegar a `storeAllImagenBlobs` (`crear_venta_con_imagenes.go:84`) por las validaciones (`:61, 68, 80`) o porque el middleware de idempotencia lo contesta desde caché (`cmd/api/server.go:221` y `venthttp/handlers_ventas.go:54-57`). Si llega, `cleanupBlobs` (`:101`) borraría el original, porque la ruta es estable (`venthttp/multipart.go:24-30`). **No cambies ningún archivo de producción de ventas.** Las pruebas afirman lo que pasa hoy.

| # | Prueba | Dónde |
|---|---|---|
| V1 | Servicio con fakes: un segundo `CrearVentaConImagenes` con el mismo `ID` de venta, el mismo `ImagenID` y la misma ruta, con stock suficiente y con `Save` devolviendo **el mismo error que devuelve el repositorio real ante un duplicado** (V2). ¿Llega a `Store`? ¿El archivo sigue? | `internal/ventas/app/crear_venta_con_imagenes_reintento_test.go` |
| V2 | Contra Firebird, **dentro de `fbtestutil.WithTestTransaction`**: `VentaRepo.Save` dos veces con la misma venta. Qué error exacto sale la segunda vez. V1 usa ese error | `internal/ventas/infra/ventfb/venta_repo_duplicado_test.go` |
| V3 | HTTP con el middleware de idempotencia real y un `Store` de prueba en memoria. (a) Mismo `Idempotency-Key` después de una respuesta completa → ¿se llama al handler? ¿El archivo sigue? (b) Sin `Idempotency-Key`, o con otra → ¿llega al servicio? | `internal/ventas/infra/venthttp/reintento_imagenes_test.go` |
| V4 | Si se puede: contra Firebird, dentro de `WithTestTransaction`, ¿la validación de stock rechaza el reintento después de guardar la primera venta? Si no se puede medir, queda **INFERIDO** y se dice qué haría falta | El mismo archivo de V2, o se declara |

Cada una lleva su control positivo: la condición que la haría cambiar de resultado, probada.

### Visitas: confirmar con una prueba

Visitas está LEÍDO como sano (`registrar_visita_con_imagenes.go:106-117`). Confírmalo en `internal/visitas/app/registrar_visita_con_imagenes_reintento_test.go`:
- **VI1**, el reintento puro: con la misma visita y la misma imagen, `Store` se llama una vez y el archivo sigue con sus bytes;
- **VI2**, la carrera de `:79-83`: `Insert` devuelve `ErrVisitaYaExiste` → no se borra nada.

Control positivo: con `planImagenes` forzado a devolver "no existe", VI1 se pone roja. Pega el parche y la salida.

### Consumidores: se enumeran, no se adivinan

En el reporte, una tabla con **cada** consumidor de lo que tocaste (`CrearPagoConImagenes`, `cleanupBlobs`, `MSP_PAGOS_IMAGENES.STORAGE_KEY`). Lleva el comando con que lo encontraste, `archivo:línea` y si cambia su comportamiento. El punto de partida, que confirmas tú:

```sh
git grep -n "CrearPagoConImagenes(\|\.CrearPago(\|cleanupBlobs(" -- internal cmd
git grep -n "STORAGE_KEY\|Storage().Key()\|\"pagos/\"" -- internal cmd scripts
```

Entre lo que tienes que encontrar:
- el handler `CrearPago` (`cobranzahttp/handlers_pago_recibido.go:112`);
- `CrearPago` sin imágenes (`app/crear_pago.go:71`): ahora también hace la lectura previa;
- el reenvío de intentos fallidos `/replay-with-multipart`, que entra por el mismo handler con el mismo `id_<n>` (`handlers_pago_recibido.go:64-70` y `platform/failedintent/http/replay_with_multipart_handler.go:57-112`);
- la descarga y el borrado (D1);
- los conteos de fotos, que sólo cuentan filas: `clientesfb/queries.go:834` y `desempenofb/actividades_repo.go:101,151`;
- `AdjuntarImagenPago`, que no se afecta porque usa un `uuid.New()` nuevo en cada llamada (`handlers_pago_imagenes.go:51-53`).

**La app Android** (repositorio `msp-app-kt`): manda un `id_<n>` estable por imagen (`data/pagos/ComprobantesDePago.kt:77-79`) y **no** lee la ruta. Esa ausencia **no es un hallazgo** hasta que la demuestres con un control positivo: el mismo `grep` sobre una cadena que sabes que sí existe en la app tiene que encontrarla. Pega los dos.

### Evidencia nueva

Usa la forma de §4.3, completa, con "Caduca si".
- **`E-COB-061`** en `docs/evidencia/cobranza.md`: "Un reintento de un pago con imagen ya no borra ni sobrescribe el comprobante original". MEDIDO, con T1–T7, M1–M6 y el SHA. **Contradice:** `E-COB-060`. No borres la 060: queda como lo que medía `main` antes del arreglo, con una línea que remita a la 061. Si T4 mide algo, esa medición va aquí o en `E-COB-062`.
- **`E-VEN-014`** en adelante, en `docs/evidencia/ventas.md`: el resultado de V1–V4, cada afirmación con su nivel real. Actualiza la cabecera "Siguiente identificador libre" de los dos archivos.
- El resultado de visitas va en el reporte, citado desde `E-COB-061` como comparación.

El identificador va en el **comentario del código** junto a `cleanupBlobsSinFila` y a la lectura previa, en el **nombre de cada prueba** y en el **mensaje del commit**.

### Las dos compuertas de la §8 las corre el dueño, no tú

Esto toca dinero. Antes de fusionar, el dueño hace una verificación **independiente** de la evidencia, sin conocer tu conclusión, y otra de alcance. Tú no las corres, pero entregas todo lo que necesitan para correrlas sin preguntarte nada:
1. **La pregunta**, sin la respuesta: "¿Un reintento de `POST /cobranza/pagos` con el mismo pago y el mismo `id_0` borra o sobrescribe el comprobante que ya quedó guardado?".
2. **Cada afirmación en la que se apoya el arreglo**, con su nivel y su `archivo:línea` o su salida.
3. **El revert que se pone rojo** (M6), con el parche y las dos salidas.
4. **La lista de consumidores** de arriba, enumerada.
5. **Lo que no verificaste**, dicho en voz alta. "No lo medí" es una respuesta válida; inventarlo no.

Si cualquiera de las dos compuertas devuelve algo distinto de SOSTIENE, el arreglo no se fusiona.

### Lo que NO se hace en la Parte 2

- Nada contra producción. Tú no tienes ni necesitas acceso.
- Toda prueba que escriba en Firebird va dentro de `fbtestutil.WithTestTransaction`. Si una necesita confirmar, pregunta antes.
- No cambies `newStorageKeyPago` (D4), ni nada de ventas o visitas fuera de las pruebas, ni la validación del saldo (D3).

### Archivos de la Parte 2

```
internal/cobranza/app/crear_pago_con_imagenes.go
internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go
cmd/api/cobranza_storage_reintento_test.go                       (nuevo)
internal/ventas/app/crear_venta_con_imagenes_reintento_test.go     (nuevo)
internal/ventas/infra/ventfb/venta_repo_duplicado_test.go          (nuevo)
internal/ventas/infra/venthttp/reintento_imagenes_test.go          (nuevo)
internal/visitas/app/registrar_visita_con_imagenes_reintento_test.go (nuevo)
docs/evidencia/cobranza.md                                         (sólo agregar y la cabecera)
docs/evidencia/ventas.md                                           (sólo agregar y la cabecera)
docs/superpowers/plans/fotos-reenvio-task-2-report.md              (nuevo)
```

Nada más. Si crees que necesitas otro archivo, pregunta en el PR **antes** de tocarlo.

---

## Errores que no se repiten

Cada uno ya pasó en la tarea 1. Si la entrega repite uno, rebota.

1. **Entregar tarde y en silencio.** La tarea 1 se pidió para el viernes 30 a las 12 y llegó el jueves 5, seis días tarde y sin una sola pregunta en el PR. Si te atoras más de una hora, pregunta en el PR ese mismo día. Si ves que no llegas al miércoles, avísalo el día que lo veas, no el día de la entrega.
2. **Saltarse hooks.** Nada de `LEFTHOOK_EXCLUDE`, `--no-verify` ni equivalentes. Si un hook falla, pega la salida en el PR y pregunta.
3. **Pegar salidas de otra versión del código.** Cada salida del reporte sale del SHA que declaras, con el árbol limpio y `go clean -testcache`. Si cambias una línea después de correr, vuelves a correr todo.
4. **Afirmar sin comprobar.** "Main tiene errores de lint" se podía comprobar en un minuto y era falso. Cada afirmación del reporte lleva su nivel y su salida o su `archivo:línea`.

---

## Verificación antes de entregar

En cada rama, sobre el SHA que entregas:

```sh
gofmt -l ./internal/cobranza ./internal/ventas ./internal/visitas ./cmd/api ./scripts/medicion
go vet ./internal/cobranza/... ./internal/ventas/... ./internal/visitas/... ./cmd/api/... ./scripts/medicion/...
go build ./...
golangci-lint run --timeout 15m ./...
go clean -testcache
go test -race -short ./internal/cobranza/... ./internal/ventas/... ./internal/visitas/... ./cmd/api/... ./scripts/medicion/...
make test-firebird-all
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./scripts/medicion/fotos-pagos-faltantes/
```

Todo tiene que salir limpio. Pega la salida **literal**, no un resumen. Los hooks de `lefthook` corren completos en cada commit y cada push.

---

## Reportes

**Parte 1:** actualiza `docs/superpowers/plans/fotos-reenvio-task-1-report.md` (B2).

**Parte 2:** `docs/superpowers/plans/fotos-reenvio-task-2-report.md`, con:
1. El SHA que entregas y la salida literal de la verificación sobre ese SHA.
2. El paquete para las compuertas (las cinco piezas de arriba).
3. T1–T7 en verde y la tabla M1–M6, cada mutante con su parche y sus salidas roja y verde.
4. V1–V4 y VI1–VI2, cada resultado con su nivel y su control positivo.
5. La tabla de consumidores, con los comandos.
6. Lo que hiciste distinto al brief, con el porqué.
7. Lo que **no** verificaste. Como mínimo: Windows (sólo se compila), producción, y lo que T4 deje abierto.

PR de la Parte 2 contra `main`, con el título `fix(cobranza): un reintento ya no borra el comprobante del pago (E-COB-061)` y la descripción apuntando al reporte. Commits en formato `tipo(alcance): asunto`, citando el identificador.

---

## Calendario

| Cuándo | Qué |
|---|---|
| **Miércoles 11 de noviembre, 1pm** | **Entrega única:** el #35 corregido (B1–B4 y menores) y el PR de `fix/fotos-reenvio-no-borra` con D1–D6, T1–T7, M1–M6, V1–V4, VI1–VI2, evidencia y los dos reportes |

Si en algún momento ves que no llegas, avísalo en el PR ese mismo día. Si necesitas algo, pregúntalo en el PR.
