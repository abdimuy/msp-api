# Evidencia — cobranza

Registro de hechos y su prueba para el módulo de cobranza (pagos, saldos,
reconciliador y el sync que los mueve). El diseño y las reglas viven en
[`../registro-de-evidencia.md`](../registro-de-evidencia.md); este archivo es
sólo el libro mayor del área.

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

El identificador (`E-COB-014`) es el puente. Se cita desde el comentario que
explica la trampa, desde el nombre de la prueba que la guarda y desde el mensaje
del commit que la cierra:

```go
// Escribimos sólo si algo cambió: la proc mueve UPDATED_AT siempre
// y UPDATED_AT es el cursor del sync. Ver E-COB-014.
```

```
fix(cobranza): comparar antes de escribir en el barrido de pagos

Cierra E-COB-014.
```

Los números son correlativos y **nunca se reutilizan ni se renumeran**. Lo
refutado **no se borra**: cambia de estado a REFUTADA y se queda, con la fecha y
con la condición que lo volvería cierto.

**Siguiente identificador libre: `E-COB-019`.**

---

### E-COB-001 · El barrido de pagos reescribe `UPDATED_AT` de casi toda la tabla y cero filas cambian de valor

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, base de **dev** (`mueblera-firebird`)
- **Cómo:** comparación fila a fila entre los respaldos del 15-ago y el 2-sep de
  `MSP_PAGOS_VENTAS`, todas las columnas menos `UPDATED_AT` y `TX_ID`.
  **2,173,269 filas comunes, 0 con cambio de valor.**
- **Control positivo:** mutando 3 filas del volcado, el comparador reporta 3.
- **Procedencia:** la salida cruda **no está en el árbol**; la cifra viene de
  `docs/registro-de-evidencia.md` §4.3. El mecanismo que la explica sí está
  verificado y vive en E-COB-002.
- **Vive en:** `internal/cobranza/app/reconcile.go:415-444` (`runPagosPass`)
- **Guardada por:** — (sin prueba todavía)
- **Contradice:** el comentario de `internal/cobranza/app/reconcile.go:411-414`
  (ver E-COB-005)
- **Caduca si:** cambia la definición de `MSP_RECOMPUTE_PAGO`

### E-COB-002 · `MSP_RECOMPUTE_PAGO` escribe `UPDATED_AT = CURRENT_TIMESTAMP` en todas sus rutas de salida

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `migrations-firebird/000023_recompute_changelog.up.sql`
  - ruta lápida: líneas 120 y 124 (`UPDATED_AT` en la lista de columnas,
    `CURRENT_TIMESTAMP` en los valores)
  - ruta upsert normal: líneas 143 y 147
  - no hay ninguna comparación previa: el `UPDATE OR INSERT ... MATCHING` se
    ejecuta siempre que se llegue a esa línea
- **Por qué importa:** `UPDATED_AT` es la mitad del cursor del sync (E-COB-008).
  Mover la columna sin cambiar el valor manda la fila al teléfono otra vez.
- **Qué lo volvería MEDIDO adicional:** ya está medido por sus efectos en
  E-COB-001.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** se toca la proc en cualquier migración posterior

### E-COB-003 · El paso de saldos tampoco condiciona la escritura

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `internal/cobranza/app/reconcile.go`
  - `checkOneSaldo` llama `Recomputer.Recompute(ctx, cargoID)` en la **línea 386**,
    sin condición previa
  - la comparación ocurre **después**, en la línea 394 (`if r.hasDrift(...)`)
  - `hasDrift` sólo decide si se escribe un log: la línea 396 es
    `if r.deps.Config.DriftLog`
- **Contradice:** la creencia de que el paso de saldos "compara antes de
  escribir" — registrada como error propio en `docs/registro-de-evidencia.md` §1,
  caso 3
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** `checkOneSaldo` deja de llamar `Recompute` incondicionalmente

### E-COB-004 · `FixDrift` es configuración muerta: se declara, se pone en `true` y nunca se lee

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, sobre el árbol de trabajo de `msp-api` (rama `main`)
- **Cómo:** `grep -rn "FixDrift" --include='*.go' .`, descartando
  `.claude/worktrees/`. Los seis aciertos son: la declaración
  (`internal/cobranza/app/reconcile.go:25,28`), el valor de producción
  (`cmd/api/cobranza_wiring.go:204`), un comentario
  (`internal/cobranza/infra/cobranzahttp/handlers.go:130`) y tres literales de
  configuración en pruebas. **Ninguno es una lectura de `Config.FixDrift`.**
- **Control positivo:** el mismo grep sobre `DriftLog` **sí** encuentra su
  lectura: `internal/cobranza/app/reconcile.go:396`.
