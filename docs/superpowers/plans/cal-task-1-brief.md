# Task 1 (Backend) — Ponderado de cobranza por CALENDARIO

This file is your requirements. Use the exact signatures, decisions, and test
expectations verbatim. Repo: `/Volumes/M2-1TB/Developer/msp-api`, module
`internal/rutas/`. Work on branch `main` (do NOT push). Follow TDD.

## Why
El reporte de cobranza semanal (`GET /v2/rutas` + drill-down
`GET /v2/rutas/{zona}/cobranza`) calcula un `% ponderado` cuyo denominador
("¿esta venta aplica esta semana?") y atraso (`Plazos`/vencidas) hoy se
infieren de `FECHA_ULT_PAGO`/cadencia → inestables: al pagar, la fecha se
recorre y la venta desaparece o se cae del reporte. Cambiamos a fechas FIJAS
del calendario (día 1, día 15, último-día-de-mes, aniversarios semanales),
que no se mueven con el pago.

**`CalcAporte` (en `domain/aporte.go`) NO CAMBIA.** Sigue recibiendo
`AporteInput.Plazos`. Solo cambia CÓMO se calcula `Plazos` y el denominador.
Los 5 tests de `domain/aporte_test.go` deben seguir verdes sin tocarlos.

## Modelo (días naturales — no se puede saber quién cobra domingos)
- Mensual: vence el día **1** de cada mes. Gracia +2 (a tiempo hasta el 3).
- Quincenal: vence el día **15** y el **último día del mes**. Gracia +2.
- Semanal: ciclo semanal; toda la semana de gracia.
- Primer vencimiento = el primero del calendario **estrictamente > FECHA_CARGO**
  (el enganche cubre el momento de la venta).
- Gracia +2 aplica **solo al atraso** (VencimientosVencidos), NUNCA al
  denominador (AplicaEnVentana).

## Decisiones de borde (FIJADAS — respétalas)
- **D1:** comparar por **fecha civil UTC** (truncar la hora a medianoche).
  La hora nunca mueve un vencimiento. Trunca `fechaCargo`, `fechaInicio`,
  `now`, `desde`, `hasta`.
- **D2:** ventana `[desde, hasta]` **inclusiva** en ambos extremos.
- **D3:** primer vencimiento **estrictamente > FechaCargo** (regla del enganche)
  — en ambas funciones, todo `v` debe cumplir `dateUTC(fechaCargo) < v`.
- **D5:** gracia +2 SOLO en `VencimientosVencidos`; NUNCA en `AplicaEnVentana`.
- **D6:** SEMANAL en `AplicaEnVentana` = `true` solo si existe una semana
  cumplida (`fechaCargo+7k`, k≥1) dentro de la ventana — una venta hecha hace
  3 días, sin pago aún exigible, NO cuenta todavía.
- `graceDias` es parámetro (se pasa 2 para quincenal/mensual; se ignora en
  semanal).
- Frecuencia desconocida → se trata como **SEMANAL** (igual que
  `CadenciaDias`, default 7).

---

## B1 — NUEVO archivo `internal/rutas/domain/calendario.go` (puro, sin I/O)

```go
// Vencimientos programados ANTERIORES al periodo actual cuya fecha + gracia
// ya pasó (estrictamente antes de fechaInicio). Reemplaza el viejo Plazos.
//   SEMANAL   → floor(días(fechaInicio − fechaCargo)/7), piso 0 (gracia ignorada).
//   QUINCENAL → cuenta días-15 y últimos-día-de-mes v con dateUTC(fechaCargo)<v y v+gracia<dateUTC(fechaInicio).
//   MENSUAL   → cuenta días-1 v con dateUTC(fechaCargo)<v y v+gracia<dateUTC(fechaInicio).
func VencimientosVencidos(frec Frecuencia, fechaCargo, fechaInicio time.Time, graceDias int) int

// ¿Hay un vencimiento del calendario dentro de [desde, hasta] (inclusivo),
// estrictamente > fechaCargo? Gracia NO aplica aquí.
//   SEMANAL   → existe k≥1 con fechaCargo+7k en [desde, hasta].
//   QUINCENAL → existe un día-15 o último-día-de-mes en [desde, hasta] y > fechaCargo.
//   MENSUAL   → existe un día-1 en [desde, hasta] y > fechaCargo.
func AplicaEnVentana(frec Frecuencia, fechaCargo, desde, hasta time.Time) bool
```

