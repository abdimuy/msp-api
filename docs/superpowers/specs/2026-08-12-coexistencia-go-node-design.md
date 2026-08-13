# Coexistencia de `msp-api` (Go) con `sys_msp_backend` (Node) — diseño

- **Fecha:** 2026-08-12
- **Estado:** aprobado, listo para plan de implementación
- **Alcance:** despliegue del API Go en producción, convivencia con el Node legacy y ruta para apagarlo

## 1. Objetivo

Poner el API Go en producción conviviendo con el Node legacy, sin ventana de apagón y sin
que ambos escriban el mismo dato, y dejar trazada la ruta que permite apagar el Node.

**Fuera de alcance:** reescribir funcionalidad que el Node ya cubre y el Go no. Eso se
inventaría en §5 y se prioriza aparte.

## 2. Invariante

> **Nunca dos sistemas escribiendo el mismo dato.**

Todo el orden de fases se deriva de esa regla. Cuando un dominio migre, se apaga el del
Node **antes** de encender el del Go, nunca al revés y nunca solapado.

## 3. Estado actual (verificado 2026-08-12)

### 3.1 Topología

| | Node legacy | Go (prueba) | Go (producción) |
|---|---|---|---|
| Ruta | `C:\projects\sys_msp_backend` (corre desde `dist/`) | `C:\msp-api-test\` | `C:\msp-api\` (no existe) |
| Puerto | `3001` | `3011` | `3010` (propuesto) |
| Base | `MUEBLERA_SNP.FDB` + MongoDB (sync 30 s) | `MUEBLERA_TEST.FDB` | `MUEBLERA_SNP.FDB` |
| Arranque | **no documentado — confirmar** | `run.bat` + tarea programada `msp-api-test` | `run.bat` manual (§4.2) |
| Público | `msp2025.loclx.io` | `apidev.loclx.io` | sin resolver (§6.1) |

El Node conecta a Firebird con `node-firebird` 1.1.9, que obligó a relajar dos ajustes de
FB5 (`WireCrypt = Enabled`, `AuthServer = Srp256, Srp`). Ya aplicados; ver
`docs/ops/legacy-api-notas.md`.

### 3.2 Enrutamiento: lo deciden los clientes

No hay ni habrá proxy inverso. Cada cliente elige su API por configuración:

- **App Android:** `LEGACY_BASE_URL` y `V2_BASE_URL`, ambos `buildConfigField` por flavor.
- **Web/escritorio:** `VITE_URL_API` y `VITE_URL_API_V2`.

`ApiProvider.kt:33` define `DEFAULT_BASE_URL = BuildConfig.LEGACY_BASE_URL`: **la base por
defecto de la app es el Node.** Al Go solo van tres cosas.

| Función | Cliente Go | Interruptor | Valor en `prod` |
|---|---|---|---|
| Ventas locales | `POST /v2/ventas` | **ninguno** | siempre Go |
| Pagos | `POST /v2/cobranza/pagos` | `PAGOS_USE_V2` | `false` |
| Visitas (alta) | `POST /v2/visitas` | `VISITAS_USE_V2` | `false` |

Los dos fronts traen el mismo marcador inválido a propósito:

```
app/build.gradle.kts    V2_BASE_URL = "https://todo-go-prod-host.invalid/"
web .env.production     VITE_URL_API_V2 = https://todo-go-prod-host.invalid
```

**Consecuencia:** hoy, en producción, nada consume el Go. Y como ventas no tiene
interruptor, una build de producción intentaría subir ventas a un host inválido sin caer
al Node.

### 3.3 Los interruptores son de tiempo de compilación

`PAGOS_USE_V2` y `VISITAS_USE_V2` son `buildConfigField`, no configuración remota. Cada
cambio es un APK nuevo y **cada reversión es otro APK**. Hay que agrupar: no sacar tres
versiones donde cabe una.

### 3.4 MongoDB está sostenido por un campo que nadie lee

Único consumidor verificado:

```
SalesApi.kt:23      GET /ventas/getAllVentasByZona/{zona}
network.ts:83       controller.getVentasByZona
controller.ts:275   const pagos = await pagosStore.getPagosByVentaIdsMongo(ventasIds)
                    → MongoDB