- **Consecuencia:** el recompute corre siempre, haya o no drift. Apagar
  `FixDrift` no apaga nada.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** alguien cablea `Config.FixDrift` a la rama de recompute

### E-COB-005 · El comentario de `reconcile.go:411-414` es falso

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **El comentario dice** (literal, `internal/cobranza/app/reconcile.go:411-414`):

  > `// runPagosPass pages through MSP_PAGOS_VENTAS and recomputes each pago. We`
  > `// don't compare values here — the recompute is idempotent and updates`
  > `// UPDATED_AT when the row changes, which is the only signal mobile clients`
  > `// care about. Drift detection lives in the saldos pass.`

- **Por qué es falso:** la proc actualiza `UPDATED_AT` **siempre**, no "cuando la
  fila cambia" (E-COB-002). El comentario justificó el diseño durante meses.
- **Contradice:** nada aguas arriba; es el comentario el que quedó desmentido.
- **Acción pendiente:** corregir el comentario en sitio y citar E-COB-001 desde
  ahí. Mientras no se haga, la mentira sigue viva en su lugar original.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** se corrige el comentario

### E-COB-006 · No hay pasada al arranque; cada reinicio reinicia el reloj de 7 días

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `internal/cobranza/app/reconcile.go:209-239` (`loop`). El `for/select`
  sólo tiene dos ramas: `ctx.Done()` y `ticker.C`. No hay llamada a `Run` antes de
  entrar al ciclo, y `Start` (líneas ~170-205) tampoco la hace.
- **Intervalo:** `7 * 24 * time.Hour` (`cmd/api/cobranza_wiring.go:201`).
- **Consecuencia:** un reinicio del servicio antes del día 7 pospone la pasada
  otros 7 días. En un servidor que se arranca a mano desde `run.bat`, la pasada
  puede no correr nunca (ver E-INF-005 en `infra.md`).
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** se agrega una pasada inicial o cambia `Interval`

### E-COB-007 · `/v2/_admin/saldos/reconcile` y `/backfill` disparan exactamente la misma pasada

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `internal/cobranza/infra/cobranzahttp/handlers.go:122` y `:138` —
  los dos handlers llaman `h.reconciler.Run(ctx)` y no reciben ningún parámetro
  (`ReconcileInput` y `BackfillInput` están vacíos, `dto.go:93-99`).
- **Consecuencia:** cualquiera de los dos reescribe `UPDATED_AT` de la tabla
  entera (E-COB-001) a demanda, fuera del reloj de 7 días.
- **Corroboración de campo:** una llamada a esas rutas en producción contesta
  **401, no 404** — existen y están montadas. MEDIDO 2026-09-23. *Ojo:* esa
  prueba establece que existen y exigen sesión; **no** dice nada sobre permisos.
  Ver E-COB-012.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** cambia la ruta o el handler deja de llamar `Run`

### E-COB-008 · El cursor del sync de pagos es el **par** `(UPDATED_AT, IMPTE_DOCTO_CC_ID)`

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `internal/cobranza/infra/ventfb/pagos_repo.go:409-412`:

  ```sql
  AND (p.UPDATED_AT > ? OR (p.UPDATED_AT = ? AND p.IMPTE_DOCTO_CC_ID > ?))
  ...
  ORDER BY p.UPDATED_AT, p.IMPTE_DOCTO_CC_ID
  ```

- **Por qué importa:** con `UPDATED_AT` solo, varias filas del mismo instante se
  saltan o se repiten entre páginas. Es el invariante duro de
  `docs/COBRANZA-SYNC.md`.
- **Guardada por:** las pruebas de paginación de `internal/cobranza/infra/ventfb`
  (pendiente de nombrar la que lo fija exactamente)
- **Caduca si:** cambia el `ORDER BY` o el predicado del cursor

### E-COB-009 · `MSP_PAGOS_DOCTOS_CC_AU` no vigila `CLIENTE_ID`

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `migrations-firebird/000013_msp_pagos_ventas.up.sql:213-227`. El
  trigger `AFTER UPDATE` sobre `DOCTOS_CC` dispara el recompute sólo si cambia
  `CANCELADO`, `CONCEPTO_CC_ID`, `FOLIO` o `FECHA` (líneas 220-223).
- **Consecuencia:** reasignar un pago a otro cliente deja el caché rancio: el
  `CLIENTE_ID` denormalizado en `MSP_PAGOS_VENTAS` no se recalcula y nadie se
  entera hasta la pasada del reconciliador.
- **Control positivo:** `grep -rn "MSP_PAGOS_DOCTOS_CC_AU" migrations-firebird/`
  encuentra el `CREATE TRIGGER` (000013:213), su `DROP` en el `.down` y una
  mención en `000060_desactivar_mggencobro.up.sql:13`. **No hay ningún
  `ALTER TRIGGER` posterior**, así que la definición leída es la vigente.
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** una migración redefine el trigger

