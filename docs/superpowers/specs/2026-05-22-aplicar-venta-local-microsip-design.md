# Diseño — Aplicar venta local en Microsip (`MSP_VENTAS → DOCTOS_PV`)

> Spec de diseño. Estado: aprobado para escribir plan de implementación.
> Fecha: 2026-05-22.

## Contexto y objetivo

`msp-api` captura **ventas locales** (`MSP_VENTAS`) que los vendedores levantan en
campo. Hoy esas ventas viven solo en las tablas `MSP_*` y **nunca se materializan
en Microsip**. Este feature agrega la capacidad de **materializar una venta local
aprobada como una venta real de Punto de Venta de Microsip** (`DOCTOS_PV` + su
cascada), vía un endpoint on-demand.

La mecánica exacta de creación en Microsip ya fue capturada por ingeniería inversa
(trace de ejecución) y está documentada en:
- [`microsip-crear-venta-paso-a-paso.md`](../../microsip-crear-venta-paso-a-paso.md) — receta de escritura (fases, columnas, folio, particulares, enganche).
- [`microsip-venta-flow.md`](../../microsip-venta-flow.md) — análisis del trace.

Como `msp-api` y Microsip comparten **la misma base Firebird**, "materializar" es
escribir a la familia `DOCTOS_PV` desde la misma conexión/transacción — no es un
sync cross-DB.

## Alcance

**Dentro:**
- Endpoint para aplicar una venta local aprobada → crea `DOCTOS_PV` (contado y crédito), inventario, CxC, particulares y pago de enganche.
- Rediseño del modelo de estados de la venta local (3 dimensiones).
- Capa de configuración del mapeo (zona→caja, cajero, sucursal, frecuencia→lista).
- Idempotencia y manejo de errores.
- Migración de datos del modelo de estados.

**Fuera (por ahora):**
- Cancelar/reversar una venta **ya aplicada** en Microsip (requiere reversa del `DOCTOS_PV`).
- Materialización automática/batch (este feature es on-demand).
- UI/panel (solo el endpoint).

## Modelo de estados (3 dimensiones separadas)

Decisión basada en cómo lo hacen los referentes (Shopify separa
financial/fulfillment status; commercetools state machines; cancelar ≠ borrar):
**3 dimensiones independientes**, no un enum gigante.

| Dimensión | Campo | Valores | Concepto |
|---|---|---|---|
| Existencia (técnico) | `STATUS` | `active` \| `deleted` | soft-delete; `deleted` solo para registros creados por error |
| Negocio | `SITUACION` | `borrador → revisada → aprobada → cancelada` | ciclo de la venta local |
| Integración | `SINCRONIZACION` | `pendiente → aplicada` | situación frente a Microsip |
| Artefactos | `MICROSIP_DOCTO_PV_ID`, `MICROSIP_FOLIO`, `MICROSIP_APLICADA_AT` | int / char(9) / timestamp | resultado de materializar (respaldan `SINCRONIZACION=aplicada`) |

### Transiciones de `SITUACION`
- `borrador → revisada` (se envía a revisión)
- `revisada → aprobada` (revisor/aprobador OK)
- `revisada → borrador` (rechazo / regresa a corrección)
- `borrador|revisada|aprobada → cancelada`
- `aplicada` no es un valor de `SITUACION` (vive en `SINCRONIZACION`)

### Transiciones de `SINCRONIZACION`
- `pendiente → aplicada` (materialización exitosa; única transición que toca Microsip; setea los artefactos atómicamente)
- En fallo: queda `pendiente` (reintentable); el error se registra en `failed_intents`.

### Invariantes
- `SINCRONIZACION=aplicada` ⟺ `MICROSIP_DOCTO_PV_ID` y `MICROSIP_FOLIO` no nulos.
- Solo se materializa si `STATUS=active` **y** `SITUACION=aprobada` **y** `SINCRONIZACION=pendiente`.
- Una venta `aplicada` no se cancela por esta vía (fuera de alcance).

## Endpoint y flujo