```

Pero `SalesViewModel.syncSales` solo usa `body.garantias` y `body.eventosGarantias`. Se
verificó que **no hay ningún consumidor** de `body.pagos`, `body.ventas` ni
`body.productos` en la app. El payload de Mongo se descarga y se descarta.

**Mongo puede morir con un cambio en el Node, sin APK:** devolver `pagos: []`, o
descomentar la consulta a Firebird que está justo encima de esa línea. Sale de la ruta
crítica y puede hacerse en cualquier momento, independiente del resto.

### 3.5 El botón «SINCRONIZAR GARANTÍAS»

`HomeFooterSection.kt:150` → `onSyncSales` → `syncSales`. Es el último cliente real del
endpoint legacy de zona. Además de guardar garantías hace dos cosas que su nombre no
anuncia:

1. `visitsStore.deleteUploadedVisits()` — **único llamador en toda la app**. El
   `PendingVisitsWorker` marca `GUARDADO_EN_MICROSIP` pero no borra. Si se quita esta
   línea sin reemplazo, la tabla `visits` de Room crece sin tope en el teléfono.
2. `saveLastSyncDate(...)` — se lee en `HomeStartWeekSection.kt:29` y **se muestra**. El
   cobrador puede ver un sello reciente aunque solo hayan bajado garantías.

**Acción:** mover la poda a `PendingVisitsWorker`, junto al marcado de
`GUARDADO_EN_MICROSIP`. El sello se deja mientras el botón exista.

## 4. Despliegue

### 4.1 Un proceso, un directorio, un lanzador

Tres procesos independientes en tres puertos. **No se comparte el `.bat`:**

- La última línea de `run.bat` es bloqueante (`msp-api.exe serve > api.log 2>&1`), así que
  dos APIs en un archivo implican que la segunda nunca arranca.
- Los `set` de un `.bat` los hereda todo lo que se lance después: el Node arrancaría con
  `FB_DATABASE` y `APP_PORT` del Go encima. Falla que no truena, solo apunta mal.
- Reiniciar uno reiniciaría al otro.

### 4.2 Arranque manual, por decisión

El Go de producción se levanta a mano, sin tarea programada. Un archivo maestro invoca a
los lanzadores de cada API con `start ... cmd /c`, que le da a cada una su propia copia
del entorno, más una comprobación con `tasklist` para no arrancar dos veces.

**Consecuencias asumidas:** tras un reinicio del server nadie levanta las APIs hasta que
alguien entre y las arranque; y los procesos lanzados desde una sesión **mueren al cerrar
sesión** (hay que desconectar sin cerrar sesión).

Variante disponible si se quiere revertir sin tocar archivos: dejar la tarea programada
creada pero deshabilitada y que el `.bat` haga `schtasks /Run`. Corre como SYSTEM,
sobrevive al cierre de sesión, y habilitar el arranque automático es un cambio de estado.

### 4.3 Divergencias documentales a corregir

- **`CLAUDE.md` §5 dice `nssm`; la realidad es Tarea Programada.** Igual
  `docs/ops/inventario-cutover.md`, que dice «reiniciar el servicio `nssm`».
- **`inventario-cutover.md` asume que el Go escucha en `:3001`**, el puerto del Node. Se
  escribió para un mundo posterior al Node; leerlo durante la convivencia provoca colisión
  de puertos. Corregir antes de que alguien lo siga.

## 5. Matriz de paridad (parcial)

Lo que el Node todavía hace y el Go no. **Esta lista no está completa** — son los huecos
encontrados al investigar, no un barrido sistemático.

| Hueco | Quién depende | Reemplazo |
|---|---|---|
| `GET /ventas/getAllVentasByZona/{zona}` | App (botón de garantías) | Consulta de garantías en el Go — es el módulo que se está construyendo |
| `GET /visitas/pagos-visitas` | Web/escritorio (`getPagosAndVisitas.ts:36`, vía `URL_API`) | Lectura de visitas en el Go (§6) |
| `POST ventas-locales` | App, **si `LocalSaleSyncHandler` sigue vivo** | `POST /v2/ventas`, ya existe |

**El módulo de garantías cambia de prioridad:** no es solo funcionalidad nueva, es la
última pieza que permite apagar `getAllVentasByZona`.

## 6. Visitas: lectura y sincronización

### 6.1 Estado

`internal/visitas` expone **un solo endpoint**: `POST /v2/visitas`. Verificado en
`main`, `feat/visitas` y `origin/feat/visitas` (un `huma.Register`, method POST en las
tres), en todos los `routes.go` del repo, en todo el código que toca `MSP_VISITAS`, y en
la app (cero `@GET` de visitas). El worktree `msp-api-visitas` ya no existe en disco; su
rama `feat/visitas` sigue existiendo y expone las mismas rutas que `main` — no se perdió
trabajo ahí (verificado sobre `routes.go`, no sobre la rama completa).

El lado de lectura existe, pero **en el Node**: `GET /visitas/`, `POST /visitas/` y
`GET /visitas/pagos-visitas`.

### 6.2 La referencia a la venta ya está en la tabla

`MSP_VISITAS.IMPTE_DOCTO_CC_ID` **no guarda un id de importe: guarda el id de la venta.**

```kotlin
VisitFactory.kt:53   IMPTE_DOCTO_CC_ID = sale.DOCTO_CC_ACR_ID
```

`DOCTO_CC_ACR_ID` es el cargo al que se abona, y se usa como id de venta en toda la app
(`loadSaleDetails`, `getPaymentsBySaleId`, navegación a `sale_details`). Coincide con la
llave de nuestro caché: `MSP_SALDOS_VENTAS` tiene `PRIMARY KEY (DOCTO_CC_ID)` y es
explícitamente el cargo (`NATURALEZA_CONCEPTO='C'`).

**Mismo espacio de ids.** El `by-ids` funciona sin migración, sin backfill y sin tabla
puente.

**No renombrar la columna** — el Node también escribe `MSP_VISITAS`. La corrección vive en
el DTO del Go:

```go
CargoID *int `json:"cargo_id" doc:"DOCTO_CC_ID del cargo (NATURALEZA_CONCEPTO='C') al que corresponde la visita; nulo si no se ancló a una venta"`
```

No llamarlo `venta_id`: esa ambigüedad es la que produjo el defecto de §6.3.

### 6.3 Defecto heredado, para no repetirlo

La consulta `GET_PAGOS_AND_VISITAS_BY_FECHA` del Node alias-ea como `VENTA_ID` dos cosas
distintas:

```sql
COALESCE(MSP_VISITAS.IMPTE_DOCTO_CC_ID, 0) AS VENTA_ID   -- el cargo (correcto)
DOCTOS_CC.DOCTO_CC_ID                AS VENTA_ID   -- el documento del abono (incorrecto)
```

Como cargos y abonos comparten tabla y espacio de ids en `DOCTOS_CC`, lo que devuelve la
rama de pagos es un id válido, del tipo correcto, que apunta al documento equivocado.
Debería ser `IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID`. Ese error no se manifiesta solo.

### 6.4 Modelo de acotamiento — restricción de diseño, no detalle

El sync de cobranza acota con tres cosas distintas:

```go
SyncPorZona(ctx, zonaID, cursor time.Time, afterID, limit int, desde time.Time)
```

- `zonaID` — alcance
- `desde` — ventana (`FECHA_CARGA_INICIAL` del dispositivo)
- `cursor` + `afterID` — incremental por keyset sobre `UPDATED_AT`

Y encima, el filtro que hace que el historial no crezca: **activos (`SALDO > 0`) siempre
viajan; los saldados no se mandan.** La retención no es «guardar N meses»: una venta
desaparece del sync cuando se liquida.

**Para visitas, el alcance se deriva del de ventas, no se calcula aparte.** El dispositivo
ya sabe qué cargos tiene y pide sus visitas:

```
POST /v2/visitas/by-cargos   { cargo_ids: [...] }
  → WHERE IMPTE_DOCTO_CC_ID IN (...)
