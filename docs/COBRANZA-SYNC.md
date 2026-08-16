# Sincronización de cobranza — contrato, invariantes y trampas

> **Lea esto ANTES de tocar cualquier cosa del sync de cobranza.** No es
> documentación de cortesía: cada regla de aquí abajo existe porque su
> ausencia ya costó dinero o tiempo en producción, y varias no las detecta
> ninguna prueba si se rompen "con buena intención".
>
> **Alcance:** cubre los dos repos. El protocolo lo define el servidor
> (`msp-api`), el cliente lo consume (`msp-app-kt`). Un cambio en cualquiera
> de los dos puede romper al otro sin que nada se ponga rojo.

---

## 1. Por qué este mecanismo es delicado

El sync de cobranza mueve **pagos**. Un defecto aquí no produce una pantalla
fea: produce que **un cobrador no vea un pago, crea que no se registró y
vuelva a cobrarle a un cliente que ya pagó**. Por el sistema pasan ~341,000
pagos al año.

El 2026-08-15 se encontraron **siete defectos simultáneos en producción**.
La suite de pruebas estaba **verde en los siete**. Ese es el punto de partida
que este documento intenta que no se repita.

---

## 2. El cursor es un PAR, no una fecha

El servidor pagina por `(UPDATED_AT, PK)`. El cliente **debe persistir las dos
mitades**.

```
cobranza_sync_state:  CURSOR (UPDATED_AT)  +  AFTER_ID (PK)
```

### Invariante duro

> **Donde se escribe uno, se escribe el otro. Todo camino que deje `CURSOR` en
> `NULL` debe dejar `AFTER_ID` en 0 en la misma operación.**

### Por qué importa (incidente real)

Durante meses el cliente guardó **sólo el cursor**, con un comentario que
justificaba el atajo: *"las escrituras son idempotentes (UPSERT por PK), sólo
gasta red"*. El supuesto era que el grupo de filas empatadas en el mismo
`UPDATED_AT` sería chico.

Un backfill de migración dejó **1,835,734 de 2,173,422 filas compartiendo un
único `UPDATED_AT`**. El grupo empatado pasó a ser el historial completo, la
paginación nunca salía de él, y el resultado medido en un teléfono fue:

```
2,057 pagos re-descargados cada ~76 segundos, indefinidamente
```

Se auto-cura sólo si entra un pago con fecha nueva. Es suerte de los datos,
no diseño.

### Trampa que casi lo reintroduce

Persistir `AFTER_ID` **no basta**. El lambda de página traducía una respuesta
vacía a `afterId = 0`:

```kotlin
afterId = response.items.lastOrNull()?.impte_docto_cc_id ?: 0   // ← MAL
```

Eso borra la posición recién guardada y devuelve el bucle **un tick después**.
El servidor devuelve el **cursor recibido** cuando no hay filas
(`page_helpers.go`: `MaxUpdatedAt: cursor.UTC()`, sobrescrito sólo
`if len(items) > 0`), así que la posición sigue siendo válida y **debe
conservarse**. Hoy `SyncPage.afterId` es `Int?` y `null` significa "sin filas,
no hay posición nueva".

### Pruebas que lo guardan (`CobranzaSyncManagerTest`)

- `segundaCorridaRetomaDelAfterIdPersistidoConTodoElLoteEmpatado`
- `paginaVaciaNoReiniciaElAfterIdPersistido`
- `corridaInterrumpidaRetomaDesdeLaUltimaPaginaAplicada`
- `replayPorEpochReiniciaElAfterIdJuntoConElCursor`
- `cambioDeZonaDejaCursorNuloYAfterIdEnCero`

---

## 3. La generación (`sync_epoch`)

El sync es incremental por cursor. Cuando cambia **lo que el servidor
proyecta** —no la fila de origen— las filas ya guardadas **no vuelven a
bajar**: su `UPDATED_AT` no cambió.

`MSP_CFG_SYNC_EPOCH` resuelve eso: el servidor sube la generación, el cliente
ve una distinta a la suya, limpia el cursor y replica desde cero.

### Reglas, todas deliberadas

