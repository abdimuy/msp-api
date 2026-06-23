# Diseño — Modal de auditoría del % ponderado de cobranza

Fecha: 2026-06-23
Repos: msp-api (`internal/rutas`) + sistema-cobro-web (`src/modules/rutas`).
Tipo: feature read-only, sin migración. Commit a `main` local de cada repo (no push), como el resto del feature de rutas.

## Contexto
El reporte de cobranza semanal (`GET /v2/rutas` + drill-down `GET /v2/rutas/{zona}/cobranza`) muestra por cobrador un `% cobertura` y un `% ponderado`. El ponderado ya se ancla al calendario (días 1/15/fin). Hoy el drill-down es un panel inline que lista las ventas con: cliente_id, parcialidad, frecuencia, abonó, vencidas, aporte, saldo y el badge "Aplica" (Sí/No).

La oficina necesita **entender/auditar cómo se calcula el % ponderado**: ver, por venta, con nombres legibles y el detalle de la venta, cuánto atraso había antes de los pagos de la semana y cuánto queda después, y cómo cada venta contribuye al numerador/denominador.

## Objetivo
Convertir el desglose en un **modal de oficina** que haga auditable el % ponderado:
- Por venta: **cliente (nombre)**, **folio**, **productos** (carga lazy), y la cadena del cálculo **atraso antes → pago → aporte → atraso después** en **cuotas y pesos**.
- **Encabezado** con la fórmula: `numerador (Σ aporte) / denominador (# que aplican) = % ponderado`.

## Decisiones (acordadas con el usuario)
1. **Productos:** carga **lazy** al hacer click en la venta (no eager para todas; una zona puede tener 126+ ventas).
2. **Atraso antes/después:** mostrar en **cuotas y pesos** (ambos).
3. **Ventas a listar:** **todas** las de la zona, con el badge **Sí/No** de "Aplica" (para ver qué entra al denominador y qué no).
4. **Fórmula:** **encabezado** (numerador/denominador/%) **+ desglose por venta**.
5. **Fuente de productos (lazy):** **reusar el endpoint existente** `GET /v2/clientes/{clienteId}/ventas/{doctoPvId}` (ya devuelve `productos`, ya probado). Requiere permiso `clientes:leer` (el usuario de oficina/super_admin ya lo tiene).

## Semántica del cálculo (la que el modal hace transparente)
Inputs por venta (ya disponibles en `internal/rutas`): Parcialidad `P`, `Vencidas` (cuotas vencidas al inicio de la ventana, = atraso ANTES), `AbonoSemana` `A`, `Aporte` (capado a `vencidas+1`).

- `atraso_antes_cuotas` = `Vencidas` (ya se calcula hoy)
- `atraso_antes_pesos`  = `Vencidas × P`
- `pago_cuotas`         = `A / P`
- `aporte_cuotas`       = `Aporte` (ya existe; = min(pago_cuotas, vencidas+1))
- `atraso_despues_cuotas` = `max(0, Vencidas − pago_cuotas)`
- `atraso_despues_pesos`  = `atraso_despues_cuotas × P`

