# Cutover runbook — módulo `inventario`

Cuándo aplica: el día que se decide reemplazar el endpoint Node legacy
(`sys_msp_backend/src/components/traspasos`) por el módulo Go nuevo
(`internal/inventario`). El cambio activa stock validation + traspaso
automático dentro de la transacción de `crear_venta`, y cambia el almacén
que `aplicar_venta` referencia en `DOCTOS_PV`.

## Resumen del cambio

Antes:
1. `POST /v2/ventas` solo inserta `MSP_VENTAS_*` en Firebird.
2. Operadora aprueba → `aplicar_venta` escribe `DOCTOS_PV/DOCTOS_PV_DET`
   apuntando al almacén origen del producto; los triggers internos de
   Microsip generan un `DOCTOS_IN` salida del origen.
3. El sistema legacy Node crea, *dentro de su propia* transacción de
   `POST /traspasos`, el `DOCTOS_IN` de salida + entrada al almacén 11058
   que reserva el stock.

Después (con este módulo activo):
1. `POST /v2/ventas` valida stock en almacén origen (`READ COMMITTED NO
   WAIT`), inserta `MSP_VENTAS_*` y crea automáticamente el traspaso a
   `ALMACEN_DESTINO_VENTAS_ID=11058` — todo en una sola transacción.
2. Operadora aprueba → `aplicar_venta` escribe `DOCTOS_PV/DOCTOS_PV_DET`
   apuntando al 11058 (porque el stock ya está ahí). El descuento ahora
   es del 11058 en lugar del origen — pero el resultado neto sobre
   `SALDOS_IN` es el mismo: -1 del origen, 0 del 11058.
3. Cancelar venta antes de aplicarla emite un traspaso reverso 11058 →
   origen, también dentro de una sola transacción.

## Prerrequisitos

- **Coordinación con el legacy Node** — el endpoint
  `POST /traspasos` y/o el código que llama a `traspasos/store.ts` desde
  el flujo de `ventasLocales` deben estar deshabilitados antes del
  despliegue del Go. Dos opciones:
  - Eliminar la llamada `crearTraspaso(...)` de
    `ventasLocales/store.ts` (commit en `sys_msp_backend`), o
  - Setear el flag interno `omitirTraspaso=true` si existe.

  Si ambos sistemas crean traspasos para la misma venta el inventario
  queda doblemente descontado del origen.

- **Variables de entorno** del API Go deben estar pobladas. Defaults
  coinciden con el legacy:

  | Variable | Default | Descripción |
  |---|---|---|
  | `INVENTARIO_ALMACEN_DESTINO_VENTAS_ID` | `11058` | Almacén reservado para stock vendido pero no aplicado. |
  | `INVENTARIO_CONCEPTO_IN_SALIDA_ID` | `36` | `CONCEPTO_IN_ID` para la salida del traspaso. |
  | `INVENTARIO_CONCEPTO_IN_ENTRADA_ID` | `25` | `CONCEPTO_IN_ID` para la entrada del traspaso. |
  | `INVENTARIO_SUCURSAL_ID` | `225490` | `SUCURSAL_ID` estampado en `DOCTOS_IN`. |

  Verificar en `.env.prod` y `compose.yml`.

- **Migración `000028_create_gen_mst_folio`** aplicada
  (`make fb-migrate-status` debe listarla en `applied`). En producción
  Microsip ya tiene el generador; la migración es idempotente y no
  altera nada si el generador existe.

- **Migración `000027_create_msp_ventas_traspasos`** aplicada.

## Cleanup pre-deploy

Solo si el plan acordado fue arrancar desde cero (sin migrar el
metadata de ventas que el legacy haya creado en Firebird).

```sql
-- Firebird (MUEBLERA.FDB), como SYSDBA:
DELETE FROM MSP_VENTAS_TRASPASOS;
DELETE FROM MSP_VENTAS_IMAGENES;
DELETE FROM MSP_VENTAS_PRODUCTOS;
DELETE FROM MSP_VENTAS_COMBOS;
DELETE FROM MSP_VENTAS_VENDEDORES;
DELETE FROM MSP_VENTAS;
COMMIT;
```

```sql
-- Postgres (msp-postgres), opcional, recomendado para mantener limpios
-- los caches del idempotency middleware y la cola de failed-intents:
TRUNCATE failed_intents CASCADE;
TRUNCATE idempotency_keys;
```

Las ventas ya aplicadas en Microsip (`DOCTOS_PV`, `CLIENTES` creados,
`DOCTOS_IN` generados) **no se tocan** — viven en sus tablas de
Microsip y permanecen consistentes.

## Pasos del despliegue

1. Confirmar que el legacy Node está deshabilitado (ver "Coordinación"
   arriba).
2. Aplicar migraciones Firebird:
   ```
   make fb-migrate-up
   ```
   Esperar la línea `✔ Firebird migrations applied`.
3. Ejecutar el cleanup SQL si aplica (ver sección anterior).
4. Desplegar el binario:
   ```
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o api.exe ./cmd/api
   ```
   Reemplazar el binario antiguo en el servidor Windows y reiniciar el
   servicio `nssm` correspondiente.
5. Verificar arranque limpio en los logs:
   ```
   INFO firebird: connected ...
   INFO api: listening on :3001
   ```

## Verificaciones post-deploy

### A) Stock check rechaza ventas inválidas

Con un artículo cuya existencia en el almacén origen sea **0**:

```bash
curl -X POST http://localhost:3001/v2/ventas \
  -H "Authorization: Bearer ..." \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{ "tipo_venta": "CONTADO", ..., "productos": [{"articulo_id": X, "almacen_origen": Y, "cantidad": 1, ...}] }'
```