| Regla | Por qué |
|---|---|
| El epoch se persiste **sólo cuando el replay terminó** (`has_more == false`) | Si el proceso muere a media descarga, la generación guardada sigue siendo la vieja y el próximo arranque replica otra vez. El costo de equivocarse por ese lado es **ancho de banda**; por el otro sería un replay a medias **congelado para siempre** |
| Un solo reinicio por corrida | Sin ese cerrojo, un epoch distinto reiniciaría la paginación en cada página |
| Epoch inválido (ausente, nulo, 0, negativo) = mecanismo apagado | Un servidor viejo se comporta como antes; ninguna ruta que produzca un cero por default mete al cliente en un bucle de replays |
| Un epoch que **retrocede** también replica | La generación es **identidad**, no orden |
| Si cambia a media paginación, no se persiste ninguno | El resultado sería una mezcla de dos generaciones |

**Verificado en dispositivo (2026-08-16):** se mató la app a media descarga con
`AFTER_ID` intermedio; el epoch **no** se guardó, y al reabrir replicó desde
cero **una vez**, terminó, y quedó estable. Sin duplicados ni pérdidas.

### Cuándo hay que subir el epoch

**Siempre que cambie el `WHERE` de lo que se entrega**, aunque no se mueva
ningún `UPDATED_AT`. Si no, las filas que ahora califican quedan por debajo
del cursor de todos los dispositivos y **no llegan nunca**.

Precedente: migración `000056`, que acompañó el cambio de ventana de D2.

### Pruebas que lo guardan (`CobranzaSyncManagerTest`)

`epochNuevoLimpiaElCursorYReplicaDesdeElInicio` · `epochIgualNoReplica` ·
`epochNoSePersisteHastaQueElReplayTermina` ·
`servidorSinEpochNoReplicaNiPierdeLaGeneracionAplicada` ·
`epochCeroSeIgnoraYNoEntraEnBucleDeReplays` ·
`epochQueRetrocedeReplicaUnaSolaVez` ·
`epochQueCambiaAMediaPaginacionNoSePersiste`

---

## 4. Los tres canales deben coincidir

Un pago viaja por **tres** rutas distintas, y las tres deben entregar **el
mismo conjunto**:

| Canal | Uso |
|---|---|
| `/sync/pagos/zona/{id}` | descarga incremental por cursor |
| `/sync/pagos/zona/{id}/digest` + `/ids` | inventario del reconciliador |
| `/sync/pagos/by-ids` | rescate puntual de filas faltantes |

### Invariante duro

> **Para la misma zona y el mismo `desde`, el conjunto de ids del sync (sin
> tombstones), el del inventario y el de `by-ids` deben ser IDÉNTICOS.**

### Por qué (incidente real)

`ByIDs` no tenía **ningún** filtro — sólo `zona` + `id IN (...)`. Era el canal
permisivo del que salían los duplicados reportados en campo. Y el digest usaba
un predicado distinto al del sync, así que el reconciliador **borraba como
fantasmas** justo lo que el sync entregaba.

La ausencia de una prueba de paridad es lo que dejó derivar los tres canales
durante meses.

### Pruebas que lo guardan

`TestE2E_ParidadCanales_Pagos` · `TestE2E_ParidadCanales_Ventas` ·
`TestE2E_ByIDs_AplicaLaVentana` · `TestE2E_DigestPagos_MismaVentanaQueElSync`

---

## 5. La ventana se deriva de la VENTA, no del pago

```sql
-- pagos: sync, digest y by-ids comparten LA MISMA constante
(s.SALDO > 0 OR s.FECHA_ULT_PAGO >= ?)

-- ventas: + la rama de cancelados, que NO es un filtro de saldo
(s.SALDO > 0
   OR s.FECHA_ULT_PAGO >= ?
   OR (s.CARGO_CANCELADO = 'S' AND EXISTS (... FECHA_HORA_CANCELACION >= ?)))
```

### Por qué

El pago que **salda** una venta deja el saldo en cero y, con el filtro viejo,
**se borraba a sí mismo**. Peor: la venta saldada tampoco viajaba —el filtro de
ventas no tenía rama de `FECHA_ULT_PAGO`— así que **desaparecía del teléfono
justo después de cobrar**.

El Node legacy no hacía esto: filtraba las **ventas** y derivaba los pagos.

### Cosas que NO se deben tocar

