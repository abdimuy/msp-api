# Modal de auditoría del % ponderado — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convertir el desglose de cobranza por zona en un modal de oficina que hace auditable el % ponderado: por venta muestra nombre del cliente, folio, productos (lazy), y la cadena atraso antes → pago → aporte → atraso después (cuotas y pesos), más un encabezado con numerador/denominador/%.

**Architecture:** Backend `internal/rutas`: enriquecer la query por zona (nombre/folio/docto_pv_id), helpers de dominio puros para los derivados y el resumen, DTO + handler. Frontend `src/modules/rutas`: modal (dialog) con encabezado de fórmula + columnas nuevas + badge Aplica (ya existe) + carga lazy de productos reusando el endpoint de `clientes`. Sin migración, read-only.

**Tech Stack:** Go (Firebird/firebirdsql, shopspring/decimal, Huma+chi, testify) · React+TS (axios, vitest, shadcn/ui Dialog).

## Global Constraints
- CLAUDE.md §1: sin lógica en BD; sin migración en esta feature; fechas/derivados en Go.
- CLAUDE.md §2: vertical slices; cross-module solo vía contratos. `clientes` NO expone contratos Go → el FE reusa el endpoint HTTP de clientes (no se importa código de clientes en el backend de rutas).
- CLAUDE.md §3: código/identificadores/comentarios en inglés; texto de usuario en español neutro.
- Dinero y cuotas decimales viajan como **string** en el DTO (decimal.Decimal en Go). Cuotas `StringFixed(4)`, pesos `StringFixed(2)`.
- `CalcAporte` (domain/aporte.go) NO cambia; sus 5 tests siguen verdes.
- Tablas legacy Microsip (CLIENTES, DOCTOS_PV) se leen ISO8859_1, NFC en Go; sin CAST WIN1252.
- firebirdsql: `CAST(SUM(...) AS NUMERIC(18,s))` para agregados; subqueries escalares correlacionadas OK (ver clientes).
- Commit a `main` local de cada repo, NO push. Conventional commits, scope `rutas`, sin footer de atribución a Claude. No `--no-verify`.

---

### Task 1: Helpers de dominio — desglose del aporte + resumen ponderado (BE, puro)

**Files:**
- Create: `internal/rutas/domain/desglose.go`
- Test: `internal/rutas/domain/desglose_test.go`
- Modify: `internal/rutas/app/listar_rutas.go` (refactor de `calcReporteZona` para usar el resumen compartido)
- Test: `internal/rutas/app/listar_rutas_test.go` (sigue verde tras el refactor)

**Interfaces:**
- Produces:
  - `type AporteDesglose struct { AtrasoAntesCuotas, AtrasoAntesPesos, PagoCuotas, AporteCuotas, AtrasoDespuesCuotas, AtrasoDespuesPesos decimal.Decimal }`
  - `func DesglosarAporte(parcialidad, vencidas, abonoSemana, aporte decimal.Decimal) AporteDesglose`
  - `type ResumenPonderado struct { Numerador decimal.Decimal; Denominador int; Pct *decimal.Decimal }`
  - `func CalcularResumenPonderado(ventas []VentaCobranza) ResumenPonderado`
- Consumes: `VentaCobranza` (domain, ya existe; campos `Aporte`, `AplicaPonderado`).

