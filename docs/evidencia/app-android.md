# Evidencia — cliente Android (`msp-app-kt`)

Registro de hechos y su prueba para la app del cobrador. **Las afirmaciones del
cliente Android viven aquí y no en el otro repo**, para no partir el registro:
casi todas cruzan la frontera (el sync, el `?desde=`, los ids) y separarlas
obliga a leer dos libros mayores para entender un solo defecto.
`msp-app-kt/docs/evidencia.md` es un puntero a este archivo.

El diseño y las reglas viven en
[`../registro-de-evidencia.md`](../registro-de-evidencia.md).

## Los cuatro niveles

| Nivel | Qué significa | Qué tiene que enseñar |
|---|---|---|
| **MEDIDO** | Se ejecutó y hay salida | La salida, el comando o la consulta, la fecha, **dónde** (dev / producción / prueba) y un **control positivo** |
| **LEÍDO** | Está escrito en el código | `archivo:línea`. No se ejecutó |
| **INFERIDO** | Se deduce de lo leído | De qué se deduce, y **qué lo volvería MEDIDO** |
| **SUPUESTO** | Nadie lo revisó | Nada. Es una creencia, y se marca como tal |

**Regla dura:** ningún cambio que toque dinero puede apoyarse en una afirmación
por debajo de **MEDIDO**. Si está en INFERIDO, primero se mide; si no se puede
medir, no se cambia. Una ausencia no es un hallazgo hasta enseñar el control
positivo que demuestre que la búsqueda habría encontrado el caso.

## Cómo se cita desde el código

El identificador (`E-APP-007`) es el puente. Se cita desde el comentario que
explica la trampa, desde el nombre de la prueba que la guarda y desde el mensaje
del commit que la cierra:

```kotlin
@Test fun `no borra por ausencia si la lista del servidor vino vacia`() // E-APP-002
```

```
fix(cobranza): acotar la enumeración local a la ventana del cobrador

Cierra E-APP-001.
```

Los números son correlativos y **nunca se reutilizan ni se renumeran**. Lo
refutado **no se borra**: cambia de estado a REFUTADA y se queda.

> **Nota sobre los `archivo:línea`.** Las citas de abajo se reverificaron
> abriendo cada archivo el 2026-09-23 sobre la rama `feat/notas-visibles` del
> checkout principal. Varias corrigen por unas líneas lo que decía
> `msp-app-kt/docs/inicializar-semana.md`; donde eso pasa, se anota.

**Siguiente identificador libre: `E-APP-057`.**

> **Tercera tanda (2026-09-24).** `E-APP-029 … E-APP-047`. Es la primera tanda
> con **mediciones en un aparato**: el teléfono de `javier.gil.perez@msp.com`
> (samsung SM-A155M, Android 16, app **2.18.1**), leído el **2026-09-24
> 16:41:56 UTC** por el depurador remoto, **sólo `SELECT`**, con canario contra
> el truncamiento en cada consulta que podía cortarse (ver **E-INF-008** en
> `infra.md`: el depurador **no anuncia** que cortó). La salida cruda **no está
> en el árbol**; las cifras vienen del informe de esa lectura, igual que
> E-APP-020 y E-APP-025. Lo LEÍDO de esta tanda se verificó abriendo cada
> archivo el 2026-09-24 sobre la rama `feat/notas-visibles` del checkout
> principal de `msp-app-kt` (`d88dfb8c`), y lo MEDIDO por `git` se corrió aquí.
>
> La misma tanda **corrige cuatro fichas sembradas el 2026-09-23**
> —E-APP-010, E-APP-020, E-APP-022 y E-APP-024— y **enlaza** E-APP-022 con
> E-COB-014, que se contradecían sin que ninguna lo dijera. Ninguna se borró.

> **Segunda tanda (2026-09-23, tarde).** `E-APP-010 … E-APP-019` salieron de la
> primera corrida de la compuerta previa de `CLAUDE.md` §8, que **detuvo** un
> cambio en el reconciliador; el registro de esa corrida está al final del
> archivo. `E-APP-020 … E-APP-028` salieron del **defecto de campo del mismo
> día** —el cobrador que no podía condonar y veía −$3,000—, con dos hipótesis
> **REFUTADAS** (E-APP-026, E-APP-027) y una corrección de fechas de despliegue
> (E-APP-028). Las citas se verificaron abriendo cada archivo en la rama
> `feat/notas-visibles` del checkout principal de `msp-app-kt`. **Nada se midió
> en un aparato**: lo MEDIDO de esta tanda es `grep`, `git` y el censo de
> producción de E-APP-025.

---

### E-APP-001 · El reconciliador borra por ausencia y enumera lo local por zona sin ventana

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:**
  - `core/database/.../dao/payment/PaymentDao.kt:718-721` —
    `SELECT ID FROM Payment WHERE ZONA_CLIENTE_ID = :zonaId ORDER BY CAST(ID AS INTEGER) ASC`.
    **Sin filtro de fecha: enumera todo el histórico de la zona.**
  - `app/.../core/sync/cobranza/CobranzaReconciler.kt:300-302` — el mismo
    `getActiveIDsByZona` alimenta `localPagoIds`.
  - `CobranzaReconciler.kt:304` — `val pagoPhantoms = localPagoIds - serverPagoIds`
  - `CobranzaReconciler.kt:308-310` — `paymentDao.deleteByIDs(...)` sobre esos
    fantasmas.
- **Por qué es dinero:** el conjunto del servidor **sí** aplica la ventana
  (`?desde=`) y además filtra por la **venta**, no por el pago. Una venta saldada
  sale del conjunto del servidor **con todas sus filas de pago**, y la resta las
  clasifica como fantasmas.
- **Corrección mínima conocida:** acotar `getActiveIDsByZona` a la misma ventana
  que se manda como `?desde=` (ver `msp-app-kt/docs/inicializar-semana.md`).
- **Contradice:** el cuidado explícito de
  `core/database/.../dao/sale/SaleDao.kt:392-394`, que dice que
  `deleteSaldadasFueraDeVentana` **no toca `Payment`** porque *"el histórico de
  pagos se conserva por normativa SAT y para los reportes"*. El reconciliador lo
  deshace por otra puerta.
- **SUPUESTO, sin medir:** cuántas filas se pierden por cobrador por semana.
  Requiere leer un Room de campo.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** cambia el filtro del servidor o se acota la enumeración local

### E-APP-002 · Con `has_more=true` y página vacía el cliente corta el ciclo y **sigue adelante a borrar**

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `app/.../core/sync/cobranza/CobranzaReconciler.kt:436-448`
  (cuerpo de `fetchAllServerIds`). La línea **445** es
  `if (!page.has_more || page.ids.isEmpty()) break`, y la función **retorna el
  `result` parcial** en la línea 448.
- **Contradice — el comentario que miente**, líneas 441-444, literal:

  > `// Defensa: si el server dice has_more=true pero manda una página`
  > `// vacía (bug del server), `last()` lanzaría NoSuchElementException`
  > `// y el outer try/catch lo convertiría en Error silencioso. Cortar`
  > `// el loop es safer — el siguiente reconcile reintentará desde 0.`

  El comentario vende el `break` como defensa. Lo que hace es entregar un
  conjunto del servidor **truncado** al llamador de la línea 294, que en la
  línea 304 resta y en la 310 **borra**. No aborta la pasada, no marca el
  resultado como parcial y no espera al siguiente reconcile: borra ahora, con
  datos incompletos.
- **Por qué es dinero:** cuanto más temprano venga la página vacía, más pagos
  quedan fuera de `serverPagoIds` y más filas se clasifican como fantasmas.
- **Qué lo volvería MEDIDO:** un test con un `MockWebServer` que conteste
  `has_more=true` con `ids=[]` y compruebe que no se borra nada.
- **Guardada por:** — (sin prueba todavía). **Es la prueba que más falta.**
- **Precisión del 2026-09-23 (tarde), sin cambiar el estado:** el **disparador**
  —una página vacía con `has_more=true`— **no es alcanzable por HTTP hoy**. Ver
  **E-COB-013** en `cobranza.md`: el `limit` tiene `minimum:1` y el servidor mapea
  `<= 0` al default de 5000. La trampa del cliente es real y sigue VIGENTE, pero
  es **latente**: la espera a que el servidor cambie. El riesgo **alcanzable hoy**
  por esta misma ruta es otro y no necesita ningún bug del servidor: la lista
  **legítimamente vacía** (`ids=[]`, `has_more=false`), que el cliente también
  convierte en "todo lo local es fantasma" — E-APP-014, y es justo lo que
  `CobranzaReconcilerTest.kt:293-314` afirma como correcto.
- **Caduca si:** `fetchAllServerIds` deja de cortar en seco o el llamador
  aprende a distinguir un conjunto parcial

### E-APP-003 · Para pagos no existe poda local por ventana; para ventas sí

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre el checkout principal de `msp-app-kt`
- **Cómo:** `grep -n "DELETE FROM" PaymentDao.kt` devuelve **siete** sentencias:
  `:584` (por `DOCTO_CC_ACR_ID` + `GUARDADO_EN_MICROSIP=1`), `:598` (por id),
  `:606` (por lista de ids), `:674` y `:696` (los dos `deleteLegacyTwins*`),
  `:879` (`DELETE FROM payment`, borrado total) y `:890` (sólo sincronizados).
  **Ninguna acota por fecha.**
- **Control positivo:** el mismo grep sobre `SaleDao.kt` **sí** encuentra la poda
  por ventana: `:398-409`, `deleteSaldadasFueraDeVentana(fechaIso)`, con
  `Payment.FECHA_HORA_PAGO >= :fechaIso` en el `NOT EXISTS`.
- **Consecuencia:** el único mecanismo que reduce el histórico de `Payment` es el
  borrado por ausencia de E-APP-001 — un camino que nadie diseñó como poda.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** se agrega una poda por ventana a `PaymentDao`

### E-APP-004 · Dos cerrojos protegen el dinero sin subir

- **Nivel:** LEÍDO · **Estado:** VIGENTE · **NO TOCAR**
- **Leído:** 2026-09-23
- **Cerrojo 1 — bloqueo por pagos pendientes:**
  `app/.../features/auth/viewModels/AuthViewModel.kt:122-127`. Si
  `paymentStore.getPendingPayments()` no viene vacío, `updateStartOfWeekDate()`
  corta con `ResultState.Error("Hay N pagos pendientes")` y **retorna antes** de
  tocar Firestore. *(`inicializar-semana.md` lo citaba como `:120-125`; el rango
  exacto hoy es `:122-127`.)*
  El cerrojo hermano de visitas está en `:128-133`.
- **Cerrojo 2 — el filtro de ids no numéricos:**
  `CobranzaReconciler.kt:115` (`localIds = paymentDao.getActiveIDsByZona(zona).mapNotNull { it.toIntOrNull() }`)
  y `:300-302` (el mismo filtro en `reconcilePagosViaIds`), documentado en el
  comentario de `:297-299`. Un pago capturado offline tiene id UUID y por eso
  **nunca se cuenta como fantasma**. *(`inicializar-semana.md` los citaba como
  `:119-121` y `:296-298`; los rangos exactos hoy son `:115` y `:300-302`.)*
- **Por qué NO se tocan:** relajar el cerrojo 2 a aceptar UUID **borra dinero**
  capturado y no subido. El cerrojo 1 es lo único que impide abrir la semana
  encima de pagos que todavía viven sólo en el teléfono.
- **Guardada por:** el cerrojo de visitas sí tiene prueba
  (`RetiroDelDialogoDeVisitaTest.kt`); los dos de arriba, no.
- **Caduca si:** cambia el formato de los ids locales

### E-APP-005 · El botón de semana reescribe `FREC_PAGO` a `SEMANAL` por un mapeador de 37 argumentos posicionales

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **La cadena:**
  1. `AuthViewModel.kt:144-152` — lee **todas** las ventas
     (`salesStore.getAll()`), las copia con `ESTADO_COBRANZA = PENDIENTE` y
     llama `salesStore.saveAll(...)`. La copia pasa por
     `.toSale().toEntity()`.
  2. `app/.../data/models/sale/SaleMappers.kt:7-45` — `Sale.toEntity()` son
     **37 argumentos posicionales sin nombre** (líneas 8 a 44).
  3. **Línea 44**, el último: `FREC_PAGO?.name ?: FrecuenciaPago.SEMANAL.name`.
     Cualquier frecuencia nula resuelve a `SEMANAL` **al pasar por el mapeador**,
     no al capturarla.
  *(`inicializar-semana.md` citaba `SaleMappers.kt:7-42`; el rango exacto es
  `:7-45`.)*
- **Por qué importa:** `FREC_PAGO` pesa en el ponderado de la Meta de la semana.
  El botón, que dice abrir la semana, reescribe en silencio la frecuencia de
  cobro de toda la cartera.
- **Corrección conocida:** sustituir `replaceAll` por un `UPDATE` dirigido — la
  forma ya existe, hermana de `SaleDao.kt:316-326`. Con eso el mapeador de 37
  argumentos deja de correr.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** cambia la firma de `SaleEntity` o el botón deja de usar
  `toEntity()`

### E-APP-006 · El botón de semana no toma `CobranzaWriteMutex`

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre el checkout principal de `msp-app-kt`
- **Cómo:** `grep -n "Mutex\|withLock" AuthViewModel.kt` → **cero aciertos**. El
  `getAll()` + `saveAll()` de `AuthViewModel.kt:144-152` escribe la tabla `sales`
  entera sin tomar ningún cerrojo.
- **Control positivo:** `grep -rn "CobranzaWriteMutex" --include='*.kt' .` **sí**
  encuentra a los dos escritores de cobranza que lo toman:
  `CobranzaReconciler.kt:79` y `CobranzaSyncManager.kt:69`, los dos vía
  `CobranzaWriteMutexProvider.get()`.
- **Consecuencia:** un `mergeVentas` que aterrice en medio del `deleteAll()` +
  `insertAll()` se pierde.
- **Nota al margen, LEÍDO:** `mergeVentas` **sí** preserva `ESTADO_COBRANZA`
  (`CobranzaSyncManager.kt:708-713`), pero `CobranzaReconciler.kt:387` **no** —
  hay un segundo camino que resetea estados sin que nadie lo pida. *(Pendiente
  de reverificar línea por línea; se hereda de `inicializar-semana.md`.)*
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** el botón se mueve a un caso de uso que sí tome el mutex

### E-APP-007 · Cero pruebas del botón de semana

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre el checkout principal de `msp-app-kt`
- **Cómo:** `grep -rn "updateStartOfWeekDate\|onInitWeek\|HomeFooterSection" --include='*.kt' .`
  restringido a `src/test/` y `src/androidTest/` → **cero aciertos**.
- **Control positivo:** el mismo grep con `resolveStartWeekDate` **sí** encuentra
  `app/src/test/java/com/example/msp_app/features/home/screens/HomeStartWeekDateFallbackTest.kt`.
- **Consecuencia:** las afirmaciones E-APP-005 y E-APP-006 no tienen nada que las
  fije. Cualquier cambio al botón entra sin red.
- **Guardada por:** — por definición, nada
- **Caduca si:** aparece la primera prueba

### E-APP-008 · Room va en v33, y las dos ramas que se creían sin fusionar ya están en `main`

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre el checkout principal de `msp-app-kt`
- **Cómo:**
  - `core/database/.../AppDatabase.kt:82` → `version = 33`.
  - `git branch --merged main` lista **`feat/campos-venta`** y
    **`feat/editar-venta-local`** entre las ramas ya contenidas en `main`. Para
    la primera hay además commit de integración explícito:
    `25308fd3 feat(cobranza): integra feat/campos-venta`. Para la segunda **no
    se buscó** el commit; la evidencia es sólo el `--merged`.
- **Control positivo:** el mismo `git branch --merged main` **deja fuera**
  `feat/meses-del-historial`, `feat/notas-visibles`, `feature/sale-kits`,
  `feature/nueva-venta-tests` y `features/payment-anomaly-detection`, así que el
  listado sí discrimina.
- **Contradice:** la nota de memoria *"Fila de contactos + editar venta antes de
  subir (20-sep) — ninguna pusheada; ambas suben Room a v30"*. Room ya no está en
  v30 y las ramas ya no están fuera.
- **Por qué importa:** planear una migración de Room "desde v30" hoy produce una
  migración que no aplica.
- **Guardada por:** — (no aplica; es estado del repositorio, no del código)
- **Caduca si:** sube la versión de Room o se fusionan más ramas

### E-APP-009 · El API ya sirve `fecha_inicio_semana` desde `FECHA_CARGA_INICIAL`, y la app Android no lo consume

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre los dos checkouts principales
- **Lado servidor (LEÍDO):**
  - `msp-api/internal/rutas/infra/rutasfirestore/calendario_client.go:54` — lee
    `data["FECHA_CARGA_INICIAL"]` de Firestore.
  - `msp-api/internal/rutas/infra/rutashttp/dto.go:44` y `:59` — lo expone como
    `json:"fecha_inicio_semana"`, documentado como *"Inicio de ventana del
    usuario (FECHA_CARGA_INICIAL, RFC3339 UTC)"*.