- **La rama de cancelados** no es un filtro de saldo: es la señal con la que el
  teléfono **borra** un cargo que Microsip canceló. Sin ella el cobrador
  arrastra ventas fantasma para siempre.
- **`desde` no es opcional.** Tiene default de servidor (7 días,
  `app.ResolveSyncDesde`). Sin él, el predicado colapsa a `SALDO > 0` estricto
  y el pago que salda desaparece.
- El default **no** puede vivir en `parseOptionalDesde`: ese helper lo comparten
  `/saldos/zona` y `/pagos/zona`, donde `desde` y `ventana_dias` son
  **excluyentes** (`ErrParametrosExcluyentes`). Un default no-nil ahí haría que
  toda llamada con `?ventana_dias=` respondiera **422**.

### Rendimiento

Usa `IDX_MSP_SALDOS_ZONA_FUP (ZONA_CLIENTE_ID, FECHA_ULT_PAGO)`, que ya existe.

| Predicado | Tiempo | Lecturas |
|---|---|---|
| `s.SALDO > 0 OR s.FECHA_ULT_PAGO >= ?` | **0.285 s** | 18,163 |
| `EXISTS` correlacionado | 7.5 s | 1,324,166 |

### Pruebas que lo guardan

`TestE2E_SyncPagos_LaVentanaEsDeLaVenta_NoDelPago` ·
`TestE2E_SaldosDigest_VentanaIncluyeSaldadasRecientes` ·
`syncEnviaDesdeEnTodasLasPaginas`

---

## 6. Los gemelos: DOS mecanismos distintos

Cuando el cobrador captura un pago, la app crea una fila con **UUID**. Cuando
el servidor lo aplica, vuelve la fila **numérica**. Las dos no pueden convivir.

| Mecanismo | Criterio | Aplica a |
|---|---|---|
| **Gemelo UUID** | `PAGO_RECIBIDO_ID` de la fila numérica nombra al UUID local | pagos v2 |
| **Gemelo legacy** | mismo `DOCTO_CC_ID` + `ID LIKE '%-%'` + `GUARDADO_EN_MICROSIP = 1` | histórico pre-cutover |

Son **complementarios**; ninguno sustituye al otro.

### Invariante duro

> **No debe existir un instante observable con las dos filas.** El colapso va
> en la **misma transacción** que el insert, en **todos** los caminos que
> insertan pagos.

Los `Flow` de Room de la UI **no** pasan por el `cobranzaWriteMutex`, así que
sin transacción hay una ventana real, por corta que sea.

### Orden dentro de la transacción

```kotlin
db.withTransaction {
    collapseLegacyTwins(alive, fase)   // ANTES: ningún cerrojo sostiene
    paymentDao.saveAll(...)            //        "la fila que escribí se borra sola"
    collapseUuidTwins(fase)            // DESPUÉS: el gemelo sólo nace
}                                      //          colapsable con la numérica escrita
```

### El candado invertido (incidente real)

El colapso exigía `GUARDADO_EN_MICROSIP = 1` — una bandera **local**. Pero que
el servidor devuelva `pago_recibido_id` **ya prueba** que el pago está en
Microsip: es **evidencia más fuerte**.

Y la carrera es estructural: el `POST_EVENT` sale **dentro de la misma
transacción** que escribe el pago, así que el aviso puede llegar antes de que
el worker marque. Además, si el worker recibe `IOException` tras aplicar,
reintenta **sin** `markDone`.

**Verificado en dispositivo:** el colapso ocurre con la bandera en **0**.

### La protección que reemplaza a la bandera

> **El UUID lo genera el teléfono. El servidor no puede nombrar uno que nunca
> recibió.**

`Payment.PAGO_RECIBIDO_ID` la escribe **un solo lugar** en producción:
`PagoDto.toEntity()`. Ninguna ruta de captura local la toca.

El gemelo **legacy** sí conserva `GUARDADO_EN_MICROSIP = 1`, porque ahí no hay
evidencia del servidor que la reemplace: `DOCTO_CC_ID` solo no distingue
"mismo pago, `markDone` perdido" de "captura pendiente con `docto_cc_id` ya
anotado" — y esa ventana **existe de verdad** (`persistDoctoCcId` corre
**antes** de `markDone`).

### Pruebas que lo guardan