**`POST /ventas/{id}/aplicar`** — síncrono. Permiso `ventas:aplicar` (roles
**admin** y **oficina**; el vendedor de campo NO).

Todo ocurre en **una transacción Firebird** (`READ_COMMITTED`, `READ_WRITE`):

1. `SELECT … FROM MSP_VENTAS WHERE ID=? WITH LOCK` → toma lock de fila (guard anti doble-submit; un 2º request concurrente espera aquí).
2. **Precondiciones:**
   - `STATUS≠active` → `409` ("venta no activa").
   - `SITUACION≠aprobada` → `409` ("solo ventas aprobadas se pueden aplicar").
   - `SINCRONIZACION=aplicada` → **devuelve** `MICROSIP_FOLIO`/`DOCTO_PV_ID` existentes (idempotente, no recrea). Fin.
3. **Resolver mapeo** (capa de config + datos de la venta).
4. **Materializar** (fases del trace, ver receta): folio `FOLIOS_CAJAS` → `INSERT DOCTOS_PV (APLICADO='N')` → `DOCTOS_PV_DET` (por producto) → totales → `DOCTOS_PV_COBROS` → `LIBRES_VTA_PV` → **`UPDATE APLICADO='S'`** (dispara cascada: inventario `DOCTOS_IN` + CxC `DOCTOS_CC`) → `FOLIOS_CAJAS +1` → obtener cargo `DOCTOS_CC` → `LIBRES_CARGOS_CC` (particulares completos) → si hay enganche: `DOCTOS_CC` concepto Enganche (24533) + `IMPORTES_DOCTOS_CC` + `FOLIOS_CONCEPTOS +1`.
5. **`UPDATE MSP_VENTAS`** → `SINCRONIZACION='aplicada'`, `MICROSIP_DOCTO_PV_ID`, `MICROSIP_FOLIO`, `MICROSIP_APLICADA_AT`.
6. **COMMIT** atómico (todo o nada). En cualquier error de 3-5 → **ROLLBACK**, registrar en `failed_intents`, responder error; la venta sigue `pendiente`.

**Garantía:** `DOCTOS_PV` y el `UPDATE` a `MSP_VENTAS` van en la misma transacción
→ imposible quedar a medias (incrementos de folio vía `FOLIOS_*` van dentro también).

## Mapeo de campos

| Destino Microsip | Origen |
|---|---|
| `CLIENTE_ID`, `CLAVE_CLIENTE` | de la venta / cliente |
| `ALMACEN_ID` | vendedor (`MSP_USUARIOS.AlmacenID`) / producto |
| `FORMA_COBRO_ID` | `tipo_venta`: CONTADO→67 (Efectivo), CREDITO→71 (Crédito) |
| `CAJA_ID` | **zona→caja** (config; `MSP_VENTAS.ZONA_CLIENTE_ID`) |
| `CAJERO_ID`, `SUCURSAL_ID` | **config** (valores definidos, cambiables) |
| `FOLIO` | `FOLIOS_CAJAS` (serie + consecutivo) |
| `LIBRES_CARGOS_CC.*` (particulares) | plan de crédito: `ENGANCHE`, `PARCIALIDAD`, `PLAZO_MESES`, vendedores, precio contado, aval, observaciones |
| `FORMA_DE_PAGO` (lista) | **frecuencia→ID de lista** (config; ej. Semanal→33824) |
| pago de enganche | si `ENGANCHE>0`: `DOCTOS_CC` concepto 24533 + importe = enganche |

## Configuración ("fácil de cambiar")

Una/varias **tablas de config** (`MSP_*`), no `.env`, porque el mapa zona→caja
tiene varias filas y debe cambiarse sin redeploy:
- `MSP_CFG_ZONA_CAJA` (o similar): `ZONA_ID → CAJA_ID`.
- Config de un solo valor: `CAJERO_ID`, `SUCURSAL_ID` (tabla de config key-value o columnas).
- `MSP_CFG_FRECUENCIA_FORMA_PAGO`: `frecuencia (Semanal/Quincenal/Mensual) → FORMA_DE_PAGO_ID` de lista, y `PLAZO_MESES → CREDITO_EN_MESES_ID` si aplica.