### E-COB-010 · La limpieza de lápidas nunca borra nada

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **La cadena, toda en el mismo `Run`** (`internal/cobranza/app/reconcile.go:242-264`):
  1. `runSaldosPass` recomputa **cada** cargo → `UPDATED_AT = CURRENT_TIMESTAMP`
     en las dos rutas de salida de `MSP_RECOMPUTE_SALDO_VENTA`
     (`000023_recompute_changelog.up.sql:216` para la lápida, `:287` para la fila
     viva).
  2. `runPagosPass` hace lo mismo con cada pago (E-COB-002).
  3. **Después**, la línea 257-260 calcula `cutoff = ahora - 30 días` y llama
     `runTombstoneCleanup`.
  4. El `DELETE` es `WHERE CARGO_CANCELADO = 'S' AND UPDATED_AT < ?`
     (`internal/cobranza/infra/ventfb/saldos_repo.go:319-320`) y su gemelo
     `WHERE CANCELADO = 'S' AND UPDATED_AT < ?`
     (`internal/cobranza/infra/ventfb/pagos_repo.go:501-502`).
- **Por qué nunca borra:** los pasos 1 y 2 acaban de poner `UPDATED_AT` en
  *ahora*, incluidas las lápidas. Ninguna fila puede ser más vieja que el corte.
- **Qué lo volvería MEDIDO:** contar `MSP_SALDOS_VENTAS` con
  `CARGO_CANCELADO='S'` antes y después de una pasada en dev.
- **Guardada por:** `internal/cobranza/infra/ventfb/pagos_repo_tombstone_test.go`
  prueba el `DELETE` **aislado** — no prueba que la pasada completa llegue a
  borrar. Esa es justo la brecha.
- **Caduca si:** se reordena `Run` o se deja de recomputar las lápidas

### E-COB-011 · La lápida huérfana se voltea a `CARGO_CANCELADO='N'`

- **Nivel:** MEDIDO · **Estado:** **REFUTADA (2026-09-23)**
- **Se creyó cierta** porque la lectura del PSQL era correcta: cuando el cargo ya
  no existe en `DOCTOS_CC`, el `SELECT` de
  `migrations-firebird/000023_recompute_changelog.up.sql:194-197` no trae fila y
  deja todas las variables en NULL; el `IF (v_cancelado = 'S')` de la línea 207
  da UNKNOWN y la ejecución **sí** cae al upsert normal de la línea 280, que
  escribe `'N'`.
- **Por qué es falsa:** ese upsert **no puede completarse**. Los valores que
  manda incluyen `:v_cliente_id` y `:v_fecha_cargo`, los dos NULL (línea 285), y
  en `MSP_SALDOS_VENTAS` las columnas `CLIENTE_ID` y `FECHA_CARGO` son
  `NOT NULL` (`migrations-firebird/000010_msp_saldos_ventas.up.sql:38` y `:41`).
  Lanza `isc_not_valid`, el `WHEN ANY DO` de la línea 299 lo atrapa, registra en
  `MSP_SALDOS_ERRORS` y sale. **Es un no-op.**
- **Control positivo:** ensuciando a mano un cargo **vivo**, la misma proc sí lo
  repara — es decir, la proc funciona y lo que falla es únicamente la ruta
  huérfana.
- **Corroboración de campo:** `MSP_SALDOS_ERRORS` con **3,915 filas, 100% del
  mismo error**, sobre **2,798 cargos**, todos sin fila en `DOCTOS_CC`.
- **Procedencia de las cifras:** `docs/registro-de-evidencia.md` §4.4; la salida
  cruda no está en el árbol. El mecanismo (las dos `NOT NULL` y el `WHEN ANY DO`)
  sí está verificado línea por línea arriba, 2026-09-23.
- **CUIDADO — la protección es ACCIDENTAL.** No hay ninguna guarda explícita
  contra el cargo huérfano: lo único que impide la pérdida de datos son dos
  `NOT NULL` del esquema. **La reactivarían:**
  - relajar `CLIENTE_ID` o `FECHA_CARGO` a nullable en `MSP_SALDOS_VENTAS`;
  - "arreglar" la proc metiendo un `COALESCE` en `:v_cliente_id` o
    `:v_fecha_cargo`;
  - agregar un `DEFAULT` a esas columnas;
  - cualquier intento de "limpiar" `MSP_SALDOS_ERRORS` sin entender que esas
    3,915 filas **son** el escudo funcionando.
- **Guardada por:** — (sin prueba todavía). **Una prueba que fije este
  comportamiento es lo que convertiría la protección accidental en deliberada.**
- **Caduca si:** cambia el esquema de `MSP_SALDOS_VENTAS` o la proc