Esperado: HTTP 422 con `code: "articulo_sin_existencia"` y `details`
listando `articulo_id`, `almacen_id`, `cantidad_requerida`,
`existencia_disponible`. La venta **no** queda persistida en
`MSP_VENTAS`.

### B) Traspaso automático atómico

Con un artículo con existencia ≥ 1:

```bash
saleId=$(uuidgen)
curl -X POST http://localhost:3001/v2/ventas -H "Authorization: ..." -d '...'
```

Verificar en Firebird (como SYSDBA en `MUEBLERA.FDB`):

```sql
SELECT * FROM MSP_VENTAS WHERE ID = ':saleId';
-- → 1 fila

SELECT * FROM MSP_VENTAS_TRASPASOS WHERE VENTA_ID = ':saleId';
-- → 1 fila TIPO='directo' con DOCTO_IN_ID

SELECT * FROM DOCTOS_IN WHERE DOCTO_IN_ID = :doctoInId;
-- → fila con FOLIO LIKE 'MST%', ALMACEN_ID=<origen>,
--   ALMACEN_DESTINO_ID=11058, NATURALEZA_CONCEPTO='S', APLICADO='S',
--   CONCEPTO_IN_ID=36, SUCURSAL_ID=225490, SISTEMA_ORIGEN='IN'.

SELECT TIPO_MOVTO, COUNT(*) FROM DOCTOS_IN_DET WHERE DOCTO_IN_ID = :doctoInId GROUP BY TIPO_MOVTO;
-- → 2 filas: 'S' y 'E', cada una con UNIDADES = cantidad de la venta.

-- Saldo neto del artículo:
SELECT ALMACEN_ID, SUM(ENTRADAS_UNIDADES - SALIDAS_UNIDADES)
FROM SALDOS_IN
WHERE ARTICULO_ID = :X AND ALMACEN_ID IN (:origen, 11058)
GROUP BY ALMACEN_ID;
-- → :origen: -1 (vs antes), 11058: +1 (vs antes).
```

### C) Aplicar usa el almacén 11058

Tras aprobar la venta y aplicarla:

```sql
SELECT ALMACEN_ID FROM DOCTOS_PV_DET WHERE DOCTO_PV_ID = :doctoPvId;
-- → todas las filas con ALMACEN_ID = 11058 (no el origen).
```

El resultado neto sobre `SALDOS_IN` sigue siendo correcto (-1 del
origen acumulado entre traspaso + aplicar; el 11058 termina en 0 tras
los dos movimientos).

### D) Cancelar emite traspaso reverso

Con una venta `pendiente` (sin aplicar):

```bash
curl -X PATCH http://localhost:3001/v2/ventas/:saleId/cancel \
  -H "Authorization: Bearer ..." -d '{"reason":"prueba cutover"}'
```

Verificar:

```sql
SELECT TIPO, DOCTO_IN_ID FROM MSP_VENTAS_TRASPASOS WHERE VENTA_ID = ':saleId';
-- → 2 filas: 'directo' y 'reverso', con DOCTO_IN_ID distintos.

SELECT ALMACEN_ID, ALMACEN_DESTINO_ID FROM DOCTOS_IN WHERE DOCTO_IN_ID = :reversoId;
-- → ALMACEN_ID = 11058, ALMACEN_DESTINO_ID = <origen> (invertido).

SELECT SUM(ENTRADAS_UNIDADES - SALIDAS_UNIDADES) FROM SALDOS_IN WHERE ARTICULO_ID = :X AND ALMACEN_ID = :origen;
-- → vuelve al valor original (antes de la venta).
```

### E) Endpoints admin responden

```bash
curl -H "Authorization: Bearer ..." http://localhost:3001/v2/traspasos/:doctoInId
# 200 con DTO completo

curl -H "Authorization: Bearer ..." "http://localhost:3001/v2/traspasos?venta_id=:saleId"
# 200 con lista (1 o 2 elementos según si hubo reverso)

curl -H "Authorization: Bearer ..." "http://localhost:3001/v2/inventario/stock?articulo_id=:X&almacen_id=11058"
# 200 con {articulo_id, almacen_id, cantidad}

curl -H "Authorization: Bearer ..." http://localhost:3001/v2/inventario/almacenes
# 200 con catálogo
```

Los tres endpoints requieren los permisos
`traspasos:ver` / `stock:consultar` / `inventario:ver`
respectivamente — asegurarse de que el rol del usuario administrativo
los tenga asignados (`POST /v2/roles/:id/permisos`).

## Rollback

Si algo sale mal:

1. Volver al binario anterior (sin el módulo inventario activo).
2. La migración `000028` y la tabla `MSP_VENTAS_TRASPASOS` se pueden
   dejar — no afectan al código viejo.
3. Si el legacy Node estaba en marcha en paralelo (mal — no debería
   ocurrir), revisar `DOCTOS_IN` por traspasos duplicados con folios
   sucesivos del rango MST y resolver manualmente.
4. Si quedaron ventas con traspaso pero sin haberse aplicado en
   Microsip, no requieren cleanup — al volver al binario viejo, la
   operadora puede cancelarlas y emitir reverso manualmente.

## Cosas que **no** hace este módulo

- No valida stock para productos dentro de combos — solo productos
  sueltos. Si un caso real lo necesita, ampliar
  `validateStockParaProductos` en `internal/ventas/app/crear_venta.go`.
- No genera traspasos por almacén origen distinto: si dos productos de
  una venta vienen de origenes diferentes, la venta se rechaza con
  `productos_multiples_almacenes_origen`. Si esto bloquea casos
  reales, ampliar `crearTraspasoParaVenta` para emitir N traspasos.
- No cancela el traspaso desde Microsip: si la venta ya fue aplicada,
  el reverso lo emite la lógica interna de Microsip, no este módulo.
