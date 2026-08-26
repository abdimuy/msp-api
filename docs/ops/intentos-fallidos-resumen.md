# Intentos fallidos — el resumen en la fila

> Acompaña a la migración `000061` y al binario que trae los extractores de
> resumen. **El orden de los cuatro pasos no es negociable** — ver más abajo.

## Qué se arregló

La pantalla de intentos fallidos mostraba **"Sin nombre capturado"** en las 19
filas de ventas, y al abrir el detalle salía `{}` o un aviso de multipart
ilegible.

**No era un defecto de la UI: el dato nunca llegaba.** El escritorio deducía el
nombre y el monto del cuerpo capturado, y una venta lleva fotos —o sea
multipart—, y en esa ruta `BODY` se guarda vacío **a propósito**: el cuerpo
vive en disco (`BODY_BLOB_PATH`) y la fila sólo tiene `null`. Deducir del
cuerpo devolvía nulo siempre.

Ahora el servidor extrae **una vez**, al capturar, y el resumen viaja en la
fila y en el listado.

| Antes | Ahora |
|---|---|
| El escritorio deducía quién y cuánto del cuerpo de la fila | El servidor los extrae al capturar y los manda en el listado |
| Una venta multipart no tenía cuerpo en la fila → nombre nulo siempre | El extractor lee el campo `datos` del blob, una sola vez |
| El módulo se deducía de la ruta en el escritorio | Es columna real e indexada; los chips filtran por SQL |
| Las filas anteriores al cambio se quedaban sin nombre | El janitor las rellena en su siguiente pasada |

## La restricción de diseño que gobierna todo

**El listado se sirve de una consulta, sin abrir un solo blob.**

Existe `GET /_admin/failed-intents/{id}/blob-parts`, y sería tentador usarlo
para sacar el nombre de cada tarjeta. Eso convierte la ruta más caliente de la
pantalla en un N+1 sobre el disco del servidor. El blob se abre **una vez**:
al capturar, o después en el janitor para las filas viejas.

Hay dos pruebas que lo vigilan y **no se deben relajar**:

- `ResumenDelServidor.integration.test.tsx` → "no dispara una sola petición de
  blob mientras nadie abre un renglón": renderiza 20 renglones con blob y
  exige **cero** llamadas.
- `TestJanitor_RellenaLaFilaViejaLeyendoElBlobUnaVez` → mide que el blob de una
  fila se abre exactamente una vez **en toda su vida**, no una vez por ciclo.

## Orden de despliegue

**El orden importa.** Si el escritorio va primero, muestra huecos donde espera
resumen. Es el mismo error que ya se cometió con el APK y la migración
`000056`.

1. **Migración `000061`, sola y sin binario.**
   `make fb-migrate-up` (o el `.sql` a mano en producción). Agrega `MODULO`
   VARCHAR(40) ASCII, `RESUMEN` BLOB TEXT UTF8 y el índice
   `IDX_MSP_FAILED_INTENTS_MODULO`.
   No rompe nada: las dos columnas son **nullable** y ningún binario las lee
   todavía. `ALTER TABLE ADD` de una columna nullable en Firebird es un cambio
   de metadatos, no reescribe filas — no hace falta ventana.

2. **Binario con los extractores + el janitor.**
   Desde aquí, **los intentos nuevos traen resumen**. Los viejos no.

3. **El janitor rellena las filas viejas en su siguiente pasada** (una hora
   después del arranque, o en el arranque mismo: el primer `tick` corre al
   iniciar). Abre el blob de cada fila una vez, extrae y guarda.
   Se puede comprobar con:

   ```sql
   SELECT COUNT(*) FROM MSP_FAILED_INTENTS
   WHERE MODULO IS NULL AND RESUMEN IS NULL;
   ```

   Ese número tiene que bajar. Lo que **no** baja a cero son las rutas sin
   extractor registrado (`/v2/visitas`, hoy): no tienen módulo que afirmar y
   se quedan así. Es correcto.

4. **Escritorio con la pantalla nueva.**

### Reversión

Al revés y con cuidado: **primero el binario, después la migración**. El
binario escribe las dos columnas en cada captura y las lee en cada listado;
correr el `down` con ese binario vivo hace que toda captura de un 4xx/5xx falle
con "column unknown" — y la captura es justo lo que evita perder la venta.

## Añadir un módulo nuevo

Dos cosas, y ninguna toca ni el esquema ni el escritorio:

1. Una implementación de `failedintent.ResumenExtractor` en
   `internal/{modulo}/infra/failedintents/resumen.go`.
2. Una línea en `provideFailedIntentResumenExtractor`
   (`cmd/api/failedintent_wiring.go`):

   ```go
   Registrar("/v2/garantias", "garantias", garantiasfailedintents.NewResumenExtractor())
   ```

El nombre del módulo que se registra ahí **es** el que sale en el chip del
escritorio: es un valor de presentación que viaja por el DTO. El escritorio ya
acepta cualquier cadena y capitaliza la que no conozca.

`RESUMEN` es un JSON `{titulo, monto, referencia}` precisamente para esto: un
módulo nuevo llena los mismos tres campos y no pide migración.

## Lo que deliberadamente NO se hizo

El detalle del 422 de existencias —*"falta 1 base Leos Venecia; hay 16 en
Almacén General"*— y el botón **"Ya hice el traspaso"** con su reasignación.
Exige persistir el detalle del error en la captura y otro despliegue del API.
El `RESUMEN` en JSON deja sitio para añadirlo después sin rehacer nada.

## El corte por etapa (2026-08-26)

El resumen arregló las filas que **sí** podían tener nombre. Quedaban otras que
no pueden tenerlo nunca, y estaban en la misma lista.

Una venta puede estar en tres lugares: **sólo aquí** (llegó al API y no entró a
la base), **en `MSP_VENTAS`** (guardada, sin aplicar) o **en Microsip**
(aplicada). Esta pantalla existe para el primero.

| Etapa | Regla | Ejemplos |
|---|---|---|
| **1 — creación** | la ruta **exacta** del recurso, **sin id** | `POST /v2/ventas`, `POST /v2/cobranza/pagos`, `POST /v2/visitas` |
| 2 — edición | cualquier ruta **con id** | `PATCH /ventas/{id}`, `PUT /{id}/combos`, `POST /{id}/revisar` |
| 3 — aplicación | `/{id}/aplicar` | `POST /ventas/{id}/aplicar` |

**Si la ruta trae un id, la fila ya existe.** No es una heurística sobre
mensajes de error: es la forma de la ruta.

El caso de `/aplicar` es el extremo: `AplicarVentaInput`
(`internal/ventas/infra/venthttp/dto.go`) **no tiene cuerpo** —la operación se
identifica sólo por el id de la ruta—, así que no hay nombre que extraer porque
nunca se manda ninguno. Las de etapa 2 sí traen cuerpo, pero es el cuerpo de un
cambio parcial sobre una venta que ya está guardada: aunque se les sacara un
renglón legible, seguirían sin ser lo que esta pantalla existe para mostrar.

Desde este cambio **el listado devuelve por defecto sólo la etapa 1**. El corte
es `PATH IN (…)` en SQL, con igualdad exacta —no `STARTING WITH`, que metería
las tres etapas en el mismo saco— y `?etapa=todas` devuelve el comportamiento
anterior.

### Lo que se midió en producción antes de cambiarlo

`MUEBLERA_SNP.FDB`, 2026-08-26, 371 filas. **El paso que faltó la vez
anterior:** todo el trabajo del resumen asumió que las filas capturadas eran
creaciones, y los conteos usaron `PATH STARTING WITH '/v2/ventas'`. Dev tampoco
lo habría revelado — sus filas son todas `POST /v2/ventas`.

| Forma de la ruta | Método | Estado | Filas |
|---|---|---|---|
| `/v2/ventas` | POST | resolved_manual | 133 |
| `/v2/cobranza/pagos` | POST | new | 69 |
| `/v2/cobranza/pagos` | POST | resolved_manual | 49 |
| `/v2/visitas` | POST | new | 25 |
| `/v2/visitas` | POST | resolved_manual | 24 |
| `/v2/ventas/{id}/aplicar` | POST | new | 26 |
| `/v2/ventas/{id}` | PATCH | new | 16 |
| `/v2/ventas/{id}/revisar` | POST | new | 16 |
| `/v2/ventas/{id}/combos` | PUT | new | 12 |
| `/v2/ventas/{id}/cliente` | PATCH | new | 1 |

Etapa 1: **300 filas** (ventas 133, pagos 118, visitas 49). Etapa 2 y 3: **71
filas, todas de ventas** — cobranza y visitas no tienen ni una con id, porque
sus capturadores sólo miran `POST` sobre su prefijo.