### E-COB-012 · Los endpoints `/v2/_admin/saldos/*` están protegidos sólo por autenticación, sin permiso

- **Nivel:** LEÍDO · **Estado:** **REFUTADA (2026-09-23)**
- **Se creyó cierta** porque el router **sí** monta el grupo con autenticación
  sola: `cmd/api/server.go:392-395` es `r.Use(authn.Handler)` y nada más, sin
  middleware de permiso, y el comentario de la línea 391 lo dice literal
  (*"authn only; no failed-intent capture"*). Y la prueba de campo —401, no 404—
  es compatible con la afirmación.
- **Por qué es falsa:** la compuerta de permiso existe, sólo que **dentro del
  handler**, no en el router:
  - `internal/cobranza/infra/cobranzahttp/handlers.go:119` —
    `authorize(ctx, auth.PermCobranzaReconciliar)`
  - `internal/cobranza/infra/cobranzahttp/handlers.go:135` —
    `authorize(ctx, auth.PermCobranzaBackfill)`
  - `authorize` no es cosmético: `internal/cobranza/infra/cobranzahttp/auth.go:48-54`
    resuelve el usuario actual y delega en `requirePerm`.
  - Los permisos están declarados y catalogados:
    `internal/auth/domain/permission_codes.go:91`, `:93`, `:198-199`
    (`cobranza:reconciliar`, `cobranza:backfill`).
- **Desde cuándo:** `git blame` sitúa las dos líneas en el commit `10d1628`
  (2026-05-30), que sustituyó un `currentUserOrError` + `requirePerm` explícito
  por el helper `authorize`. Nunca estuvieron sin permiso.
- **De dónde salió el error:** de leer el router y no el handler. El 401 medido
  en producción **no distingue** entre "sin permiso" y "con permiso": una sesión
  ausente contesta 401 en los dos casos. La medición era real; la conclusión que
  se le colgó, no.
- **Lo que sí queda en pie:** E-COB-007 — los dos endpoints existen, están
  montados y disparan la pasada completa.
- **Caduca si:** se quita el `authorize` de alguno de los dos handlers, o si el
  permiso se concede por defecto a algún rol amplio (**no verificado**: qué roles
  traen hoy `cobranza:reconciliar` y `cobranza:backfill` en producción está
  **sin medir**)

### E-COB-013 · La página vacía con `has_more=true` es LATENTE: hoy no es alcanzable por HTTP

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde), rama `main` del checkout principal de `msp-api`
- **Por qué existe esta afirmación:** el cliente Android **sí** tiene la trampa
  (E-APP-002 en `app-android.md`: con `has_more=true` y página vacía corta el
  ciclo y **sigue adelante a borrar** con un conjunto truncado). Lo que faltaba
  era decir si el servidor puede producir esa respuesta. **Por HTTP, no.**
- **Los tres eslabones:**
  1. `internal/cobranza/infra/cobranzahttp/handlers_sync_reconcile.go:32` — el
     campo `Limit` de `SyncListIDsInput` lleva
     `minimum:"1" maximum:"10000" default:"5000"`. Huma resuelve el default cuando
     el parámetro falta y valida el mínimo cuando viene; un `limit=0` explícito no
     pasa la validación.
  2. `internal/cobranza/app/service.go:405-414` — `clampReconcileLimit` mapea
     `limit <= 0` a `DefaultReconcileLimit = 5000` y recorta por arriba a 10000.
     Lo aplican los dos servicios: `:434` (pagos) y `:454` (saldos).
  3. `internal/cobranza/infra/ventfb/digest_query.go:309-324` —
     `scanIDsWithHasMore` lee `limit+1` filas y sólo devuelve
     `return ids[:limit], true` cuando `len(ids) > limit`.
- **Pero la ruta existe fuera de HTTP:** `ListIDs` está **exportada** en los dos
  repositorios (`digest_query.go:73` para `PagosRepo`, `:170` para `SaldosRepo`) y
  no clampea nada por su cuenta. Con `limit == 0` la consulta es
  `SELECT FIRST 1 ...` (`:80`, `args = limit+1`), y si hay al menos una fila
  `scanIDsWithHasMore` devuelve **`ids[:0]` con `hasMore = true`**: exactamente la
  respuesta anómala. Un llamador nuevo en Go —una herramienta, un job, un handler
  que olvide el clamp— la produce sin tocar nada más.
- **Qué corrige:** cualquier lectura que dé por **activa** esa trampa. El riesgo
  alcanzable hoy es la **lista legítimamente vacía** (`ids=[]`, `has_more=false`,
  una zona sin filas en la ventana), no la anómala — y ésa la procesa el cliente
  como "todo lo local es fantasma" sin ninguna cota (E-APP-014).