`mergePagosColapsaGemeloUuidAunSiElWorkerNoAlcanzoAMarcarLaBandera` ·
`mergePagosNoBorraCapturasPendientesQueElServidorNuncaNombro` ·
`byIdsColapsaElGemeloUuidDentroDelMismoReconcileNow` ·
`byIdsColapsaElGemeloLegacyDentroDelMismoReconcileNow` ·
`byIdsNoBorraLaCapturaQueElServidorNuncaNombro` ·
`byIdsNoBorraLaCapturaPendienteAunqueCompartaDoctoCcId`

---

## 7. Trampas que no son obvias

### 7.1 Dos ejes de filtrado distintos

| Quién | Columna | Valores |
|---|---|---|
| Servidor | `CONCEPTO_CC_ID` | `IN (87327, 27969)` |
| App | `FORMA_COBRO_ID` | `IN (157, 158, 52569)` |

**No son la misma lista en dos lugares: son columnas distintas.** Quitar el
filtro del servidor **no** queda cubierto por el de la app.

Medido: quitarlo haría entrar **enganches (24533)**, "mal cliente" y "fugas" a
los totales del cobrador — **+4%**.

### 7.2 Los nombres del código mienten

`CONCEPTOS_CC` de Microsip es la autoridad:

| ID | Nombre real | Lo que dice el código |
|---|---|---|
| 87327 | Cobranza en ruta | ✅ |
| 155 | **Cobro en mostrador** | excluido como "concepto interno" |
| **27969** | **Condonaciones** | ❌ "abono mostrador" (falso, en 4 lugares) |

El mapeo `forma_cobro 137026 → concepto 27969` **sí es correcto**
(`FORMAS_COBRO_CC` dice que 137026 es "Condonacion"). Lo falso son los
identificadores en Go.

**`FECHA_ULT_PAGO` se restringe a `(87327, 155)`** por la migración `000011`
(vigente en `000023`) — otra lista más, que **no** coincide con la del sync.

### 7.3 `EsIngreso()` cuenta como ingreso todo lo que no conoce

`clientes/domain/categoria.go` clasifica y `EsIngreso()` devuelve `true` para
`CategoriaOtro`. Medido en producción: **$6.0 M de "Devolución"**, $1.5 M de
anticipos, $875 k de FCOBRADOR y $792 k de ajustes entran a los totales del
cliente sin clasificar. Un concepto nuevo en Microsip entra solo.

### 7.4 Límite de parámetros de SQLite

En Android ≤ 11 el tope es **999**. `by-ids` **no está acotado**
(`fetchInChunks` aplana y puede devolver miles), así que todo `IN (...)` sobre
ese conjunto **debe trocearse** (`SQLITE_MAX_IN_PARAMS = 900`). Sin eso lanza
*"too many SQL variables"*, lo atrapa el `try/catch` externo y se convierte en
**error silencioso en cada tick**.

> ⚠️ **Sospecha abierta, sin medir:** `mergePagos` arma sus `doctoCcIds` desde
> una página de hasta **1000** pagos → ~1000 parámetros contra un tope de 999.
> Es inferencia por versión de SQLite según API level. **Robolectric usa SQLite
> de escritorio con tope alto, así que ninguna prueba puede detectarlo.**
> Requiere un teléfono viejo de la flota para confirmarlo.

### 7.5 `FECHA` es la excepción documentada

Regla general: los datos salen de `DOCTOS_CC`/`IMPORTES_DOCTOS_CC`, la caché es
respaldo. **`FECHA` va al revés** — la hora real del cobro sólo existe en
`MSP_PAGOS_RECIBIDOS`; `DOCTOS_CC.FECHA` es un `DATE` sin hora.

---

## 8. Orden de despliegue — no negociable

Hay dos restricciones que se pisan si se despliega todo junto:

- **D1 (`AFTER_ID`) debe estar en la flota ANTES de subir el epoch**, o toda la
  flota entra en re-descarga por ciclo.
- **El servidor debe entregar confiable ANTES de que el colapso relajado se
  active**, o se borra la copia local de un pago que el canal quizá no reenvíe.

Y ambos cambios viajan en el mismo APK. Se resuelve así:

| # | Paso | Por qué ahí |
|---|---|---|
| 1 | **API con el predicado nuevo, SIN correr la migración del epoch** | El `WHERE` queda vivo y los pagos nuevos fluyen; las filas viejas no se re-sincronizan → **cero re-descarga**. El canal ya es confiable |
| 2 | **APK a la flota** | El colapso relajado ya es seguro |
| 3 | **Esperar adopción** (bloqueo por versión mínima de `:core:appgate`) | |
| 4 | **Correr la migración del epoch** | Cada teléfono replica — y con `AFTER_ID` puede **terminar** |

> Las migraciones de Firebird **no** corren solas al arrancar el API: se aplican
> a mano con `make fb-migrate-up`. Por eso el paso 1 es separable del 4.

**Snapshot antes de migrar:** `make fb-snapshot NAME=antes-<mig>`.

---

## 9. Cómo probar en un dispositivo real

Lo único que cazó los cinco diagnósticos falsos del incidente fue **medir en un
teléfono**, no leer código. Vale la pena repetirlo.

### Preparación

- **`devserver` está RETIRADO**: su túnel pasó a ser el de **producción**. Un
  APK de prueba apuntando ahí **escribiría pagos en la base de producción**.
  Use **`devlocal`**, que toma host y puerto de `local.properties`.
- Android bloquea HTTP en claro. `127.0.0.1` **sí** está en la lista blanca del
  config de debug, así que use **`adb reverse tcp:3001 tcp:3001`** con
  `LOCAL_API_HOST=127.0.0.1`. Evita tocar la política de red.
- `devlocal` instala con sufijo `.test`: **convive con producción** sin
  desinstalarla.

### Trampas del entorno (costaron horas)

- **`adb reverse` no derriba conexiones ya establecidas.** OkHttp mantiene viva
  la del pool, y como el sync corre cada 30 s nunca queda ociosa. Cambiar el
  mapeo **no tiene efecto**. Para cortar de verdad: **`adb kill-server`**.
- **El depurador remoto de Room no arranca tras el login.** `init()` corre en
  `Application.onCreate()`, cuando `currentUser` aún es `null`, y sólo
  re-evalúa cuando **cambia el documento de config**. Para activarlo: tocar
  `config/db_debug` en Firestore.
- `config/db_debug.allowedDevices` debe contener el correo, o el teléfono
  **rechaza los comandos** (correctamente).
- El resultado se guarda con **id autogenerado** y el comando va en el campo
  `commandId` — no con el id del comando.

### Qué mirar

| Prueba | Evidencia buscada |
|---|---|
| **D1** | `after_id` **avanzando** entre páginas y **persistido** entre corridas; conteos **decrecientes**, no fijos |
| **D1 duro** | Matar la app a media descarga → al reabrir **retoma o replica una vez**, y termina |
| **D2** | Saldar una venta → **la venta sigue en el teléfono** (`ventas page: items=1` con saldo 0) |
| **D4** | Colapso con `GUARDADO_EN_MICROSIP = 0` |
| **Migración** | Instalar **encima** de la versión anterior → columna nueva **y datos intactos** |

### Reglas de seguridad

1. **No toque `config/api_settings`** en Firestore: ese `baseURL` es el
   kill-switch de la flota entera.
2. **Sólo `SELECT`** con el depurador remoto (`blockDangerousQueries` está en
   `false` en producción).
3. Si el sync se cuelga sin avanzar y sin error, puede ser el **pool de
   Firebird envenenado**, no la app: `docker restart msp-api-dev`.

---

## 10. Catálogo de incidentes y su guardia