`CalcAporte` **no cambia**. Estos derivados se calculan en la capa `app` (decimal en Go) y viajan como string en el DTO (regla de dinero del proyecto). El resumen del encabezado reusa la lógica de `calcReporteZona` (numerador = Σ aporte de las que `AplicaPonderado`, denominador = # que `AplicaPonderado`, pct = num/den×100) — extraída/compartida para que el modal cuadre exacto con el % del listado.

## Backend (`internal/rutas`)
1. **Query `VentasPorZona`** (`infra/rutasfb`): agregar joins baratos:
   - `CLIENTES c ON c.CLIENTE_ID = s.CLIENTE_ID` → `c.NOMBRE` (tabla legacy → lectura ISO8859_1, NFC en Go; no CAST WIN1252).
   - Enlace `DOCTOS_CC → DOCTOS_PV` → `DOCTO_PV_ID` + `FOLIO` (folio de la venta). El join exacto se confirma en implementación; el módulo `clientes` ya mapea esta relación (referencia).
2. **Domain `VentaCobranza`**: agregar `ClienteNombre string`, `Folio string`, `DoctoPVID int`. (Mantener `ClienteID`.)
3. **App (`enrichVentas` / desglose)**: calcular y exponer los derivados antes/después (sección Semántica). Agregar al resultado del desglose un **resumen** `{ numerador decimal, denominador int, pct_ponderado *decimal }`.
4. **DTO `VentaCobranzaDTO`** (`infra/rutashttp`): `cliente_nombre`, `folio`, `docto_pv_id`, `atraso_antes_cuotas`, `atraso_antes_pesos`, `pago_cuotas`, `atraso_despues_cuotas`, `atraso_despues_pesos` (strings los montos/cuotas decimales). El response `DesglosePorZonaOutput` agrega un objeto `resumen` con `numerador`, `denominador`, `pct_ponderado`.
   - El contrato de `GET /v2/rutas` (listado) **no cambia**.

## Frontend (`src/modules/rutas`, hexagonal intacto)
- Reemplazar `DesglosePanel` inline por un **modal** (dialog de `@/components/ui/dialog`) que se abre al hacer click en la fila de zona.
- **Encabezado** del modal: zona · semana (`fecha_inicio`) · fórmula `Σ aporte (num) / # aplican (den) = %`.
- **Tabla** de ventas: Cliente (nombre) · Folio · Frecuencia · Aplica (badge Sí/No) · Atraso antes (cuotas + $) · Pago (cuotas + $) · Aporte · Atraso después (cuotas + $).
- **Click en una venta** → fetch lazy a `GET /v2/clientes/{clienteId}/ventas/{doctoPvId}` → expandir mostrando productos (artículo · cantidad · importe). Estado de carga/colapso por fila.
- Entidad `VentaCobranza` (FE) + DTO + mapper: agregar los campos nuevos (con guardas como las existentes). `fakeRutasPort` actualizado.
- Reuse: `formatMoney`/`formatPct`; el badge Aplica ya existe. Para productos, un usecase/port nuevo en el módulo rutas que llame al endpoint de clientes (adapter HTTP), o reusar el adapter de clientes si está accesible — se define en el plan.

## Flujo de datos
zona click → `GET /v2/rutas/{zona}/cobranza` (filas eager + resumen) → render modal → venta click → `GET /v2/clientes/{clienteId}/ventas/{doctoPvId}` (lazy) → expandir productos.

## Reuse (no reinventar)
- BE: `CalcAporte`, `Vencidas`, `Aporte`, `AplicaPonderado`, `calcReporteZona` (extraer el cálculo del resumen para compartir listado↔desglose). Query `VentasPorZona` se edita en sitio.
- FE: pipeline existente entidad→DTO→mapper→fake→pantalla; `formatMoney`/`formatPct`; badge Aplica; endpoint de productos de `clientes` ya construido y probado.

## Pruebas
- BE: unit de los derivados antes/después y del resumen (numerador/denominador/pct), incl. bordes (parcialidad 0 → no divide; pago > vencidas → después = 0). Mapper DTO.
- FE: mapper de campos nuevos (+ caso inválido → DomainError); render del modal (encabezado fórmula + filas); hook/usecase de lazy productos (éxito + error). Tests existentes siguen verdes vía `makeFakeVentaCobranza`.

## Verificación
- BE: `go build ./...`; `go test ./internal/rutas/...`; `golangci-lint run ./internal/rutas/...` (0 issues). Smoke en vivo: `GET /v2/rutas/{zona}/cobranza` con token real devuelve nombres/folio/derivados/resumen.
- FE: `npx tsc --noEmit`; `npx vitest run src/modules/rutas`. Visual: modal abre, muestra fórmula + filas, click en venta expande productos.

## Permisos
El modal usa el endpoint de `clientes` para productos → el usuario necesita `clientes:leer` además de `rutas:leer`. super_admin (boot-sync) ya los tiene. Si se quiere desacoplar a futuro, mover productos a un endpoint propio de rutas (alternativa descartada por ahora: menos código reusando clientes).

## Fuera de alcance
- Materializar/cachear.
- Validar números reales en apidev (paso server posterior y aparte).
- Calendario de cobranza por-cliente desde historial.