- **Qué lo volvería MEDIDO:** un `GET .../ids?limit=0` contra el servidor (se
  espera 422) y una prueba en Go que llame `ListIDs` con `limit=0` y compruebe la
  respuesta anómala. **Ninguna de las dos se corrió.**
- **Guardada por:** — el clamp no tiene prueba que lo fije desde el borde HTTP
- **Caduca si:** se relaja `minimum:"1"`, se quita `clampReconcileLimit` de alguno
  de los dos servicios, o aparece un llamador de `ListIDs` fuera de `app`

### E-COB-014 · Un saldo negativo SÍ viaja al teléfono: entra por `FECHA_ULT_PAGO`, no por `SALDO > 0`

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde), rama `main` del checkout principal de `msp-api`
- **Dónde:** `internal/cobranza/infra/ventfb/ventas_repo.go:142-144` —
  `ventaStatusFilterConVentana` empieza con
  `(s.SALDO > 0 OR s.FECHA_ULT_PAGO >= ? OR (s.CARGO_CANCELADO = 'S' AND (...)))`.
  Las ramas son un **OR**: una venta con `SALDO < 0` **falla** la primera y **pasa**
  por la segunda mientras su último pago caiga en la ventana. El comentario
  `:105-108` dice que ése es el predicado que comparten **los tres canales** —
  sync, digest/`ListIDs` y `ByIDs`—, así que la fila negativa entra por los tres.
- **La consecuencia, y es la que muerde:** la rama `FECHA_ULT_PAGO >= ?` es
  exactamente la que pone la venta en la ruta después de cobrar (incidentes 2/2b,
  `docs/COBRANZA-SYNC.md` §5). **La pastilla verde "Pagó esta semana" y el número
  negativo en pantalla son la MISMA condición**: si acaba de pagar, viaja; si
  viaja con el saldo por debajo de cero, el cobrador ve el negativo. No son dos
  defectos, es uno.
- **Qué lo volvería MEDIDO:** una venta con `SALDO < 0` y `FECHA_ULT_PAGO` dentro
  de la ventana, comprobando que sale por los tres canales. **No se corrió.**
- **CONTRADICE / enlaza — anotado el 2026-09-24:** **E-APP-022** en
  `app-android.md` se sembró el mismo día afirmando en su título que
  `SaleDao.updateTotal` era *"el único camino local **capaz de** fabricar un
  saldo negativo"*. Si esta ficha es cierta —y lo es—, **hay un segundo
  camino**: el negativo que viaja entra a Room por
  `CobranzaReconciler.kt:387`, que escribe el saldo crudo del servidor **sin**
  el `coerceAtLeast(0.0)` que sí aplica el camino hermano
  (`CobranzaSyncManager.kt:702`). **Las dos fichas se contradecían y ninguna lo
  decía.** E-APP-022 quedó corregida el 2026-09-24 con la enumeración de los
  **cinco** escritores de `sales.SALDO_REST` y los **tres** que pueden dejarlo
  negativo; esta línea es el otro extremo del enlace.
- **Lo que E-APP-025 aporta y lo que NO:** mide que **hoy** Microsip no tiene
  ninguna venta con saldo negativo que entregar (11,178 revisadas, 0 negativas).
  Eso hace que el negativo **observado** en campo sea de fabricación local
  (E-APP-030), y **no cierra esta ficha**: el servidor sigue entregando por esta
  rama lo que sea que tenga, y sigue sin saber detectarlo (E-COB-015).
- **Guardada por:** — (sin prueba que fije el comportamiento con saldo negativo)
- **Caduca si:** cambia `ventaStatusFilterConVentana`, o aparece un piso en el
  saldo aguas arriba

### E-COB-015 · El reconciliador no puede detectar un saldo negativo: compara el caché contra el mismo procedimiento que lo produjo

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **La cadena:**
  - `internal/cobranza/app/reconcile.go:375-394` — `checkOneSaldo` lee el caché
    (`SaldosRepo.PorCargo`, `:378`), llama `Recomputer.Recompute` (`:386`) y
    compara (`:394`).
  - `reconcile.go:446-455` — `hasDrift` compara **sólo** `Saldo()`,
    `TotalImporte()` y `NumPagos()` entre las dos instantáneas. No hay ninguna
    comprobación de signo ni de rango.
  - El caché que lee fue escrito por **ese mismo** procedimiento, así que un saldo
    negativo se reproduce idéntico: `hasDrift` da `false` y la fila pasa como sana.
