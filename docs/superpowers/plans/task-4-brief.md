# Task 4 — Trait catalog (`Rasgo` domain type + `CatalogoRasgos` + helpers)

## Where this fits
The AI picks 1-3 behavioral traits for a client, but ONLY from a curated, finite catalog. This task creates that catalog and its lookup helpers. Later: the port `NarrativeInput` carries this catalog (with definitions) into the LLM prompt; validation filters the LLM's chosen codes against it; the read-path resolves codes → Spanish display labels.

**These traits are behavioral/descriptive and must NOT duplicate the existing deterministic badges** (no "Moroso", "CRÍTICO", segment, or tier — those are separate auditable badges). This is a SEED list the user will refine later; implement it exactly as given.

## Layering (important — prevents an import cycle)
- The **type** `Rasgo` goes in **`internal/analytics/domain/`** (a pure value object — three strings, no deps). Put it in a new file `internal/analytics/domain/rasgo.go`. Domain may only import stdlib (no new deps needed here).
- The **catalog data + helpers** go in **`internal/analytics/app/rasgos_catalogo.go`**, referencing `domain.Rasgo`. (`app` imports `domain` — allowed. `ports/outbound` will later import `domain.Rasgo` for `NarrativeInput` — also allowed. Do NOT put the catalog var in a place `ports` would need to import from `app`.)

## Files

### `internal/analytics/domain/rasgo.go`
```go
package domain

// Rasgo is a curated behavioral trait the analyst-AI may assign to a client.
// Codigo is the stable enum key (English snake_case); Etiqueta is the Spanish
// display label; Definicion is a short Spanish description (~50 words) used to
// anchor the LLM's choice.
type Rasgo struct {
	Codigo     string
	Etiqueta   string
	Definicion string
}
```

### `internal/analytics/app/rasgos_catalogo.go`
- `var CatalogoRasgos = []domain.Rasgo{ ... }` with EXACTLY these 12 entries (codes verbatim, Spanish labels/definitions verbatim — neutral professional Spanish, no colloquialisms):

| Codigo | Etiqueta | Definicion |
|---|---|---|
| `loyal_but_stagnant` | Leal pero estancado | Compra desde hace tiempo y sigue presente, pero su frecuencia y su ticket dejaron de crecer; mantiene la relación sin profundizarla. |
| `recoverable_with_promo` | Recuperable con promoción | Bajó su ritmo o se alejó, pero su historial sugiere que responde a un incentivo puntual para reactivar la compra. |
| `enganche_sensitive` | Sensible al enganche | Su decisión de compra depende del enganche requerido; un enganche alto lo frena y uno accesible lo activa. |
| `seasonal_buyer` | Comprador de temporada | Concentra sus compras en ciertas épocas del año y permanece inactivo el resto, siguiendo un patrón estacional. |
| `pays_in_streaks` | Paga en rachas | Alterna periodos de pagos puntuales con pausas; cumple, pero de forma irregular y por tramos. |
| `steady_reliable` | Cumplido constante | Paga con regularidad y previsibilidad; su comportamiento es estable y de bajo mantenimiento. |
| `dormant_valuable` | Dormido valioso | Lleva tiempo sin comprar, pero su valor histórico lo vuelve prioritario para un intento de reactivación. |
| `high_value_at_risk` | Alto valor en riesgo | Representa ingresos importantes, pero señales recientes de atraso o silencio ponen esa relación en riesgo. |
| `cash_reliable` | Contado confiable | Prefiere comprar de contado y cumple sin generar exposición de crédito; relación sana y de bajo riesgo. |
| `churn_risk` | Riesgo de fuga | Su recencia y la caída de actividad indican que podría dejar de comprar si no se interviene pronto. |
| `growing_relationship` | Relación en crecimiento | Su frecuencia o su ticket vienen subiendo; es un cliente con impulso que conviene acompañar. |
| `price_sensitive` | Sensible al precio | Su compra reacciona al precio y a los descuentos; busca el mejor trato antes de decidir. |

- Helpers (in the same file):
  ```go
  // EsRasgoValido reports whether codigo is a known catalog code.
  func EsRasgoValido(codigo string) bool
  // EtiquetaDe returns the Spanish display label for a code, or "" if unknown.
  func EtiquetaDe(codigo string) string
  ```
  Back both with a package-level `map[string]domain.Rasgo` built once from `CatalogoRasgos` (e.g. a `var rasgoPorCodigo = func() map[string]domain.Rasgo { ... }()`), so lookups are O(1) and there is a single source of truth. Do not hand-maintain a second list.

## Constraints
- CLAUDE.md §3: codes English snake_case; labels/definitions Spanish, neutral professional tone (memory: avoid colloquialisms). Definitions ~50 words max, no trailing period issues — keep them as written above.
- No duplication of deterministic badges.
- Single source of truth: helpers derive from `CatalogoRasgos`.

## Tests — `internal/analytics/app/rasgos_catalogo_test.go`
- `EsRasgoValido`: true for a few known codes (e.g. `loyal_but_stagnant`, `churn_risk`), false for `""`, `"nonexistent"`, `"MOROSO"`.
- `EtiquetaDe`: returns the exact Spanish label for known codes; returns `""` for unknown.
- Catalog integrity: all codes unique; all codes are non-empty snake_case lowercase; all entries have non-empty Etiqueta and Definicion; len == 12.
Keep output pristine.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): catálogo curado de rasgos conductuales para asignación por IA`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-4-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
