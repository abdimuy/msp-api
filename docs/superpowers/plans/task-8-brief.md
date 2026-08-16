# Task 8 — LLM generator adapter (`infra/llm`) + reusable fake

## Where this fits
Implements `outbound.NarrativeGenerator` (Task 6) using the platform LLM client (Task 3) and the trait catalog (Task 4). It builds a FACT-ANCHORED prompt from `NarrativeInput`, requests structured JSON output `{narrativa, rasgos:[codes]}`, and parses it. Also provides a deterministic FAKE generator that all downstream tests (worker, read-path) use — so NOTHING in this plan ever calls a real model.

## Read first
- `internal/analytics/ports/outbound/narrative_generator.go` (Task 6) — the `NarrativeGenerator` interface + `NarrativeInput`/`NarrativeOutput`.
- `internal/platform/llm/client.go` (Task 3) — `Client`, `ChatReq`, `Message`, `ResponseFormat` (Type "json_schema" with `Schema map[string]any`, `Name`). And `IsTransient`, `ErrLLMDisabled`.
- `internal/analytics/domain/rasgo.go` (Task 4) — `domain.Rasgo` (Codigo/Etiqueta/Definicion).

## Files

### 1. `internal/analytics/infra/llm/generator.go`
```go
package llm

// Generator implements outbound.NarrativeGenerator using a local OpenAI-compatible
// LLM. It anchors the model on already-computed facts and constrains trait output
// to the provided catalog via a JSON schema. It never computes scores or bands.
type Generator struct {
	client llm.Client   // platform client (aliased import — see note)
	model  string
}

func NewGenerator(client llm.Client, model string) *Generator
```
Note: the platform package is `github.com/abdimuy/msp-api/internal/platform/llm` and this new package is also named `llm` — import the platform one with an alias, e.g. `llmclient "github.com/abdimuy/msp-api/internal/platform/llm"`, OR name THIS package differently. Pick whichever reads cleanly and avoids collision; if a same-name collision is awkward, name this package `narrativellm` (directory `infra/narrativellm`) — but prefer keeping `infra/llm` with an aliased platform import. State your choice in the report.

`Generar(ctx, in outbound.NarrativeInput) (outbound.NarrativeOutput, error)`:
1. Build the **system message** (Spanish, neutral professional tone — memory: no colloquialisms). Content, roughly:
   > Eres un analista interno de cartera. Escribe para uso interno de oficina, nunca para el cliente. Te doy hechos YA calculados sobre un cliente. NO inventes números, NO cambies ni contradigas las bandas o el riesgo que te paso. Redacta UN solo párrafo en español neutro (máximo ~4 frases) que sintetice los tres ejes (crédito, recompra, valor) y cierre con UNA acción interna recomendada. Además, ELIGE entre 1 y 3 rasgos del catálogo que te doy, usando EXCLUSIVAMENTE sus códigos. Responde SOLO en el formato JSON pedido.
2. Build the **user message** embedding the facts from `in` (bands, scores, segmento, tier, estado de pago, saldo, monetary, montoCLV, frecuencia, recencia, cadencia, días de atraso, % a tiempo) AND the three Fase-1 titulars (CreditoResumen/RecompraResumen/CLVResumen) AND the drivers. Then the **catalog**: for each `in.Catalogo` entry, list `codigo — etiqueta: definicion`. Label everything in Spanish, compact.
3. Set `ChatReq.ResponseFormat` to a `json_schema` constraining the output:
   ```
   {
     "type": "object",
     "properties": {
       "narrativa": {"type": "string"},
       "rasgos": {"type": "array", "items": {"type": "string", "enum": [<all codes from in.Catalogo>]},
                  "minItems": 1, "maxItems": 3}
     },
     "required": ["narrativa", "rasgos"],
     "additionalProperties": false
   }
   ```
   Build the `enum` dynamically from `in.Catalogo` codes (so the model is constrained to valid codes — defense in depth; the app layer still validates). Use `Temperature` 0 (or a low value) for determinism. Name the schema e.g. `"analyst_reading"`.