- **Lado app (MEDIDO):** `grep -rn "v2/rutas" --include='*.kt' .` sobre
  `msp-app-kt`, excluyendo `/build/`, → **cero aciertos**.
- **Control positivo:** el mismo grep con `v2/cobranza` encuentra los canales que
  la app sí usa: `V2CobranzaApi.kt:22`, `:31`, `:40`, los dos streams SSE de
  `CobranzaSseSubscriber.kt:181` y `:193`, y las pruebas E2E.
- **Consecuencia:** el consumidor de `fecha_inicio_semana` es **el escritorio**.
  Cuando el cobrador presiona el botón de semana, el número semanal que ve la
  oficina se mueve sin que nadie en la oficina lo sepa ni pueda auditarlo.
- **Sin verificar:** si el escritorio efectivamente lo consume — no se revisó ese
  repositorio. **SUPUESTO.**
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** la app empieza a llamar `/v2/rutas/*`

### E-APP-010 · Los ~~dos~~ **TRES** borrados sin trocear del reconciliador, contra el tope de 999 de SQLite

> **CORREGIDA el 2026-09-24 — eran tres, no dos, y el que faltaba es el peor.**
> **Qué se creyó y por qué parecía razonable:** la ficha se escribió recorriendo
> los dos borrados **de fantasmas** (`:310` pagos, `:374` saldos), que son los
> que la compuerta estaba auditando y los únicos que el título del cambio
> propuesto nombraba. Los dos citados **son correctos**. Lo que no se miró fue
> el tercer `deleteByIDs` del mismo archivo, que no borra fantasmas sino
> **gemelos UUID** y por eso no cayó en la búsqueda: está dentro de
> `collapseUuidTwins`, una función que la ficha leyó como *reparadora*, no como
> borradora. El título decía "los dos" y se volvió indistinguible de una cuenta
> verificada. La cuenta verificada es **tres**.

- **Nivel:** LEÍDO · **Estado:** VIGENTE (corregida 2026-09-24)
- **Leído:** 2026-09-23 (tarde) y reverificado el 2026-09-24, rama
  `feat/notas-visibles` del checkout principal de `msp-app-kt` (`d88dfb8c`)
- **Dónde — los tres:**
  - `app/.../core/sync/cobranza/CobranzaReconciler.kt:310` —
    `paymentDao.deleteByIDs(pagoPhantoms.map { it.toString() })`. **Sin
    `.chunked(...)`.**
  - `CobranzaReconciler.kt:374` — `saleDao.deleteByDoctoCcIds(saldoPhantoms.toList())`.
    **Sin `.chunked(...)`.** Las dos consultas son `IN (:ids)` directas:
    `PaymentDao.kt:606` y `SaleDao.kt:441`.
  - **EL QUE FALTABA — `CobranzaReconciler.kt:194`**, dentro de
    `collapseUuidTwins` (`:191-195`): `paymentDao.deleteByIDs(collapsed)`, donde
    `collapsed` es el resultado completo de `paymentDao.findCollapsibleUuidTwins()`
    (`:192`) — **una consulta sin `LIMIT`, sin ventana y sin cota**. Misma
    sentencia `IN (:ids)` de `PaymentDao.kt:606`. **Sin `.chunked(...)`.**
