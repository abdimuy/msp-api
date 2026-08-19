# Plan — Kits de venta local → juegos en Microsip (al aplicar la venta)

## Context
Hoy, cuando una venta local (app Android) que trae un **combo/kit** se sincroniza a Microsip (`VentaWriter.Aplicar`), el writer **aplana** el combo: escribe cada componente como una línea normal `DOCTOS_PV_DET ROL='N'` con su propio precio, e **ignora** el combo (su nombre y su precio de bundle). No existe ningún juego de Microsip ni concepto de kit.

El usuario quiere que **al convertir la venta local en venta de Microsip, el kit exista como juego real** (`ARTICULOS.ES_JUEGO='S'` + receta en `JUEGOS_DET`): buscar si ya existe un juego con **exactamente** esos componentes y cantidades; si existe se reusa, si no se **crea** (solo catálogo) dentro de la misma transacción; y la venta se escribe con una línea **`ROL='J'`** a **nuestro precio** del combo. La cascada de Microsip (`APLICA_VTA_PV → GENERA_DOCTO_IN_PV → ALTA_COMPONENTES_PV`), que ya dispara el `UPDATE ... APLICADO='S'` actual, explota la receta y descarga inventario por componentes.

Decisiones ya acordadas con el usuario:
- **Match exacto** por receta = multiset de `{COMPONENTE_ID, UNIDADES por juego}`. Si una cantidad/componente difiere → no es match → se crea uno nuevo.
- **Todo-o-nada transaccional**: crear/usar el juego + escribir la venta en **una sola transacción**; si algo falla, `ROLLBACK` completo (sin juego huérfano ni venta a medias) y se reintenta.
- **Sin `PRECIOS_ARTICULOS`**: el precio va explícito en el renglón `ROL='J'` (tomado de `combo.Precios()`), nunca del lookup `GET_PRECIO_ARTCLI`. Crear el juego es **solo catálogo** (`ARTICULOS` + `JUEGOS_DET` + `LIBRES_ARTICULOS`), sin precio/saldo/impuesto. (Documentado en `docs/microsip-crear-kit-paso-a-paso.md`.)

Resultado: los kits aparecen como juegos reales en Microsip, reutilizables, con el inventario descargándose por componentes vía la cascada nativa.

**Restricciones del proyecto (no negociables):** arquitectura hexagonal por slices (`CLAUDE.md §2`); código EN / mensajes ES (§3); sin lógica de negocio en BD para nuestras tablas (§1 — aquí no creamos migraciones, solo escribimos tablas Microsip legacy que están exentas por ADR-0006). **Tests súper robustos y completos para TODO lo implementado** (mandato explícito del usuario): coverage domain ≥99%, app ≥90%, infra-fb ≥80%, mutation kill ≥80%, más property-based (`pgregory.net/rapid`), fuzz, integración rollback-only y security sweep donde aplique, según `docs/module-standards/TESTING_REQUIREMENTS.md`.

**Ejecución:** subagent-driven-development orquestado por mí (un implementer + review de spec/calidad por tarea + review final de rama). Yo verifico cada paso. Commits a `main` local (no push), como el resto.

---

## Arquitectura (hexagonal, dónde vive cada cosa)

- **Domain** (`internal/ventas/domain/`, puro, sin I/O): VO `RecetaComponente` + derivación de la receta canónica de cada combo a partir de sus productos hijos. Reusa: `Combo` (`combo.go`), `Producto.ComboID()/ArticuloID()/Cantidad()` (`producto.go`), `Venta.Combos()/Productos()` (`venta.go`).
- **Port outbound** (`internal/ventas/ports/outbound/microsip_juego_resolver.go`, nuevo): `MicrosipJuegoResolver` (match-or-create). Sigue el patrón de `MicrosipVentaWriter`/`MicrosipClienteWriter` (`microsip_venta_writer.go`).
- **Adapter infra** (`internal/ventas/infra/microsip/juego_resolver.go`, nuevo): implementa el port — query de match contra `JUEGOS_DET` + alta catálogo. Reusa primitivas `firebird.GetQuerier`, `nextID`/`GEN_ID(ID_CATALOGOS,1)`, `firebird.MapError`, `firebird.Win1252` (bind de `NOMBRE` legacy), patrón de `cliente_writer.go`.
- **App** (`internal/ventas/app/aplicar_venta.go`): `AplicarVenta` resuelve, dentro de su `runInTx`, cada combo→`ARTICULO_ID` de juego (vía el port) y pasa el mapeo al writer.
- **Writer** (`internal/ventas/infra/microsip/venta_writer.go`): `insertDetalles` emite una línea `ROL='J'` por combo (precio = `combo.Precios()`, unidades = `combo.Cantidad()`), omite los productos-hijo como líneas con precio, y mantiene los productos sueltos en `ROL='N'`. La cascada del `UPDATE APLICADO='S'` (Phase 6, ya existente) crea los `ROL='C'`.
- **Composición** (`cmd/api/ventas_wiring.go`): construir/inyectar el resolver y la config de línea de juegos.