- **Y no hay piso en ninguna de las tres capas:**
  - **El procedimiento:** `migrations-firebird/000023_recompute_changelog.up.sql:277`
    es `v_saldo = v_precio_total - v_total_importe - v_impte_rest`, sin
    `MAXVALUE`/`COALESCE`/tope, y se escribe tal cual (`:282` en la lista de
    columnas, `:286` en los valores).
  - **La tabla:** `migrations-firebird/000010_msp_saldos_ventas.up.sql:35-51` —
    `SALDO NUMERIC(14,2) NOT NULL` y **ningún `CHECK`**. (Coherente con
    `CLAUDE.md` §1: la regla canónica va en el dominio; el punto aquí es que
    **tampoco está en Go**.)
  - **Go:** `hasDrift` es todo lo que mira el saldo en la pasada.
- **Consecuencia:** si un saldo negativo llega a `MSP_SALDOS_VENTAS`, **nada en el
  servidor lo detecta ni lo repara**; y por E-COB-014 sí lo entrega.
- **Qué lo volvería MEDIDO:** censo `SELECT COUNT(*) FROM MSP_SALDOS_VENTAS WHERE SALDO < 0`
  en producción. **No se pudo: el túnel a producción está caído (2026-09-23).**
- **Guardada por:** — (sin prueba todavía)
- **Caduca si:** aparece un piso en el SP, un `CHECK`, o una comprobación de signo
  en la pasada

### E-COB-016 · `Service.AplicarPago` no llama a `validateCargo`: el tope "importe ≤ saldo" sólo existe en la captura

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23 (tarde)
- **Dónde está el tope:** `internal/cobranza/app/crear_pago.go:95-113` —
  `validateCargo` rechaza cargo inexistente, cargo cancelado e
  `importe.GreaterThan(saldo.Saldo())` (`:109-111`), y su propio comentario dice
  que es *"defense against double-collection from multiple devices"*.
- **Quién lo llama — MEDIDO (`grep -rn "validateCargo" --include='*.go' internal/`,
  sin `_test.go`):** **un solo sitio**,
  `internal/cobranza/app/crear_pago_con_imagenes.go:93`.