Helpers internos (privados):
- `dateUTC(t time.Time) time.Time` → `time.Date(y, m, d, 0,0,0,0, time.UTC)` con
  `y,m,d` tomados de `t.UTC()`. Trunca a medianoche UTC.
- `daysBetween(a, b time.Time) int` → días civiles `dateUTC(b) − dateUTC(a)`
  (entero, puede ser negativo). Sugerencia robusta:
  `int(dateUTC(b).Sub(dateUTC(a)).Hours() / 24)` redondeado con cuidado, o
  contar con `AddDate`. Prefiere una resta de medianoches UTC (no hay DST en UTC,
  así que `Sub(...).Hours()/24` es exacto sobre medianoches).
- `ultimoDiaDeMes(t time.Time) time.Time` → `time.Date(y, m+1, 0, 0,0,0,0, time.UTC)`
  (Go normaliza día 0 = último día del mes anterior; maneja 28/29/30/31 y bisiestos).
- enumeración de candidatos:
  - MENSUAL: para cada mes que toque el rango relevante, el día-1.
  - QUINCENAL: para cada mes, el día-15 y `ultimoDiaDeMes`.
  - SEMANAL: usar aritmética de múltiplos de 7 sobre `daysBetween` (no enumerar
    fecha por fecha si puedes evitarlo).

Implementación sugerida para SEMANAL:
- `VencimientosVencidos`: `d := daysBetween(fechaCargo, fechaInicio); if d < 0 { return 0 }; return d/7` (división entera de Go = floor para no-negativos).
- `AplicaEnVentana`: existe `k≥1` con `desde ≤ fechaCargo+7k ≤ hasta`. En offsets
  desde `fechaCargo`: sea `lo = daysBetween(fechaCargo, desde)`, `hi = daysBetween(fechaCargo, hasta)`.
  ¿hay múltiplo de 7 en `[max(lo,7), hi]`? (k≥1 ⇒ offset≥7). Si `hi < 7` → false.

Para QUINCENAL/MENSUAL: itera meses desde el mes de `fechaCargo` (o de `desde`)
hasta el mes de `fechaInicio`/`hasta` inclusive, genera los candidatos, aplica
los filtros (`>fechaCargo`, y para vencidos `v+grace < fechaInicio`; para ventana
`desde ≤ v ≤ hasta`). Cuida el cruce de año.

## B2 — `internal/rutas/domain/cobranza.go`
Agregar a `VentaCobranza`:
```go
// AplicaPonderado indica si esta venta cuenta en el denominador del % ponderado
// (hay un vencimiento del calendario dentro de la ventana del reporte).
AplicaPonderado bool
```
Deja `FechaUltPago` en el struct (compatibilidad) pero ya NO alimenta lógica de
ponderado. Puedes ajustar el comentario de `FechaUltPago` para reflejarlo.

## B3 — `internal/rutas/app/cobranza_semanal.go` (`enrichVentas`)
Nueva firma: `func enrichVentas(ventas []rutasdomain.VentaCobranza, fechaInicio, now time.Time)`.
Dentro del loop, REEMPLAZA el cálculo de `windowDays`/`plazos` por:
```go
grace := 0
if v.Frecuencia == rutasdomain.Quincenal || v.Frecuencia == rutasdomain.Mensual {
    grace = 2
}
plazos := decimal.NewFromInt(int64(
    rutasdomain.VencimientosVencidos(v.Frecuencia, v.FechaCargo, fechaInicio, grace),
))
v.AplicaPonderado = rutasdomain.AplicaEnVentana(v.Frecuencia, v.FechaCargo, fechaInicio, now)
```
`plazos` se SIGUE pasando a `AporteInput.Plazos` y al bloque de cálculo de
`v.Vencidas` (ese bloque informativo se conserva, solo usa el nuevo `plazos`).
**No toques `CalcAporte`.**