Decisiones de implementación (defaults, confirmables en review):
- **Nombre del juego creado:** `combo.Nombre()`; como `ARTICULOS.NOMBRE` es único (trigger `ARTICULOS_BEFINSUPD`), si choca con un artículo existente se le agrega un sufijo determinístico (firma corta de la receta). El **match es por receta numérica** (componentes+unidades), NO por nombre → no hay riesgo de encoding en el match.
- **`LINEA_ARTICULO_ID`** de juegos creados: **config** (`MICROSIP_VENTA.JUEGOS_LINEA_ARTICULO_ID`), validada al boot; el spike usa una línea real de la dev.
- **`ES_ALMACENABLE='N'`** para el juego (el kit no tiene existencia propia; descarga por componentes). Sin `CLAVE_ARTICULO` (opcional, fuera de alcance).
- **Feature flag** `MICROSIP_VENTA.JUEGOS_ENABLED` (default `false`): mientras esté off, se conserva el aplanado `ROL='N'` actual. Se enciende tras el spike + validación.

---

## Tareas (subagent-driven; orden 0→5, 0 es gating)

### Task 0 — Spike de verificación (GATING, hands-on Microsip, sin código de producción)
Confirmar por código (EXECUTE BLOCK + `ROLLBACK`, contra la dev `DESARROLLO.FDB`/`MUEBLERA.FDB`) lo que el doc marca como ⏳ pendiente:
1. Crear un juego con inserts directos (`ARTICULOS ES_JUEGO='S'` + `JUEGOS_DET` + `LIBRES_ARTICULOS`) funciona desde fuera del cliente Microsip.
2. Insertar **solo** una línea `DOCTOS_PV_DET ROL='J'` (sin `ROL='C'`) + `UPDATE DOCTOS_PV APLICADO='S'` ⇒ ¿la cascada `ALTA_COMPONENTES_PV`/`GENERA_DOCTO_IN_PV` (a) crea las líneas `ROL='C'` y (b) descarga inventario por componentes?
3. Cómo quedan `IMPORTE_NETO`/`TOTAL_IMPUESTOS` del header con un `ROL='J'`.

**Salida:** decisión registrada — **camino A** (cascada auto: el writer solo inserta `ROL='J'`) **o camino B** (debemos insertar nosotros los `ROL='C'` desde la receta antes del aplicar). Esto define el alcance exacto de Task 4. **Gatea Tasks 2-4.** (Lo corro yo / o el usuario captura el trace; solo lectura+rollback sobre la BD compartida.)

### Task 1 — Domain: receta canónica (puro)
- Nuevo `internal/ventas/domain/receta.go`: VO `RecetaComponente { articuloID int; unidades decimal.Decimal }` (campos privados + accessors, sentinels `apperror.New*`) y derivación canónica.
- Método en `Venta` (en `venta.go` o `receta.go`): p.ej. `func (v *Venta) RecetasDeCombos() map[uuid.UUID]Receta` y/o `RecetaDeCombo(comboID uuid.UUID) (Receta, error)` — para cada combo, junta sus productos hijo (`ComboID()==combo.ID`) en una lista **ordenada determinísticamente** (por `articuloID`) de `{articuloID, unidades=producto.Cantidad()}`. (Recordatorio: `combo-child producto.Cantidad()` ES por-juego; `combo.Cantidad()` es nº de bundles — verificado en `venta.go:recomputarMontos`.)
- Tipo `Receta` con igualdad/firma canónica (para match y para nombre-suffix).
- **Tests (≥99% + property + fuzz):** `receta_test.go` (tabla, `package domain_test`): combo de 1/N componentes; orden de inserción distinto → misma receta; cantidades fraccionarias; combo sin hijos → error/empty; dos combos en una venta → recetas independientes; productos sueltos ignorados. Property (`rapid`): la firma es estable ante permutaciones. Fuzz: no panics.

### Task 2 — Port + Adapter: `MicrosipJuegoResolver` (match-or-create)
- **Port** `internal/ventas/ports/outbound/microsip_juego_resolver.go`:
  ```go
  type MicrosipJuegoResolver interface {
      Resolver(ctx context.Context, in MicrosipJuegoInput) (MicrosipJuegoResult, error)
  }
  // in: receta (componentes+unidades) + NombrePropuesto + LineaArticuloID
  // result: ArticuloID int, Creado bool
  ```