**Consecuencia que hay que esperar al desplegar:** las 133 filas de etapa 1 de
ventas están **todas** en `resolved_manual`, y las 71 pendientes son **todas**
de etapa 2 o 3. O sea que el chip "Ventas" filtrado por pendientes pasa de 71
renglones a **cero**. No es que se haya perdido nada: es que ninguno de esos 71
era lo que la pantalla existe para mostrar. Los 69 pagos pendientes de etapa 1
siguen viéndose.

La consulta para repetirla después del despliegue, a mano con `isql` (el
acceso está en `docs/deploy-test-server-runbook.md` §2):

```sql
SELECT COALESCE(MODULO, '(sin modulo)'), COUNT(*)
FROM MSP_FAILED_INTENTS
WHERE PATH IN ('/v2/ventas', '/v2/cobranza/pagos', '/v2/visitas')
GROUP BY 1;
```

### Se sigue capturando todo

Sólo cambia lo que se **lista**. La captura de un `/aplicar` fallido es el
único rastro de que un pago o una venta no llegó a Microsip, y ese rastro ya ha
hecho falta antes. Borrarlo para limpiar una pantalla cambiaría evidencia por
orden — justo al revés de para qué existe este módulo.

### Lo que este cambio NO resuelve

Las 71 filas de etapa 2 y 3 quedan invisibles pero **siguen ahí**, en `new`,
hasta que la retención las purgue. Que una venta no pueda aplicarse a Microsip
—"registro duplicado", "falta la caja de contado configurada"— **es un problema
real de otra naturaleza**, y hoy nadie lo mira. Esconderlas de esta pantalla no
es decidir sobre ellas. Falta decidir dónde se ven: otro chip, otra pantalla, o
una alerta.

## Invariantes que no se deben romper

- **`MODULO` y `RESUMEN` son dos hechos distintos.** El módulo se sabe por la
  **ruta**; el resumen, por el **cuerpo**. Un cuerpo que el extractor no
  reconoce deja `MODULO` puesto y `RESUMEN` NULL. Si el módulo también se
  perdiera, la fila se caería del chip "Ventas" —que filtra por SQL— y el
  operador vería una lista incompleta sin que nada se lo advierta.

- **Un resumen vacío se guarda como NULL, no como `{}`.** Guardar el envoltorio
  vacío haría que la fila se viera "ya rellenada" ante el filtro del janitor,
  que dejaría de intentarlo.

- **El filtro del relleno mira las DOS columnas** (`MODULO IS NULL AND RESUMEN
  IS NULL`). Si mirara sólo `RESUMEN`, las filas cuyo cuerpo no se reconoce se
  reabrirían cada hora, para siempre, releyendo su blob.

- **La fusión de la dedup nunca sustituye un resumen bueno por uno peor.** Un
  reintento cuyo blob no se pudo guardar llega sin resumen; escribirlo encima
  borraría el nombre que el intento anterior sí alcanzó a extraer, y el renglón
  se apagaría solo.

- **El extractor es una función pura del cuerpo y no puede tumbar la captura.**
  El registro atrapa su pánico. Perder la evidencia porque un extractor se
  rompió sería exactamente el fallo que este módulo existe para evitar.

- **`internal/platform/failedintent` no importa un módulo de negocio.** Lo
  vigilan la regla `failedintent-no-modules` de `.golangci.yml` y
  `TestPlataformaNoImportaModulos`. Son dos porque una de ellas se puede
  desactivar editando un YAML.

- **La lista de rutas raíz se deriva de los capturadores, no se escribe
  aparte.** La ruta raíz de un módulo *es* su prefijo de captura, y los tres se
  declaran una sola vez en `cmd/api/failedintent_wiring.go`. Si se escribieran
  dos veces, registrar un capturador nuevo capturaría filas que el listado
  nunca mostraría, y nada lo advertiría. Lo vigila
  `TestFailedIntentRutasRaiz_SonLosPrefijosDeLosCapturadores`.

- **El corte por etapa va en SQL, no sobre la página ya recibida.** Recortar en
  memoria mostraría "las creaciones que cupieron en los primeros veinte
  renglones". Lo vigila `TestList_ElCorteDeEtapaVaEnSQL`, que pide una página de
  UNO sobre un conjunto donde las creaciones son las más viejas.

- **`internal/platform/failedintent` no sabe qué es una creación.** La lista de
  rutas raíz entra por parámetro desde la raíz de composición. Vacía significa
  "sin acotar", que es lo correcto para quien no la tiene: el janitor y las
  pruebas.