Actualiza las 2 llamadas a `enrichVentas`:
- en `DesglosePorZona` (mismo archivo): `enrichVentas(ventas, fechaInicio, now)`
  (ya hay un `now := time.Now().UTC()` arriba — reúsalo).
- en `ListarRutas` (`app/listar_rutas.go` línea ~66): `enrichVentas(ventas, fechaInicio, now)`
  (ya existe `now` en ese scope).

## B4 — `internal/rutas/app/listar_rutas.go` (`calcReporteZona`)
- **Borra por completo** la función `ventaAplicaEnVentana` (líneas ~131-143).
- En el loop de ponderado, reemplaza
  `if v.Frecuencia == rutasdomain.Semanal || ventaAplicaEnVentana(v, fechaInicio, now) {`
  por simplemente `if v.AplicaPonderado {`.
- `calcReporteZona` ya no usa `fechaInicio`/`now` para esa decisión. Si quedan sin
  uso tras el cambio, ajústalo: probablemente puedas quitar `now` de la firma de
  `calcReporteZona` y/o `fechaInicio` si quedan sin referencias (verifica con el
  compilador; mantén lo que cobertura sí use). Actualiza su llamada y tests acorde.
  (La cobertura usa `coberturaDen`/`coberturaNum`, que no dependen de fechas.)

## B5 — `internal/rutas/infra/rutashttp/cobranza_dto.go` + handler mapper
Agregar a `VentaCobranzaDTO`:
```go
AplicaPonderado bool `json:"aplica_ponderado" doc:"Si la venta cuenta en el denominador del % ponderado esta semana"`
```
En `toVentaCobranzaDTOs` (`cobranza_handlers.go`), mapear
`AplicaPonderado: v.AplicaPonderado`. El listado `GET /v2/rutas` (dto.go,
handlers.go) NO cambia su contrato.

---

## B6 — Tests (el corazón; el usuario pidió "tests muy robustos")

### `domain/calendario_test.go` (NUEVO) — tabla + testify, como `aporte_test.go`
Construye fechas con un helper local `d(s string) time.Time` que parsee
`"2026-01-08"` a medianoche UTC, y `dh(s string) time.Time` que parsee
`"2026-01-08T23:30:00Z"` para los casos de hora ≠ 0.

#### Matriz `VencimientosVencidos` — `(frec, fechaCargo, fechaInicio, grace) → esperado`
Grace = 2 para quincenal/mensual; 0 para semanal (ignorado).

SEMANAL (grace 0):
| id | cargo | inicio | esperado |
|----|-------|--------|----------|
| V-S1 | 2026-01-01 | 2026-01-01 | 0 |
| V-S2 | 2026-01-01 | 2026-01-08 | 1 |
| V-S3 | 2026-01-01 | 2026-01-07 | 0 |
| V-S4 | 2026-01-01 | 2026-01-15 | 2 |
| V-S5 | 2026-01-01 | 2026-02-01 | 4 |  (31 días → floor(31/7)=4)
| V-S6 | 2026-01-10 | 2026-01-05 | 0 |  (inicio antes de cargo → piso 0)
| V-S7 | 2026-01-01T23:30:00Z | 2026-01-08T00:10:00Z | 1 |  (D1: hora ignorada)
| V-S8 (frec="DIARIO" desconocida) | 2026-01-01 | 2026-01-22 | 3 |  (→ semanal, 21/7)