- **Quién NO pasa por él:** `Service.AplicarPago`
  (`internal/cobranza/app/aplicar_pago.go:73-...`) no lo invoca en ninguna de sus
  ramas. Y tiene **dos** llamadores:
  - `internal/cobranza/app/pago_retry_worker.go:186` — el worker de reintentos;
  - `internal/cobranza/infra/cobranzahttp/handlers_pago_recibido.go:203`, el
    handler `AplicarPagoForzar` (registrado en `routes.go:206`: *"Ejecuta
    AplicarPago manualmente sobre un pago pendiente"*).

#### Ampliación del 2026-09-24 — la ruta, el worker y la forma exacta del piso

- **CORRECCIÓN a esta misma ficha: la ruta admin NO es `/_admin/cobranza/...`.**
  Así se escribió el 2026-09-23 y es incorrecto. **Qué se creyó y por qué parecía
  razonable:** el path declarado en `routes.go:206` es `/pagos/{id}/aplicar`,
  relativo; el prefijo lo pone el montaje, y el handler vive en el paquete
  `cobranzahttp`, con lo que "cobranza" era la suposición natural. **La real,
  verificada:**
  - `cmd/api/server.go:392-395` — `r.Route("/_admin/saldos", ...)` es donde se
    monta `cobranzahttp.MountAdminRouter` (`:394`);
  - `cmd/api/server.go:233` — todo ese árbol cuelga de `r.Route("/v2", ...)`.

  **La ruta real es `POST /v2/_admin/saldos/pagos/{id}/aplicar`** — bajo el
  grupo de **saldos**, no bajo `/v2/cobranza`. Quien busque el endpoint por el
  nombre equivocado concluirá que no existe.
- **Y sí tiene permiso, aunque el router no lo ponga:**
  `handlers_pago_recibido.go:196` — `requirePerm(cu, auth.PermCobranzaReconciliar)`,
  con `currentUserOrError` en `:191`. Es el mismo patrón que **E-COB-012**
  refutó para `/reconcile` y `/backfill`: **la compuerta vive en el handler, no
  en el router.** Que esquive `validateCargo` no significa que sea público.
- **El worker de reintentos lo esquiva igual, y ése no tiene permiso que valga:**
  `internal/cobranza/app/pago_retry_worker.go:186` —
  `w.svc.AplicarPago(ctx, p.ID(), uuid.Nil)` dentro del bucle sobre
  `pendientes` (`:181`), con `uuid.Nil` como actor. Es un camino **automático**:
  no hay sesión, no hay permiso y no hay nadie mirando. Cualquier pago que
  quedara pendiente con importe mayor al saldo se aplica en el siguiente drenado
  sin que nada compare.
- **El único piso en Go es *check-then-act*, y hay que nombrarlo así.** Donde
  `validateCargo` **sí** corre (`crear_pago_con_imagenes.go:93`), la secuencia
  es:
  1. `validateCargo` (`crear_pago.go:98-113`) llama
     `s.saldos.PorCargo(ctx, cargoDoctoCCID)` (`:99`) — **una lectura sin
     candado**: no hay `WITH LOCK`, no hay `FOR UPDATE`, nada retiene el cargo;
  2. compara `importe.GreaterThan(saldo.Saldo())` (`:109-111`);
  3. y **sólo después**, en `crear_pago_con_imagenes.go:188`, abre `runInTx`.

  Entre (1) y (3) cabe otro pago. El `SELECT ... WITH LOCK` que sí existe
  (`aplicar_pago.go:55` y `:93`) bloquea **la fila del pago**, no el cargo, así
  que no cierra esa ventana. Es la definición de libro de *check-then-act*: se
  comprueba una condición y se actúa sobre ella **fuera** de la sección crítica,
  confiando en que no cambió.
- **Los tres huecos son independientes.** Cerrar uno no cierra los otros: meter
  `validateCargo` dentro de `AplicarPago` tapa el endpoint y el worker pero deja
  el *check-then-act*; poner el `WITH LOCK` sobre el cargo tapa la carrera pero
  no los dos llamadores que ni siquiera validan. Un arreglo que sólo haga una de
  las dos cosas y se anuncie como cerrado, no lo cierra.
- **Y donde sí corre, corre fuera de la transacción:** en
  `crear_pago_con_imagenes.go` la validación está en `:93` y el `runInTx` abre en
  `:188`. Lee el saldo **sin `WITH LOCK`**: entre la lectura y la escritura cabe
  otro pago. (El `SELECT WITH LOCK` que sí existe, `aplicar_pago.go:55` y `:93`,
  bloquea **la fila del pago**, no el cargo.)
- **Consecuencia — INFERIDO:** un pago que exceda el saldo puede aplicarse por el
  worker o por el endpoint de administración sin que nadie compare contra el
  saldo, y dos capturas concurrentes pueden pasar las dos la validación de la
  captura. Es un camino plausible hacia el saldo negativo de E-COB-015.
- **Qué lo volvería MEDIDO:** aplicar por el endpoint de administración un pago
  mayor al saldo en dev y mirar el saldo resultante. **No se corrió.**
- **Guardada por:** — (sin prueba que fije el tope en el camino de `AplicarPago`)
- **Caduca si:** `AplicarPago` gana la validación, o `validateCargo` se mueve
  dentro de la transacción con bloqueo del cargo

### E-COB-017 · El catálogo `FORMAS_COBRO` no contiene los IDs que usan los pagos

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, clon local de producción (`mueblera-firebird`,
  `MUEBLERA.FDB`), sólo `SELECT`
- **Qué dice la tabla catálogo:**

  ```
  SELECT FORMA_COBRO_ID, NOMBRE FROM FORMAS_COBRO ORDER BY 1;

  FORMA_COBRO_ID  NOMBRE
  ==============  =====================
              67  Efectivo
              68  Cheque
              71  Crédito
           27773  TRASPASO CTA ANTERIOR
  ```

- **Qué usan los datos de verdad** (sobre la población exacta que el teléfono
  sincroniza — `s.SALDO > 0`, `CONCEPTO_CC_ID IN (87327, 27969)`, vía
  `FORMAS_COBRO_DOCTOS`):

  ```
  FORMA_COBRO_ID   PAGOS    CON_GPS
  ==============  ======   ========
             157  209894     194335   <- efectivo
           52569    4263       4163   <- transferencia
             158       8          7   <- cheque
          <null>       3          0
  ```

- **Ninguno de los cuatro IDs del catálogo aparece en los pagos**, y ninguno de
  los tres IDs reales aparece en el catálogo. Son universos disjuntos.
- **El código está BIEN, el catálogo es el que miente.**
  `msp-app-kt/feature/pagos/.../domain/model/HistorialDePagos.kt:225-227`
  declara `FORMA_EFECTIVO = 157`, `FORMA_CHEQUE = 158`,
  `FORMA_TRANSFERENCIA = 52569` — que es lo que los datos confirman. Quien
  "corrija" esas constantes contra `FORMAS_COBRO` rompe la app.
- **Por qué importa:** es otro caso de la regla de que **los nombres mienten**.
  Un agente que necesite el id de "transferencia" y lo busque en el catálogo
  —que es el lugar obvio— se lleva `67/68/71` y no encuentra un solo pago. La
  única fuente de verdad es `FORMAS_COBRO_DOCTOS` cruzada contra los pagos.
- **Control positivo:** la misma consulta sí devuelve filas y la suma de las
  cuatro categorías (209,894 + 4,263 + 8 + 3 = 214,168) coincide exactamente con
  el `COUNT(*)` independiente de la misma población, así que la consulta no está
  filtrando de más.
- **Corroboración independiente dentro del código (LEÍDO, 2026-09-24):**
  `msp-app-kt/core/database/.../dao/payment/PaymentDao.kt:17-23` documenta los
  mismos IDs desde otro módulo y sin conocer al anterior —
  *"157=efectivo, 158=cheque, 52569=transferencia, 137026=condonacion"*— y los
  usa inlineados en el SQL (`:144-149`). Son **dos lugares del código que
  coinciden entre sí y ambos discrepan del catálogo**, lo que deja poco margen a
  que el equivocado sea el código.
- **Límite honesto:** no se investigó **por qué** divergen (¿catálogo de otra
  empresa del mismo Firebird? ¿migración vieja?). Sólo se constata que divergen.
  **Qué lo volvería completo:** revisar si `FORMAS_COBRO` tiene columna de
  empresa y si hay más de una.
- **Guardada por:** — (hallazgo de medición; no hay prueba automática que lo fije)
- **Caduca si:** alguien puebla `FORMAS_COBRO` con los IDs reales, o Microsip
  renumera las formas de cobro

### E-COB-018 · Las coordenadas de los pagos: dónde viven, cuántas hay y qué tan dispersas son

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, clon local de producción (`mueblera-firebird`), sólo
  `SELECT`. **Límite de la fuente:** ese clon trae Microsip **hasta feb-2026**
  (enero 27,492 pagos, febrero 20,842, y de mayo a septiembre 25 filas en total,
  que son de prueba). Los números de abajo salen de 2.17 M de pagos históricos;
  **no son "la producción de hoy"**.
- **Control positivo de que el clon concuerda con producción:** el censo de
  `LAT` en cero da **68,526**, exactamente el número que
  `internal/cobranza/infra/ventfb/pagos_repo.go:695` documenta como medido en
  producción.

**Dónde viven.** `DOCTOS_CC.LAT` / `.LON`, de Microsip — **`VARCHAR(100)`**,
verificado contra `RDB$FIELDS` (tipo 37, longitud 100). Ojo: la nota que
circulaba decía `VARCHAR(20)`; ese es el de **otra** tabla,
`MSP_PAGOS_RECIBIDOS` (`migrations-firebird/000016_pagos_outbox.up.sql:39`).
`MSP_PAGOS_VENTAS.LAT` está **NULL en el 100 %** de las filas y es una trampa ya
documentada en `pagos_repo.go:215-227`. En el teléfono llegan ya numéricas:
`PaymentEntity.LAT/LNG` son `Double?`.

**Cuántas.** Sobre la población exacta que el teléfono sincroniza:

| | |
|---|---|
| pagos que el teléfono tiene | 214,168 |
| **con coordenada de verdad** | **198,505 (92.7 %)** |
| en cero (centinela "sin GPS") | 13,207 (6.2 %) |
| sin columna | 2,456 (1.1 %) |

El histórico completo es mucho peor (51 % de 2.17 M) porque el GPS entró en 2021
y no se generalizó hasta 2023 — **2018-2020 tienen cero**. Pero el teléfono no ve
ese histórico, así que **el número que manda es 92.7 %**.

**Cuántos puntos por cliente.** De 9,404 clientes con GPS: el **44 % tiene 21 o
más**, y sólo el 7.6 % tiene uno o dos. "Decenas de puntos" es la norma.

**Qué tan dispersos.** Barrido del radio de agrupación sobre 1,091 cuentas con
≥8 mediciones (26,342 puntos), midiendo el % de mediciones que caen en el grupo
mayor: 5 m → 49.4 %, 10 m → 69.4 %, 15 m → 76.6 %, 20 m → 79.5 %,
**30 m → 82.9 %**, 40 m → 84.4 %, 100 m → 87.5 %, 500 m → 92.7 %. El codo está
entre 25 y 40 m.

- **La parte incómoda, y es la que hay que citar:** **no hay un valle limpio**.
  El histograma de distancias decae de forma continua desde su pico en 5-10 m:
  **no existe una distancia donde el mundo se parta en dos**. Los 30 m son un
  codo, o sea un compromiso medido, no una frontera natural. Quien lo suba no
  está corrigiendo un error.
- **Dispersión del grupo-puerta:** mediana de 7.9 m al centro; su p95 interno
  tiene mediana 24.4 m y p90 entre clientes de 43.6 m — **varía de verdad**, que
  es lo que hace que el círculo de precisión valga la pena.
- **Guardada por:** `msp-app-kt/core/geo/src/test/.../AgrupadorDeLugaresTest.kt`
  (`el radio corta en treinta metros, ni mas ni menos`), con las distancias
  **literales**. Ver E-INF-009: la primera versión de esa prueba leía la
  constante y por eso no podía fallar.
- **Caduca si:** cambia la generación de teléfonos de la flota, o el GPS pasa a
  guardarse con otra precisión
