# Orden fijo de los tres precios (2026-10-16)

Pedido del dueño: en la tarjeta de producto los tres precios van **siempre** en este orden:
contado → corto plazo (4 meses) → crédito (largo plazo).

## Causa (LEÍDO + MEDIDO en dev, 2026-10-16)

- `GET /almacenes/{id}/articulos` arma los precios con `LIST(PRECIOS_EMPRESA.NOMBRE || ':' || PRECIO)`
  (`internal/microsip/infra/microsipfb/queries.go:57`). Firebird **no garantiza el orden** de `LIST()`;
  el `ORDER BY ARTICULO` (`:81`) no ordena dentro de la lista.
- La app conserva el orden de llegada (`ParsePrice.kt:37-39`, `ProductsCatalogScreen.kt:377,383`,
  `ProductDetailsScreen.kt:501`). INFERIDO por la semántica de `LinkedTreeMap`/`mutableMapOf`.
- Dev, `MUEBLERA.FDB`, SELECT READ ONLY: 5,685 artículos llegan `42,6925,8437` (lista → 4 meses →
  1 mes, **al revés** de lo pedido); 8 artículos con tres precios llegan en otro orden; 71 tienen uno o dos.
- Tipos (dev): 8437 "Precio 1 Meses" = contado; 6925 "Precio 4 Meses" = corto plazo; 42 "Precio de
  lista" = largo plazo (`config.go:209`). Existe 8941 "Precio de Contado", fuera de la config.

## Corrección (revisión, 2026-10-16, LEÍDO)

Los teléfonos **no** llaman a `GET /almacenes/{id}/articulos` del API Go: piden el catálogo al
API Node heredado, `GET /almacenes/{id}` (`sys_msp_backend/src/components/almacenes/querys.ts:58`,
misma `LIST()` sin orden). El orden en que la flota pinta los precios lo fija el clasificador de la
app; el cambio del servidor Go no lo arregla. Lo que sigue en este documento sobre "la app recibe
el orden del endpoint" queda superado por esta nota. En Go la cadena `precios` quedó en orden fijo
largo → corto → contado (contado al final, por la lógica `lastOrNull()` de los APKs 2.20.0), y el
arreglo nuevo `precios_ordenados` en contado → corto → largo; sus consumidores reales son el
escritorio y reactivación. Ver E-INF-017.

## Diseño

- Servidor: clasifica por `PRECIO_EMPRESA_ID` (estable), no por nombre, con un mapeo en config
  `contado=8437, corto_plazo=6925, largo_plazo=42`; devuelve un arreglo ordenado
  `[{tipo, nombre, precio}]` y la cadena `precios` vieja **también en orden canónico** para los APKs
  actuales. Tipo ausente: se omite, no se inventa.
- Clientes: pintan en el orden recibido; no reordenan.
- Prueba roja al revertir: artículo cuyo `LIST` llega desordenado → respuesta en orden canónico.

## Consumidores que cambian con el orden (deben tocarse en el mismo cambio)

- **Riesgo de dinero, sin medir:** `ModernProductSelector.kt:282` y `SimpleProductSelector.kt:223`
  toman `priceMap.values.lastOrNull()` como precio mostrado: depende de la posición. Con el orden
  canónico el último pasaría a ser el de crédito. Hay que medir si ese valor llega a la venta
  (CLAUDE.md §8) antes de cambiar nada.
- `CartItemCard.kt:309,337` invierte la lista (`asReversed()`) para poner contado primero: con el
  orden nuevo se invertiría.
- Venta nueva, ticket, edición (`PriceParser`) y reactivación (`nbp_reader.go:122`) buscan por nombre:
  no les afecta el orden.
- Escritorio: sin pantalla de lista de precios. `AgregarProductoPanel.tsx:38` espera JSON
  (`PRECIO_LISTA`…); con la cadena vieja probablemente da 0 (INFERIDO; se comprueba con prueba unitaria).

---

# Parte 2 — regla de orden en la captura de ventas y venta de contado mixta (2026-10-16)

## La regla del servidor (LEÍDO)

- contado ≤ corto plazo ≤ anual, no estricta; un escalón en 0 se salta
  (`internal/ventas/domain/monto_snapshot.go:89-104`).
- Por línea: producto (`producto.go:65`), combo (`combo.go:68`) → `precio_tier_order_invalid`
  (`errors.go:613`). Por totales que recalcula el servidor sumando líneas (`venta.go:1080-1119`,
  `app/crear_venta.go:75` ignora los `montos` del cliente) → `monto_tier_order_invalid` (`errors.go:622`).
- No distingue contado de crédito. El comentario de `venta.go:1108` ("sólo con líneas de
  persistencia") es falso: ver abajo.

## App Android (LEÍDO)

- Sin validación equivalente: `tier_order|de menor a mayor|precioContado >` → 0 en `*.kt`/`*.json`;
  control positivo: la misma búsqueda halla `CreateComboDialog.kt:66`.
- Producto: los 3 precios salen del catálogo sin importar el tipo de venta
  (`NewLocalSaleViewModel.kt:328-340`). Combo en contado: solo campo Contado, lista y corto en 0
  (`ProductSaleViewModel.kt:263-264,289-290`).

## El defecto (MEDIDO 2026-10-16, dev, `go test -overlay`, sin tocar el repo)

Combo 0/0/C + producto 1600/1200/1000 → totales contado = 1000·q + C, corto = 1200·q, anual = 1600·q;
falla si C > (corto − contado)·q. Combo 5000 → `monto_tier_order_invalid`; controles: combo 150 pasa;
producto 0/0/1000 pasa. El teléfono toma el 422 con `X-Intent-Captured` como permanente
(`PendingLocalSalesWorker.kt:538-570`) y la venta queda en `MSP_FAILED_INTENTS`. Dev: 0 filas con esos
códigos (control: hay otros códigos). Producción sin consultar:

```sql
SELECT ID, RECEIVED_AT, STATUS, ERROR_CODE, RETRY_COUNT, BODY_BLOB_PATH
FROM MSP_FAILED_INTENTS
WHERE ERROR_CODE IN ('monto_tier_order_invalid','precio_tier_order_invalid')
ORDER BY RECEIVED_AT DESC;
```

## Diseño propuesto (dinero, §8; sin implementar)

- Servidor: en venta de CONTADO se valida solo el contado y corto/anual se guardan en 0 en líneas y
  encabezado (ya es lo que hacen 103 de 160 líneas de contado, `monto_snapshot.go:62`). **Antes:**
  enumerar y medir quién lee `PRECIO_ANUAL`/`PRECIO_CORTO` de ventas de contado (aplicación a
  Microsip, reportes, comisiones, desempeño).
- App: en contado muestra y envía solo contado (también productos, al crear y al cambiar el tipo).
- Validación previa en la app con el mismo código y mensaje, por línea y por totales; casos
  compartidos Go↔Kotlin en `core/common/src/test/resources/precios-orden/`; el caso medido es uno.
- Atoradas: tras desplegar, reenviar sin cambios; si no, `replay-with-multipart` (un solo intento;
  confirmar contra la nota física y verificar en `MSP_VENTAS`).
- Sin verificar: edición (`EditSaleScreen.kt:441-454`) y escritorio.