MENSUAL (grace 2):
| id | cargo | inicio | esperado |
|----|-------|--------|----------|
| V-M1 | 2026-01-01 | 2026-01-15 | 0 |  (día-1 2026-01-01 == cargo, excluido; 02-01 futuro)
| V-M2 | 2025-12-15 | 2026-01-15 | 1 |  (01-01, +2=01-03 < 01-15)
| V-M3 | 2025-11-20 | 2026-02-10 | 3 |  (12-01, 01-01, 02-01; cada +2 < 02-10)
| V-M4 | 2025-12-15 | 2026-01-03 | 0 |  (01-01+2=01-03, NO < 01-03 estricto)
| V-M5 | 2025-12-15 | 2026-01-04 | 1 |  (01-01+2=01-03 < 01-04)
| V-M6 | 2025-06-10 | 2026-01-15 | 7 |  (07-01..2026-01-01 = 7 días-1)

QUINCENAL (grace 2):
| id | cargo | inicio | esperado |
|----|-------|--------|----------|
| V-Q1 | 2026-01-01 | 2026-01-20 | 1 |  (01-15+2=01-17<01-20 sí; 01-31+2=02-02 no)
| V-Q2 | 2026-01-01 | 2026-02-05 | 2 |  (01-15, 01-31; 02-15 futuro)
| V-Q3 | 2026-02-01 | 2026-03-10 | 2 |  (02-15, 02-28 [feb no bisiesto])
| V-Q5 | 2026-04-01 | 2026-05-05 | 2 |  (04-15, 04-30 [abril 30 días])
| V-Q6 | 2026-03-01 | 2026-04-05 | 2 |  (03-15, 03-31 [marzo 31])
| V-Q7 | 2026-01-01 | 2026-02-02 | 1 |  (01-15 sí; 01-31+2=02-02 NO<02-02 estricto)
| V-Q8 | 2026-01-01 | 2026-02-03 | 2 |  (01-15, 01-31+2=02-02<02-03)
| V-Q9 | 2025-12-10 | 2026-01-20 | 3 |  (12-15, 12-31, 01-15; cruce de año)

CERO / fresh:
| id | frec | cargo | inicio | esperado |
|----|------|-------|--------|----------|
| V-Z1 | MENSUAL | 2026-01-05 | 2026-01-10 | 0 |
| V-Z2 | QUINCENAL | 2026-01-16 | 2026-01-20 | 0 |  (primer candidato>cargo=01-31, +2 no<01-20)

#### Matriz `AplicaEnVentana` — `(frec, fechaCargo, desde, hasta) → esperado`
Gracia NO aplica. `[desde, hasta]` inclusivo.

SEMANAL:
| id | cargo | desde | hasta | esperado |
|----|-------|-------|-------|----------|
| A-S1 | 2026-01-01 | 2026-01-08 | 2026-01-14 | true |  (k=1 → 01-08)
| A-S2 | 2026-01-01 | 2026-01-02 | 2026-01-07 | false |  (D6: semana no cumplida)
| A-S3 | 2026-01-01 | 2026-01-15 | 2026-01-21 | true |  (k=2 → 01-15)
| A-S4 | 2026-01-01 | 2026-01-09 | 2026-01-13 | false |  (01-08 antes, 01-15 después)
| A-S5 | 2026-01-01 | 2026-01-08 | 2026-01-08 | true |  (borde desde=hasta inclusivo)
| A-S6 | 2026-01-01 | 2026-01-03 | 2026-01-08 | true |  (01-08==hasta inclusivo)
| A-S7 | 2026-01-01T18:00:00Z | 2026-01-08T01:00:00Z | 2026-01-08T23:00:00Z | true |  (D1)
| A-S8 (frec="X" desconocida) | 2026-01-01 | 2026-01-08 | 2026-01-14 | true |

MENSUAL:
| id | cargo | desde | hasta | esperado |
|----|-------|-------|-------|----------|
| A-M1 | 2025-12-20 | 2026-01-01 | 2026-01-07 | true |  (01-01 en ventana, >cargo)
| A-M2 | 2025-12-20 | 2026-01-02 | 2026-01-10 | false |  (01-01 antes de desde)
| A-M3 | 2025-12-20 | 2026-01-01 | 2026-01-01 | true |  (borde)
| A-M4 | 2026-01-01 | 2026-01-01 | 2026-01-10 | false |  (01-01==cargo no es >cargo; 02-01 fuera)
| A-M5 | 2026-01-01 | 2026-01-28 | 2026-02-03 | true |  (02-01 en ventana)