- **Adapter** `internal/ventas/infra/microsip/juego_resolver.go` (usa `firebird.GetQuerier(ctx, w.pool.DB)` → corre en la tx ambiente):
  - **Match:** `SELECT ARTICULO_ID FROM ARTICULOS WHERE ES_JUEGO='S' AND ESTATUS='A'` con `JUEGOS_DET` agrupado, buscando el que tenga **exactamente** el mismo conjunto `{COMPONENTE_ID, UNIDADES}` (mismo conteo + cada par presente). Match **numérico** (sin `NOMBRE`). Patrón de join nuevo (no existe query a `JUEGOS_DET` aún).
  - **Create (si no hay match):** alta catálogo por el `docs/microsip-crear-kit-paso-a-paso.md` — `GEN_ID(ID_CATALOGOS,1)` → `INSERT ARTICULOS (ES_JUEGO='S', ES_ALMACENABLE='N', ESTATUS='A', LINEA_ARTICULO_ID, NOMBRE, APLICAR_FACTOR_VENTA='N', FACTOR_VENTA=0, RED_PRECIO_CON_IMPTO='N')` → `INSERT JUEGOS_DET` (1×componente, `UNIDADES`, `ES_REEMPLAZABLE='N'`, `PERMITIR_MODIF_UNID='N'`, `CLAVE_ARTICULO_ID` del componente vía lookup como en `selectClavesArticulo`) → `INSERT LIBRES_ARTICULOS (ARTICULO_ID)`. Bind `NOMBRE` con `firebird.Win1252` (legacy, como `cliente_writer.go`). Nombre único: si `MapError`→Conflict por nombre, reintar con sufijo de firma de receta. **Sin** `PRECIOS_ARTICULOS`/`SALDOS_IN`/`IMPUESTOS_ARTICULOS`.
- **Tests integración (fbtestutil.WithTestTransaction, rollback-only, gate `FB_DATABASE`):** match exacto encuentra; near-miss NO encuentra (cantidad distinta / componente distinto / componente extra / faltante / orden distinto sí matchea); create produce `ARTICULOS ES_JUEGO='S'` + `JUEGOS_DET` == receta + `LIBRES_ARTICULOS`; idempotencia (resolver 2× la misma receta en la tx ⇒ 2ª hace match, `Creado=false`); colisión de nombre → sufijo. Coverage ≥80% del adapter; unit del armado de SQL/decisiones donde sea puro.

### Task 3 — App: resolver combos dentro de `AplicarVenta` y pasar el mapeo
- `MicrosipVentaInput` (`microsip_venta_writer.go`): agregar `JuegosPorCombo map[uuid.UUID]int` (comboID→ARTICULO_ID resuelto) **o** dejar que el writer lo reciba aparte. (Elegir lo que mantenga el writer "escribe lo que le dicen".)
- `Service.AplicarVenta` (`app/aplicar_venta.go`), dentro del `runInTx` existente y **antes** de `microsipWriter.Aplicar`: si `JUEGOS_ENABLED`, por cada `v.Combos()` calcular su receta (Task 1) y llamar `s.juegoResolver.Resolver(...)` (nuevo port inyectado al `Service`); construir el mapa y meterlo en `writerIn`. Si `JUEGOS_ENABLED=false`, mapa vacío (writer aplana como hoy).
- Inyectar `MicrosipJuegoResolver` en `app.Service` (constructor `NewService`).
- **Tests app (≥90%, fakes a mano, `package app_test`):** fake `MicrosipJuegoResolver` + fake writer; verificar que con combos se llama al resolver por combo y el mapa llega al writer input; resolver error → `AplicarVenta` falla y NO marca aplicada (rollback); flag off → no se llama al resolver. Reusa el harness de fakes existente de los tests de `aplicar_venta`.

### Task 4 — Writer: emitir `ROL='J'` por combo (+ camino A/B del spike)
- `insertDoctoPVDet` (`venta_writer.go:138`): parametrizar `ROL` (hoy hardcodeado `'N'`).
- `insertDetalles`:
  - Productos **sueltos** (`ComboID()==nil`) → `ROL='N'` (como hoy).
  - Por cada **combo** → una línea `ROL='J'` con `ARTICULO_ID` = `JuegosPorCombo[comboID]`, `UNIDADES = combo.Cantidad()`, precio = `combo.Precios()` (Contado/Anual según `TipoVenta`), neto = precio/(1+IVA del juego). **Omitir** los productos-hijo como líneas con precio.
  - Header `IMPORTE_NETO`/`TOTAL_IMPUESTOS`: sumar sueltos (`ROL='N'`) + combos (`ROL='J'`) — alinea con `recomputarMontos` (que ya ignora hijos).
  - **Camino B (si el spike lo exige):** insertar también las líneas `ROL='C'` (precio 0, `UNIDADES = combo.Cantidad() × receta.unidades`, ligadas vía `SUB_MOVTOS_PV`) antes del aplicar.