## Ubicación del código (módulo `ventas`)

Sigue vertical slices; la materialización opera sobre el agregado Venta:
- `internal/ventas/domain/` — enums nuevos (`Situacion`, `Sincronizacion`, `EstadoRegistro`) y las reglas de transición; el `VentaStatus` actual se redefine.
- `internal/ventas/app/` — `AplicarVentaService` (command) con la orquestación + precondiciones.
- `internal/ventas/infra/microsip/` (o extender `ventfb`) — adapter que escribe la familia `DOCTOS_PV` (la receta), aislando el encoding legacy.
- `internal/ventas/infra/venthttp/` — endpoint `POST /ventas/{id}/aplicar`.
- Config: repos para las tablas `MSP_CFG_*`.

> **Excepción consciente a "no logic in DB" (CLAUDE.md):** esa regla aplica a
> *nuestras* tablas. Microsip **sí** tiene lógica (triggers, cascada de aplicación)
> y dependemos de ella a propósito. El adapter `infra/microsip/` documenta esta
> frontera. Encoding: tablas legacy Microsip son `ISO8859_1`; aislar la conversión
> (ver `ENCODING_HANDLING.md`). Fechas: `firebird.ToWallClock` al escribir.

## Idempotencia y concurrencia
- **Idempotencia:** `SINCRONIZACION=aplicada` + artefactos guardados; segunda llamada devuelve el folio existente.
- **Concurrencia:** `SELECT … WITH LOCK` sobre la fila `MSP_VENTAS` dentro de la transacción serializa los intentos.

## Manejo de errores
- Fallo en materialización → ROLLBACK total (no queda `DOCTOS_PV` ni cambios), registrar detalle en `failed_intents` (mig. 000004 existente), responder error. Venta sigue `pendiente` → reintentable.
- Sin estado `error` en los enums (se evita; el detalle vive en `failed_intents`).

## Migración de datos
1. **Columnas nuevas** en `MSP_VENTAS`: `SITUACION`, `SINCRONIZACION`, `MICROSIP_DOCTO_PV_ID`, `MICROSIP_FOLIO`, `MICROSIP_APLICADA_AT`.
2. **Repurpose de `STATUS`:** valores actuales (`borrador/aprobada/cancelada`) → mover a `SITUACION`; `STATUS` pasa a `active` para todos (no hay `deleted` aún). Los `cancelada` → `STATUS=active, SITUACION=cancelada`.
3. **`SINCRONIZACION='pendiente'`** para todas las existentes (ninguna aplicada aún).
4. Tablas de config nuevas (`MSP_CFG_*`) + datos iniciales (mapa zona→caja, cajero, sucursal, frecuencias).
5. Tocar domain + repos + tests del módulo `ventas` (el enum `VentaStatus` cambia de significado).

## Riesgos / consideraciones
- **Motor Firebird Super compartido:** evitar operaciones que afecten a todas las DBs; nunca reiniciar el servicio como parte del flujo.
- **Preferencias de empresa destino:** la cascada de inventario/CxC depende de `INTEG_IN_PV`/`INTEG_CC_PV='S'` y de que el cliente no sea el "cliente eventual" (ver receta). Validar/asumir en producción.
- **Catálogos de lista** (`FORMA_DE_PAGO`, `CREDITO_EN_MESES`, etc.): no tienen FK; usar IDs que existan como opción en Microsip. Falta mapearlos completos en la config.
- **Cliente/Artículo deben existir en Microsip** (mismos IDs). Si no, la materialización falla — manejar como error reintentable.

## Pendientes a resolver en el plan de implementación
- Nombres exactos de las tablas/columnas de config y la migración.
- Mapa completo frecuencia→`FORMA_DE_PAGO` y `PLAZO_MESES→CREDITO_EN_MESES`.
- Validaciones de mapeo faltante (zona sin caja, etc.) → error claro.
- Tests E2E contra el Firebird de pruebas (rollback) replicando lo ya validado a mano.