4. Call `client.Chat(ctx, req)`. On error, return it unchanged (worker decides retry via `llmclient.IsTransient`). 
5. Parse the returned content string as JSON into `{Narrativa string; Rasgos []string}`. If JSON parse fails, return a non-nil error (the worker will skip/fallback). Trim whitespace. Return `outbound.NarrativeOutput{Narrativa, Rasgos}` — do NOT validate codes here (that is the app layer's job); return whatever the model gave, the validator filters.
   - Be defensive: some local models wrap JSON in markdown fences or emit a `<think>...</think>` preamble (qwen). Extract the first balanced top-level `{...}` JSON object from the content before unmarshalling (a small helper `extractJSON(s string) (string, bool)`); if none found → error. Keep this helper unit-tested.

### 2. `internal/analytics/infra/llm/llmfake/generator.go` (reusable fake — its own package so tests anywhere can import it)
```go
package llmfake

// Generator is a deterministic outbound.NarrativeGenerator for tests. It records
// the inputs it received and returns a configured output/error. It never calls a
// real model. Field-configurable so tests can simulate success, an invalid trait
// code, an error, etc.
type Generator struct {
	Out      outbound.NarrativeOutput
	Err      error
	Inputs   []outbound.NarrativeInput // appended on each call (for assertions)
}
func (g *Generator) Generar(ctx context.Context, in outbound.NarrativeInput) (outbound.NarrativeOutput, error)
```
Compile-time assertion `var _ outbound.NarrativeGenerator = (*Generator)(nil)`. Keep it minimal — no logic beyond recording + returning configured values. (If `Out` is zero and `Err` is nil, returning the zero output is fine; tests set what they need.)

## Tests — `internal/analytics/infra/llm/generator_test.go`
Use a small STUB implementing `llmclient.Client` inline (a struct with a `ChatFunc func(ctx, ChatReq) (string, error)` field) — do NOT hit a real LLM or httptest a real Ollama.
- **Happy parse:** stub returns `{"narrativa":"...","rasgos":["loyal_but_stagnant","churn_risk"]}` → `Generar` returns those values.
- **Prompt anchoring:** capture the `ChatReq` the stub received; assert the user message contains key facts (e.g. the BandaCredito value, a titular string) AND every catalog code passed in; assert `ResponseFormat.Type=="json_schema"` and its enum equals the catalog codes.
- **Markdown/think wrapper:** stub returns content with ```json fences``` and/or a `<think>...</think>` prefix around the JSON → still parses (covers `extractJSON`).
- **Bad JSON:** stub returns non-JSON → `Generar` returns an error.
- **Client error passthrough:** stub returns `llmclient.ErrLLMDisabled` (and separately a transient error) → `Generar` returns an error (unchanged or wrapped; assert non-nil).
- A tiny direct test of `extractJSON` edge cases.
Keep output pristine. The fake package needs no test of its own (trivial), but if you add one, make it assert recording behavior — no assertion-free tests.

## Constraints
- CLAUDE.md §2: `infra/llm` may import `ports/outbound`, `domain`, platform `llm`, stdlib. It must not import `app`.
- CLAUDE.md §3: English identifiers/comments; the PROMPT STRINGS are Spanish (user-facing-internal content, neutral professional tone). The prompt is a first version the user tunes at the gated step — make it solid and readable.
- Must cross-compile for Windows (pure stdlib + platform client). No new third-party deps beyond what's already vendored (`encoding/json`).
- The generator does NOT validate/cap/dedup trait codes — that's Task 9. It returns the model's raw selection.

## Verification
- `go build ./...`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./internal/analytics/...`
- `go test ./internal/analytics/infra/llm/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): generador de narrativa LLM anclado a hechos + fake para pruebas`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-8-report.md` (state your package-naming choice and how extractJSON handles wrappers). Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