- **Tests integración e2e (fbtestutil rollback):** venta con 1 combo (match a juego existente sembrado) → 1 línea `ROL='J'` + tras `APLICADO='S'`: existen `ROL='C'` + se descargó inventario de los componentes + se generó el `DOCTOS_CC` (crédito). Venta con combo cuyo juego NO existe → se crea el juego y luego la venta. Venta mixta (suelto + combo). Venta sin combos → comportamiento idéntico al actual (regresión). Caso `JUEGOS_ENABLED=false` → aplanado `ROL='N'` (regresión). Verificar montos del header.

### Task 5 — Config, wiring y composición + docs
- Config (`internal/platform/config/config.go`): `MicrosipVenta.JuegosEnabled bool` (`MICROSIP_VENTA_JUEGOS_ENABLED`, default false) y `JuegosLineaArticuloID int` (`MICROSIP_VENTA_JUEGOS_LINEA_ARTICULO_ID`), con validación (si enabled, línea > 0).
- `cmd/api/ventas_wiring.go`: `provideVentasMicrosipJuegoResolver(p, cfg)` → `microsip.NewJuegoResolver(p)`; inyectar al `ventasapp.Service` (junto a `microsipWriter`).
- **Composition E2E test** (`venthttp/e2e_firebird_*_test.go`, patrón `TestE2E_*` + `WithTestTransaction` + router con tx injector): POST aplicar venta con combo, contra writers Microsip reales, verifica `ROL='J'`/`ROL='C'`/inventario end-to-end.
- Actualizar `docs/microsip-crear-kit-paso-a-paso.md`: marcar el spike como ✅ verificado y enlazar el flujo de venta-con-juego.
- `.env.example` + `MICROSIP_VENTA_*` documentadas.

---

## Reuse (no reinventar)
- Transacción/querier: `firebird.GetQuerier`, `RunInTx`, `runInTx` (`internal/platform/firebird/transaction.go`) — el resolver corre en la tx de `AplicarVenta`.
- IDs/errores/encoding: `nextID`/`GEN_ID(ID_CATALOGOS,1)`, `firebird.MapError`, `firebird.Win1252`, `selectClavesArticulo` (lookup de clave de componente) — todo en `infra/microsip/*` y `platform/firebird/*`.
- Patrón de adapter/port: `MicrosipClienteWriter`/`ClienteWriter` (`cliente_writer.go`) como molde del `JuegoResolver`.
- Cascada: NO se llama explícitamente; la dispara el `UPDATE APLICADO='S'` ya existente (Phase 6, `venta_writer.go:399`).
- Tests: `fbtestutil.WithTestTransaction`, `NewTestFirebirdPool`, harness e2e de `venthttp/e2e_firebird_autocrea_cliente_test.go`, fakes a mano de `app_test`.
- Receta-quantity: `Combo.Cantidad()` (bundles) vs `Producto.Cantidad()` (por-juego) — verificado en `venta.go:recomputarMontos`.

## Fuera de alcance
- `PRECIOS_ARTICULOS`/`SALDOS_IN`/`IMPUESTOS_ARTICULOS` del juego (el precio va en el renglón).
- Lado Android / contrato de entrada (la venta ya llega con combos+precios).
- Productos sueltos (siguen `ROL='N'`).
- `CLAVE_ARTICULO` (código de barras) del juego creado.

## Verificación (end-to-end)
- `go build ./...`; `golangci-lint run ./internal/ventas/...` (0 issues).
- `go test ./internal/ventas/domain/... ./internal/ventas/app/...` (unit/property/fuzz verdes; coverage domain ≥99%, app ≥90%).
- `make test-firebird-ventas` (integración rollback: resolver match/create/idempotencia + writer e2e `ROL='J'`/`ROL='C'`/inventario + regresión sin-combos y flag-off).
- `gremlins unleash ./internal/ventas/domain ./internal/ventas/app` (kill ≥80%).
- Smoke en vivo (con flag on en dev): aplicar una venta con combo y verificar en Microsip el juego (`ES_JUEGO='S'`), la línea `ROL='J'`, los `ROL='C'` y el movimiento de inventario por componentes.
- **Gate:** Task 0 (spike) debe pasar antes de Tasks 2-4; su resultado (camino A vs B) ajusta Task 4.