- **Por qué el tercero es el peor — y esto es nuevo (LEÍDO, 2026-09-24):**
  `collapseUuidTwins` tiene **dos sitios de invocación**, y el segundo está
  **dentro de una transacción**:
  - `CobranzaReconciler.kt:103` — `collapseUuidTwins(FASE_INICIO)`, fuera de toda
    transacción. Si revienta ahí, se lo come el `try/catch` de `:157-160` como
    los otros dos.
  - `CobranzaReconciler.kt:347` — `collapseUuidTwins(FASE_POST_BY_IDS)`, **dentro
    del `db.withTransaction { ... }` que abre en `:335`**, y la línea
    inmediatamente anterior, **`:346`, es
    `paymentDao.saveAll(alive.map { it.toEntity() })`**: el insert de los pagos
    que el canal `by-ids` acaba de **recuperar** del servidor.
    Un *"too many SQL variables"* en `:347` **revierte el `saveAll` de `:346`**.
    O sea: el desbordamiento no sólo falla el colapso — **deshace la
    recuperación de los pagos faltantes**, que es exactamente lo que el canal
    `by-ids` existe para hacer. Los borrados de `:310` y `:374` corren fuera de
    esa transacción y no tienen esa segunda consecuencia.
  - Que el colapso vaya en la misma transacción que el insert **es deliberado y
    correcto** — el KDoc de `:63` lo dice (*"colapsa su gemelo UUID en el mismo
    `withTransaction`"*) y es el invariante duro de `docs/COBRANZA-SYNC.md`. Lo
    que está mal no es la transacción: es el `IN (...)` sin trocear dentro de
    ella.
  - **El conjunto no tiene cota.** `pagoPhantoms` es `localPagoIds - serverPagoIds`
    (`:304`) y `saldoPhantoms` es `localSaldoIds - serverSaldoIds` (`:368`).
    `fetchAllServerIds` (`:433-449`) acumula **todas** las páginas en un solo
    `mutableSetOf`, y `PAGE_LIMIT = 5000` (`:452`) acota la página, no el
    acumulado. Del lado local no acota nada (E-APP-001).
  - `app/build.gradle.kts:79` — `minSdk = 24`.
  - `SqliteInParams.kt:3-20` — el KDoc dice que `SQLITE_MAX_VARIABLE_NUMBER` es
    **999** en el SQLite del framework para **todo** Android por debajo de API 31;
    `:21` es la constante compartida `SQLITE_MAX_IN_PARAMS = 900`.
- **Efecto — INFERIDO, no medido en ningún aparato:** en un teléfono < API 31 con
  más de ~900 fantasmas en una corrida, el `IN (...)` lanza *"too many SQL
  variables"*; la excepción la atrapa el `try/catch` de `CobranzaReconciler.kt:157-160`
  y `reconcileNow` devuelve `ReconcileOutcome.Error`. Como el reconciliador es sin
  estado entre corridas (`:428-431`, *"reconcile is stateless across runs by
  design"*), el siguiente tick repite la misma corrida y vuelve a fallar: el canal
  `by-ids` queda **muerto en ese teléfono, en silencio**.
- **Por qué es dinero:** `CobranzaReconcileWorker.kt:21-23` describe `reconcileNow()`
  como *"la única ruta que detecta pagos faltantes y los pide por el canal
  `by-ids`, que no tiene watermark: es la defensa robusta contra un servidor con
  el sync congelado"*.
- **No es observable en pruebas:** `SqliteInParams.kt:16-19` — *"Ninguna prueba
  puede detectar que falta el troceo: Robolectric corre sobre el SQLite de
  escritorio, cuyo tope es de decenas de miles."*
- **Contradice — MEDIDO (`git show 1d8ae40b`, 2026-09-23):** el commit `1d8ae40b`
  (2026-09-23 13:11, *"fix(cobranza): trocea los IN (...) de mergePagos, que
  tenían una fila de margen"*) editó **este mismo archivo** —16 líneas— y dejó las
  dos llamadas de arriba sin trocear. El arreglo cubrió `mergePagos` y
  `collapseLegacyTwins`; el borrado de fantasmas no.
- **Guardada por:** — y por `SqliteInParams.kt:16-19` ninguna prueba de Robolectric
  puede fijarlo. La red tendría que ser una prueba instrumentada o una revisión.
  **Esa frase del código es falsa como está redactada: ver E-APP-040.**
- **Precisión del 2026-09-24 — el defecto es LATENTE, no vigente:** el tope de
  999 vive por debajo de API 31 y **hoy no hay ningún teléfono en servicio por
  debajo de API 31** (E-APP-036, MEDIDO). Lo que **sí** alcanza el tamaño
  peligroso es el conjunto: 5,522 pagos en una sola zona, seis veces el tope
  (E-APP-034, MEDIDO). O sea: la bala existe y la cámara está cargada; lo que
  falta hoy es el aparato que la dispare, y `minSdk = 24` permite que entre uno.
- **Y el arreglo obvio es incorrecto:** `.chunked()` a secas sobre un `DELETE`
  cambia una sentencia por *N* y rompe la atomicidad. Ver **E-APP-041**.
- **Caduca si:** se trocean las **tres** llamadas o `minSdk` sube a 31

### E-APP-011 · La mitad destructiva del reconciliador no tiene apagador; la reparadora sí

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **El borrado corre siempre:** `CobranzaReconciler.kt:308-311` (pagos) y
  `:372-375` (saldos). La única condición es que el conjunto no esté vacío.
- **La reposición está condicionada:** `:317` y `:379`, los dos
  `if (ByIdsChunker.byIdsAvailable.get())`. Con el latch apagado, el `else`
  (`:350-356`) sólo escribe un log y deja los faltantes al cursor-sync.
- **El latch:** `ByIdsChunker.kt:33` (`val byIdsAvailable = AtomicBoolean(true)`),
  se fija a `false` en el **primer 404** (`:61-62`) y, según su propio KDoc
  (`:16-19`), **no se resetea hasta reiniciar el proceso** — *"proceso-level:
  intencional"*.
- **Orden y transacción:** el borrado ocurre **antes** de la reposición y **fuera**
  de su transacción. El `db.withTransaction { ... }` de `:335-348` envuelve sólo el
  insert y los dos colapsos de gemelos.
- **Consecuencia — INFERIDO:** un solo 404 del canal `by-ids` —un servidor viejo,
  una ruta renombrada, un despliegue a medias— deja al reconciliador **borrando
  sin reponer** durante toda la sesión, y nada en la app lo señala.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** el latch deja de ser de proceso, o el borrado pasa a
  condicionarse igual que la reposición

### E-APP-012 · Bucle pagos→ventas: borrar pagos borra ventas en el tick siguiente

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `CobranzaSyncManager.kt:685-693` — en `mergeVentas`, si
  `saldo <= 0.0 && desdeIso != null`, la venta se conserva **sólo si**
  `paymentDao.countPagosDesde(dto.docto_cc_id, desdeIso) > 0`; si no,
  `saleDao.deleteByDoctoCcId` + `productDao.deleteByFolio` y `continue`.
  `PaymentDao.kt:752-760` — `countPagosDesde` es
  `SELECT COUNT(*) FROM Payment WHERE DOCTO_CC_ACR_ID = :doctoCcAcrId AND FECHA_HORA_PAGO >= :fechaIso`:
  **cuenta filas de `Payment`**.
- **Por qué es dinero:** lo que el reconciliador borra como fantasma (E-APP-001)
  son filas de `Payment`. Borrada la última fila en ventana de una venta saldada,
  el siguiente `mergeVentas` ya no la conserva y **la venta también desaparece**
  de la pantalla del cobrador. Es el incidente **2/2b** del catálogo de
  `docs/COBRANZA-SYNC.md` §10 —*"el pago que salda desaparece"* / *"la venta
  saldada desaparece"*, explicados en §5— reentrando por otra puerta: no por el
  filtro del servidor, sino por el borrado local.
- **Qué lo volvería MEDIDO:** una prueba de dos ticks —borrar el pago en el
  primero, correr `mergeVentas` en el segundo— que compruebe que la venta
  sobrevive.
- **Guardada por:** — (sin prueba todavía). Las pruebas que guardan 2 y 2b son del
  **servidor** (`TestE2E_SyncPagos_LaVentanaEsDeLaVenta_NoDelPago`,
  `TestE2E_ParidadCanales_Ventas`) y no ven este camino.
- **Caduca si:** `mergeVentas` deja de decidir por `countPagosDesde`

### E-APP-013 · Borrar un pago borra también su evidencia fotográfica

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `core/database/.../dao/payment/PaymentImageDao.kt:82-86` —
  `huerfanasAnterioresA(limite)` es
  `SELECT * FROM pago_imagenes WHERE CREADA_EN < :limite AND PAGO_ID NOT IN (SELECT ID FROM Payment)`.
  Único llamador de producción: `app/.../data/pagos/ComprobantesDeAbonoAdapter.kt:280`,
  dentro de `barrerHuerfanos()` (`:273`), que además **borra el archivo del disco**
  (`:282-283`) antes de borrar la fila.
- **El cerrojo de antigüedad sólo lo retrasa:** el KDoc de `PaymentImageDao.kt:70-81`
  dice por qué existe la condición `CREADA_EN < :limite` — el **re-llaveado del
  sync** deja comprobantes un instante sin padre cuando `mergePagos` borra el pago
  capturado para reinsertarlo bajo su llave numérica. No contempla el borrado por
  ausencia: un pago borrado como fantasma no vuelve, y pasada la vejez el barrido
  se lleva su foto y el archivo.
- **Por qué es dinero:** el comprobante es la prueba de que el cobrador cobró.
- **Guardada por:** `core/database/.../PaymentImageDaoTest.kt:114` y `:141` fijan la
  consulta, **no** el escenario de fantasma.
- **Caduca si:** el barrido deja de cruzar contra `Payment`, o el borrado de
  fantasmas aprende a conservar comprobantes

### E-APP-014 · No existe ninguna protección contra borrado masivo, y el borrado masivo está afirmado por pruebas

- **Nivel:** MEDIDO (los dos greps) · **Estado:** VIGENTE
- **Medido:** 2026-09-23 (tarde), checkout principal de `msp-app-kt`
- **Cómo:** `grep -rn "require(\|requireNotNull\|check(\|error(" app/src/main/java/com/example/msp_app/core/sync/cobranza/`
  → **dos aciertos, ninguno es una guarda**: los dos son el `error(...)` de la
  interfaz de telemetría (`CobranzaSyncTelemetry.kt:137` en un comentario y `:319`
  en el override no-op). **Cero `require` / `check` en todo el paquete del sync de
  cobranza.**
- **Control positivo:** el mismo vocabulario sobre el paquete hermano
  `app/src/main/java/com/example/msp_app/core/sync/pendingwork/` **sí** encuentra
  guardas: `V2VisitCustodyRegistry.kt:37` y `:40`, con un KDoc (`:30-35`) que
  explica que *"no son ceremonia"*. El grep discrimina.
- **Y el borrado masivo está afirmado por pruebas (LEÍDO):**
  `app/src/test/.../CobranzaReconcilerTest.kt:293-314` (`phantomPagosAreDeleted`) y
  `:321-342` (`phantomSaldosAreDeleted`) siembran tres filas, hacen que `/ids`
  conteste `IdsResponse(emptyList(), false)` y **afirman que desaparecen**. Una
  guarda contra el borrado masivo pondría esas dos pruebas en rojo: hay que
  cambiarlas con el ojo puesto en qué escenario quieren fijar.
- **El conteo del servidor no se usa como cordura (LEÍDO):** `count_activos` vive
  en `DigestResponse` (`V2CobranzaApi.kt:90`) y su **única** comparación es contra
  el conteo **local**, en el pre-check de digest (`CobranzaReconciler.kt:417`).
  **Nunca** se compara contra `serverPagoIds.size` / `serverSaldoIds.size` después
  de `fetchAllServerIds`. `IdsResponse` sólo lleva `ids` y `has_more`
  (`V2CobranzaApi.kt:130-133`): no hay `count` ni `total` con que detectar una
  enumeración truncada.
- **Guardada por:** — nada; las dos pruebas existentes fijan lo contrario
- **Caduca si:** aparece una cota, un umbral o una comparación contra el conteo del
  servidor antes de borrar

### E-APP-015 · La protección del dinero sin subir es un accidente de tipo, no una guarda

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `PaymentDao.kt:606-607` — `deleteByIDs` es
  `DELETE FROM Payment WHERE ID IN (:ids)`. **No lleva `GUARDADO_EN_MICROSIP = 1`.**
- **Cuatro de los siete `DELETE FROM Payment` del mismo DAO sí lo llevan:** `:584`
  (`deleteByDoctoCcAcrId`), `:675` (dentro de `deleteLegacyTwinsByDoctoCcIds`,
  sentencia desde `:674`), `:697` (dentro de `deleteLegacyTwins`, sentencia desde
  `:696`) y `:890` (`deleteUploaded`). La enumeración de los siete está MEDIDA en
  E-APP-003. El KDoc de `:665-671` los llama *"tres cerrojos"* y el de `:574-583`
  dice literal que *"perder dinero en silencio nunca es una opción; fallar
  ruidosamente sí"*.
- **Lo único que hoy protege** el pago capturado y no subido es que las capturas
  locales acuñan **UUID** y el filtro `mapNotNull { it.toIntOrNull() }` los
  descarta antes de la resta: `CobranzaReconciler.kt:115` (digest) y `:300-302`
  (`reconcilePagosViaIds`). Es el cerrojo 2 de E-APP-004 — un filtro de **tipo**,
  no una guarda del borrado.
- **Consecuencia — INFERIDO:** cualquier cambio que haga numérico un id local, o
  que relaje ese `mapNotNull`, convierte `deleteByIDs` en un borrador de dinero sin
  subir. No hay una segunda línea.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** `deleteByIDs` gana el predicado, o cambia el formato de los ids
  locales

### E-APP-016 · El borrado de fantasmas ES el podador de la tabla `Payment`

- **Nivel:** MEDIDO (grep) · **Estado:** VIGENTE
- **Medido:** 2026-09-23 (tarde), checkout principal de `msp-app-kt`
- **Cómo:** `grep -rn "FueraDeVentana" --include='*.kt' .` (sin `/build/`) → todos
  los aciertos son del lado de **ventas**: `SaleDao.kt:409`
  (`deleteSaldadasFueraDeVentana`) y su mención en `:420`,
  `CobranzaSyncManager.kt:405-406` y su llamador `:268`, `AppNavigation.kt:349`, y
  la prueba `CobranzaSyncManagerTest.kt:404`
  (`pruneSaldadasFueraDeVentanaBorraSoloVentasSinTocarPayments`). **Ninguna
  contraparte para `Payment`.**
- **Control positivo:** el mismo grep **sí** encuentra la poda de ventas — el
  vocabulario discrimina. Y E-APP-003 ya midió que **ninguno** de los siete
  `DELETE FROM Payment` acota por fecha.
- **Consecuencia de diseño — INFERIDO, y es la que manda:** acotar la enumeración
  local a la ventana —la corrección mínima de E-APP-001— **exige añadir una poda
  explícita de `Payment` en el mismo cambio**. Sin ella la tabla crece sin límite,
  el digest local nunca vuelve a coincidir con el del servidor
  (`CobranzaReconciler.kt:417` compara conteo, XOR y suma sobre **todo** lo local)
  y `/ids` corre en **cada tick, para siempre**.
- **Guardada por:** `CobranzaSyncManagerTest.kt:404` guarda que la poda de ventas
  **no** toca `Payment` — es decir, guarda justo la ausencia que aquí duele
- **Caduca si:** aparece una poda por ventana en `PaymentDao`

### E-APP-017 · Dos historiales vivos del mismo cargo, con conjuntos de filas distintos

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **El legado no filtra:** `PaymentDao.kt:102-122` — `getPaymentsBySaleId` es
  `... FROM Payment WHERE DOCTO_CC_ACR_ID = :saleId`, **sin `FORMA_COBRO_ID`**.
  Lo consume `PaymentsViewModel.kt:197-210` y `:249-264`, que sólo mapea
  `toDomain()`, y de ahí salen
  `features/sales/components/paymentshistorysection/PaymentsHistorySection.kt:32`,
  `PaymentTicketScreen.kt:107` y `SaleMapScreen.kt:37`. **Ninguno filtra después.**
  Así que el historial legado **incluye condonaciones**.
- **El módulo nuevo sí filtra:** `RoomPagosAdapter.kt:35` lee el mismo DAO y
  `aHistorial` (`:80`) se queda sólo con
  `it.FORMA_COBRO_ID in VentanaCobro.FORMAS_COBRO_COBRANZA` —
  `setOf(157, 158, 52569)` (`VentanaCobro.kt:65`)—, y el KDoc `:47-58` dice que la
  condonación (`137026`) queda fuera a propósito: *"no es dinero que entró, es
  deuda perdonada"*.
- **Los dos están vivos** en la app de hoy.
- **BAJADA DE NIVEL — lo que no se confirmó:** la compuerta lo reportó como *"dos
  historiales con **totales** distintos"*. `grep -n "sumOf\|total" PaymentsHistorySection.kt`
  → **cero aciertos**: la pantalla legada **lista, no suma**. El subtotal que sí
  existe es el del módulo nuevo, por mes (`RitmoYRiel.kt:339`,
  `RoomPagosAdapter.kt:87-96`). Queda registrado lo verificado —**conjuntos de
  filas distintos**— y **no** "totales distintos".
- **Guardada por:** — (sin prueba de paridad entre los dos historiales)
- **Caduca si:** uno de los dos historiales se retira, o el legado gana el filtro

### E-APP-018 · Tres definiciones de "qué cuenta como cobranza", y sólo una es deliberada

- **Nivel:** MEDIDO (grep) · **Estado:** VIGENTE
- **Medido:** 2026-09-23 (tarde), checkout principal de `msp-app-kt`
- **Cómo:** `grep -n "157" core/database/.../dao/payment/PaymentDao.kt` → **siete
  aciertos**: el KDoc de cabecera (`:18`) y **seis listas literales**
  `IN (157, 158, 52569)` repartidas en **cuatro consultas**: `getPaymentsByDate`
  (`:146-148`), `observePaymentsByDate` (`:278-280`),
  `getAdjustedPaymentPercentage` (`:406-408` y `:416-418`) y
  `observeAdjustedPaymentPercentage` (`:513-515` y `:523-525`).
  *(El informe de la compuerta decía "cinco sitios"; la cuenta exacta es **seis
  listas en cuatro consultas**.)*
- **Segunda definición:** `VentanaCobro.FORMAS_COBRO_COBRANZA` — `setOf(157, 158, 52569)`
  en `core/common/.../cobranza/domain/VentanaCobro.kt:65`, cuyo KDoc (`:47-63`)
  reconoce que los valores están **copiados de `Constants` (`:app`)**.
- **La tercera es deliberada y no es una copia:** `getCollectedAmounts`
  (`PaymentDao.kt:169-176`) recibe el conjunto **por parámetro**, y su KDoc
  (`:164-168`) dice por qué: *"duplicarlo aquí crearía una segunda definición de
  'qué cuenta como cobranza' que puede despegarse de la primera sin que nada
  avise"*. El llamador es `RoomPagosAdapter.kt:75-77`, que le pasa la canónica.
- **El propio DAO lo reconoce:** el KDoc de `PaymentDao.kt:16-23` dice que los IDs
  están inlineados porque `Constants` *"se queda en `:app`, no es alcanzable desde
  `:core:database`"*, y pide actualizar ambos lados a mano.
- **Guardada por:** `app/src/test/.../core/utils/TipoVisitaCatalogoDriftTest.kt`
  existe y, según el KDoc de `VentanaCobro.kt:60-63`, compara la constante contra
  `Constants` y se pone rojo si se despegan. **No se abrió la prueba**: que cubra
  `FORMAS_COBRO_COBRANZA` es LEÍDO del KDoc, no verificado. Las seis listas
  literales de `PaymentDao` no las fija nada.
- **Caduca si:** cambian los IDs en Microsip, o `:core:database` gana acceso a la
  constante canónica

### E-APP-019 · El ajuste del saldo decide "no reconocido" por `DOCTO_CC_ID = 0`, no por la bandera de subido

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `PaymentDao.kt:864-877` — `sumImporteNoReconocidoPorElServidor` es
  `SELECT COALESCE(SUM(IMPORTE), 0) FROM Payment WHERE DOCTO_CC_ACR_ID = :doctoCcAcrId AND DOCTO_CC_ID = 0 AND ID LIKE '%-%' AND ID NOT IN (SELECT PAGO_RECIBIDO_ID ...)`.
  **`GUARDADO_EN_MICROSIP` no aparece.** Lo llama `CobranzaSyncManager` dentro del
  bucle de `mergeVentas`.
- **Su propio KDoc lo documenta**, `:843-854`, bajo *"Límites conocidos"*:
  1. un pago que Microsip **RECHAZÓ** se queda con `DOCTO_CC_ID = 0` **para
     siempre**, *"así que se resta para siempre y el saldo mostrado queda por
     debajo del real"*, y dice explícitamente que el arreglo vigente **no lo
     cierra**;
  2. un pago que el servidor **sí** aplicó pero cuya respuesta nunca llegó queda
     igual, y nombra la firma que ya se vio en la flota: `GUARDADO_EN_MICROSIP = 1`
     con `DOCTO_CC_ID = 0`, **incidente del 2026-08-31**.
- **Enlace:** la nota de memoria *"Pago marcado enviado SIN llegar (2026-08-31) —
  `GUARDADO_EN_MICROSIP=1` con `DOCTO_CC_ID=0`; ningún 401 se captura jamás"*. Es
  el mismo estado de fila, visto desde el saldo que ve el cobrador.
- **Qué lo volvería MEDIDO:** contar en un Room de campo las filas con
  `GUARDADO_EN_MICROSIP = 1 AND DOCTO_CC_ID = 0` y cuánto restan.
- **Guardada por:** — `sumImporteNoReconocidoPorElServidor` sólo aparece en el DAO
  y en `CobranzaSyncManager`; **ninguna prueba la nombra** (MEDIDO, `grep -rl`
  sobre `*.kt` sin `/build/`)
- **Caduca si:** la fila local aprende a decir "rechazado"

### E-APP-020 · El caso de campo del 2026-09-23: el dinero está bien, el −$3,000 es de pantalla

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, **producción** (Microsip), por el agente que diagnosticó
  el reporte del cobrador
- **El reporte:** *"no me deja meter la condonación"* y *"ahora me sale −$3,000"*.
- **Lo medido:** venta **`M00002099`** (`DOCTO_CC_ACR_ID = 14417151`, $11,000)
  traía **$5,700** de saldo. Entraron **$4,700 en efectivo** (15:25) y **$1,000 de
  condonación** (15:27, documento `16132275`) = **$5,700 exactos**. **Saldo real
  en Microsip: $0.**
- **Conclusión:** no se cobró de más ni se perdió dinero. El **−$3,000** es un
  número de **pantalla del teléfono**, y las afirmaciones E-APP-021 a E-APP-024
  son el mecanismo que lo explica.
- **Procedencia:** la salida cruda **no está en el árbol**; las cifras vienen del
  informe del 2026-09-23. Lo que sí se verificó línea por línea en el código es el
  mecanismo (E-APP-021 … E-APP-024, E-COB-014 … E-COB-016).
- **CERRADO EL 2026-09-24 — lo que faltaba, ya está medido.** La ficha decía:
  *"Lo que cerraría el caso y sigue SIN MEDIR: el Room del teléfono de ese
  cobrador — cuántas filas de condonación tiene sobre esa venta."* Se midió, y
  **el conteo es cuatro, no una**: **E-APP-029**. La lectura de esta ficha
  —*"$1,000 de condonación (15:27, documento `16132275`)"*— **no era falsa**:
  ése es el único de los cuatro que llegó a Microsip, y por eso la suma cerraba
  en $5,700 exactos. Lo que no podía ver, midiendo sólo del lado de Microsip,
  son los **tres intentos que no dejaron rastro ahí**. Las horas tampoco se
  contradicen: 15:27 local (CST, UTC−6) es 21:27 UTC, que es como las reporta
  E-APP-029.
  **La conclusión de esta ficha se sostiene y se refuerza:** no se cobró de más
  ni se perdió dinero **en Microsip**, y el −$3,000 es de pantalla. La versión
  también quedó fechada: **E-APP-038**.
- **Sigue SIN MEDIR:** `MSP_SALDOS_VENTAS` para el cargo `14417151` y el censo
  `WHERE SALDO < 0` — **el túnel a producción está caído**.
- **Guardada por:** — (es un caso, no un comportamiento)
- **Caduca si:** aparece el volcado del Room de ese teléfono

### E-APP-021 · El tope `coerceAtLeast(0.0)` del saldo vive dentro del `else` y no corre en reposo

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde), rama `feat/notas-visibles` del checkout principal
  de `msp-app-kt`
- **Dónde:** `app/.../data/api/services/cobranza/VentaDto.kt:193-197`:

  ```kotlin
  fun SaleEntity.conSaldoAjustadoPorPagosEnVuelo(enVuelo: Double): SaleEntity = if (enVuelo <= 0.0) {
      this
  } else {
      copy(SALDO_REST = (SALDO_REST - enVuelo).coerceAtLeast(0.0))
  }
  ```

  Con `enVuelo <= 0.0` devuelve la entidad **tal cual, sin pasar por el tope**.
- **Por qué en reposo `enVuelo` es 0:** la suma de pagos en vuelo es
  `sumImporteNoReconocidoPorElServidor` (`PaymentDao.kt:864-877`), que sólo cuenta
  filas con `DOCTO_CC_ID = 0`. Todo pago ya aplicado por el servidor trae
  `DOCTO_CC_ID > 0`, así que **en reposo la rama del tope no se ejecuta nunca** y
  el `SALDO_REST` local se pinta sin piso.
- **Contradice — el KDoc de la propia función**, `VentaDto.kt:189-191`, literal:

  > `` `coerceAtLeast(0.0)` es defensa, no lógica: si por lo que sea el descuento ``
  > `excediera el saldo, un negativo en pantalla sería peor que un cero.`

  Ese *"un negativo en pantalla sería peor que un cero"* se lee como una defensa
  general contra el negativo. **Sólo cubre el caso de capturas sin subir.** El
  negativo que ya está en `SALDO_REST` —venga de donde venga— pasa intacto.
- **PRECISIÓN del 2026-09-24 — MEDIDO (`grep -rn 'conSaldoAjustadoPorPagosEnVuelo'
  --include='*.kt' .`, sin `/build/`): el tope tiene UN SOLO LLAMADOR.** Los tres
  aciertos son la declaración (`VentaDto.kt:193`), el `import`
  (`CobranzaSyncManager.kt:16`) y **la única invocación**,
  `CobranzaSyncManager.kt:702`, dentro del upsert de `mergeVentas`.
  **Control positivo:** el mismo grep sobre `sumImporteNoReconocidoPorElServidor`
  sí encuentra sus dos sitios (el DAO y ese mismo llamador), así que discrimina.
  **Consecuencia, y hay que decirla en voz alta porque el KDoc invita a lo
  contrario:** ese tope **no cubre la app**. No lo ve
  `CobranzaReconciler.kt:387` (E-APP-022), no lo ve `SaleDao.updateTotal`, no lo
  ve `SaleDao.replaceAll`, y **ninguna pantalla** lo aplica al pintar — las
  pantallas leen `SALDO_REST` de Room tal cual. Es un ajuste de **un** camino de
  sincronización, no un piso del saldo.
- **Guardada por:** — (sin prueba que fije el caso `enVuelo == 0` con
  `SALDO_REST < 0`)
- **Caduca si:** el tope sale del `else`, o el saldo deja de pintarse desde
  `SALDO_REST`

### E-APP-022 · `SaleDao.updateTotal` resta sin piso, y es el único camino local que **FABRICA** un saldo negativo — ~~el único capaz de~~

> **CORREGIDA el 2026-09-24.** El título decía *"el único camino local capaz de
> fabricar un saldo negativo"*. Es **el único que RESTA**, no el único que
> **escribe** `SALDO_REST`, y no es el único por el que un negativo puede acabar
> en Room.
> **Qué se creyó y por qué parecía razonable:** la ficha salió de seguir el
> camino de la captura —el que explica el caso de campo— y ahí `updateTotal` es
> en efecto el único escritor: es el único `UPDATE` con forma
> `SALDO_REST = SALDO_REST - :amount`, el único sin cota, y el único que corre
> por cada abono y cada condonación. La palabra que sobró fue **"capaz"**: de
> "es el único que resta" se pasó a "es el único por el que puede entrar", que
> es una afirmación distinta y más ancha, y nadie enumeró a los demás
> escritores.
> **Y contradecía a E-COB-014 sin decirlo:** esa ficha establece que un saldo
> negativo **sí viaja** desde el servidor por la rama `FECHA_ULT_PAGO >= ?`.
> Si viaja, entra por `CobranzaReconciler.kt:387` y se escribe crudo. Las dos
> fichas se sembraron el mismo día, ninguna citó a la otra. **Quedan enlazadas
> abajo, y en E-COB-014.**

- **Nivel:** MEDIDO (la enumeración de escritores) · **Estado:** VIGENTE
  (corregida 2026-09-24)
- **Medido:** 2026-09-24, checkout principal de `msp-app-kt` (`d88dfb8c`)
- **Cómo:** `grep -n 'SALDO_REST' SaleDao.kt` (13 aciertos) cruzado con
  `grep -rn 'replaceAll\|saleDao.insertAll\|updateTotal' --include='*.kt'` sobre
  `app/src/main`, `core/` y `feature/`, sin `/build/`.
- **Control positivo:** el mismo cruce **sí** separa los escritores que **no**
  tocan el saldo —`updateEstadoCobranzaActivasByClienteId` (`SaleDao.kt:320`,
  cuyo KDoc `:310` dice literal *"No toca `SALDO_REST`"*) y
  `updateTemporaryCollectionDate` (`:339`)—, así que la enumeración discrimina
  entre "escribe la fila" y "escribe el saldo".

#### Los CINCO escritores de `sales.SALDO_REST`, y los TRES que pueden dejarlo negativo

| # | Sitio | Forma | ¿Negativo? |
|---|---|---|---|
| 1 | `SaleDao.kt:279-289` `updateTotal` | `SALDO_REST = SALDO_REST - :amount`, sin piso | **Lo FABRICA** |
| 2 | `CobranzaReconciler.kt:387` `saleDao.insertAll(alive.map { it.toEntity() })` | `SET` absoluto con el valor **crudo del servidor** | **Lo IMPORTA** |
| 3 | `SaleDao.kt:350-354` `replaceAll` | `deleteAll()` + `insertAll()`, valor crudo | **Lo PERPETÚA** |
| 4 | `SaleDao.kt:339-340` `insertAll` desde `CobranzaSyncManager.kt:715` | `SET` absoluto, **con** el tope condicional | no (único con piso) |
| 5 | `VisitsLocalDataSource.kt:99` `saleDao.updateTotal(saleId, 0.0, newState)` | resta **0.0**: sólo mueve `ESTADO_COBRANZA` | no (no mueve el saldo) |

- **(2) es el que faltaba, y es el que cierra el círculo con E-COB-014.**
  `CobranzaReconciler.kt:387` escribe `it.toEntity()` **sin** pasar por
  `conSaldoAjustadoPorPagosEnVuelo`, que es donde vive el único
  `coerceAtLeast(0.0)` de la app (E-APP-021, **un solo llamador**:
  `CobranzaSyncManager.kt:702`). El camino hermano, (4), **sí** lo aplica. Es
  decir: **los dos caminos que escriben la misma columna desde el mismo
  servidor discrepan en si hay piso**, y el que no lo tiene es el del
  reconciliador. Esto ya estaba anotado al margen en E-APP-006 (*"`mergeVentas`
  sí preserva `ESTADO_COBRANZA`, pero `CobranzaReconciler.kt:387` no"*) — es el
  mismo par de caminos divergiendo, visto desde otra columna.
- **(3) `replaceAll` es el botón de semana.** La cadena es
  `SaleDao.kt:350-354` ← `SalesLocalDataSource.kt:37` ← `AuthViewModel.kt:152`
  (E-APP-005). No fabrica el negativo, pero lo **reescribe tal cual** al abrir
  la semana: un `-3,000` sobrevive al botón que el cobrador usa para "empezar de
  cero".
- **Contradice / enlaza:** **E-COB-014** (`cobranza.md`) — el servidor **sí**
  entrega saldos negativos, y (2) es la puerta por la que entran. E-APP-025 mide
  que **hoy** Microsip no tiene ninguno que entregar, lo que hace que (1) sea la
  única fuente **observada**; eso no reduce la lista de puertas abiertas.
- **Dónde (sin cambios, reverificado 2026-09-24):**
  `core/database/.../dao/sale/SaleDao.kt:279-289` —
  `UPDATE sales SET SALDO_REST = SALDO_REST - :amount, ESTADO_COBRANZA = :estadoCobranza WHERE DOCTO_CC_ACR_ID = :saleId`.
  Es un **decremento**, no un `SET` absoluto, y **no tiene `MAX(0, ...)`** ni
  ninguna cota.
- **Quién lo llama:** `app/.../data/local/datasource/payment/PaymentsLocalDataSource.kt:166`,
  dentro de `insertPaymentAndUpdateSale` (`:158-167`), ~~que es `@Transaction`:
  cada captura inserta el pago y descuenta el saldo en la misma transacción~~.
  **CORREGIDO el 2026-09-24:** ese `@Transaction` de `:158` **no genera nada** —
  la clase no es un `@Dao` (**E-APP-045**). El insert del pago y el descuento del
  saldo son **dos escrituras sueltas**. La ficha lo dio por bueno leyendo la
  anotación; la anotación está ahí y no hace nada.
- **Por qué importa:** toda captura —abono o condonación— pasa por aquí. Dos
  restas por el mismo dinero (E-APP-023) bajan `SALDO_REST` dos veces y nada lo
  detiene en cero. El KDoc del guard del abono nuevo
  (`RegistrarAbonoViewModel.kt:890-894`) nombra exactamente este mecanismo:
  *"`updateTotal` —que es un decremento, no un set absoluto— bajaba `SALDO_REST`
  dos veces"*.
- **Corroboración de campo (2026-09-24, MEDIDO):** el `-3000.0` medido en el
  teléfono es **exactamente** cuatro restas de $1,000 desde un saldo de $1,000
  — la aritmética de (1), no la de (2). Ver **E-APP-030**.
- **Guardada por:** — (sin prueba que fije un piso; no lo hay que fijar).
  Tampoco hay ninguna que fije que (2) y (4) escriben lo mismo.
- **Caduca si:** `updateTotal` gana un piso, el saldo local pasa a escribirse
  como valor absoluto, o `CobranzaReconciler.kt:387` aprende a aplicar el mismo
  ajuste que `CobranzaSyncManager.kt:702`

### E-APP-023 · El diálogo de condonación no tiene ninguna de las tres protecciones del abono nuevo

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `app/.../features/forgiveness/components/NewForgivenessDialog.kt`.
- **(a) Id nuevo en cada confirmación.** `:111` —
  `ID = UUID.randomUUID().toString()` se acuña **dentro** de
  `handleSaveForgiveness()`, o sea en **cada** toque de "Confirmar". El insert es
  `@Insert(onConflict = OnConflictStrategy.REPLACE)` **por ID**
  (`PaymentDao.kt:550-551`), así que dos toques no se pisan: son **dos filas**, y
  por E-APP-022, **dos restas**.
  **Compara:** el abono nuevo acuña su id **una sola vez** y lo persiste en el
  `SavedStateHandle` (`RegistrarAbonoViewModel.kt:119-120`), justamente para que
  el reintento reuse el mismo.
- **(b) Sin guarda de reentrada.** El `TextButton("Confirmar")` (`:271-274`) llama
  `handleSaveForgiveness()` directo, y el diálogo se cierra **al final** de la
  corrutina (`:137-138`: `showAlertDialog = false` y `onDismissRequest()`), después
  de guardar, arrancar el servicio de ubicación y navegar. La ventana entre el
  primer toque y el cierre está abierta.
  **Compara:** `RegistrarAbonoViewModel.kt:661-664` —
  `if (yaSeEncolo || actual.guardando || actual.registrado != null) return` y
  `yaSeEncolo = true` **sincrónico y antes del `launch`**, con el guard persistido
  en `SavedStateHandle` (`:122-126`).
  *(El informe apuntaba a `RegistrarAbonoViewModel.kt:890`; ahí está el **KDoc**
  que narra el incidente —`:880-895`—, no la guarda. La guarda es `:661-664`.)*
- **(c) Valida contra una foto del saldo.** `ForgivenessScreen.kt:44-46` es un
  `LaunchedEffect(saleId) { viewModel.loadSaleDetails(saleId) }`: carga la venta
  **una sola vez** por `saleId`. El `sale` que recibe el diálogo —y su
  `SALDO_REST`, que es contra lo que valida `:152` y lo que pinta `:219`— **no se
  mueve aunque Room baje** por una captura hecha en esa misma pantalla.
- **Por qué es dinero:** la combinación (a)+(b) es un doble descuento local por un
  solo perdón, y (c) es la razón de que la pantalla no se entere.
- **AMPLIADA el 2026-09-24: son trece, no tres.** Ver **E-APP-043**, con la
  tabla completa y el censo de pruebas. Y (a) y (c) quedaron **medidas en
  campo**: cuatro filas separadas 11-15 s (**E-APP-029**), que es exactamente lo
  que predicen un id nuevo por toque y una validación contra una foto del saldo.
- **Guardada por:** — (el diálogo de condonación no tiene pruebas de su
  comportamiento; MEDIDO en E-APP-043)
- **Caduca si:** el diálogo se migra al módulo nuevo, o gana guard e id estable

### E-APP-024 · Con saldo ≤ 0 el diálogo de condonación queda inservible, y su mensaje miente

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde:** `NewForgivenessDialog.kt:147-154` — la validación es, en orden:
  `inputDouble <= 0` → *"El monto debe ser mayor a cero"*;
  `inputDouble > sale.SALDO_REST` → *"El monto no puede ser mayor al saldo
  restante"*. Y `:251-255` — el botón lleva
  `enabled = inputValue.isNotBlank() && errorMessage == null`.
- **La aritmética:** con `SALDO_REST <= 0`, el conjunto aceptado es
  `{x > 0} ∩ {x ≤ SALDO_REST}` = **vacío**. El prellenado con el saldo falla por
  *"debe ser mayor a cero"*; cualquier monto positivo falla por *"no puede ser
  mayor al saldo restante"*. **El botón queda deshabilitado para siempre.**
- **Y el mensaje miente:** arriba, `:210` y `:219`, la pantalla pinta
  *"Saldo actual: "* seguido de `sale.SALDO_REST.toCurrency(noDecimals = true)`
  — es decir, **"Saldo actual: −$3,000"**. El cobrador lee un saldo negativo y
  un error que no le dice lo único cierto: que esa venta ya está saldada.
- **CORRECCIÓN del 2026-09-24 — los dos errores NO se ven a la vez.** La ficha
  decía que el cobrador ve *"dos errores que se contradicen"*. **El defecto es
  real; la descripción era imprecisa.** El `when` de `NewForgivenessDialog.kt:148-154`
  es **excluyente**: `errorMessage` es **un** `String?`, las ramas se evalúan en
  orden y la primera que casa gana, así que **se pinta uno a la vez**.
  **Qué se creyó y por qué parecía razonable:** la aritmética del párrafo
  anterior es correcta —los dos errores son **alcanzables** con `SALDO_REST ≤ 0`,
  y entre los dos cierran el conjunto aceptado hasta dejarlo vacío—, y de
  "alcanzables los dos" se pasó a "visibles los dos". Lo que el cobrador vive no
  es una contradicción en pantalla: es que **ningún monto funciona**, con un
  motivo distinto según lo que teclee. Que es peor de explicar y más difícil de
  reportar.
- **Y hay un segundo motivo por el que la ficha no cierra el caso de campo:**
  decía *"el botón queda deshabilitado para siempre"*, y el cobrador **sí**
  registró cuatro condonaciones (E-APP-029). No es un contraejemplo: el diálogo
  valida contra una **foto** del saldo tomada una sola vez (E-APP-023(c),
  `ForgivenessScreen.kt:44-46`), así que las cuatro pasaron contra el mismo
  `SALDO_REST = 1,000` que ya no era cierto desde la primera. El botón se
  deshabilita **cuando la pantalla se vuelve a montar**, no mientras está
  abierta. Ver **E-APP-031**.
- **Es exactamente el reporte de campo** de E-APP-020: *"no me deja meter la
  condonación"* y *"me sale −$3,000"* son **el mismo defecto**, visto dos veces.
- **Guardada por:** — (sin prueba)
- **Caduca si:** el diálogo detecta el saldo ≤ 0 y lo dice, o se retira

### E-APP-025 · En Microsip no hay saldos negativos: 11,178 ventas, 0 con saldo < 0

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, **producción** (Microsip), censo de las **46 zonas**
- **Cómo y salida:** **11,178 ventas** revisadas, **0 con saldo < 0**; **44 con
  saldo exactamente 0**.
- **Control positivo:** esas 44 también fallan `SALDO > 0` y **aun así salieron**
  del censo — es decir, la consulta no filtraba por saldo positivo y **habría
  devuelto una negativa** si existiera. La ausencia es un hallazgo, no un hueco.
- **LÍMITE HONESTO, y hay que respetarlo:** una venta negativa cuyo **último pago
  quede fuera de la ventana** no entra por ninguna rama del filtro
  (`ventaStatusFilterConVentana`, E-COB-014), así que el censo es **cota
  inferior**, no el universo. "Cero encontradas" no es "cero existen".
- **Procedencia:** la salida cruda no está en el árbol; la cifra viene del informe
  del 2026-09-23. El **túnel a producción está caído**, así que no se pudo repetir
  el censo ni correr el equivalente sobre `MSP_SALDOS_VENTAS`
  (`WHERE SALDO < 0`) — ver E-COB-015.
- **Consecuencia:** el negativo que ve el cobrador **se fabrica en el teléfono**
  (E-APP-021 … E-APP-024), no llega desde Microsip. Eso no cierra E-COB-014 ni
  E-COB-015: el servidor **entregaría** un negativo y **no sabría** detectarlo; lo
  que dice esta medición es que hoy no tiene ninguno que entregar.
- **Guardada por:** — (es un censo, no un comportamiento)
- **Caduca si:** cambia el cálculo del saldo en Microsip, o aparece la primera
  negativa

### E-APP-026 · «El monto de $4,700 salió de una liquidación sugerida con un saldo inflado»

- **Nivel:** MEDIDO · **Estado:** **REFUTADA (2026-09-23)**
- **Quién lo creyó y por qué parecía razonable:** lo planteó como hipótesis el
  agente que diagnosticó el caso del 2026-09-23, y encajaba: la pantalla ofrece un
  monto de liquidación, y un saldo local inflado explicaría de un golpe tanto el
  monto raro como el negativo posterior.
- **Por qué es falsa:** los **$4,700 eran exactamente el saldo ($5,700) menos el
  perdón ($1,000)** (E-APP-020). No hubo monto de más: la aritmética cierra.
- **Y la validación "monto ≤ saldo" sí existe, en cuatro sitios (LEÍDO,
  verificados uno por uno):**
  - `feature/pagos/.../domain/SeguridadDelAbono.kt:154-161` — `bloqueosDe` con
    `NO_ES_POSITIVO`, `EXCEDE_EL_SALDO` y `VENTA_SIN_SALDO`;
  - `feature/pagos/.../ui/RegistrarAbonoViewModel.kt:686-688` — `puedeEscribirse`,
    que la vuelve a abrochar antes de escribir;
  - `feature/pagos/.../application/RegistrarAbono.kt:50-61` — el caso de uso, que
    además reporta por telemetría si la pantalla dejó pasar algo;
  - `app/.../data/pagos/RegistroDeAbonoAdapter.kt:344-356` — `sobrepasaElSaldo`,
    contra el `SALDO_REST` real al momento de escribir.
  Los cuatro llaman a **la misma función**, que es lo que impide que discrepen.
- **CUIDADO:** esto refuta la hipótesis **para el camino del abono nuevo**. El
  diálogo de **condonación** no pasa por ninguno de esos cuatro sitios: tiene su
  propia validación en sitio (E-APP-024).
- **Caduca si:** alguno de los cuatro sitios deja de llamar a `bloqueosDe`

### E-APP-027 · «El teléfono perdió filas de pago y por eso sugirió de más»

- **Nivel:** INFERIDO · **Estado:** **REFUTADA (2026-09-23)**
- **Quién lo creyó y por qué parecía razonable:** misma sesión, mismo agente. Era
  la hipótesis natural dado E-APP-001 (el reconciliador **sí** borra filas de
  `Payment` por ausencia): si el teléfono perdió pagos, el saldo que calcula sube.
- **Por qué es falsa — por dirección:** con filas de pago faltantes el teléfono
  habría creído que **faltaba más** por cobrar, no menos. El error observado
  apunta al lado contrario. El razonamiento se sostiene sin medir nada: es un
  signo.
- **Corroboración:** el ritmo que pinta la pantalla (**7 de 12** semanas con pago)
  **coincide con Microsip** (6-7 semanas con pago en las 12 previas). Si faltaran
  filas, ese conteo sería menor.
- **CUIDADO — lo que esto NO refuta:** E-APP-001 sigue VIGENTE. El borrado por
  ausencia existe; lo que se refuta es que **este** caso se explique por él.
- **Caduca si:** aparece un caso donde el desfase vaya en la otra dirección

### E-APP-028 · El arreglo del doble descuento es de HOY, y `v2.18.0` NO lo contiene

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23 (tarde), checkout principal de `msp-app-kt`
- **Cómo y salida:**
  - `git show -s --format='%H %ad %s' --date=iso c28cdb36` →
    `c28cdb36… 2026-09-23 13:14:58 -0600 fix(cobranza): el saldo deja de descontar dos veces el pago recién cobrado`.
    **Es de hoy 23, no del 22.**
  - `git merge-base --is-ancestor c28cdb36 v2.18.0` → **falso** (rc=1).
    `v2.18.0` es `130e4089`, del **2026-09-22 19:31:11 −0600**
    (*"integra los nueve arreglos de la ruta del 22-sep"*), o sea **anterior** al
    arreglo. **`v2.18.0` NO lo contiene.**
  - `15ae6cfb` es `chore(release): 2.18.1 (62)`, **2026-09-23 13:47:51 −0600**,
    contenido en `main` y `origin/main`, y `git tag --points-at 15ae6cfb` →
    **vacío**: la **2.18.1 no está etiquetada**.
- **Control positivo:** `git merge-base --is-ancestor 130e4089 v2.18.0` → **rc=0**
  (verdadero). El comando sí distingue: contesta afirmativo cuando el commit **sí**
  está contenido. Y el padre del arreglo (`ee320fec`) tampoco es ancestro, lo que
  ubica la rama completa fuera de la etiqueta.
- **CORRIGE:** cualquier afirmación que sitúe ese arreglo como desplegado *"la
  noche del 22"*. La versión que salió el 22 (`v2.18.0` / `2.18.0 (61)`) **no lo
  lleva**; lo lleva la **2.18.1**, que es de hoy y **no está etiquetada**. Al
  revisar `app-android.md` y `cobranza.md` del 2026-09-23 **no se encontró**
  ninguna afirmación sembrada que lo dijera, así que no hubo nada que editar — la
  confusión vive en las notas de memoria y en la conversación.
- **Por qué importa:** si un teléfono reporta "2.18.1" **sin fecha del reporte**
  (E-APP-020), no se puede concluir qué versión corría a las 15:25 de ayer ni
  esta mañana. Fechar el binario es parte de fechar la lectura.
- **Guardada por:** — (es estado del repositorio)
- **Caduca si:** se etiqueta la 2.18.1 o se publica una 2.18.2

### E-APP-029 · La venta `14417151` tiene CUATRO condonaciones de $1,000, y sólo la primera llegó a Microsip

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** **2026-09-24 16:41:56 UTC**, **en el aparato**: teléfono de
  `javier.gil.perez@msp.com` (samsung SM-A155M, Android 16, app **2.18.1**), vía
  el depurador remoto del Room, **sólo `SELECT`**
- **Cómo y salida:** las cuatro filas de condonación sobre
  `DOCTO_CC_ACR_ID = 14417151`, del **2026-09-23**, a las
  **21:27:25, 21:27:38, 21:27:53 y 21:28:04 UTC** (15:27:25 … 15:28:04 hora
  local, CST):

  | # | Hora UTC | `ID` | `DOCTO_CC_ID` | `GUARDADO_EN_MICROSIP` | ¿Llegó? |
  |---|---|---|---|---|---|
  | 1 | 21:27:25 | `16132276` (numérico) | `16132275` | 1 | **sí** |
  | 2 | 21:27:38 | UUID | `0` | 1 | **no** |
  | 3 | 21:27:53 | UUID | `0` | 1 | **no** |
  | 4 | 21:28:04 | UUID | `0` | 1 | **no** |

- **Canario contra el truncamiento:** `COUNT(*) = 24` contra el `rowCount = 24`
  del mismo resultado. Coinciden: **no hubo corte**. El canario no es ceremonia
  — el depurador corta en `maxQueryRows = 1000` y **no lo anuncia** (E-INF-008),
  así que sin el conteo aparte un resultado cortado se lee como completo.
- **La separación de 11-15 s DESCARTA el toque repetido.** Un doble toque
  accidental sobre "Confirmar" cae en sub-segundo. ~13 s es otra cosa, y el
  código dice cuál: el diálogo **limpia el campo** al confirmar
  (`NewForgivenessDialog.kt:135`, `inputValue = ""`), así que para volver a
  intentar hay que **reescribir el importe** — el prellenado con el saldo ocurre
  en otro momento (`:92`). Teclear $1,000 y confirmar cuesta del orden de esos
  segundos. La lectura que **apoya** —no prueba— es la natural: el cobrador
  reintentó porque **no vio confirmación**, que es exactamente lo que
  **E-APP-031** describe.
- **Los tres que no llegaron traen la firma del 2026-08-31:**
  `GUARDADO_EN_MICROSIP = 1` con `DOCTO_CC_ID = 0` — el estado de fila que
  E-APP-019 documenta como *"el servidor lo aplicó pero la respuesta nunca
  llegó"* o *"Microsip lo rechazó"*, indistinguibles desde el teléfono. Ver
  también **E-APP-032**.
- **Enlaza:** cierra la pregunta abierta de **E-APP-020**. Confirma
  **E-APP-023**(a) —id nuevo por confirmación, así que son cuatro filas y no una
  reescrita— y **(c)** —la validación corre contra una foto del saldo, que es
  por lo que las tres de más pasaron el filtro de `:152`.
- **Procedencia:** la salida cruda **no está en el árbol**; las cifras vienen del
  informe de esa lectura.
- **Guardada por:** — (es una lectura de campo, no un comportamiento)
- **Caduca si:** se reprocesa esa venta o se corrigen las filas a mano

### E-APP-030 · `sales.SALDO_REST = -3000.0` en el teléfono, y la aritmética que cierra es la LOCAL

- **Nivel:** MEDIDO (el valor) · **Estado:** VIGENTE
- **Medido:** 2026-09-24 16:41:56 UTC, mismo teléfono y misma lectura que
  E-APP-029
- **Salida:** para el cargo `14417151`, `sales.SALDO_REST = -3000.0`.
- **La aritmética que cierra, y es UNA sola (LEÍDO):** saldo $5,700 → $4,700 en
  efectivo → **$1,000**; y de ahí **cuatro** `updateTotal(−1,000)`
  (`SaleDao.kt:279-289`, un decremento sin piso, uno por cada fila de E-APP-029)
  → **−$3,000**. Es el camino **(1)** de E-APP-022, el único que resta.
- **La aritmética que NO cierra, y por eso hay que decirla:** *"el servidor
  publica $0 y el teléfono resta los tres pagos que no reconoce"* **no produce
  −$3,000 en la 2.18.1**. Ese camino es `mergeVentas`
  (`CobranzaSyncManager.kt:702`), que aplica `conSaldoAjustadoPorPagosEnVuelo`:
  con `enVuelo = 3,000` entra al `else` y hace
  `(0 − 3000).coerceAtLeast(0.0)` = **0.0** (`VentaDto.kt:193-197`). El arreglo
  que puso ese tope **sí** está en la 2.18.1 (**E-APP-038**). Los dos caminos
  dan números distintos y el medido es el local.
- **Lo que eso implica, y está SIN MEDIR:** para que el `−3000.0` siguiera ahí
  un día después, **esa fila no volvió a pasar por `mergeVentas`** — o el
  teléfono no sincronizó esa venta, o no llegó en la ventana. Cuál de las dos,
  **no se midió**. **Qué lo volvería MEDIDO:** leer `UPDATED_AT`/la marca de
  último sync de esa fila en el mismo Room, o forzar un tick y releer.
- **Por qué importa que sea la local y no la del sync:** son dos arreglos
  distintos. Si fuera del sync, el arreglo es cablear el tope en
  `CobranzaReconciler.kt:387` (E-APP-022, camino **(2)**). Como es local, el
  arreglo es el piso en `updateTotal` **y** la revalidación del diálogo
  (E-APP-031, E-APP-043).
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (sin prueba que fije el piso)
- **Caduca si:** `updateTotal` gana un piso, o esa fila se re-sincroniza

### E-APP-031 · La causa raíz de los cuatro intentos: el diálogo de condonación se saca de encima su propio ticket

- **Nivel:** LEÍDO (el orden de llamadas) · **Estado:** VIGENTE
- **Leído:** 2026-09-24, rama `feat/notas-visibles` (`d88dfb8c`)
- **La secuencia, tal cual está escrita** (`NewForgivenessDialog.kt`, dentro del
  `coroutineScope.launch` de `:106`):
  1. `:126` — `paymentsViewModel.savePayment(forgiveness)`.
  2. `:133` — `navController.navigate("payment_ticket/${forgiveness.ID}")`
     **empuja el ticket**.
  3. `:135` — `inputValue = ""` limpia el campo.
  4. `:137` — `showAlertDialog = false`.
  5. `:138` — `onDismissRequest()`, que **en la ruta nueva es
     `navController.popBackStack()`** (`ForgivenessScreen.kt:63`) — y lo que
     está arriba de la pila es **el ticket que acaba de empujarse en el paso 2**.
- **El resultado:** el cobrador vuelve al **mismo `ForgivenessScreen`**, sin
  ticket, y con el saldo **viejo**: `ForgivenessScreen.kt:44-46` es un
  `LaunchedEffect(saleId) { viewModel.loadSaleDetails(saleId) }` cuya llave no
  cambió, así que **no recarga**. **La condonación se guardó y la pantalla se ve
  exactamente igual que antes de confirmar.** No hay ticket, no hay saldo nuevo,
  no hay mensaje: no hay ninguna señal de que pasó algo.
- **Contrasta con el abono nuevo, que hace justo lo contrario:**
  `DestinosDeCobranzaGraph.kt:227-231` —
  `navController.navigate(Screen.PaymentTicket.createRoute(pagoId)) { popUpTo(PagosRutas.REGISTRAR_ABONO) { inclusive = true } }`.
  Ahí el `popUpTo` saca **la pantalla de captura**, no el ticket. Es la misma
  intención —"no dejes la captura en la pila"— resuelta bien en un lado y al
  revés en el otro.
- **Por qué es dinero:** sin confirmación, el cobrador reintenta. Cada reintento
  es una fila nueva (E-APP-023(a)) y una resta nueva (E-APP-022). **E-APP-029 lo
  midió: cuatro veces, separadas por el tiempo exacto de volver a teclear.**
- **Honestidad sobre el nivel:** el encadenamiento `navigate` → `popBackStack`
  está **LEÍDO**, no ejecutado. Lo que lo sostiene es la medición de E-APP-029,
  que es compatible con él y difícil de explicar sin él. **Qué lo volvería
  MEDIDO:** una prueba de navegación que confirme y afirme que la pila queda en
  `Screen.Forgiveness`; o abrir el aparato y condonar una vez.
- **Sólo aplica a la puerta nueva.** Por la puerta legada
  (`SaleActionsSection.kt:64`) `onDismissRequest` es
  `{ openForgivenessDialog = false }` (`:66`), que **no toca la pila**: ahí el
  ticket sí se queda. Son **dos puertas** con comportamientos distintos, y el
  comentario del archivo dice que hay una (**E-APP-044**).
- **Guardada por:** — (sin prueba; ver E-APP-043, el diálogo no tiene ninguna)
- **Caduca si:** el diálogo deja de navegar y descartar en la misma corrutina, o
  `ForgivenessScreen` recarga la venta

### E-APP-032 · 61 pagos con `GUARDADO_EN_MICROSIP = 1` y `DOCTO_CC_ID = 0` en un solo teléfono, $16,520

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24 16:41:56 UTC, mismo teléfono y misma lectura que
  E-APP-029
- **Salida:** **61 filas**, **$16,520** en total. El desglose por día:
  - **58 del incidente de agosto** — **26** el 2026-08-11, **26** el 08-12 y
    **6** el 08-13;
  - **3 de ayer** — las tres condonaciones de E-APP-029.
- **Detalle de formato, y sirve para distinguirlos:** las filas de agosto traen
  **milisegundos** en la marca de tiempo; las de ayer **no**. Dos generaciones de
  captura conviviendo en la misma tabla.
- **Esto convierte E-APP-019 en MEDIDO por sus efectos.** Esa ficha pedía
  exactamente este conteo (*"contar en un Room de campo las filas con
  `GUARDADO_EN_MICROSIP = 1 AND DOCTO_CC_ID = 0` y cuánto restan"*). Son
  **$16,520 que `sumImporteNoReconocidoPorElServidor` resta del saldo mostrado**
  en ese teléfono — salvo las filas que otra fila nombre por
  `PAGO_RECIBIDO_ID`, que la consulta excluye (`PaymentDao.kt:864-877`). **Cuánto
  de los $16,520 sobrevive a esa exclusión no se midió.**
- **Enlaza:** la nota de memoria *"Pago marcado enviado SIN llegar
  (2026-08-31)"* y *"11 pagos nunca llegaron a Microsip (2026-08-17)"*. Las
  fechas de agosto (11, 12, 13) **no** son las de ese incidente: son anteriores.
  **Si son el mismo evento o uno distinto, no se determinó.**
- **Límite honesto:** es **un** teléfono. No dice nada de la flota.
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (es un censo)
- **Caduca si:** se depuran esas filas o se cierra el camino que las produce

### E-APP-033 · No es un patrón repetido: un solo cargo con más de una condonación

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24 16:41:56 UTC, mismo teléfono y misma lectura que
  E-APP-029
- **Cómo y salida:** `GROUP BY DOCTO_CC_ACR_ID HAVING COUNT(*) > 1` sobre las
  filas de condonación → **un solo cargo**, el `14417151`.
- **Control positivo — y aquí es indispensable, porque la afirmación es una
  ausencia:** esa misma consulta **devolvió** el cargo conocido con sus
  **cuatro** filas. La consulta habría encontrado un segundo caso si existiera.
- **Límite honesto, y es el mismo de E-APP-025:** es un teléfono y es **el
  histórico que ese teléfono todavía tiene**. La tabla `Payment` se poda por una
  sola vía —el borrado por ausencia de E-APP-001/E-APP-016—, así que un caso
  viejo pudo haber desaparecido del aparato sin dejar rastro. **"Uno
  encontrado" es cota inferior, no el universo.**
- **Consecuencia:** el defecto de E-APP-031 es real y **raro**, no sistemático.
  Eso cambia la urgencia, no la corrección.
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (es un censo)
- **Caduca si:** aparece un segundo cargo, o se lee otro teléfono

### E-APP-034 · El conjunto a borrar SÍ alcanza el tamaño peligroso: 5,522 pagos en una zona

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24 16:41:56 UTC, mismo teléfono y misma lectura que
  E-APP-029
- **Salida:** **5,627 pagos** en la tabla `Payment`, de los cuales **5,522** en
  la zona **21551** — que es la que `getActiveIDsByZona` enumera entera, sin
  ventana (E-APP-001, `PaymentDao.kt:718-721`).
- **La cuenta que importa:** 5,522 ÷ 900 (`SQLITE_MAX_IN_PARAMS`) ≈ **6,1**.
  Es **seis veces** el tope. La pregunta abierta de la compuerta del 2026-09-23
  (*"¿el volumen de fantasmas por corrida pasa de ~900?"*) tiene respuesta del
  lado del **conjunto local**: un `/ids` que devuelva vacío —o truncado—
  clasifica **5,522 ids como fantasmas** y se los manda a un solo `IN (...)`.
- **Lo que esto NO mide:** cuántos **fantasmas** produce una corrida real. Mide
  la **cota superior** del conjunto, que es lo que decide si el desbordamiento
  es alcanzable. Es la mitad de la pregunta; la otra mitad sigue sin medir.
- **Por qué aun así el defecto es latente:** el tope de 999 sólo existe por
  debajo de API 31, y hoy no hay aparatos ahí (**E-APP-036**).
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (es un censo)
- **Caduca si:** se acota la enumeración local, o se trocean los tres borrados

### E-APP-035 · El reporte de cobranza va holgado HOY: 142 cargos contra el tope de 900

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24 16:41:56 UTC, mismo teléfono y misma lectura que
  E-APP-029
- **Salida:** **142 cargos distintos** en la semana del reporte; **155** con el
  corte ampliado. Contra `SQLITE_MAX_IN_PARAMS = 900`, holgado por un factor de
  ~6.
- **Qué acota, exactamente:** el tamaño de los dos `IN (...)` **sin trocear** de
  `:feature:collectionReport` (**E-APP-042**). Hoy no revientan porque el
  conjunto es chico, no porque alguien lo haya acotado.
- **Es un margen de HOY, no una propiedad del diseño.** Nada en el código limita
  el número de cargos de una semana. Un cobrador con más ruta, una semana de
  cierre o un corte más ancho lo mueven, y el tope no se mueve con él.
- **Contrasta con E-APP-034:** el mismo teléfono, el mismo tope, dos conjuntos
  — uno seis veces por encima y otro seis veces por debajo. Por eso los dos
  hechos van en fichas separadas: **medir uno no dice nada del otro**.
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (es un censo)
- **Caduca si:** crece la ruta, se amplía el corte, o se trocean los dos `IN`

### E-APP-036 · No hay teléfonos por debajo de API 31 en servicio — el tope de 999 es latente, no vigente

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, barrido **exhaustivo** del tramo `sdk ≤ 30` sobre los
  aparatos que reportan al backend
- **Salida:** **un solo aparato** en todo el tramo, con **último rastro el
  2026-03-12**, y ese cobrador **ya cambió de teléfono**. Cero aparatos
  `sdk ≤ 30` activos hoy.
- **Control positivo — y es obligatorio, porque esto es una ausencia:** el
  barrido fue **exhaustivo sobre el tramo**, no una muestra: recorre cada nivel
  de sdk hasta 30 y **sí devolvió** el aparato de marzo. La consulta habría
  encontrado a los demás si existieran. *(Lo que **no** se enseñó por escrito es
  la salida cruda por nivel de sdk; la afirmación descansa en el informe de esa
  corrida.)*
- **Lo que esto hace con E-APP-010:** lo baja de **defecto activo** a **defecto
  latente**. El tope de `SQLITE_MAX_VARIABLE_NUMBER = 999` vive por debajo de
  API 31 (`SqliteInParams.kt:6-7`), y hoy nadie está ahí.
- **Y lo que NO hace:** cerrarlo. `app/build.gradle.kts:79` sigue en
  `minSdk = 24`, así que **la app acepta que vuelva a entrar uno** — un teléfono
  de repuesto, un aparato viejo de la bodega, un cobrador nuevo. El día que
  entre, el defecto se reactiva en silencio y con 5,522 filas esperándolo
  (E-APP-034). Subir `minSdk` a 31 lo cerraría de verdad; trocear también.
- **Procedencia:** la salida cruda no está en el árbol.
- **Guardada por:** — (es un censo de flota)
- **Caduca si:** entra un aparato `sdk < 31`, o sube `minSdk`

### E-APP-037 · No existe telemetría del borrado de fantasmas, y la que existe no sale del teléfono

- **Nivel:** MEDIDO (los dos extremos) · **Estado:** VIGENTE
- **Medido:** 2026-09-24. El lado del código, leyendo
  `CobranzaReconciler.kt`; el lado del campo, en la lectura de E-APP-029.
- **El hueco, LEÍDO:** `CobranzaReconciler.kt:308-310` —el borrado de pagos
  fantasma— anuncia con **`Log.i` y nada más**. Logcat es volátil: se pierde al
  reiniciar y nunca sale del aparato. Lo mismo `:373-374` para los saldos.
  **Después del hecho no hay forma de saber si una corrida borró tres filas o
  tres mil**, que es justo el dato que la compuerta del 2026-09-23 pidió para
  poder poner una cota (E-APP-014).
- **Control positivo, y es el que convierte esto en hallazgo:** en **el mismo
  archivo**, `collapseUuidTwins` (`:197`) y `collapseLegacyTwins` (`:279`)
  **sí** persisten vía `Logger.get().info(module = "COBRANZA", action =
  "COLLAPSE_TWIN", ...)` → Firestore. Y **ese teléfono emitió 9 filas** de esa
  telemetría los días 23 y 24 (MEDIDO en la lectura de E-APP-029). O sea: el
  canal **existe, funciona y está probado en campo** — el borrado de fantasmas
  simplemente no lo usa. No es que no se pueda; es que no se hizo.
- **Segundo hueco, LEÍDO — y es peor, porque parece que sí hay telemetría:**
  el único destino de `CobranzaSyncTelemetry` es un doble que **registra y
  devuelve**, cuyo `drain()` el llamador toma por **entrega confirmada** y
  después **purga**. **Nada de esa telemetría sale jamás del teléfono.** Una
  instrumentación que se ve viva en el código y muere en memoria es peor que no
  tenerla: invita a creer que el dato existe.
  *(Nivel honesto: esto se registra como **LEÍDO del informe de análisis**. La
  cadena `CobranzaSyncTelemetry` → doble → `drain()` → purga **no se reverificó
  línea por línea aquí**; lo que sí se verificó es que `E-APP-014` ya había
  medido que los dos únicos aciertos de `error(` en el paquete son
  `CobranzaSyncTelemetry.kt:137` y `:319`, el segundo **un override no-op**.
  **Qué lo volvería MEDIDO/LEÍDO completo:** abrir `CobranzaSyncTelemetry.kt` y
  citar el doble y su `drain()` por línea.)*
- **Consecuencia:** E-APP-014 sigue sin poder cerrarse. Una cota contra el
  borrado masivo necesita saber qué es normal, y hoy nadie lo sabe.
- **Guardada por:** — (por definición, nada)
- **Caduca si:** el borrado de fantasmas emite por `Logger.get()`, o
  `CobranzaSyncTelemetry` gana un destino real

### E-APP-038 · El teléfono corría 2.18.1 el 23-sep, y 2.18.1 **SÍ** contiene el arreglo del doble descuento — que no toca la condonación

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24. La versión, en la lectura del aparato (tabla
  `app_logs` del mismo Room); la genealogía, con `git` sobre el checkout
  principal de `msp-app-kt`.
- **La versión, fechada:** el teléfono corría **2.18.1 a las 15:25 del
  2026-09-23** —o sea **antes** de las cuatro condonaciones de las 15:27— y la
  instalación fue **~1 h antes**. Esto **cierra el hueco que E-APP-020 dejó
  abierto** (*"el endpoint reporta 2.18.1 pero sin fecha del reporte, así que no
  prueba qué corría en ese momento"*): ahora sí está fechado.
- **La genealogía, MEDIDA aquí:**

  ```
  $ git merge-base --is-ancestor c28cdb36 15ae6cfb ; echo $?
  0
  $ git show -s --format='%H %ad %s' --date=iso 15ae6cfb
  15ae6cfb… 2026-09-23 13:47:51 -0600 chore(release): 2.18.1 (62)
  ```

  `c28cdb36` es *"fix(cobranza): el saldo deja de descontar dos veces el pago
  recién cobrado"*, 2026-09-23 13:14:58. **La 2.18.1 lo contiene.**
- **Control positivo y control negativo:** `--is-ancestor 130e4089 15ae6cfb` →
  **0** (afirma cuando debe) y `--is-ancestor 15ae6cfb c28cdb36` → **1** (niega
  cuando debe). El comando discrimina en las dos direcciones.
- **Y ahora lo que importa — ese arreglo NO toca la condonación.**
  `git show --stat c28cdb36` da **seis archivos**: `CobranzaReconciler.kt`,
  `CobranzaSyncManager.kt`, `SqliteInParams.kt` (nuevo), `VentaDto.kt`,
  `CobranzaSyncManagerTest.kt` y `PaymentDao.kt`. **No aparece
  `NewForgivenessDialog.kt` ni `SaleDao.kt`.** Es decir: el teléfono corría la
  versión **con** el arreglo del doble descuento y aun así produjo E-APP-029 y
  E-APP-030 — porque ese arreglo cubre el camino del **sync**, no el de la
  **captura**.
- **Contra qué previene:** contra leer "ya está arreglado, trae la 2.18.1" y
  cerrar el caso. La 2.18.1 arregla otra cosa.
- **No contradice a E-APP-028**, la complementa: aquélla midió que **`v2.18.0`
  NO** lo contiene; ésta, que **`15ae6cfb` / 2.18.1 SÍ**. Y la 2.18.1 sigue
  **sin etiquetar**, así que "2.18.1" se identifica por commit, no por tag.
- **Guardada por:** — (es estado del repositorio y una lectura de campo)
- **Caduca si:** se etiqueta la 2.18.1, o se publica una versión posterior

### E-APP-039 · Una prueba verde que finge guardar el troceo y no guarda nada

- **Nivel:** LEÍDO (la cadena) · **Estado:** VIGENTE
- **Leído:** 2026-09-24, rama `feat/notas-visibles` (`d88dfb8c`)
- **La prueba:**
  `app/src/test/.../core/sync/pendingwork/data/visits/RoomPendingVisitsStoreTest.kt:72-82`
  — `` `un lote mayor al tope de parametros de SQLite se trocea y marca todo` ``,
  con `val many = List(1500) { "v-$it" }` (`:76`) y un comentario (`:74-75`) que
  asegura: *"1500 > SQLITE_MAX_VARIABLE_NUMBER (999): sin trocear, SQLite lanza
  'too many SQL variables' — el error silencioso que CLAUDE.md registra"*.
- **Por qué no guarda nada — la cadena de herencia, verificada:**
  - `RoomPendingVisitsStoreTest.kt:17` — `class RoomPendingVisitsStoreTest : RoomTestBase()`
  - `core/testing/.../RoomTestBase.kt:9` — `abstract class RoomTestBase : RobolectricTestBase()`
  - `core/testing/.../RobolectricTestBase.kt:8-9` — `@RunWith(RobolectricTestRunner::class)`
    y **`@Config(sdk = [33], ...)`**.

  **En sdk 33 el tope de 999 no aplica** —es de API < 31, `SqliteInParams.kt:6-7`—
  y encima Robolectric corre sobre el SQLite del escritorio, cuyo tope es de
  decenas de miles (`SqliteInParams.kt:16-19`). **Dos razones independientes,
  cada una suficiente.**
- **La consecuencia — INFERIDO, y se puede medir en un minuto:** **pasaría
  igual sin el `.chunked()`**. La prueba afirma un comportamiento que su entorno
  no puede distinguir de su ausencia. **Qué lo volvería MEDIDO:** quitar el
  `.chunked(...)` del store, correr la prueba y enseñar que sigue **verde**. Es
  la comprobación de reversión que `registro-de-evidencia.md` §4.5 exige y que
  aquí nadie hizo.
- **Y la red instrumentada tampoco la cubre:**
  `.github/workflows/instrumented-tests.yml:73` corre **`api-level: [34]`**, un
  solo nivel — mientras su **propio comentario**, `:67-72`, reconoce que
  *"`minSdk` es 24 y `targetSdk` 35, asi que lo honesto seria una matriz
  24/35"*, y explica que se arranca con 34 *"y se amplia cuando el job tenga
  historial de estabilidad"*. La decisión está razonada y es defendible; el
  punto es que **mientras siga en 34, ningún troceo está guardado por nada**.
- **Guardada por:** — es la ficha de una prueba que no guarda
- **Caduca si:** la matriz gana un nivel por debajo de 31, o la prueba se mueve
  a `androidTest`

### E-APP-040 · «Ninguna prueba puede detectar que falta el troceo» — falso como está redactado

- **Nivel:** LEÍDO · **Estado:** VIGENTE (es una **corrección a un comentario
  del código**)
- **Leído:** 2026-09-24
- **El comentario dice** (literal, `SqliteInParams.kt:16-19`):

  > `**Ninguna prueba puede detectar que falta el troceo**: Robolectric corre`
  > `sobre el SQLite de escritorio, cuyo tope es de decenas de miles. Por eso el`
  > `porqué vive aquí, en el código, que es la única red que queda.`

- **Qué es cierto y qué no.** La **premisa** es correcta: bajo Robolectric no se
  detecta. La **conclusión** no se sigue: *"ninguna prueba"* incluye las
  **instrumentadas**, que corren en un emulador con el SQLite de Android de
  verdad. Añadir **`24`** o **`30`** a la matriz de
  `.github/workflows/instrumented-tests.yml:73` —hoy `[34]`— vuelve el
  desbordamiento **reproducible y detectable**. La frase honesta no es *"no se
  puede"*: es **"nadie lo intentó"**.
- **Por qué importa, y no es pedantería:** ese comentario es el argumento con el
  que el porqué del troceo se dejó **sólo en el código** —*"la única red que
  queda"*—, y es el que E-APP-010 citó para explicar por qué los tres borrados
  no tienen prueba. Una afirmación de imposibilidad **cierra la búsqueda**. Es
  exactamente el patrón que `registro-de-evidencia.md` §1 describe: un
  comentario que justificó un diseño y que nadie verificó porque parecía ya
  verificado.
- **Contradice:** el propio comentario de `.github/workflows/instrumented-tests.yml:67-72`,
  que **en el mismo repositorio** dice que lo honesto sería una matriz 24/35 —
  o sea, alguien ya sabía que el nivel de API es una variable de prueba.
- **Acción pendiente:** corregir el comentario en sitio y citar E-APP-040 desde
  ahí. Mientras no se haga, la frase sigue viva en su lugar original.
- **Guardada por:** — (sin prueba; ésa es la afirmación)
- **Caduca si:** se corrige el comentario, o la matriz gana un nivel < 31

### E-APP-041 · `.chunked()` a secas es un arreglo INCORRECTO para un `DELETE`

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-24
- **El problema:** hoy cada uno de los tres borrados de E-APP-010 es **una
  sentencia**, y una sentencia de Room lleva su transacción implícita: o borra
  todo o no borra nada. Trocearla la convierte en ***N* sentencias**, y un fallo
  a la mitad —el proceso muere, el disco se llena, la app pasa a segundo plano—
  **deja los trozos anteriores commiteados**. Se cambia un fallo ruidoso y total
  por un borrado parcial y silencioso. En el camino del dinero eso es un
  retroceso, no un arreglo.
- **Y el conteo que se reporta ya miente, hoy:** `reconcilePagosViaIds` devuelve
  `Pair(pagoPhantoms.size, pagosMissing.size)` (`CobranzaReconciler.kt:359`) —
  **el tamaño del conjunto que se pidió borrar, no el que se borró**. Ese número
  sube por `ReconcileOutcome.Ok(pagosPhantomsDeleted = ...)` (`:152-153`) con un
  nombre que promete filas borradas. Troceado sin transacción, la mentira pasa
  de teórica a operativa: el primer trozo revienta y el resultado sigue
  diciendo 5,522.
- **El patrón correcto ya existe en el repositorio, a 400 líneas:**
  `CobranzaSyncManager.kt:797-822` —dentro de `mergePagos`— envuelve **los
  trozos** en `db.withTransaction { ... }`, con
  `doctoCcIds.chunked(SQLITE_MAX_IN_PARAMS).forEach { ... }` (`:807-809`) y
  `uuidTwins.chunked(SQLITE_MAX_IN_PARAMS).forEach { ... }` (`:818-820`)
  **dentro** de esa transacción, y su comentario `:804` lo nombra: *"Un DELETE
  por trozo, no uno por fila."* Es el mismo commit (`1d8ae40b`) que dejó los
  tres borrados del reconciliador sin tocar.
- **Consecuencia para quien vaya a arreglar E-APP-010:** el arreglo es
  `db.withTransaction { ids.chunked(SQLITE_MAX_IN_PARAMS).forEach { ... } }`,
  **sumando lo que cada trozo devuelve**, no `ids.chunked(...).forEach { ... }`
  a secas. Y para `CobranzaReconciler.kt:194` hay un cuidado extra: ese sitio ya
  corre **dentro** de la transacción de `:335` cuando lo llama `:347`, así que
  anidar otra ahí no es lo mismo que abrirla en `:103`.
- **Guardada por:** — (sin prueba; y por E-APP-040, sí se podría tener una)
- **Caduca si:** se trocean los tres borrados con transacción y conteo real

### E-APP-042 · Dos `IN (...)` sin trocear que nadie había mirado, en `:feature:collectionReport`

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-24
- **Dónde — los dos:**
  - `core/database/.../dao/sale/SaleDao.kt:68` —
    `WHERE DOCTO_CC_ACR_ID IN (:acrIds)`, de `getSaleRefsByAcrIds` (`:71`).
    Único llamador:
    `feature/collectionReport/.../data/adapter/RoomPaymentsAdapter.kt:69` —
    `return saleDao.getSaleRefsByAcrIds(acrIds).associateBy { it.saleId }`.
    **Sin `.chunked(...)`.**
  - `core/database/.../dao/ClienteDao.kt:58` —
    `SELECT CLIENTE_ID, NOMBRE FROM cliente WHERE CLIENTE_ID IN (:ids)`, de
    `getNombresByIds` (`:59`). Único llamador:
    `feature/collectionReport/.../data/adapter/RoomVisitsAdapter.kt:43` —
    `return clienteDao.getNombresByIds(ids).associate { ... }`.
    **Sin `.chunked(...)`.**
- **Qué pantalla es:** el **reporte de cobranza**, el que el cobrador imprime al
  cerrar el día. Los dos son consultas anti-N+1 —su propio KDoc lo dice
  (`ClienteDao.kt:53-56`)— y por eso reciben el lote entero de un tirón: el
  arreglo del N+1 es lo que creó el `IN` sin cota.
- **Nota deliberada:** son **lecturas**, no borrados, así que E-APP-041 no
  aplica igual — trocear un `SELECT` y unir los mapas es seguro y no rompe
  ninguna atomicidad. Es el caso fácil.
- **Hoy van holgados:** 142 cargos en la semana (155 con el corte ampliado)
  contra el tope de 900 — **E-APP-035**, MEDIDO. Y el tope sólo muerde por
  debajo de API 31, donde hoy no hay nadie (**E-APP-036**). **Latente por
  partida doble.**
- **Por qué se registra igual:** porque la búsqueda que encontró los tres del
  reconciliador (E-APP-010) no miró este módulo, y "no lo miramos" es un estado
  distinto de "está bien".
- **Guardada por:** — (sin prueba; y por E-APP-039, la que existe para el
  troceo de visitas no guarda nada)
- **Caduca si:** se trocean, o crece la ruta del cobrador

### E-APP-043 · El diálogo de condonación no tiene NINGUNA de las protecciones del abono nuevo — trece comparadas

- **Nivel:** LEÍDO (la comparación) + MEDIDO (el censo de pruebas) ·
  **Estado:** VIGENTE
- **Leído / medido:** 2026-09-24, rama `feat/notas-visibles` (`d88dfb8c`)
- **Amplía E-APP-023**, que comparó **tres**. Son trece.

| # | Protección | Abono nuevo (`:feature:pagos`) | Condonación (`NewForgivenessDialog`) |
|---|---|---|---|
| 1 | Clave idempotente estable | `RegistrarAbonoViewModel.kt:119-120` — `savedStateHandle.get(CLAVE_ABONO_ID) ?: UUID.randomUUID()...also { savedStateHandle[...] = it }` | `:111` — `UUID.randomUUID()` **dentro** de cada confirmación |
| 2 | Guarda anti-doble-toque **síncrona** | `:660-664` — `if (yaSeEncolo \|\| actual.guardando \|\| ...) return`; `yaSeEncolo = true` antes del `launch` | ninguna: `:271-274` llama directo |
| 3 | Guarda que **sobrevive al proceso** | `:122-126` — `yaSeEncolo` sobre `SavedStateHandle` | ninguna |
| 4 | Revalidación contra el saldo **vigente** | `:686-688` `puedeEscribirse`, releído al confirmar | valida contra una **foto** (`ForgivenessScreen.kt:44-46`) |
| 5 | Segundo cinturón (caso de uso) | `RegistrarAbono.kt:50-61` — `bloqueosDe` otra vez, y reporta si la pantalla dejó pasar algo | ninguno |
| 6 | Tercer cinturón (al escribir) | `RegistroDeAbonoAdapter.kt:344-356` `sobrepasaElSaldo`, contra el `SALDO_REST` real | ninguno |
| 7 | Bloqueo **"venta sin saldo"** | `SeguridadDelAbono.kt:160` — `if (saldo <= Money.ZERO) add(VENTA_SIN_SALDO)`, con mensaje propio | no existe: el saldo ≤ 0 se manifiesta como dos errores sobre el **monto** (E-APP-024) |
| 8 | Aritmética con `Money` | `bloqueosDe(monto: Money, saldo: Money)` (`:154`) | `Double`: `inputValue.toDoubleOrNull()`, `sale.SALDO_REST` (`:147-152`) |
| 9 | Transacción **real** | `RegistroDeAbonoAdapter.kt:204` — `db.withTransaction { ... }` | `@Transaction` **inerte** (E-APP-045) |
| 10 | `NonCancellable` | `RegistroDeAbonoAdapter.kt:202` — `withContext(NonCancellable) { ... }` | ninguno: corre en el `coroutineScope` del diálogo, que muere al descartarlo |
| 11 | Encolado **incondicional** tras el commit | `:221` `encolarSubida(abono.abonoId)`, con su propio `try` (`:240-245`) que nunca tumba el dinero ya escrito | ninguno en el camino de escritura (**E-APP-046**) |
| 12 | Telemetría con código grepeable | `PagosTelemetria.CODE_ABONO_BLOQUEADO_EN_APLICACION` / `..._EN_ESCRITURA` | ninguna |
| 13 | Pruebas | diez y pico archivos (ver el censo) | ninguna que ejercite el diálogo |

- **El censo de pruebas — MEDIDO:** `git ls-tree -r --name-only main | grep -i forgive`
  → **tres archivos**, `NewForgivenessDialog.kt`, `ForgivenessScreen.kt` y
  `feature/collectionReport/.../domain/model/Forgiveness.kt`. **Ninguno en
  `src/test/` ni `src/androidTest/`.**
- **Control positivo — y hubo que reforzarlo:** el control que traía el informe
  (*"el mismo patrón sí encuentra `feature/collectionReport/.../Forgiveness.kt`"*)
  prueba que el `grep` casa, **no** que el listado contenga archivos de prueba.
  El control fuerte es
  `git ls-tree -r --name-only main | grep -iE 'src/(test|androidTest).*[Aa]bono'`
  → **diez archivos** (`RegistrarAbonoTest.kt`, `SeguridadDelAbonoTest.kt`,
  `RegistroDeAbonoAdapterTest.kt`, `AbonoMatrixScreenshotTest.kt`, …). El
  listado **sí** trae pruebas cuando existen; para la condonación no trae
  ninguna.
- **PRECISIÓN, y baja el filo de "cero pruebas":** sí hay pruebas que **nombran**
  `NewForgivenessDialog` —`RetiroDelDialogoDePagoTest.kt:85`,
  `RetiroDelDialogoDeVisitaTest.kt:76`, `SaleIdSpacesCallSitesTest.kt:75`— pero
  lo usan como **control positivo de un escáner de fuentes** (*"el símbolo
  hermano que sí existe"*, `RetiroDelDialogoDeVisitaTest.kt:26`), no ejercitan
  su comportamiento. Y `PaymentTimestampTest.kt` cubre el helper de la marca de
  tiempo, no el diálogo. **La afirmación exacta es: cero pruebas del
  comportamiento del diálogo de condonación.** MEDIDO por
  `grep -rn 'NewForgivenessDialog\|ForgivenessScreen' --include='*.kt' .`, cuyos
  aciertos en `src/test/` son todos comentarios o cadenas de escáner.
- **Por qué es dinero:** el abono y la condonación **escriben la misma tabla por
  el mismo `PaymentFactory`** y bajan el mismo `SALDO_REST`. Trece protecciones
  de diferencia sobre la misma escritura.
- **Guardada por:** — nada
- **Caduca si:** el diálogo se migra a `:feature:pagos`, o gana cualquiera de las
  trece

### E-APP-044 · Un comentario que miente, y es grave: «este bloque sigue siendo su única puerta»

- **Nivel:** LEÍDO · **Estado:** VIGENTE (es una **corrección a un comentario
  del código**)
- **Leído:** 2026-09-24
- **El comentario dice** (literal,
  `app/.../features/sales/components/saleactionssection/SaleActionsSection.kt:57-58`):

  > `**La condonación no se toca**: sigue siendo el mismo `NewForgivenessDialog``
  > `con la misma lógica, y este bloque sigue siendo su única puerta.`

- **Es falso, y el propio repositorio lo desmiente en tres sitios:**
  - `DestinosDeCobranzaGraph.kt:171` — `navController.navigate(Screen.Forgiveness.createRoute(ventaId))`,
    bajo un comentario (`:165-169`) que dice literal *"Puerta **NUEVA** (el dueño
    la pidió en el detalle de venta nuevo, no sólo en el legado)"*.
  - `DestinosDeCobranzaGraph.kt:253` — `destinoDeCondonacion(navController)`, y
    su registro en `:272-281`, que monta `ForgivenessScreen` → el mismo diálogo.
  - `SaleActionsSection.kt:64` — la puerta legada, la que el comentario describe.

  **Son dos puertas vivas**, y no se comportan igual: por la nueva,
  `onDismissRequest` es `popBackStack()` (`ForgivenessScreen.kt:63`) y se lleva
  el ticket por delante (**E-APP-031**); por la legada es
  `{ openForgivenessDialog = false }` (`SaleActionsSection.kt:66`) y el ticket se
  queda. **El mismo botón hace dos cosas distintas según por dónde se entró.**
- **Y la parte grave, que es la que hay que leer dos veces:** el **mismo
  comentario**, líneas **`:43-50`**, enumera los tres defectos por los que se
  **retiró** el `NewPaymentDialog` legado —
  (a) *"su clave de idempotencia era un `remember { UUID.randomUUID() }` —muere
  al rotar o al morir el proceso, y el reintento cobra dos veces—"*,
  (b) *"su 'atomicidad' era un `@Transaction` inerte fuera de un `@Dao`"*,
  (c) *"sus errores morían en un `printStackTrace()`"*—
  y **los tres siguen vivos en el diálogo de condonación**, que el mismo párrafo
  declara intacto. Verificado uno por uno:
  - **(a) — vivo y peor.** No es *literalmente* un `remember { UUID.randomUUID() }`:
    es `UUID.randomUUID()` **dentro** de `handleSaveForgiveness()`
    (`NewForgivenessDialog.kt:111`), o sea un id nuevo **por cada toque**, no uno
    por instancia de pantalla. Más frágil que el que se retiró. *(Esta es la
    única de las tres donde la palabra "literal" no aplica; se anota para no
    inflar la afirmación.)*
  - **(b) — vivo y literal.** `savePayment` → `PaymentsLocalDataSource.saveAndEnqueue`
    (`:168-175`) → `insertPaymentAndUpdateSale` (`:158-167`), cuyo `@Transaction`
    de `:158` **no genera nada** porque la clase no es un `@Dao` (**E-APP-045**).
    Es el mismo método que el KDoc de `RetiroDelDialogoDePagoTest.kt:21-25` cita
    por su nombre como el defecto de atomicidad del diálogo retirado.
  - **(c) — vivo, y la variante es peor que un `printStackTrace()`.**
    `PaymentsViewModel.savePayment` (`:455-473`) sí atrapa y escribe
    `_savePaymentState.value = ResultState.Error(...)` (`:470`) — pero
    **`NewForgivenessDialog` nunca lee `savePaymentState`** (MEDIDO:
    `grep -n 'savePaymentState' NewForgivenessDialog.kt` → **cero aciertos**;
    control positivo: el mismo grep sí encuentra `paymentsViewModel` en `:78`,
    `:126` y `:140`). Un `printStackTrace()` al menos deja una línea en logcat.
    Esto no deja nada, y la pantalla **sigue adelante igual**.
- **Por qué importa:** ese párrafo es el argumento con el que la condonación
  quedó fuera de la migración. Dice *"no se toca"* apoyándose en que es la única
  puerta, y describe con precisión quirúrgica tres defectos que no vio que
  tenía enfrente. Es el caso de `registro-de-evidencia.md` §1 en su forma más
  pura: **un comentario correcto sobre lo que arregló y falso sobre lo que
  quedó.**
- **Acción pendiente:** corregir el comentario en sitio y citar E-APP-044.
- **Guardada por:** — (sin prueba)
- **Caduca si:** se cierra una de las dos puertas, o se corrige el comentario

### E-APP-045 · `PaymentsLocalDataSource` no es un `@Dao`, así que su `@Transaction` no genera nada

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-24
- **Dónde:**
  `app/.../data/local/datasource/payment/PaymentsLocalDataSource.kt:16` —
  `class PaymentsLocalDataSource @Inject constructor(private val paymentDao: PaymentDao, private val saleDao: SaleDao)`.
  **Una clase ordinaria**: no lleva `@Dao`, no es `abstract`, no es una
  `interface`. Importa `androidx.room.Transaction` (`:4`) y la usa en `:158`,
  sobre `insertPaymentAndUpdateSale` (`:159-167`).
- **Qué implica:** `@Transaction` es una anotación que **el procesador de Room
  lee al generar la implementación de un `@Dao`**. Fuera de un `@Dao` no hay
  nada que generar: la anotación queda de adorno. Las dos escrituras de
  `:165-166` —`paymentDao.savePayment(payment)` y
  `saleDao.updateTotal(saleId, newAmount, newEstadoCobranza)`— corren **sueltas**.
  Un fallo entre las dos deja **el pago contado con el saldo intacto**, o al
  revés.
- **El repositorio ya lo sabía, por escrito, en dos sitios:**
  `SaleActionsSection.kt:47-48` y `RetiroDelDialogoDePagoTest.kt:21-25`, los dos
  nombrando `PaymentsLocalDataSource.saveAndEnqueue` y el *"`@Transaction`
  **inerte fuera de un `@Dao`**"*. Se documentó como razón para **retirar** el
  diálogo de pago legado — y se dejó en pie para la condonación, que pasa por el
  mismo método.
- **El abono nuevo lo resuelve, y ahí sí es una transacción de verdad:**
  `RegistroDeAbonoAdapter.kt:204` — `db.withTransaction { pagos.saveAndEnqueue(...) }`,
  bajo `withContext(NonCancellable)` (`:202`) y con el comentario `:203`:
  *"El insert del pago y el descuento del saldo, o los dos o ninguno."* El mismo
  método, envuelto por fuera.
- **Contraste que vale la pena:** `SaleDao.kt:350-354` **sí** es un `@Transaction`
  que funciona (`replaceAll`), y su KDoc `:341-348` explica por qué. La
  anotación no está mal usada en el repositorio: está mal usada **ahí**.
- **Guardada por:** `app/src/test/.../PaymentsLocalDataSourceTest.kt:296`
  (`saveAndEnqueue_insertsPaymentAndDecrementsSaldo`) fija el **camino feliz**
  — que las dos escrituras ocurren. **No** fija la atomicidad: para eso haría
  falta fallar la segunda y comprobar que la primera se deshizo, y eso **no se
  puede** con la implementación actual, porque no hay transacción que deshacer.
- **Caduca si:** `insertPaymentAndUpdateSale` se envuelve en `db.withTransaction`,
  o se mueve a un `@Dao`

### E-APP-046 · `saveAndEnqueue` **no encola**, y el único encolado de la condonación es un servicio en primer plano

- **Nivel:** LEÍDO · **Estado:** VIGENTE · *(hallazgo del 2026-09-24, no venía
  en el encargo)*
- **Leído:** 2026-09-24
- **El nombre miente:**
  `PaymentsLocalDataSource.saveAndEnqueue` (`:168-175`) tiene **un solo
  cuerpo**: `insertPaymentAndUpdateSale(payment, saleId, newAmount, newEstadoCobranza)`.
  **No encola nada.** El `AndEnqueue` es historia fósil de cuando sí lo hacía.
- **Quién sí encola en el camino de la condonación:** nadie en la corrutina de
  la escritura. El único encolado es el **efecto secundario del servicio de
  ubicación**: `NewForgivenessDialog.kt:128-131` arranca
  `UpdateLocationService` con `payment_id` en el `Intent`, y de ahí
  `UpdateLocationHandler.handle` llama `enqueuePayment(paymentId)`.
- **Y el código lo dice, con todas sus letras** —el KDoc de
  `UpdateLocationHandler.kt`, en la rama `paymentId != null`:

  > `Desde el Arreglo C RegistroDeAbonoAdapter ya encola en la misma corrutina`
  > `de la escritura, asi que este encolado es la segunda red (y el unico del`
  > `camino de la condonacion).`

  El mismo archivo explica, para las **visitas**, por qué eso era un defecto y
  se arregló: *"If this `Service` never ran (permission denied, Play Services
  down, foreground-service start refused), the visit was written to Room and
  never enqueued."* **La lección llegó a las visitas y al abono; a la
  condonación no.**
- **Por qué es dinero — INFERIDO:** una condonación escrita con el servicio de
  ubicación caído (permiso denegado, Play Services, restricción de arranque en
  primer plano, batería) queda en Room **sin encolar**. No se pierde para
  siempre —`PaymentsPendingSynchronizer` recoge en el siguiente login lo que
  tenga `GUARDADO_EN_MICROSIP = 0`—, pero **se retrasa hasta el siguiente login**
  y el cobrador no se entera. **Qué lo volvería MEDIDO:** negar el permiso de
  ubicación en un aparato, condonar, y mirar si hay trabajo encolado.
- **No explica E-APP-029.** Las tres filas que no llegaron tienen
  `GUARDADO_EN_MICROSIP = 1`, o sea **sí** se subieron. Este defecto es otro, y
  se registra aparte para que no se confundan.
- **Guardada por:** — (sin prueba que fije el encolado de la condonación)
- **Caduca si:** la condonación encola en su propia corrutina, o
  `saveAndEnqueue` recupera su nombre

### E-APP-047 · El diálogo de condonación no espera a que la escritura termine

- **Nivel:** LEÍDO · **Estado:** VIGENTE · *(hallazgo del 2026-09-24, no venía
  en el encargo)*
- **Leído:** 2026-09-24
- **Dónde:** `NewForgivenessDialog.kt:126` — `paymentsViewModel.savePayment(forgiveness)`.
  `PaymentsViewModel.savePayment` (`:455-473`) **no es `suspend`**: abre su
  propio `viewModelScope.launch { ... }` (`:456`) y **retorna inmediatamente**.
- **Consecuencia:** las líneas `:128` a `:140` —arrancar el servicio de
  ubicación, navegar al ticket, limpiar el campo, cerrar el diálogo, recargar el
  historial— corren **sin saber si el pago se escribió**. El diálogo no espera,
  no comprueba y, por **E-APP-044(c)**, tampoco observa el error.
- **Por qué importa sumado a lo demás:** es la tercera pata del mismo banco.
  El cobrador no ve confirmación (E-APP-031), la app no le dice si falló
  (E-APP-044c) y la escritura ni siquiera había terminado cuando la pantalla
  volvió a su sitio. Reintentar es la única conducta razonable que le queda.
- **Contrasta con el abono nuevo:** `RegistroDeAbonoAdapter` escribe **dentro**
  de `withContext(NonCancellable) { db.withTransaction { ... } }` (`:202-211`) y
  la pantalla sólo pasa a `registrado` cuando eso volvió.
- **Guardada por:** — (sin prueba)
- **Caduca si:** `savePayment` se vuelve `suspend`, o el diálogo espera su
  resultado antes de navegar

---

### E-APP-048 · Desde 2.18.0 el saldo del teléfono resta otra vez abonos que el servidor ya aplicó: las capturas de la era Node

- **Nivel:** MEDIDO · **Estado:** CERRADA en rama (`msp-app-kt` `dedee4a7`, sin publicar)
- **Medido:** 2026-09-30, teléfono de `juan.martinez@msp.com` (app 2.19.2) por el
  depurador remoto, sólo `SELECT`, con canario `COUNT(*)`; producción
  (`MUEBLERA_SNP`) por `isql` en transacción de sólo lectura.
- **El caso:** Q00002413 (cargo 14186779). Servidor: `SALDO` 3,800 = 12,800 −
  8,500 − 500, cuadra con sus 35 abonos (control). Room: `SALDO_REST` 3,550 =
  base 3,800 − 250. La única fila que resta es `cfd7bc17-…` (11-ago, $250,
  `GUARDADO_EN_MICROSIP = 1`, `DOCTO_CC_ID = 0`, nadie la nombra). Los abonos del
  22 y del 29-sep están anotados y nombrados (control: no restan).
- **En producción** `cfd7bc17` existe en `MSP_PAGOS_RECIBIDOS` con
  `DOCTO_CC_ID = 15866668`, `ESTADO = A` e **`IMPTE_DOCTO_CC_ID` NULL**; el
  documento tiene un solo abono vivo, mismo cargo y monto. La subconsulta del sync
  (`pagos_repo.go:276`) da `pago_recibido_id` NULL para 15866669 (control:
  15893809 sí devuelve su UUID).
- **Por qué no se colapsa (LEÍDO):** el colapso UUID exige `PAGO_RECIBIDO_ID`
  (`PaymentDao.findCollapsibleUuidTwins`); el legacy exige `DOCTO_CC_ID > 0`
  (`deleteLegacyTwinsByDoctoCcIds`).
- **Cuándo empezó (MEDIDO, `app_logs`):** el ticket bueno del 22-sep (16:01) salió
  con 2.17.3; Juan instaló 2.18.0 a las 20:08. La resta entró en `c38c7e36`
  (v2.18.0) y `999a5f93` (v2.18.1) le agregó `DOCTO_CC_ID = 0`; las dos restan esta
  fila. Control: `git grep sumImporteNoReconocidoPorElServidor` da 0 en v2.17.3.
- **Producción, universo (MEDIDO 2026-10-01):** 514,296 capturas con
  `IMPTE_DOCTO_CC_ID` NULL, del 2024-09-15 al 2026-08-12 (API Node); desde el
  13-ago todas lo traen salvo 2 del 19-ago en estado `P`.
- **Verificada por:** compuerta ANTES del 2026-09-30 (agente independiente):
  SOSTIENE.
- **Guardada por:** `ResolucionDeCapturasSueltasTest`
  (`E-APP-048 caso Q00002413 …`, `E-APP-048 propiedad - 300 escenarios …`).
- **Caduca si:** el servidor llena `IMPTE_DOCTO_CC_ID` de las capturas Node, o la
  app deja de restar capturas sueltas.
- **Arreglo publicado:** app 2.19.3 (66), 2026-10-02 ~15:30, commit `62f2cab0`
  de msp-app-kt; remedición en E-APP-056.

### E-APP-049 · Alcance en la flota: 2,206 capturas sueltas, 1,725 ventas por debajo de Microsip

- **Nivel:** MEDIDO · **Estado:** VIGENTE hasta publicar el arreglo
- **Medido:** 2026-09-30 17:15 → 10-01 01:13 UTC, 50 de 54 teléfonos por el
  depurador remoto (sólo `SELECT`, canarios `M2.rowCount = N_FOSIL` y
  `M3.rowCount = N_ABAJO + N_ARRIBA` en los 50); cruce en producción el 2026-10-01.
  `config/db_debug` restaurado idéntico al terminar.
- **Teléfonos:** 30 de 50 con capturas sueltas; 2,206 filas, $547,838.
- **Producción, fila por fila** (`MSP_PAGOS_RECIBIDOS` + `DOCTOS_CC` +
  `IMPORTES_DOCTOS_CC`; canario: 2,199 encontradas = suma de los conteos por lote):
  - 2,188 ($528,738) aplicadas una vez, mismo cargo y monto;
  - 9 ($8,600) apuntan a un documento que no existe en `DOCTOS_CC`;
  - 7 ($9,500) no existen en el servidor; 5 están en `MSP_FAILED_INTENTS` como
    422 `pago_saldo_insuficiente` (STATUS `new`) y oficina capturó a mano el mismo
    monto el mismo día;
  - 2 ($1,000) condonaciones del 19-ago en estado `P`, `EX_SALDO_CARGO_EXCEDIDO`.
- **Ventas (contra `MSP_SALDOS_VENTAS` de producción):** 1,725 por debajo por
  capturas sueltas ($359,860, según la compuerta ANTES), 34 en $0 con deuda,
  **0 por encima**. Las 44 "sin explicar" del teléfono: 42 coinciden con producción
  (el teléfono tenía una base vieja, no es defecto).
- **Control:** `oscar.roque` en 2.17.3 tiene 166 capturas sueltas y 0 ventas abajo.
- **Caduca si:** se publica el arreglo y la remedición a 24–72 h da cero.

### E-APP-050 · `GET /v2/cobranza/pagos/{id}` distingue las cuatro clases, pero un 200 con documento no prueba que el documento exista

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-10-01 17:48 UTC, API Go local (binario `a56d58f`) contra el
  Firebird de dev, sólo GET. Repetido por la compuerta ANTES.
- **Respuestas:** era Node aplicada → 200 con `docto_cc_id`, cargo e importe en 0;
  moderna → 200 completa; pendiente con error → 200 `sincronizacion: pendiente`
  sin documento; inexistente → 404 `problem+json` con `pago_no_encontrado` en
  `errors[0].message`. **Un documento borrado de `DOCTOS_CC` también da 200 con
  documento.** Controles: ruta inexistente 404 `text/plain`; sin token 401.
- **Consecuencia de diseño:** el arreglo exige dos pruebas para reconocer una
  captura: el documento del GET **y** el gemelo del sync con ese documento, cargo
  e importe.
- **Guardada por:** `ResolucionDeCapturasSueltasTest` (`documento sin gemelo …`,
  `un 404 del API sin pago_no_encontrado no marca`).

### E-APP-051 · El arreglo de capturas sueltas, verificado en un aparato

- **Nivel:** MEDIDO · **Estado:** VIGENTE (sólo dev)
- **Medido:** 2026-10-01/02, Galaxy A25 (SM-A256E, Android 15) por cable, app
  `devlocal` contra el API local y el Firebird de dev, zona 21563. Base del
  teléfono leída binaria por `adb exec-out run-as`; servidor por `isql`.
- **Línea base:** 310 de 310 ventas iguales al servidor (control de la comparación).
- **Reproducción con la versión de hoy (`main`):** 4 capturas sembradas → 4 ventas
  abajo, $550, 0 arriba.
- **Con el arreglo:** 3 resueltas al saldo del servidor (2 con gemelo: copia
  borrada, gemelo intacto; 1 con 404: marcada −1); la de documento inexistente no
  se tocó. 309 iguales, 1 abajo (la esperada), 0 arriba; `Payment` 6,086 → 6,084.
- **Sin servidor:** el worker devolvió RETRY dos veces y no cambió nada; al volver
  el acceso se resolvió solo (23:12:14).
- **Resincronización completa forzada:** ninguna venta volvió a bajar.
- **Abono real con ticket:** Y00001481 $1,500 → $100 → $1,400 y Y00001967 $2,800 →
  $100 → $2,700; un solo abono de cada uno en el servidor.
- **No verificado:** el endpoint en producción, la escala (≈215 consultas en un
  teléfono), otros modelos, y la reparación corriendo mientras se registra un abono.

### E-APP-052 · El historial del ticket muestra como abono una captura marcada "no aplicada" (−1)

- **Nivel:** MEDIDO · **Estado:** CERRADA en rama `fix/capturas-sueltas-saldo`
  (msp-app-kt, sin publicar) el 2026-10-02
- **Medido:** 2026-10-02 en el aparato de E-APP-051: el ticket de Y00001481 lista
  "ABONO: 12/08/2026 – $100", que es la fila con `DOCTO_CC_ID = −1`.
- **Consecuencia:** el saldo es correcto, pero el cliente ve un abono que Microsip
  no tiene; donde oficina lo capturó a mano, el abono saldría dos veces.
- **Arreglo (decisión del dueño, 2026-10-02: ocultar):** `RoomPagosAdapter.aHistorial`
  descarta `DOCTO_CC_ID = −1` antes de contar descartes (afecta `pagosDe`, `pago(id)`
  y `pagosDelPeriodo`); el ticket filtra con `abonosDelTicket` en sus dos listas.
  Las condonaciones −1 siguen saliendo como "no aplicada" (`condonacionesDe`, sin cambio).
- **Pruebas que lo guardan:** `CondonacionesDeLaVentaTest` "el abono que el servidor no
  tiene no sale en el historial" y `AbonosDelTicketTest`; control en el mismo lote
  (aplicado y pendiente siguen). **Rojo al revertir, MEDIDO 2026-10-02:** sin el filtro
  cada una falla con exactamente `no-aplicado` de más.
- **Sin verificar:** `importesCobrados` (línea base de la ruta) no filtra −1; una fila
  −1 sigue contando en el techo. No se midió si mueve la línea base.

### E-APP-053 · La condonación rechazada busca su código donde el API no lo pone

- **Nivel:** INFERIDO (fuerte) · **Estado:** VIGENTE
- **Leído:** `PendingPaymentsWorker.esCondonacionRechazadaPorSaldo` lee un `"code"` de
  primer nivel; el API lo pone en `errors[].message` (`auth.go` `mapAppError`) y el
  404 medido en E-APP-050 tiene esa forma. La prueba de la app usa un cuerpo con
  `"code"` arriba.
- **Consecuencia probable:** una condonación rechazada cae en el RELEASE genérico y
  queda como captura suelta. El arreglo de E-APP-048 la resuelve después (404 →
  −1), pero el camino directo no funciona.
- **Lo volvería MEDIDO:** un POST de condonación mayor al saldo contra dev.

### E-APP-054 · "Las 37 capturas sin gemelo de Juan pueden ser abonos perdidos"

- **Nivel:** — · **Estado:** REFUTADA el 2026-09-30
- **Quién lo creyó y por qué:** el autor del diagnóstico, al ver que 37 de 215
  capturas no tenían gemelo en el snapshot del teléfono; era razonable porque el
  14-ago se perdieron 11 abonos reales.
- **Qué lo refutó (MEDIDO):** las 215, incluidas las 37, están aplicadas una vez en
  producción con su documento.
- **Lo volvería cierto:** una captura suelta sin documento en `MSP_PAGOS_RECIBIDOS`
  ni abono equivalente en `IMPORTES_DOCTOS_CC`.

### E-APP-055 · "El depurador remoto sólo se activa al abrir la app desde cero"

- **Nivel:** — · **Estado:** REFUTADA el 2026-10-01
- **Quién lo creyó y por qué:** el autor, al esperar respuesta del teléfono de un
  cobrador; se apoyaba en la nota de memoria de que la lista blanca se revisa al
  arrancar.
- **Qué lo refutó (LEÍDO):** `RemoteDbDebugger.kt:72-170` escucha `config/db_debug`
  con `addSnapshotListener`; al agregar el correo, el listener de comandos se
  enciende en vivo, sin reiniciar. Basta el proceso vivo y conexión.

### E-APP-056 · Con 2.19.3 el saldo del teléfono ya no queda abajo del servidor por capturas sueltas; ningún teléfono queda arriba

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** depurador remoto del Room (consultas M1 resumen / M2 capturas
  sueltas / M3 ventas descuadradas, sólo `SELECT`), comparando `SALDO_REST`
  contra `PRECIO_TOTAL − TOTAL_IMPORTE − IMPTE_REST` que manda el servidor;
  línea base = medición del 30-sep/1-oct (E-APP-048). `allowedDevices` se
  respaldó y se restauró idéntico en ambas pasadas. Producción, teléfonos de
  la flota.
- **Pasada 1 (2026-10-02, 16:19–19:20, 33/54 teléfonos):** 7 teléfonos en
  2.19.3: ventas abajo 97 ($24,600) → 0; en $0 con deuda 3 → 0; arriba 0 → 0.
  **Control positivo:** 26 teléfonos aún en 2.19.2 conservaban el defecto (979
  abajo $228,780 → 861 $185,130).
- **Pasada 2 (2026-10-05, ~17:50–20:54, 46/54 teléfonos, 44 en 2.19.3):**
  ventas abajo 1,813 ($402,710) → 106 ($24,750); abajo por captura suelta
  1,722 → 55; en $0 con deuda 36 → 1; arriba 0 → 0. Caso de campo Juan
  Martínez: 174 ventas abajo ($36,550) → 0.
- **Telemetría 2026-10-02 (primeros 5 teléfonos con resolución):** 234
  revisadas, 202 reconocidas, 32 sin evidencia ($13,850), 0 no aplicadas. En
  Miguel Suárez las 5 restantes sin evidencia no restan en ninguna venta
  visible (M3 vacío): **INFERIDO** que pertenecen a ventas que ya no están en
  su teléfono; lo volvería MEDIDO consultar `sales` por esos
  `DOCTO_CC_ACR_ID`.
- **Residuo (abierto):** Pablo Vargas (2.19.3) 52 ventas abajo ($17,350), 70
  capturas sueltas; su telemetría `RESOLUCION_CAPTURAS_SUELTAS` da
  `pendientes: 70` en las 4 corridas del 2026-10-05 (13:07, 13:11, 13:30,
  19:13) — cada `GET /v2/cobranza/pagos/{id}` cae en un error transitorio
  (red/5xx/401/408/409/425/429/404 no del API); causa NO medida, la app no
  registra el código; lo volvería MEDIDO leer los logs del API de producción
  para sus GET. Otros 3 teléfonos con 1 venta cada uno (Rigoberto Santiago,
  Israel Soto, Alejandro Chavarría). Jesús Guillermo sigue en 2.18.1 (3
  ventas abajo, $550), versión anterior a la actualización sugerida.
- **Sin verificar:** en Joaquín Rivera desaparecieron 25 ventas abajo "sin
  explicar" (no por captura suelta) entre la base y la pasada 1; no se sabe
  si fue el arreglo o su sync. Las 106 − 55 = 51 ventas abajo restantes no se
  deben a capturas sueltas (la mayoría explicadas por abonos pendientes de
  subir, p. ej. Eliseo Ramírez); no se revisaron una por una.

---

## Compuerta previa del 2026-09-23

Primera corrida de la compuerta previa de `CLAUDE.md` §8, sobre un cambio
propuesto en la **reconciliación de cobranza del cliente Android** (la familia de
E-APP-001: acotar la enumeración local a la ventana del cobrador).

**Veredicto: cambio DETENIDO.** Siete de las ocho preguntas salieron distintas de
SOSTIENE, y la regla es que cualquier cosa que no sea SOSTIENE detiene el cambio.

| # | Relación | ¿Bloquea? |
|---|---|---|
| 1 | SOSTIENE | no |
| 2 | CONTRADICE | **sí** |
| 3 | PARCIAL | **sí** |
| 4 | CONTRADICE | **sí** |
| 5 | PARCIAL | **sí** |
| 6 | SOSTIENE | no |
| 7 | SOSTIENE | no |
| 8 | CONTRADICE | **sí** |

> **Hueco honesto.** El **enunciado** de cada una de las ocho preguntas no se
> transcribió: vive sólo en la conversación del 2026-09-23 y no se puede
> reconstruir sin inventarlo, así que aquí queda el número, la relación y el
> veredicto. Lo que la corrida **sí** dejó por escrito son las afirmaciones
> **E-APP-010 … E-APP-019** de arriba y la corrección **E-COB-013** en
> `cobranza.md`; ésas son el producto de la compuerta y están verificadas cita
> por cita. Para la próxima corrida: **copiar la pregunta literal en la tabla**,
> junto a su relación.

**Nada de esto se midió en un aparato.** Todas las afirmaciones nuevas son LEÍDO
(código abierto línea por línea el 2026-09-23) salvo E-APP-014, E-APP-016 y
E-APP-018, que son MEDIDO **por salida de `grep` con control positivo** — no por
observación de campo. Los efectos (el desbordamiento del `IN (...)`, el canal
muerto, el bucle pagos→ventas) están marcados INFERIDO dentro de cada
afirmación.

### Qué hay que medir en un aparato para poder reintentar

> **Contestado el 2026-09-24, en un aparato.** Los tres puntos de abajo se
> midieron; se conservan tal cual se escribieron y aquí queda el resultado:
>
> 1. **Volumen de fantasmas por corrida** — **contestado a medias.** Se midió la
>    **cota superior del conjunto**: 5,522 pagos en la zona, seis veces el tope
>    (**E-APP-034**). Cuántos clasifica como fantasma *una corrida real* sigue
>    **sin medir**.
> 2. **Censo de versión de Android de la flota** — **contestado.** Cero
>    aparatos por debajo de API 31 en servicio (**E-APP-036**). E-APP-010 pasa
>    de urgente a **latente**, con `minSdk = 24` dejando la puerta abierta.
> 3. **Si existe telemetría que registre cuántas filas se borraron** —
>    **contestado: no existe**, y el canal que sí funciona está en el mismo
>    archivo, sin usarse (**E-APP-037**).

1. **Volumen de fantasmas por corrida.** Cuántos ids clasifica como fantasma
   `reconcilePagosViaIds` / `reconcileSaldosViaIds` en un teléfono real, por
   corrida y por zona. Es el número que decide si E-APP-010 es teórico o si ya
   está pasando: por debajo de ~900 el `IN (...)` no revienta; por encima, sí, y
   en silencio.
2. **Censo de versión de Android de la flota.** Cuántos teléfonos están por
   debajo de **API 31**, que es donde vive el tope de 999
   (`SqliteInParams.kt:3-20`). Si la flota entera está en 31 o más, E-APP-010
   cambia de urgente a deuda; si no, es un defecto activo y no medido.
3. **Si existe telemetría que registre cuántas filas se borraron.** Hoy el
   borrado se anuncia con un `Log.i` (`CobranzaReconciler.kt:309` y `:373`), que
   no sale del teléfono. Sin una cuenta que llegue al servidor no hay forma de
   saber, después del hecho, si una corrida borró tres filas o tres mil — y ése
   es exactamente el dato que hace falta para poner la cota de E-APP-014.