QUINCENAL:
| id | cargo | desde | hasta | esperado |
|----|-------|-------|-------|----------|
| A-Q1 | 2026-01-01 | 2026-01-15 | 2026-01-15 | true |  (15 borde)
| A-Q2 | 2026-01-01 | 2026-01-16 | 2026-01-30 | false |  (15 antes, 31 después)
| A-Q3 | 2026-01-01 | 2026-01-31 | 2026-02-02 | true |  (último-día 31)
| A-Q4 | 2026-02-01 | 2026-02-28 | 2026-02-28 | true |  (feb NO bisiesto: último=28)
| A-Q5 | 2026-02-01 | 2026-02-16 | 2026-02-27 | false |  (15 antes, 28 después)
| A-Q6 | 2024-02-01 | 2024-02-29 | 2024-02-29 | true |  (feb bisiesto: último=29)
| A-Q7 | 2024-02-01 | 2024-02-28 | 2024-02-28 | false |  (bisiesto: 28 NO es vencimiento, último=29)
| A-Q8 | 2026-04-01 | 2026-04-30 | 2026-04-30 | true |  (abril último=30)
| A-Q9 | 2026-04-01 | 2026-04-16 | 2026-04-29 | false |  (15 antes, 30 después)
| A-Q10 | 2025-12-01 | 2025-12-31 | 2026-01-01 | true |  (12-31 cruce de año)
| A-Q11 | 2026-01-15 | 2026-01-15 | 2026-01-20 | false |  (15==cargo no >cargo; 31 fuera)

Cubre: mensual a principio/mitad/fin de semana; quincenal el 15 y último día con
meses 28/29/30/31 (incl. feb bisiesto y no bisiesto); cruce dic→ene; venta recién
hecha (0 vencidos / no aplica); venta vieja (muchos vencidos, contados a mano);
vencimiento justo en el borde (inclusivo D2 / estricto en vencidos); gracia que
empuja vencido↔no-vencido; semana sin 1/15/último → no aplica; horas ≠ 0 (D1);
frecuencia desconocida → semanal. Puedes AÑADIR casos si encuentras huecos.

### `domain/aporte_test.go` — INTACTO. Solo confirma que los 5 casos siguen verdes.

### `app/cobranza_semanal_test.go` y `app/listar_rutas_test.go`
- Actualiza a la nueva firma `enrichVentas(ventas, fechaInicio, now)`.
- El denominador del ponderado ahora depende de `v.AplicaPonderado` (la bandera
  la pone `enrichVentas`; en tests de `calcReporteZona` puro, setéala en el input).
- Agrega/ajusta un caso que mezcle ventas SEMANAL/QUINCENAL/MENSUAL con
  `AplicaPonderado` true/false y verifica el denominador del ponderado
  (`PctPonderadoSemanal`).
- Confirma que el repo de skip-mode y todo el módulo compila SIN
  `ventaAplicaEnVentana`.

---

## Verificación (ejecútala y reporta la salida)
1. `cd /Volumes/M2-1TB/Developer/msp-api && go build ./...`
2. `go test ./internal/rutas/...` (matriz verde + 5 casos CalcAporte intactos + service tests)
3. `golangci-lint run ./internal/rutas/...` (0 issues; si no está instalado, repórtalo)

## Commit
Un commit (o pocos lógicos) a `main`, NO push. Mensaje conventional, scope `rutas`,
en español neutro. SIN footer de atribución a Claude. Ej:
`feat(rutas): ponderado de cobranza anclado al calendario (días 1/15/fin)`

## Reglas del proyecto (CLAUDE.md)
- Código/identificadores/comentarios en inglés; mensajes de usuario en español.
- Sin lógica en BD (esta tarea no toca migraciones). Comparación de fechas en Go.
- No `--no-verify`. Respeta los hooks de lefthook.