- [ ] **Step 1: Write the failing test** — `internal/rutas/domain/desglose_test.go`

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestDesglosarAporte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id                                                                       string
		parcialidad, vencidas, abono, aporte                                     decimal.Decimal
		antesC, antesP, pagoC, aporteC, despuesC, despuesP                       string
	}{
		// debía 2 cuotas, pagó 1 (parcialidad 100): después queda 1.
		{"normal", dec("100"), dec("2"), dec("100"), dec("1"), "2", "200", "1", "1", "1", "100"},
		// pagó de más (3 cuotas) sobre 2 vencidas → después 0 (no negativo).
		{"sobrepago", dec("100"), dec("2"), dec("300"), dec("2"), "2", "200", "3", "2", "0", "0"},
		// sin atraso, sin pago.
		{"cero", dec("100"), dec("0"), dec("0"), dec("0"), "0", "0", "0", "0", "0", "0"},
		// parcialidad 0 → no divide; pago en cuotas 0.
		{"parcialidad_cero", dec("0"), dec("0"), dec("0"), dec("0"), "0", "0", "0", "0", "0", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := rutasdomain.DesglosarAporte(tc.parcialidad, tc.vencidas, tc.abono, tc.aporte)
			assert.True(t, got.AtrasoAntesCuotas.Equal(dec(tc.antesC)), "antesC")
			assert.True(t, got.AtrasoAntesPesos.Equal(dec(tc.antesP)), "antesP")
			assert.True(t, got.PagoCuotas.Equal(dec(tc.pagoC)), "pagoC")
			assert.True(t, got.AporteCuotas.Equal(dec(tc.aporteC)), "aporteC")
			assert.True(t, got.AtrasoDespuesCuotas.Equal(dec(tc.despuesC)), "despuesC")
			assert.True(t, got.AtrasoDespuesPesos.Equal(dec(tc.despuesP)), "despuesP")
		})
	}
}