| # | Incidente | Causa raíz | Prueba que lo impide |
|---|---|---|---|
| 1 | Re-descarga infinita (2,057 cada 76 s) | `after_id` no persistido | `segundaCorridaRetomaDelAfterIdPersistido…` |
| 1b | Idem, un tick después | página vacía → `afterId = 0` | `paginaVaciaNoReiniciaElAfterIdPersistido` |
| 2 | El pago que salda desaparece | filtro propio del pago | `TestE2E_SyncPagos_LaVentanaEsDeLaVenta_NoDelPago` |
| 2b | La venta saldada desaparece | ventas sin rama `FECHA_ULT_PAGO` | `TestE2E_ParidadCanales_Ventas` |
| 3 | Duplicados por `by-ids` | canal sin filtro | `TestE2E_ByIDs_AplicaLaVentana` |
| 4 | Duplicado visible | candado invertido | `mergePagosColapsaGemeloUuidAunSi…` |
| 4b | Duplicado por minutos | colapso fuera de la transacción | `byIdsColapsaElGemeloUuidDentroDelMismoReconcileNow` |
| 4c | "Totales al doble" (cutover) | gemelo legacy sin colapsar en `by-ids` | `byIdsColapsaElGemeloLegacyDentroDelMismoReconcileNow` |
| 5 | Semana en $0 con tabla llena | fecha de carga → "ahora" | `cycleRange con carga null es null…` |
| 6 | Porcentaje pegado en 0.00% | lectura de un solo tiro | `AdjustedPaymentPercentageReactiveTest` |
| 7 | Escaneo completo en el colapso | índice faltante | `Migration28to29Test` |
| 8 | Supuestos falsos sobre Microsip | el código y la prueba comparten la premisa | `TestContrato_*` |

### Las pruebas de contrato (§8 de la tabla)

Hay una clase de defecto que las pruebas **no pueden** atrapar: cuando el
código y la prueba comparten la misma premisa falsa sobre el mundo exterior.
El concepto 27969 es el ejemplo — cualquier prueba escrita a mano habría
codificado el nombre equivocado.

`TestContrato_*` consulta **la base**, no nuestras constantes:

- `TestContrato_PagoConceptoFilter_ContraCatalogoConceptosCC`
- `TestContrato_ListasDeConceptos_SyncVsFechaUltPago`
- `TestContrato_DosEjes_ConceptoCC_vs_FormaCobroCC`
- `TestContrato_ClasificacionDeConceptos_ContraCatalogoConceptosCC`
- `TestContrato_ConceptoDeCargo_QueLasKPIsDelClienteAsumen`

Los ids **no se escriben a mano**: salen de `pagoConceptoFilter`, del cuerpo
vivo del procedure en `RDB$PROCEDURES` y de los catálogos. Si alguien edita la
constante, la prueba lee el valor nuevo.

---

## 11. Telemetría — cómo se ve un cursor atorado sin tener el teléfono

`CobranzaSyncTelemetry` emite `cobranza_sync.resource`,
`cobranza_sync.cursor_stalled` y `cobranza_sync.run`.

El campo que importa es **`resumed`**, y su diseño no es obvio:

> Comparar la posición de arranque contra la de cierre **dentro de una misma
> corrida** da **falso negativo**: con el código roto, cada corrida arrancaba
> en `afterId = 0` y cerraba en `(X, 2057)` — el bucle infinito se habría
> reportado como **sano**.

La comparación correcta es **cierre contra cierre entre corridas**, en memoria
de la propia telemetría y **sin leer el estado persistido**: *una telemetría
que lee la fuente rota hereda su ceguera.*

**Cero datos personales**: sólo zona, recurso, conteos y tiempos. La posición
viaja como huella de 8 hex — que **no es anonimización criptográfica** (poca
entropía, forzable), pero evita que el PK del documento salga del teléfono.

Guardado por `ningun evento emite datos personales` (lista blanca cerrada de
claves) y `si la telemetria revienta el sync sigue funcionando`.

---

## 12. Antes de dar por bueno un cambio

- [ ] `go test -short ./...` y `golangci-lint run ./...` **a mano y completo**
      (`--new-from-rev` deja pasar cosas)
- [ ] Integración con `FB_DATABASE` y `-timeout 1800s`
- [ ] `./gradlew prePushCheck` — con el árbol caliente sale en segundos y **no
      prueba nada**; use `--rerun-tasks` al menos una vez
- [ ] **Cada arreglo con una prueba que se ponga ROJA al revertirlo.**
      Compruébelo de verdad: revierta, corra, confirme el rojo, restaure, y
      **verifique que no quedaron restos** — ya pasó que un marcador de
      mutación se quedara puesto y el reporte dijera "verde"
- [ ] Si cambió el `WHERE` de lo entregado: **subir el epoch**
- [ ] Si tocó los tres canales: correr las pruebas de **paridad**
- [ ] Si tocó cualquier camino que inserte pagos: verificar que **colapsa en la
      misma transacción**