```

Alcance y retención salen gratis y no pueden divergir: si un cargo se liquida y sale del
set del dispositivo, sus visitas salen con él en el mismo ciclo.

**Por qué no copiar `zona + desde`:** obligaría a `internal/visitas` a saber qué es un
cargo activo y qué es la ventana de un cobrador, reglas que viven en `internal/cobranza`.
Duplicarlas crea dos verdades; importarlas rompe la rebanada vertical. Con `by-ids`,
`internal/visitas` recibe enteros y devuelve filas.

**Dos cabos que hay que amarrar explícitamente:**

1. **Visitas sin cargo.** `IMPTE_DOCTO_CC_ID` es nullable. No entran por `by-ids` y
   necesitan regla propia — lo natural es por `CLIENTE_ID` de los clientes que el
   dispositivo ya tiene, con la misma ventana `desde`. Decidirlo o quedan huérfanas.
2. **La purga en el teléfono debe cascadear.** Cuando la reconciliación de Room saque un
   cargo, tiene que sacar sus visitas en la misma operación.

### 6.5 Dimensionamiento

Sin migración de base. El grueso está en Android, no en el backend.

| Parte | Alcance | Tamaño |
|---|---|---|
| Go | `by-cargos` (Android), lectura por zona y rango (web), publicar al `eventbus` en el POST, DTO, auth, pruebas | 2-3 días |
| Android | `VisitasSyncManager` + suscriptor SSE + reconciliación en Room | 4-6 días |
| Web | Apuntar `getPagosAndVisitas.ts` a los endpoints del Go | medio día |

Son **dos endpoints de lectura con propósitos distintos**, no uno reutilizado: `by-cargos`
acota por lo que el dispositivo tiene (§6.4) y sirve a Android; la lectura por zona y rango
de fechas replica lo que la web pide hoy, que no tiene noción de dispositivo. La web además
tendrá que componer dos llamadas —una a cobranza y otra a visitas— porque un endpoint
combinado obligaría a que un módulo importe al otro.

Referencia de tamaño en Android: `CobranzaSyncManager` 600 líneas, `CobranzaSseSubscriber`
451, resto 189. Visitas sale más ligero: no hay tombstones ni el problema del gemelo
UUID↔numérico.

**El tiempo real sale más barato que en pagos.** Para pagos, el SSE se alimenta de un
vigilante sobre las tablas caché que llenan los triggers, porque quien escribe es Microsip.
En visitas escribimos nosotros: el handler del `POST /v2/visitas` sabe el instante exacto y
publica al `eventbus` directo.

**Costura de entrega:** `by-cargos` + sync de fondo resuelve el grueso del valor; el SSE es
una capa encima que no obliga a rehacer nada.

**Dónde vive el código Android:** en `:core:*` / `:feature:*`, no en `:app`.
`CobranzaSyncManager` está en la zona legacy y no se debe extender ahí.

## 7. Plan de fases

### Fase 0 — Cerrar incógnitas

1. **¿Qué ruta de ventas locales está viva?** `PendingLocalSalesWorker` (Go, sin flag) o
   `LocalSaleSyncHandler` (Node). Decide la rama de las fases 4-6.
2. **Host del Go de producción** (§8.1). Prerequisito, no detalle.
3. **Cómo se levanta el Node hoy.**
4. **Completar la matriz de paridad** de §5.

### Fase 1 — Base de datos de producción (irreversible)

`gbak` de prod. Luego las 52 migraciones sobre `MUEBLERA_SNP.FDB`, **con las
`000051/052/053` en la misma ventana**.

El riesgo no son las tablas `MSP_*` nuevas, que son aditivas: son los triggers y
procedimientos que `000010` y `000013` instalan sobre tablas vivas de Microsip
(`DOCTOS_CC`, `IMPORTES_DOCTOS_CC`, `CLIENTES`). Todos usan `WHEN ANY DO` para que un error
del caché no tumbe la transacción de Microsip.

Las grants no son opcionales: sin ellas `Cxc.exe` truena al cobrar — ya ocurrió en TEST.

**El smoke es abrir Microsip y cobrar**, no que la API arranque.

Antes de migrar, re-verificar la colisión de tablas `MSP_*` en prod: el runbook advierte
que ese conjunto cambia y que no se asuma.

### Fase 2 — Go arriba, sin consumidores

`C:\msp-api\` en `:3010` contra la base de producción, con el `.bat`. Decidir qué workers
se habilitan — escriben, y el server ya se ha saturado antes.

**La verificación es SQL, no HTTP:** que `MSP_SALDOS_VENTAS` se llene y crezca al cobrar en
Microsip. Eso confirma que la Fase 1 quedó bien.

### Fase 3 — Llenar el marcador en los dos fronts

`V2_BASE_URL` y `VITE_URL_API_V2`.

### Fases 4-6 — Según la Fase 0.1

**Si `LocalSaleSyncHandler` está vivo** (ventas negociable):

| | Qué | Por qué ahí |
|---|---|---|
| 4 | Pagos y visitas: encender los flags | Tienen retorno al Node; sirven de canario bajo carga real |
| 5 | Ventas | Con la infraestructura ya probada |
| 6 | Sync de zona → apagar Mongo | Cuando todos los cobradores estén actualizados |

**Si solo está vivo `PendingLocalSalesWorker`** (ventas forzoso): ventas pasa a Fase 4 y se
pierde el canario. En ese caso conviene **agregar un flag `VENTAS_USE_V2`** para recuperar
el orden de arriba. Es un cambio chico que compra reversibilidad.

### Fase 7 — Apagar el Node

Instalado y detenido como vía de regreso, no desinstalado.

### Secuenciación con el trabajo de visitas

El sync de visitas **no bloquea el despliegue**: está en la ruta de *apagar el Node*, no en
la de *desplegar el Go*. Las fases 1 y 2 no necesitan ningún APK, así que el trabajo de
visitas corre en paralelo, no detrás.

## 8. Riesgos

### 8.1 Reusar el túnel de dev apunta APKs de prueba a producción

No hay túnel ni puerto extra, así que `apidev.loclx.io` pasaría a apuntar al Go de
producción. Pero el flavor `devserver` tiene esto quemado en tiempo de compilación:

```kotlin
V2_BASE_URL = "https://apidev.loclx.io/"
PAGOS_USE_V2 = true
```

**El día que se repunte ese túnel, los APKs de prueba instalados en campo empiezan a
escribir pagos en producción**, sin recompilar y sin que nadie lo note. Hay una cohorte de
prueba con `v2.13.0-dev` distribuido.

Mitigaciones, a elegir antes de repuntar: desinstalar los `devserver` de campo; sacar un
`devserver` nuevo apuntando a otro lado; o dar salida al Go de producción por
`msp2025.loclx.io` con prefijo de ruta y no darle túnel propio.

**Efecto colateral asumido:** el entorno de prueba se queda sin salida pública.

### 8.2 Doble creación de traspasos

Si ventas migra al Go y el Node sigue creando traspasos, el inventario se descuenta doble
del almacén origen. Hay que desactivar `crearTraspaso` en el flujo de `ventasLocales`
(`src/components/traspasos/controller.ts`) antes de encender ventas en el Go. Documentado
en `docs/ops/inventario-cutover.md`.

### 8.3 `MSP_PAGOS_RECIBIDOS` la escriben los dos

Existe en producción, la escribe el Node, y la migración `000013` le cuelga un trigger que
alimenta nuestro caché. Mientras convivan, el caché del Go se alimenta de lo que escribe el
Node. Los dos escribiendo ahí es doble cobro.

## 9. Abierto

1. Qué ruta de ventas locales está viva en la app (Fase 0.1).
2. Host del Go de producción (§8.1).
3. Cómo se levanta el proceso Node hoy.
4. Matriz de paridad completa — §5 es parcial.
5. Regla para las visitas sin cargo (§6.4).
6. Qué workers se habilitan en producción (Fase 2).
7. Si se agrega `VENTAS_USE_V2`.

## 10. Referencias

- `CLAUDE.md` §1 (sin lógica en la base), §5 (restricciones de stack)
- `docs/deploy-test-server-runbook.md` — despliegue del entorno de prueba
- `docs/ops/legacy-api-notas.md` — el Node y su driver de Firebird
- `docs/ops/inventario-cutover.md` — traspasos; **corregir el puerto `:3001` y `nssm`**
- ADR-0006 (excepción de triggers), ADR-0007 (canal push + watermark de cobranza)
- ADR-0003 (almacenamiento en filesystem local)