func TestCalcularResumenPonderado(t *testing.T) {
	t.Parallel()
	ventas := []rutasdomain.VentaCobranza{
		{Aporte: dec("1.0"), AplicaPonderado: true},
		{Aporte: dec("0.5"), AplicaPonderado: true},
		{Aporte: dec("1.0"), AplicaPonderado: false}, // no cuenta
	}
	got := rutasdomain.CalcularResumenPonderado(ventas)
	assert.Equal(t, 2, got.Denominador)
	assert.True(t, got.Numerador.Equal(dec("1.5")), "numerador")
	if assert.NotNil(t, got.Pct) {
		assert.True(t, got.Pct.Equal(dec("75")), "pct = 1.5/2*100")
	}

	// Sin ventas que aplican → Pct nil, denominador 0.
	empty := rutasdomain.CalcularResumenPonderado([]rutasdomain.VentaCobranza{{Aporte: dec("1"), AplicaPonderado: false}})
	assert.Equal(t, 0, empty.Denominador)
	assert.Nil(t, empty.Pct)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rutas/domain/ -run 'TestDesglosarAporte|TestCalcularResumenPonderado' -v`
Expected: FAIL (undefined: DesglosarAporte / CalcularResumenPonderado).

- [ ] **Step 3: Write minimal implementation** — `internal/rutas/domain/desglose.go`

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import "github.com/shopspring/decimal"

// AporteDesglose is the per-venta breakdown of how a sale contributes to the
// weighted percentage, expressed in both quotas and pesos. It makes the
// office-facing calculation auditable. Pure projection of existing values; it
// does NOT re-run CalcAporte.
type AporteDesglose struct {
	AtrasoAntesCuotas   decimal.Decimal // overdue quotas at window start (= vencidas)
	AtrasoAntesPesos    decimal.Decimal // vencidas × parcialidad
	PagoCuotas          decimal.Decimal // abonoSemana ÷ parcialidad (0 if parcialidad ≤ 0)
	AporteCuotas        decimal.Decimal // capped contribution (= aporte argument)
	AtrasoDespuesCuotas decimal.Decimal // max(0, vencidas − pagoCuotas)
	AtrasoDespuesPesos  decimal.Decimal // atrasoDespuesCuotas × parcialidad
}

// DesglosarAporte projects the aporte calculation into an auditable breakdown.
func DesglosarAporte(parcialidad, vencidas, abonoSemana, aporte decimal.Decimal) AporteDesglose {
	pagoCuotas := decimal.Zero
	if parcialidad.IsPositive() {
		pagoCuotas = abonoSemana.Div(parcialidad)
	}
	atrasoDespues := decimal.Max(decimal.Zero, vencidas.Sub(pagoCuotas))
	return AporteDesglose{
		AtrasoAntesCuotas:   vencidas,
		AtrasoAntesPesos:    vencidas.Mul(parcialidad),
		PagoCuotas:          pagoCuotas,
		AporteCuotas:        aporte,
		AtrasoDespuesCuotas: atrasoDespues,
		AtrasoDespuesPesos:  atrasoDespues.Mul(parcialidad),
	}
}

// ResumenPonderado is the aggregate behind the weighted percentage for a set of
// ventas: numerador = Σ aporte of applicable sales, denominador = count of
// applicable sales, Pct = numerador/denominador×100 (nil when none apply).
type ResumenPonderado struct {
	Numerador   decimal.Decimal
	Denominador int
	Pct         *decimal.Decimal
}

// CalcularResumenPonderado computes the weighted-percentage aggregate. It is the
// single source of truth shared by the zona listing and the breakdown modal so
// both always agree.
func CalcularResumenPonderado(ventas []VentaCobranza) ResumenPonderado {
	var (
		num decimal.Decimal
		den int
	)
	for _, v := range ventas {
		if v.AplicaPonderado {
			den++
			num = num.Add(v.Aporte)
		}
	}
	r := ResumenPonderado{Numerador: num, Denominador: den}
	if den > 0 {
		pct := num.Div(decimal.NewFromInt(int64(den))).Mul(decimal.NewFromInt(100))
		r.Pct = &pct
	}
	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rutas/domain/ -run 'TestDesglosarAporte|TestCalcularResumenPonderado' -v`
Expected: PASS.

- [ ] **Step 5: Refactor `calcReporteZona` to use the shared resumen**

In `internal/rutas/app/listar_rutas.go`, replace the ponderado accumulation block inside `calcReporteZona` so the ponderado metric comes from `rutasdomain.CalcularResumenPonderado(ventas)`:

```go
	// Cobertura (unchanged): keep the existing coberturaNum/coberturaDen loop.
	// Ponderado: delegate to the shared aggregate so listing and modal agree.
	resumen := rutasdomain.CalcularResumenPonderado(ventas)
	reporte := rutasdomain.ReporteZona{ZonaID: zonaID}
	if coberturaDen > 0 {
		pct := decimal.NewFromInt(int64(coberturaNum)).
			Div(decimal.NewFromInt(int64(coberturaDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctCoberturaSemanal = &pct
	}
	reporte.PctPonderadoSemanal = resumen.Pct
	return reporte
```

Remove the now-unused `aporteSum`/`aporteDen` accumulators (and the loop branch that set them). Keep the cobertura loop. If `now`/`fechaInicio` params become unused, drop them and update the call site + `listar_rutas_test.go` accordingly.

- [ ] **Step 6: Run rutas suite + lint**

Run: `go test ./internal/rutas/... && golangci-lint run ./internal/rutas/...`
Expected: PASS, 0 issues.

- [ ] **Step 7: Commit**

```bash
git add internal/rutas/domain/desglose.go internal/rutas/domain/desglose_test.go internal/rutas/app/listar_rutas.go internal/rutas/app/listar_rutas_test.go
git commit -m "feat(rutas): helpers de dominio para desglose de aporte y resumen ponderado"
```

---

### Task 2: Enriquecer datos + DTO del desglose (BE)

**Files:**
- Modify: `internal/rutas/domain/cobranza.go` (campos nuevos en `VentaCobranza`)
- Modify: `internal/rutas/infra/rutasfb/cobranza_repo.go` (SQL + scan)
- Modify: `internal/rutas/infra/rutashttp/cobranza_dto.go` (DTO + resumen)
- Modify: `internal/rutas/infra/rutashttp/cobranza_handlers.go` (mapper + resumen)
- Test: `internal/rutas/infra/rutasfb/cobranza_repo_test.go` (compila en skip-mode; aserción de nuevos campos en integración)

**Interfaces:**
- Consumes: `DesglosarAporte`, `CalcularResumenPonderado` (Task 1).
- Produces: `GET /v2/rutas/{zona}/cobranza` devuelve por venta `cliente_nombre`, `folio`, `docto_pv_id` y los 6 derivados, más un objeto `resumen { numerador, denominador, pct_ponderado }`.

- [ ] **Step 1: Add fields to `VentaCobranza`** — `internal/rutas/domain/cobranza.go`

Agregar al struct (después de `ClienteID`/`ZonaID`):

```go
	// ClienteNombre from CLIENTES.NOMBRE (legacy table; NFC in Go).
	ClienteNombre string
	// Folio de la venta PV (DOCTOS_PV.FOLIO), "" si no se resuelve.
	Folio string
	// DoctoPVID is the linked PV sale id (DOCTOS_ENTRE_SIS bridge), 0 si no se resuelve.
	// Used by the FE to fetch products lazily via the clientes endpoint.
	DoctoPVID int
```

- [ ] **Step 2: Extend the SQL + scan** — `internal/rutas/infra/rutasfb/cobranza_repo.go`

Agregar al SELECT de la query de ventas por zona: `c.NOMBRE` (vía `LEFT JOIN CLIENTES c ON c.CLIENTE_ID = s.CLIENTE_ID`) y dos subqueries escalares correlacionadas para `DOCTO_PV_ID` y `FOLIO`.

**Referencia obligatoria:** copiar el patrón exacto de `internal/clientes/infra/clientesfb/queries.go` (líneas ~290-313): resuelve `DOCTO_PV_ID` vía `DOCTOS_ENTRE_SIS` (`des.DOCTO_DEST_ID = <cargo DOCTO_CC>` → `des.DOCTO_FTE_ID = pv.DOCTO_PV_ID`) y `FOLIO` desde `DOCTOS_PV`. Adaptar el destino al `s.DOCTO_CC_ID` de esta query. Forma:

```sql
COALESCE((
  SELECT FIRST 1 des.DOCTO_FTE_ID
  FROM DOCTOS_ENTRE_SIS des
  WHERE des.DOCTO_DEST_ID = s.DOCTO_CC_ID
), 0) AS DOCTO_PV_ID,
COALESCE((
  SELECT FIRST 1 pv.FOLIO
  FROM DOCTOS_PV pv
  WHERE pv.DOCTO_PV_ID = (
    SELECT FIRST 1 des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
    WHERE des.DOCTO_DEST_ID = s.DOCTO_CC_ID
  )
), '') AS FOLIO
```

(Confirmar en el código de clientes la dirección exacta DEST/FTE y el uso de `FIRST 1` vs `ROWS 1` — usar lo que clientes usa.) Leer `NOMBRE`/`FOLIO` con el helper de lectura legacy (ISO8859_1 → NFC) que ya usan los repos de rutas/clientes; `DOCTO_PV_ID` como int. Mapear a los campos nuevos de `VentaCobranza` en el rowmapper/scan.

- [ ] **Step 3: Extend the DTO** — `internal/rutas/infra/rutashttp/cobranza_dto.go`

Agregar a `VentaCobranzaDTO`:

```go
	ClienteNombre       string `json:"cliente_nombre"        doc:"Nombre del cliente"`
	Folio               string `json:"folio"                 doc:"Folio de la venta (DOCTOS_PV), vacío si no se resuelve"`
	DoctoPVID           int    `json:"docto_pv_id"           doc:"ID de la venta PV para cargar productos (0 si no se resuelve)"`
	AtrasoAntesCuotas   string `json:"atraso_antes_cuotas"   doc:"Cuotas vencidas al inicio de la ventana"`
	AtrasoAntesPesos    string `json:"atraso_antes_pesos"    doc:"Atraso antes en pesos"`
	PagoCuotas          string `json:"pago_cuotas"           doc:"Pago de la semana en cuotas"`
	AtrasoDespuesCuotas string `json:"atraso_despues_cuotas" doc:"Cuotas que siguen vencidas tras el pago"`
	AtrasoDespuesPesos  string `json:"atraso_despues_pesos"  doc:"Atraso después en pesos"`
```

Agregar el resumen al output:

```go
type ResumenPonderadoDTO struct {
	Numerador     string  `json:"numerador"      doc:"Σ aporte de las ventas que aplican (4 decimales)"`
	Denominador   int     `json:"denominador"    doc:"Número de ventas que aplican"`
	PctPonderado  *string `json:"pct_ponderado"  doc:"Porcentaje ponderado (2 decimales) o null si denominador 0"`
}
```

y dentro de `DesglosePorZonaOutput.Body` agregar `Resumen ResumenPonderadoDTO `json:"resumen"``.

- [ ] **Step 4: Map in the handler** — `internal/rutas/infra/rutashttp/cobranza_handlers.go`

En `toVentaCobranzaDTOs`, por cada venta computar el desglose y mapear:

```go
		dg := rutasdomain.DesglosarAporte(v.Parcialidad, v.Vencidas, v.AbonoSemana, v.Aporte)
		dtos[i] = VentaCobranzaDTO{
			// ...campos existentes (incl. AplicaPonderado)...
			ClienteNombre:       v.ClienteNombre,
			Folio:               v.Folio,
			DoctoPVID:           v.DoctoPVID,
			AtrasoAntesCuotas:   dg.AtrasoAntesCuotas.StringFixed(4),
			AtrasoAntesPesos:    dg.AtrasoAntesPesos.StringFixed(2),
			PagoCuotas:          dg.PagoCuotas.StringFixed(4),
			AtrasoDespuesCuotas: dg.AtrasoDespuesCuotas.StringFixed(4),
			AtrasoDespuesPesos:  dg.AtrasoDespuesPesos.StringFixed(2),
		}
```

En el handler `DesglosePorZona`, construir el resumen:

```go
	res := rutasdomain.CalcularResumenPonderado(ventas)
	out.Body.Resumen = ResumenPonderadoDTO{
		Numerador:   res.Numerador.StringFixed(4),
		Denominador: res.Denominador,
	}
	if res.Pct != nil {
		s := res.Pct.StringFixed(2)
		out.Body.Resumen.PctPonderado = &s
	}
```

- [ ] **Step 5: Build + tests + lint**

Run: `go build ./... && go test ./internal/rutas/... && golangci-lint run ./internal/rutas/...`
Expected: build OK, tests PASS (skip-mode repo test compila), 0 issues. Si hay BD local, correr la integración del repo y aseverar que `ClienteNombre`/`Folio`/`DoctoPVID` vienen poblados para una zona conocida.

- [ ] **Step 6: Smoke en vivo (opcional, si el contenedor dev está arriba)**

Run (con token real en `/tmp/fb_token.txt`):
`curl -s -H "Authorization: Bearer $(cat /tmp/fb_token.txt)" "http://localhost:3001/v2/rutas/23301/cobranza" | head -c 800`
Expected: items con `cliente_nombre`, `folio`, `docto_pv_id`, los derivados y un `resumen` con `pct_ponderado` que coincide con el del listado.

- [ ] **Step 7: Commit**

```bash
git add internal/rutas/domain/cobranza.go internal/rutas/infra/rutasfb/cobranza_repo.go internal/rutas/infra/rutashttp/cobranza_dto.go internal/rutas/infra/rutashttp/cobranza_handlers.go internal/rutas/infra/rutasfb/cobranza_repo_test.go
git commit -m "feat(rutas): desglose de cobranza con nombre, folio, docto_pv_id, derivados y resumen"
```

---

### Task 3: Modal + campos nuevos (FE)

**Files (repo `sistema-cobro-web`):**
- Modify: `src/modules/rutas/domain/entities/VentaCobranza.ts`
- Modify: `src/modules/rutas/infrastructure/http/dtos.ts`
- Modify: `src/modules/rutas/infrastructure/mappers/dtoToVentaCobranza.ts`
- Modify: `src/modules/rutas/application/ports/RutasPort.ts` (tipo de retorno de desgloseCobranza + resumen)
- Modify: `src/modules/rutas/application/usecases/desgloseCobranza.ts`
- Modify: `src/modules/rutas/infrastructure/http/HttpRutasAdapter.ts` (mapear `resumen`)
- Modify: `src/modules/rutas/application/__tests__/fakeRutasPort.ts`
- Modify: `src/modules/rutas/components/RutasScreen.tsx` (panel → modal + columnas + encabezado)
- Test: `src/modules/rutas/infrastructure/mappers/__tests__/dtoToVentaCobranza.test.ts`

**Interfaces:**
- Consumes: el contrato del backend de Task 2 (`cliente_nombre`, `folio`, `docto_pv_id`, derivados, `resumen`).
- Produces: `VentaCobranza` con `clienteNombre, folio, doctoPvId, atrasoAntesCuotas, atrasoAntesPesos, pagoCuotas, atrasoDespuesCuotas, atrasoDespuesPesos`; el resultado de `desgloseCobranza` incluye `resumen { numerador: string; denominador: number; pctPonderado: string | null }`.

- [ ] **Step 1: Extend entity + DTO**

`VentaCobranza.ts`: agregar `clienteNombre: string; folio: string; doctoPvId: number; atrasoAntesCuotas: string; atrasoAntesPesos: string; pagoCuotas: string; atrasoDespuesCuotas: string; atrasoDespuesPesos: string;`.

`dtos.ts`: agregar a `VentaCobranzaDTO` los snake_case equivalentes (todos `string` excepto `docto_pv_id: number`). Agregar a `DesgloseCobranzaDTO` un `resumen: { numerador: string; denominador: number; pct_ponderado: string | null }`.

- [ ] **Step 2: Write failing mapper test** — `dtoToVentaCobranza.test.ts`

Agregar a `buildValidDTO` los campos nuevos (`cliente_nombre: "JUAN PÉREZ"`, `folio: "A-123"`, `docto_pv_id: 555`, derivados `"2"/"200.00"/"1"/"1"/"100.00"`). Tests nuevos:
```ts
it("mapea los campos de auditoría", () => {
  const v = dtoToVentaCobranza(buildValidDTO());
  expect(v.clienteNombre).toBe("JUAN PÉREZ");
  expect(v.folio).toBe("A-123");
  expect(v.doctoPvId).toBe(555);
  expect(v.atrasoAntesCuotas).toBe("2");
});
it("lanza DomainError si cliente_nombre no es string", () => {
  expect(() => dtoToVentaCobranza(buildValidDTO({ cliente_nombre: 1 as unknown as string })))
    .toThrowError(expect.objectContaining({ code: "cliente_nombre_invalido" }));
});
it("lanza DomainError si docto_pv_id no es número", () => {
  expect(() => dtoToVentaCobranza(buildValidDTO({ docto_pv_id: "x" as unknown as number })))
    .toThrowError(expect.objectContaining({ code: "docto_pv_id_invalido" }));
});
```

- [ ] **Step 3: Run test (RED)** — `npx vitest run src/modules/rutas/infrastructure/mappers` → FAIL.

- [ ] **Step 4: Implement mapper guards + mapping** — en `dtoToVentaCobranza.ts` agregar guardas (mismo estilo): `cliente_nombre`/`folio` y los 5 derivados `typeof === "string"` (codes `<campo>_invalido`); `docto_pv_id` con `typeof === "number" && Number.isFinite` (code `docto_pv_id_invalido`, permitir 0). Mapear a camelCase en el return.

- [ ] **Step 5: Run test (GREEN)** — `npx vitest run src/modules/rutas/infrastructure/mappers` → PASS.

- [ ] **Step 6: Thread `resumen` through port/usecase/adapter + fake**

- `RutasPort.ts`: el retorno de `desgloseCobranza` pasa a `{ fechaInicioSemana: string | null; ventas: VentaCobranza[]; resumen: { numerador: string; denominador: number; pctPonderado: string | null } }`.
- `HttpRutasAdapter.ts`: mapear `dto.resumen` (snake→camel; `pct_ponderado` → `pctPonderado`).
- `desgloseCobranza.ts`: propagar `resumen` (sin lógica nueva).
- `fakeRutasPort.ts`: `makeFakeVentaCobranza` agrega los campos nuevos (base coherente); `desgloseResponse` por defecto incluye `resumen: { numerador: "0", denominador: 0, pctPonderado: null }`.

- [ ] **Step 7: Convert `DesglosePanel` → modal** — `RutasScreen.tsx`

- Importar `Dialog, DialogContent, DialogHeader, DialogTitle` de `@/components/ui/dialog`.
- En `RutasScreen`, en vez de renderizar `<DesglosePanel>` inline, renderizar `<Dialog open={selectedZonaId !== null} onOpenChange={(o) => !o && setSelectedZonaId(null)}>` con `<DialogContent>` conteniendo el `DesglosePanel`.
- `DesglosePanel` recibe `zonaId`; usa `useDesgloseCobranza(zonaId)` que ahora expone también `resumen`.
- **Encabezado** dentro del modal: zona, semana (`fechaInicio`), y la fórmula:
  `Σ aporte {resumen.numerador} ÷ {resumen.denominador} aplican = {formatPct(resumen.pctPonderado)}` (minimalista, 1 línea).
- **Columnas** de la tabla: Cliente (`venta.clienteNombre || venta.clienteId`) · Folio · Frecuencia · Aplica (badge existente) · Atraso antes (`{Number(atrasoAntesCuotas).toFixed(2)} · {formatMoney(atrasoAntesPesos)}`) · Pago (`{Number(pagoCuotas).toFixed(2)} · {formatMoney(abonoSemana)}`) · Aporte (`Number(aporte).toFixed(2)`) · Atraso después (`{Number(atrasoDespuesCuotas).toFixed(2)} · {formatMoney(atrasoDespuesPesos)}`). Ajustar `colSpan`/skeleton al nuevo número de columnas. Quitar la columna "Cliente" por id.
- `useDesgloseCobranza` (`presentation/hooks/useDesgloseCobranza.ts`): exponer `resumen` además de `ventas`/`fechaInicio` (es additive; el test del hook sigue verde vía el fake).

- [ ] **Step 8: tsc + tests**

Run: `npx tsc --noEmit && npx vitest run src/modules/rutas`
Expected: limpio y verde.

- [ ] **Step 9: Commit**

```bash
git add src/modules/rutas
git commit -m "feat(rutas): desglose de cobranza en modal con nombre, folio y desglose del % (cuotas y pesos)"
```

---

### Task 4: Carga lazy de productos por venta (FE)

**Files (repo `sistema-cobro-web`):**
- Create: `src/modules/rutas/domain/entities/ProductoVenta.ts`
- Modify: `src/modules/rutas/application/ports/RutasPort.ts` (método `obtenerProductos`)
- Create: `src/modules/rutas/application/usecases/obtenerProductosVenta.ts`
- Modify: `src/modules/rutas/infrastructure/http/HttpRutasAdapter.ts` (llamada al endpoint de clientes + mapper)
- Create: `src/modules/rutas/infrastructure/mappers/dtoToProductoVenta.ts`
- Modify: `src/modules/rutas/application/__tests__/fakeRutasPort.ts` (fake del nuevo método)
- Modify: `src/modules/rutas/components/RutasScreen.tsx` (expandir fila → productos)
- Test: `src/modules/rutas/infrastructure/mappers/__tests__/dtoToProductoVenta.test.ts`

**Interfaces:**
- Consumes: endpoint existente `GET /v2/clientes/{clienteId}/ventas/{doctoPvId}` (campo `productos: ProductoVentaDTO[]`). El implementador DEBE leer `internal/clientes/infra/clienteshttp/dto.go` (`ProductoVentaDTO`) en el repo msp-api para los nombres exactos de campos (p.ej. artículo/nombre, cantidad, importe).
- Produces: `RutasPort.obtenerProductos(clienteId: number, doctoPvId: number, signal?): Promise<ProductoVenta[]>`.

- [ ] **Step 1: Define entity + mapper test**

`ProductoVenta.ts`: `export interface ProductoVenta { nombre: string; cantidad: number; importe: string; }` (ajustar a los campos reales del DTO de clientes leído en el contrato).

`dtoToProductoVenta.test.ts`: happy-path (mapea nombre/cantidad/importe) + caso inválido → `DomainError` con code `producto_invalido`.

- [ ] **Step 2: Run (RED)** — `npx vitest run src/modules/rutas/infrastructure/mappers` → FAIL.

- [ ] **Step 3: Implement mapper + usecase + adapter**

- `dtoToProductoVenta.ts`: guardas estilo proyecto + mapeo.
- `obtenerProductosVenta.ts`: `(port, clienteId, doctoPvId, signal) => port.obtenerProductos(...)`.
- `HttpRutasAdapter.ts`: `obtenerProductos` hace `GET /clientes/{clienteId}/ventas/{doctoPvId}` con el `apiClient` existente (mismo baseURL `/v2`, el interceptor agrega el token), toma `data.productos`, mapea con `dtoToProductoVenta`. Manejar error → propagar para que el hook muestre estado de error.
- `RutasPort.ts`: agregar la firma `obtenerProductos`.
- `fakeRutasPort.ts`: implementar `obtenerProductos` (registra llamada; respuesta configurable; soporta `throwOnNext`).

- [ ] **Step 4: Run (GREEN)** — `npx vitest run src/modules/rutas/infrastructure/mappers` → PASS.

- [ ] **Step 5: Expand-on-click en el modal** — `RutasScreen.tsx`

- Estado por fila: `expandedVentaId: number | null` + cache `Record<number, { loading; error; productos }>`.
- Al click en una fila de venta (si `doctoPvId > 0`): si no hay cache, llamar `obtenerProductosVenta(port, venta.clienteId, venta.doctoPvId)`; mostrar fila expandida con skeleton mientras carga; al resolver, render productos (nombre · cantidad · importe) en una sub-fila (`<TableRow><TableCell colSpan={...}>`). Error → mensaje corto "Error al cargar". Si `doctoPvId === 0`, deshabilitar expand (sin productos vinculados).
- Mantener strings de UI minimalistas (español neutro).

- [ ] **Step 6: tsc + tests**

Run: `npx tsc --noEmit && npx vitest run src/modules/rutas`
Expected: limpio y verde.

- [ ] **Step 7: Commit**

```bash
git add src/modules/rutas
git commit -m "feat(rutas): carga lazy de productos por venta en el desglose (reusa endpoint de clientes)"
```

---

## Notas de ejecución
- Tasks 1-2 en `msp-api`, Tasks 3-4 en `sistema-cobro-web`. Ambos repos en `main` local, sin push.
- Task 3 depende del contrato de Task 2; Task 4 depende de Task 3 (modal) y del endpoint de clientes (ya existe). Orden: 1 → 2 → 3 → 4.
- Permisos: el usuario necesita `clientes:leer` (además de `rutas:leer`) para los productos; super_admin ya los tiene.
